package attachments

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// FuzzProcess feeds arbitrary bytes and filenames through the pipeline:
// malformed images, mismatched extensions, oversized and zero-byte files and
// path-traversal names. It asserts no panics, no leaked temp files, only
// sentinel/IO errors, and that accepted files only ever get allow-listed
// content types.
func FuzzProcess(f *testing.F) {
	seeds := []struct {
		name string
		data []byte
	}{
		{"a.png", pngBytes(f, 8, 8)},
		{"a.jpg", jpegWithExif(f, 8, 8)},
		{"a.gif", gifBytes(f, 4, 4)},
		{"a.webp", webpBytes(f)},
		{"a.pdf", pdfBytes},
		{"a.docx", ooxmlBytes(f, "word/document.xml")},
		{"a.odt", odfBytes(f, mimeODT)},
		{"a.txt", []byte("hello\n")},
		{"a.csv", []byte("a,b\n1,2\n3,4\n")},
		{"a.ics", []byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")},
		{"bomb.png", pngHeaderOnly(50000, 50000)},
		{"../../etc/passwd.txt", []byte("root:x:0:0")},
		{`..\..\win.ini.md`, []byte("[x]")},
		{"x.jpg", pngBytes(f, 2, 2)},
		{"x.txt", []byte("<html><script>")},
		{"empty.txt", nil},
		{"big.txt", bytes.Repeat([]byte("a"), 70_000)},
		{"evil\u202egnp.exe", []byte("MZ")},
	}
	for _, s := range seeds {
		f.Add(s.name, s.data)
	}
	allowed := AllowedContentTypes()
	f.Fuzz(func(t *testing.T, name string, data []byte) {
		dir := t.TempDir()
		store, err := NewFSStore(filepath.Join(dir, "att"))
		if err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(dir, "tmp")
		os.Mkdir(tmp, 0o700)
		// Small limits keep each iteration fast and exercise the limits.
		p := &Pipeline{Store: store, MaxSize: 64 << 10, MaxPixels: 1_000_000, TempDir: tmp}
		res, err := p.Process(context.Background(), name, bytes.NewReader(data))
		if ents, _ := os.ReadDir(tmp); len(ents) != 0 {
			t.Fatalf("temp files leaked: %v", ents)
		}
		if err != nil {
			if !isSentinel(err) {
				t.Fatalf("unexpected error kind: %v", err)
			}
			return
		}
		if !allowed[res.ContentType] {
			t.Fatalf("disallowed content type %q", res.ContentType)
		}
		if len(data) == 0 || int64(len(data)) > p.MaxSize {
			t.Fatalf("accepted %d bytes", len(data))
		}
		if res.StorageKey != KeyFor(res.SHA256) || !ValidKey(res.StorageKey) {
			t.Fatalf("bad key %q", res.StorageKey)
		}
		checkSanitized(t, name, res.Filename)
		if res.IsImage != (res.ThumbKey != nil) {
			t.Fatal("image/thumbnail mismatch")
		}
		if res.IsImage && int64(*res.Width)*int64(*res.Height) > int64(p.MaxPixels) {
			t.Fatal("pixel limit bypassed")
		}
	})
}

func isSentinel(err error) bool {
	for _, s := range []error{ErrTooLarge, ErrEmpty, ErrTypeNotAllowed, ErrTypeMismatch, ErrImageTooLarge, ErrCorrupt} {
		if errors.Is(err, s) {
			return true
		}
	}
	return false
}

func FuzzSanitizeFilename(f *testing.F) {
	for _, s := range []string{
		"report.pdf", "../../etc/passwd", `..\..\win.ini`, "invoice\u202efdp.exe",
		"nul\x00.txt", "café.txt", " . ", string(make([]byte, 500)),
		"\xff\xfe", "a/b\\c", "e\x00́", "क़́",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		checkSanitized(t, in, SanitizeFilename(in))
		// Content-Disposition must stay a single, valid header value.
		cd := ContentDisposition("attachment", in)
		for i := 0; i < len(cd); i++ {
			if cd[i] < 0x20 || cd[i] >= 0x7F {
				t.Fatalf("non-ASCII/control byte in header: %q", cd)
			}
		}
	})
}
