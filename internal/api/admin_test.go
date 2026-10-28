package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

func TestAdminEndpointsForbiddenToOthers(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice := e.login(t, "alice")
	for _, c := range []*client{e.login(t, "bob"), e.login(t, "carol")} {
		for _, r := range []struct{ method, path string }{
			{"GET", "/api/users"}, {"PATCH", "/api/users/" + alice.ID}, {"GET", "/api/users/" + alice.ID + "/sessions"},
			{"DELETE", "/api/users/" + alice.ID + "/sessions"}, {"POST", "/api/users/" + alice.ID + "/transfer"},
			{"POST", "/api/tags"}, {"GET", "/api/settings"}, {"PUT", "/api/settings"}, {"GET", "/api/trash"},
			{"POST", "/api/themes"}, {"DELETE", "/api/settings/logo"},
		} {
			var body any
			if r.method != "GET" && r.method != "DELETE" {
				body = map[string]any{}
			}
			if st := c.status(r.method, r.path, body); st != 403 && st != 422 {
				t.Errorf("%s %s %s: %d", c.Role, r.method, r.path, st)
			}
		}
	}
}

func TestTags(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, carol := e.login(t, "alice"), e.login(t, "carol")
	var a, b struct {
		ID string `json:"id"`
	}
	alice.mustJSON("POST", "/api/tags", map[string]any{"name": "Work", "colorSlot": "tag-1"}, 201, &a)
	alice.mustJSON("POST", "/api/tags", map[string]any{"name": "Play", "colorSlot": "tag-2"}, 201, &b)
	if st := alice.status("POST", "/api/tags", map[string]any{"name": "work", "colorSlot": "tag-3"}); st != 422 {
		t.Errorf("duplicate (case-insensitive): %d", st)
	}
	if st := alice.status("POST", "/api/tags", map[string]any{"name": "Red", "colorSlot": "#ff0000"}); st != 422 {
		t.Errorf("hex slot: %d", st)
	}
	alice.mustJSON("PATCH", "/api/tags/"+a.ID, map[string]any{"name": "Office", "colorSlot": "tag-5"}, 200, nil)
	var tags struct {
		Tags []struct {
			ID, Name, ColorSlot string
		} `json:"tags"`
	}
	alice.mustJSON("POST", "/api/tags/reorder", map[string]any{"ids": []string{b.ID, a.ID}}, 200, &tags)
	carol.mustJSON("GET", "/api/tags", nil, 200, &tags)
	if len(tags.Tags) != 2 || tags.Tags[0].ID != b.ID || tags.Tags[1].Name != "Office" || tags.Tags[1].ColorSlot != "tag-5" {
		t.Fatalf("tags: %+v", tags.Tags)
	}
	if st := alice.status("POST", "/api/tags/reorder", map[string]any{"ids": []string{a.ID}}); st != 422 {
		t.Errorf("partial reorder: %d", st)
	}
	ev := e.login(t, "bob").createEvent(map[string]any{"title": "t", "tags": []string{a.ID, b.ID}})
	if st := alice.status("DELETE", "/api/tags/"+a.ID, nil); st != 204 {
		t.Fatal("delete tag")
	}
	var got Event
	carol.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if len(got.Tags) != 1 || got.Tags[0] != b.ID {
		t.Errorf("event tags after delete: %v", got.Tags)
	}
}

func TestUserAdministration(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol")
	// Last admin protection.
	if st := alice.status("PATCH", "/api/users/"+alice.ID, map[string]any{"role": "writer"}); st != 409 {
		t.Errorf("demote last admin: %d", st)
	}
	if st := alice.status("PATCH", "/api/users/"+alice.ID, map[string]any{"active": false}); st != 409 {
		t.Errorf("deactivate last admin: %d", st)
	}
	// Promote carol manually; source becomes manual.
	var u struct {
		Role, RoleSource string
	}
	alice.mustJSON("PATCH", "/api/users/"+carol.ID, map[string]any{"role": "writer"}, 200, &u)
	if u.Role != "writer" || u.RoleSource != "manual" {
		t.Fatalf("promote: %+v", u)
	}
	// Takes effect on the next request without re-login.
	if st := carol.status("POST", "/api/events", map[string]any{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC"}); st != 201 {
		t.Errorf("promoted user create: %d", st)
	}
	// Manual role survives re-login; reset to idp recomputes.
	if c := e.login(t, "carol"); c.Role != "writer" {
		t.Errorf("manual role after relogin: %s", c.Role)
	}
	alice.mustJSON("PATCH", "/api/users/"+carol.ID, map[string]any{"roleSource": "idp"}, 200, &u)
	if u.Role != "viewer" {
		t.Errorf("reset to idp: %+v", u)
	}
	// Sessions list and transfer of ownership.
	var sess struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	alice.mustJSON("GET", "/api/users/"+bob.ID+"/sessions", nil, 200, &sess)
	if len(sess.Sessions) != 1 || len(sess.Sessions[0].ID) != 16 {
		t.Fatalf("sessions: %+v", sess)
	}
	bob.createEvent(map[string]any{"title": "mine"})
	dave := e.login(t, "dave")
	var moved struct {
		Moved int `json:"moved"`
	}
	alice.mustJSON("POST", "/api/users/"+bob.ID+"/transfer", map[string]any{"toUserId": dave.ID}, 200, &moved)
	if moved.Moved != 1 {
		t.Errorf("moved %d", moved.Moved)
	}
	if st := alice.status("POST", "/api/users/"+bob.ID+"/transfer", map[string]any{"toUserId": carol.ID}); st != 422 {
		t.Errorf("transfer to viewer: %d", st)
	}
	// Deactivation revokes sessions immediately and blocks sign-in.
	alice.mustJSON("PATCH", "/api/users/"+bob.ID, map[string]any{"active": false}, 200, nil)
	if st := bob.status("GET", "/api/me", nil); st != 401 {
		t.Errorf("deactivated user still signed in: %d", st)
	}
	c := e.newBrowser()
	if resp := e.loginFlow(t, c, "bob", ""); resp.StatusCode != http.StatusForbidden {
		t.Errorf("deactivated login: %d", resp.StatusCode)
	}
	// Revoke a single session by id.
	alice.mustJSON("GET", "/api/users/"+dave.ID+"/sessions", nil, 200, &sess)
	if st := alice.status("DELETE", "/api/users/"+dave.ID+"/sessions?id="+sess.Sessions[0].ID, nil); st != 204 {
		t.Fatalf("revoke one: %d", st)
	}
	if st := dave.status("GET", "/api/me", nil); st != 401 {
		t.Errorf("revoked session: %d", st)
	}
}

func TestSettingsAndThemes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, carol := e.login(t, "alice"), e.login(t, "carol")
	var st map[string]any
	alice.mustJSON("GET", "/api/settings", nil, 200, &st)
	st["calendarName"] = "Family"
	st["accentColor"] = "#3366cc"
	st["enabledThemes"] = []string{"nord", "bogus"}
	alice.mustJSON("PUT", "/api/settings", st, 200, &st)
	if st["calendarName"] != "Family" {
		t.Fatalf("settings: %v", st)
	}
	st["accentColor"] = "red;}body{"
	if code := alice.status("PUT", "/api/settings", st); code != 422 {
		t.Errorf("bad accent: %d", code)
	}
	var me struct {
		Instance struct {
			CalendarName string `json:"calendarName"`
		} `json:"instance"`
	}
	carol.mustJSON("GET", "/api/me", nil, 200, &me)
	if me.Instance.CalendarName != "Family" {
		t.Errorf("public settings: %+v", me)
	}
	var themes struct {
		Themes []struct {
			ID string `json:"id"`
		} `json:"themes"`
	}
	carol.mustJSON("GET", "/api/themes", nil, 200, &themes)
	ids := []string{}
	for _, th := range themes.Themes {
		ids = append(ids, th.ID)
	}
	if strings.Join(ids, ",") != "default,nord" {
		t.Errorf("enabled themes: %v", ids)
	}
	good := map[string]any{"id": "mono", "name": "Mono", "author": "me", "modes": map[string]any{
		"light": map[string]string{"background": "#ffffff", "foreground": "#111111", "primary": "#000000", "muted": "#eeeeee", "border": "#dddddd", "radius": "0.25rem"},
		"dark":  map[string]string{"background": "#000000", "foreground": "#f0f0f0", "primary": "#ffffff", "muted": "#222222", "border": "#333333"},
	}}
	alice.mustJSON("POST", "/api/themes", good, 201, nil)
	bad := map[string]any{"id": "evil", "name": "Evil", "author": "x", "modes": map[string]any{
		"light": map[string]string{"background": "url(javascript:alert(1))", "foreground": "#111111", "primary": "#000", "muted": "#eee", "border": "#ddd"},
		"dark":  map[string]string{"background": "#000000", "foreground": "#f0f0f0", "primary": "#fff", "muted": "#222", "border": "#333"},
	}}
	if code := alice.status("POST", "/api/themes", bad); code != 422 {
		t.Errorf("injection theme: %d", code)
	}
	if code := alice.status("DELETE", "/api/themes/default", nil); code != 404 {
		t.Errorf("delete builtin: %d", code)
	}
	if code := alice.status("DELETE", "/api/themes/mono", nil); code != 204 {
		t.Errorf("delete uploaded: %d", code)
	}
	// Preferences round trip and validation.
	prefs := map[string]any{"themeId": "nord", "colorMode": "dark", "timeZone": "Europe/Berlin", "weekStart": 0, "clock24h": true, "defaultView": "week", "density": "compact"}
	carol.mustJSON("PUT", "/api/me/preferences", prefs, 200, nil)
	prefs["timeZone"] = "Nowhere/Land"
	if code := carol.status("PUT", "/api/me/preferences", prefs); code != 422 {
		t.Errorf("bad tz: %d", code)
	}
}

func TestICSFeed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob, carol := e.login(t, "bob"), e.login(t, "carol")
	bob.createEvent(map[string]any{"title": "Feed me", "start": "2099-01-01T10:00:00", "end": "2099-01-01T11:00:00"})
	var feed struct {
		URL string `json:"url"`
	}
	carol.mustJSON("POST", "/api/me/feed-token", nil, 200, &feed)
	if !strings.HasPrefix(feed.URL, e.appURL+"/ics/") {
		t.Fatalf("feed url: %s", feed.URL)
	}
	anon := &http.Client{}
	resp, err := anon.Get(feed.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/calendar") || !strings.Contains(string(body), "SUMMARY:Feed me") {
		t.Fatalf("feed: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(feed.URL, e.appURL+"/ics/"), ".ics")
	if strings.Contains(e.logs.String(), token) {
		t.Error("feed token leaked into logs")
	}
	// Rotation invalidates the old URL.
	carol.mustJSON("POST", "/api/me/feed-token", nil, 200, nil)
	resp, _ = anon.Get(feed.URL)
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("old token after rotate: %d", resp.StatusCode)
	}
	resp, _ = anon.Get(e.appURL + "/ics/nonsense.ics")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("bad token: %d", resp.StatusCode)
	}
}

// feedEvent fetches the feed and returns the VEVENT with this UID, or nil.
func feedEvent(t *testing.T, url, uid string) *ical.Component {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	cal, err := ical.NewDecoder(resp.Body).Decode()
	if err != nil {
		t.Fatalf("decode feed: %v", err)
	}
	for _, ev := range cal.Events() {
		if id, _ := ev.Props.Text(ical.PropUID); id == uid {
			return ev.Component
		}
	}
	return nil
}

func TestICSFeedCancelsDeletedEvents(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob := e.login(t, "bob")
	ev := bob.createEvent(map[string]any{"title": "Secret offsite", "location": "HQ", "start": "2099-01-01T10:00:00", "end": "2099-01-01T11:00:00"})
	var feed struct {
		URL string `json:"url"`
	}
	bob.mustJSON("POST", "/api/me/feed-token", nil, 200, &feed)
	live := feedEvent(t, feed.URL, ev.ID)
	if live == nil {
		t.Fatal("live event missing from feed")
	}
	liveSeq, _ := strconv.Atoi(live.Props.Get(ical.PropSequence).Value)

	// Deleted: still listed, as a bare cancellation with a higher SEQUENCE.
	if st := bob.status("DELETE", "/api/events/"+ev.ID, nil); st != 204 {
		t.Fatalf("delete: %d", st)
	}
	gone := feedEvent(t, feed.URL, ev.ID)
	if gone == nil {
		t.Fatal("deleted event dropped from feed instead of cancelled")
	}
	if got := gone.Props.Get(ical.PropStatus).Value; got != "CANCELLED" {
		t.Errorf("status %q", got)
	}
	if seq, _ := strconv.Atoi(gone.Props.Get(ical.PropSequence).Value); seq <= liveSeq {
		t.Errorf("SEQUENCE %d not above %d", seq, liveSeq)
	}
	for _, p := range []string{ical.PropSummary, ical.PropLocation, ical.PropDescription, ical.PropURL} {
		if gone.Props.Get(p) != nil {
			t.Errorf("cancelled entry leaks %s", p)
		}
	}

	// Restored: back in full.
	bob.mustJSON("POST", "/api/events/"+ev.ID+"/restore", nil, 200, nil)
	back := feedEvent(t, feed.URL, ev.ID)
	if back == nil || back.Props.Get(ical.PropStatus).Value != "CONFIRMED" {
		t.Fatalf("restored event: %v", back)
	}
	if title, _ := back.Props.Text(ical.PropSummary); title != "Secret offsite" {
		t.Errorf("restored title %q", title)
	}

	// Past the trash window the cancellation leaves the feed with the event.
	if st := bob.status("DELETE", "/api/events/"+ev.ID, nil); st != 204 {
		t.Fatalf("delete again: %d", st)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := e.db.W.Model(&store.Event{}).Where("id = ?", ev.ID).Update("deleted_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if feedEvent(t, feed.URL, ev.ID) != nil {
		t.Error("event deleted over 30 days ago is still in the feed")
	}
}

func TestSSE(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob, carol, dave := e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")
	cs := carol.stream("")
	ds := dave.stream("")
	time.Sleep(100 * time.Millisecond) // let the subscriptions register

	ev := bob.createEvent(map[string]any{"title": "Live"})
	got := cs.next(t, "event.created")
	var payload Event
	jsonPayload(t, got, &payload)
	if payload.ID != ev.ID || payload.Permissions != (authz.Permissions{Comment: true}) {
		t.Fatalf("viewer payload: %+v", payload)
	}
	jsonPayload(t, ds.next(t, "event.created"), &payload)
	if !payload.Permissions.Suggest || payload.Permissions.Edit {
		t.Errorf("writer payload permissions: %+v", payload.Permissions)
	}
	lastID := got.ID

	// Inbox items reach only their recipient.
	carol.mustJSON("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "hi", "mentions": []string{dave.ID}}, 201, nil)
	ds.next(t, "inbox.item")
	cs.none(t, "inbox.item", 300*time.Millisecond)

	// Presence broadcasts.
	if st := dave.status("POST", "/api/presence", map[string]any{"eventId": ev.ID, "state": "viewing"}); st != 204 {
		t.Fatalf("presence: %d", st)
	}
	var pres struct {
		EventID string `json:"eventId"`
		Viewers []struct {
			ID, State string
		} `json:"viewers"`
	}
	jsonPayload(t, cs.next(t, "presence"), &pres)
	if pres.EventID != ev.ID || len(pres.Viewers) != 1 || pres.Viewers[0].ID != dave.ID {
		t.Errorf("presence: %+v", pres)
	}
	// The viewer is left out of their own copy.
	jsonPayload(t, ds.next(t, "presence"), &pres)
	if pres.EventID != ev.ID || len(pres.Viewers) != 0 {
		t.Errorf("own presence: %+v", pres)
	}

	// Reconnect with Last-Event-ID replays what was missed.
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "Live 2"}, 200, nil, "If-Match", "1")
	replay := carol.stream(lastID)
	jsonPayload(t, replay.next(t, "event.updated"), &payload)
	if payload.Title != "Live 2" {
		t.Errorf("replayed: %+v", payload)
	}
	// An ID older than the retained log asks for a resync. Simulate pruning.
	latest, err := e.svc.LatestSeq(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.W.Exec("DELETE FROM change_log WHERE seq < ?", latest).Error; err != nil {
		t.Fatal(err)
	}
	old := carol.stream("1")
	old.next(t, "resync")

	// Revoking the session closes the stream.
	alice := e.login(t, "alice")
	if st := alice.status("DELETE", "/api/users/"+carol.ID+"/sessions", nil); st != 204 {
		t.Fatal("revoke")
	}
	select {
	case <-cs.done:
	case <-time.After(5 * time.Second):
		t.Fatal("stream not closed after session revocation")
	}
}

func TestEraseUser(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")

	// bob owns an event, comments (one with a reply), uploads, suggests and
	// has a feed token.
	own := bob.createEvent(map[string]any{"title": "Bob's"})
	other := dave.createEvent(map[string]any{"title": "Dave's"})
	var lonely, replied, reply comment
	bob.mustJSON("POST", "/api/events/"+other.ID+"/comments", map[string]any{"body": "secret one"}, 201, &lonely)
	bob.mustJSON("POST", "/api/events/"+other.ID+"/comments", map[string]any{"body": "secret two"}, 201, &replied)
	carol.mustJSON("POST", "/api/events/"+other.ID+"/comments", map[string]any{"body": "reply", "parentId": replied.ID}, 201, &reply)
	r := bob.upload("/api/events/"+own.ID+"/attachments", "notes.txt", []byte("private"))
	if r.StatusCode != 201 {
		t.Fatalf("upload: %d %s", r.StatusCode, readAll(r))
	}
	att := decode[attachmentResp](t, r.Body)
	var sg suggestion
	bob.mustJSON("POST", "/api/events/"+other.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1, "changes": map[string]any{"title": "Bob was here"}}, 201, &sg)
	bob.mustJSON("POST", "/api/me/feed-token", nil, 200, nil)

	// Only admins, never themselves, and live events need a new owner.
	if st := dave.status("DELETE", "/api/users/"+bob.ID, nil); st != 403 {
		t.Errorf("writer erase: %d", st)
	}
	if st := alice.status("DELETE", "/api/users/"+alice.ID, nil); st != 422 {
		t.Errorf("self erase: %d", st)
	}
	if st := alice.status("DELETE", "/api/users/"+bob.ID, nil); st != 422 {
		t.Errorf("erase without transfer: %d", st)
	}
	if st := alice.status("DELETE", "/api/users/"+bob.ID+"?transferTo="+carol.ID, nil); st != 422 {
		t.Errorf("transfer to viewer: %d", st)
	}

	var res struct {
		EventsTransferred  int `json:"eventsTransferred"`
		CommentsRemoved    int `json:"commentsRemoved"`
		AttachmentsRemoved int `json:"attachmentsRemoved"`
		SuggestionsRemoved int `json:"suggestionsRemoved"`
	}
	alice.mustJSON("DELETE", "/api/users/"+bob.ID+"?transferTo="+dave.ID, nil, 200, &res)
	if res.EventsTransferred != 1 || res.CommentsRemoved != 2 || res.AttachmentsRemoved != 1 || res.SuggestionsRemoved != 1 {
		t.Fatalf("result: %+v", res)
	}
	if st := alice.status("DELETE", "/api/users/"+bob.ID+"?transferTo="+dave.ID, nil); st != 409 {
		t.Errorf("erase twice: %d", st)
	}

	// Signed out at once; the event survives under its new owner.
	if st := bob.status("GET", "/api/me", nil); st != 401 {
		t.Errorf("erased user still signed in: %d", st)
	}
	var ev struct {
		Owner struct {
			ID string `json:"id"`
		} `json:"owner"`
	}
	dave.mustJSON("GET", "/api/events/"+own.ID, nil, 200, &ev)
	if ev.Owner.ID != dave.ID {
		t.Errorf("owner after erase: %s", ev.Owner.ID)
	}
	// Content is gone; the thread with a reply keeps an empty placeholder.
	var cs struct {
		Comments []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"comments"`
	}
	dave.mustJSON("GET", "/api/events/"+other.ID+"/comments", nil, 200, &cs)
	for _, c := range cs.Comments {
		if strings.Contains(c.Body, "secret") || c.ID == lonely.ID {
			t.Errorf("comment survived erase: %+v", c)
		}
	}
	if st := dave.status("GET", att.URL, nil); st != 404 {
		t.Errorf("attachment after erase: %d", st)
	}
	var sgs struct {
		Suggestions []struct {
			ID string `json:"id"`
		} `json:"suggestions"`
	}
	dave.mustJSON("GET", "/api/events/"+other.ID+"/suggestions?all=true", nil, 200, &sgs)
	if len(sgs.Suggestions) != 0 {
		t.Errorf("suggestions after erase: %+v", sgs)
	}
	// The audit log keeps the actor as "Deleted user" without the content.
	var act struct {
		Items []struct {
			Actor *struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"actor"`
			Action string          `json:"action"`
			After  json.RawMessage `json:"after"`
			Before json.RawMessage `json:"before"`
		} `json:"items"`
	}
	alice.mustJSON("GET", "/api/activity?actor="+bob.ID+"&limit=200", nil, 200, &act)
	if len(act.Items) == 0 {
		t.Fatal("erased user's activity rows were removed")
	}
	for _, it := range act.Items {
		if it.Actor == nil || it.Actor.Name != "Deleted user" {
			t.Errorf("actor not anonymized: %+v", it.Actor)
		}
		if strings.Contains(string(it.After)+string(it.Before), "secret") || strings.Contains(string(it.After)+string(it.Before), "bob@") {
			t.Errorf("%s keeps erased content: %s %s", it.Action, it.Before, it.After)
		}
	}
	// People shows the row as erased; the directory no longer lists it.
	var people struct {
		Users []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Email  string `json:"email"`
			Erased bool   `json:"erased"`
		} `json:"users"`
	}
	alice.mustJSON("GET", "/api/users", nil, 200, &people)
	for _, u := range people.Users {
		if u.ID == bob.ID && (!u.Erased || u.Email != "" || u.Name != "Deleted user") {
			t.Errorf("people row: %+v", u)
		}
	}
	// Signing in again creates a fresh account.
	if again := e.login(t, "bob"); again.ID == bob.ID {
		t.Error("erased identity was reused")
	}
}
