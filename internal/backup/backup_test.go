package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite" // registers "sqlite" for reading the snapshot

	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func seedUser(t *testing.T, db *store.DB, id string) {
	t.Helper()
	ts := time.Now().UTC()
	u := store.User{ID: id, OIDCIssuer: "https://id.example.com", OIDCSubject: id, Role: "admin",
		RoleSource: store.RoleSourceIDP, GroupsJSON: "[]", Active: true, CreatedAt: ts, UpdatedAt: ts}
	if err := db.W.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
}

func setClock(t *testing.T, ts time.Time) {
	t.Helper()
	old := now
	now = func() time.Time { return ts }
	t.Cleanup(func() { now = old })
}

func TestRunIncrementalAndPrune(t *testing.T) {
	db := storetest.Open(t)
	ctx := context.Background()
	dataDir := t.TempDir()
	outDir := filepath.Join(t.TempDir(), "backups")

	blobA := filepath.Join("ab", strings.Repeat("ab", 32))
	blobB := filepath.Join("cd", strings.Repeat("cd", 32))
	writeFile(t, filepath.Join(dataDir, "attachments", blobA), "blob a")
	writeFile(t, filepath.Join(dataDir, "attachments", ".tmp", "upload-1"), "partial")
	writeFile(t, filepath.Join(dataDir, "themes", "nord.json"), `{"id":"nord"}`)
	writeFile(t, filepath.Join(dataDir, "branding", "logo.png"), "png")
	if db.Dialect == store.SQLite {
		seedUser(t, db, "u1")
	}

	setClock(t, time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC))
	first, err := Run(ctx, db, dataDir, outDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "sharedcal-20261001T030000Z" {
		t.Fatalf("unexpected name %s", first)
	}
	for _, p := range []string{blobA, filepath.Join("..", "themes", "nord.json"), filepath.Join("..", "branding", "logo.png")} {
		if _, err := os.Stat(filepath.Join(first, "attachments", p)); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(first, "attachments", ".tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp upload directory should be skipped")
	}

	if db.Dialect == store.SQLite {
		// The snapshot is a standalone, readable database with the seeded row.
		snap, err := sql.Open("sqlite", filepath.Join(first, DBFile))
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err := snap.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil {
			t.Fatal(err)
		}
		snap.Close()
		if n != 1 {
			t.Fatalf("snapshot has %d users, want 1", n)
		}
	} else if _, err := os.Stat(filepath.Join(first, DBFile)); !os.IsNotExist(err) {
		t.Fatalf("non-sqlite backup must not contain a database file")
	}

	// Second run: blob A is unchanged and should be hard-linked; B is new.
	writeFile(t, filepath.Join(dataDir, "attachments", blobB), "blob b")
	setClock(t, time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC))
	second, err := Run(ctx, db, dataDir, outDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ia, _ := os.Stat(filepath.Join(first, "attachments", blobA))
	ib, _ := os.Stat(filepath.Join(second, "attachments", blobA))
	if !os.SameFile(ia, ib) {
		t.Errorf("unchanged blob was copied, not hard-linked")
	}
	if st, ok := ib.Sys().(*syscall.Stat_t); ok && st.Nlink < 2 {
		t.Errorf("expected link count >= 2, got %d", st.Nlink)
	}
	if b, err := os.ReadFile(filepath.Join(second, "attachments", blobB)); err != nil || string(b) != "blob b" {
		t.Fatalf("new blob missing: %v", err)
	}

	// Same-second run gets a suffix and still sorts newest.
	third, err := Run(ctx, db, dataDir, outDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(third) != "sharedcal-20261002T030000Z-1" {
		t.Fatalf("unexpected name %s", third)
	}

	// Unrelated directories are never pruned.
	if err := os.MkdirAll(filepath.Join(outDir, "keep-me"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := Prune(outDir, 2); err != nil {
		t.Fatal(err)
	}
	l, err := List(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(l) != 2 || l[0] != third || l[1] != second {
		t.Fatalf("after prune: %v", l)
	}
	if _, err := os.Stat(filepath.Join(outDir, "keep-me")); err != nil {
		t.Fatal("prune removed an unrelated directory")
	}
	// The linked blob survives deletion of the backup it was first copied into.
	if b, err := os.ReadFile(filepath.Join(second, "attachments", blobA)); err != nil || string(b) != "blob a" {
		t.Fatalf("linked blob lost after prune: %v", err)
	}
}

func TestRunWithoutAttachments(t *testing.T) {
	db := storetest.Open(t)
	p, err := Run(context.Background(), db, t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p, ManifestFile)); err != nil {
		t.Fatal(err)
	}
}

func TestRefuseNestedOutput(t *testing.T) {
	dataDir := t.TempDir()
	_, err := Run(context.Background(), nil, dataDir, filepath.Join(dataDir, "attachments", "bk"), nil)
	if err == nil {
		t.Fatal("expected error for output inside attachments")
	}
	// /data/backups (sibling of attachments) is fine.
	if _, err := Run(context.Background(), nil, dataDir, filepath.Join(dataDir, "backups"), nil); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotPathWithQuote(t *testing.T) {
	db := storetest.Open(t)
	if db.Dialect != store.SQLite {
		t.Skip("sqlite only")
	}
	dir := filepath.Join(t.TempDir(), "it's; DROP TABLE users; --")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "x.db")
	if err := snapshotSQLite(context.Background(), db, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatal(err)
	}
}

func TestIsBackupName(t *testing.T) {
	for name, want := range map[string]bool{
		"sharedcal-20261001T030000Z":    true,
		"sharedcal-20261001T030000Z-12": true,
		"sharedcal-20261001T030000Z-":   false,
		"sharedcal-20261001T030000Zx":   false,
		"sharedcal-latest":              false,
		".tmp-sharedcal-123":            false,
	} {
		if got := isBackupName(name); got != want {
			t.Errorf("isBackupName(%q) = %v", name, got)
		}
	}
}

func TestSchedule(t *testing.T) {
	if err := Schedule(context.Background(), "not a cron", func(context.Context) {}); err == nil {
		t.Fatal("expected parse error")
	}
	if _, err := ParseSchedule("0 3 * * *"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	done := make(chan struct{})
	if err := Schedule(ctx, "@every 1s", func(context.Context) {
		if runs.Add(1) == 2 {
			close(done)
		}
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("schedule did not fire")
	}
	cancel()
	n := runs.Load()
	time.Sleep(1500 * time.Millisecond)
	if runs.Load() > n {
		t.Fatal("schedule kept running after cancel")
	}
}
