package api_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/felixtrav/sharedcal/internal/attachments"
)

type suggestion struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Changes map[string]struct {
		Base     any  `json:"base"`
		Proposed any  `json:"proposed"`
		Current  any  `json:"current"`
		Conflict bool `json:"conflict"`
	} `json:"changes"`
	AcceptedFields []string `json:"acceptedFields"`
	Permissions    struct {
		Review   bool `json:"review"`
		Withdraw bool `json:"withdraw"`
	} `json:"permissions"`
}

func TestSuggestions(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")
	ev := bob.createEvent(map[string]any{"title": "Board games"})

	// Viewers and owners cannot suggest.
	if st := carol.status("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1, "changes": map[string]any{"title": "x"}}); st != 403 {
		t.Errorf("viewer suggest: %d", st)
	}
	if st := bob.status("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1, "changes": map[string]any{"title": "x"}}); st != 403 {
		t.Errorf("owner suggest: %d", st)
	}
	// No-op suggestion refused.
	if st := dave.status("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1, "changes": map[string]any{"title": "Board games"}}); st != 422 {
		t.Errorf("no-op suggest: %d", st)
	}
	var sg suggestion
	dave.mustJSON("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1,
		"changes": map[string]any{"title": "Game night", "location": "Dana's"}}, 201, &sg)
	if len(sg.Changes) != 2 || !sg.Permissions.Withdraw || sg.Permissions.Review {
		t.Fatalf("created: %+v", sg)
	}
	// Owner sees it in the inbox.
	var inbox struct {
		Items []struct {
			Kind string `json:"kind"`
		} `json:"items"`
		Unread int `json:"unread"`
	}
	bob.mustJSON("GET", "/api/inbox", nil, 200, &inbox)
	if inbox.Unread != 1 || inbox.Items[0].Kind != "suggestion.created" {
		t.Fatalf("owner inbox: %+v", inbox)
	}
	// Review rights: other writer and viewer cannot accept; author cannot.
	for _, c := range []*client{carol, dave} {
		if st := c.status("POST", "/api/suggestions/"+sg.ID+"/accept", map[string]any{}); st != 403 {
			t.Errorf("accept by %s: %d", c.Role, st)
		}
	}
	// Owner changes the title meanwhile → conflict on title.
	bob.mustJSON("PATCH", "/api/events/"+ev.ID, map[string]any{"title": "Board games!"}, 200, nil, "If-Match", "1")
	var list struct {
		Suggestions []suggestion `json:"suggestions"`
	}
	bob.mustJSON("GET", "/api/events/"+ev.ID+"/suggestions", nil, 200, &list)
	if !list.Suggestions[0].Changes["title"].Conflict || list.Suggestions[0].Changes["location"].Conflict {
		t.Fatalf("conflict flags: %+v", list.Suggestions[0].Changes)
	}
	if st := bob.status("POST", "/api/suggestions/"+sg.ID+"/accept", map[string]any{}); st != 422 {
		t.Errorf("accept conflicting without resolution: %d", st)
	}
	var res struct {
		Suggestion suggestion `json:"suggestion"`
		Event      Event      `json:"event"`
	}
	bob.mustJSON("POST", "/api/suggestions/"+sg.ID+"/accept", map[string]any{"resolutions": map[string]string{"title": "current"}}, 200, &res)
	if res.Suggestion.Status != "partially_accepted" || res.Event.Title != "Board games!" || res.Event.Location != "Dana's" || res.Event.Counts.OpenSuggestions != 0 {
		t.Fatalf("partial accept: %+v / %+v", res.Suggestion, res.Event)
	}
	// Double accept fails.
	if st := bob.status("POST", "/api/suggestions/"+sg.ID+"/accept", map[string]any{}); st != 403 {
		t.Errorf("re-accept: %d", st)
	}
	// Suggester gets the decision.
	dave.mustJSON("GET", "/api/inbox", nil, 200, &inbox)
	if inbox.Unread != 1 || inbox.Items[0].Kind != "suggestion.resolved" {
		t.Errorf("suggester inbox: %+v", inbox)
	}

	// Reject with a note; withdraw.
	var s2, s3 suggestion
	dave.mustJSON("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": res.Event.Version, "changes": map[string]any{"title": "Nope"}}, 201, &s2)
	alice.mustJSON("POST", "/api/suggestions/"+s2.ID+"/reject", map[string]any{"note": "no thanks"}, 200, &s2)
	if s2.Status != "rejected" {
		t.Errorf("reject: %s", s2.Status)
	}
	dave.mustJSON("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": res.Event.Version, "changes": map[string]any{"title": "Maybe"}}, 201, &s3)
	if st := alice.status("POST", "/api/suggestions/"+s3.ID+"/withdraw", nil); st != 403 {
		t.Errorf("admin withdraw: %d", st)
	}
	dave.mustJSON("POST", "/api/suggestions/"+s3.ID+"/withdraw", nil, 200, &s3)
	if s3.Status != "withdrawn" {
		t.Errorf("withdraw: %s", s3.Status)
	}
	// Stale base version is refused with 409.
	if st := dave.status("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": 1, "changes": map[string]any{"title": "Old"}}); st != 409 {
		t.Errorf("stale base: %d", st)
	}

	// Delete suggestion: accepting deletes the event and closes the others.
	var del, other suggestion
	dave.mustJSON("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "delete", "baseVersion": res.Event.Version}, 201, &del)
	alice.mustJSON("POST", "/api/events/"+ev.ID+"/suggestions", map[string]any{"kind": "edit", "baseVersion": res.Event.Version, "changes": map[string]any{"title": "Admin idea"}}, 201, &other)
	bob.mustJSON("POST", "/api/suggestions/"+del.ID+"/accept", map[string]any{}, 200, &res)
	if st := carol.status("GET", "/api/events/"+ev.ID, nil); st != 404 {
		t.Errorf("event after accepted delete: %d", st)
	}
	var trash struct {
		Events []Event `json:"events"`
	}
	alice.mustJSON("GET", "/api/trash", nil, 200, &trash)
	if len(trash.Events) != 1 {
		t.Fatalf("trash: %d", len(trash.Events))
	}
	if st := alice.status("POST", "/api/suggestions/"+other.ID+"/accept", map[string]any{}); st != 403 && st != 422 {
		t.Errorf("closed suggestion accept: %d", st)
	}
}

type comment struct {
	ID          string   `json:"id"`
	ParentID    *string  `json:"parentId"`
	Body        string   `json:"body"`
	Mentions    []string `json:"mentions"`
	EditedAt    *string  `json:"editedAt"`
	Deleted     bool     `json:"deleted"`
	Permissions struct {
		Edit, Delete, Attach bool
	} `json:"permissions"`
}

func TestComments(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{})
	alice, bob, carol, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")
	ev := bob.createEvent(map[string]any{"title": "Picnic"})
	var top, reply comment
	carol.mustJSON("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "Who brings <b>snacks</b>?", "mentions": []string{dave.ID, carol.ID, "missing"}}, 201, &top)
	if len(top.Mentions) != 1 || top.Mentions[0] != dave.ID || top.Body != "Who brings <b>snacks</b>?" {
		t.Fatalf("top: %+v", top)
	}
	bob.mustJSON("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "Me", "parentId": top.ID}, 201, &reply)
	if st := dave.status("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "deep", "parentId": reply.ID}); st != 422 {
		t.Errorf("reply to reply: %d", st)
	}
	if st := carol.status("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "   "}); st != 422 {
		t.Errorf("blank: %d", st)
	}
	inboxKinds := func(c *client) []string {
		var in struct {
			Items []struct {
				Kind string `json:"kind"`
			} `json:"items"`
		}
		c.mustJSON("GET", "/api/inbox", nil, 200, &in)
		var out []string
		for _, i := range in.Items {
			out = append(out, i.Kind)
		}
		return out
	}
	if k := inboxKinds(dave); len(k) != 1 || k[0] != "comment.mention" {
		t.Errorf("dave inbox: %v", k)
	}
	if k := inboxKinds(carol); len(k) != 1 || k[0] != "comment.reply" {
		t.Errorf("carol inbox: %v", k)
	}
	// Edit: author only; admin cannot edit but can delete.
	if st := alice.status("PATCH", "/api/comments/"+top.ID, map[string]any{"body": "x"}); st != 403 {
		t.Errorf("admin edit: %d", st)
	}
	if st := dave.status("DELETE", "/api/comments/"+top.ID, nil); st != 403 {
		t.Errorf("other delete: %d", st)
	}
	carol.mustJSON("PATCH", "/api/comments/"+top.ID, map[string]any{"body": "Who brings drinks?"}, 200, &top)
	if top.EditedAt == nil {
		t.Error("edit not marked")
	}
	if st := alice.status("DELETE", "/api/comments/"+top.ID, nil); st != 204 {
		t.Fatalf("admin delete: %d", st)
	}
	var list struct {
		Comments []comment `json:"comments"`
	}
	dave.mustJSON("GET", "/api/events/"+ev.ID+"/comments", nil, 200, &list)
	if len(list.Comments) != 2 || !list.Comments[0].Deleted || list.Comments[0].Body != "" || list.Comments[1].Body != "Me" {
		t.Fatalf("placeholder: %+v", list.Comments)
	}
	// Mark all read.
	var unread struct {
		Unread int `json:"unread"`
	}
	dave.mustJSON("POST", "/api/inbox/read", map[string]any{"all": true}, 200, &unread)
	if unread.Unread != 0 {
		t.Errorf("unread after mark all: %d", unread.Unread)
	}
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type attachmentResp struct {
	ID         string  `json:"id"`
	URL        string  `json:"url"`
	ThumbURL   *string `json:"thumbUrl"`
	IsImage    bool    `json:"isImage"`
	ScanStatus string  `json:"scanStatus"`
	Filename   string  `json:"filename"`
}

func decode[T any](t *testing.T, r io.Reader) T {
	t.Helper()
	var v T
	b, _ := io.ReadAll(r)
	if err := jsonUnmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	return v
}

func TestAttachments(t *testing.T) {
	t.Parallel()
	e := newEnv(t, options{maxUpload: 1 << 20, maxPerEvent: 2})
	alice, bob, carol, dave := e.login(t, "alice"), e.login(t, "bob"), e.login(t, "carol"), e.login(t, "dave")
	ev := bob.createEvent(map[string]any{"title": "Flyer"})
	path := "/api/events/" + ev.ID + "/attachments"

	// Viewer and non-owner writer cannot attach to the event.
	for _, c := range []*client{carol, dave} {
		if r := c.upload(path, "a.png", pngBytes(t, 20, 10)); r.StatusCode != 403 {
			t.Errorf("%s upload: %d", c.Role, r.StatusCode)
		}
	}
	r := bob.upload(path, "../../etc/photo.png", pngBytes(t, 1200, 600))
	if r.StatusCode != 201 {
		t.Fatalf("png: %d %s", r.StatusCode, readAll(r))
	}
	img := decode[attachmentResp](t, r.Body)
	if !img.IsImage || img.ThumbURL == nil || img.ScanStatus != "skipped" || strings.Contains(img.Filename, "/") {
		t.Fatalf("png result: %+v", img)
	}
	// Served inline with hardened headers.
	resp := carol.do("GET", img.URL, nil)
	h := resp.Header
	if resp.StatusCode != 200 || h.Get("Content-Type") != "image/png" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Cross-Origin-Resource-Policy") != "same-origin" || !strings.Contains(h.Get("Content-Security-Policy"), "sandbox") ||
		!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") || !strings.HasPrefix(h.Get("Content-Disposition"), "inline") {
		t.Fatalf("image headers: %d %v", resp.StatusCode, h)
	}
	if _, err := png.Decode(resp.Body); err != nil {
		t.Fatalf("served image does not decode: %v", err)
	}
	thumb := carol.do("GET", *img.ThumbURL, nil)
	if cfg, _, err := image.DecodeConfig(thumb.Body); err != nil || cfg.Width != 480 {
		t.Errorf("thumbnail: %v %+v", err, cfg)
	}
	// Text is served as an attachment download.
	r = bob.upload(path, "notes.txt", []byte("hello\n"))
	if r.StatusCode != 201 {
		t.Fatalf("txt: %d %s", r.StatusCode, readAll(r))
	}
	txt := decode[attachmentResp](t, r.Body)
	resp = carol.do("GET", txt.URL, nil)
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(resp.Header.Get("Content-Disposition"), "filename*=") {
		t.Errorf("txt disposition: %q", resp.Header.Get("Content-Disposition"))
	}
	// Count limit (2 per event in this env).
	if r := bob.upload(path, "c.txt", []byte("third")); r.StatusCode != 422 {
		t.Errorf("count limit: %d", r.StatusCode)
	}
	// Refused types and sizes (delete one first to get under the limit).
	if st := dave.status("DELETE", "/api/attachments/"+txt.ID, nil); st != 403 {
		t.Errorf("non-owner delete: %d", st)
	}
	if st := bob.status("DELETE", "/api/attachments/"+txt.ID, nil); st != 204 {
		t.Fatalf("owner delete: %d", st)
	}
	if st := carol.status("GET", txt.URL, nil); st != 404 {
		t.Errorf("deleted attachment still served: %d", st)
	}
	for name, data := range map[string][]byte{
		"x.svg":  []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"x.txt":  []byte("<!doctype html><html><script>alert(1)</script></html>"),
		"x.jpg":  pngBytes(t, 5, 5),
		"x.html": []byte("<html></html>"),
	} {
		if r := bob.upload(path, name, data); r.StatusCode != 415 {
			t.Errorf("%s: %d %s", name, r.StatusCode, readAll(r))
		}
	}
	if r := bob.upload(path, "big.txt", bytes.Repeat([]byte("a"), 2<<20)); r.StatusCode != 413 {
		t.Errorf("oversized: %d", r.StatusCode)
	}
	if r := bob.upload(path, "empty.txt", nil); r.StatusCode != 422 && r.StatusCode != 400 {
		t.Errorf("empty: %d", r.StatusCode)
	}
	// The attachment trash is admin-only and lists the removed file.
	type trashBody struct {
		Attachments []struct {
			ID         string `json:"id"`
			EventTitle string `json:"eventTitle"`
			DeletedAt  string `json:"deletedAt"`
		} `json:"attachments"`
	}
	if st := bob.status("GET", "/api/trash/attachments", nil); st != 403 {
		t.Errorf("writer attachment trash: %d", st)
	}
	var trash trashBody
	alice.mustJSON("GET", "/api/trash/attachments", nil, 200, &trash)
	if len(trash.Attachments) != 1 || trash.Attachments[0].ID != txt.ID || trash.Attachments[0].EventTitle != "Flyer" || trash.Attachments[0].DeletedAt == "" {
		t.Fatalf("attachment trash: %+v", trash)
	}
	// Admin restore; the attachment leaves the trash and shows up in the event detail.
	alice.mustJSON("POST", "/api/attachments/"+txt.ID+"/restore", nil, 200, nil)
	alice.mustJSON("GET", "/api/trash/attachments", nil, 200, &trash)
	if len(trash.Attachments) != 0 {
		t.Errorf("restored attachment still in trash: %+v", trash)
	}
	var detail Event
	carol.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &detail)
	if len(detail.Attachments) != 2 {
		t.Errorf("detail attachments: %d", len(detail.Attachments))
	}
	// Comment attachments: writers on their own comments only.
	var cm comment
	dave.mustJSON("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "see map"}, 201, &cm)
	if r := bob.upload("/api/comments/"+cm.ID+"/attachments", "map.png", pngBytes(t, 10, 10)); r.StatusCode != 403 {
		t.Errorf("attach to other's comment: %d", r.StatusCode)
	}
	if r := dave.upload("/api/comments/"+cm.ID+"/attachments", "map.png", pngBytes(t, 10, 10)); r.StatusCode != 201 {
		t.Errorf("attach to own comment: %d %s", r.StatusCode, readAll(r))
	}
	// A file on a deleted comment is not offered for restore, and restoring
	// it directly is refused.
	r = dave.upload("/api/comments/"+cm.ID+"/attachments", "route.txt", []byte("left at the oak"))
	if r.StatusCode != 201 {
		t.Fatalf("comment attachment: %d %s", r.StatusCode, readAll(r))
	}
	route := decode[attachmentResp](t, r.Body)
	if st := dave.status("DELETE", "/api/attachments/"+route.ID, nil); st != 204 {
		t.Fatalf("remove comment attachment: %d", st)
	}
	if st := dave.status("DELETE", "/api/comments/"+cm.ID, nil); st != 204 {
		t.Fatalf("delete comment: %d", st)
	}
	alice.mustJSON("GET", "/api/trash/attachments", nil, 200, &trash)
	for _, a := range trash.Attachments {
		if a.ID == route.ID {
			t.Error("attachment of a deleted comment listed in trash")
		}
	}
	if st := alice.status("POST", "/api/attachments/"+route.ID+"/restore", nil); st != 409 {
		t.Errorf("restore onto deleted comment: %d", st)
	}
	var vc comment
	carol.mustJSON("POST", "/api/events/"+ev.ID+"/comments", map[string]any{"body": "viewer"}, 201, &vc)
	if r := carol.upload("/api/comments/"+vc.ID+"/attachments", "v.png", pngBytes(t, 10, 10)); r.StatusCode != 403 {
		t.Errorf("viewer comment attach: %d", r.StatusCode)
	}
}

// blockingScanner holds every scan until released.
type blockingScanner struct{ release chan struct{} }

func (b *blockingScanner) Scan(ctx context.Context, r io.Reader) (attachments.Verdict, error) {
	_, _ = io.Copy(io.Discard, r)
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return attachments.Verdict{Clean: true}, nil
}

func TestPendingAttachmentsVisibleOnlyToUploader(t *testing.T) {
	t.Parallel()
	sc := &blockingScanner{release: make(chan struct{})}
	e := newEnv(t, options{scanner: sc})
	t.Cleanup(func() { close(sc.release) })
	bob, carol := e.login(t, "bob"), e.login(t, "carol")
	ev := bob.createEvent(map[string]any{"title": "Scan"})
	r := bob.upload("/api/events/"+ev.ID+"/attachments", "doc.txt", []byte("data"))
	if r.StatusCode != 201 {
		t.Fatalf("upload: %d", r.StatusCode)
	}
	a := decode[attachmentResp](t, r.Body)
	if a.ScanStatus != "pending" {
		t.Fatalf("status %s", a.ScanStatus)
	}
	if st := carol.status("GET", a.URL, nil); st != 404 {
		t.Errorf("viewer fetched pending file: %d", st)
	}
	if st := bob.status("GET", a.URL, nil); st != 200 {
		t.Errorf("uploader fetch pending: %d", st)
	}
	var d Event
	carol.mustJSON("GET", "/api/events/"+ev.ID, nil, 200, &d)
	if len(d.Attachments) != 0 {
		t.Error("pending attachment listed for others")
	}
}
