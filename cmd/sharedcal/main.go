// Command sharedcal is the SharedCal server and its maintenance CLI.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/felixtrav/sharedcal/internal/api"
	"github.com/felixtrav/sharedcal/internal/archive"
	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/backup"
	"github.com/felixtrav/sharedcal/internal/config"
	"github.com/felixtrav/sharedcal/internal/domain"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/web"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

const usage = `SharedCal %s

Usage: sharedcal <command> [flags]

Commands:
  serve                 run the server (default)
  migrate               apply database migrations and exit
  backup --out DIR      back up the database (SQLite) and attachments [--keep N]
  export --out FILE     write a portable archive of all data
  import --in FILE      load an archive into an empty database
  healthcheck           exit 0 if the local server answers /healthz
  openapi               print the OpenAPI 3.1 document
  version               print the version
`

func main() {
	archive.AppVersion = version
	cmd := "serve"
	args := os.Args[1:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx)
	case "migrate":
		err = withConfig(func(cfg *config.Config, log *slog.Logger) error {
			return store.Migrate(ctx, cfg.DBDriver, cfg.DBDSN, log)
		})
	case "backup":
		err = runBackup(ctx, args)
	case "export":
		err = runExport(ctx, args)
	case "import":
		err = runImport(ctx, args)
	case "healthcheck":
		err = healthcheck()
	case "openapi":
		err = printOpenAPI(os.Stdout)
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		fmt.Printf(usage, version)
	default:
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sharedcal:", err)
		os.Exit(1)
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stderr, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, opts))
}

func withConfig(fn func(*config.Config, *slog.Logger) error) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	log := newLogger(cfg)
	slog.SetDefault(log)
	return fn(cfg, log)
}

// openDB migrates (holding the migration lock) and opens the pools.
func openDB(ctx context.Context, cfg *config.Config, log *slog.Logger) (*store.DB, error) {
	if err := store.Migrate(ctx, cfg.DBDriver, cfg.DBDSN, log); err != nil {
		return nil, err
	}
	return store.Open(cfg.DBDriver, cfg.DBDSN)
}

func serve(ctx context.Context) error {
	return withConfig(func(cfg *config.Config, log *slog.Logger) error {
		log.Info("starting SharedCal", "version", version, "db", cfg.DBDriver, "url", cfg.AppURL.String())
		if err := cfg.LoadOrCreateSessionSecret(); err != nil {
			return err
		}
		db, err := openDB(ctx, cfg, log)
		if err != nil {
			return err
		}
		defer db.Close()

		broker := realtime.NewLocal(512)
		presence := realtime.NewPresence(broker)
		sessions := &auth.Sessions{DB: db, IdleTTL: cfg.SessionIdleTTL, MaxTTL: cfg.SessionMaxTTL,
			OnRevoke: func(hashes []string) {
				payload, _ := json.Marshal(hashes)
				broker.Publish(realtime.Message{Type: realtime.TypeSessionsRevoked, Payload: payload})
			}}

		blobs, pipeline, scanner, err := newAttachments(cfg)
		if err != nil {
			return err
		}
		svc := &domain.Service{
			DB: db, Broker: broker, Presence: presence, Log: log, Sessions: sessions,
			Attachments: pipeline, Blobs: blobs, Scanner: scanner,
			Opts: domain.Options{
				Roles:          roleMapping(cfg),
				BootstrapEmail: cfg.BootstrapAdminEmail, BootstrapSub: cfg.BootstrapAdminSub,
				AppURL: cfg.AppURL.String(), DataDir: cfg.DataDir,
				AttachmentMaxSize: cfg.AttachmentMaxSize, AttachmentMaxPerEvent: cfg.AttachmentMaxPerEvent,
				AttachmentMaxPerComment: cfg.AttachmentMaxPerComment, AttachmentQuota: cfg.AttachmentQuota,
				BuiltinThemes: web.Themes(),
			},
		}
		defer svc.Wait()

		clientIP := api.ClientIP(cfg.TrustedProxies)
		authSvc, err := auth.New(auth.Config{
			AppURL: cfg.AppURL, Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
			Scopes: cfg.OIDCScopes, RoleClaim: cfg.OIDCRoleClaim, Secret: cfg.SessionSecret, ClientIP: clientIP,
		}, sessions, svc, log)
		if err != nil {
			return err
		}

		reg := prometheus.NewRegistry()
		reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
		registerMetrics(reg, db, broker, svc)

		srv := api.New(api.Deps{
			Domain: svc, Auth: authSvc, DB: db, Broker: broker, Presence: presence, Log: log,
			SPA: web.Dist(), TrustedProxies: cfg.TrustedProxies, MaxUpload: cfg.AttachmentMaxSize,
			Version: version, Registry: reg,
		})
		bgCtx, cancelBG := context.WithCancel(ctx)
		defer cancelBG()
		srv.RunBackground(bgCtx)
		go presence.Run(bgCtx)
		go svc.RunMaintenance(bgCtx, time.Hour)
		go func() {
			if err := authSvc.Discover(bgCtx); err != nil {
				log.Error("OIDC discovery gave up", "err", err)
				return
			}
			log.Info("OIDC provider discovered", "issuer", cfg.OIDCIssuer)
			srv.SetReady(true)
		}()
		if cfg.BackupSchedule != "" {
			out := filepath.Join(cfg.DataDir, "backups")
			err := backup.Schedule(bgCtx, cfg.BackupSchedule, func(ctx context.Context) {
				if _, err := backup.Run(ctx, db, cfg.DataDir, out, log); err != nil {
					log.Error("scheduled backup failed", "err", err)
					return
				}
				lastBackup.SetToCurrentTime()
				if err := backup.Prune(out, cfg.BackupKeep); err != nil {
					log.Error("pruning backups failed", "err", err)
				}
			})
			if err != nil {
				return fmt.Errorf("BACKUP_SCHEDULE: %w", err)
			}
		}
		if cfg.MetricsAddr != "" {
			go serveMetrics(bgCtx, cfg.MetricsAddr, reg, log)
		}

		httpSrv := &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           srv,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       120 * time.Second,
			MaxHeaderBytes:    64 << 10,
			// No Read/WriteTimeout: uploads and the SSE stream are long-lived;
			// the stream sets per-write deadlines itself.
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		}
		errCh := make(chan error, 1)
		go func() {
			log.Info("listening", "addr", cfg.ListenAddr)
			errCh <- httpSrv.ListenAndServe()
		}()
		select {
		case err := <-errCh:
			return err
		case <-ctx.Done():
		}
		log.Info("shutting down")
		cancelBG()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	})
}

func roleMapping(cfg *config.Config) auth.RoleMapping {
	return auth.RoleMapping{
		AdminGroups: cfg.OIDCAdminGroups, WriterGroups: cfg.OIDCWriterGroups, ViewerGroups: cfg.OIDCViewerGroups,
		DefaultRole: authz.Role(cfg.DefaultRole), AllowUnmapped: cfg.AllowUnmapped,
	}
}

func newAttachments(cfg *config.Config) (attachments.BlobStore, *attachments.Pipeline, attachments.Scanner, error) {
	blobs, err := attachments.NewFSStore(filepath.Join(cfg.DataDir, "attachments"))
	if err != nil {
		return nil, nil, nil, err
	}
	tmp := filepath.Join(cfg.DataDir, "attachments", ".tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, nil, nil, err
	}
	var scanner attachments.Scanner
	if cfg.ClamAVAddr != "" {
		scanner = attachments.NewClamAV(cfg.ClamAVAddr, 2*time.Minute)
	}
	p := &attachments.Pipeline{Store: blobs, MaxSize: cfg.AttachmentMaxSize, MaxPixels: attachments.DefaultMaxPixels, TempDir: tmp, ScanEnabled: scanner != nil}
	return blobs, p, scanner, nil
}

var lastBackup = prometheus.NewGauge(prometheus.GaugeOpts{Name: "sharedcal_backup_last_success_timestamp_seconds", Help: "Time of the last successful scheduled backup."})

func registerMetrics(reg prometheus.Registerer, db *store.DB, broker realtime.Broker, svc *domain.Service) {
	reg.MustRegister(lastBackup)
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "sharedcal_sse_clients", Help: "Open SSE connections."},
		func() float64 { return float64(broker.Clients()) }))
	if sqlDB, err := db.W.DB(); err == nil {
		reg.MustRegister(collectors.NewDBStatsCollector(sqlDB, "write"))
	}
	if db.R != db.W {
		if sqlDB, err := db.R.DB(); err == nil {
			reg.MustRegister(collectors.NewDBStatsCollector(sqlDB, "read"))
		}
	}
	reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "sharedcal_attachment_storage_bytes", Help: "Bytes of attachments stored."},
		func() float64 {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			n, err := svc.StorageUsed(ctx)
			if err != nil {
				return -1
			}
			return float64(n)
		}))
}

func serveMetrics(ctx context.Context, addr string, reg *prometheus.Registry, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	s := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	log.Info("metrics listening", "addr", addr)
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("metrics server failed", "err", err)
	}
}

func runBackup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	out := fs.String("out", "", "output directory (required)")
	keep := fs.Int("keep", 0, "keep only the newest N backups (0 = all)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("backup: --out is required")
	}
	return withConfig(func(cfg *config.Config, log *slog.Logger) error {
		db, err := openDB(ctx, cfg, log)
		if err != nil {
			return err
		}
		defer db.Close()
		dir, err := backup.Run(ctx, db, cfg.DataDir, *out, log)
		if err != nil {
			return err
		}
		fmt.Println(dir)
		if *keep > 0 {
			return backup.Prune(*out, *keep)
		}
		return nil
	})
}

func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("out", "", "archive file to write (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("export: --out is required")
	}
	return withConfig(func(cfg *config.Config, log *slog.Logger) error {
		db, err := openDB(ctx, cfg, log)
		if err != nil {
			return err
		}
		defer db.Close()
		tmp, err := os.CreateTemp(filepath.Dir(*out), ".sharedcal-export-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if err := archive.Export(ctx, db, cfg.DataDir, tmp); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Sync(); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), *out); err != nil {
			return err
		}
		log.Info("export written", "file", *out)
		return nil
	})
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	in := fs.String("in", "", "archive file to read (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *in == "" {
		return errors.New("import: --in is required")
	}
	return withConfig(func(cfg *config.Config, log *slog.Logger) error {
		db, err := openDB(ctx, cfg, log)
		if err != nil {
			return err
		}
		defer db.Close()
		f, err := os.Open(*in)
		if err != nil {
			return err
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if err := archive.Import(ctx, db, cfg.DataDir, f, st.Size()); err != nil {
			return err
		}
		log.Info("import complete", "file", *in)
		return nil
	})
}

// healthcheck probes the local server; used by the image HEALTHCHECK, since
// distroless has no shell or curl.
func healthcheck() error {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %d", resp.StatusCode)
	}
	return nil
}
