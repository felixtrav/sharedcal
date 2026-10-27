package attachments

import (
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	// MaxFilenameRunes is the maximum length of a sanitized display filename.
	MaxFilenameRunes = 200
	// maxPreservedExtRunes bounds how long an extension may be and still be
	// kept intact when a long name is truncated.
	maxPreservedExtRunes = 16
	fallbackFilename     = "file"
)

// SanitizeFilename turns a client-supplied filename into safe display
// metadata. The result is never used as a filesystem path (blobs are stored
// by hash), but it is shown in the UI and sent in Content-Disposition, so it
// must not carry path components, control characters or bidi overrides that
// could disguise the real extension (e.g. "invoice‮fdp.exe").
//
// Steps: drop invalid UTF-8, keep only the last path component (both '/' and
// '\' separators), remove control and bidi-control characters, NFC-normalize,
// trim spaces and dots, truncate to MaxFilenameRunes runes (keeping a short
// extension intact), and fall back to "file" when nothing is left.
func SanitizeFilename(name string) string {
	name = strings.ToValidUTF8(name, "")
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(dropUnsafeRune, name)
	// Normalize after removing characters so the result is stable (removal
	// can bring a base letter and a combining mark together). NFC never
	// produces '/' or '\', so path stripping above cannot be bypassed.
	name = norm.NFC.String(name)
	name = trimName(name)
	name = truncateName(name, MaxFilenameRunes)
	if name == "" {
		return fallbackFilename
	}
	return name
}

// dropUnsafeRune removes control characters, bidi embedding/override/isolate
// controls and other invisible direction marks.
func dropUnsafeRune(r rune) rune {
	switch {
	case unicode.IsControl(r):
		return -1
	case r >= 0x202A && r <= 0x202E, // LRE, RLE, PDF, LRO, RLO
		r >= 0x2066 && r <= 0x2069, // LRI, RLI, FSI, PDI
		r == 0x200E, r == 0x200F,   // LRM, RLM
		r == 0x061C, // ALM
		r == 0xFEFF, // BOM / ZWNBSP
		r == utf8.RuneError:
		return -1
	}
	return r
}

// trimName removes leading/trailing whitespace and dots. Trailing dots and
// spaces are silently dropped by Windows, so "x.exe." would otherwise show
// one extension and save as another; leading dots would create hidden files.
func trimName(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r == '.' || unicode.IsSpace(r) })
}

// truncateName limits s to max runes, never splitting a rune. When the name
// has a short extension, the stem is shortened instead so the extension (which
// drives type checking) survives.
func truncateName(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	ext := path.Ext(s)
	extRunes := utf8.RuneCountInString(ext)
	if ext == s || extRunes > maxPreservedExtRunes {
		ext, extRunes = "", 0
	}
	stem := []rune(strings.TrimSuffix(s, ext))
	stem = stem[:max-extRunes]
	return trimName(string(stem)) + ext
}

// replaceExt swaps the extension of a sanitized filename, used when an image
// is re-encoded into a different format (e.g. photo.webp -> photo.png).
func replaceExt(name, newExt string) string {
	stem := strings.TrimSuffix(name, path.Ext(name))
	if stem == "" {
		stem = fallbackFilename
	}
	return truncateName(stem+newExt, MaxFilenameRunes)
}
