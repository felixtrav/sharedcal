package ics

import (
	"bytes"
	"io"
	"unicode/utf8"
)

// maxLineOctets is the RFC 5545 §3.1 limit, excluding the CRLF.
const maxLineOctets = 75

// foldingWriter folds content lines at 75 octets. go-ical does not fold, so
// every encoded line passes through here. Folds are inserted only at UTF-8
// rune boundaries so multi-byte characters (emoji, CJK) are never split, and
// each continuation line starts with a single space (which counts toward its
// 75 octets).
type foldingWriter struct {
	w   io.Writer
	buf bytes.Buffer // pending, not yet terminated line
	err error
}

func (f *foldingWriter) Write(p []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.buf.Write(p)
	for {
		line, ok := f.nextLine()
		if !ok {
			break
		}
		if err := f.writeFolded(line); err != nil {
			f.err = err
			return 0, err
		}
	}
	return len(p), nil
}

// nextLine pops one CRLF-terminated line (without the CRLF) from buf.
func (f *foldingWriter) nextLine() ([]byte, bool) {
	b := f.buf.Bytes()
	i := bytes.Index(b, []byte("\r\n"))
	if i < 0 {
		return nil, false
	}
	line := append([]byte(nil), b[:i]...)
	f.buf.Next(i + 2)
	return line, true
}

func (f *foldingWriter) writeFolded(line []byte) error {
	var out bytes.Buffer
	limit := maxLineOctets
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		out.Write(line[:cut])
		out.WriteString("\r\n ")
		line = line[cut:]
		limit = maxLineOctets - 1 // continuation lines begin with a space
	}
	out.Write(line)
	out.WriteString("\r\n")
	_, err := f.w.Write(out.Bytes())
	return err
}

// flush writes any unterminated trailing data (go-ical always terminates
// lines, so this is defensive).
func (f *foldingWriter) flush() error {
	if f.err != nil || f.buf.Len() == 0 {
		return f.err
	}
	line := append([]byte(nil), f.buf.Bytes()...)
	f.buf.Reset()
	return f.writeFolded(line)
}
