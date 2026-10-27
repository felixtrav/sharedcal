package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// Principal is the authenticated caller of a request.
type Principal struct {
	User    *store.User
	Session *store.Session
	CSRF    string
}

// Actor converts the principal for authz checks.
func (p *Principal) Actor() authz.Actor {
	if p == nil || p.User == nil {
		return authz.Actor{}
	}
	return authz.Actor{ID: p.User.ID, Role: authz.Role(p.User.Role), Active: p.User.Active}
}

type ctxKey struct{}

// FromContext returns the request's principal, or nil when anonymous.
func FromContext(ctx context.Context) *Principal {
	p, _ := ctx.Value(ctxKey{}).(*Principal)
	return p
}

// WithPrincipal attaches a principal (used by tests).
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// Authenticate resolves the session cookie, if any, into a Principal. It
// never rejects; handlers decide whether anonymity is acceptable.
func (s *Service) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.sessionCookie())
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, user, err := s.Sessions.Lookup(r.Context(), c.Value)
		if err != nil {
			if r.Context().Err() != nil {
				return // client went away
			}
			if !errors.Is(err, ErrNoSession) {
				s.log.ErrorContext(r.Context(), "session lookup failed", "err", err)
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			// Stale cookie: clear it so the browser stops sending it.
			s.clearCookies(w)
			next.ServeHTTP(w, r)
			return
		}
		p := &Principal{User: user, Session: sess, CSRF: s.CSRFToken(sess.ID)}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

// ErrCSRF is reported when a state-changing request fails CSRF checks.
var ErrCSRF = errors.New("CSRF check failed")

// CheckCSRF enforces, for every non-safe method: a same-origin Origin (or
// Referer) header, and an X-CSRF-Token header that matches both the CSRF
// cookie (double submit) and the session-bound HMAC.
func (s *Service) CheckCSRF(r *http.Request) error {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	if !s.sameOrigin(r) {
		return ErrCSRF
	}
	p := FromContext(r.Context())
	if p == nil {
		// Unauthenticated writes are rejected by handlers anyway; logout
		// without a session is harmless once the origin is verified.
		return nil
	}
	header := r.Header.Get("X-CSRF-Token")
	c, err := r.Cookie(s.csrfCookie())
	if header == "" || err != nil || !macEqual(header, c.Value) || !macEqual(header, p.CSRF) {
		return ErrCSRF
	}
	return nil
}

func (s *Service) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return o == s.origin
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		return err == nil && u.Scheme+"://"+u.Host == s.origin
	}
	return false
}

// Secure reports whether cookies are issued with the Secure attribute.
func (s *Service) Secure() bool { return s.secure }
