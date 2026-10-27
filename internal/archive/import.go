package archive

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"github.com/felixtrav/sharedcal/internal/store"
)

// Limits bound what Import will decompress, as a defence against zip bombs
// and hostile archives. Sizes are checked against each entry's declared size
// up front and enforced again while reading (archive/zip also fails an entry
// that inflates beyond its declared size).
type Limits struct {
	MaxEntries    int   // number of zip entries
	MaxEntryBytes int64 // decompressed size of any single entry
	MaxTotalBytes int64 // decompressed size of all entries together
	MaxLineBytes  int   // one JSON row
	MaxManifest   int64 // manifest.json
}

// DefaultLimits are generous for any realistic SharedCal instance.
var DefaultLimits = Limits{
	MaxEntries:    1_000_000,
	MaxEntryBytes: 4 << 30,
	MaxTotalBytes: 256 << 30,
	MaxLineBytes:  32 << 20,
	MaxManifest:   256 << 20,
}

// ErrNotEmpty is returned when the target database already holds data.
var ErrNotEmpty = errors.New("archive: the target database is not empty; import needs a freshly migrated database")

// Import restores an archive written by Export into db and dataDir, using
// DefaultLimits. See ImportWithLimits.
func Import(ctx context.Context, db *store.DB, dataDir string, r io.ReaderAt, size int64) error {
	return ImportWithLimits(ctx, db, dataDir, r, size, DefaultLimits)
}

// ImportWithLimits restores an archive. The database must be freshly migrated
// and empty. The whole archive is validated first (entry names, types,
// sizes, manifest); then files are restored (each verified against its
// SHA-256 while being written, and moved into place atomically) and finally
// all rows are inserted in dependency order, in batches, inside one
// transaction. IDs are preserved, and the change_log sequence continues after
// the source's high-water mark.
//
// Files are restored before rows on purpose: if inserting fails nothing is
// committed and the only leftovers are content-addressed blobs, so the import
// can simply be retried; the reverse order could commit rows pointing at
// blobs that never arrived.
func ImportWithLimits(ctx context.Context, db *store.DB, dataDir string, r io.ReaderAt, size int64, lim Limits) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("archive: not a valid archive: %w", err)
	}
	entries, err := validateEntries(zr, lim)
	if err != nil {
		return err
	}
	mf, ok := entries[manifestName]
	if !ok {
		return errors.New("archive: manifest.json missing")
	}
	var m Manifest
	if err := readJSON(mf, lim.MaxManifest, &m); err != nil {
		return fmt.Errorf("archive: manifest: %w", err)
	}
	if m.Format < 1 || m.Format > FormatVersion {
		return fmt.Errorf("archive: format version %d is not supported by this version (max %d); upgrade SharedCal first", m.Format, FormatVersion)
	}

	tbls, err := tables(db.W)
	if err != nil {
		return err
	}
	byName := map[string]table{}
	for _, t := range tbls {
		byName[t.schema.Table] = t
	}
	files := map[string]File{}
	for _, f := range m.Files {
		if err := checkName(f.Path); err != nil {
			return fmt.Errorf("archive: manifest file %q: %w", f.Path, err)
		}
		if _, err := hex.DecodeString(f.SHA256); err != nil || len(f.SHA256) != 64 {
			return fmt.Errorf("archive: manifest file %q: bad sha256", f.Path)
		}
		files[f.Path] = f
	}
	// Every entry must be expected; anything else signals a corrupt or
	// crafted archive.
	for name := range entries {
		switch {
		case name == manifestName:
		case strings.HasPrefix(name, tablesDir):
			tn := strings.TrimSuffix(strings.TrimPrefix(name, tablesDir), ".jsonl")
			if _, ok := byName[tn]; !ok || !strings.HasSuffix(name, ".jsonl") {
				return fmt.Errorf("archive: unknown table file %s (archive from a newer version?)", name)
			}
		case strings.HasPrefix(name, attachDir), isExtraFile(name):
			if _, ok := files[name]; !ok {
				return fmt.Errorf("archive: %s is not listed in the manifest", name)
			}
		default:
			return fmt.Errorf("archive: unexpected entry %s", name)
		}
	}
	for p := range files {
		if _, ok := entries[p]; !ok {
			return fmt.Errorf("archive: %s is listed in the manifest but missing", p)
		}
		if !strings.HasPrefix(p, attachDir) && !isExtraFile(p) {
			return fmt.Errorf("archive: manifest file %s is outside the allowed directories", p)
		}
	}

	if err := ensureEmpty(ctx, db, tbls); err != nil {
		return err
	}

	if err := restoreFiles(ctx, dataDir, entries, m.Files, lim); err != nil {
		return err
	}

	var maxSeq int64
	err = db.W.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, t := range tbls {
			name := t.schema.Table
			zf, ok := entries[tablesDir+name+".jsonl"]
			if !ok {
				continue
			}
			n, hi, err := importTable(ctx, tx, t.schema, zf, lim)
			if err != nil {
				return fmt.Errorf("archive: import %s: %w", name, err)
			}
			if want, ok := m.RowCounts[name]; ok && want != n {
				return fmt.Errorf("archive: %s: manifest says %d rows, archive has %d", name, want, n)
			}
			if name == "change_log" {
				maxSeq = hi
			}
		}
		maxSeq = max(maxSeq, m.ChangeLogMaxSeq)
		if db.Dialect != store.MySQL {
			return bumpSequence(tx, db.Dialect, maxSeq)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if db.Dialect == store.MySQL {
		// ALTER TABLE commits implicitly in MySQL, so it runs after the import.
		return bumpSequence(db.W.WithContext(ctx), db.Dialect, maxSeq)
	}
	return nil
}

func isExtraFile(name string) bool {
	for _, d := range extraDirs {
		if strings.HasPrefix(name, filesDir+d+"/") {
			return true
		}
	}
	return false
}

func validateEntries(zr *zip.Reader, lim Limits) (map[string]*zip.File, error) {
	if len(zr.File) > lim.MaxEntries {
		return nil, fmt.Errorf("archive: too many entries (%d)", len(zr.File))
	}
	out := make(map[string]*zip.File, len(zr.File))
	var total uint64
	for _, f := range zr.File {
		name := f.Name
		if strings.HasSuffix(name, "/") && f.Mode().IsDir() {
			if err := checkName(strings.TrimSuffix(name, "/")); err != nil {
				return nil, fmt.Errorf("archive: entry %q: %w", name, err)
			}
			continue
		}
		if err := checkName(name); err != nil {
			return nil, fmt.Errorf("archive: entry %q: %w", name, err)
		}
		if !f.Mode().IsRegular() {
			return nil, fmt.Errorf("archive: entry %q is not a regular file (symlinks and devices are refused)", name)
		}
		if f.Method != zip.Store && f.Method != zip.Deflate {
			return nil, fmt.Errorf("archive: entry %q uses an unsupported compression method", name)
		}
		if f.UncompressedSize64 > uint64(lim.MaxEntryBytes) {
			return nil, fmt.Errorf("archive: entry %q is too large (%d bytes)", name, f.UncompressedSize64)
		}
		total += f.UncompressedSize64
		if total > uint64(lim.MaxTotalBytes) {
			return nil, errors.New("archive: total decompressed size exceeds the limit")
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("archive: duplicate entry %q", name)
		}
		out[name] = f
	}
	return out, nil
}

// open returns the entry's content limited to its declared size (and at
// most max). Reading past either is an error, not a silent truncation.
func open(f *zip.File, max int64) (io.ReadCloser, error) {
	limit := int64(f.UncompressedSize64)
	if limit > max {
		return nil, fmt.Errorf("%s exceeds the size limit", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	return &capped{rc: rc, left: limit, name: f.Name}, nil
}

type capped struct {
	rc   io.ReadCloser
	left int64
	name string
}

func (c *capped) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// Probe for extra data beyond the declared size.
		var one [1]byte
		if n, _ := c.rc.Read(one[:]); n > 0 {
			return 0, fmt.Errorf("%s is larger than declared", c.name)
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.rc.Read(p)
	c.left -= int64(n)
	return n, err
}

func (c *capped) Close() error { return c.rc.Close() }

func readJSON(f *zip.File, max int64, v any) error {
	rc, err := open(f, max)
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(rc).Decode(v)
}

func ensureEmpty(ctx context.Context, db *store.DB, tbls []table) error {
	for _, t := range tbls {
		var n int64
		if err := db.W.WithContext(ctx).Table(t.schema.Table).Count(&n).Error; err != nil {
			return fmt.Errorf("archive: check %s: %w (has the database been migrated?)", t.schema.Table, err)
		}
		if n > 0 {
			return fmt.Errorf("%w (%s has %d rows)", ErrNotEmpty, t.schema.Table, n)
		}
	}
	return nil
}

// restoreFiles writes every listed file below dataDir through an os.Root,
// which refuses any path (including via pre-existing symlinks) that would
// leave dataDir.
func restoreFiles(ctx context.Context, dataDir string, entries map[string]*zip.File, files []File, lim Limits) error {
	if len(files) == 0 {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	defer root.Close()
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		rel := strings.TrimPrefix(f.Path, filesDir) // files/themes/x → themes/x
		if err := restoreFile(root, rel, entries[f.Path], f, lim); err != nil {
			return fmt.Errorf("archive: restore %s: %w", f.Path, err)
		}
	}
	return nil
}

func restoreFile(root *os.Root, rel string, zf *zip.File, want File, lim Limits) error {
	// Content-addressed blobs are named by their hash: the name must match.
	base := path.Base(rel)
	if strings.HasPrefix(rel, attachDir) && isHex64(base) && base != want.SHA256 {
		return errors.New("file name does not match its sha256")
	}
	if int64(zf.UncompressedSize64) != want.Size {
		return errors.New("size differs from manifest")
	}
	if dir := path.Dir(rel); dir != "." {
		if err := root.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	// Already present with the right content (e.g. the same /data volume is
	// reused when switching databases)? Nothing to do.
	if got, err := hashFile(root, rel); err == nil && got == want.SHA256 {
		return nil
	}
	rc, err := open(zf, lim.MaxEntryBytes)
	if err != nil {
		return err
	}
	defer rc.Close()
	tmp := path.Join(path.Dir(rel), "."+base+".import-"+randHex())
	out, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = out.Close()
			_ = root.Remove(tmp)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), rc)
	if err != nil {
		return err
	}
	if n != want.Size {
		return fmt.Errorf("size %d, manifest says %d", n, want.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s, manifest says %s", got, want.SHA256)
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := root.Rename(tmp, rel); err != nil {
		return err
	}
	ok = true
	return nil
}

func hashFile(root *os.Root, rel string) (string, error) {
	fi, err := root.Lstat(rel)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fs.ErrInvalid
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil && strings.ToLower(s) == s
}

func randHex() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// importTable inserts the rows of one JSON-lines entry in batches and returns
// the row count and, for change_log, the highest seq.
func importTable(ctx context.Context, tx *gorm.DB, s *schema.Schema, zf *zip.File, lim Limits) (int64, int64, error) {
	rc, err := open(zf, lim.MaxEntryBytes)
	if err != nil {
		return 0, 0, err
	}
	defer rc.Close()
	br := bufio.NewReaderSize(rc, 64<<10)
	batch := reflect.MakeSlice(reflect.SliceOf(reflect.PointerTo(s.ModelType)), 0, batchSize)
	var n, hi int64
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		if err := tx.Table(s.Table).Create(batch.Interface()).Error; err != nil {
			return err
		}
		batch = batch.Slice(0, 0)
		return nil
	}
	for line := 1; ; line++ {
		if err := ctx.Err(); err != nil {
			return n, hi, err
		}
		b, err := readLine(br, lim.MaxLineBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return n, hi, fmt.Errorf("line %d: %w", line, err)
		}
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		rv, err := decodeRow(ctx, s, b)
		if err != nil {
			return n, hi, fmt.Errorf("line %d: %w", line, err)
		}
		if s.Table == "change_log" {
			if v, ok := rv.Elem().FieldByName("Seq").Interface().(int64); ok && v > hi {
				hi = v
			}
		}
		batch = reflect.Append(batch, rv)
		n++
		if batch.Len() >= batchSize {
			if err := flush(); err != nil {
				return n, hi, err
			}
		}
	}
	return n, hi, flush()
}

// readLine returns one line without its newline; io.EOF only when no data
// is left.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > max {
			return nil, errors.New("row too large")
		}
		if err == nil {
			return line[:len(line)-1], nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(line) > 0 {
			return line, nil
		}
		return nil, err
	}
}

// decodeRow maps {"column": value} onto a new model value. Unknown columns
// are refused (the archive is from a newer schema); missing ones keep their
// zero value.
func decodeRow(ctx context.Context, s *schema.Schema, b []byte) (reflect.Value, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return reflect.Value{}, err
	}
	rv := reflect.New(s.ModelType)
	for col, msg := range raw {
		f, ok := s.FieldsByDBName[col]
		if !ok {
			return reflect.Value{}, fmt.Errorf("unknown column %q", col)
		}
		ptr := reflect.New(f.FieldType)
		if err := json.Unmarshal(msg, ptr.Interface()); err != nil {
			return reflect.Value{}, fmt.Errorf("column %q: %w", col, err)
		}
		if err := f.Set(ctx, rv.Elem(), ptr.Elem().Interface()); err != nil {
			return reflect.Value{}, fmt.Errorf("column %q: %w", col, err)
		}
	}
	return rv, nil
}

// bumpSequence makes the next change_log.seq greater than maxSeq.
func bumpSequence(db *gorm.DB, dialect string, maxSeq int64) error {
	if maxSeq <= 0 {
		return nil
	}
	var err error
	switch dialect {
	case store.SQLite:
		// AUTOINCREMENT keeps its high-water mark in sqlite_sequence.
		res := db.Exec("UPDATE sqlite_sequence SET seq = ? WHERE name = 'change_log' AND seq < ?", maxSeq, maxSeq)
		err = res.Error
		if err == nil && res.RowsAffected == 0 {
			var n int64
			if err = db.Raw("SELECT COUNT(*) FROM sqlite_sequence WHERE name = 'change_log'").Scan(&n).Error; err == nil && n == 0 {
				err = db.Exec("INSERT INTO sqlite_sequence (name, seq) VALUES ('change_log', ?)", maxSeq).Error
			}
		}
	case store.Postgres:
		err = db.Exec("SELECT setval(pg_get_serial_sequence('change_log', 'seq'), GREATEST(?, (SELECT COALESCE(MAX(seq), 0) FROM change_log)), true)", maxSeq).Error
	case store.MySQL:
		// AUTO_INCREMENT never goes below MAX(seq)+1, so this is safe.
		err = db.Exec(fmt.Sprintf("ALTER TABLE change_log AUTO_INCREMENT = %d", maxSeq+1)).Error
	}
	if err != nil {
		return fmt.Errorf("archive: advance change_log sequence: %w", err)
	}
	return nil
}
