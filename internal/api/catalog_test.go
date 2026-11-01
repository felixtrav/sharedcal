package api_test

import "testing"

type catalogReq struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	Icon          string  `json:"icon"`
	ColorSlot     string  `json:"colorSlot"`
	Other         string  `json:"other"`
	Status        string  `json:"status"`
	ReviewNote    string  `json:"reviewNote"`
	CreatedItemID *string `json:"createdItemId"`
	Author        struct {
		ID string `json:"id"`
	} `json:"author"`
	Permissions struct {
		Review, Withdraw bool
	} `json:"permissions"`
}

type catalogList struct {
	Requests []catalogReq `json:"requests"`
}

func TestCatalogRequests(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol")
	alice.mustJSON("POST", "/api/tags", map[string]any{"name": "Work", "colorSlot": "tag-1"}, 201, nil)
	alice.mustJSON("POST", "/api/event-types", map[string]any{"name": "Birthday", "colorSlot": "tag-2"}, 201, nil)

	// Viewers and writers can see what exists.
	for _, c := range []*client{bob, carol} {
		if st := c.status("GET", "/api/tags", nil); st != 200 {
			t.Errorf("%s list tags: %d", c.Role, st)
		}
		if st := c.status("GET", "/api/event-types", nil); st != 200 {
			t.Errorf("%s list event types: %d", c.Role, st)
		}
	}

	var tagReq, typeReq catalogReq
	bob.mustJSON("POST", "/api/catalog-requests", map[string]any{"kind": "tag", "name": "  Social ", "colorSlot": "tag-3", "other": "for parties"}, 201, &tagReq)
	if tagReq.Name != "Social" || tagReq.Status != "open" || tagReq.Icon != "" || tagReq.Other != "for parties" || !tagReq.Permissions.Withdraw || tagReq.Permissions.Review {
		t.Fatalf("tag request: %+v", tagReq)
	}
	carol.mustJSON("POST", "/api/catalog-requests", map[string]any{"kind": "event_type", "name": "Concert", "colorSlot": "tag-4", "icon": "music"}, 201, &typeReq)
	if typeReq.Icon != "music" {
		t.Fatalf("type request icon: %+v", typeReq)
	}

	for name, body := range map[string]map[string]any{
		"extra setting":  {"kind": "event_type", "name": "Gala", "colorSlot": "tag-1", "adminOnly": true},
		"extra field":    {"kind": "event_type", "name": "Gala", "colorSlot": "tag-1", "fields": []any{}},
		"tag with icon":  {"kind": "tag", "name": "Gala", "colorSlot": "tag-1", "icon": "music"},
		"unknown icon":   {"kind": "event_type", "name": "Gala", "colorSlot": "tag-1", "icon": "rocket"},
		"hex color":      {"kind": "tag", "name": "Gala", "colorSlot": "#ff0000"},
		"bad kind":       {"kind": "theme", "name": "Gala", "colorSlot": "tag-1"},
		"empty name":     {"kind": "tag", "name": "  ", "colorSlot": "tag-1"},
		"existing tag":   {"kind": "tag", "name": "work", "colorSlot": "tag-1"},
		"existing type":  {"kind": "event_type", "name": "BIRTHDAY", "colorSlot": "tag-1"},
		"already asked":  {"kind": "tag", "name": "social", "colorSlot": "tag-1"},
		"note too long":  {"kind": "tag", "name": "Gala", "colorSlot": "tag-1", "other": string(make([]byte, 501))},
		"note too long2": {"kind": "tag", "name": "Gala", "colorSlot": "tag-1", "other": "x"},
	} {
		want := 422
		if name == "note too long2" {
			want = 201
		}
		if st := carol.status("POST", "/api/catalog-requests", body); st != want {
			t.Errorf("%s: %d, want %d", name, st, want)
		}
	}
	// The same name can be asked for as a tag and as an event type.
	bob.mustJSON("POST", "/api/catalog-requests", map[string]any{"kind": "event_type", "name": "Social", "colorSlot": "tag-3"}, 201, nil)

	if st := alice.status("POST", "/api/catalog-requests", map[string]any{"kind": "tag", "name": "Admin tag", "colorSlot": "tag-1"}); st != 403 {
		t.Errorf("admin request: %d", st)
	}

	// People see their own requests; admins see everyone's.
	var l catalogList
	bob.mustJSON("GET", "/api/catalog-requests", nil, 200, &l)
	for _, r := range l.Requests {
		if r.Author.ID == carol.ID {
			t.Errorf("bob sees carol's request %q", r.Name)
		}
	}
	if len(l.Requests) != 2 {
		t.Errorf("bob's requests: %d", len(l.Requests))
	}
	alice.mustJSON("GET", "/api/catalog-requests", nil, 200, &l)
	if len(l.Requests) != 4 || !l.Requests[0].Permissions.Review {
		t.Fatalf("admin list: %+v", l.Requests)
	}
	var count struct{ Open int64 }
	alice.mustJSON("GET", "/api/catalog-requests/count", nil, 200, &count)
	if count.Open != 4 {
		t.Errorf("open count: %d", count.Open)
	}
	if st := bob.status("GET", "/api/catalog-requests/count", nil); st != 403 {
		t.Errorf("count as writer: %d", st)
	}

	// Only admins review.
	for _, p := range []string{"approve", "reject"} {
		if st := bob.status("POST", "/api/catalog-requests/"+tagReq.ID+"/"+p, map[string]any{}); st != 403 {
			t.Errorf("writer %s: %d", p, st)
		}
	}
	if st := bob.status("POST", "/api/catalog-requests/"+typeReq.ID+"/withdraw", nil); st != 403 {
		t.Errorf("withdraw someone else's: %d", st)
	}

	// Approving creates the tag, with the admin's adjustments.
	var done catalogReq
	alice.mustJSON("POST", "/api/catalog-requests/"+tagReq.ID+"/approve", map[string]any{"name": "Socials", "colorSlot": "tag-5"}, 200, &done)
	if done.Status != "approved" || done.Name != "Socials" || done.CreatedItemID == nil {
		t.Fatalf("approved: %+v", done)
	}
	var tags struct {
		Tags []struct{ ID, Name, ColorSlot string } `json:"tags"`
	}
	carol.mustJSON("GET", "/api/tags", nil, 200, &tags)
	if len(tags.Tags) != 2 || tags.Tags[1].ID != *done.CreatedItemID || tags.Tags[1].Name != "Socials" || tags.Tags[1].ColorSlot != "tag-5" {
		t.Errorf("tags after approval: %+v", tags.Tags)
	}
	if st := alice.status("POST", "/api/catalog-requests/"+tagReq.ID+"/approve", map[string]any{}); st != 422 {
		t.Errorf("approve twice: %d", st)
	}
	if st := alice.status("POST", "/api/catalog-requests/"+tagReq.ID+"/reject", map[string]any{}); st != 422 {
		t.Errorf("reject after approval: %d", st)
	}
	if st := bob.status("POST", "/api/catalog-requests/"+tagReq.ID+"/withdraw", nil); st != 403 {
		t.Errorf("withdraw after approval: %d", st)
	}

	// Approving an event type creates it with plain defaults.
	alice.mustJSON("POST", "/api/catalog-requests/"+typeReq.ID+"/approve", map[string]any{}, 200, &done)
	var types struct {
		EventTypes []struct {
			ID, Name, Icon, ColorSlot string
			AdminOnly, Ownerless      bool
			Fields                    []any
		} `json:"eventTypes"`
	}
	carol.mustJSON("GET", "/api/event-types", nil, 200, &types)
	if len(types.EventTypes) != 2 {
		t.Fatalf("event types: %+v", types.EventTypes)
	}
	if got := types.EventTypes[1]; got.Name != "Concert" || got.Icon != "music" || got.ColorSlot != "tag-4" || got.AdminOnly || got.Ownerless || len(got.Fields) != 0 {
		t.Errorf("approved type: %+v", got)
	}

	// A name taken in the meantime blocks approval until it is changed.
	var dup catalogReq
	bob.mustJSON("POST", "/api/catalog-requests", map[string]any{"kind": "tag", "name": "Family", "colorSlot": "tag-6"}, 201, &dup)
	alice.mustJSON("POST", "/api/tags", map[string]any{"name": "family", "colorSlot": "tag-1"}, 201, nil)
	if st := alice.status("POST", "/api/catalog-requests/"+dup.ID+"/approve", map[string]any{}); st != 422 {
		t.Errorf("approve taken name: %d", st)
	}
	alice.mustJSON("POST", "/api/catalog-requests/"+dup.ID+"/reject", map[string]any{"note": "we have Family"}, 200, &done)
	if done.Status != "rejected" || done.ReviewNote != "we have Family" || done.Permissions.Review {
		t.Errorf("rejected: %+v", done)
	}

	// Authors can withdraw while open.
	var mine catalogReq
	bob.mustJSON("POST", "/api/catalog-requests", map[string]any{"kind": "tag", "name": "Oops", "colorSlot": "tag-1"}, 201, &mine)
	bob.mustJSON("POST", "/api/catalog-requests/"+mine.ID+"/withdraw", nil, 200, &done)
	if done.Status != "withdrawn" {
		t.Errorf("withdrawn: %+v", done)
	}
	bob.mustJSON("GET", "/api/catalog-requests?status=all", nil, 200, &l)
	if len(l.Requests) != 4 {
		t.Errorf("bob's history: %d", len(l.Requests))
	}
	bob.mustJSON("GET", "/api/catalog-requests", nil, 200, &l)
	if len(l.Requests) != 1 {
		t.Errorf("bob's open requests: %d", len(l.Requests))
	}
	// The withdrawn name can be requested again.
	bob.mustJSON("POST", "/api/catalog-requests", map[string]any{"kind": "tag", "name": "Oops", "colorSlot": "tag-1"}, 201, nil)
}
