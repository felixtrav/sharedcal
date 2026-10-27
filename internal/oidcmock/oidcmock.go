// Package oidcmock is a tiny OpenID Connect provider for local development
// and tests. It is NOT for production: anyone who can reach it can sign in
// as any of its fixed users.
//
// Users (login_hint): alice (admin), bob (writer), carol (viewer),
// dave (writer). Without login_hint, /authorize shows a picker page.
package oidcmock

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

type user struct {
	Sub, Name, Email string
	Groups           []string
}

var users = map[string]user{
	"alice": {"mock-alice", "Alice Admin", "alice@example.test", []string{"calendar-admins"}},
	"bob":   {"mock-bob", "Bob Writer", "bob@example.test", []string{"calendar-writers"}},
	"carol": {"mock-carol", "Carol Viewer", "carol@example.test", []string{"calendar-viewers"}},
	"dave":  {"mock-dave", "Dave Writer", "dave@example.test", []string{"calendar-writers"}},
}

type grant struct {
	user      user
	nonce     string
	challenge string
	redirect  string
	expires   time.Time
}

type provider struct {
	issuer, clientID, secret string
	key                      *rsa.PrivateKey
	signer                   jose.Signer
	mu                       sync.Mutex
	codes                    map[string]grant
	tokens                   map[string]user
}

// New returns the provider's handler for the given issuer URL.
func New(issuer, clientID, secret string) (http.Handler, error) {
	p := &provider{
		issuer: strings.TrimRight(issuer, "/"), clientID: clientID, secret: secret,
		codes: map[string]grant{}, tokens: map[string]user{},
	}
	var err error
	if p.key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		return nil, err
	}
	p.signer, err = jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "mock"))
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	mux.HandleFunc("GET /userinfo", p.userinfo)
	mux.HandleFunc("GET /end_session", p.endSession)
	return mux, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{
		"issuer": p.issuer, "authorization_endpoint": p.issuer + "/authorize", "token_endpoint": p.issuer + "/token",
		"userinfo_endpoint": p.issuer + "/userinfo", "jwks_uri": p.issuer + "/jwks", "end_session_endpoint": p.issuer + "/end_session",
		"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"}, "code_challenge_methods_supported": []string{"S256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
	})
}

func (p *provider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "mock", Algorithm: "RS256", Use: "sig"}}})
}

// allowedRedirect accepts only loopback callbacks: this provider must never
// hand codes to arbitrary hosts.
func allowedRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/auth/callback" {
		return false
	}
	h := u.Hostname()
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

var picker = template.Must(template.New("p").Parse(`<!doctype html><meta charset="utf-8"><title>Mock sign-in</title>
<body style="font:16px system-ui;max-width:28rem;margin:10vh auto"><h1>Mock identity provider</h1>
<p>Development only. Choose a user:</p><ul>{{range .}}<li><a href="{{.URL}}">{{.Label}}</a></li>{{end}}</ul></body>`))

func (p *provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("client_id") != p.clientID || q.Get("response_type") != "code" || !allowedRedirect(q.Get("redirect_uri")) {
		http.Error(w, "invalid client_id, response_type or redirect_uri", http.StatusBadRequest)
		return
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "PKCE (S256) is required", http.StatusBadRequest)
		return
	}
	name := q.Get("login_hint")
	u, ok := users[name]
	if !ok {
		type item struct{ URL, Label string }
		var items []item
		for _, n := range []string{"alice", "bob", "carol", "dave"} {
			q2 := r.URL.Query()
			q2.Set("login_hint", n)
			items = append(items, item{URL: "/authorize?" + q2.Encode(), Label: users[n].Name + " (" + strings.Join(users[n].Groups, ", ") + ")"})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = picker.Execute(w, items)
		return
	}
	code := random()
	p.mu.Lock()
	p.codes[code] = grant{user: u, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), expires: time.Now().Add(time.Minute)}
	p.mu.Unlock()
	dest, _ := url.Parse(q.Get("redirect_uri"))
	rq := dest.Query()
	rq.Set("code", code)
	rq.Set("state", q.Get("state"))
	dest.RawQuery = rq.Encode()
	http.Redirect(w, r, dest.String(), http.StatusFound)
}

func (p *provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, 400, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != p.clientID || subtle.ConstantTimeCompare([]byte(secret), []byte(p.secret)) != 1 {
		writeJSON(w, 401, map[string]string{"error": "invalid_client"})
		return
	}
	code := r.PostForm.Get("code")
	p.mu.Lock()
	g, found := p.codes[code]
	delete(p.codes, code)
	p.mu.Unlock()
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !found || time.Now().After(g.expires) || r.PostForm.Get("redirect_uri") != g.redirect ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, 400, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.issuer, "sub": g.user.Sub, "aud": p.clientID, "exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(),
		"nonce": g.nonce, "name": g.user.Name, "email": g.user.Email, "email_verified": true, "groups": g.user.Groups,
	}
	idToken, err := jwt.Signed(p.signer).Claims(claims).Serialize()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "server_error"})
		return
	}
	access := random()
	p.mu.Lock()
	p.tokens[access] = g.user
	p.mu.Unlock()
	writeJSON(w, 200, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 300, "id_token": idToken})
}

func (p *provider) userinfo(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	p.mu.Lock()
	u, ok := p.tokens[tok]
	p.mu.Unlock()
	if !ok {
		writeJSON(w, 401, map[string]string{"error": "invalid_token"})
		return
	}
	writeJSON(w, 200, map[string]any{"sub": u.Sub, "name": u.Name, "email": u.Email, "email_verified": true, "groups": u.Groups})
}

func (p *provider) endSession(w http.ResponseWriter, r *http.Request) {
	dest := r.URL.Query().Get("post_logout_redirect_uri")
	u, err := url.Parse(dest)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		http.Error(w, "signed out", http.StatusOK)
		return
	}
	http.Redirect(w, r, dest, http.StatusFound)
}

func random() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
