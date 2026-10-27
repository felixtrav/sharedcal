package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"strings"
)

// Scan statuses stored on attachment rows.
const (
	ScanPending  = "pending"
	ScanSkipped  = "skipped"
	ScanClean    = "clean"
	ScanInfected = "infected"
)

// Pipeline runs the spec's upload sanitization pipeline.
type Pipeline struct {
	Store       BlobStore
	MaxSize     int64  // per-file bytes; must be > 0
	MaxPixels   int    // decompression-bomb limit; 0 means DefaultMaxPixels
	TempDir     string // spill directory (DATA_DIR/tmp); uploads are never buffered in memory
	ScanEnabled bool   // when true, results get ScanStatus "pending", else "skipped"
}

// Result describes a stored, sanitized attachment. It maps onto the
// attachments table (sha256, storage_key, content_type, filename, size_bytes,
// width, height, thumb_key, scan_status).
type Result struct {
	SHA256, StorageKey, ContentType, Filename string
	Size                                      int64
	Width, Height                             *int
	ThumbKey                                  *string
	IsImage                                   bool
	ScanStatus                                string // "pending" | "skipped"
}

// Process runs steps 1-5, 7 and 8 of the pipeline on an upload stream:
// size-limited spill to disk with hashing, magic-byte detection checked
// against the allow-list and extension, image re-encoding or text/container
// validation, filename sanitization and content-addressed storage.
// Errors wrap the package's sentinel errors; see errors.go.
func (p *Pipeline) Process(ctx context.Context, filename string, r io.Reader) (*Result, error) {
	if p.Store == nil || p.MaxSize <= 0 {
		return nil, errors.New("attachments: pipeline misconfigured (Store and MaxSize are required)")
	}
	name := SanitizeFilename(filename)
	ext := strings.ToLower(path.Ext(name))
	// Refuse unknown extensions before reading the body: nothing with this
	// name could ever be accepted.
	if _, ok := allowList[ext]; !ok {
		return nil, ErrTypeNotAllowed
	}

	upload, err := p.spill(ctx, r)
	if err != nil {
		return nil, err
	}
	defer upload.cleanup()

	detected, err := detect(upload.f)
	if err != nil {
		return nil, fmt.Errorf("attachments: detect type: %w", err)
	}
	ft, err := classify(ext, detected)
	if err != nil {
		return nil, err
	}

	res := &Result{Filename: name, ScanStatus: ScanSkipped}
	if p.ScanEnabled {
		res.ScanStatus = ScanPending
	}

	if ft.kind == kindImage {
		if err := p.storeImage(ctx, upload, detected.String(), res); err != nil {
			return nil, err
		}
		return res, nil
	}
	if err := validateNonImage(upload, ft); err != nil {
		return nil, err
	}
	if err := p.put(ctx, upload); err != nil {
		return nil, err
	}
	res.SHA256, res.StorageKey = upload.sum, KeyFor(upload.sum)
	res.ContentType, res.Size = ft.contentType, upload.size
	return res, nil
}

// validateNonImage applies the per-kind checks for files stored as-is.
func validateNonImage(upload *spillFile, ft fileType) error {
	switch ft.kind {
	case kindText:
		return validateText(upload.f)
	case kindOOXML, kindODF:
		return validateContainer(upload.f, upload.size, ft)
	case kindOpaque:
		return nil // PDF: magic bytes checked, never parsed (spec step 5)
	}
	return ErrTypeNotAllowed
}

// storeImage re-encodes the upload and stores the clean copy and thumbnail.
func (p *Pipeline) storeImage(ctx context.Context, upload *spillFile, mime string, res *Result) error {
	full, thumb, err := p.reencode(upload.f, mime)
	if err != nil {
		return err
	}
	defer full.spill.cleanup()
	defer thumb.spill.cleanup()
	if err := p.put(ctx, full.spill); err != nil {
		return err
	}
	if err := p.put(ctx, thumb.spill); err != nil {
		return err
	}
	thumbKey := KeyFor(thumb.spill.sum)
	w, h := full.width, full.height
	res.IsImage = true
	res.SHA256, res.StorageKey = full.spill.sum, KeyFor(full.spill.sum)
	res.ContentType, res.Size = full.contentType, full.spill.size
	res.Width, res.Height, res.ThumbKey = &w, &h, &thumbKey
	res.Filename = outputImageName(res.Filename, full.ext)
	return nil
}

// outputImageName adjusts the display name to the stored format, keeping a
// ".jpeg" spelling for JPEG sources.
func outputImageName(name, ext string) string {
	cur := strings.ToLower(path.Ext(name))
	if cur == ext || (ext == ".jpg" && cur == ".jpeg") {
		return name
	}
	return replaceExt(name, ext)
}

func (p *Pipeline) put(ctx context.Context, sf *spillFile) error {
	if _, err := sf.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := p.Store.Put(ctx, KeyFor(sf.sum), sf.f); err != nil {
		return fmt.Errorf("attachments: store blob: %w", err)
	}
	return nil
}

// ScanStored streams a stored blob to the scanner (spec step 6). The domain
// layer calls it in the background after inserting a "pending" row.
func (p *Pipeline) ScanStored(ctx context.Context, s Scanner, key string) (Verdict, error) {
	rc, _, err := p.Store.Open(ctx, key)
	if err != nil {
		return Verdict{}, err
	}
	defer rc.Close()
	return s.Scan(ctx, rc)
}

// spill copies the upload into a 0600 temp file in TempDir, hashing it and
// enforcing MaxSize while streaming.
func (p *Pipeline) spill(ctx context.Context, r io.Reader) (*spillFile, error) {
	sf, err := newSpill(p.TempDir)
	if err != nil {
		return nil, err
	}
	src := &limitReader{r: ctxReader{ctx, r}, remaining: p.MaxSize}
	if _, err := io.Copy(sf.writer(), src); err != nil {
		sf.cleanup()
		return nil, err
	}
	sf.finish()
	if sf.size == 0 {
		sf.cleanup()
		return nil, ErrEmpty
	}
	return sf, nil
}

// limitReader returns ErrTooLarge as soon as more than remaining bytes are
// read. Unlike io.LimitReader it distinguishes "exactly at the limit" from
// "over the limit" by probing for one extra byte.
type limitReader struct {
	r         io.Reader
	remaining int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if int64(len(p)) > l.remaining+1 {
		p = p[:l.remaining+1]
	}
	n, err := l.r.Read(p)
	if int64(n) > l.remaining {
		l.remaining = 0
		return 0, ErrTooLarge
	}
	l.remaining -= int64(n)
	return n, err
}

// spillFile is a private temp file plus the SHA-256 and size of what was
// written to it. cleanup closes and removes it and is safe to call twice.
type spillFile struct {
	f    *os.File
	h    hash.Hash
	size int64
	sum  string
}

func newSpill(dir string) (*spillFile, error) {
	// os.CreateTemp creates the file with mode 0600.
	f, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return nil, fmt.Errorf("attachments: create temp file: %w", err)
	}
	return &spillFile{f: f, h: sha256.New()}, nil
}

func (s *spillFile) writer() io.Writer { return &countingWriter{s} }

type countingWriter struct{ s *spillFile }

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.s.f.Write(p)
	c.s.h.Write(p[:n])
	c.s.size += int64(n)
	return n, err
}

// finish records the digest once writing is complete.
func (s *spillFile) finish() {
	s.sum = hex.EncodeToString(s.h.Sum(nil))
}

func (s *spillFile) cleanup() {
	if s == nil || s.f == nil {
		return
	}
	name := s.f.Name()
	s.f.Close()
	os.Remove(name)
	s.f = nil
}
