// Package archive implements SharedCal's portable export/import format, used
// to move an instance between databases (e.g. SQLite to PostgreSQL) or hosts.
//
// An archive is a zip file:
//
//	manifest.json                 format version, app version, source dialect,
//	                              creation time, row counts, file checksums
//	tables/<table>.jsonl          one JSON object per row, keyed by column name,
//	                              ordered by primary key, in store.AllModels() order
//	attachments/<prefix>/<sha>    every blob the database references (attachments,
//	                              thumbnails, the logo), read through the BlobStore
//	files/themes/…, files/branding/…  only in archives from older versions, which
//	                              exported DATA_DIR files; import still restores them
//
// Values are dialect-neutral JSON: strings, numbers, booleans, null, and
// RFC 3339 UTC timestamps. Two tables are deliberately left out:
//
//   - sessions: they are bearer credentials (hashed, but still), and moving an
//     instance is a good moment for everyone to sign in again;
//   - change_log: it is the SSE replay buffer, pruned after 24 hours anyway.
//     Only its high-water mark is kept (manifest changeLogMaxSeq) so the
//     imported instance continues numbering after it: browsers reconnecting
//     with an old Last-Event-ID then simply get nothing (or a resync) instead
//     of silently missing new changes that reused old sequence numbers.
package archive

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/store"
)

// FormatVersion is the archive layout version written to manifest.json.
// Import accepts archives with a format version up to this one.
const FormatVersion = 1

// AppVersion is recorded in exported manifests. cmd/sharedcal sets it from
// its build version.
var AppVersion = "dev"

const (
	manifestName = "manifest.json"
	tablesDir    = "tables/"
	attachDir    = "attachments/"
	filesDir     = "files/"
	batchSize    = 200
)

// pageSize is the number of rows read per query during export (a variable so
// tests can force paging).
var pageSize = 500

// skipped tables are never exported (see the package documentation).
var skipped = map[string]bool{"sessions": true, "change_log": true}

// extraDirs are DATA_DIR subdirectories that older versions exported under
// files/. Themes now live in the settings table, so only Import uses this.
var extraDirs = []string{"themes", "branding"}

// Manifest is the archive's manifest.json.
type Manifest struct {
	Format          int              `json:"format"`
	AppVersion      string           `json:"appVersion"`
	Dialect         string           `json:"dialect"`
	CreatedAt       time.Time        `json:"createdAt"`
	Tables          []string         `json:"tables"` // in insertion order
	RowCounts       map[string]int64 `json:"rowCounts"`
	Skipped         []string         `json:"skipped"`
	ChangeLogMaxSeq int64            `json:"changeLogMaxSeq"`
	Files           []File           `json:"files"`
}

// File describes one non-table entry (blob, theme, logo).
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// table is a parsed model.
type table struct {
	model  any
	schema *schema.Schema
}

var cache sync.Map

func tables(db *gorm.DB) ([]table, error) {
	var out []table
	for _, m := range store.AllModels() {
		s, err := schema.Parse(m, &cache, db.NamingStrategy)
		if err != nil {
			return nil, fmt.Errorf("archive: parse model %T: %w", m, err)
		}
		out = append(out, table{model: m, schema: s})
	}
	return out, nil
}

// Export writes a portable archive of the database and the blobs it
// references to w. Rows are read in pages inside one read transaction, so the
// archive is a consistent snapshot and whole tables are never held in memory.
func Export(ctx context.Context, db *store.DB, blobs attachments.BlobStore, w io.Writer) error {
	tbls, err := tables(db.R)
	if err != nil {
		return err
	}
	m := Manifest{
		Format: FormatVersion, AppVersion: AppVersion, Dialect: db.Dialect,
		CreatedAt: time.Now().UTC(), RowCounts: map[string]int64{},
	}
	zw := zip.NewWriter(w)

	var opts *sql.TxOptions
	if db.Dialect != store.SQLite { // SQLite read transactions are snapshots already
		opts = &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}
	}
	tx := db.R.WithContext(ctx).Begin(opts)
	if tx.Error != nil {
		return fmt.Errorf("archive: begin: %w", tx.Error)
	}
	defer tx.Rollback()

	for _, t := range tbls {
		name := t.schema.Table
		if skipped[name] {
			m.Skipped = append(m.Skipped, name)
			continue
		}
		fw, err := zw.CreateHeader(&zip.FileHeader{Name: tablesDir + name + ".jsonl", Method: zip.Deflate, Modified: m.CreatedAt})
		if err != nil {
			return err
		}
		n, err := exportTable(ctx, tx, t.schema, fw)
		if err != nil {
			return fmt.Errorf("archive: export %s: %w", name, err)
		}
		m.Tables = append(m.Tables, name)
		m.RowCounts[name] = n
	}
	var maxSeq sql.NullInt64
	if err := tx.Raw("SELECT MAX(seq) FROM change_log").Scan(&maxSeq).Error; err != nil {
		return fmt.Errorf("archive: read change_log: %w", err)
	}
	m.ChangeLogMaxSeq = maxSeq.Int64
	keys, err := blobKeys(tx)
	if err != nil {
		return fmt.Errorf("archive: list blobs: %w", err)
	}
	_ = tx.Rollback()

	// Blobs are content-addressed and immutable, so reading them after the
	// row snapshot cannot produce a row that points at a missing blob (blobs
	// are only removed after their rows).
	for _, k := range keys {
		if err := exportBlob(ctx, zw, blobs, k, &m); err != nil {
			return err
		}
	}

	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: manifestName, Method: zip.Deflate, Modified: m.CreatedAt})
	if err != nil {
		return err
	}
	if _, err := fw.Write(append(b, '\n')); err != nil {
		return err
	}
	return zw.Close()
}

// exportTable streams one table as JSON lines, paging by primary key
// (keyset pagination, so each page is an index range scan).
func exportTable(ctx context.Context, tx *gorm.DB, s *schema.Schema, w io.Writer) (int64, error) {
	pks := s.PrimaryFields
	if len(pks) == 0 {
		return 0, fmt.Errorf("table %s has no primary key", s.Table)
	}
	order := make([]string, len(pks))
	for i, f := range pks {
		order[i] = quote(tx, f.DBName)
	}
	var last []any
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		page := reflect.New(reflect.SliceOf(s.ModelType))
		q := tx.Table(s.Table).Order(strings.Join(order, ", ")).Limit(pageSize)
		if last != nil {
			cond, args := after(order, last)
			q = q.Where(cond, args...)
		}
		if err := q.Find(page.Interface()).Error; err != nil {
			return n, err
		}
		rows := page.Elem()
		for i := 0; i < rows.Len(); i++ {
			rv := rows.Index(i)
			line, err := encodeRow(ctx, s, rv)
			if err != nil {
				return n, err
			}
			if _, err := w.Write(line); err != nil {
				return n, err
			}
			n++
		}
		if rows.Len() < pageSize {
			return n, nil
		}
		lastRow := rows.Index(rows.Len() - 1)
		last = make([]any, len(pks))
		for i, f := range pks {
			last[i], _ = f.ValueOf(ctx, lastRow)
		}
	}
}

// after builds the keyset condition (a, b) > (x, y) portably:
// a > x OR (a = x AND b > y).
func after(cols []string, vals []any) (string, []any) {
	var ors []string
	var args []any
	for i := range cols {
		var ands []string
		for j := 0; j < i; j++ {
			ands = append(ands, cols[j]+" = ?")
			args = append(args, vals[j])
		}
		ands = append(ands, cols[i]+" > ?")
		args = append(args, vals[i])
		ors = append(ors, "("+strings.Join(ands, " AND ")+")")
	}
	return strings.Join(ors, " OR "), args
}

func quote(db *gorm.DB, col string) string {
	var b strings.Builder
	db.Dialector.QuoteTo(&b, col)
	return b.String()
}

// encodeRow writes {"column": value, …} in schema field order, with times
// normalized to UTC.
func encodeRow(ctx context.Context, s *schema.Schema, rv reflect.Value) ([]byte, error) {
	buf := []byte{'{'}
	first := true
	for _, f := range s.Fields {
		if f.DBName == "" {
			continue
		}
		v, _ := f.ValueOf(ctx, rv)
		switch t := v.(type) {
		case time.Time:
			v = t.UTC()
		case *time.Time:
			if t != nil {
				u := t.UTC()
				v = &u
			}
		}
		val, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("%s.%s: %w", s.Table, f.DBName, err)
		}
		key, _ := json.Marshal(f.DBName)
		if !first {
			buf = append(buf, ',')
		}
		first = false
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, val...)
	}
	return append(buf, '}', '\n'), nil
}

// blobKeys returns every blob key the database references: attachments,
// their thumbnails, and the branding logo (the logoKey field of the
// "instance" settings row, see internal/domain/settings.go).
func blobKeys(tx *gorm.DB) ([]string, error) {
	var rows []struct {
		StorageKey string
		ThumbKey   *string
	}
	if err := tx.Model(&store.Attachment{}).Select("storage_key", "thumb_key").Find(&rows).Error; err != nil {
		return nil, err
	}
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.StorageKey)
		if r.ThumbKey != nil {
			keys = append(keys, *r.ThumbKey)
		}
	}
	var st store.Setting
	switch err := tx.Where(map[string]any{"key": "instance"}).Limit(1).Find(&st).Error; {
	case err != nil:
		return nil, err
	case st.ValueJSON != "":
		var v struct {
			LogoKey string `json:"logoKey"`
		}
		if json.Unmarshal([]byte(st.ValueJSON), &v) == nil && v.LogoKey != "" {
			keys = append(keys, v.LogoKey)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys), nil
}

// exportBlob adds one blob as attachments/<key>. A blob that is already gone
// (an infected upload, whose row is kept for the audit trail) is skipped.
func exportBlob(ctx context.Context, zw *zip.Writer, blobs attachments.BlobStore, key string, m *Manifest) error {
	if !attachments.ValidKey(key) {
		return fmt.Errorf("archive: refusing to export blob with invalid key %q", key)
	}
	rc, _, err := blobs.Open(ctx, key)
	if errors.Is(err, attachments.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("archive: blob %s: %w", key, err)
	}
	defer rc.Close()
	name := attachDir + key
	fw, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: m.CreatedAt})
	if err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(fw, h), rc)
	if err != nil {
		return fmt.Errorf("archive: blob %s: %w", key, err)
	}
	m.Files = append(m.Files, File{Path: name, Size: size, SHA256: hex.EncodeToString(h.Sum(nil))})
	return nil
}

// checkName accepts only clean, relative, forward-slash paths with no "..",
// backslashes, drive letters or control characters.
func checkName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:") {
		return errors.New("absolute or non-portable path")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return errors.New("control character in path")
		}
	}
	if !fs.ValidPath(name) || path.Clean(name) != name {
		return errors.New("path is not clean or escapes the archive")
	}
	return nil
}
