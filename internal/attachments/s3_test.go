package attachments

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
)

// fakeS3 starts an in-memory S3 server with one bucket and returns a store
// for it plus the backend, so tests can check the stored objects directly.
func fakeS3(t *testing.T, prefix string) (*S3Store, *s3mem.Backend) {
	t.Helper()
	backend := s3mem.New()
	if err := backend.CreateBucket("sharedcal"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gofakes3.New(backend).Server())
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	s, err := NewS3Store(S3Config{Endpoint: u, Bucket: "sharedcal", Region: "us-east-1", Prefix: prefix,
		AccessKey: "test", SecretKey: "testtesttest", PathStyle: true, TempDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s, backend
}

func keyOf(b []byte) string {
	sum := sha256.Sum256(b)
	return KeyFor(hex.EncodeToString(sum[:]))
}

// streamOnly hides io.Seeker, like a zip entry during archive import.
type streamOnly struct{ io.Reader }

func TestS3StoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, backend := fakeS3(t, "/cal/blobs/")
	if err := s.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if s.String() != "s3://sharedcal/cal/blobs" {
		t.Errorf("String() = %q", s.String())
	}
	body := []byte("hello from a bucket")
	key := keyOf(body)

	for name, r := range map[string]io.Reader{"seekable": bytes.NewReader(body), "stream": streamOnly{bytes.NewReader(body)}} {
		if err := s.Put(ctx, key, r); err != nil {
			t.Fatalf("%s put: %v", name, err)
		}
	}
	obj, err := backend.HeadObject("sharedcal", "cal/blobs/"+key)
	if err != nil || obj.Size != int64(len(body)) {
		t.Fatalf("object not stored under the prefix: %v %+v", err, obj)
	}

	rc, size, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if size != int64(len(body)) || !bytes.Equal(got, body) {
		t.Fatalf("open: size %d, %q", size, got)
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("open after delete: %v", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("deleting a missing blob: %v", err)
	}
}

func TestS3StorePutExistingSkipsUpload(t *testing.T) {
	ctx := context.Background()
	s, _ := fakeS3(t, "")
	body := []byte("stored once")
	key := keyOf(body)
	if err := s.Put(ctx, key, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	// The second Put must not read (or verify) its input at all.
	if err := s.Put(ctx, key, iotest{t}); err != nil {
		t.Fatal(err)
	}
}

type iotest struct{ t *testing.T }

func (r iotest) Read([]byte) (int, error) {
	r.t.Error("Put read its input although the blob exists")
	return 0, io.EOF
}

func TestS3StoreRejectsMismatchedContent(t *testing.T) {
	ctx := context.Background()
	s, backend := fakeS3(t, "")
	key := keyOf([]byte("the real content"))
	for name, r := range map[string]io.Reader{
		"seekable": strings.NewReader("something else"),
		"stream":   streamOnly{strings.NewReader("something else")},
	} {
		if err := s.Put(ctx, key, r); err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("%s: want hash mismatch, got %v", name, err)
		}
	}
	if _, err := backend.HeadObject("sharedcal", key); err == nil {
		t.Fatal("mismatched content reached the bucket")
	}
	if err := s.Put(ctx, "../../etc/passwd", strings.NewReader("x")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid key: %v", err)
	}
	if _, _, err := s.Open(ctx, "nope"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid key on open: %v", err)
	}
}

// Downloads go through http.ServeContent, which seeks; Range requests must
// return the right bytes.
func TestS3StoreServesRanges(t *testing.T) {
	ctx := context.Background()
	s, _ := fakeS3(t, "")
	body := bytes.Repeat([]byte("0123456789"), 1000)
	key := keyOf(body)
	if err := s.Put(ctx, key, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	rc, _, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Range", "bytes=5000-5009")
	w := httptest.NewRecorder()
	http.ServeContent(w, req, "", time.Time{}, rc)
	if w.Code != http.StatusPartialContent || w.Body.String() != "0123456789" || w.Header().Get("Content-Range") != "bytes 5000-5009/10000" {
		t.Fatalf("range: %d %q %q", w.Code, w.Body.String(), w.Header().Get("Content-Range"))
	}
}

func TestS3StoreCheckMissingBucket(t *testing.T) {
	s, _ := fakeS3(t, "")
	s.bucket = "missing"
	if err := s.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("want missing-bucket error, got %v", err)
	}
}

// The upload pipeline works unchanged on top of S3.
func TestPipelineOnS3(t *testing.T) {
	ctx := context.Background()
	s, _ := fakeS3(t, "")
	p := &Pipeline{Store: s, MaxSize: 1 << 20, TempDir: t.TempDir()}
	res, err := p.Process(ctx, "notes.txt", strings.NewReader("meeting notes\n"))
	if err != nil {
		t.Fatal(err)
	}
	rc, _, err := s.Open(ctx, res.StorageKey)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if b, _ := io.ReadAll(rc); string(b) != "meeting notes\n" {
		t.Fatalf("stored %q", b)
	}
}
