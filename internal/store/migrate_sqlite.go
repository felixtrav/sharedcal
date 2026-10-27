package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/golang-migrate/migrate/v4/database"
)

// sqliteMigrateDriver is a golang-migrate database.Driver for the pure-Go
// SQLite driver used by GORM. golang-migrate's bundled sqlite driver imports
// modernc.org/sqlite, which registers the same database/sql driver name as
// glebarez/go-sqlite and cannot be linked into the same binary.
//
// Each migration runs in one transaction together with the version update,
// so a failed migration leaves the schema untouched.
type sqliteMigrateDriver struct {
	db *sql.DB
	mu sync.Mutex
	// locked guards against concurrent runs inside this process; SQLite's own
	// write lock (BEGIN IMMEDIATE) serializes processes.
	locked bool
}

const sqliteMigrationsTable = "schema_migrations"

func newSQLiteMigrateDriver(db *sql.DB) (database.Driver, error) {
	d := &sqliteMigrateDriver{db: db}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS ` + sqliteMigrationsTable + ` (version INTEGER NOT NULL PRIMARY KEY, dirty INTEGER NOT NULL)`)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", sqliteMigrationsTable, err)
	}
	return d, nil
}

func (d *sqliteMigrateDriver) Open(string) (database.Driver, error) {
	return nil, errors.New("sqlite migrate driver: use WithInstance")
}

func (d *sqliteMigrateDriver) Close() error { return d.db.Close() }

func (d *sqliteMigrateDriver) Lock() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.locked {
		return database.ErrLocked
	}
	d.locked = true
	return nil
}

func (d *sqliteMigrateDriver) Unlock() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.locked {
		return database.ErrNotLocked
	}
	d.locked = false
	return nil
}

func (d *sqliteMigrateDriver) Run(migration io.Reader) error {
	body, err := io.ReadAll(migration)
	if err != nil {
		return err
	}
	tx, err := d.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(string(body)); err != nil {
		_ = tx.Rollback()
		return database.Error{OrigErr: err, Err: "migration failed", Query: body}
	}
	return tx.Commit()
}

func (d *sqliteMigrateDriver) SetVersion(version int, dirty bool) error {
	tx, err := d.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM ` + sqliteMigrationsTable); err != nil {
		_ = tx.Rollback()
		return err
	}
	if version >= 0 || (version == database.NilVersion && dirty) {
		if _, err := tx.Exec(`INSERT INTO `+sqliteMigrationsTable+` (version, dirty) VALUES (?, ?)`, version, dirty); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (d *sqliteMigrateDriver) Version() (int, bool, error) {
	var v int
	var dirty bool
	err := d.db.QueryRow(`SELECT version, dirty FROM `+sqliteMigrationsTable+` LIMIT 1`).Scan(&v, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return database.NilVersion, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, dirty, nil
}

func (d *sqliteMigrateDriver) Drop() error {
	rows, err := d.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		names = append(names, n)
	}
	rows.Close()
	if _, err := d.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	for _, n := range names {
		if _, err := d.db.Exec(`DROP TABLE IF EXISTS "` + strings.ReplaceAll(n, `"`, `""`) + `"`); err != nil {
			return err
		}
	}
	_, err = d.db.Exec(`PRAGMA foreign_keys = ON`)
	return err
}
