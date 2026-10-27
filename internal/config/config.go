// Package config parses and validates SharedCal's environment configuration.
//
// Configuration is environment variables only. Every secret also accepts a
// _FILE suffix pointing at a file holding the value (Docker/Kubernetes secrets).
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Role names, duplicated here as plain strings to keep config dependency-free.
const (
	RoleViewer = "viewer"
	RoleWriter = "writer"
	RoleAdmin  = "admin"
)

// Config is the fully validated runtime configuration.
type Config struct {
	AppURL     *url.URL
	ListenAddr string
	DataDir    string

	DBDriver string
	DBDSN    string

	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCScopes       []string
	OIDCRoleClaim    string
	OIDCAdminGroups  []string
	OIDCWriterGroups []string
	OIDCViewerGroups []string
	DefaultRole      string
	AllowUnmapped    bool

	BootstrapAdminEmail string
	BootstrapAdminSub   string

	SessionSecret  []byte
	SessionIdleTTL time.Duration
	SessionMaxTTL  time.Duration

	TrustedProxies []netip.Prefix

	LogLevel  string
	LogFormat string

	MetricsAddr string

	AttachmentMaxSize       int64
	AttachmentMaxPerEvent   int
	AttachmentMaxPerComment int
	AttachmentQuota         int64
	ClamAVAddr              string

	BackupSchedule string
	BackupKeep     int
}

// Secure reports whether the public URL is HTTPS, which controls the Secure
// cookie attribute and HSTS.
func (c *Config) Secure() bool { return c.AppURL.Scheme == "https" }

// Getenv abstracts os.Getenv so tests can inject values.
type Getenv func(string) string

// Load reads configuration from the process environment.
func Load() (*Config, error) { return LoadFrom(os.Getenv) }

// LoadFrom reads configuration using the given lookup function. All problems
// are collected and reported together so operators can fix them in one pass.
func LoadFrom(getenv Getenv) (*Config, error) {
	p := &parser{getenv: getenv}
	c := &Config{}

	appURL := p.required("APP_URL")
	if appURL != "" {
		u, err := url.Parse(strings.TrimRight(appURL, "/"))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			p.errf("APP_URL must be an absolute http(s) URL, got %q", appURL)
		} else {
			c.AppURL = u
		}
	}
	c.ListenAddr = p.str("LISTEN_ADDR", ":8080")
	c.DataDir = p.str("DATA_DIR", "/data")

	c.DBDriver = strings.ToLower(p.str("DB_DRIVER", "sqlite"))
	switch c.DBDriver {
	case "sqlite", "postgres", "mysql":
	default:
		p.errf("DB_DRIVER must be sqlite, postgres or mysql, got %q", c.DBDriver)
	}
	c.DBDSN = p.secret("DB_DSN", "")
	if c.DBDSN == "" {
		if c.DBDriver == "sqlite" {
			c.DBDSN = filepath.Join(c.DataDir, "sharedcal.db")
		} else {
			p.errf("DB_DSN is required when DB_DRIVER=%s", c.DBDriver)
		}
	}

	c.OIDCIssuer = p.required("OIDC_ISSUER")
	c.OIDCClientID = p.required("OIDC_CLIENT_ID")
	c.OIDCClientSecret = p.secret("OIDC_CLIENT_SECRET", "")
	if c.OIDCClientSecret == "" {
		p.errf("OIDC_CLIENT_SECRET (or OIDC_CLIENT_SECRET_FILE) is required")
	}
	c.OIDCScopes = strings.Fields(p.str("OIDC_SCOPES", "openid profile email groups"))
	if !contains(c.OIDCScopes, "openid") {
		p.errf("OIDC_SCOPES must include openid")
	}
	c.OIDCRoleClaim = p.str("OIDC_ROLE_CLAIM", "groups")
	c.OIDCAdminGroups = p.list("OIDC_ADMIN_GROUPS")
	c.OIDCWriterGroups = p.list("OIDC_WRITER_GROUPS")
	c.OIDCViewerGroups = p.list("OIDC_VIEWER_GROUPS")
	c.DefaultRole = strings.ToLower(p.str("DEFAULT_ROLE", RoleViewer))
	if !ValidRole(c.DefaultRole) {
		p.errf("DEFAULT_ROLE must be viewer, writer or admin, got %q", c.DefaultRole)
	}
	c.AllowUnmapped = p.boolean("ALLOW_UNMAPPED_USERS", true)

	c.BootstrapAdminEmail = strings.TrimSpace(p.str("BOOTSTRAP_ADMIN_EMAIL", ""))
	c.BootstrapAdminSub = strings.TrimSpace(p.str("BOOTSTRAP_ADMIN_SUB", ""))

	if s := p.secret("SESSION_SECRET", ""); s != "" {
		if len(s) < 32 {
			p.errf("SESSION_SECRET must be at least 32 characters")
		}
		c.SessionSecret = []byte(s)
	}
	c.SessionIdleTTL = p.duration("SESSION_IDLE_TTL", 168*time.Hour)
	c.SessionMaxTTL = p.duration("SESSION_MAX_TTL", 720*time.Hour)
	if c.SessionIdleTTL > c.SessionMaxTTL {
		p.errf("SESSION_IDLE_TTL must not exceed SESSION_MAX_TTL")
	}

	for _, cidr := range p.list("TRUSTED_PROXIES") {
		pfx, err := parsePrefix(cidr)
		if err != nil {
			p.errf("TRUSTED_PROXIES: invalid CIDR %q", cidr)
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, pfx)
	}

	c.LogLevel = strings.ToLower(p.str("LOG_LEVEL", "info"))
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		p.errf("LOG_LEVEL must be debug, info, warn or error")
	}
	c.LogFormat = strings.ToLower(p.str("LOG_FORMAT", "json"))
	if c.LogFormat != "json" && c.LogFormat != "text" {
		p.errf("LOG_FORMAT must be json or text")
	}
	c.MetricsAddr = p.str("METRICS_ADDR", "")
	if c.MetricsAddr != "" {
		if _, _, err := net.SplitHostPort(c.MetricsAddr); err != nil {
			p.errf("METRICS_ADDR must be host:port")
		}
	}

	c.AttachmentMaxSize = p.size("ATTACHMENT_MAX_SIZE", 25<<20)
	c.AttachmentMaxPerEvent = p.integer("ATTACHMENT_MAX_PER_EVENT", 10)
	c.AttachmentMaxPerComment = p.integer("ATTACHMENT_MAX_PER_COMMENT", 5)
	c.AttachmentQuota = p.size("ATTACHMENT_QUOTA", 2<<30)
	c.ClamAVAddr = p.str("CLAMAV_ADDR", "")

	c.BackupSchedule = p.str("BACKUP_SCHEDULE", "")
	c.BackupKeep = p.integer("BACKUP_KEEP", 7)

	if len(p.errs) > 0 {
		return nil, errors.Join(p.errs...)
	}
	return c, nil
}

// LoadOrCreateSessionSecret fills SessionSecret from DATA_DIR when it was not
// set explicitly, generating and persisting a new one on first start.
func (c *Config) LoadOrCreateSessionSecret() error {
	if len(c.SessionSecret) > 0 {
		return nil
	}
	path := filepath.Join(c.DataDir, "session_secret")
	if b, err := os.ReadFile(path); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) >= 32 {
			c.SessionSecret = []byte(s)
			return nil
		}
		return fmt.Errorf("%s is too short; delete it to regenerate", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read session secret: %w", err)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	s := hex.EncodeToString(buf)
	if err := os.MkdirAll(c.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("write session secret: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(s + "\n"); err != nil {
		return fmt.Errorf("write session secret: %w", err)
	}
	c.SessionSecret = []byte(s)
	return nil
}

// ValidRole reports whether r is a known role name.
func ValidRole(r string) bool {
	return r == RoleViewer || r == RoleWriter || r == RoleAdmin
}

type parser struct {
	getenv Getenv
	errs   []error
}

func (p *parser) errf(format string, args ...any) {
	p.errs = append(p.errs, fmt.Errorf(format, args...))
}

func (p *parser) str(key, def string) string {
	if v := strings.TrimSpace(p.getenv(key)); v != "" {
		return v
	}
	return def
}

func (p *parser) required(key string) string {
	v := p.secret(key, "")
	if v == "" {
		p.errf("%s is required", key)
	}
	return v
}

// secret reads KEY, or the contents of the file named by KEY_FILE.
func (p *parser) secret(key, def string) string {
	direct := strings.TrimSpace(p.getenv(key))
	file := strings.TrimSpace(p.getenv(key + "_FILE"))
	if direct != "" && file != "" {
		p.errf("set only one of %s and %s_FILE", key, key)
		return ""
	}
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			p.errf("%s_FILE: %v", key, err)
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	if direct != "" {
		return direct
	}
	return def
}

func (p *parser) list(key string) []string {
	var out []string
	for _, part := range strings.Split(p.getenv(key), ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (p *parser) boolean(key string, def bool) bool {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.errf("%s must be true or false, got %q", key, v)
		return def
	}
	return b
}

func (p *parser) integer(key string, def int) int {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		p.errf("%s must be a non-negative integer, got %q", key, v)
		return def
	}
	return n
}

func (p *parser) duration(key string, def time.Duration) time.Duration {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		p.errf("%s must be a positive duration like 168h, got %q", key, v)
		return def
	}
	return d
}

func (p *parser) size(key string, def int64) int64 {
	v := p.str(key, "")
	if v == "" {
		return def
	}
	n, err := ParseSize(v)
	if err != nil {
		p.errf("%s: %v", key, err)
		return def
	}
	return n
}

// ParseSize parses sizes such as "25MB", "2GB", "512KB" or plain bytes.
// Units are binary (1MB = 1024*1024 bytes), matching common operator intuition
// for upload limits.
func ParseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for _, u := range []struct {
		suffix string
		mult   int64
	}{{"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			mult = u.mult
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	if n > (1<<62)/mult {
		return 0, fmt.Errorf("size %q too large", s)
	}
	return n * mult, nil
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
