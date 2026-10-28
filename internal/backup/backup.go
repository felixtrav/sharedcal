// Package backup writes timestamped, self-contained backups of a SharedCal
// data directory: an online snapshot of the SQLite database plus the
// attachment blobs, uploaded themes and branding files.
//
// Each backup is a directory named sharedcal-YYYYMMDDTHHMMSSZ under the
// output directory. Attachment blobs are content-addressed and immutable, so
// blobs already present in the previous backup are hard-linked instead of
// copied: every backup directory is complete on its own (any of them can be
// restored or deleted independently) while unchanged blobs take no extra
// space. Backups are written to a hidden temporary directory and renamed into
// place only when complete, so a crash never leaves a half-written backup
// that later runs would link against.
package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/felixtrav/sharedcal/internal/store"
)

const (
	// DirPrefix starts the name of every backup directory.
	DirPrefix = "sharedcal-"
	// timeLayout is lexically sortable, so name order is chronological.
	timeLayout = "20060102T150405Z"
	tmpPrefix  = ".tmp-" + DirPrefix
	// DBFile is the SQLite snapshot's name inside a backup directory.
	DBFile = "sharedcal.db"
	// ManifestFile describes the backup.
	ManifestFile = "backup.json"
)

// copiedDirs are the DATA_DIR subdirectories included in every backup.
// "attachments" is handled incrementally; the others are small and copied.
var copiedDirs = []string{"themes", "branding"}

// Manifest is written to backup.json in every backup directory.
type Manifest struct {
	CreatedAt   time.Time `json:"createdAt"`
	Dialect     string    `json:"dialect"`
	Database    string    `json:"database"` // DBFile, or "" when not included
	Attachments Stats     `json:"attachments"`
	Note        string    `json:"note,omitempty"`
}

// Stats counts attachment files in a backup.
type Stats struct {
	Files  int   `json:"files"`
	Bytes  int64 `json:"bytes"`
	Linked int   `json:"linked"` // reused from the previous backup via hard link
	Copied int   `json:"copied"`
}

// Options adjust what Run includes.
type Options struct {
	// ExternalAttachments names where attachment blobs live when they are not
	// in dataDir (e.g. "s3://bucket/prefix"). They are then left out: the
	// bucket's own versioning or replication protects them.
	ExternalAttachments string
}

// now is replaceable in tests.
var now = func() time.Time { return time.Now().UTC() }

// Run creates a new backup under outDir and returns its path.
//
// For SQLite the database is snapshotted online with VACUUM INTO (see
// snapshotSQLite). PostgreSQL and MySQL are not dumped: use pg_dump or
// mysqldump, which understand their own consistency and permission models far
// better than the app could. Attachments, themes and branding are backed up
// for every dialect, unless opts says attachments live elsewhere.
func Run(ctx context.Context, db *store.DB, dataDir, outDir string, opts Options, log *slog.Logger) (string, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return "", fmt.Errorf("backup: create output directory: %w", err)
	}
	if err := refuseNested(dataDir, outDir); err != nil {
		return "", err
	}
	prev, err := latest(outDir)
	if err != nil {
		return "", err
	}

	started := now()
	tmp, err := os.MkdirTemp(outDir, tmpPrefix)
	if err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tmp)
		}
	}()

	m := Manifest{CreatedAt: started}
	var notes []string
	if db != nil {
		m.Dialect = db.Dialect
		switch db.Dialect {
		case store.SQLite:
			if err := snapshotSQLite(ctx, db, filepath.Join(tmp, DBFile)); err != nil {
				return "", err
			}
			m.Database = DBFile
		default:
			notes = append(notes, fmt.Sprintf("database (%s) not included: back it up with %s", db.Dialect, nativeTool(db.Dialect)))
			log.WarnContext(ctx, "backup: database not included; use the database's native backup tool",
				"dialect", db.Dialect, "tool", nativeTool(db.Dialect))
		}
	}

	if opts.ExternalAttachments == "" {
		prevAttach := ""
		if prev != "" {
			prevAttach = filepath.Join(prev, "attachments")
		}
		st, err := syncTree(ctx, filepath.Join(dataDir, "attachments"), filepath.Join(tmp, "attachments"), prevAttach)
		if err != nil {
			return "", fmt.Errorf("backup: attachments: %w", err)
		}
		m.Attachments = st
	} else {
		notes = append(notes, "attachments not included: they are stored in "+opts.ExternalAttachments)
	}
	m.Note = strings.Join(notes, "; ")
	for _, d := range copiedDirs {
		if _, err := syncTree(ctx, filepath.Join(dataDir, d), filepath.Join(tmp, d), ""); err != nil {
			return "", fmt.Errorf("backup: %s: %w", d, err)
		}
	}

	if err := writeJSON(filepath.Join(tmp, ManifestFile), m); err != nil {
		return "", err
	}
	final, err := finalName(outDir, started)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", fmt.Errorf("backup: %w", err)
	}
	ok = true
	log.InfoContext(ctx, "backup complete", "path", final, "dialect", m.Dialect,
		"attachments", m.Attachments.Files, "linked", m.Attachments.Linked, "copied", m.Attachments.Copied,
		"duration", time.Since(started).Round(time.Millisecond).String())
	return final, nil
}

func nativeTool(dialect string) string {
	switch dialect {
	case store.Postgres:
		return "pg_dump"
	case store.MySQL:
		return "mysqldump (or mariadb-dump)"
	}
	return "the database's native backup tool"
}

// snapshotSQLite writes a consistent copy of the live database to dst.
//
// Why VACUUM INTO rather than the sqlite3_backup_* API: the driver is pure Go
// (modernc via glebarez), and its backup API is only reachable through
// driver-specific raw connections, which ties this package to driver
// internals. VACUUM INTO is plain SQL, so it works through GORM unchanged. It
// runs inside a single read transaction, so the copy is a consistent
// point-in-time snapshot even while the app keeps serving (in WAL mode readers
// are never blocked); the result is also compacted and defragmented, and is a
// standalone rollback-journal file with no -wal/-shm companions. The cost is
// that it occupies the single writer connection (db.W) for its duration, so
// writes queue briefly; for the small databases SharedCal targets that is
// milliseconds to a few seconds. It always copies the whole file (no
// incremental pages), which is fine at this size.
//
// The destination path is passed as a bound parameter, so no quoting is
// needed and no path can inject SQL.
func snapshotSQLite(ctx context.Context, db *store.DB, dst string) error {
	if strings.ContainsRune(dst, 0) {
		return errors.New("backup: invalid destination path")
	}
	if err := db.W.WithContext(ctx).Exec("VACUUM INTO ?", dst).Error; err != nil {
		return fmt.Errorf("backup: sqlite snapshot: %w", err)
	}
	return nil
}

// refuseNested stops outDir from being inside a directory that is itself
// backed up, which would make every backup contain the previous ones.
func refuseNested(dataDir, outDir string) error {
	absOut, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	for _, d := range append([]string{"attachments"}, copiedDirs...) {
		absD, err := filepath.Abs(filepath.Join(dataDir, d))
		if err != nil {
			return err
		}
		if rel, err := filepath.Rel(absD, absOut); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("backup: output directory %s must not be inside %s", outDir, absD)
		}
	}
	return nil
}

// syncTree copies every regular file under src to dst. When prev is set and
// holds a file with the same relative path, size and modification time, it is
// hard-linked from there instead. A missing src is not an error. Symlinks and
// other special files are skipped: nothing in DATA_DIR should be one.
func syncTree(ctx context.Context, src, dst, prev string) (Stats, error) {
	var st Stats
	if _, err := os.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != src {
				return filepath.SkipDir // e.g. in-progress upload temp dirs
			}
			return os.MkdirAll(target, 0o750)
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		st.Files++
		st.Bytes += info.Size()
		if prev != "" {
			old := filepath.Join(prev, rel)
			if pi, err := os.Lstat(old); err == nil && pi.Mode().IsRegular() &&
				pi.Size() == info.Size() && pi.ModTime().Equal(info.ModTime()) {
				if err := os.Link(old, target); err == nil {
					st.Linked++
					return nil
				}
				// Fall through to a copy (e.g. filesystem without hard links).
			}
		}
		if err := copyFile(path, target, info); err != nil {
			return err
		}
		st.Copied++
		return nil
	})
	return st, err
}

func copyFile(src, dst string, info fs.FileInfo) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	// Keep the source mtime so the next run can recognise the file as unchanged.
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o640)
}

func finalName(outDir string, t time.Time) (string, error) {
	base := filepath.Join(outDir, DirPrefix+t.UTC().Format(timeLayout))
	name := base
	for i := 1; ; i++ {
		if _, err := os.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", err
		}
		if i > 100 {
			return "", errors.New("backup: too many backups in the same second")
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// List returns the complete backups in outDir, newest first.
func List(outDir string) ([]string, error) {
	entries, err := os.ReadDir(outDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("backup: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && isBackupName(e.Name()) {
			names = append(names, e.Name())
		}
	}
	// Same-second suffixes ("-1") sort after the base name, as they should.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = filepath.Join(outDir, n)
	}
	return out, nil
}

func isBackupName(name string) bool {
	rest, ok := strings.CutPrefix(name, DirPrefix)
	if !ok || len(rest) < len(timeLayout) {
		return false
	}
	if _, err := time.Parse(timeLayout, rest[:len(timeLayout)]); err != nil {
		return false
	}
	suffix := rest[len(timeLayout):]
	if suffix == "" {
		return true
	}
	if len(suffix) < 2 || suffix[0] != '-' {
		return false
	}
	for _, c := range suffix[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func latest(outDir string) (string, error) {
	l, err := List(outDir)
	if err != nil || len(l) == 0 {
		return "", err
	}
	return l[0], nil
}

// Prune deletes all but the newest keep backups in outDir. keep <= 0 keeps
// everything. Leftover temporary directories from interrupted runs older than
// a day are removed too. Directories not created by Run are never touched.
func Prune(outDir string, keep int) error {
	if keep <= 0 {
		return nil
	}
	l, err := List(outDir)
	if err != nil {
		return err
	}
	var errs []error
	if len(l) > keep {
		for _, p := range l[keep:] {
			if err := os.RemoveAll(p); err != nil {
				errs = append(errs, err)
			}
		}
	}
	entries, err := os.ReadDir(outDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), tmpPrefix) {
				continue
			}
			if info, err := e.Info(); err == nil && now().Sub(info.ModTime()) > 24*time.Hour {
				errs = append(errs, os.RemoveAll(filepath.Join(outDir, e.Name())))
			}
		}
	}
	return errors.Join(errs...)
}
