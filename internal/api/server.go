// Package api is SharedCal's HTTP edge: the huma REST operations (which
// generate the OpenAPI 3.1 document), the SSE stream, uploads/downloads, the
// ICS feed, health endpoints and the embedded SPA.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/domain"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
)

// maxJSONBody caps JSON request bodies (uploads have their own limit).
const maxJSONBody = 256 << 10

// Deps are the server's collaborators.
type Deps struct {
	Domain   *domain.Service
	Auth     *auth.Service
	DB       *store.DB
	Broker   realtime.Broker
	Presence *realtime.Presence
	Log      *slog.Logger
	// SPA is the built web app (web/dist); nil serves a placeholder.
	SPA            fs.FS
	TrustedProxies []netip.Prefix
	MaxUpload      int64
	// ImgSources are origins (besides this one) allowed to serve images.
	ImgSources []string
	Version    string
	Registry   prometheus.Registerer
}

// Server is the HTTP handler.
type Server struct {
	Deps
	router   chi.Router
	api      huma.API
	stream   *realtime.Stream
	clientIP func(*http.Request) string
	actors   *actorCache

	writeLimit, uploadLimit, presenceLimit, authLimit, callbackLimit *limiter

	ready atomic.Bool
}

// New builds the handler.
func New(d Deps) *Server {
	s := &Server{
		Deps:          d,
		clientIP:      ClientIP(d.TrustedProxies),
		writeLimit:    newLimiter(60, 30),
		uploadLimit:   newLimiter(20, 10),
		presenceLimit: newLimiter(30, 10),
		authLimit:     newLimiter(60, 30),
		callbackLimit: newLimiter(10, 10),
	}
	s.actors = newActorCache(d.DB, 10*time.Second)
	s.stream = &realtime.Stream{
		Broker:      d.Broker,
		Replay:      d.Domain.Replay,
		Personalize: s.personalize,
		SessionValid: func(ctx context.Context, c realtime.Conn) bool {
			return d.Auth.Sessions.Valid(ctx, c.SessionHash)
		},
		Log: d.Log,
	}
	m := newMetrics(d.Registry)

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(s.recoverer)
	r.Use(requestLogger(d.Log))
	r.Use(m.middleware)
	r.Use(securityHeaders(d.Auth.Secure(), d.ImgSources))
	r.Use(d.Auth.Authenticate)
	r.Use(s.rateLimit)
	s.router = r

	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)

	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", d.Auth.Login)
		r.Get("/callback", d.Auth.Callback)
		r.With(s.csrf).Post("/logout", d.Auth.Logout)
	})
	r.Get("/ics/{file}", s.icsFeed)
	r.Get("/branding/logo", s.logo)

	r.Group(func(r chi.Router) {
		r.Use(s.csrf)
		r.Use(noStore)
		cfg := huma.DefaultConfig("SharedCal API", d.Version)
		cfg.OpenAPIPath = "/api/openapi"
		cfg.DocsPath = ""
		cfg.SchemasPath = "/api/schemas"
		cfg.CreateHooks = nil // no $schema links injected into bodies
		cfg.Info.Description = "REST API of SharedCal. Authenticate with the session cookie; send X-CSRF-Token on every non-GET request."
		cfg.Info.License = &huma.License{Name: "MIT"}
		s.api = humachi.New(r, cfg)
		s.registerOperations()
		r.Get("/api/stream", s.handleStream)
		r.Post("/api/events/{id}/attachments", s.uploadEventAttachment)
		r.Post("/api/comments/{id}/attachments", s.uploadCommentAttachment)
		r.Post("/api/settings/logo", s.uploadLogo)
		r.Get("/api/attachments/{id}/content", s.attachmentContent(false))
		r.Get("/api/attachments/{id}/thumb", s.attachmentContent(true))
		s.documentRawOperations()
	})

	r.NotFound(s.spa)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// OpenAPI returns the generated OpenAPI document.
func (s *Server) OpenAPI() *huma.OpenAPI { return s.api.OpenAPI() }

// SetReady flips /readyz once startup (migrations, OIDC discovery) is done.
func (s *Server) SetReady(v bool) { s.ready.Store(v) }

// RunBackground runs limiter housekeeping until ctx ends.
func (s *Server) RunBackground(ctx context.Context) {
	for _, l := range []*limiter{s.writeLimit, s.uploadLimit, s.presenceLimit, s.authLimit, s.callbackLimit} {
		go l.run(ctx)
	}
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// actorFrom returns the authenticated actor or a 401 problem.
func actorFrom(ctx context.Context) (authz.Actor, error) {
	p := auth.FromContext(ctx)
	if p == nil {
		return authz.Actor{}, errUnauthenticated()
	}
	return p.Actor(), nil
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case !s.ready.Load():
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"starting"}`))
	case s.DB.Ping(ctx) != nil:
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"database unreachable"}`))
	case !s.Auth.Ready():
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"identity provider not discovered"}`))
	default:
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	}
}
