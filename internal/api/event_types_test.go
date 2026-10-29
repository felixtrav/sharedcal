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

	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/store"
)

type typeDTO struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	Icon          string           `json:"icon"`
	ColorSlot     string           `json:"colorSlot"`
	DefaultAllDay bool             `json:"defaultAllDay"`
	DefaultRRule  string           `json:"defaultRrule"`
	Fields        []map[string]any `json:"fields"`
}

func birthdayType() map[string]any {
	return map[string]any{
		"name": "Birthday", "icon": "cake", "colorSlot": "tag-3", "defaultAllDay": true, "singleDay": true, "defaultRrule": "FREQ=YEARLY",
		"fields": []map[string]any{
			{"key": "person", "label": "Person", "kind": "text", "required": true},
			{"key": "born", "label": "Year born", "kind": "number"},
			{"key": "relation", "label": "Relation", "kind": "select", "options": []string{"Family", "Friend"}},
			{"key": "gift_list", "label": "Gift list", "kind": "url"},
			{"key": "party", "label": "Party planned", "kind": "checkbox"},
		},
	}
}

func TestEventTypeDefinitions(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	var bday typeDTO
	alice.mustJSON("POST", "/api/event-types", birthdayType(), 201, &bday)
	if bday.Icon != "cake" || !bday.DefaultAllDay || len(bday.Fields) != 5 {
		t.Fatalf("created: %+v", bday)
	}
	var holiday typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Holiday", "colorSlot": "tag-5"}, 201, &holiday)
	if holiday.Icon != "calendar" || len(holiday.Fields) != 0 {
		t.Fatalf("defaults: %+v", holiday)
	}

	bad := map[string]map[string]any{
		"duplicate name": {"name": "birthday", "colorSlot": "tag-1"},
		"hex color":      {"name": "X", "colorSlot": "#fff"},
		"unknown icon":   {"name": "X", "colorSlot": "tag-1", "icon": "skull"},
		"bad rrule":      {"name": "X", "colorSlot": "tag-1", "defaultRrule": "nonsense"},
		"duplicate key":  {"name": "X", "colorSlot": "tag-1", "fields": []map[string]any{{"key": "a", "label": "A", "kind": "text"}, {"key": "a", "label": "B", "kind": "text"}}},
		"bad key":        {"name": "X", "colorSlot": "tag-1", "fields": []map[string]any{{"key": "Bad Key", "label": "A", "kind": "text"}}},
		"select no opts": {"name": "X", "colorSlot": "tag-1", "fields": []map[string]any{{"key": "a", "label": "A", "kind": "select"}}},
		"required check": {"name": "X", "colorSlot": "tag-1", "fields": []map[string]any{{"key": "a", "label": "A", "kind": "checkbox", "required": true}}},
		"unknown kind":   {"name": "X", "colorSlot": "tag-1", "fields": []map[string]any{{"key": "a", "label": "A", "kind": "blob"}}},
		"empty label":    {"name": "X", "colorSlot": "tag-1", "fields": []map[string]any{{"key": "a", "label": "  ", "kind": "text"}}},
		"missing color":  {"name": "X"},
	}
	for name, body := range bad {
		if st := alice.status("POST", "/api/event-types", body); st != 422 {
			t.Errorf("%s: %d", name, st)
		}
	}

	// Everyone can read; only admins change.
	var list struct {
		EventTypes []typeDTO `json:"eventTypes"`
	}
	bob.mustJSON("GET", "/api/event-types", nil, 200, &list)
	if len(list.EventTypes) != 2 || list.EventTypes[0].ID != bday.ID {
		t.Fatalf("list: %+v", list.EventTypes)
	}
	if st := bob.status("POST", "/api/event-types", birthdayType()); st != 403 {
		t.Errorf("writer create: %d", st)
	}

	alice.mustJSON("PATCH", "/api/event-types/"+holiday.ID, map[string]any{"name": "Public holiday", "icon": "flag"}, 200, &holiday)
	if holiday.Name != "Public holiday" || holiday.Icon != "flag" || holiday.ColorSlot != "tag-5" {
		t.Errorf("patch: %+v", holiday)
	}
	alice.mustJSON("POST", "/api/event-types/reorder", map[string]any{"ids": []string{holiday.ID, bday.ID}}, 200, &list)
	if list.EventTypes[0].ID != holiday.ID {
		t.Errorf("reorder: %+v", list.EventTypes)
	}
	if st := alice.status("POST", "/api/event-types/reorder", map[string]any{"ids": []string{bday.ID}}); st != 422 {
		t.Errorf("partial reorder: %d", st)
	}

	// A type in use cannot be deleted; an unused one can.
	bob.createEvent(map[string]any{"title": "Sam", "eventTypeId": holiday.ID})
	if st := alice.status("DELETE", "/api/event-types/"+holiday.ID, nil); st != 409 {
		t.Errorf("delete in use: %d", st)
	}
	if st := alice.status("DELETE", "/api/event-types/"+bday.ID, nil); st != 204 {
		t.Errorf("delete unused: %d", st)
	}
}

func TestTypedEvents(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var bday, plain typeDTO
	alice.mustJSON("POST", "/api/event-types", birthdayType(), 201, &bday)
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Other", "colorSlot": "tag-2", "fields": []map[string]any{{"key": "note", "label": "Note", "kind": "text"}}}, 201, &plain)

	allDay := map[string]any{"allDay": true, "start": "2026-11-05", "end": "2026-11-06"}
	create := func(fields map[string]any) (int, Event) {
		body := map[string]any{"title": "Sam", "eventTypeId": bday.ID, "fields": fields, "timeZone": "UTC", "rrule": "FREQ=YEARLY"}
		for k, v := range allDay {
			body[k] = v
		}
		var ev Event
		resp := bob.do("POST", "/api/events", body)
		defer resp.Body.Close()
		if resp.StatusCode == 201 {
			_ = json.NewDecoder(resp.Body).Decode(&ev)
		}
		return resp.StatusCode, ev
	}

	// Required field, unknown field, wrong kinds, bad select, bad URL.
	for name, f := range map[string]map[string]any{
		"missing required": {},
		"unknown field":    {"person": "Sam", "nope": 1},
		"text as number":   {"person": "Sam", "born": "1990"},
		"bad option":       {"person": "Sam", "relation": "Enemy"},
		"bad url":          {"person": "Sam", "gift_list": "javascript:alert(1)"},
		"bad checkbox":     {"person": "Sam", "party": "yes"},
	} {
		if st, _ := create(f); st != 422 {
			t.Errorf("%s: %d", name, st)
		}
	}
	// Description is refused on typed events, and fields on generic ones.
	if st := bob.status("POST", "/api/events", map[string]any{"title": "x", "eventTypeId": bday.ID, "fields": map[string]any{"person": "S"}, "description": "free text",
		"allDay": true, "start": "2026-11-05", "end": "2026-11-06", "timeZone": "UTC"}); st != 422 {
		t.Errorf("description on typed: %d", st)
	}
	if st := bob.status("POST", "/api/events", map[string]any{"title": "x", "fields": map[string]any{"person": "S"},
		"start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00", "timeZone": "UTC"}); st != 422 {
		t.Errorf("fields on generic: %d", st)
	}
	if st := bob.status("POST", "/api/events", map[string]any{"title": "x", "eventTypeId": "nope",
		"start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00", "timeZone": "UTC"}); st != 422 {
		t.Errorf("unknown type: %d", st)
	}

	st, ev := create(map[string]any{"person": " Sam ", "born": 1990, "relation": "Friend", "party": false, "gift_list": ""})
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	// Normalized: trimmed, empty/false values dropped.
	if ev.EventTypeID == nil || *ev.EventTypeID != bday.ID || len(ev.Fields) != 3 || ev.Fields["person"] != "Sam" || ev.Fields["born"] != float64(1990) {
		t.Fatalf("stored: %+v %+v", ev.EventTypeID, ev.Fields)
	}

	// A plain edit leaves fields alone; replacing fields revalidates them.
	var got Event
	patch := func(body map[string]any, ifMatch string) {
		t.Helper()
		got = Event{}
		bob.mustJSON("PATCH", "/api/events/"+ev.ID, body, 200, &got, "If-Match", ifMatch)
	}
	patch(map[string]any{"title": "Sam's birthday"}, "1")
	if got.Fields["person"] != "Sam" {
		t.Errorf("fields lost on title edit: %+v", got.Fields)
	}
	if st := bob.status("PATCH", "/api/events/"+ev.ID, map[string]any{"fields": map[string]any{"born": 1991}}, "If-Match", "2"); st != 422 {
		t.Errorf("fields without required: %d", st)
	}
	patch(map[string]any{"fields": map[string]any{"person": "Samantha"}}, "2")
	if got.Fields["person"] != "Samantha" || len(got.Fields) != 1 {
		t.Errorf("replace fields: %+v", got.Fields)
	}

	// Switching type drops the old values; going generic clears the type.
	patch(map[string]any{"eventTypeId": plain.ID}, "3")
	if got.EventTypeID == nil || *got.EventTypeID != plain.ID || len(got.Fields) != 0 {
		t.Errorf("switch type: %+v %+v", got.EventTypeID, got.Fields)
	}
	patch(map[string]any{"eventTypeId": "", "description": "free again"}, "4")
	if got.EventTypeID != nil || got.Description != "free again" {
		t.Errorf("generic again: %+v %q", got.EventTypeID, got.Description)
	}
	// Becoming typed clears the free-form description.
	patch(map[string]any{"eventTypeId": plain.ID, "fields": map[string]any{"note": "hi"}}, "5")
	if got.Description != "" || got.Fields["note"] != "hi" {
		t.Errorf("typed again: %q %+v", got.Description, got.Fields)
	}

	// Filtering and searching.
	var out struct {
		Events []Event `json:"events"`
	}
	q := "from=2026-11-01T00:00:00Z&to=2026-12-01T00:00:00Z"
	alice.mustJSON("GET", "/api/events?"+q+"&type="+plain.ID, nil, 200, &out)
	if len(out.Events) != 1 {
		t.Errorf("type filter: %d", len(out.Events))
	}
	alice.mustJSON("GET", "/api/events?"+q+"&type="+bday.ID, nil, 200, &out)
	if len(out.Events) != 0 {
		t.Errorf("type filter other: %d", len(out.Events))
	}
	alice.mustJSON("GET", "/api/events?"+q+"&q=hi", nil, 200, &out)
	if len(out.Events) != 1 {
		t.Errorf("search in fields: %d", len(out.Events))
	}

	// A single occurrence of a typed series can't take a description (see TestOccurrenceTypes for type and fields).
	rec := bob.createEvent(map[string]any{"title": "Weekly", "eventTypeId": plain.ID, "rrule": "FREQ=WEEKLY"})
	occ := "2026-11-09T10:00:00Z"
	if st := bob.status("PATCH", "/api/events/"+rec.ID+"?scope=this&occurrence="+occ, map[string]any{"description": "x"}, "If-Match", "1"); st != 422 {
		t.Errorf("occurrence description on typed: %d", st)
	}
	bob.mustJSON("PATCH", "/api/events/"+rec.ID+"?scope=this&occurrence="+occ, map[string]any{"title": "Moved"}, 200, nil, "If-Match", "1")

	// Series split keeps the type and fields.
	bob.mustJSON("PATCH", "/api/events/"+rec.ID+"?scope=following&occurrence=2026-11-16T10:00:00Z", map[string]any{"title": "Later"}, 200, &got, "If-Match", "2")
	if got.EventTypeID == nil || *got.EventTypeID != plain.ID {
		t.Errorf("split lost type: %+v", got.EventTypeID)
	}
}

func TestTypedEventSuggestionsAndFeed(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "dave")
	var bday typeDTO
	alice.mustJSON("POST", "/api/event-types", birthdayType(), 201, &bday)
	ev := bob.createEvent(map[string]any{
		"title": "Sam", "eventTypeId": bday.ID, "allDay": true, "start": "2099-01-01", "end": "2099-01-02", "rrule": "FREQ=YEARLY",
		"fields": map[string]any{"person": "Sam", "relation": "Friend"},
	})

	var sg struct {
		ID      string `json:"id"`
		Changes map[string]struct {
			Proposed any `json:"proposed"`
		} `json:"changes"`
	}
	dave.mustJSON("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1,
		"changes": map[string]any{"fields": map[string]any{"person": "Sam", "relation": "Family"}}}, 201, &sg)
	if _, ok := sg.Changes["fields"]; !ok || len(sg.Changes) != 1 {
		t.Fatalf("changes: %+v", sg.Changes)
	}
	// Invalid proposals are rejected up front.
	if st := dave.status("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1,
		"changes": map[string]any{"fields": map[string]any{"relation": "Family"}}}); st != 422 {
		t.Errorf("suggest missing required: %d", st)
	}
	var res struct {
		Event Event `json:"event"`
	}
	bob.mustJSON("POST", "/api/suggestions/"+sg.ID+"/accept", map[string]any{}, 200, &res)
	if res.Event.Fields["relation"] != "Family" {
		t.Errorf("accepted: %+v", res.Event.Fields)
	}

	// The ICS feed shows field values as the description and the type as a category.
	var feed struct {
		URL string `json:"url"`
	}
	bob.mustJSON("POST", "/api/me/feed-token", nil, 200, &feed)
	comp := feedEvent(t, feed.URL, ev.ID)
	if comp == nil {
		t.Fatal("event missing from feed")
	}
	desc, _ := comp.Props.Text("DESCRIPTION")
	cats, _ := comp.Props.Text("CATEGORIES")
	if !strings.Contains(desc, "Person: Sam") || !strings.Contains(desc, "Relation: Family") || cats != "Birthday" {
		t.Errorf("feed: desc=%q cats=%q", desc, cats)
	}
}

func TestMembersField(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol")
	var party typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{
		"name": "Party", "colorSlot": "tag-4",
		"fields": []map[string]any{{"key": "guests", "label": "Guests", "kind": "members"}},
	}, 201, &party)
	if st := alice.status("POST", "/api/event-types", map[string]any{"name": "Bad", "colorSlot": "tag-1",
		"fields": []map[string]any{{"key": "g", "label": "G", "kind": "members", "required": true, "options": []string{"x"}}}}); st != 201 {
		t.Errorf("required members with stray options is cleaned up, got %d", st)
	}

	create := func(guests any) int {
		return bob.status("POST", "/api/events", map[string]any{
			"title": "Party", "eventTypeId": party.ID, "timeZone": "UTC", "start": "2099-01-01T10:00:00", "end": "2099-01-01T11:00:00",
			"fields": map[string]any{"guests": guests},
		})
	}
	for name, g := range map[string]any{
		"not a list":      "carol",
		"unknown user":    []string{"nobody"},
		"non-string item": []any{1},
	} {
		if st := create(g); st != 422 {
			t.Errorf("%s: %d", name, st)
		}
	}

	var ev Event
	bob.mustJSON("POST", "/api/events", map[string]any{
		"title": "Party", "eventTypeId": party.ID, "timeZone": "UTC", "start": "2099-01-01T10:00:00", "end": "2099-01-01T11:00:00",
		"fields": map[string]any{"guests": []string{carol.ID, alice.ID, carol.ID}},
	}, 201, &ev)
	got, _ := ev.Fields["guests"].([]any)
	if len(got) != 2 || got[0] != carol.ID || got[1] != alice.ID {
		t.Fatalf("guests: %v", ev.Fields["guests"])
	}
	// An empty list clears the field.
	var cleared Event
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"fields": map[string]any{"guests": []string{}}}, 200, &cleared, "If-Match", "1")
	if len(cleared.Fields) != 0 {
		t.Errorf("cleared: %v", cleared.Fields)
	}

	// The feed names the guests.
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"fields": map[string]any{"guests": []string{carol.ID}}}, 200, &cleared, "If-Match", "2")
	var feed struct {
		URL string `json:"url"`
	}
	bob.mustJSON("POST", "/api/me/feed-token", nil, 200, &feed)
	comp := feedEvent(t, feed.URL, ev.ID)
	if comp == nil {
		t.Fatal("event missing from feed")
	}
	if desc, _ := comp.Props.Text("DESCRIPTION"); !strings.HasPrefix(desc, "Guests: ") || strings.Contains(desc, carol.ID) {
		t.Errorf("feed description: %q", desc)
	}
}

func TestTypeStylesAndYearCounter(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	var def typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Plain", "colorSlot": "tag-1"}, 201, &def)
	var instant struct {
		DefaultDurationMinutes int `json:"defaultDurationMinutes"`
	}
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Instant", "colorSlot": "tag-2", "defaultDurationMinutes": 0}, 201, &instant)
	if instant.DefaultDurationMinutes != 0 {
		t.Errorf("explicit zero duration on create became %d", instant.DefaultDurationMinutes)
	}
	var list struct {
		EventTypes []struct {
			Style                  string `json:"style"`
			YearCounter            bool   `json:"yearCounter"`
			DefaultDurationMinutes int    `json:"defaultDurationMinutes"`
		} `json:"eventTypes"`
	}
	alice.mustJSON("GET", "/api/event-types", nil, 200, &list)
	if got := list.EventTypes[0]; got.Style != "default" || got.YearCounter || got.DefaultDurationMinutes != 60 {
		t.Errorf("defaults: %+v", got)
	}

	for name, body := range map[string]map[string]any{
		"bad style": {"style": "neon"},
		"negative":  {"defaultDurationMinutes": -1},
		"too long":  {"defaultDurationMinutes": 1441},
	} {
		if st := alice.status("PATCH", "/api/event-types/"+def.ID, body); st != 422 {
			t.Errorf("%s: %d", name, st)
		}
	}
	alice.mustJSON("PATCH", "/api/event-types/"+def.ID, map[string]any{"name": "Birthday", "style": "banner", "yearCounter": true, "defaultDurationMinutes": 0}, 200, &def)
	alice.mustJSON("GET", "/api/event-types", nil, 200, &list)
	if got := list.EventTypes[0]; got.Style != "banner" || !got.YearCounter || got.DefaultDurationMinutes != 0 {
		t.Errorf("updated: %+v", got)
	}

	// A yearly series that started in 1990 counts years from its first occurrence.
	ev := bob.createEvent(map[string]any{"title": "Dana", "eventTypeId": def.ID, "allDay": true, "start": "1990-05-12", "end": "1990-05-13", "rrule": "FREQ=YEARLY"})
	type counted struct {
		Event
		YearsSince *int `json:"yearsSince"`
	}
	var out struct {
		Events []counted `json:"events"`
	}
	out.Events = nil
	bob.mustJSON("GET", "/api/events?from=1990-05-01T00:00:00Z&to=1990-06-01T00:00:00Z", nil, 200, &out)
	if len(out.Events) != 1 || out.Events[0].YearsSince != nil {
		t.Fatalf("first year: %+v", out.Events)
	}
	out.Events = nil
	bob.mustJSON("GET", "/api/events?from=1991-05-01T00:00:00Z&to=1991-06-01T00:00:00Z", nil, 200, &out)
	if len(out.Events) != 1 || out.Events[0].YearsSince == nil || *out.Events[0].YearsSince != 1 {
		t.Fatalf("second year: %+v", out.Events)
	}
	out.Events = nil
	bob.mustJSON("GET", "/api/events?from=2026-05-01T00:00:00Z&to=2026-06-01T00:00:00Z", nil, 200, &out)
	if len(out.Events) != 1 || out.Events[0].YearsSince == nil || *out.Events[0].YearsSince != 36 {
		t.Fatalf("2026: %+v", out.Events)
	}
	// Without the counter (or without a type) nothing is reported.
	alice.mustJSON("PATCH", "/api/event-types/"+def.ID, map[string]any{"yearCounter": false}, 200, nil)
	out.Events = nil
	bob.mustJSON("GET", "/api/events?from=2026-05-01T00:00:00Z&to=2026-06-01T00:00:00Z", nil, 200, &out)
	if out.Events[0].YearsSince != nil {
		t.Errorf("counter off: %v", *out.Events[0].YearsSince)
	}
	_ = ev
}

func TestOccurrenceTypes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "dave")
	var movie, game typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Movie release", "colorSlot": "tag-2", "icon": "star", "fields": []map[string]any{
		{"key": "film", "label": "Film", "kind": "text", "required": true}, {"key": "studio", "label": "Studio", "kind": "text"}}}, 201, &movie)
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Game release", "colorSlot": "tag-5", "fields": []map[string]any{
		{"key": "game", "label": "Game", "kind": "text", "required": true}}}, 201, &game)

	// A weekly "Releases" series; individual weeks become particular releases.
	ev := bob.createEvent(map[string]any{"title": "Releases", "description": "Weekly roundup", "rrule": "FREQ=WEEKLY;COUNT=4",
		"allDay": true, "start": "2026-11-06", "end": "2026-11-07"})
	rid := func(day string) string { return "?scope=this&occurrence=" + day + "T00:00:00Z" }
	patch := func(day string, body map[string]any, version string, want int) {
		t.Helper()
		if st := bob.status("PATCH", "/api/events/"+ev.ID+rid(day), body, "If-Match", version); st != want {
			t.Fatalf("PATCH %s %v: %d, want %d", day, body, st, want)
		}
	}
	// Validation uses the occurrence's new type.
	patch("2026-11-13", map[string]any{"eventTypeId": "nope"}, "1", 422)
	patch("2026-11-13", map[string]any{"eventTypeId": movie.ID}, "1", 422) // film is required
	patch("2026-11-13", map[string]any{"eventTypeId": movie.ID, "fields": map[string]any{"film": "Dune", "bogus": 1}}, "1", 422)
	patch("2026-11-13", map[string]any{"eventTypeId": movie.ID, "fields": map[string]any{"film": "Dune"}, "description": "text"}, "1", 422)
	patch("2026-11-13", map[string]any{"eventTypeId": movie.ID, "fields": map[string]any{"film": "Dune", "studio": "WB"}}, "1", 200)
	patch("2026-11-20", map[string]any{"eventTypeId": game.ID, "fields": map[string]any{"game": "Halo"}}, "2", 200)

	type occ struct {
		Event
		Occurrence time.Time `json:"occurrence"`
	}
	var out struct {
		Events []occ `json:"events"`
	}
	list := func(q string) []occ {
		t.Helper()
		out.Events = nil
		bob.mustJSON("GET", "/api/events?from=2026-11-01T00:00:00Z&to=2026-12-05T00:00:00Z"+q, nil, 200, &out)
		return out.Events
	}
	got := list("")
	if len(got) != 4 {
		t.Fatalf("occurrences: %d", len(got))
	}
	kind := func(o occ) string {
		if o.EventTypeID == nil {
			return "generic"
		}
		return map[string]string{movie.ID: "movie", game.ID: "game"}[*o.EventTypeID]
	}
	want := []string{"generic", "movie", "game", "generic"}
	for i, o := range got {
		if kind(o) != want[i] {
			t.Errorf("occurrence %d is %s, want %s", i, kind(o), want[i])
		}
	}
	if got[0].Description != "Weekly roundup" || got[1].Description != "" || got[1].Fields["film"] != "Dune" || got[3].Description != "Weekly roundup" {
		t.Errorf("descriptions/fields: %q %q %v %q", got[0].Description, got[1].Description, got[1].Fields, got[3].Description)
	}

	// Filters and search see occurrence types.
	if r := list("&type=" + movie.ID); len(r) != 1 || r[0].Fields["film"] != "Dune" {
		t.Errorf("type filter: %+v", r)
	}
	if r := list("&q=halo"); len(r) != 1 || kind(r[0]) != "game" {
		t.Errorf("search: %+v", r)
	}

	// Editing only the fields keeps the occurrence's type; the master and the others are untouched.
	patch("2026-11-13", map[string]any{"fields": map[string]any{"film": "Dune: Part Two"}}, "3", 200)
	got = list("")
	if kind(got[1]) != "movie" || got[1].Fields["film"] != "Dune: Part Two" || len(got[1].Fields) != 1 {
		t.Errorf("fields edit: %+v", got[1].Fields)
	}
	var detail struct {
		EventTypeID *string `json:"eventTypeId"`
		Overrides   []struct {
			TypeOverridden bool           `json:"typeOverridden"`
			EventTypeID    *string        `json:"eventTypeId"`
			Fields         map[string]any `json:"fields"`
		} `json:"overrides"`
	}
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &detail)
	if detail.EventTypeID != nil || len(detail.Overrides) != 2 || !detail.Overrides[0].TypeOverridden || *detail.Overrides[0].EventTypeID != movie.ID {
		t.Errorf("detail: %+v", detail)
	}

	// The type can't be deleted while an occurrence uses it.
	if st := alice.status("DELETE", "/api/event-types/"+movie.ID, nil); st != 409 {
		t.Errorf("delete type used by an occurrence: %d", st)
	}
	// A non-owner can't change an occurrence's type; the feed shows its fields.
	if st := dave.status("PATCH", "/api/events/"+ev.ID+rid("2026-11-27"), map[string]any{"eventTypeId": game.ID, "fields": map[string]any{"game": "x"}}, "If-Match", "4"); st != 403 {
		t.Errorf("non-owner: %d", st)
	}
	var feed struct {
		URL string `json:"url"`
	}
	bob.mustJSON("POST", "/api/me/feed-token", nil, 200, &feed)
	resp, err := http.Get(feed.URL)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), "Film: Dune: Part Two") || !strings.Contains(string(raw), "Game: Halo") || !strings.Contains(string(raw), "Weekly roundup") {
		t.Errorf("feed descriptions missing:\n%s", raw)
	}

	// Back to generic: the occurrence inherits the master's description again.
	patch("2026-11-13", map[string]any{"eventTypeId": ""}, "4", 200)
	got = list("")
	if kind(got[1]) != "generic" || got[1].Description != "Weekly roundup" || len(got[1].Fields) != 0 {
		t.Errorf("generic again: %q %v", got[1].Description, got[1].Fields)
	}
	// A typed master can have a generic occurrence.
	typed := bob.createEvent(map[string]any{"title": "Premieres", "eventTypeId": game.ID, "fields": map[string]any{"game": "Halo"}, "rrule": "FREQ=WEEKLY;COUNT=2",
		"allDay": true, "start": "2026-11-06", "end": "2026-11-07"})
	if st := bob.status("PATCH", "/api/events/"+typed.ID+rid("2026-11-13"), map[string]any{"eventTypeId": "", "description": "TBA"}, "If-Match", "1"); st != 200 {
		t.Fatalf("typed master, generic occurrence: %d", st)
	}
	r := list("&type=" + game.ID)
	if len(r) != 2 || r[0].ID != typed.ID && r[1].ID != typed.ID {
		t.Errorf("game filter (halo week, typed master first week): %d", len(r))
	}
}

func TestTitleTemplates(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	// Placeholders must name text, number or choice fields of the type.
	bad := birthdayType()
	bad["titleTemplate"] = "{{gift_list}}'s birthday"
	if st := alice.status("POST", "/api/event-types", bad); st != 422 && st != 400 {
		t.Errorf("template over a url field: %d", st)
	}
	bad["titleTemplate"] = "Birthday"
	if st := alice.status("POST", "/api/event-types", bad); st != 422 && st != 400 {
		t.Errorf("template without placeholders: %d", st)
	}

	// Generated titles need something to generate from, so a required field is a must.
	noReq := map[string]any{"name": "Loose", "colorSlot": "tag-2", "titleTemplate": "{{note}}", "fields": []map[string]any{{"key": "note", "label": "Note", "kind": "text"}}}
	if st := alice.status("POST", "/api/event-types", noReq); st != 422 && st != 400 {
		t.Errorf("template without a required field: %d", st)
	}

	typ := birthdayType()
	typ["titleTemplate"] = "{{person}}'s Birthday"
	var bday typeDTO
	alice.mustJSON("POST", "/api/event-types", typ, 201, &bday)

	var ev Event
	bob.mustJSON("POST", "/api/events", map[string]any{
		"title": "Typed by hand", "eventTypeId": bday.ID, "fields": map[string]any{"person": "Sam"},
		"timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": "2026-11-06", "rrule": "FREQ=YEARLY",
	}, 201, &ev)
	if ev.Title != "Sam's Birthday" {
		t.Errorf("created title %q", ev.Title)
	}

	// A title in an update is ignored; changing the field regenerates it.
	var got Event
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "Nope"}, 200, &got, "If-Match", strconv.FormatInt(ev.Version, 10))
	if got.Title != "Sam's Birthday" {
		t.Errorf("hand-set title not ignored: %q", got.Title)
	}
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"fields": map[string]any{"person": "Samantha"}}, 200, &got, "If-Match", strconv.FormatInt(got.Version, 10))
	if got.Title != "Samantha's Birthday" {
		t.Errorf("regenerated title %q", got.Title)
	}

	// Changing the template retitles existing events.
	alice.mustJSON("PATCH", "/api/event-types/"+bday.ID, map[string]any{"titleTemplate": "Happy birthday {{person}}!"}, 200, nil)
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if got.Title != "Happy birthday Samantha!" {
		t.Errorf("retitled %q", got.Title)
	}

	// A field the template uses cannot be removed.
	if st := alice.status("PATCH", "/api/event-types/"+bday.ID, map[string]any{"fields": []map[string]any{{"key": "born", "label": "Born", "kind": "number"}}}); st != 422 && st != 400 {
		t.Errorf("removing a templated field: %d", st)
	}
	// Nor can the last required field be made optional while a template is set.
	if st := alice.status("PATCH", "/api/event-types/"+bday.ID, map[string]any{"fields": []map[string]any{{"key": "person", "label": "Person", "kind": "text"}}}); st != 422 && st != 400 {
		t.Errorf("no required field left: %d", st)
	}
}

func TestTitleTemplateWithPeople(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var typ typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{
		"name": "Party", "colorSlot": "tag-4", "titleTemplate": "Party for {{guests}}",
		"fields": []map[string]any{{"key": "guests", "label": "Guests", "kind": "members", "required": true}},
	}, 201, &typ)

	var me struct {
		User struct{ ID string } `json:"user"`
	}
	alice.mustJSON("GET", "/api/me", nil, 200, &me)
	var ev Event
	bob.mustJSON("POST", "/api/events", map[string]any{
		"eventTypeId": typ.ID, "fields": map[string]any{"guests": []string{me.User.ID}},
		"timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": "2026-11-06",
	}, 201, &ev)
	if !strings.HasPrefix(ev.Title, "Party for ") || ev.Title == "Party for " {
		t.Fatalf("title %q", ev.Title)
	}
	oldName := strings.TrimPrefix(ev.Title, "Party for ")

	// A new display name from the identity provider follows into the stored title.
	var u store.User
	if err := e.db.R.Where("id = ?", me.User.ID).Take(&u).Error; err != nil {
		t.Fatal(err)
	}
	var groups []string
	_ = json.Unmarshal([]byte(u.GroupsJSON), &groups)
	if _, err := e.svc.Provision(context.Background(), auth.Identity{Issuer: u.OIDCIssuer, Subject: u.OIDCSubject, Email: u.Email, EmailVerified: true, Name: oldName + " Renamed", Groups: groups}); err != nil {
		t.Fatal(err)
	}
	var got Event
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if got.Title != "Party for "+oldName+" Renamed" {
		t.Errorf("title after rename %q", got.Title)
	}
}

func TestFixedAllDayAndRepeatAreEnforced(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var bday, holiday typeDTO
	alice.mustJSON("POST", "/api/event-types", birthdayType(), 201, &bday) // always all-day, always yearly
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Holiday", "colorSlot": "tag-5", "defaultAllDay": true}, 201, &holiday)

	body := func(typeID string, over map[string]any) map[string]any {
		m := map[string]any{"title": "x", "eventTypeId": typeID, "timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": "2026-11-06", "rrule": "FREQ=YEARLY", "fields": map[string]any{"person": "Sam"}}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	for name, c := range map[string]struct {
		typ  string
		over map[string]any
	}{
		"timed":         {bday.ID, map[string]any{"allDay": false, "start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00"}},
		"monthly":       {bday.ID, map[string]any{"rrule": "FREQ=MONTHLY"}},
		"no repeat":     {bday.ID, map[string]any{"rrule": ""}},
		"three days":    {bday.ID, map[string]any{"end": "2026-11-08"}},
		"holiday timed": {holiday.ID, map[string]any{"allDay": false, "rrule": "", "start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00"}},
	} {
		if st := bob.status("POST", "/api/events", body(c.typ, c.over)); st != 422 {
			t.Errorf("%s: %d, want 422", name, st)
		}
	}

	// A fixed all-day type without a fixed repeat still allows several days and any repeat.
	bob.mustJSON("POST", "/api/events", body(holiday.ID, map[string]any{"end": "2026-11-08", "rrule": "FREQ=MONTHLY", "fields": map[string]any{}}), 201, nil)

	var ev Event
	bob.mustJSON("POST", "/api/events", body(bday.ID, nil), 201, &ev)
	for name, patch := range map[string]map[string]any{
		"edit to timed":  {"allDay": false, "start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00"},
		"edit repeat":    {"rrule": "FREQ=WEEKLY"},
		"stretch to two": {"end": "2026-11-07"},
	} {
		if st := bob.status("PATCH", "/api/events/"+ev.ID, patch, "If-Match", "1"); st != 422 {
			t.Errorf("%s: %d, want 422", name, st)
		}
	}
	// Unrelated edits and moving to another date still work.
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"location": "Home"}, 200, nil, "If-Match", "1")
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"start": "2026-11-09", "end": "2026-11-10"}, 200, nil, "If-Match", "2")
}

func TestYearCounterRecountsFromTheNewStartYear(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	typ := birthdayType()
	typ["yearCounter"] = true
	var bday typeDTO
	alice.mustJSON("POST", "/api/event-types", typ, 201, &bday)
	ev := bob.createEvent(map[string]any{
		"title": "Dana", "eventTypeId": bday.ID, "allDay": true, "start": "1990-05-12", "end": "1990-05-13", "rrule": "FREQ=YEARLY",
		"fields": map[string]any{"person": "Dana"},
	})

	years := func(year string) int {
		var out struct {
			Events []struct {
				YearsSince *int `json:"yearsSince"`
			} `json:"events"`
		}
		bob.mustJSON("GET", "/api/events?from="+year+"-05-01T00:00:00Z&to="+year+"-06-01T00:00:00Z", nil, 200, &out)
		if len(out.Events) != 1 || out.Events[0].YearsSince == nil {
			t.Fatalf("%s: %+v", year, out.Events)
		}
		return *out.Events[0].YearsSince
	}
	if got := years("2026"); got != 36 {
		t.Fatalf("before: %d", got)
	}

	// Opening this year's occurrence and moving it to 1995 starts the series in 1995;
	// it must not shift the whole series back by 31 years.
	bob.mustJSON("PATCH", "/api/events/"+ev.ID+"?scope=all&occurrence=2026-05-12T00:00:00Z",
		map[string]any{"start": "1995-05-12", "end": "1995-05-13"}, 200, nil, "If-Match", "1")
	if got := years("2026"); got != 31 {
		t.Errorf("after moving the year to 1995: %d, want 31", got)
	}
	var got Event
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if got.Start != "1995-05-12" {
		t.Errorf("series start %q", got.Start)
	}
}

func TestEventTypeInstructions(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var typ struct {
		ID           string `json:"id"`
		Instructions string `json:"instructions"`
	}
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Guided", "colorSlot": "tag-2", "instructions": "  Add the full name.  "}, 201, &typ)
	if typ.Instructions != "Add the full name." {
		t.Errorf("created: %q", typ.Instructions)
	}
	alice.mustJSON("PATCH", "/api/event-types/"+typ.ID, map[string]any{"instructions": "Call ahead."}, 200, &typ)
	if typ.Instructions != "Call ahead." {
		t.Errorf("updated: %q", typ.Instructions)
	}
	if st := alice.status("PATCH", "/api/event-types/"+typ.ID, map[string]any{"instructions": strings.Repeat("x", 1001)}); st != 422 && st != 400 {
		t.Errorf("too long: %d", st)
	}
	// Writers read them with the type; clearing removes them.
	var list struct {
		EventTypes []struct {
			Instructions string `json:"instructions"`
		} `json:"eventTypes"`
	}
	bob.mustJSON("GET", "/api/event-types", nil, 200, &list)
	if len(list.EventTypes) != 1 || list.EventTypes[0].Instructions != "Call ahead." {
		t.Errorf("listed: %+v", list.EventTypes)
	}
	alice.mustJSON("PATCH", "/api/event-types/"+typ.ID, map[string]any{"instructions": ""}, 200, &typ)
	if typ.Instructions != "" {
		t.Errorf("cleared: %q", typ.Instructions)
	}
}

func TestNeverRepeatsTypes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var rel typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Release", "colorSlot": "tag-6", "neverRepeats": true}, 201, &rel)
	if st := alice.status("POST", "/api/event-types", map[string]any{"name": "Both", "colorSlot": "tag-2", "neverRepeats": true, "defaultRrule": "FREQ=YEARLY"}); st != 422 {
		t.Errorf("never repeats with a fixed repeat: %d", st)
	}

	base := map[string]any{"title": "v2", "eventTypeId": rel.ID, "timeZone": "UTC", "start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00"}
	with := func(over map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range over {
			m[k] = v
		}
		return m
	}
	if st := bob.status("POST", "/api/events", with(map[string]any{"rrule": "FREQ=WEEKLY"})); st != 422 {
		t.Errorf("repeating release: %d", st)
	}
	var ev Event
	bob.mustJSON("POST", "/api/events", base, 201, &ev)
	if st := bob.status("PATCH", "/api/events/"+ev.ID, map[string]any{"rrule": "FREQ=DAILY"}, "If-Match", "1"); st != 422 {
		t.Errorf("making a release repeat: %d", st)
	}
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"location": "Online"}, 200, nil, "If-Match", "1")

	// Switching the rule: a type can't be both, but can move from a fixed repeat to none.
	var yearly typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Yearly", "colorSlot": "tag-3", "defaultRrule": "FREQ=YEARLY"}, 201, &yearly)
	if st := alice.status("PATCH", "/api/event-types/"+yearly.ID, map[string]any{"neverRepeats": true}); st != 422 {
		t.Errorf("adding never to a fixed repeat: %d", st)
	}
	alice.mustJSON("PATCH", "/api/event-types/"+yearly.ID, map[string]any{"neverRepeats": true, "defaultRrule": ""}, 200, nil)
}

func TestSingleDayIsItsOwnSetting(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	// All-day and never repeating does not by itself limit the length.
	var range_, single typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Trip", "colorSlot": "tag-1", "defaultAllDay": true, "neverRepeats": true}, 201, &range_)
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Release", "colorSlot": "tag-2", "defaultAllDay": true, "neverRepeats": true, "singleDay": true}, 201, &single)
	ev := func(typeID, end string) map[string]any {
		return map[string]any{"title": "x", "eventTypeId": typeID, "timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": end}
	}
	bob.mustJSON("POST", "/api/events", ev(range_.ID, "2026-11-09"), 201, nil)
	if st := bob.status("POST", "/api/events", ev(single.ID, "2026-11-09")); st != 422 {
		t.Errorf("range on a single-date type: %d", st)
	}
	bob.mustJSON("POST", "/api/events", ev(single.ID, "2026-11-06"), 201, nil)

	// The setting only applies while the type is always all-day.
	var got struct {
		SingleDay bool `json:"singleDay"`
	}
	alice.mustJSON("PATCH", "/api/event-types/"+single.ID, map[string]any{"defaultAllDay": false}, 200, &got)
	if got.SingleDay {
		t.Error("singleDay survived turning off all-day")
	}
}

func TestConditionalTitleWithATimeField(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var typ typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{
		"name": "Release", "colorSlot": "tag-6", "neverRepeats": true,
		"titleTemplate": "{{game}} Releases{{#at}} @{{at}}{{/at}}",
		"fields": []map[string]any{
			{"key": "game", "label": "Game", "kind": "text", "required": true},
			{"key": "at", "label": "Time", "kind": "time"},
		},
	}, 201, &typ)
	if st := alice.status("POST", "/api/event-types", map[string]any{"name": "Broken", "colorSlot": "tag-2", "titleTemplate": "{{game}}{{#at}} x",
		"fields": []map[string]any{{"key": "game", "label": "Game", "kind": "text", "required": true}, {"key": "at", "label": "Time", "kind": "time"}}}); st != 422 {
		t.Errorf("unbalanced template: %d", st)
	}

	make := func(fields map[string]any) (int, Event) {
		var ev Event
		resp := bob.do("POST", "/api/events", map[string]any{
			"eventTypeId": typ.ID, "fields": fields, "timeZone": "UTC", "start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00",
		})
		defer resp.Body.Close()
		if resp.StatusCode == 201 {
			_ = json.NewDecoder(resp.Body).Decode(&ev)
		}
		return resp.StatusCode, ev
	}
	if st, ev := make(map[string]any{"game": "Game X"}); st != 201 || ev.Title != "Game X Releases" {
		t.Errorf("name only: %d %q", st, ev.Title)
	}
	if st, ev := make(map[string]any{"game": "Game X", "at": "19:30"}); st != 201 || ev.Title != "Game X Releases @19:30" {
		t.Errorf("with a time: %d %q", st, ev.Title)
	}
	if st, _ := make(map[string]any{"game": "Game X", "at": "7:30pm"}); st != 422 {
		t.Errorf("bad time: %d", st)
	}
}

func TestTitleTimesFollowEachSubscribersZoneAndClock(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")
	var typ typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{
		"name": "Release", "colorSlot": "tag-6", "neverRepeats": true,
		"titleTemplate": "{{game}} Releases{{#at}} @{{at}}{{/at}}",
		"fields": []map[string]any{
			{"key": "game", "label": "Game", "kind": "text", "required": true},
			{"key": "at", "label": "Time", "kind": "time"},
		},
	}, 201, &typ)
	var ev Event
	bob.mustJSON("POST", "/api/events", map[string]any{
		"eventTypeId": typ.ID, "fields": map[string]any{"game": "Game X", "at": "19:30"},
		"timeZone": "America/New_York", "start": "2026-11-05T10:00:00", "end": "2026-11-05T11:00:00",
	}, 201, &ev)
	if ev.Title != "Game X Releases @19:30" {
		t.Fatalf("stored title %q", ev.Title)
	}

	summary := func(c *client) string {
		var feed struct {
			URL string `json:"url"`
		}
		c.mustJSON("POST", "/api/me/feed-token", nil, 200, &feed)
		comp := feedEvent(t, feed.URL, ev.ID)
		if comp == nil {
			t.Fatal("event missing from feed")
		}
		s, _ := comp.Props.Text("SUMMARY")
		return s
	}
	// No preferences: the event's own zone, 12-hour clock.
	if got := summary(bob); got != "Game X Releases @7:30 PM" {
		t.Errorf("default: %q", got)
	}
	// Berlin is six hours ahead of New York in early November.
	alice.mustJSON("PUT", "/api/me/preferences", map[string]any{"themeId": "default", "colorMode": "light", "timeZone": "Europe/Berlin", "weekStart": 1, "clock24h": true, "defaultView": "week", "density": "comfortable"}, 200, nil)
	if got := summary(alice); got != "Game X Releases @01:30" {
		t.Errorf("Berlin, 24-hour: %q", got)
	}
}

func TestAdminOnlyTypes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	var closure, plain typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Closure", "colorSlot": "tag-3", "adminOnly": true}, 201, &closure)
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Plain", "colorSlot": "tag-4"}, 201, &plain)
	body := func(typeID string) map[string]any {
		return map[string]any{"title": "x", "eventTypeId": typeID, "timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": "2026-11-06"}
	}

	if st := bob.status("POST", "/api/events", body(closure.ID)); st != 403 {
		t.Errorf("writer creating admin-only: %d, want 403", st)
	}
	alice.mustJSON("POST", "/api/events", body(closure.ID), 201, nil)

	var ev Event
	bob.mustJSON("POST", "/api/events", body(plain.ID), 201, &ev)
	if st := bob.status("PATCH", "/api/events/"+ev.ID, map[string]any{"eventTypeId": closure.ID}, "If-Match", "1"); st != 403 {
		t.Errorf("writer switching to admin-only: %d, want 403", st)
	}
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"location": "Home"}, 200, nil, "If-Match", "1")

	// Opening the type up again lets writers use it.
	alice.mustJSON("PATCH", "/api/event-types/"+closure.ID, map[string]any{"adminOnly": false}, 200, nil)
	bob.mustJSON("POST", "/api/events", body(closure.ID), 201, nil)
}

const holidayICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:xmas@test\r\nDTSTART;VALUE=DATE:20261225\r\nDTEND;VALUE=DATE:20261226\r\nSUMMARY:Christmas Day\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:ny@test\r\nDTSTART;VALUE=DATE:20270101\r\nSUMMARY:New Year\r\nRRULE:FREQ=YEARLY\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:cancelled@test\r\nDTSTART;VALUE=DATE:20261101\r\nSUMMARY:Gone\r\nSTATUS:CANCELLED\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:meet@test\r\nDTSTART:20261110T090000Z\r\nDTEND:20261110T100000Z\r\nSUMMARY:Timed\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

type importResult struct {
	DryRun     bool `json:"dryRun"`
	Created    int  `json:"created"`
	Duplicates int  `json:"duplicates"`
	Invalid    int  `json:"invalid"`
	Ignored    int  `json:"ignored"`
	Entries    []struct {
		UID     string `json:"uid"`
		Outcome string `json:"outcome"`
		Error   string `json:"error"`
	} `json:"entries"`
}

func TestImportICSAndOwnerlessTypes(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	// Holidays are all-day, ownerless and can't be created by writers.
	var holiday typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Holiday", "colorSlot": "tag-5", "defaultAllDay": true, "ownerless": true, "adminOnly": true}, 201, &holiday)
	req := func(typeID string, dry bool) map[string]any {
		return map[string]any{"ics": holidayICS, "eventTypeId": typeID, "dryRun": dry}
	}

	if st := bob.status("POST", "/api/import/ics", req(holiday.ID, false)); st != 403 {
		t.Errorf("writer import: %d, want 403", st)
	}

	// Preview creates nothing.
	var res importResult
	alice.mustJSON("POST", "/api/import/ics", req(holiday.ID, true), 200, &res)
	// The timed event doesn't fit an all-day type.
	if !res.DryRun || res.Created != 2 || res.Ignored != 1 || res.Invalid != 1 {
		t.Fatalf("preview: %+v", res)
	}
	var list struct {
		Events []Event `json:"events"`
	}
	alice.mustJSON("GET", "/api/events?from=2026-06-01T00:00:00Z&to=2027-06-01T00:00:00Z", nil, 200, &list)
	if len(list.Events) != 0 {
		t.Fatalf("preview created %d events", len(list.Events))
	}

	alice.mustJSON("POST", "/api/import/ics", req(holiday.ID, false), 200, &res)
	if res.Created != 2 {
		t.Fatalf("import: %+v", res)
	}
	var got struct {
		Events []Event `json:"events"`
	}
	bob.mustJSON("GET", "/api/events?from=2026-12-01T00:00:00Z&to=2026-12-31T00:00:00Z", nil, 200, &got)
	if len(got.Events) != 1 || got.Events[0].Title != "Christmas Day" || !got.Events[0].AllDay || !got.Events[0].Owner.System {
		t.Fatalf("imported: %+v", got.Events)
	}
	if got.Events[0].Start != "2026-12-25" || got.Events[0].End != "2026-12-26" {
		t.Errorf("dates: %s – %s", got.Events[0].Start, got.Events[0].End)
	}

	// Importing again skips everything.
	alice.mustJSON("POST", "/api/import/ics", req(holiday.ID, false), 200, &res)
	if res.Created != 0 || res.Duplicates != 2 {
		t.Fatalf("re-import: %+v", res)
	}

	// Nobody owns the event, so a writer can only suggest; admins edit.
	id := got.Events[0].ID
	if st := bob.status("PATCH", "/api/events/"+id, map[string]any{"location": "Everywhere"}, "If-Match", "1"); st != 403 {
		t.Errorf("writer edit: %d, want 403", st)
	}
	alice.mustJSON("PATCH", "/api/events/"+id, map[string]any{"location": "Everywhere"}, 200, nil, "If-Match", "1")

	// The calendar's own user stays out of user lists, and can't be managed.
	for _, c := range []*client{alice, bob} {
		var dir struct {
			Users []struct {
				ID string `json:"id"`
			} `json:"users"`
		}
		c.mustJSON("GET", "/api/users/directory", nil, 200, &dir)
		for _, u := range dir.Users {
			if u.ID == store.SystemUserID {
				t.Error("system user in directory")
			}
		}
	}
	var users struct {
		Users []struct {
			ID string `json:"id"`
		} `json:"users"`
	}
	alice.mustJSON("GET", "/api/users", nil, 200, &users)
	for _, u := range users.Users {
		if u.ID == store.SystemUserID {
			t.Error("system user in admin list")
		}
	}
	if st := alice.status("PATCH", "/api/users/"+store.SystemUserID, map[string]any{"active": true}); st != 404 {
		t.Errorf("patch system user: %d, want 404", st)
	}

	// Making a type ownerless later also takes the owner off events it already has.
	var late typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Late", "colorSlot": "tag-2", "defaultAllDay": true}, 201, &late)
	lateBody := map[string]any{"title": "Early", "eventTypeId": late.ID, "timeZone": "UTC", "allDay": true, "start": "2026-07-01", "end": "2026-07-02"}
	var early Event
	bob.mustJSON("POST", "/api/events", lateBody, 201, &early)
	if early.Owner.ID != bob.ID {
		t.Fatalf("owner before: %+v", early.Owner)
	}
	alice.mustJSON("PATCH", "/api/event-types/"+late.ID, map[string]any{"ownerless": true}, 200, nil)
	alice.mustJSON("GET", "/api/events/"+early.ID, nil, 200, &early)
	if !early.Owner.System || early.Version != 2 {
		t.Errorf("after flag: owner %+v, version %d", early.Owner, early.Version)
	}

	// Events created by hand get no owner either, and an owner can't be forced.
	body := map[string]any{"title": "Founders' Day", "eventTypeId": holiday.ID, "timeZone": "UTC", "allDay": true, "start": "2026-06-01", "end": "2026-06-02"}
	var made Event
	alice.mustJSON("POST", "/api/events", body, 201, &made)
	if !made.Owner.System {
		t.Error("manually created holiday has an owner")
	}
	body["ownerId"] = bob.ID
	if st := alice.status("POST", "/api/events", body); st != 422 {
		t.Errorf("owner on ownerless type: %d, want 422", st)
	}

	// Rows that don't fit the type are reported, not fatal.
	var strict typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Yearly", "colorSlot": "tag-1", "defaultAllDay": true, "defaultRrule": "FREQ=YEARLY"}, 201, &strict)
	fresh := req(strict.ID, false)
	fresh["ics"] = strings.ReplaceAll(holidayICS, "@test", "@strict") // new UIDs, so nothing counts as imported already
	alice.mustJSON("POST", "/api/import/ics", fresh, 200, &res)
	if res.Created != 1 || res.Invalid != 2 {
		t.Fatalf("strict type: %+v", res)
	}
	if st := alice.status("POST", "/api/import/ics", map[string]any{"ics": "nonsense"}); st != 422 {
		t.Errorf("garbage: %d, want 422", st)
	}
}
