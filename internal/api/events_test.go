package api_test

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/felixtrav/sharedcal/internal/authz"
)

func TestEventPermissionMatrix(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")

	// Create: writers and admins only.
	if st := carol.status("POST", "/api/events", map[string]any{"title": "v", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC"}); st != 403 {
		t.Fatalf("viewer create: %d", st)
	}
	ev := bob.createEvent(map[string]any{"title": "Bob's"})

	// The permissions object reflects role × ownership.
	want := map[*client]authz.Permissions{
		alice: {Edit: true, Delete: true, Suggest: true, Comment: true, Attach: true, Review: true},
		bob:   {Edit: true, Delete: true, Suggest: false, Comment: true, Attach: true, Review: true},
		carol: {Comment: true},
		dave:  {Suggest: true, Comment: true},
	}
	for c, p := range want {
		var got Event
		c.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
		if got.Permissions != p {
			t.Errorf("%s permissions: %+v want %+v", c.Role+"/"+c.ID[:4], got.Permissions, p)
		}
	}

	// PATCH: non-owner writer gets 403 with suggest:true; viewer plain 403.
	var prob struct {
		Suggest bool `json:"suggest"`
	}
	dave.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "x"}, 403, &prob, "If-Match", "1")
	if !prob.Suggest {
		t.Error("non-owner writer should be told to suggest")
	}
	prob.Suggest = false
	carol.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "x"}, 403, &prob, "If-Match", "1")
	if prob.Suggest {
		t.Error("viewer must not be offered suggestions")
	}
	// Owner: If-Match required (428), stale → 409 with current.
	if st := bob.status("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "x"}); st != 428 {
		t.Errorf("missing If-Match: %d", st)
	}
	var upd Event
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "v2"}, 200, &upd, "If-Match", `"1"`)
	if upd.Version != 2 || upd.Title != "v2" {
		t.Fatalf("update: %+v", upd)
	}
	var conflict struct {
		Current Event `json:"current"`
	}
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "stale"}, 409, &conflict, "If-Match", "1")
	if conflict.Current.Version != 2 || conflict.Current.Title != "v2" {
		t.Errorf("409 current: %+v", conflict.Current)
	}
	// Admin can edit anyone's event.
	alice.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "v3"}, 200, &upd, "If-Match", "2")

	// DELETE: viewer and non-owner writer refused; owner allowed.
	for _, c := range []*client{carol, dave} {
		if st := c.status("DELETE", "/api/events/"+ev.ID, nil); st != 403 {
			t.Errorf("delete by %s: %d", c.Role, st)
		}
	}
	if st := bob.status("DELETE", "/api/events/"+ev.ID, nil); st != 204 {
		t.Fatalf("owner delete: %d", st)
	}
	// Owner can undo right away; non-admins cannot see the trash.
	bob.mustJSON("POST", "/api/events/"+ev.ID+"/restore", nil, 200, &upd)
	if st := bob.status("GET", "/api/trash", nil); st != 403 {
		t.Errorf("writer trash: %d", st)
	}
	// Admin delete + restore from trash; the owner cannot restore an admin's delete.
	if st := alice.status("DELETE", "/api/events/"+ev.ID, nil); st != 204 {
		t.Fatal("admin delete")
	}
	if st := bob.status("POST", "/api/events/"+ev.ID+"/restore", nil); st != 403 {
		t.Errorf("owner restoring admin delete: %d", st)
	}
	var trash struct {
		Events []Event `json:"events"`
	}
	alice.mustJSON("GET", "/api/trash", nil, 200, &trash)
	if len(trash.Events) != 1 {
		t.Fatalf("trash: %d", len(trash.Events))
	}
	alice.mustJSON("POST", "/api/events/"+ev.ID+"/restore", nil, 200, &upd)
	// Admin ownership transfer via ownerId; writers cannot.
	if st := bob.status("PATCH", "/api/events/"+ev.ID, map[string]any{"ownerId": dave.ID}, "If-Match", ver(upd.Version)); st != 403 {
		t.Errorf("writer ownerId: %d", st)
	}
	alice.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"ownerId": dave.ID}, 200, &upd, "If-Match", ver(upd.Version))
	if upd.Owner.ID != dave.ID {
		t.Error("ownership not transferred")
	}
}

func TestEventValidation(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob := e.login(t, "bob")
	bad := []map[string]any{
		{"title": "", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC"},
		{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T09:00:00", "timeZone": "UTC"},
		{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "Mars/Olympus"},
		{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC", "rrule": "FREQ=HOURLY"},
		{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC", "rrule": "FREQ=DAILY;BYMINUTE=1,2,3"},
		{"title": "x", "start": "2026-11-02", "end": "2026-11-02", "timeZone": "UTC", "allDay": true},
		{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC", "tags": []string{"nope"}},
	}
	for i, b := range bad {
		if st := bob.status("POST", "/api/events", b); st != 422 {
			t.Errorf("case %d: status %d", i, st)
		}
	}
	// Unknown JSON fields are rejected rather than ignored.
	if st := bob.status("POST", "/api/events", map[string]any{"title": "x", "start": "2026-11-02T10:00:00", "end": "2026-11-02T11:00:00", "timeZone": "UTC", "ownerID": "x", "evil": 1}); st != 422 {
		t.Errorf("unknown field: %d", st)
	}
	// Range limit.
	if st := bob.status("GET", "/api/events?from=2026-01-01T00:00:00Z&to=2028-01-01T00:00:00Z", nil); st != 422 {
		t.Errorf("huge range: %d", st)
	}
	// All-day: dates, exclusive end.
	ev := bob.createEvent(map[string]any{"title": "Trip", "start": "2026-11-02", "end": "2026-11-05", "allDay": true})
	if ev.Start != "2026-11-02" || ev.End != "2026-11-05" || !ev.AllDay {
		t.Errorf("all-day: %+v", ev)
	}
	// Body cap: 256 KB.
	big := make([]byte, 300<<10)
	for i := range big {
		big[i] = 'a'
	}
	if st := bob.status("POST", "/api/events", map[string]any{"title": "x", "description": string(big)}); st != 413 {
		t.Errorf("oversized body: %d", st)
	}
}

func TestSearchAndFilters(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var tag struct {
		ID string `json:"id"`
	}
	alice.mustJSON("POST", "/api/tags", map[string]any{"name": "Social", "colorSlot": "tag-3"}, 201, &tag)
	bob.createEvent(map[string]any{"title": "Board games", "location": "Dana's 100%_place", "tags": []string{tag.ID}})
	alice.createEvent(map[string]any{"title": "Dentist"})
	from, to := "2026-11-01T00:00:00Z", "2026-11-08T00:00:00Z"
	q := func(params string) int {
		var out struct {
			Events []Event `json:"events"`
		}
		bob.mustJSON("GET", "/api/events?from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to)+params, nil, 200, &out)
		return len(out.Events)
	}
	if n := q(""); n != 2 {
		t.Fatalf("all: %d", n)
	}
	if n := q("&q=board"); n != 1 {
		t.Errorf("q=board: %d", n)
	}
	if n := q("&q=" + url.QueryEscape("100%_")); n != 1 {
		t.Errorf("wildcards are literal: %d", n)
	}
	if n := q("&q=" + url.QueryEscape("%")); n != 1 {
		t.Errorf("percent literal: %d", n)
	}
	if n := q("&tag=" + tag.ID); n != 1 {
		t.Errorf("tag: %d", n)
	}
	if n := q("&owner=" + alice.ID); n != 1 {
		t.Errorf("owner: %d", n)
	}
	if n := q("&owner=" + alice.ID + "&owner=" + bob.ID); n != 2 {
		t.Errorf("owners: %d", n)
	}
}

func TestRecurringDSTAndScopes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob := e.login(t, "bob")
	ev := bob.createEvent(map[string]any{"title": "Game night", "start": "2026-10-23T19:00:00", "end": "2026-10-23T23:00:00",
		"timeZone": "America/New_York", "rrule": "FREQ=WEEKLY;BYDAY=FR;COUNT=6"})
	occ := bob.list("2026-10-20T00:00:00Z", "2026-12-31T00:00:00Z")
	if len(occ) != 6 {
		t.Fatalf("occurrences: %d", len(occ))
	}
	for _, o := range occ {
		if o.Start[11:] != "19:00:00" {
			t.Errorf("wall time drifted across DST: %s", o.Start)
		}
	}
	if occ[0].StartUTC.Hour() != 23 || occ[2].StartUTC.Hour() != 0 {
		t.Errorf("UTC instants: %v %v", occ[0].StartUTC, occ[2].StartUTC)
	}
	v := ev.Version

	// scope=this: move one occurrence an hour later.
	third := occ[2].Occurrence.Format(time.RFC3339)
	var upd Event
	bob.mustJSON("PATCH", "/api/events/"+ev.ID+"?scope=this&occurrence="+url.QueryEscape(third),
		map[string]any{"title": "Special", "start": "2026-11-06T20:00:00", "end": "2026-11-06T23:30:00"}, 200, &upd, "If-Match", ver(v))
	v = upd.Version
	occ = bob.list("2026-10-20T00:00:00Z", "2026-12-31T00:00:00Z")
	if len(occ) != 6 || occ[2].Title != "Special" || occ[2].Start != "2026-11-06T20:00:00" || !occ[2].Overridden || occ[1].Title != "Game night" {
		t.Fatalf("after this-edit: %+v", occ[2])
	}
	// scope=this with a series-only field is refused.
	if st := bob.status("PATCH", "/api/events/"+ev.ID+"?scope=this&occurrence="+url.QueryEscape(third), map[string]any{"rrule": ""}, "If-Match", ver(v)); st != 422 {
		t.Errorf("this + rrule: %d", st)
	}
	// Not an occurrence.
	if st := bob.status("PATCH", "/api/events/"+ev.ID+"?scope=this&occurrence=2026-10-24T23:00:00Z", map[string]any{"title": "x"}, "If-Match", ver(v)); st != 422 {
		t.Errorf("bogus occurrence: %d", st)
	}

	// scope=all with occurrence: drag the 2nd occurrence by +1 day; the
	// whole series shifts and the override follows.
	second := occ[1].Occurrence.Format(time.RFC3339)
	bob.mustJSON("PATCH", "/api/events/"+ev.ID+"?scope=all&occurrence="+url.QueryEscape(second),
		map[string]any{"start": occ[1].StartUTC.Add(24 * time.Hour).Format(time.RFC3339), "end": occ[1].EndUTC.Add(24 * time.Hour).Format(time.RFC3339)}, 200, &upd, "If-Match", ver(v))
	v = upd.Version
	if upd.Start != "2026-10-24T19:00:00" {
		t.Fatalf("series start after shift: %s", upd.Start)
	}
	occ = bob.list("2026-10-20T00:00:00Z", "2026-12-31T00:00:00Z")
	if len(occ) != 6 || occ[2].Title != "Special" {
		t.Fatalf("override lost after shifting series: %d %+v", len(occ), occ[2])
	}

	// scope=following from the 4th occurrence: split.
	fourth := occ[3].Occurrence.Format(time.RFC3339)
	var tail Event
	bob.mustJSON("PATCH", "/api/events/"+ev.ID+"?scope=following&occurrence="+url.QueryEscape(fourth),
		map[string]any{"title": "Later nights"}, 200, &tail, "If-Match", ver(v))
	if tail.ID == ev.ID {
		t.Fatal("following should create a new series")
	}
	occ = bob.list("2026-10-20T00:00:00Z", "2026-12-31T00:00:00Z")
	if len(occ) != 6 {
		t.Fatalf("split changed occurrence count: %d", len(occ))
	}
	for i, o := range occ {
		wantTitle := "Game night"
		switch {
		case i == 2:
			wantTitle = "Special"
		case i >= 3:
			wantTitle = "Later nights"
		}
		if o.Title != wantTitle {
			t.Errorf("occurrence %d title %q want %q", i, o.Title, wantTitle)
		}
	}

	// Delete this occurrence (EXDATE) and then following from the 2nd.
	var master Event
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &master)
	firstOcc := occ[0].Occurrence.Format(time.RFC3339)
	if st := bob.status("DELETE", "/api/events/"+ev.ID+"?scope=this&occurrence="+url.QueryEscape(firstOcc), nil, "If-Match", ver(master.Version)); st != 204 {
		t.Fatalf("delete this: %d", st)
	}
	occ = bob.list("2026-10-20T00:00:00Z", "2026-12-31T00:00:00Z")
	if len(occ) != 5 {
		t.Fatalf("after delete this: %d", len(occ))
	}
	if st := bob.status("DELETE", "/api/events/"+ev.ID+"?scope=following&occurrence="+url.QueryEscape(occ[0].Occurrence.Format(time.RFC3339)), nil); st != 204 {
		t.Fatalf("delete following: %d", st)
	}
	occ = bob.list("2026-10-20T00:00:00Z", "2026-12-31T00:00:00Z")
	if len(occ) != 3 { // only the split-off tail remains
		t.Fatalf("after delete following: %d", len(occ))
	}
}

func TestHistory(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob, carol := e.login(t, "bob"), e.login(t, "carol")
	ev := bob.createEvent(map[string]any{"title": "a"})
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "b"}, 200, nil, "If-Match", "1")
	var h struct {
		Items []struct {
			Action string          `json:"action"`
			Before json.RawMessage `json:"before"`
		} `json:"items"`
	}
	carol.mustJSON("GET", "/api/events/"+ev.ID+"/history", nil, 200, &h)
	if len(h.Items) != 2 || h.Items[0].Action != "event.created" || h.Items[1].Action != "event.updated" {
		t.Fatalf("history: %+v", h.Items)
	}
	// Activity: viewers see only their own; admins everything.
	var act struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	carol.mustJSON("GET", "/api/activity", nil, 200, &act)
	if len(act.Items) != 0 {
		t.Errorf("viewer sees others' activity: %+v", act.Items)
	}
	alice := e.login(t, "alice")
	alice.mustJSON("GET", "/api/activity", nil, 200, &act)
	if len(act.Items) < 2 {
		t.Errorf("admin activity: %+v", act.Items)
	}
}
