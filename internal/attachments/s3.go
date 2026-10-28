package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures an S3-compatible bucket (AWS S3, MinIO, Garage,
// Cloudflare R2, Backblaze B2, Wasabi…).
type S3Config struct {
	Endpoint  *url.URL // scheme and host, e.g. https://s3.eu-central-1.amazonaws.com
	Bucket    string
	Region    string // empty lets the client discover it
	Prefix    string // optional key prefix, without leading or trailing slash
	AccessKey string // empty uses the AWS environment, credentials file or IAM role
	SecretKey string
	PathStyle bool // force path-style URLs (most self-hosted servers need this)
	// TempDir holds uploads that arrive as plain streams (archive import)
	// while they are hashed; empty means os.TempDir().
	TempDir string
}

// S3Store stores blobs as objects named <prefix>/<sha256[:2]>/<sha256>, the
// same layout as FSStore, so a data directory can be copied into a bucket
// with any S3 sync tool.
type S3Store struct {
	c       *minio.Client
	bucket  string
	prefix  string
	tempDir string
	desc    string
}

var _ BlobStore = (*S3Store)(nil)

// NewS3Store creates the client. It does not contact the server; call Check.
func NewS3Store(cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == nil || cfg.Endpoint.Host == "" || cfg.Bucket == "" {
		return nil, errors.New("attachments: S3 endpoint and bucket are required")
	}
	creds := credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")
	if cfg.AccessKey == "" {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{}, &credentials.FileAWSCredentials{}, &credentials.IAM{},
		})
	}
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	c, err := minio.New(cfg.Endpoint.Host, &minio.Options{
		Creds: creds, Secure: cfg.Endpoint.Scheme == "https", Region: cfg.Region, BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("attachments: S3 client: %w", err)
	}
	prefix := strings.Trim(cfg.Prefix, "/")
	desc := "s3://" + cfg.Bucket
	if prefix != "" {
		desc += "/" + prefix
		prefix += "/"
	}
	return &S3Store{c: c, bucket: cfg.Bucket, prefix: prefix, tempDir: cfg.TempDir, desc: desc}, nil
}

// String names the bucket and prefix, e.g. "s3://sharedcal/attachments".
func (s *S3Store) String() string { return s.desc }

// Check verifies that the bucket exists and the credentials can reach it.
func (s *S3Store) Check(ctx context.Context) error {
	ok, err := s.c.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("attachments: reach S3 bucket %s: %w", s.bucket, err)
	}
	if !ok {
		return fmt.Errorf("attachments: S3 bucket %s does not exist", s.bucket)
	}
	return nil
}

// WaitReady calls Check until it succeeds, giving up after about a minute so
// SharedCal can start alongside a self-hosted S3 server.
func (s *S3Store) WaitReady(ctx context.Context) error {
	delay := time.Second
	for attempt := 1; ; attempt++ {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := s.Check(cctx)
		cancel()
		if err == nil || attempt == 6 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
}

func (s *S3Store) object(key string) (string, error) {
	if !ValidKey(key) {
		return "", ErrInvalidKey
	}
	return s.prefix + key, nil
}

// Put uploads r under key unless the object already exists. The content is
// hashed before anything is sent, so a key can never point at different
// content: seekable readers (upload spill files) are hashed in place, other
// streams are first copied to a temp file.
func (s *S3Store) Put(ctx context.Context, key string, r io.Reader) error {
	name, err := s.object(key)
	if err != nil {
		return err
	}
	if _, err := s.c.StatObject(ctx, s.bucket, name, minio.StatObjectOptions{}); err == nil {
		return nil
	} else if !isNoSuchKey(err) {
		return fmt.Errorf("attachments: stat blob: %w", err)
	}

	rs, ok := r.(io.ReadSeeker)
	if !ok {
		f, err := os.CreateTemp(s.tempDir, "s3-put-*")
		if err != nil {
			return fmt.Errorf("attachments: create temp blob: %w", err)
		}
		defer os.Remove(f.Name())
		defer f.Close()
		if _, err := io.Copy(f, ctxReader{ctx, r}); err != nil {
			return fmt.Errorf("attachments: spool blob: %w", err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		rs = f
	}
	start, err := rs.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(h, ctxReader{ctx, rs})
	if err != nil {
		return fmt.Errorf("attachments: hash blob: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != key[3:] {
		return fmt.Errorf("attachments: blob content hash %s does not match key %s", got, key)
	}
	if _, err := rs.Seek(start, io.SeekStart); err != nil {
		return err
	}
	if _, err := s.c.PutObject(ctx, s.bucket, name, rs, size, minio.PutObjectOptions{ContentType: "application/octet-stream"}); err != nil {
		return fmt.Errorf("attachments: upload blob: %w", err)
	}
	return nil
}

// Open returns the object as a seekable reader; seeks turn into ranged GETs,
// so http.ServeContent can answer Range requests without downloading the
// whole object.
func (s *S3Store) Open(ctx context.Context, key string) (io.ReadSeekCloser, int64, error) {
	name, err := s.object(key)
	if err != nil {
		return nil, 0, err
	}
	obj, err := s.c.GetObject(ctx, s.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("attachments: open blob: %w", err)
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		if isNoSuchKey(err) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, fmt.Errorf("attachments: open blob: %w", err)
	}
	return obj, info.Size, nil
}

// Delete removes the object; a missing object is not an error.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	name, err := s.object(key)
	if err != nil {
		return err
	}
	if err := s.c.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{}); err != nil && !isNoSuchKey(err) {
		return fmt.Errorf("attachments: delete blob: %w", err)
	}
	return nil
}

func isNoSuchKey(err error) bool {
	return minio.ToErrorResponse(err).Code == minio.NoSuchKey
}
