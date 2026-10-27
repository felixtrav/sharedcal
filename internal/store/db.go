package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/felixtrav/sharedcal/migrations"
)

// Dialect names.
const (
	SQLite   = "sqlite"
	Postgres = "postgres"
	MySQL    = "mysql"
)

// DB bundles the connection pools. For SQLite, W is a single writer
// connection and R a read-only pool; for other databases they are the same.
type DB struct {
	W       *gorm.DB
	R       *gorm.DB
	Dialect string
}

// Close releases every pool.
func (d *DB) Close() error {
	var errs []error
	seen := map[*sql.DB]bool{}
	for _, g := range []*gorm.DB{d.W, d.R} {
		if g == nil {
			continue
		}
		s, err := g.DB()
		if err != nil || seen[s] {
			continue
		}
		seen[s] = true
		errs = append(errs, s.Close())
	}
	return errors.Join(errs...)
}

func nowUTC() time.Time { return time.Now().UTC() }

func gormConfig() *gorm.Config {
	return &gorm.Config{
		NowFunc:                nowUTC,
		Logger:                 logger.Discard,
		SkipDefaultTransaction: true,
		TranslateError:         true,
		PrepareStmt:            false,
	}
}

// Open connects to the configured database. It does not migrate.
func Open(driver, dsn string) (*DB, error) {
	switch driver {
	case SQLite:
		return openSQLite(dsn)
	case Postgres:
		conn, err := openPostgres(dsn)
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		g, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), gormConfig())
		if err != nil {
			return nil, fmt.Errorf("open postgres: %w", err)
		}
		if err := tunePool(g, 20); err != nil {
			return nil, err
		}
		return &DB{W: g, R: g, Dialect: Postgres}, nil
	case MySQL:
		g, err := gorm.Open(mysql.Open(normalizeMySQLDSN(dsn)), gormConfig())
		if err != nil {
			return nil, fmt.Errorf("open mysql: %w", err)
		}
		if err := tunePool(g, 20); err != nil {
			return nil, err
		}
		return &DB{W: g, R: g, Dialect: MySQL}, nil
	}
	return nil, fmt.Errorf("unsupported database driver %q", driver)
}

// openPostgres opens a pgx pool whose sessions run in UTC and whose
// timestamptz values scan as UTC. pgx otherwise returns them in time.Local,
// so the API would emit offsets that depend on the server's TZ.
func openPostgres(dsn string) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.RuntimeParams["timezone"] = "UTC"
	return stdlib.OpenDB(*cfg, stdlib.OptionAfterConnect(func(_ context.Context, c *pgx.Conn) error {
		c.TypeMap().RegisterType(&pgtype.Type{Name: "timestamptz", OID: pgtype.TimestamptzOID, Codec: &pgtype.TimestamptzCodec{ScanLocation: time.UTC}})
		return nil
	})), nil
}

func tunePool(g *gorm.DB, n int) error {
	s, err := g.DB()
	if err != nil {
		return err
	}
	s.SetMaxOpenConns(n)
	s.SetMaxIdleConns(n)
	s.SetConnMaxLifetime(30 * time.Minute)
	return nil
}

// normalizeMySQLDSN forces the options the app depends on: parsed UTC times
// and utf8mb4.
func normalizeMySQLDSN(dsn string) string {
	base, query, _ := strings.Cut(dsn, "?")
	v, _ := url.ParseQuery(query)
	v.Set("parseTime", "true")
	v.Set("loc", "UTC")
	v.Set("time_zone", "'+00:00'")
	if v.Get("charset") == "" {
		v.Set("charset", "utf8mb4")
	}
	return base + "?" + v.Encode()
}

func sqliteDSN(path string, readOnly bool) string {
	v := url.Values{}
	v.Add("_pragma", "busy_timeout(5000)")
	v.Add("_pragma", "foreign_keys(1)")
	v.Add("_pragma", "journal_mode(WAL)")
	v.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		v.Add("_pragma", "query_only(1)")
	} else {
		v.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + v.Encode()
}

func sqlitePath(dsn string) (string, error) {
	path := strings.TrimPrefix(dsn, "file:")
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == ":memory:" {
		return "", errors.New("sqlite: a file path is required (in-memory databases cannot be shared between pools)")
	}
	return path, nil
}

func openSQLite(dsn string) (*DB, error) {
	path, err := sqlitePath(dsn)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("sqlite: create directory: %w", err)
	}
	w, err := gorm.Open(sqlite.Open(sqliteDSN(path, false)), gormConfig())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := tunePool(w, 1); err != nil {
		return nil, err
	}
	// Create the file and switch it to WAL before readers attach.
	if err := w.Exec("SELECT 1").Error; err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	r, err := gorm.Open(sqlite.Open(sqliteDSN(path, true)), gormConfig())
	if err != nil {
		return nil, fmt.Errorf("open sqlite (read pool): %w", err)
	}
	if err := tunePool(r, 8); err != nil {
		return nil, err
	}
	return &DB{W: w, R: r, Dialect: SQLite}, nil
}

// Migrate applies every pending migration for the given database, holding
// the driver's migration lock (advisory lock on PostgreSQL, GET_LOCK on
// MySQL). It uses its own short-lived connection because golang-migrate
// closes the database handle it is given.
func Migrate(ctx context.Context, driver, dsn string, log *slog.Logger) error {
	m, err := newMigrator(driver, dsn)
	if err != nil {
		return err
	}
	defer m.Close()
	before, _, _ := m.Version()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: %w", err)
	}
	after, dirty, err := m.Version()
	if err != nil {
		return fmt.Errorf("migrate: read version: %w", err)
	}
	if dirty {
		return fmt.Errorf("migrate: database is dirty at version %d; fix manually", after)
	}
	if log != nil && after != before {
		log.InfoContext(ctx, "database migrated", "from", before, "to", after, "dialect", driver)
	}
	return nil
}

func newMigrator(driver, dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, driver)
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	var sqlDB *sql.DB
	switch driver {
	case SQLite:
		path, perr := sqlitePath(dsn)
		if perr != nil {
			return nil, perr
		}
		sqlDB, err = sql.Open("sqlite", sqliteDSN(path, false))
	case Postgres:
		sqlDB, err = sql.Open("pgx", dsn)
	case MySQL:
		// Migration files hold several statements; enable that only here.
		sqlDB, err = sql.Open("mysql", normalizeMySQLDSN(dsn)+"&multiStatements=true")
	default:
		err = fmt.Errorf("unsupported dialect %q", driver)
	}
	if err != nil {
		return nil, fmt.Errorf("migrate: open: %w", err)
	}
	var drv database.Driver
	switch driver {
	case SQLite:
		sqlDB.SetMaxOpenConns(1)
		drv, err = newSQLiteMigrateDriver(sqlDB)
	case Postgres:
		drv, err = migratepgx.WithInstance(sqlDB, &migratepgx.Config{MultiStatementEnabled: true})
	case MySQL:
		drv, err = migratemysql.WithInstance(sqlDB, &migratemysql.Config{})
	}
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migrate driver: %w", err)
	}
	return migrate.NewWithInstance("iofs", src, driver, drv)
}

// Ping checks that the database answers.
func (d *DB) Ping(ctx context.Context) error {
	s, err := d.R.DB()
	if err != nil {
		return err
	}
	return s.PingContext(ctx)
}
