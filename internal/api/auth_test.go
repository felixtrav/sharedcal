package api_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestLoginMapsRolesFromGroups(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	for user, role := range map[string]string{"alice": "admin", "bob": "writer", "carol": "viewer"} {
		if c := e.login(t, user); c.Role != role {
			t.Errorf("%s: role %s, want %s", user, c.Role, role)
		}
	}
}

func TestLoginRejectsTamperedState(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	c := e.newBrowser()
	resp := mustDo(t, c, mustReq(t, "GET", e.appURL+"/auth/login", nil))
	authz, _ := url.Parse(resp.Header.Get("Location"))
	q := authz.Query()
	q.Set("state", "forged")
	q.Set("login_hint", "alice")
	authz.RawQuery = q.Encode()
	resp = mustDo(t, c, mustReq(t, "GET", authz.String(), nil))
	resp = mustDo(t, c, mustReq(t, "GET", resp.Header.Get("Location"), nil))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", resp.StatusCode)
	}
	// No session was created.
	if st := mustDo(t, c, mustReq(t, "GET", e.appURL+"/api/me", nil)).StatusCode; st != 401 {
		t.Fatalf("/api/me after forged state: %d", st)
	}
}

func TestLoginCallbackWithoutFlowCookie(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	c := e.newBrowser()
	resp := mustDo(t, c, mustReq(t, "GET", e.appURL+"/auth/callback?code=x&state=y", nil))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestLoginReturnPathIsLocalOnly(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	for ret, want := range map[string]string{
		"//evil.com/x":         "/",
		"https://evil.com/":    "/",
		"/\\evil.com":          "/",
		"/?event=abc&view=day": "/?event=abc&view=day",
	} {
		c := e.newBrowser()
		resp := e.loginFlow(t, c, "bob", ret)
		if got := resp.Header.Get("Location"); got != want {
			t.Errorf("return=%q: redirected to %q, want %q", ret, got, want)
		}
	}
}

func TestBootstrapAdminSub(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{bootstrapSub: "mock-carol"})
	if c := e.login(t, "carol"); c.Role != "admin" {
		t.Fatalf("bootstrap user role %s", c.Role)
	}
}

func TestUnmappedUsersRefused(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{refuseUnmapped: true})
	c := e.newBrowser()
	if resp := e.loginFlow(t, c, "carol", ""); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("unmapped login: %d", resp.StatusCode)
	}
	if c := e.login(t, "alice"); c.Role != "admin" {
		t.Fatal("mapped admin refused")
	}
}

func TestUnauthenticated(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	c := e.newBrowser()
	resp := mustDo(t, c, mustReq(t, "GET", e.appURL+"/api/events?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z", nil))
	if resp.StatusCode != 401 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("API: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	resp = mustDo(t, c, mustReq(t, "GET", e.appURL+"/calendar?view=week", nil))
	if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), "/auth/login?return=") {
		t.Fatalf("SPA: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	// Static assets are public.
	if resp := mustDo(t, c, mustReq(t, "GET", e.appURL+"/assets/app.js", nil)); resp.StatusCode != 200 {
		t.Fatalf("asset: %d", resp.StatusCode)
	}
	if resp := mustDo(t, c, mustReq(t, "GET", e.appURL+"/api/stream", nil)); resp.StatusCode != 401 {
		t.Fatalf("stream: %d", resp.StatusCode)
	}
}

func TestCSRF(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob := e.login(t, "bob")
	body := map[string]any{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC"}
	cases := map[string][]string{
		"no token":     {"X-CSRF-Token", ""},
		"wrong token":  {"X-CSRF-Token", "AAAA"},
		"wrong origin": {"Origin", "http://evil.example"},
	}
	for name, hdr := range cases {
		if st := bob.status("POST", "/api/events", body, hdr...); st != 403 {
			t.Errorf("%s: status %d", name, st)
		}
	}
	// Another user's token does not work with my session.
	carol := e.login(t, "carol")
	if st := bob.status("POST", "/api/events", body, "X-CSRF-Token", carol.csrf); st != 403 {
		t.Errorf("foreign token: %d", st)
	}
	if st := bob.status("POST", "/api/events", body); st != 201 {
		t.Errorf("valid: %d", st)
	}
}

func TestLogout(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob := e.login(t, "bob")
	var out struct {
		LogoutURL *string `json:"logoutUrl"`
	}
	bob.mustJSON("POST", "/auth/logout", nil, 200, &out)
	if out.LogoutURL == nil || !strings.Contains(*out.LogoutURL, "end_session") {
		t.Fatalf("logout url: %v", out.LogoutURL)
	}
	if st := bob.status("GET", "/api/me", nil); st != 401 {
		t.Fatalf("after logout: %d", st)
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob := e.login(t, "bob")
	for _, path := range []string{"/", "/api/me"} {
		resp := bob.do("GET", path, nil)
		h := resp.Header
		csp := h.Get("Content-Security-Policy")
		if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-eval") {
			t.Errorf("%s CSP: %q", path, csp)
		}
		if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "same-origin" {
			t.Errorf("%s headers: %v", path, h)
		}
	}
}

func TestRateLimitWrites(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	carol := e.login(t, "carol")
	got429 := false
	for i := 0; i < 80; i++ {
		resp := carol.do("POST", "/api/inbox/read", map[string]any{"all": true})
		if resp.StatusCode == 429 {
			if resp.Header.Get("Retry-After") == "" {
				t.Fatal("429 without Retry-After")
			}
			got429 = true
			break
		}
	}
	if !got429 {
		t.Fatal("write burst was never rate limited")
	}
}
