package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
)

func TestRoleMappingResolve(t *testing.T) {
	m := RoleMapping{AdminGroups: []string{"Cal-Admins"}, WriterGroups: []string{"writers", "editors"}, ViewerGroups: []string{"viewers"}, DefaultRole: authz.Writer, AllowUnmapped: true}
	cases := []struct {
		groups  []string
		role    authz.Role
		matched bool
	}{
		{[]string{"cal-admins"}, authz.Admin, true},
		{[]string{"viewers", "EDITORS"}, authz.Writer, true},
		{[]string{"viewers", "cal-admins", "writers"}, authz.Admin, true},
		{[]string{" viewers "}, authz.Viewer, true},
		{[]string{"other"}, authz.Writer, false},
		{nil, authz.Writer, false},
	}
	for _, c := range cases {
		role, matched, ok := m.Resolve(c.groups)
		if !ok || role != c.role || matched != c.matched {
			t.Errorf("%v: %s %v %v", c.groups, role, matched, ok)
		}
	}
	m.AllowUnmapped = false
	if _, _, ok := m.Resolve([]string{"other"}); ok {
		t.Error("unmapped allowed")
	}
	m.AllowUnmapped, m.DefaultRole = true, "bogus"
	if r, _, _ := m.Resolve(nil); r != authz.Viewer {
		t.Errorf("invalid default role falls back to viewer, got %s", r)
	}
}

func TestMappedDropsUnrelatedGroups(t *testing.T) {
	m := RoleMapping{AdminGroups: []string{"Cal-Admins"}, ViewerGroups: []string{" cal-viewers"}}
	if g := m.Mapped([]string{"cal-admins", "family", "CAL-VIEWERS", "vpn-users"}); strings.Join(g, ",") != "cal-admins,CAL-VIEWERS" {
		t.Errorf("mapped: %v", g)
	}
	if g := m.Mapped(nil); g == nil || len(g) != 0 {
		t.Errorf("no groups must store an empty list, got %#v", g)
	}
}

func TestGroupsFromClaim(t *testing.T) {
	if g := GroupsFromClaim([]any{"a", 1, " b ", ""}); strings.Join(g, ",") != "a,b" {
		t.Errorf("array: %v", g)
	}
	if g := GroupsFromClaim("a, b"); strings.Join(g, ",") != "a,b" {
		t.Errorf("string: %v", g)
	}
	if g := GroupsFromClaim(map[string]any{}); g != nil {
		t.Errorf("object: %v", g)
	}
}

func TestSafeReturn(t *testing.T) {
	cases := map[string]string{
		"":                      "/",
		"/":                     "/",
		"/?event=1&view=week":   "/?event=1&view=week",
		"/settings":             "/settings",
		"//evil.com":            "/",
		"/\\evil.com":           "/",
		"https://evil.com":      "/",
		"evil.com":              "/",
		"/auth/login":           "/",
		"/x\r\nSet-Cookie: a=b": "/",
		"/%2F%2Fevil.com":       "/%2F%2Fevil.com",
		"javascript:alert(1)":   "/",
	}
	for in, want := range cases {
		if got := SafeReturn(in); got != want {
			t.Errorf("SafeReturn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSealedCookie(t *testing.T) {
	s, err := newSealer([]byte("0123456789abcdef0123456789abcdef"), "test")
	if err != nil {
		t.Fatal(err)
	}
	v, err := seal(s, flowState{State: "s", Nonce: "n"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	got, err := open[flowState](s, v)
	if err != nil || got.State != "s" {
		t.Fatalf("roundtrip: %+v %v", got, err)
	}
	if strings.Contains(v, "s\"") {
		t.Error("payload not encrypted")
	}
	// Tampering with any byte fails.
	b := []byte(v)
	b[len(b)/2] ^= 1
	if _, err := open[flowState](s, string(b)); err == nil {
		t.Error("tampered cookie accepted")
	}
	// A different purpose key cannot open it.
	other, _ := newSealer([]byte("0123456789abcdef0123456789abcdef"), "other")
	if _, err := open[flowState](other, v); err == nil {
		t.Error("cross-purpose cookie accepted")
	}
	expired, _ := seal(s, flowState{}, -time.Second)
	if _, err := open[flowState](s, expired); err == nil {
		t.Error("expired cookie accepted")
	}
	if _, err := open[flowState](s, "!!!"); err == nil {
		t.Error("garbage accepted")
	}
}

func TestSessionsExpiryAndTouch(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	revoked := 0
	s := &Sessions{DB: db, IdleTTL: 7 * 24 * time.Hour, MaxTTL: 30 * 24 * time.Hour, Now: func() time.Time { return now }, OnRevoke: func(h []string) { revoked += len(h) }}
	u := store.User{ID: store.NewID(), OIDCIssuer: "i", OIDCSubject: "s", Role: "viewer", RoleSource: "idp", GroupsJSON: "[]", Active: true}
	if err := db.W.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	raw, sess, err := s.Create(ctx, u.ID, "1.2.3.4", strings.Repeat("ü", 400))
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == raw || len(sess.ID) != 64 || len(raw) < 40 {
		t.Fatal("session id must be stored hashed")
	}
	// Within an hour: no touch.
	now = now.Add(30 * time.Minute)
	got, _, err := s.Lookup(ctx, raw)
	if err != nil || !got.LastSeenAt.Equal(sess.LastSeenAt) {
		t.Fatalf("early touch: %v %v", got.LastSeenAt, err)
	}
	// After an hour: idle window extended.
	now = now.Add(time.Hour)
	got, _, _ = s.Lookup(ctx, raw)
	if !got.IdleExpiresAt.Equal(now.Add(7 * 24 * time.Hour)) {
		t.Errorf("idle not extended: %v", got.IdleExpiresAt)
	}
	// Idle timeout.
	now = now.Add(8 * 24 * time.Hour)
	if _, _, err := s.Lookup(ctx, raw); err != ErrNoSession {
		t.Errorf("idle expiry: %v", err)
	}
	// Absolute lifetime even when active.
	now = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	raw2, _, _ := s.Create(ctx, u.ID, "", "")
	for i := 0; i < 31; i++ {
		now = now.Add(24 * time.Hour)
		_, _, err = s.Lookup(ctx, raw2)
	}
	if err != ErrNoSession {
		t.Errorf("absolute expiry: %v", err)
	}
	// Inactive users have no valid sessions; revoke-all reports hashes.
	raw3, s3, _ := s.Create(ctx, u.ID, "", "")
	db.W.Model(&store.User{}).Where("id = ?", u.ID).Update("active", false)
	if _, _, err := s.Lookup(ctx, raw3); err != ErrNoSession || s.Valid(ctx, s3.ID) {
		t.Errorf("inactive user: %v", err)
	}
	revoked = 0
	if err := s.RevokeUser(ctx, u.ID, ""); err != nil || revoked == 0 {
		t.Errorf("revoke user: %v %d", err, revoked)
	}
	if _, _, err := s.Lookup(ctx, strings.Repeat("x", 500)); err != ErrNoSession {
		t.Error("oversized token")
	}
}
