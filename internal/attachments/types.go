package attachments

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/gabriel-vasile/mimetype"
)

// kind groups allowed types by how the pipeline treats them.
type kind int

const (
	kindImage  kind = iota + 1 // decoded and re-encoded
	kindText                   // validated as UTF-8, stored as-is
	kindOpaque                 // PDF: stored as-is, never parsed
	kindOOXML                  // zip container, structure checked, never parsed
	kindODF                    // zip container, structure checked, never parsed
)

// fileType is one allow-list entry, keyed by lower-case extension.
type fileType struct {
	kind kind
	// detected lists the MIME types mimetype may report for this extension.
	detected []string
	// contentType is what we store and serve (for images: of the source;
	// the re-encoded output type is decided in image.go).
	contentType string
	// mainPart is the required zip entry for OOXML/ODF containers.
	mainPart string
}

// textDetections are the mimetype results accepted for text files. Anything
// more specific (HTML, XML, SVG, JS, JSON, scripts, vCard, ...) is refused so
// that markup can never be uploaded under a text extension.
var textDetections = []string{"text/plain", "text/csv", "text/tab-separated-values"}

// allowList is the v1 allow-list from the spec. Everything else is refused.
var allowList = map[string]fileType{
	".png":  {kind: kindImage, detected: []string{"image/png", "image/apng"}, contentType: "image/png"},
	".jpg":  {kind: kindImage, detected: []string{"image/jpeg"}, contentType: "image/jpeg"},
	".jpeg": {kind: kindImage, detected: []string{"image/jpeg"}, contentType: "image/jpeg"},
	".gif":  {kind: kindImage, detected: []string{"image/gif"}, contentType: "image/gif"},
	".webp": {kind: kindImage, detected: []string{"image/webp"}, contentType: "image/webp"},

	".pdf": {kind: kindOpaque, detected: []string{"application/pdf"}, contentType: "application/pdf"},

	".txt":      {kind: kindText, detected: textDetections, contentType: "text/plain; charset=utf-8"},
	".md":       {kind: kindText, detected: textDetections, contentType: "text/markdown; charset=utf-8"},
	".markdown": {kind: kindText, detected: textDetections, contentType: "text/markdown; charset=utf-8"},
	".csv":      {kind: kindText, detected: textDetections, contentType: "text/csv; charset=utf-8"},
	".ics":      {kind: kindText, detected: []string{"text/calendar", "text/plain"}, contentType: "text/calendar; charset=utf-8"},

	".docx": {kind: kindOOXML, detected: []string{mimeDOCX}, contentType: mimeDOCX, mainPart: "word/document.xml"},
	".xlsx": {kind: kindOOXML, detected: []string{mimeXLSX}, contentType: mimeXLSX, mainPart: "xl/workbook.xml"},
	".pptx": {kind: kindOOXML, detected: []string{mimePPTX}, contentType: mimePPTX, mainPart: "ppt/presentation.xml"},
	".odt":  {kind: kindODF, detected: []string{mimeODT}, contentType: mimeODT, mainPart: "content.xml"},
	".ods":  {kind: kindODF, detected: []string{mimeODS}, contentType: mimeODS, mainPart: "content.xml"},
	".odp":  {kind: kindODF, detected: []string{mimeODP}, contentType: mimeODP, mainPart: "content.xml"},
}

const (
	mimeDOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	mimeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	mimePPTX = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	mimeODT  = "application/vnd.oasis.opendocument.text"
	mimeODS  = "application/vnd.oasis.opendocument.spreadsheet"
	mimeODP  = "application/vnd.oasis.opendocument.presentation"
)

// classify checks the detected MIME type against the allow-list entry for
// ext. It distinguishes "allowed type, wrong extension" (ErrTypeMismatch) from
// "type not allowed at all" (ErrTypeNotAllowed) so users get a useful error.
func classify(ext string, detected *mimetype.MIME) (fileType, error) {
	ft, ok := allowList[ext]
	if !ok {
		return fileType{}, ErrTypeNotAllowed
	}
	if detectedIn(detected, ft.detected) {
		return ft, nil
	}
	for _, other := range allowList {
		if detectedIn(detected, other.detected) {
			return fileType{}, ErrTypeMismatch
		}
	}
	return fileType{}, ErrTypeNotAllowed
}

func detectedIn(m *mimetype.MIME, types []string) bool {
	for _, t := range types {
		if m.Is(t) {
			return true
		}
	}
	return false
}

// detect sniffs the real type from the leading bytes of f.
func detect(f *os.File) (*mimetype.MIME, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return mimetype.DetectReader(f)
}

// validateText scans the whole file (the sniffer only looks at the head):
// it must be valid UTF-8 and contain no NUL bytes. UTF-16 is refused by
// the NUL check. The decoder is streaming so large files are not buffered.
func validateText(f *os.File) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	buf := make([]byte, 32*1024)
	var carry []byte
	for {
		n, err := f.Read(buf[len(carry):])
		chunk := buf[:len(carry)+n]
		if bytes.IndexByte(chunk[len(carry):], 0) >= 0 {
			return ErrCorrupt
		}
		valid, rest := splitIncompleteRune(chunk)
		if !utf8.Valid(valid) {
			return ErrCorrupt
		}
		carry = append(buf[:0:0], rest...)
		copy(buf, carry)
		if err == io.EOF {
			if len(carry) > 0 {
				return ErrCorrupt
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// splitIncompleteRune separates a possibly truncated trailing UTF-8 sequence
// (at most 3 bytes) so it can be revalidated with the next chunk.
func splitIncompleteRune(b []byte) (valid, rest []byte) {
	for i := 1; i <= utf8.UTFMax-1 && i <= len(b); i++ {
		c := b[len(b)-i]
		if c < 0x80 { // ASCII: nothing pending
			return b, nil
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i], b[len(b)-i:]
			}
			return b, nil
		}
	}
	return b, nil
}

// zipMarkersForbidden are entries that betray a Java/Android/e-book archive
// dressed up as an office document.
var zipMarkersForbidden = []string{
	"META-INF/MANIFEST.MF", "AndroidManifest.xml", "classes.dex", "META-INF/container.xml",
}

// validateContainer performs a structural check of OOXML/ODF zip containers
// on top of the magic-byte sniff, which only looks at the first few local
// file headers. It reads only the central directory (and, for ODF, the tiny
// stored "mimetype" entry); document contents are never decompressed or
// parsed, matching the spec's "never parsed server-side" rule.
func validateContainer(f *os.File, size int64, ft fileType) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return ErrCorrupt
	}
	names := make(map[string]*zip.File, len(zr.File))
	for _, zf := range zr.File {
		names[zf.Name] = zf
	}
	for _, bad := range zipMarkersForbidden {
		if _, ok := names[bad]; ok {
			return ErrTypeNotAllowed
		}
	}
	if _, ok := names[ft.mainPart]; !ok {
		return ErrCorrupt
	}
	switch ft.kind {
	case kindOOXML:
		if _, ok := names["[Content_Types].xml"]; !ok {
			return ErrCorrupt
		}
	case kindODF:
		return checkODFMimetype(zr, ft.contentType)
	}
	return nil
}

// checkODFMimetype verifies the ODF rule that the first entry is an
// uncompressed "mimetype" file holding exactly the media type.
func checkODFMimetype(zr *zip.Reader, want string) error {
	if len(zr.File) == 0 {
		return ErrCorrupt
	}
	first := zr.File[0]
	if first.Name != "mimetype" || first.Method != zip.Store || first.UncompressedSize64 > 128 {
		return ErrCorrupt
	}
	rc, err := first.Open()
	if err != nil {
		return ErrCorrupt
	}
	defer rc.Close()
	got, err := io.ReadAll(io.LimitReader(rc, 129))
	if err != nil || string(got) != want {
		return fmt.Errorf("%w: ODF mimetype entry %q", ErrCorrupt, got)
	}
	return nil
}
