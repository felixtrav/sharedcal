package attachments

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestSanitizeFilename(t *testing.T) {
	long := strings.Repeat("a", 500) + ".pdf"
	longUni := strings.Repeat("日", 300)
	cases := []struct{ in, want string }{
		{"report.pdf", "report.pdf"},
		{"../../etc/passwd", "passwd"},
		{`..\..\win.ini`, "win.ini"},
		{"C:\\Users\\bob\\file.txt", "file.txt"},
		{"/abs/path/x.png", "x.png"},
		{"dir/", "file"},
		{"..", "file"},
		{"", "file"},
		{"   ", "file"},
		{"\x00\x01\x1f", "file"},
		{"nul\x00byte.txt", "nulbyte.txt"},
		{"tab\tnew\nline\r.txt", "tabnewline.txt"},
		{"invoice\u202Efdp.exe", "invoicefdp.exe"},
		{"a\u2066b\u2069c\u200f.txt", "abc.txt"},
		{"del\x7f.txt", "del.txt"},
		{"c1\u0085ctrl.txt", "c1ctrl.txt"},
		{"  .hidden.txt. . ", "hidden.txt"},
		{"x.exe.", "x.exe"},
		{"cafe\u0301.txt", "café.txt"}, // NFD -> NFC
		{"e\x00\u0301.txt", "é.txt"},   // removal then NFC
		{"bad\xff\xfeutf8.txt", "badutf8.txt"},
		{long, strings.Repeat("a", 196) + ".pdf"},
		{longUni, strings.Repeat("日", 200)},
		{"emoji😀.png", "emoji😀.png"},
	}
	for _, tc := range cases {
		got := SanitizeFilename(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
		checkSanitized(t, tc.in, got)
	}
}

// checkSanitized asserts the invariants of SanitizeFilename output.
func checkSanitized(t testing.TB, in, got string) {
	t.Helper()
	if got == "" || !utf8.ValidString(got) {
		t.Fatalf("%q -> invalid %q", in, got)
	}
	if n := utf8.RuneCountInString(got); n > MaxFilenameRunes {
		t.Fatalf("%q -> %d runes", in, n)
	}
	if strings.ContainsAny(got, `/\`) {
		t.Fatalf("%q -> path separator in %q", in, got)
	}
	for _, r := range got {
		if dropUnsafeRune(r) == -1 {
			t.Fatalf("%q -> unsafe rune %U in %q", in, r, got)
		}
	}
	if trimName(got) != got {
		t.Fatalf("%q -> untrimmed %q", in, got)
	}
	if again := SanitizeFilename(got); again != got {
		t.Fatalf("not idempotent: %q -> %q -> %q", in, got, again)
	}
}

func TestServeHeaders(t *testing.T) {
	h := http.Header{}
	ServeHeaders(h, "image/png", "photo.png", true)
	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"Content-Security-Policy":      "sandbox; default-src 'none'",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Content-Type":                 "image/png",
		"Cache-Control":                "private, max-age=3600",
		"Content-Disposition":          `inline; filename="photo.png"; filename*=UTF-8''photo.png`,
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	h = http.Header{}
	ServeHeaders(h, "application/pdf", `Bericht "Q1" über 100%; final\x.pdf`, false)
	// The backslash is a path separator, so only `x.pdf` remains after sanitization.
	if got := h.Get("Content-Disposition"); got != `attachment; filename="x.pdf"; filename*=UTF-8''x.pdf` {
		t.Errorf("got %q", got)
	}
	h = http.Header{}
	ServeHeaders(h, "application/pdf", `Bericht "Q1" über 100%; ok.pdf`, false)
	wantCD := `attachment; filename="Bericht _Q1_ _ber 100_; ok.pdf"; filename*=UTF-8''Bericht%20%22Q1%22%20%C3%BCber%20100%25%3B%20ok.pdf`
	if got := h.Get("Content-Disposition"); got != wantCD {
		t.Errorf("got  %q\nwant %q", got, wantCD)
	}
	h = http.Header{}
	ServeHeaders(h, "text/plain; charset=utf-8", "evil\r\nSet-Cookie: x=1.txt", false)
	if cd := h.Get("Content-Disposition"); strings.ContainsAny(cd, "\r\n") {
		t.Errorf("header injection: %q", cd)
	}
	h = http.Header{}
	ServeHeaders(h, "image/jpeg", "日本.jpg", true)
	if got := h.Get("Content-Disposition"); got != `inline; filename="__.jpg"; filename*=UTF-8''%E6%97%A5%E6%9C%AC.jpg` {
		t.Errorf("got %q", got)
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestFSStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "att")
	s, err := NewFSStore(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data := []byte("hello blob")
	key := KeyFor(sha(data))
	if err := s.Put(ctx, key, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	// Second put is a no-op and does not even read the reader.
	if err := s.Put(ctx, key, errReader{}); err != nil {
		t.Fatalf("idempotent put: %v", err)
	}
	fi, err := os.Stat(filepath.Join(root, key))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o640 {
		t.Errorf("file mode %v", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Join(root, key[:2]))
	if di.Mode().Perm() != 0o750 {
		t.Errorf("dir mode %v", di.Mode().Perm())
	}
	rc, size, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if size != int64(len(data)) || !bytes.Equal(got, data) {
		t.Errorf("read back %q (%d)", got, size)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("delete must be idempotent: %v", err)
	}
	if _, _, err := s.Open(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Errorf("open deleted: %v", err)
	}

	// Content that does not match the key is refused and leaves no file.
	other := KeyFor(sha([]byte("other")))
	if err := s.Put(ctx, other, bytes.NewReader(data)); err == nil {
		t.Error("hash mismatch accepted")
	}
	ents, _ := os.ReadDir(filepath.Join(root, other[:2]))
	if len(ents) != 0 {
		t.Errorf("leftover files: %v", ents)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("must not be read") }

func TestFSStoreKeyValidation(t *testing.T) {
	s, _ := NewFSStore(t.TempDir())
	good := KeyFor(sha([]byte("x")))
	bad := []string{
		"", "../x", "/etc/passwd", "ab", "ab/",
		"../" + good[3:],
		good[:2] + "/../" + good[6:],
		strings.ToUpper(good),
		"zz/" + strings.Repeat("z", 64),
		good[:66],
		good + "0",
		"00/" + good[3:], // prefix does not match
		good[:2] + `\` + good[3:],
		good[:2] + "/" + good[3:66] + "\x00",
	}
	ctx := context.Background()
	for _, k := range bad {
		if ValidKey(k) {
			t.Errorf("ValidKey(%q) = true", k)
		}
		if err := s.Put(ctx, k, strings.NewReader("x")); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Put(%q): %v", k, err)
		}
		if _, _, err := s.Open(ctx, k); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Open(%q): %v", k, err)
		}
		if err := s.Delete(ctx, k); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("Delete(%q): %v", k, err)
		}
	}
	if !ValidKey(good) {
		t.Error("good key rejected")
	}
}

// fakeClamd accepts one connection, decodes the INSTREAM protocol and
// replies via respond (which receives the reassembled stream).
func fakeClamd(t *testing.T, respond func(conn net.Conn, stream []byte)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		cmd, err := br.ReadString(0)
		if err != nil || cmd != "zINSTREAM\x00" {
			conn.Write([]byte("UNKNOWN COMMAND\x00"))
			return
		}
		var stream []byte
		for {
			var n uint32
			if err := binary.Read(br, binary.BigEndian, &n); err != nil {
				return
			}
			if n == 0 {
				break
			}
			chunk := make([]byte, n)
			if _, err := io.ReadFull(br, chunk); err != nil {
				return
			}
			stream = append(stream, chunk...)
		}
		respond(conn, stream)
	}()
	return ln.Addr().String()
}

func TestClamAV(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), 10_000) // > one chunk
	cases := []struct {
		name    string
		reply   string
		want    Verdict
		wantErr error
	}{
		{"clean", "stream: OK\x00", Verdict{Clean: true}, nil},
		{"infected", "stream: Eicar-Test-Signature FOUND\x00", Verdict{Signature: "Eicar-Test-Signature"}, nil},
		{"error", "stream: Can't allocate memory ERROR\x00", Verdict{}, errAny},
		{"size limit", "INSTREAM size limit exceeded. ERROR\x00", Verdict{}, ErrScanTooLarge},
		{"garbage", "hello\x00", Verdict{}, errAny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := fakeClamd(t, func(conn net.Conn, stream []byte) {
				if !bytes.Equal(stream, payload) {
					conn.Write([]byte("stream: MISMATCH FOUND\x00"))
					return
				}
				conn.Write([]byte(tc.reply))
			})
			v, err := NewClamAV(addr, 5*time.Second).Scan(context.Background(), bytes.NewReader(payload))
			checkScan(t, v, err, tc.want, tc.wantErr)
		})
	}
}

var errAny = errors.New("any error")

func checkScan(t *testing.T, v Verdict, err error, want Verdict, wantErr error) {
	t.Helper()
	switch {
	case wantErr == errAny:
		if err == nil {
			t.Fatalf("want error, got %+v", v)
		}
	case wantErr != nil:
		if !errors.Is(err, wantErr) {
			t.Fatalf("got %v, want %v", err, wantErr)
		}
	default:
		if err != nil {
			t.Fatal(err)
		}
	}
	if v != want {
		t.Fatalf("verdict %+v, want %+v", v, want)
	}
}

func TestClamAVEarlyRejection(t *testing.T) {
	// clamd replies and closes mid-stream when StreamMaxLength is exceeded.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		io.ReadFull(conn, make([]byte, 64*1024))
		conn.Write([]byte("INSTREAM size limit exceeded. ERROR\x00"))
		conn.(*net.TCPConn).CloseWrite()
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	}()
	big := io.LimitReader(infiniteReader{}, 64<<20)
	_, err = NewClamAV(ln.Addr().String(), 5*time.Second).Scan(context.Background(), big)
	if !errors.Is(err, ErrScanTooLarge) {
		t.Fatalf("got %v", err)
	}
}

func TestClamAVTimeout(t *testing.T) {
	addr := fakeClamd(t, func(conn net.Conn, _ []byte) { time.Sleep(2 * time.Second) })
	start := time.Now()
	_, err := NewClamAV(addr, 200*time.Millisecond).Scan(context.Background(), strings.NewReader("x"))
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("expected prompt timeout, got %v after %v", err, time.Since(start))
	}

	addr = fakeClamd(t, func(conn net.Conn, _ []byte) { time.Sleep(2 * time.Second) })
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err = NewClamAV(addr, time.Minute).Scan(ctx, strings.NewReader("x"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %v", err)
	}
}

func TestClamAVConnectError(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	if _, err := NewClamAV(addr, time.Second).Scan(context.Background(), strings.NewReader("x")); err == nil {
		t.Fatal("expected connect error")
	}
}

func TestScanStored(t *testing.T) {
	p, _ := newTestPipeline(t)
	res, err := p.Process(context.Background(), "a.pdf", bytes.NewReader(pdfBytes))
	if err != nil {
		t.Fatal(err)
	}
	addr := fakeClamd(t, func(conn net.Conn, stream []byte) {
		if bytes.Equal(stream, pdfBytes) {
			conn.Write([]byte("stream: OK\x00"))
		}
	})
	v, err := p.ScanStored(context.Background(), NewClamAV(addr, 5*time.Second), res.StorageKey)
	if err != nil || !v.Clean {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err := p.ScanStored(context.Background(), NewClamAV(addr, time.Second), KeyFor(sha([]byte("missing")))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}
