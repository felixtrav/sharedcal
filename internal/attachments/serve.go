package attachments

import (
	"net/http"
	"strings"
)

// CacheControl is sent with every attachment response. "private" keeps
// shared proxies from storing files that are behind a session check; one
// hour is long enough for thumbnail strips and lightboxes to be cheap while
// bounding how long a browser keeps a file after it was deleted or access was
// revoked. Blobs are immutable, so staleness is never a correctness problem.
const CacheControl = "private, max-age=3600"

// ServeHeaders sets every response header the spec requires for serving an
// attachment or thumbnail. contentType must be the stored, server-detected
// type (never the client's). inline must be true only for re-encoded images
// and thumbnails; everything else is forced to download.
func ServeHeaders(h http.Header, contentType, filename string, inline bool) {
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("Content-Type", contentType)
	h.Set("Cache-Control", CacheControl)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	h.Set("Content-Disposition", ContentDisposition(disposition, filename))
}

// ContentDisposition builds an RFC 6266 header value with an ASCII fallback
// "filename" and a UTF-8 percent-encoded "filename*" (RFC 8187). The name is
// re-sanitized so control characters can never reach the header.
func ContentDisposition(disposition, filename string) string {
	name := SanitizeFilename(filename)
	return disposition + `; filename="` + asciiFallback(name) + `"; filename*=UTF-8''` + encodeRFC8187(name)
}

// asciiFallback replaces anything that is not safe inside a quoted-string
// for old clients: non-ASCII, quotes, backslashes and '%' (which some
// browsers would percent-decode) become '_'.
func asciiFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r >= 0x7F, r == '"', r == '\\', r == '%':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// encodeRFC8187 percent-encodes every byte that is not an RFC 8187 attr-char.
func encodeRFC8187(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAttrChar(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&0x0F])
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
