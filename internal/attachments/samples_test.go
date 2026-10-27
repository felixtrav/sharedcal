package attachments

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// Minimal real samples generated in-process.

func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	return img
}

func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage(w, h)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testImage(w, h), &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func gifBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White, color.NRGBA{255, 0, 0, 255}}
	frame := func(c uint8) *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for i := range p.Pix {
			p.Pix[i] = c
		}
		return p
	}
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{frame(2), frame(1)}, Delay: []int{10, 10}})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// webpBytes is a 1x1 lossless WebP (VP8L), there being no Go WebP encoder.
func webpBytes(t testing.TB) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// jpegWithExif inserts an APP1 Exif segment (orientation=6, plus a GPS IFD
// with a latitude ref) right after the SOI marker.
func jpegWithExif(t testing.TB, w, h int) []byte {
	t.Helper()
	src := jpegBytes(t, w, h)
	le := binary.LittleEndian
	var tiff bytes.Buffer
	tiff.WriteString("II*\x00")
	binary.Write(&tiff, le, uint32(8)) // IFD0 offset
	// IFD0: 2 entries
	binary.Write(&tiff, le, uint16(2))
	writeEntry := func(tag, typ uint16, count, value uint32) {
		binary.Write(&tiff, le, tag)
		binary.Write(&tiff, le, typ)
		binary.Write(&tiff, le, count)
		binary.Write(&tiff, le, value)
	}
	writeEntry(0x0112, 3, 1, 6)           // Orientation = 6 (rotate 90 CW)
	writeEntry(0x8825, 4, 1, 8+2+2*12+4)  // GPS IFD pointer
	binary.Write(&tiff, le, uint32(0))    // next IFD
	binary.Write(&tiff, le, uint16(1))    // GPS IFD: 1 entry
	writeEntry(0x0001, 2, 2, uint32('N')) // GPSLatitudeRef "N"
	binary.Write(&tiff, le, uint32(0))
	tiff.WriteString("GPSMARKER-48.8566N-2.3522E")

	payload := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	var out bytes.Buffer
	out.Write(src[:2]) // SOI
	out.Write([]byte{0xFF, 0xE1})
	binary.Write(&out, binary.BigEndian, uint16(len(payload)+2))
	out.Write(payload)
	out.Write(src[2:])
	return out.Bytes()
}

// pngHeaderOnly returns a PNG signature + IHDR (valid CRC) claiming w x h.
func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	// a tiny IDAT so the file looks complete-ish
	idat := append([]byte("IDAT"), 0x78, 0x9c, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01)
	binary.Write(&b, binary.BigEndian, uint32(len(idat)-4))
	b.Write(idat)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(idat))
	return b.Bytes()
}

var pdfBytes = []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n")

type zipEntry struct {
	name, body string
	store      bool
}

// zipBytes builds a zip. Stored entries are written raw (no data descriptor,
// no extra fields) so the ODF "mimetype" entry sits at offset 30 exactly as
// LibreOffice writes it.
func zipBytes(t testing.TB, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		if e.store {
			hdr := &zip.FileHeader{
				Name: e.name, Method: zip.Store,
				CRC32:              crc32.ChecksumIEEE([]byte(e.body)),
				CompressedSize64:   uint64(len(e.body)),
				UncompressedSize64: uint64(len(e.body)),
			}
			w, err := zw.CreateRaw(hdr)
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte(e.body))
			continue
		}
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const contentTypesXML = `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`

func ooxmlBytes(t testing.TB, mainPart string) []byte {
	return zipBytes(t, []zipEntry{
		{name: "[Content_Types].xml", body: contentTypesXML},
		{name: "_rels/.rels", body: "<Relationships/>"},
		{name: mainPart, body: "<doc/>"},
	})
}

func odfBytes(t testing.TB, mime string) []byte {
	return zipBytes(t, []zipEntry{
		{name: "mimetype", body: mime, store: true},
		{name: "META-INF/manifest.xml", body: "<manifest/>"},
		{name: "content.xml", body: "<office:document-content/>"},
	})
}
