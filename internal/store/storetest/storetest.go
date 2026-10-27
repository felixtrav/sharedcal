// Package storetest opens a fresh, migrated database for tests.
//
// The dialect comes from SHAREDCAL_TEST_DB (sqlite, postgres or mysql;
// default sqlite). For postgres and mysql, SHAREDCAL_TEST_DSN may point at a
// running server (as CI service containers do); otherwise a container is
// started with testcontainers-go. Every test gets its own database.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/store"
)

// Dialect returns the dialect selected for this test run.
func Dialect() string {
	if d := strings.ToLower(os.Getenv("SHAREDCAL_TEST_DB")); d != "" {
		return d
	}
	return store.SQLite
}

var (
	serverOnce sync.Once
	serverDSN  string
	serverErr  error
)

// Open returns a migrated database that is closed (and for servers dropped)
// when the test ends.
func Open(t testing.TB) *store.DB {
	t.Helper()
	ctx := context.Background()
	dialect := Dialect()
	var dsn string
	var cleanup func()
	switch dialect {
	case store.SQLite:
		dsn = filepath.Join(t.TempDir(), "test.db")
		cleanup = func() {}
	case store.Postgres, store.MySQL:
		admin := adminDSN(t, dialect)
		name := "t_" + randHex(8)
		var err error
		dsn, cleanup, err = createDatabase(admin, dialect, name)
		if err != nil {
			t.Fatalf("create test database: %v", err)
		}
	default:
		t.Fatalf("unknown SHAREDCAL_TEST_DB %q", dialect)
	}
	if err := store.Migrate(ctx, dialect, dsn, nil); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db, err := store.Open(dialect, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		cleanup()
	})
	return db
}

// adminDSN returns a DSN for a server where test databases can be created.
func adminDSN(t testing.TB, dialect string) string {
	t.Helper()
	if dsn := os.Getenv("SHAREDCAL_TEST_DSN"); dsn != "" {
		return dsn
	}
	serverOnce.Do(func() { serverDSN, serverErr = startContainer(dialect) })
	if serverErr != nil {
		t.Skipf("no %s available (set SHAREDCAL_TEST_DSN or run Docker): %v", dialect, serverErr)
	}
	return serverDSN
}

func startContainer(dialect string) (string, error) {
	ctx := context.Background()
	switch dialect {
	case store.Postgres:
		c, err := tcpostgres.Run(ctx, "postgres:16-alpine",
			tcpostgres.WithDatabase("sharedcal"), tcpostgres.WithUsername("sharedcal"), tcpostgres.WithPassword("sharedcal"),
			testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(90*time.Second)))
		if err != nil {
			return "", err
		}
		return c.ConnectionString(ctx, "sslmode=disable")
	case store.MySQL:
		c, err := tcmysql.Run(ctx, "mysql:8.4",
			tcmysql.WithDatabase("sharedcal"), tcmysql.WithUsername("root"), tcmysql.WithPassword("sharedcal"))
		if err != nil {
			return "", err
		}
		return c.ConnectionString(ctx)
	}
	return "", fmt.Errorf("no container for %s", dialect)
}

// createDatabase creates a uniquely named database on the server and returns
// a DSN for it plus a cleanup that drops it.
func createDatabase(admin, dialect, name string) (string, func(), error) {
	adminDB, err := store.Open(dialect, admin)
	if err != nil {
		return "", nil, err
	}
	exec := func(g *gorm.DB, q string) error { return g.Exec(q).Error }
	if err := exec(adminDB.W, "CREATE DATABASE "+name); err != nil {
		adminDB.Close()
		return "", nil, err
	}
	dsn, err := withDatabase(admin, dialect, name)
	if err != nil {
		adminDB.Close()
		return "", nil, err
	}
	cleanup := func() {
		if dialect == store.Postgres {
			_ = exec(adminDB.W, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		} else {
			_ = exec(adminDB.W, "DROP DATABASE IF EXISTS "+name)
		}
		adminDB.Close()
	}
	return dsn, cleanup, nil
}

func withDatabase(dsn, dialect, name string) (string, error) {
	if dialect == store.Postgres {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", err
		}
		u.Path = "/" + name
		return u.String(), nil
	}
	// MySQL DSN: user:pass@tcp(host:port)/db?params
	base, query, _ := strings.Cut(dsn, "?")
	i := strings.LastIndex(base, "/")
	if i < 0 {
		return "", fmt.Errorf("unrecognized mysql DSN")
	}
	out := base[:i+1] + name
	if query != "" {
		out += "?" + query
	}
	return out, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
