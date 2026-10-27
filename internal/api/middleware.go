package api

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/felixtrav/sharedcal/internal/auth"
)

// ClientIP returns the caller's address. X-Forwarded-For is honored only
// when the direct peer is a trusted proxy, and then the right-most address
// that is not itself a trusted proxy wins (the left part is client-supplied).
func ClientIP(trusted []netip.Prefix) func(*http.Request) string {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a.Unmap()) {
				return true
			}
		}
		return false
	}
	return func(r *http.Request) string {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		peer, err := netip.ParseAddr(host)
		if err != nil {
			return host
		}
		peer = peer.Unmap()
		if len(trusted) == 0 || !isTrusted(peer) {
			return peer.String()
		}
		hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
		for i := len(hops) - 1; i >= 0; i-- {
			a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
			if err != nil {
				break
			}
			a = a.Unmap()
			if !isTrusted(a) {
				return a.String()
			}
		}
		return peer.String()
	}
}

// securityHeaders sets the headers every response carries.
func securityHeaders(secure bool) func(http.Handler) http.Handler {
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data: blob: https:; font-src 'self' data:; connect-src 'self'; " +
		"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
			if secure {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestLogger logs one structured line per request. Secrets in paths
// (ICS feed tokens) are redacted.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			path := r.URL.Path
			if path == "/healthz" || path == "/readyz" {
				return
			}
			if strings.HasPrefix(path, "/ics/") {
				path = "/ics/[redacted].ics"
			}
			attrs := []any{
				"request_id", middleware.GetReqID(r.Context()), "method", r.Method, "path", path,
				"status", ww.Status(), "bytes", ww.BytesWritten(), "duration_ms", time.Since(start).Milliseconds(),
			}
			if p := auth.FromContext(r.Context()); p != nil {
				attrs = append(attrs, "user", p.User.ID)
			}
			level := slog.LevelInfo
			if ww.Status() >= 500 {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http request", attrs...)
		})
	}
}

// metrics records request latency by route pattern.
type metrics struct {
	latency *prometheus.HistogramVec
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "sharedcal_http_request_duration_seconds", Help: "HTTP request latency.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"method", "route", "status"})}
	if reg != nil {
		reg.MustRegister(m.latency)
	}
	return m
}

func (m *metrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" || r.URL.Path == "/api/stream" {
			if route == "" {
				route = "unmatched"
			}
		}
		if r.URL.Path != "/api/stream" {
			m.latency.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Observe(time.Since(start).Seconds())
		}
	})
}

// limiter is a keyed token-bucket rate limiter with idle eviction.
type limiter struct {
	mu      sync.Mutex
	perMin  int
	burst   int
	buckets map[string]*bucket
}

type bucket struct {
	l    *rate.Limiter
	seen time.Time
}

func newLimiter(perMinute, burst int) *limiter {
	return &limiter{perMin: perMinute, burst: burst, buckets: map[string]*bucket{}}
}

func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) > 100_000 {
			l.evictLocked(now)
		}
		b = &bucket{l: rate.NewLimiter(rate.Limit(float64(l.perMin)/60), l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	res := b.l.ReserveN(now, 1)
	if !res.OK() {
		return false, time.Minute
	}
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return false, d
	}
	return true, 0
}

func (l *limiter) evictLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.seen) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// run evicts idle buckets until ctx ends.
func (l *limiter) run(ctx context.Context) {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			l.mu.Lock()
			l.evictLocked(time.Now())
			l.mu.Unlock()
		}
	}
}

func tooMany(w http.ResponseWriter, retry time.Duration) {
	secs := int(retry.Seconds() + 0.999)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeProblem(w, newProblem(http.StatusTooManyRequests, "too many requests; slow down"))
}

// rateLimit applies per-key limits: writes and uploads per session, and
// everything under /auth per client IP.
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/auth/") || strings.HasPrefix(r.URL.Path, "/ics/") {
			if ok, retry := s.authLimit.allow(s.clientIP(r)); !ok {
				if strings.HasPrefix(r.URL.Path, "/auth/") && r.Method == http.MethodGet {
					// People land here in a browser: show a page, not JSON.
					w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retry.Seconds()+0.999))))
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Slow down · SharedCal</title><body style="font:16px system-ui;max-width:32rem;margin:15vh auto;padding:0 1.5rem"><h1>Too many sign-in attempts</h1><p>Please wait a minute, then <a href="/auth/login">try again</a>.</p>`))
					return
				}
				tooMany(w, retry)
				return
			}
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if p := auth.FromContext(r.Context()); p != nil {
				lim := s.writeLimit
				if strings.HasSuffix(r.URL.Path, "/attachments") || strings.HasSuffix(r.URL.Path, "/logo") {
					lim = s.uploadLimit
				}
				if r.URL.Path == "/api/presence" {
					lim = s.presenceLimit
				}
				if ok, retry := lim.allow(p.Session.ID); !ok {
					tooMany(w, retry)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// csrf rejects state-changing requests that fail the CSRF checks.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Auth.CheckCSRF(r); err != nil {
			writeProblem(w, toProblem(r.Context(), s.Log, err))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// recoverer turns panics into 500 problems and logs them.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.Log.ErrorContext(r.Context(), "panic", "panic", v, "request_id", middleware.GetReqID(r.Context()))
				writeProblem(w, newProblem(http.StatusInternalServerError, "internal server error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
