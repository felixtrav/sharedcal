package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// BlobStore abstracts file storage so S3-compatible storage can be added later.
// Keys are content addresses produced by KeyFor.
type BlobStore interface {
	// Put stores r under key atomically. If the key already exists it is a
	// no-op (content-addressed).
	Put(ctx context.Context, key string, r io.Reader) error
	// Open returns the blob and its size, or ErrNotFound.
	Open(ctx context.Context, key string) (io.ReadSeekCloser, int64, error)
	// Delete removes the blob; deleting a missing blob is not an error.
	Delete(ctx context.Context, key string) error
}

const (
	dirPerm  = 0o750
	filePerm = 0o640
)

// KeyFor returns the storage key "<sha256[:2]>/<sha256>" for a lower-case hex
// SHA-256 digest.
func KeyFor(sha256hex string) string {
	if len(sha256hex) < 2 {
		return sha256hex
	}
	return sha256hex[:2] + "/" + sha256hex
}

// ValidKey reports whether key has exactly the shape produced by KeyFor:
// two lower-case hex chars, '/', 64 lower-case hex chars, with matching prefix.
// Because only [0-9a-f/] can appear, no key can escape the store root.
func ValidKey(key string) bool {
	if len(key) != 2+1+64 || key[2] != '/' || key[:2] != key[3:5] {
		return false
	}
	return isLowerHex(key[:2]) && isLowerHex(key[3:])
}

func isLowerHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// FSStore stores blobs on the local filesystem under Root as
// <sha256[:2]>/<sha256>. Directories are created 0750 and files 0640.
type FSStore struct {
	Root string
}

var _ BlobStore = (*FSStore)(nil)

// NewFSStore creates the root directory if needed and returns a store.
func NewFSStore(root string) (*FSStore, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("attachments: resolve store root: %w", err)
	}
	if err := os.MkdirAll(abs, dirPerm); err != nil {
		return nil, fmt.Errorf("attachments: create store root: %w", err)
	}
	return &FSStore{Root: abs}, nil
}

func (s *FSStore) path(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	return filepath.Join(s.Root, key[:2], key[3:]), nil
}

// Put writes r to a temp file in the target directory, fsyncs it, verifies its
// SHA-256 matches the key (so a key can never point at different content),
// then renames it into place and fsyncs the directory. Concurrent Puts of the
// same key are safe: both renames install identical content.
func (s *FSStore) Put(ctx context.Context, key string, r io.Reader) error {
	dst, err := s.path(key)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("attachments: create blob dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".put-*")
	if err != nil {
		return fmt.Errorf("attachments: create temp blob: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), ctxReader{ctx, r}); err != nil {
		return fmt.Errorf("attachments: write blob: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != key[3:] {
		return fmt.Errorf("attachments: blob content hash %s does not match key %s", got, key)
	}
	if err := tmp.Chmod(filePerm); err != nil {
		return fmt.Errorf("attachments: chmod blob: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("attachments: fsync blob: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("attachments: close blob: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("attachments: install blob: %w", err)
	}
	committed = true
	return syncDir(dir)
}

// Open returns the blob as an *os.File.
func (s *FSStore) Open(_ context.Context, key string) (io.ReadSeekCloser, int64, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("attachments: open blob: %w", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, fmt.Errorf("attachments: stat blob: %w", err)
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return nil, 0, ErrNotFound
	}
	return f, st.Size(), nil
}

// Delete removes the blob. Callers must only delete a key once no attachment
// row references it any more, because identical uploads share one blob.
func (s *FSStore) Delete(_ context.Context, key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("attachments: delete blob: %w", err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("attachments: open blob dir: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil && !isUnsupportedSync(err) {
		return fmt.Errorf("attachments: fsync blob dir: %w", err)
	}
	return nil
}

// isUnsupportedSync tolerates filesystems that cannot fsync directories.
func isUnsupportedSync(err error) bool {
	return errors.Is(err, os.ErrInvalid) || strings.Contains(err.Error(), "invalid argument")
}

// ctxReader aborts a copy once the context is done.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
