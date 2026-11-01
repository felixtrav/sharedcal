package api_test

import (
	"slices"
	"testing"
)

type personRow struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	Status      string `json:"status"`
	Permissions struct {
		Edit, Manage bool
	} `json:"permissions"`
}

type peopleList struct {
	Users  []struct{ ID, Name string }
	People []personRow
}

func TestPeople(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol")
	if alice.Role != "admin" || bob.Role == "admin" {
		t.Fatalf("roles: %s %s", alice.Role, bob.Role)
	}

	var typ typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{
		"name": "Birthday", "colorSlot": "tag-2", "titleTemplate": "{{who}}'s birthday",
		"fields": []map[string]any{{"key": "who", "label": "Who", "kind": "members", "multiple": false, "required": true}},
	}, 201, &typ)

	// A writer's submission is pending; viewers can't add people.
	var kid personRow
	bob.mustJSON("POST", "/api/people", map[string]any{"firstName": " Emma ", "lastName": " Smith "}, 201, &kid)
	if kid.Name != "Emma Smith" || kid.Status != "pending" || !kid.Permissions.Edit || kid.Permissions.Manage {
		t.Fatalf("submitted: %+v", kid)
	}
	if kid.FirstName != "Emma" || kid.LastName != "Smith" {
		t.Fatalf("parts: %+v", kid)
	}
	for name, body := range map[string]map[string]any{
		"no last name":  {"firstName": "Cher"},
		"blank last":    {"firstName": "Cher", "lastName": "  "},
		"no first name": {"lastName": "Smith"},
	} {
		if st := bob.status("POST", "/api/people", body); st != 400 && st != 422 {
			t.Errorf("%s: %d", name, st)
		}
	}
	if st := carol.status("POST", "/api/people", map[string]any{"firstName": "No", "lastName": "Pe"}); st != 403 && carol.Role == "viewer" {
		t.Errorf("viewer create: %d", st)
	}
	if st := bob.status("POST", "/api/people", map[string]any{"firstName": "emma", "lastName": "SMITH"}); st != 400 && st != 422 {
		t.Errorf("duplicate name: %d", st)
	}

	// Pending people are visible to their author and admins only.
	has := func(c *client, id string) bool {
		var l peopleList
		c.mustJSON("GET", "/api/people", nil, 200, &l)
		return slices.ContainsFunc(l.People, func(p personRow) bool { return p.ID == id })
	}
	if !has(bob, kid.ID) || !has(alice, kid.ID) || has(carol, kid.ID) {
		t.Fatal("pending visibility")
	}
	var l peopleList
	carol.mustJSON("GET", "/api/people", nil, 200, &l)
	if len(l.Users) < 3 {
		t.Fatalf("users section: %+v", l.Users)
	}

	// The author can use the pending person already; the title follows renames.
	var ev Event
	bob.mustJSON("POST", "/api/events", map[string]any{
		"eventTypeId": typ.ID, "fields": map[string]any{"who": []string{kid.ID}},
		"timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": "2026-11-06",
	}, 201, &ev)
	if ev.Title != "Emma Smith's birthday" {
		t.Fatalf("title %q", ev.Title)
	}
	if st := bob.status("PATCH", "/api/people/"+kid.ID, map[string]any{"firstName": "Emma", "lastName": "Jones"}); st != 200 {
		t.Fatalf("rename: %d", st)
	}
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &ev)
	if ev.Title != "Emma Jones's birthday" {
		t.Fatalf("renamed title %q", ev.Title)
	}

	// Only admins review.
	for _, c := range []*client{bob, carol} {
		if st := c.status("POST", "/api/people/"+kid.ID+"/approve", nil); st != 403 {
			t.Errorf("%s approve: %d", c.Role, st)
		}
		if st := c.status("DELETE", "/api/people/"+kid.ID, nil); st != 403 {
			t.Errorf("%s delete: %d", c.Role, st)
		}
		if st := c.status("POST", "/api/people/"+kid.ID+"/bind", map[string]any{"userId": carol.ID}); st != 403 {
			t.Errorf("%s bind: %d", c.Role, st)
		}
	}
	var n struct{ Pending int64 }
	alice.mustJSON("GET", "/api/people/count", nil, 200, &n)
	if n.Pending != 1 {
		t.Fatalf("pending count %d", n.Pending)
	}
	alice.mustJSON("POST", "/api/people/"+kid.ID+"/approve", nil, 200, &kid)
	if kid.Status != "active" || !has(carol, kid.ID) {
		t.Fatalf("approve: %+v", kid)
	}

	// Binding rewrites the reference to the user and keeps the title in step.
	if st := alice.status("POST", "/api/people/"+kid.ID+"/bind", map[string]any{"userId": "nobody"}); st != 404 {
		t.Errorf("bind unknown user: %d", st)
	}
	if st := alice.status("POST", "/api/people/"+kid.ID+"/bind", map[string]any{"userId": carol.ID}); st != 204 {
		t.Fatalf("bind: %d", st)
	}
	if has(alice, kid.ID) {
		t.Error("bound person still listed")
	}
	var got struct {
		Title  string
		Fields map[string][]string
	}
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if len(got.Fields["who"]) != 1 || got.Fields["who"][0] != carol.ID || got.Title == "Emma Jones's birthday" {
		t.Fatalf("after bind: %+v", got)
	}

	// A stale client that still sends the old id is mapped onto the user.
	bob.mustJSON("POST", "/api/events", map[string]any{
		"eventTypeId": typ.ID, "fields": map[string]any{"who": []string{kid.ID}},
		"timeZone": "UTC", "allDay": true, "start": "2026-11-07", "end": "2026-11-08",
	}, 201, &ev)
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if got.Fields["who"][0] != carol.ID {
		t.Fatalf("stale id: %+v", got)
	}

	// Deleting a person takes them out of events.
	var gran personRow
	alice.mustJSON("POST", "/api/people", map[string]any{"firstName": "Granny", "lastName": "Jones"}, 201, &gran)
	if gran.Status != "active" {
		t.Fatalf("admin-created: %+v", gran)
	}
	bob.mustJSON("POST", "/api/events", map[string]any{
		"eventTypeId": typ.ID, "fields": map[string]any{"who": []string{gran.ID}},
		"timeZone": "UTC", "allDay": true, "start": "2026-11-09", "end": "2026-11-10",
	}, 201, &ev)
	if st := alice.status("DELETE", "/api/people/"+gran.ID, nil); st != 204 {
		t.Fatalf("delete: %d", st)
	}
	got.Fields = nil
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &got)
	if len(got.Fields["who"]) != 0 {
		t.Fatalf("after delete: %+v", got)
	}
	if st := bob.status("POST", "/api/events", map[string]any{
		"eventTypeId": typ.ID, "fields": map[string]any{"who": []string{gran.ID}},
		"timeZone": "UTC", "allDay": true, "start": "2026-11-09", "end": "2026-11-10",
	}); st != 400 && st != 422 {
		t.Errorf("deleted person accepted: %d", st)
	}
}

func TestFirstNameInTitles(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob := e.login(t, "alice"), e.login(t, "bob")

	fields := []map[string]any{
		{"key": "who", "label": "Who", "kind": "members", "required": true},
		{"key": "note", "label": "Note", "kind": "text"},
	}
	// first names only make sense for people fields
	if st := alice.status("POST", "/api/event-types", map[string]any{
		"name": "Bad", "colorSlot": "tag-1", "titleTemplate": "{{who}} {{note:first}}", "fields": fields,
	}); st != 400 && st != 422 {
		t.Fatalf("first on a text field: %d", st)
	}
	var typ typeDTO
	alice.mustJSON("POST", "/api/event-types", map[string]any{
		"name": "Birthday", "colorSlot": "tag-2", "titleTemplate": "{{ who:first }}'s birthday ({{who}})", "fields": fields,
	}, 201, &typ)

	var kid personRow
	alice.mustJSON("POST", "/api/people", map[string]any{"firstName": "Emma", "lastName": "Smith"}, 201, &kid)
	var ev Event
	create := func(id string) Event {
		var out Event
		bob.mustJSON("POST", "/api/events", map[string]any{
			"eventTypeId": typ.ID, "fields": map[string]any{"who": []string{id}},
			"timeZone": "UTC", "allDay": true, "start": "2026-11-05", "end": "2026-11-06",
		}, 201, &out)
		return out
	}
	ev = create(kid.ID)
	if ev.Title != "Emma's birthday (Emma Smith)" {
		t.Fatalf("person title %q", ev.Title)
	}
	alice.mustJSON("PATCH", "/api/people/"+kid.ID, map[string]any{"firstName": "Em", "lastName": "Smith"}, 200, nil)
	bob.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &ev)
	if ev.Title != "Em's birthday (Em Smith)" {
		t.Fatalf("renamed title %q", ev.Title)
	}

	// A registered user's first name is the first word of their display name.
	if u := create(alice.ID); u.Title != "Alice's birthday (Alice Admin)" {
		t.Fatalf("user title %q", u.Title)
	}
}
