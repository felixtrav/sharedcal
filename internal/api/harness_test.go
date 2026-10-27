package api_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/felixtrav/sharedcal/internal/api"
	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/domain"
	"github.com/felixtrav/sharedcal/internal/oidcmock"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type options struct {
	bootstrapSub string
	scanner      attachments.Scanner
	maxUpload    int64
	maxPerEvent  int
	// refuseUnmapped maps only calendar-admins and sets ALLOW_UNMAPPED_USERS=false.
	refuseUnmapped bool
}

type env struct {
	t       *testing.T
	app     *httptest.Server
	idp     *httptest.Server
	db      *store.DB
	svc     *domain.Service
	logs    *syncBuffer
	appURL  string
	srv     *api.Server
	cancel  context.CancelFunc
	broker  *realtime.Local
	presenc *realtime.Presence
}

func newEnv(t *testing.T, o options) *env {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	idpL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://" + idpL.Addr().String()
	idpH, err := oidcmock.New(issuer, "sharedcal", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	idp := httptest.NewUnstartedServer(idpH)
	idp.Listener.Close()
	idp.Listener = idpL
	idp.Start()

	appL, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	appURL := "http://" + appL.Addr().String()
	u, _ := url.Parse(appURL)

	logs := &syncBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db := storetest.Open(t)
	broker := realtime.NewLocal(256)
	presence := realtime.NewPresence(broker)
	sessions := &auth.Sessions{DB: db, IdleTTL: 168 * time.Hour, MaxTTL: 720 * time.Hour,
		OnRevoke: func(hashes []string) {
			payload, _ := json.Marshal(hashes)
			broker.Publish(realtime.Message{Type: realtime.TypeSessionsRevoked, Payload: payload})
		}}
	dataDir := t.TempDir()
	blobs, err := attachments.NewFSStore(filepath.Join(dataDir, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	maxUpload := o.maxUpload
	if maxUpload == 0 {
		maxUpload = 5 << 20
	}
	perEvent := o.maxPerEvent
	if perEvent == 0 {
		perEvent = 10
	}
	pipeline := &attachments.Pipeline{Store: blobs, MaxSize: maxUpload, MaxPixels: attachments.DefaultMaxPixels, TempDir: tmp, ScanEnabled: o.scanner != nil}
	roles := auth.RoleMapping{
		AdminGroups: []string{"calendar-admins"}, WriterGroups: []string{"calendar-writers"}, ViewerGroups: []string{"calendar-viewers"},
		DefaultRole: authz.Viewer, AllowUnmapped: true,
	}
	if o.refuseUnmapped {
		roles = auth.RoleMapping{AdminGroups: []string{"calendar-admins"}, DefaultRole: authz.Viewer, AllowUnmapped: false}
	}
	svc := &domain.Service{
		DB: db, Broker: broker, Presence: presence, Log: log, Sessions: sessions,
		Attachments: pipeline, Blobs: blobs, Scanner: o.scanner,
		Opts: domain.Options{
			Roles: roles, BootstrapSub: o.bootstrapSub, AppURL: appURL, DataDir: dataDir,
			AttachmentMaxSize: maxUpload, AttachmentMaxPerEvent: perEvent, AttachmentMaxPerComment: 5, AttachmentQuota: 1 << 30,
			BuiltinThemes: os.DirFS("../../web/src/themes"),
		},
	}
	authSvc, err := auth.New(auth.Config{
		AppURL: u, Issuer: issuer, ClientID: "sharedcal", ClientSecret: "test-secret",
		Scopes: []string{"openid", "profile", "email", "groups"}, RoleClaim: "groups",
		Secret: []byte("0123456789abcdef0123456789abcdef"), ClientIP: api.ClientIP(nil),
	}, sessions, svc, log)
	if err != nil {
		t.Fatal(err)
	}
	dctx, dcancel := context.WithTimeout(ctx, 10*time.Second)
	defer dcancel()
	if err := authSvc.Discover(dctx); err != nil {
		t.Fatal(err)
	}
	srv := api.New(api.Deps{
		Domain: svc, Auth: authSvc, DB: db, Broker: broker, Presence: presence, Log: log,
		SPA:       fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>SharedCal</title>")}, "assets/app.js": {Data: []byte("console.log(1)")}},
		MaxUpload: maxUpload, Version: "test",
	})
	srv.SetReady(true)
	srv.RunBackground(ctx)
	app := httptest.NewUnstartedServer(srv)
	app.Listener.Close()
	app.Listener = appL
	app.Start()
	e := &env{t: t, app: app, idp: idp, db: db, svc: svc, logs: logs, appURL: appURL, srv: srv, cancel: cancel, broker: broker, presenc: presence}
	t.Cleanup(func() {
		cancel()
		app.CloseClientConnections()
		app.Close()
		idp.Close()
		svc.Wait()
	})
	return e
}

// client is a signed-in browser.
type client struct {
	t    *testing.T
	e    *env
	http *http.Client
	csrf string
	ID   string
	Role string
}

func noRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (e *env) newBrowser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: noRedirect, Timeout: 30 * time.Second}
}

// loginFlow runs the OIDC flow for a mock user and returns the final
// callback response (not followed).
func (e *env) loginFlow(t *testing.T, c *http.Client, user, ret string) *http.Response {
	t.Helper()
	loginURL := e.appURL + "/auth/login"
	if ret != "" {
		loginURL += "?return=" + url.QueryEscape(ret)
	}
	resp := mustDo(t, c, mustReq(t, "GET", loginURL, nil))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login: status %d", resp.StatusCode)
	}
	authz := resp.Header.Get("Location") + "&login_hint=" + user
	resp = mustDo(t, c, mustReq(t, "GET", authz, nil))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: status %d: %s", resp.StatusCode, readAll(resp))
	}
	cb := resp.Header.Get("Location")
	return mustDo(t, c, mustReq(t, "GET", cb, nil))
}

func (e *env) login(t *testing.T, user string) *client {
	t.Helper()
	c := e.newBrowser()
	resp := e.loginFlow(t, c, user, "")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback: %d %s %s", resp.StatusCode, resp.Header.Get("Location"), readAll(resp))
	}
	cl := &client{t: t, e: e, http: c}
	var me struct {
		CSRFToken string `json:"csrfToken"`
		User      struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"user"`
	}
	cl.mustJSON("GET", "/api/me", nil, 200, &me)
	cl.csrf, cl.ID, cl.Role = me.CSRFToken, me.User.ID, me.User.Role
	return cl
}

func mustReq(t *testing.T, method, u string, body io.Reader) *http.Request {
	t.Helper()
	r, err := http.NewRequest(method, u, body)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustDo(t *testing.T, c *http.Client, r *http.Request) *http.Response {
	t.Helper()
	resp, err := c.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func readAll(r *http.Response) string {
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

// do sends a request with CSRF headers on non-GET requests.
func (c *client) do(method, path string, body any, hdr ...string) *http.Response {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	r := mustReq(c.t, method, c.e.appURL+path, rd)
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if method != "GET" {
		r.Header.Set("Origin", c.e.appURL)
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	return mustDo(c.t, c.http, r)
}

// mustJSON performs a request, asserts the status and decodes the body.
func (c *client) mustJSON(method, path string, body any, want int, out any, hdr ...string) {
	c.t.Helper()
	resp := c.do(method, path, body, hdr...)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		c.t.Fatalf("%s %s: status %d, want %d: %s", method, path, resp.StatusCode, want, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			c.t.Fatalf("%s %s: decode: %v: %s", method, path, err, data)
		}
	}
}

func (c *client) status(method, path string, body any, hdr ...string) int {
	c.t.Helper()
	resp := c.do(method, path, body, hdr...)
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

// upload sends a multipart file.
func (c *client) upload(path, filename string, data []byte) *http.Response {
	c.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		c.t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	r := mustReq(c.t, "POST", c.e.appURL+path, &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Origin", c.e.appURL)
	r.Header.Set("X-CSRF-Token", c.csrf)
	return mustDo(c.t, c.http, r)
}

// Event is the subset of the event payload the tests look at.
type Event struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Location    string            `json:"location"`
	Start       string            `json:"start"`
	End         string            `json:"end"`
	StartUTC    time.Time         `json:"startUtc"`
	EndUTC      time.Time         `json:"endUtc"`
	TimeZone    string            `json:"timeZone"`
	AllDay      bool              `json:"allDay"`
	RRule       string            `json:"rrule"`
	Version     int64             `json:"version"`
	Tags        []string          `json:"tags"`
	Owner       domain.UserRef    `json:"owner"`
	Permissions authz.Permissions `json:"permissions"`
	Occurrence  *time.Time        `json:"occurrence"`
	Overridden  bool              `json:"overridden"`
	Counts      domain.EventCounts
	Attachments []domain.AttachmentDTO `json:"attachments"`
}

func (c *client) createEvent(body map[string]any) Event {
	c.t.Helper()
	if body["timeZone"] == nil {
		body["timeZone"] = "UTC"
	}
	if body["start"] == nil {
		body["start"], body["end"] = "2026-11-02T10:00:00", "2026-11-02T11:00:00"
	}
	var e Event
	c.mustJSON("POST", "/api/events", body, 201, &e)
	return e
}

func (c *client) list(from, to string) []Event {
	c.t.Helper()
	var out struct {
		Events []Event `json:"events"`
	}
	c.mustJSON("GET", "/api/events?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to), nil, 200, &out)
	return out.Events
}

// sseClient reads a server-sent event stream.
type sseClient struct {
	resp   *http.Response
	events chan sseEvent
	done   chan struct{}
}

type sseEvent struct {
	ID, Type, Data string
}

func (c *client) stream(lastID string) *sseClient {
	c.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r, _ := http.NewRequestWithContext(ctx, "GET", c.e.appURL+"/api/stream", nil)
	if lastID != "" {
		r.Header.Set("Last-Event-ID", lastID)
	}
	h := &http.Client{Jar: c.http.Jar}
	resp, err := h.Do(r)
	if err != nil {
		cancel()
		c.t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		cancel()
		c.t.Fatalf("stream status %d", resp.StatusCode)
	}
	s := &sseClient{resp: resp, events: make(chan sseEvent, 100), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		defer close(s.events)
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.Type != "" {
					s.events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "id: "):
				ev.ID = line[4:]
			case strings.HasPrefix(line, "event: "):
				ev.Type = line[7:]
			case strings.HasPrefix(line, "data: "):
				ev.Data = line[6:]
			}
		}
	}()
	c.t.Cleanup(func() { cancel(); resp.Body.Close() })
	return s
}

// next waits for the next event of the given type.
func (s *sseClient) next(t *testing.T, typ string) sseEvent {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("stream closed while waiting for %s", typ)
			}
			if ev.Type == typ {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s", typ)
		}
	}
}

// none asserts that no event of the type arrives within d.
func (s *sseClient) none(t *testing.T, typ string, d time.Duration) {
	t.Helper()
	timeout := time.After(d)
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				return
			}
			if ev.Type == typ {
				t.Fatalf("unexpected %s: %s", typ, ev.Data)
			}
		case <-timeout:
			return
		}
	}
}

func jsonPayload(t *testing.T, ev sseEvent, out any) {
	t.Helper()
	var m struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(ev.Data), &m); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(m.Payload, out); err != nil {
		t.Fatalf("%v: %s", err, m.Payload)
	}
}

func ver(v int64) string { return fmt.Sprint(v) }

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
