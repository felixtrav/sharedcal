package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm/schema"

	"github.com/felixtrav/sharedcal/internal/store"
	"github.com/felixtrav/sharedcal/internal/store/storetest"
)

// TestModelsMatchMigrations ensures every GORM model field has a column in
// the migrated schema and the schema has no columns the models don't know.
func TestModelsMatchMigrations(t *testing.T) {
	db := storetest.Open(t)
	m := db.W.Migrator()
	for _, model := range store.AllModels() {
		s, err := schema.Parse(model, &sync.Map{}, db.W.NamingStrategy)
		if err != nil {
			t.Fatal(err)
		}
		if !m.HasTable(model) {
			t.Errorf("table %s missing", s.Table)
			continue
		}
		cols, err := m.ColumnTypes(model)
		if err != nil {
			t.Fatal(err)
		}
		inDB := map[string]bool{}
		for _, c := range cols {
			inDB[c.Name()] = true
		}
		for _, f := range s.Fields {
			if f.DBName == "" {
				continue
			}
			if !inDB[f.DBName] {
				t.Errorf("%s.%s: column missing from migrations", s.Table, f.DBName)
			}
			delete(inDB, f.DBName)
		}
		for c := range inDB {
			t.Errorf("%s.%s: column not mapped by model", s.Table, c)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	// storetest.Open migrated once already; a second run must be a no-op.
	dir := t.TempDir() + "/x.db"
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := store.Migrate(ctx, store.SQLite, dir, nil); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
}

func TestTimeRoundTripAndOrdering(t *testing.T) {
	db := storetest.Open(t)
	ts := time.Date(2026, 10, 30, 23, 0, 0, 0, time.UTC)
	u := store.User{ID: store.NewID(), OIDCIssuer: "i", OIDCSubject: "s", Role: "viewer", RoleSource: "idp", GroupsJSON: "[]", Active: true}
	if err := db.W.Create(&u).Error; err != nil {
		t.Fatal(err)
	}
	for i, start := range []time.Time{ts, ts.Add(-90 * time.Minute), ts.Add(36 * time.Hour)} {
		e := store.Event{ID: store.NewID(), OwnerID: u.ID, Title: "e", StartAt: start, EndAt: start.Add(time.Hour), TimeZone: "UTC", ExdatesJSON: "[]", Status: "confirmed", Version: 1}
		if err := db.W.Create(&e).Error; err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	var got []store.Event
	if err := db.R.Where("start_at >= ? AND start_at < ?", ts.Add(-2*time.Hour), ts.Add(time.Hour)).Order("start_at").Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].StartAt.Equal(ts.Add(-90*time.Minute)) || !got[1].StartAt.Equal(ts) {
		t.Fatalf("range query wrong: %+v", got)
	}
	// Not just a zero offset: the location must be time.UTC, or JSON and
	// iCalendar output would depend on the server's TZ.
	if loc := got[1].StartAt.Location(); loc != time.UTC {
		t.Fatalf("time not in time.UTC: %v (location %q)", got[1].StartAt, loc)
	}
}

func TestCheckConstraints(t *testing.T) {
	db := storetest.Open(t)
	u := store.User{ID: store.NewID(), OIDCIssuer: "i", OIDCSubject: "s", Role: "superuser", RoleSource: "idp", GroupsJSON: "[]", Active: true}
	if err := db.W.Create(&u).Error; err == nil {
		t.Fatal("invalid role accepted")
	}
}

func TestOIDCSubjectIsCaseSensitive(t *testing.T) {
	db := storetest.Open(t)
	for _, sub := range []string{"abc", "ABC"} {
		u := store.User{ID: store.NewID(), OIDCIssuer: "i", OIDCSubject: sub, Role: "viewer", RoleSource: "idp", GroupsJSON: "[]", Active: true}
		if err := db.W.Create(&u).Error; err != nil {
			t.Fatalf("subject %q: %v", sub, err)
		}
	}
	var n int64
	db.R.Model(&store.User{}).Where("oidc_subject = ?", "abc").Count(&n)
	if n != 1 {
		t.Fatalf("subject lookup matched %d rows", n)
	}
}
