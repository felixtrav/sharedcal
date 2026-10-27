package attachments

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestPipeline(t testing.TB) (*Pipeline, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewFSStore(filepath.Join(dir, "attachments"))
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, "tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	return &Pipeline{Store: store, MaxSize: 5 << 20, TempDir: tmp}, dir
}

func assertTempEmpty(t testing.TB, p *Pipeline) {
	t.Helper()
	ents, err := os.ReadDir(p.TempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("temp files leaked: %v", ents)
	}
}

func readBlob(t testing.TB, p *Pipeline, key string) []byte {
	t.Helper()
	rc, size, err := p.Store.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("open %s: %v", key, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(b)) != size {
		t.Fatalf("size %d != %d", len(b), size)
	}
	return b
}

func TestProcessAllowedTypes(t *testing.T) {
	csv := "name,date,place\nalice,2026-01-01,park\nbob,2026-01-02,beach\n"
	ics := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n"
	cases := []struct {
		name, filename string
		data           []byte
		wantType       string
		wantName       string
		image          bool
	}{
		{"png", "pic.png", pngBytes(t, 20, 10), "image/png", "pic.png", true},
		{"jpeg", "Photo.JPG", jpegBytes(t, 20, 10), "image/jpeg", "Photo.JPG", true},
		{"jpeg long ext", "photo.jpeg", jpegBytes(t, 20, 10), "image/jpeg", "photo.jpeg", true},
		{"gif first frame", "anim.gif", gifBytes(t, 8, 8), "image/png", "anim.png", true},
		{"webp", "tiny.webp", webpBytes(t), "image/png", "tiny.png", true},
		{"pdf", "agenda.pdf", pdfBytes, "application/pdf", "agenda.pdf", false},
		{"txt", "notes.txt", []byte("hello wörld\nline 2\n"), "text/plain; charset=utf-8", "notes.txt", false},
		{"md", "README.md", []byte("# Title\n\n* item\n"), "text/markdown; charset=utf-8", "README.md", false},
		{"markdown", "x.markdown", []byte("Some *text*\n"), "text/markdown; charset=utf-8", "x.markdown", false},
		{"csv", "list.csv", []byte(csv), "text/csv; charset=utf-8", "list.csv", false},
		{"ics", "invite.ics", []byte(ics), "text/calendar; charset=utf-8", "invite.ics", false},
		{"docx", "a.docx", ooxmlBytes(t, "word/document.xml"), mimeDOCX, "a.docx", false},
		{"xlsx", "a.xlsx", ooxmlBytes(t, "xl/workbook.xml"), mimeXLSX, "a.xlsx", false},
		{"pptx", "a.pptx", ooxmlBytes(t, "ppt/presentation.xml"), mimePPTX, "a.pptx", false},
		{"odt", "a.odt", odfBytes(t, mimeODT), mimeODT, "a.odt", false},
		{"ods", "a.ods", odfBytes(t, mimeODS), mimeODS, "a.ods", false},
		{"odp", "a.odp", odfBytes(t, mimeODP), mimeODP, "a.odp", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newTestPipeline(t)
			res, err := p.Process(context.Background(), tc.filename, bytes.NewReader(tc.data))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			assertTempEmpty(t, p)
			if res.ContentType != tc.wantType {
				t.Errorf("content type %q, want %q", res.ContentType, tc.wantType)
			}
			if res.Filename != tc.wantName {
				t.Errorf("filename %q, want %q", res.Filename, tc.wantName)
			}
			if res.IsImage != tc.image {
				t.Errorf("IsImage = %v", res.IsImage)
			}
			if res.ScanStatus != ScanSkipped {
				t.Errorf("scan status %q", res.ScanStatus)
			}
			if res.StorageKey != KeyFor(res.SHA256) {
				t.Errorf("key %q does not match sha %q", res.StorageKey, res.SHA256)
			}
			stored := readBlob(t, p, res.StorageKey)
			if int64(len(stored)) != res.Size {
				t.Errorf("size %d vs stored %d", res.Size, len(stored))
			}
			if !tc.image {
				if !bytes.Equal(stored, tc.data) {
					t.Error("non-image files must be stored byte-for-byte")
				}
				if res.ThumbKey != nil || res.Width != nil {
					t.Error("non-images must not have thumbnails or dimensions")
				}
				return
			}
			if bytes.Equal(stored, tc.data) && tc.name != "png" {
				t.Error("image should have been re-encoded")
			}
			if res.ThumbKey == nil || res.Width == nil || res.Height == nil {
				t.Fatal("image result lacks thumb/dimensions")
			}
			cfg, format, err := image.DecodeConfig(bytes.NewReader(stored))
			if err != nil {
				t.Fatalf("stored image undecodable: %v", err)
			}
			if "image/"+format != res.ContentType {
				t.Errorf("stored format %s vs content type %s", format, res.ContentType)
			}
			if cfg.Width != *res.Width || cfg.Height != *res.Height {
				t.Errorf("dims %dx%d vs result %dx%d", cfg.Width, cfg.Height, *res.Width, *res.Height)
			}
			readBlob(t, p, *res.ThumbKey)
		})
	}
}

func TestProcessScanPending(t *testing.T) {
	p, _ := newTestPipeline(t)
	p.ScanEnabled = true
	res, err := p.Process(context.Background(), "a.pdf", bytes.NewReader(pdfBytes))
	if err != nil {
		t.Fatal(err)
	}
	if res.ScanStatus != ScanPending {
		t.Errorf("got %q", res.ScanStatus)
	}
}

func TestProcessRefusals(t *testing.T) {
	plainZip := zipBytes(t, []zipEntry{{name: "a.txt", body: "hi"}, {name: "b.txt", body: "there"}})
	jarAsDocx := zipBytes(t, []zipEntry{
		{name: "[Content_Types].xml", body: contentTypesXML},
		{name: "word/document.xml", body: "<doc/>"},
		{name: "META-INF/MANIFEST.MF", body: "Main-Class: Evil\n"},
	})
	jar := zipBytes(t, []zipEntry{{name: "META-INF/MANIFEST.MF", body: "Manifest-Version: 1.0\n"}, {name: "Evil.class", body: "\xca\xfe\xba\xbe"}})
	apk := zipBytes(t, []zipEntry{{name: "AndroidManifest.xml", body: "x"}, {name: "classes.dex", body: "dex"}})
	epub := zipBytes(t, []zipEntry{{name: "mimetype", body: "application/epub+zip", store: true}, {name: "META-INF/container.xml", body: "<c/>"}})
	docxNoMain := zipBytes(t, []zipEntry{{name: "[Content_Types].xml", body: contentTypesXML}, {name: "word/evil.js", body: "x"}})
	odtWrongMime := odfBytes(t, mimeODS)
	pe := append([]byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff\x00\x00"), bytes.Repeat([]byte{0}, 200)...)
	elf := append([]byte("\x7fELF\x02\x01\x01\x00"), bytes.Repeat([]byte{0}, 200)...)

	cases := []struct {
		name, filename string
		data           []byte
		want           error
	}{
		{"svg", "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`), ErrTypeNotAllowed},
		{"svg as txt", "logo.txt", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`), ErrTypeNotAllowed},
		{"html as txt", "page.txt", []byte("<!DOCTYPE html><html><script>alert(1)</script></html>"), ErrTypeNotAllowed},
		{"html as md", "page.md", []byte("<html><body onload=alert(1)>"), ErrTypeNotAllowed},
		{"html file", "page.html", []byte("<html></html>"), ErrTypeNotAllowed},
		{"xml as csv", "data.csv", []byte(`<?xml version="1.0"?><a/>`), ErrTypeNotAllowed},
		{"js as txt", "x.txt", []byte("#!/usr/bin/env node\nconsole.log(1)\n"), ErrTypeNotAllowed},
		{"js file", "x.js", []byte("alert(1)"), ErrTypeNotAllowed},
		{"plain zip", "a.zip", plainZip, ErrTypeNotAllowed},
		{"plain zip as docx", "a.docx", plainZip, ErrTypeNotAllowed},
		{"jar as docx", "a.docx", jar, ErrTypeNotAllowed},
		{"jar markers inside docx", "a.docx", jarAsDocx, ErrTypeNotAllowed},
		{"apk as xlsx", "a.xlsx", apk, ErrTypeNotAllowed},
		{"epub as odt", "a.odt", epub, ErrTypeNotAllowed},
		{"docx without main part", "a.docx", docxNoMain, ErrCorrupt},
		{"docx named xlsx", "a.xlsx", ooxmlBytes(t, "word/document.xml"), ErrTypeMismatch},
		{"ods named odt", "a.odt", odtWrongMime, ErrTypeMismatch},
		{"PE as pdf", "doc.pdf", pe, ErrTypeNotAllowed},
		{"ELF as pdf", "doc.pdf", elf, ErrTypeNotAllowed},
		{"exe", "setup.exe", pe, ErrTypeNotAllowed},
		{"png named jpg", "pic.jpg", pngBytes(t, 4, 4), ErrTypeMismatch},
		{"jpeg named png", "pic.png", jpegBytes(t, 4, 4), ErrTypeMismatch},
		{"pdf named txt", "doc.txt", pdfBytes, ErrTypeMismatch},
		{"png named txt", "pic.txt", pngBytes(t, 4, 4), ErrTypeMismatch},
		{"no extension", "README", []byte("hello"), ErrTypeNotAllowed},
		{"double ext", "evil.txt.exe", []byte("hello"), ErrTypeNotAllowed},
		{"truncated png", "a.png", pngBytes(t, 40, 40)[:60], ErrCorrupt},
		// "<s" / "cr" are read as a 29500x29283 logical screen: refused before decoding.
		{"gif header polyglot", "a.gif", []byte("GIF89a<script>alert(1)</script>"), ErrImageTooLarge},
		{"gif header polyglot small", "a.gif", []byte("GIF89a\x01\x00\x01\x00<script>alert(1)</script>"), ErrCorrupt},
		{"pixel bomb", "bomb.png", pngHeaderOnly(50000, 50000), ErrImageTooLarge},
		{"pixel count", "big.png", pngHeaderOnly(7000, 7000), ErrImageTooLarge},
		{"zero dims", "zero.png", pngHeaderOnly(0, 10), ErrCorrupt},
		{"absurd aspect", "line.png", pngHeaderOnly(1, 1_000_000), ErrImageTooLarge},
		{"nul in text", "a.txt", append(bytes.Repeat([]byte("a"), 10000), 0, 'b'), ErrCorrupt},
		{"invalid utf8 late", "a.txt", append(bytes.Repeat([]byte("a"), 10000), 0xff, 0xfe, 'b'), ErrCorrupt},
		{"truncated utf8 at end", "a.txt", append(bytes.Repeat([]byte("a"), 10), 0xe2, 0x82), ErrCorrupt},
		{"utf16 text", "a.txt", []byte("\xff\xfeh\x00i\x00"), ErrCorrupt},
		{"empty", "a.txt", nil, ErrEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := newTestPipeline(t)
			_, err := p.Process(context.Background(), tc.filename, bytes.NewReader(tc.data))
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			assertTempEmpty(t, p)
			assertStoreEmpty(t, p)
		})
	}
}

func assertStoreEmpty(t testing.TB, p *Pipeline) {
	t.Helper()
	root := p.Store.(*FSStore).Root
	n := 0
	filepath.WalkDir(root, func(_ string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() {
			n++
		}
		return nil
	})
	if n != 0 {
		t.Fatalf("refused upload left %d blobs", n)
	}
}

func TestPolyglotPayloadDestroyed(t *testing.T) {
	// A valid PNG with an HTML page and a zip appended: accepted, but the
	// re-encoded copy must not contain the payload.
	payload := []byte("<html><script>alert(document.cookie)</script></html>PK\x03\x04")
	for name, img := range map[string][]byte{
		"cat.png": pngBytes(t, 16, 16), "cat.gif": gifBytes(t, 16, 16), "cat.jpg": jpegBytes(t, 16, 16),
	} {
		p, _ := newTestPipeline(t)
		res, err := p.Process(context.Background(), name, bytes.NewReader(append(img, payload...)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		stored := readBlob(t, p, res.StorageKey)
		if bytes.Contains(stored, []byte("<script>")) || bytes.Contains(stored, []byte("PK\x03\x04")) {
			t.Errorf("%s: polyglot payload survived re-encoding", name)
		}
	}
}

func TestExifAndGPSStripped(t *testing.T) {
	data := jpegWithExif(t, 40, 20)
	if !bytes.Contains(data, []byte("Exif\x00\x00")) || !bytes.Contains(data, []byte("GPSMARKER")) {
		t.Fatal("sample lacks EXIF")
	}
	p, _ := newTestPipeline(t)
	res, err := p.Process(context.Background(), "gps.jpg", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{res.StorageKey, *res.ThumbKey} {
		out := readBlob(t, p, key)
		if bytes.Contains(out, []byte("Exif")) || bytes.Contains(out, []byte("GPSMARKER")) || hasJPEGMarker(out, 0xE1) {
			t.Errorf("%s still has EXIF/APP1", key)
		}
	}
	// Orientation 6 rotates 90 degrees: 40x20 becomes 20x40.
	if *res.Width != 20 || *res.Height != 40 {
		t.Errorf("orientation not applied: %dx%d", *res.Width, *res.Height)
	}
}

// hasJPEGMarker walks JPEG segments before SOS looking for marker m.
func hasJPEGMarker(b []byte, m byte) bool {
	i := 2
	for i+4 <= len(b) && b[i] == 0xFF {
		marker := b[i+1]
		if marker == m {
			return true
		}
		if marker == 0xDA { // SOS: entropy data follows
			return false
		}
		i += 2 + int(b[i+2])<<8 + int(b[i+3])
	}
	return false
}

func TestThumbnail(t *testing.T) {
	p, _ := newTestPipeline(t)
	res, err := p.Process(context.Background(), "wide.png", bytes.NewReader(pngBytes(t, 1000, 500)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(readBlob(t, p, *res.ThumbKey)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 480 || cfg.Height != 240 {
		t.Errorf("thumb %dx%d", cfg.Width, cfg.Height)
	}

	res, err = p.Process(context.Background(), "tall.jpg", bytes.NewReader(jpegBytes(t, 300, 2000)))
	if err != nil {
		t.Fatal(err)
	}
	thumb := readBlob(t, p, *res.ThumbKey)
	cfg, format, _ := image.DecodeConfig(bytes.NewReader(thumb))
	if format != "jpeg" || cfg.Height != 480 || cfg.Width != 72 {
		t.Errorf("thumb %s %dx%d", format, cfg.Width, cfg.Height)
	}

	// Never upscale: small image's thumbnail keeps its size.
	res, err = p.Process(context.Background(), "small.png", bytes.NewReader(pngBytes(t, 30, 20)))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, _ = image.DecodeConfig(bytes.NewReader(readBlob(t, p, *res.ThumbKey)))
	if cfg.Width != 30 || cfg.Height != 20 {
		t.Errorf("small thumb upscaled to %dx%d", cfg.Width, cfg.Height)
	}
}

func TestSizeLimit(t *testing.T) {
	p, _ := newTestPipeline(t)
	p.MaxSize = 1024
	text := bytes.Repeat([]byte("a"), 1024)
	if _, err := p.Process(context.Background(), "ok.txt", bytes.NewReader(text)); err != nil {
		t.Fatalf("exactly MaxSize must pass: %v", err)
	}
	_, err := p.Process(context.Background(), "big.txt", bytes.NewReader(append(text, 'b')))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
	// An endless stream is cut off without reading it all.
	_, err = p.Process(context.Background(), "inf.txt", infiniteReader{})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
	assertTempEmpty(t, p)
}

type infiniteReader struct{}

func (infiniteReader) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = 'x'
	}
	return len(b), nil
}

func TestContextCancelled(t *testing.T) {
	p, _ := newTestPipeline(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Process(ctx, "a.txt", strings.NewReader("hello"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	assertTempEmpty(t, p)
}

func TestTextAcrossChunkBoundaries(t *testing.T) {
	// Multi-byte runes straddling every 32 KiB read boundary must validate.
	var b strings.Builder
	for b.Len() < 200*1024 {
		b.WriteString("aé€😀")
	}
	p, _ := newTestPipeline(t)
	if _, err := p.Process(context.Background(), "big.txt", strings.NewReader(b.String())); err != nil {
		t.Fatal(err)
	}
}

func TestDeduplication(t *testing.T) {
	p, _ := newTestPipeline(t)
	r1, err := p.Process(context.Background(), "one.pdf", bytes.NewReader(pdfBytes))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := p.Process(context.Background(), "two.pdf", bytes.NewReader(pdfBytes))
	if err != nil {
		t.Fatal(err)
	}
	if r1.StorageKey != r2.StorageKey || r1.Filename == r2.Filename {
		t.Errorf("keys %s %s names %s %s", r1.StorageKey, r2.StorageKey, r1.Filename, r2.Filename)
	}
	img := pngBytes(t, 50, 50)
	i1, _ := p.Process(context.Background(), "a.png", bytes.NewReader(img))
	i2, _ := p.Process(context.Background(), "b.png", bytes.NewReader(img))
	if i1.StorageKey != i2.StorageKey || *i1.ThumbKey != *i2.ThumbKey {
		t.Error("identical images must share blobs")
	}
	root := p.Store.(*FSStore).Root
	n := 0
	filepath.WalkDir(root, func(_ string, d os.DirEntry, _ error) error {
		if !d.IsDir() {
			n++
		}
		return nil
	})
	if n != 2 { // pdf + png (thumbnail of a 50x50 PNG is byte-identical to the full image)
		t.Errorf("want 2 blobs on disk, got %d", n)
	}
}

func TestMisconfiguredPipeline(t *testing.T) {
	if _, err := (&Pipeline{}).Process(context.Background(), "a.txt", strings.NewReader("x")); err == nil {
		t.Fatal("expected error")
	}
}

func TestAllowedContentTypes(t *testing.T) {
	got := AllowedContentTypes()
	for _, ct := range []string{"image/png", "image/jpeg", "application/pdf", "text/plain; charset=utf-8", mimeODP} {
		if !got[ct] {
			t.Errorf("missing %s", ct)
		}
	}
	for _, ct := range []string{"image/gif", "image/webp", "image/svg+xml", "text/html"} {
		if got[ct] {
			t.Errorf("%s must never be produced", ct)
		}
	}
}
