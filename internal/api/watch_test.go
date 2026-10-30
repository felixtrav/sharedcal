package api_test

import (
	"strconv"
	"testing"
)

type watchItem struct {
	ID      string  `json:"id"`
	Kind    string  `json:"kind"`
	Count   int     `json:"count"`
	EventID *string `json:"eventId"`
	ReadAt  *string `json:"readAt"`
}

func (c *client) inbox() (items []watchItem, unread int) {
	c.t.Helper()
	var in struct {
		Items  []watchItem `json:"items"`
		Unread int         `json:"unread"`
	}
	c.mustJSON("GET", "/api/inbox", nil, 200, &in)
	return in.Items, in.Unread
}

func TestWatchEvent(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	bob, carol, dave := e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")
	ev := bob.createEvent(map[string]any{"title": "Picnic"})
	other := bob.createEvent(map[string]any{"title": "Hike"})
	path := "/api/events/" + ev.ID

	var detail struct {
		Watching bool `json:"watching"`
	}
	carol.mustJSON("GET", path, nil, 200, &detail)
	if detail.Watching {
		t.Fatal("watching before opting in")
	}
	if st := carol.status("PUT", path+"/watch", nil); st != 204 {
		t.Fatalf("watch: %d", st)
	}
	if st := carol.status("PUT", path+"/watch", nil); st != 204 {
		t.Fatalf("watch twice: %d", st)
	}
	if st := carol.status("PUT", "/api/events/missing/watch", nil); st != 404 {
		t.Errorf("watch missing: %d", st)
	}
	carol.mustJSON("GET", path, nil, 200, &detail)
	if !detail.Watching {
		t.Fatal("not watching after PUT")
	}
	dave.mustJSON("GET", path, nil, 200, &detail)
	if detail.Watching {
		t.Fatal("watch leaked to another user")
	}

	// A burst of comments and edits folds into one unread item per kind.
	for i := 0; i < 5; i++ {
		dave.mustJSON("POST", path+"/comments", map[string]any{"body": "comment " + strconv.Itoa(i)}, 201, nil)
	}
	for i, title := range []string{"Picnic 2", "Picnic 3", "Picnic 4"} {
		bob.mustJSON("PATCH", path, map[string]any{"title": title}, 200, nil, "If-Match", strconv.Itoa(i+1))
	}
	// Activity on an event she does not watch, and her own, stays quiet.
	dave.mustJSON("POST", "/api/events/"+other.ID+"/comments", map[string]any{"body": "elsewhere"}, 201, nil)
	carol.mustJSON("POST", path+"/comments", map[string]any{"body": "mine"}, 201, nil)

	items, unread := carol.inbox()
	if unread != 2 || len(items) != 2 {
		t.Fatalf("want 2 folded items, got unread=%d %+v", unread, items)
	}
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Kind] = it.Count
	}
	if counts["watch.comments"] != 5 || counts["watch.updated"] != 3 {
		t.Errorf("counts: %v", counts)
	}
	// Owners watch their own events from the start, one folded item each.
	if items, _ := bob.inbox(); len(items) != 2 || items[0].Kind != "watch.comments" || items[1].Kind != "watch.comments" {
		t.Errorf("owner inbox: %+v", items)
	}
	var bobDetail struct {
		Watching bool `json:"watching"`
	}
	bob.mustJSON("GET", "/api/events/"+other.ID, nil, 200, &bobDetail)
	if !bobDetail.Watching {
		t.Error("owner not watching own event")
	}
	if st := bob.status("DELETE", "/api/events/"+other.ID+"/watch", nil); st != 204 {
		t.Errorf("owner unwatch: %d", st)
	}
	dave.mustJSON("POST", "/api/events/"+other.ID+"/comments", map[string]any{"body": "quiet now"}, 201, nil)
	if items, _ := bob.inbox(); len(items) != 2 || items[0].Count != 6 {
		t.Errorf("owner stopped watching but inbox changed: %+v", items)
	}

	// Once read, the next change starts a fresh item.
	var res struct{ Unread int }
	carol.mustJSON("POST", "/api/inbox/read", map[string]any{"all": true}, 200, &res)
	dave.mustJSON("POST", path+"/comments", map[string]any{"body": "again"}, 201, nil)
	items, unread = carol.inbox()
	if unread != 1 || items[0].Kind != "watch.comments" || items[0].Count != 1 || items[0].ReadAt != nil {
		t.Errorf("after read: unread=%d %+v", unread, items)
	}

	// A mention is more specific than the watch item for the same comment.
	dave.mustJSON("POST", path+"/comments", map[string]any{"body": "hi @carol", "mentions": []string{carol.ID}}, 201, nil)
	items, _ = carol.inbox()
	var mention, comments int
	for _, it := range items {
		if it.Kind == "comment.mention" {
			mention++
		}
		if it.Kind == "watch.comments" && it.ReadAt == nil {
			comments = it.Count
		}
	}
	if mention != 1 || comments != 1 {
		t.Errorf("mention should not also fold into the watch item: %+v", items)
	}

	// Stopping works, and deletion notifies the remaining watchers.
	dave.status("PUT", path+"/watch", nil)
	if st := carol.status("DELETE", path+"/watch", nil); st != 204 {
		t.Fatalf("unwatch: %d", st)
	}
	bob.mustJSON("DELETE", path, nil, 204, nil, "If-Match", "4")
	items, _ = carol.inbox()
	for _, it := range items {
		if it.Kind == "watch.deleted" {
			t.Errorf("carol stopped watching but got %+v", it)
		}
	}
	items, _ = dave.inbox()
	if len(items) != 1 || items[0].Kind != "watch.deleted" {
		t.Errorf("dave inbox: %+v", items)
	}
}
