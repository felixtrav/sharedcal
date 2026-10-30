package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
)

func ptr[T any](v T) *T { return &v }

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// fsBlobs is the blob store a server with DATA_DIR=dir uses.
func fsBlobs(t *testing.T, dir string) attachments.BlobStore {
	t.Helper()
	b, err := attachments.NewFSStore(filepath.Join(dir, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seed puts at least one row in every table (and two in some, so paging and
// ordering are exercised) and stores the referenced blobs: an attachment, its
// thumbnail and the logo. An unreferenced blob and an infected attachment
// whose blob is gone must both be left out of the export.
func seed(t *testing.T, db *store.DB, blobs attachments.BlobStore) {
	t.Helper()
	ts := time.Date(2026, 10, 22, 9, 30, 15, 123456000, time.UTC)
	blob := []byte("hello attachment")
	thumb := []byte("thumbnail bytes")
	logo := []byte("logo bytes")
	bsha, tsha, lsha := sha(blob), sha(thumb), sha(logo)
	gone := sha([]byte("infected, deleted"))
	for _, b := range [][]byte{blob, thumb, logo, []byte("orphan")} {
		if err := blobs.Put(context.Background(), attachments.KeyFor(sha(b)), bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
	}

	u1, u2 := store.NewID(), store.NewID()
	ev := store.NewID()
	tag := store.NewID()
	cm, reply := store.NewID(), store.NewID()
	rows := []any{
		&store.User{ID: u1, OIDCIssuer: "https://id.example.com", OIDCSubject: "a", Email: "a@example.com", DisplayName: "Dana",
			Role: "admin", RoleSource: store.RoleSourceIDP, GroupsJSON: `["calendar-admins"]`, Active: true, LastLoginAt: &ts, CreatedAt: ts, UpdatedAt: ts},
		&store.User{ID: u2, OIDCIssuer: "https://id.example.com", OIDCSubject: "b", Role: "writer", RoleSource: store.RoleSourceManual,
			GroupsJSON: `[]`, Active: false, CreatedAt: ts, UpdatedAt: ts},
		&store.UserPreference{UserID: u1, ThemeID: "default", ColorMode: "dark", TimeZone: "Europe/Berlin", WeekStart: 1, Clock24h: true,
			DefaultView: "week", CreatedAt: ts, UpdatedAt: ts},
		&store.Session{ID: strings.Repeat("f", 64), UserID: u1, ExpiresAt: ts.Add(time.Hour), IdleExpiresAt: ts.Add(time.Hour), LastSeenAt: ts, CreatedAt: ts, UpdatedAt: ts},
		&store.Event{ID: ev, OwnerID: u1, Title: "Board game night", Description: "Bring *snacks*. Ünïcødé ✓", Location: "Dana's",
			StartAt: ts, EndAt: ts.Add(4 * time.Hour), TimeZone: "America/New_York", RRule: "FREQ=WEEKLY;BYDAY=FR", SeriesEndAt: nil,
			ExdatesJSON: `["2026-11-06T23:00:00Z"]`, Status: store.StatusTentative, Version: 4, DeletedByID: nil, CreatedAt: ts, UpdatedAt: ts},
		&store.Event{ID: store.NewID(), OwnerID: u2, Title: "Deleted", Description: "", StartAt: ts, EndAt: ts, AllDay: true, TimeZone: "UTC",
			ExdatesJSON: `[]`, Status: store.StatusConfirmed, Version: 1, DeletedAt: &ts, DeletedByID: &u1, SeriesEndAt: &ts, CreatedAt: ts, UpdatedAt: ts},
		&store.EventOverride{ID: store.NewID(), EventID: ev, RecurrenceID: ts.Add(7 * 24 * time.Hour), Title: ptr("Special"), AllDay: ptr(false),
			Cancelled: false, CreatedAt: ts, UpdatedAt: ts},
		&store.Tag{ID: tag, Name: "Social", ColorSlot: "tag-3", SortOrder: 2, CreatedAt: ts, UpdatedAt: ts},
		&store.EventTag{EventID: ev, TagID: tag, Position: 0},
		&store.Comment{ID: cm, EventID: ev, AuthorID: u2, Body: "Count me in", EditedAt: &ts, CreatedAt: ts, UpdatedAt: ts},
		&store.Comment{ID: reply, EventID: ev, AuthorID: u1, ParentID: &cm, Body: "Great", CreatedAt: ts, UpdatedAt: ts},
		&store.Attachment{ID: store.NewID(), EventID: ev, CommentID: &cm, UploaderID: u2, Filename: "map.png", ContentType: "image/png",
			SizeBytes: int64(len(blob)), SHA256: bsha, StorageKey: bsha[:2] + "/" + bsha, Width: ptr(480), Height: ptr(320),
			ThumbKey: ptr(tsha[:2] + "/" + tsha), ScanStatus: store.ScanClean, CreatedAt: ts, UpdatedAt: ts},
		&store.Attachment{ID: store.NewID(), EventID: ev, UploaderID: u2, Filename: "virus.pdf", ContentType: "application/pdf",
			SizeBytes: 17, SHA256: gone, StorageKey: attachments.KeyFor(gone), ScanStatus: store.ScanInfected, CreatedAt: ts, UpdatedAt: ts},
		&store.Suggestion{ID: store.NewID(), EventID: ev, AuthorID: u2, Kind: store.SuggestionEdit, PatchJSON: `{"title":"x"}`, BaseVersion: 3,
			Status: store.SuggestionRejected, ReviewerID: &u1, ReviewNote: "no", ResolvedAt: &ts, CreatedAt: ts, UpdatedAt: ts},
		&store.Notification{ID: store.NewID(), UserID: u1, Kind: "comment.reply", SubjectType: "comment", SubjectID: reply, EventID: &ev, ActorID: &u2, CreatedAt: ts, UpdatedAt: ts},
		&store.Activity{ID: store.NewID(), ActorID: &u1, Action: "event.created", SubjectType: "event", SubjectID: ev, EventID: &ev, BeforeJSON: "null", AfterJSON: `{"a":1}`, CreatedAt: ts},
		&store.ChangeLog{Type: "event.created", SubjectID: ev, PayloadJSON: "{}", CreatedAt: ts},
		&store.ChangeLog{Type: "event.updated", SubjectID: ev, AudienceUserID: &u1, PayloadJSON: "{}", CreatedAt: ts},
		&store.FeedToken{ID: store.NewID(), UserID: u1, TokenHash: strings.Repeat("a", 64), LastUsedAt: &ts, CreatedAt: ts, UpdatedAt: ts},
		&store.Setting{Key: "calendar_name", ValueJSON: `"Family"`, CreatedAt: ts, UpdatedAt: ts},
		&store.Setting{Key: "instance", ValueJSON: `{"calendarName":"Family","logoKey":"` + attachments.KeyFor(lsha) + `"}`, CreatedAt: ts, UpdatedAt: ts},
	}
	for i := 0; i < 3; i++ { // more users than one key comparison
		id := store.NewID()
		rows = append(rows, &store.User{ID: id, OIDCIssuer: "https://id.example.com", OIDCSubject: "x" + id, Role: "viewer",
			RoleSource: store.RoleSourceIDP, GroupsJSON: "[]", Active: true, CreatedAt: ts, UpdatedAt: ts})
	}
	for _, r := range rows {
		if err := db.W.Create(r).Error; err != nil {
			t.Fatalf("seed %T: %v", r, err)
		}
	}
}

func export(t *testing.T, db *store.DB, blobs attachments.BlobStore) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := Export(context.Background(), db, blobs, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	return buf.Bytes()
}

func entries(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		c, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = c
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := storetest.Open(t)
	srcBlobs := fsBlobs(t, t.TempDir())
	seed(t, src, srcBlobs)
	pageSizeForTest(t, 2) // force several pages for users

	a := export(t, src, srcBlobs)
	ea := entries(t, a)
	var m Manifest
	if err := json.Unmarshal(ea[manifestName], &m); err != nil {
		t.Fatal(err)
	}
	if m.Format != FormatVersion || m.Dialect != src.Dialect || m.ChangeLogMaxSeq != 2 {
		t.Fatalf("manifest: %+v", m)
	}
	if m.RowCounts["users"] != 5 || m.RowCounts["comments"] != 2 {
		t.Fatalf("row counts: %v", m.RowCounts)
	}
	if _, ok := ea[tablesDir+"sessions.jsonl"]; ok {
		t.Fatal("sessions must not be exported")
	}
	if _, ok := ea[tablesDir+"change_log.jsonl"]; ok {
		t.Fatal("change_log must not be exported")
	}
	if len(m.Files) != 3 { // attachment, thumbnail, logo
		t.Fatalf("files: %+v", m.Files)
	}

	dst := storetest.Open(t)
	dstDir := t.TempDir()
	dstBlobs := fsBlobs(t, dstDir)
	if err := Import(ctx, dst, dstDir, dstBlobs, bytes.NewReader(a), int64(len(a))); err != nil {
		t.Fatalf("import: %v", err)
	}

	// Every exported table and blob is identical when exported again from the target.
	eb := entries(t, export(t, dst, dstBlobs))
	for name, content := range ea {
		if name == manifestName {
			continue
		}
		if !bytes.Equal(eb[name], content) {
			t.Errorf("%s differs after round trip:\nsrc: %s\ndst: %s", name, content, eb[name])
		}
	}
	for name := range eb {
		if _, ok := ea[name]; !ok {
			t.Errorf("unexpected %s after round trip", name)
		}
	}
	for _, f := range m.Files {
		rel := strings.TrimPrefix(f.Path, filesDir)
		b, err := os.ReadFile(filepath.Join(dstDir, filepath.FromSlash(rel)))
		if err != nil || sha(b) != f.SHA256 {
			t.Errorf("file %s not restored: %v", f.Path, err)
		}
	}

	// Spot-check typed values survived, not just their JSON form.
	var ev store.Event
	if err := dst.R.Where("title = ?", "Board game night").First(&ev).Error; err != nil {
		t.Fatal(err)
	}
	if ev.Version != 4 || ev.Status != store.StatusTentative || !ev.StartAt.Equal(time.Date(2026, 10, 22, 9, 30, 15, 123456000, time.UTC)) {
		t.Errorf("event: %+v", ev)
	}
	var n int64
	dst.R.Model(&store.Session{}).Count(&n)
	if n != 0 {
		t.Error("sessions were imported")
	}

	// change_log continues after the source's high-water mark.
	cl := store.ChangeLog{Type: "event.updated", SubjectID: ev.ID, PayloadJSON: "{}", CreatedAt: time.Now().UTC()}
	if err := dst.W.Create(&cl).Error; err != nil {
		t.Fatal(err)
	}
	if cl.Seq <= 2 {
		t.Errorf("change_log seq restarted: got %d, want > 2", cl.Seq)
	}

	// A second import into the now non-empty database is refused.
	err := Import(ctx, dst, dstDir, dstBlobs, bytes.NewReader(a), int64(len(a)))
	if !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("want ErrNotEmpty, got %v", err)
	}
}

func pageSizeForTest(t *testing.T, n int) {
	t.Helper()
	old := pageSize
	pageSize = n
	t.Cleanup(func() { pageSize = old })
}

// craft builds a minimal archive with the given extra entries.
type entry struct {
	name string
	body []byte
	mode fs.FileMode
}

func craft(t *testing.T, files []File, extra ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range extra {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(e.body)
	}
	m := Manifest{Format: FormatVersion, Dialect: "sqlite", Files: files, RowCounts: map[string]int64{}}
	b, _ := json.Marshal(m)
	w, _ := zw.Create(manifestName)
	w.Write(b)
	zw.Close()
	return buf.Bytes()
}

func TestImportRejectsHostileArchives(t *testing.T) {
	good := []byte("blob")
	gsha := sha(good)
	cases := map[string][]byte{
		"traversal":       craft(t, nil, entry{name: "../evil", body: good}),
		"nested-dotdot":   craft(t, nil, entry{name: "attachments/../../evil", body: good}),
		"absolute":        craft(t, nil, entry{name: "/etc/passwd", body: good}),
		"backslash":       craft(t, nil, entry{name: `attachments\..\evil`, body: good}),
		"symlink":         craft(t, []File{{Path: "attachments/aa/link", Size: 4, SHA256: gsha}}, entry{name: "attachments/aa/link", body: []byte("/etc"), mode: fs.ModeSymlink | 0o777}),
		"unlisted":        craft(t, nil, entry{name: "attachments/aa/" + gsha, body: good}),
		"unknown-entry":   craft(t, nil, entry{name: "evil.sh", body: good}),
		"unknown-table":   craft(t, nil, entry{name: "tables/hackers.jsonl", body: []byte("{}\n")}),
		"bad-sha":         craft(t, []File{{Path: "attachments/" + gsha[:2] + "/" + gsha, Size: 4, SHA256: gsha}}, entry{name: "attachments/" + gsha[:2] + "/" + gsha, body: []byte("evil")}),
		"name-mismatch":   craft(t, []File{{Path: "attachments/aa/" + strings.Repeat("a", 64), Size: 4, SHA256: gsha}}, entry{name: "attachments/aa/" + strings.Repeat("a", 64), body: good}),
		"missing-listed":  craft(t, []File{{Path: "attachments/aa/x", Size: 4, SHA256: gsha}}),
		"unknown-column":  craft(t, nil, entry{name: "tables/tags.jsonl", body: []byte(`{"id":"x","evil":1}` + "\n")}),
		"too-big-for-cap": craft(t, nil, entry{name: "tables/tags.jsonl", body: bytes.Repeat([]byte(" "), 2<<20)}),
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			db := storetest.Open(t)
			dir := t.TempDir()
			lim := DefaultLimits
			lim.MaxEntryBytes = 1 << 20
			err := ImportWithLimits(context.Background(), db, dir, fsBlobs(t, dir), bytes.NewReader(a), int64(len(a)), lim)
			if err == nil {
				t.Fatal("expected an error")
			}
			t.Log(err)
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
				t.Fatal("file written outside dataDir")
			}
			var n int64
			db.R.Model(&store.Tag{}).Count(&n)
			if n != 0 {
				t.Fatal("rows committed despite error")
			}
		})
	}
}

func TestImportGoodBlob(t *testing.T) {
	good := []byte("blob")
	gsha := sha(good)
	p := "attachments/" + gsha[:2] + "/" + gsha
	a := craft(t, []File{{Path: p, Size: 4, SHA256: gsha}}, entry{name: p, body: good})
	db := storetest.Open(t)
	dir := t.TempDir()
	if err := Import(context.Background(), db, dir, fsBlobs(t, dir), bytes.NewReader(a), int64(len(a))); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil || !bytes.Equal(b, good) {
		t.Fatalf("blob not restored: %v", err)
	}
	// No temp files left behind.
	ents, _ := os.ReadDir(filepath.Join(dir, "attachments", gsha[:2]))
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
}

// Archives from older versions carry DATA_DIR files (drop-in themes) under
// files/; they are still restored there.
func TestImportLegacyFiles(t *testing.T) {
	theme := []byte(`{"id":"nord"}`)
	p := "files/themes/nord.json"
	a := craft(t, []File{{Path: p, Size: int64(len(theme)), SHA256: sha(theme)}}, entry{name: p, body: theme})
	dir := t.TempDir()
	if err := Import(context.Background(), storetest.Open(t), dir, fsBlobs(t, dir), bytes.NewReader(a), int64(len(a))); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "themes", "nord.json")); err != nil || !bytes.Equal(b, theme) {
		t.Fatalf("legacy theme not restored: %v", err)
	}
}

func TestCappedReaderDetectsOverflow(t *testing.T) {
	c := &capped{rc: io.NopCloser(strings.NewReader("abcdef")), left: 3, name: "x"}
	_, err := io.ReadAll(c)
	if err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestCheckName(t *testing.T) {
	for name, ok := range map[string]bool{
		"attachments/ab/abc":  true,
		"tables/users.jsonl":  true,
		"../x":                false,
		"a/../../x":           false,
		"/abs":                false,
		"a//b":                false,
		"./a":                 false,
		"C:/x":                false,
		"a\\b":                false,
		"a/b\x00":             false,
		"":                    false,
		"files/themes/x.json": true,
	} {
		if err := checkName(name); (err == nil) != ok {
			t.Errorf("checkName(%q) = %v", name, err)
		}
	}
}
