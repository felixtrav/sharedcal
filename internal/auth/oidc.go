package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/felixtrav/sharedcal/internal/store"
)

// Identity is what the IdP tells us about a user after a verified sign-in.
type Identity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Groups        []string
}

// Provisioner creates or refreshes the local user for an identity.
type Provisioner interface {
	Provision(ctx context.Context, id Identity) (*store.User, error)
}

// ErrLoginRefused is returned by a Provisioner when the identity may not
// sign in (unmapped groups with ALLOW_UNMAPPED_USERS=false, or deactivated).
var ErrLoginRefused = errors.New("sign-in refused")

// Config configures the auth service.
type Config struct {
	AppURL       *url.URL
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	RoleClaim    string
	Secret       []byte
	// HTTPClient is used for discovery, token exchange and JWKS (tests inject one).
	HTTPClient *http.Client
	// ClientIP extracts the caller's address honoring TRUSTED_PROXIES.
	ClientIP func(*http.Request) string
}

// Service is the OIDC relying party plus session/CSRF middleware.
type Service struct {
	cfg      Config
	Sessions *Sessions
	prov     Provisioner
	log      *slog.Logger
	flow     *sealer
	csrfKey  []byte
	secure   bool
	origin   string

	mu         sync.RWMutex
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	oauth      *oauth2.Config
	endSession string
}

const flowTTL = 10 * time.Minute

// New builds the service. Call Discover before serving sign-ins.
func New(cfg Config, sessions *Sessions, prov Provisioner, log *slog.Logger) (*Service, error) {
	if len(cfg.Secret) < 32 {
		return nil, errors.New("auth: secret must be at least 32 bytes")
	}
	flow, err := newSealer(cfg.Secret, "oidc-flow")
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, cfg.Secret)
	mac.Write([]byte("sharedcal:csrf"))
	if cfg.ClientIP == nil {
		cfg.ClientIP = func(r *http.Request) string { return r.RemoteAddr }
	}
	if cfg.RoleClaim == "" {
		cfg.RoleClaim = "groups"
	}
	return &Service{
		cfg:      cfg,
		Sessions: sessions,
		prov:     prov,
		log:      log,
		flow:     flow,
		csrfKey:  mac.Sum(nil),
		secure:   cfg.AppURL.Scheme == "https",
		origin:   cfg.AppURL.Scheme + "://" + cfg.AppURL.Host,
	}, nil
}

func (s *Service) httpCtx(ctx context.Context) context.Context {
	if s.cfg.HTTPClient != nil {
		return oidc.ClientContext(ctx, s.cfg.HTTPClient)
	}
	return ctx
}

// Discover fetches the provider's discovery document, retrying with
// exponential backoff (capped at 30 s) until it succeeds or ctx ends, so the
// app can start alongside its IdP.
func (s *Service) Discover(ctx context.Context) error {
	delay := time.Second
	for {
		err := s.discoverOnce(ctx)
		if err == nil {
			return nil
		}
		s.log.WarnContext(ctx, "OIDC discovery failed; retrying", "issuer", s.cfg.Issuer, "err", err, "in", delay)
		select {
		case <-ctx.Done():
			return fmt.Errorf("OIDC discovery: %w", err)
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

func (s *Service) discoverOnce(ctx context.Context) error {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(s.httpCtx(dctx), s.cfg.Issuer)
	if err != nil {
		return err
	}
	var meta struct {
		EndSession string   `json:"end_session_endpoint"`
		PKCE       []string `json:"code_challenge_methods_supported"`
	}
	_ = p.Claims(&meta)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.provider = p
	s.verifier = p.Verifier(&oidc.Config{ClientID: s.cfg.ClientID})
	s.oauth = &oauth2.Config{
		ClientID:     s.cfg.ClientID,
		ClientSecret: s.cfg.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  s.cfg.AppURL.String() + "/auth/callback",
		Scopes:       s.cfg.Scopes,
	}
	s.endSession = meta.EndSession
	return nil
}

// Ready reports whether discovery has completed.
func (s *Service) Ready() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.provider != nil
}

func (s *Service) state() (*oauth2.Config, *oidc.IDTokenVerifier, *oidc.Provider, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.oauth, s.verifier, s.provider, s.endSession
}

// Cookie names. The __Host- prefix (Secure, Path=/, no Domain) is used when
// the app is served over HTTPS.
func (s *Service) sessionCookie() string { return s.prefixed("sharedcal_session") }
func (s *Service) csrfCookie() string    { return s.prefixed("sharedcal_csrf") }

const flowCookie = "sharedcal_oidc"

func (s *Service) prefixed(name string) string {
	if s.secure {
		return "__Host-" + name
	}
	return name
}

type flowState struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Return   string `json:"r"`
}

// SafeReturn accepts only local absolute paths, so the post-login redirect
// can never leave the app (no scheme, no //host, no backslash tricks).
func SafeReturn(p string) string {
	if p == "" || len(p) > 2048 || p[0] != '/' || strings.HasPrefix(p, "//") || strings.ContainsAny(p, "\\\r\n\t") {
		return "/"
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "/"
	}
	if strings.HasPrefix(u.Path, "/auth/") {
		return "/"
	}
	return p
}

// Login starts the authorization code flow with PKCE.
func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	oc, _, _, _ := s.state()
	if oc == nil {
		s.errorPage(w, http.StatusServiceUnavailable, "The sign-in provider is not reachable yet. Please try again in a moment.")
		return
	}
	fs := flowState{
		State:    randomToken(24),
		Nonce:    randomToken(24),
		Verifier: oauth2.GenerateVerifier(),
		Return:   SafeReturn(r.URL.Query().Get("return")),
	}
	v, err := seal(s.flow, fs, flowTTL)
	if err != nil {
		s.errorPage(w, http.StatusInternalServerError, "Could not start sign-in.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: flowCookie, Value: v, Path: "/auth/", MaxAge: int(flowTTL.Seconds()),
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
	url := oc.AuthCodeURL(fs.State, oidc.Nonce(fs.Nonce), oauth2.S256ChallengeOption(fs.Verifier))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, url, http.StatusFound)
}

// Callback finishes the flow: checks state, exchanges the code, verifies the
// ID token (signature, issuer, audience, expiry, nonce), provisions the user
// and creates a fresh session.
func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	oc, verifier, provider, _ := s.state()
	if oc == nil {
		s.errorPage(w, http.StatusServiceUnavailable, "The sign-in provider is not reachable yet.")
		return
	}
	c, err := r.Cookie(flowCookie)
	if err != nil {
		s.errorPage(w, http.StatusBadRequest, "Your sign-in attempt expired. Please start again.")
		return
	}
	// One-shot: always clear the flow cookie.
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: "", Path: "/auth/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	fs, err := open[flowState](s.flow, c.Value)
	if err != nil {
		s.errorPage(w, http.StatusBadRequest, "Your sign-in attempt expired. Please start again.")
		return
	}
	q := r.URL.Query()
	if !macEqual(q.Get("state"), fs.State) {
		s.errorPage(w, http.StatusBadRequest, "Sign-in state did not match. Please start again.")
		return
	}
	if e := q.Get("error"); e != "" {
		s.log.InfoContext(r.Context(), "OIDC provider returned an error", "error", e)
		s.errorPage(w, http.StatusForbidden, "The sign-in provider refused the request.")
		return
	}
	ctx, cancel := context.WithTimeout(s.httpCtx(r.Context()), 15*time.Second)
	defer cancel()
	tok, err := oc.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(fs.Verifier))
	if err != nil {
		s.log.WarnContext(ctx, "OIDC code exchange failed", "err", err)
		s.errorPage(w, http.StatusBadGateway, "Could not complete sign-in with the provider.")
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	if rawID == "" {
		s.errorPage(w, http.StatusBadGateway, "The provider did not return an ID token.")
		return
	}
	idt, err := verifier.Verify(ctx, rawID)
	if err != nil {
		s.log.WarnContext(ctx, "ID token verification failed", "err", err)
		s.errorPage(w, http.StatusBadGateway, "The provider's ID token could not be verified.")
		return
	}
	if !macEqual(idt.Nonce, fs.Nonce) {
		s.errorPage(w, http.StatusBadRequest, "Sign-in nonce did not match. Please start again.")
		return
	}
	ident, err := s.identity(ctx, idt, provider, tok)
	if err != nil {
		s.log.WarnContext(ctx, "reading OIDC claims failed", "err", err)
		s.errorPage(w, http.StatusBadGateway, "Could not read your profile from the provider.")
		return
	}
	user, err := s.prov.Provision(r.Context(), ident)
	if errors.Is(err, ErrLoginRefused) {
		s.errorPage(w, http.StatusForbidden, "Your account is not allowed to use this calendar. Ask an administrator for access.")
		return
	}
	if err != nil {
		s.log.ErrorContext(ctx, "provisioning user failed", "err", err)
		s.errorPage(w, http.StatusInternalServerError, "Sign-in failed.")
		return
	}
	// Session rotation: drop any session the browser already had.
	if old, err := r.Cookie(s.sessionCookie()); err == nil && old.Value != "" {
		_ = s.Sessions.Revoke(r.Context(), HashToken(old.Value))
	}
	raw, sess, err := s.Sessions.Create(r.Context(), user.ID, s.cfg.ClientIP(r), r.UserAgent())
	if err != nil {
		s.log.ErrorContext(ctx, "creating session failed", "err", err)
		s.errorPage(w, http.StatusInternalServerError, "Sign-in failed.")
		return
	}
	s.setSessionCookies(w, raw, sess)
	http.Redirect(w, r, fs.Return, http.StatusFound)
}

func (s *Service) identity(ctx context.Context, idt *oidc.IDToken, p *oidc.Provider, tok *oauth2.Token) (Identity, error) {
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return Identity{}, err
	}
	// PocketID puts groups in the ID token when the groups scope is granted;
	// other IdPs only expose them from userinfo.
	if _, ok := claims[s.cfg.RoleClaim]; !ok {
		if ui, err := p.UserInfo(ctx, oauth2.StaticTokenSource(tok)); err == nil {
			var extra map[string]any
			if ui.Subject == idt.Subject && ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, exists := claims[k]; !exists {
						claims[k] = v
					}
				}
			}
		}
	}
	str := func(k string) string { v, _ := claims[k].(string); return strings.TrimSpace(v) }
	name := str("name")
	if name == "" {
		name = str("preferred_username")
	}
	if name == "" {
		name = str("email")
	}
	verified, _ := claims["email_verified"].(bool)
	picture := str("picture")
	if u, err := url.Parse(picture); err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		picture = ""
	}
	return Identity{
		Issuer:        idt.Issuer,
		Subject:       idt.Subject,
		Email:         str("email"),
		EmailVerified: verified,
		Name:          name,
		Picture:       picture,
		Groups:        GroupsFromClaim(claims[s.cfg.RoleClaim]),
	}, nil
}

func (s *Service) setSessionCookies(w http.ResponseWriter, raw string, sess *store.Session) {
	http.SetCookie(w, &http.Cookie{
		Name: s.sessionCookie(), Value: raw, Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name: s.csrfCookie(), Value: s.CSRFToken(sess.ID), Path: "/", Expires: sess.ExpiresAt,
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

func (s *Service) clearCookies(w http.ResponseWriter) {
	for _, n := range []string{s.sessionCookie(), s.csrfCookie()} {
		http.SetCookie(w, &http.Cookie{Name: n, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	}
}

// Logout ends the session and returns the provider's logout URL when it
// advertises RP-initiated logout. It is a POST behind CSRF protection.
func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.sessionCookie()); err == nil && c.Value != "" {
		if err := s.Sessions.Revoke(r.Context(), HashToken(c.Value)); err != nil {
			s.log.ErrorContext(r.Context(), "revoking session failed", "err", err)
		}
	}
	s.clearCookies(w)
	var out struct {
		LogoutURL *string `json:"logoutUrl"`
	}
	if _, _, _, end := s.state(); end != "" {
		if u, err := url.Parse(end); err == nil {
			q := u.Query()
			q.Set("client_id", s.cfg.ClientID)
			q.Set("post_logout_redirect_uri", s.cfg.AppURL.String()+"/")
			u.RawQuery = q.Encode()
			v := u.String()
			out.LogoutURL = &v
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// CSRFToken derives the CSRF token bound to a session hash.
func (s *Service) CSRFToken(sessionHash string) string {
	m := hmac.New(sha256.New, s.csrfKey)
	m.Write([]byte(sessionHash))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

var errorTmpl = template.Must(template.New("e").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sign-in problem · SharedCal</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1.5rem;color:#1f2328;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6edf3;background:#0d1117}a{color:#7cb3ff}}
a{color:#0b5fff}</style></head>
<body><h1>Sign-in problem</h1><p>{{.}}</p><p><a href="/auth/login">Try again</a></p></body></html>`))

func (s *Service) errorPage(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = errorTmpl.Execute(w, msg)
}
