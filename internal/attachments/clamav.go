package attachments

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// Scanner scans a stream for malware.
type Scanner interface {
	Scan(ctx context.Context, r io.Reader) (Verdict, error)
}

// Verdict is the outcome of a scan. Signature is set when Clean is false.
type Verdict struct {
	Clean     bool
	Signature string
}

const (
	clamChunkSize   = 32 * 1024
	clamMaxReply    = 4096
	defaultClamWait = 2 * time.Minute
)

// ClamAV is a clamd client speaking the INSTREAM protocol over TCP.
type ClamAV struct {
	Addr    string
	Timeout time.Duration // overall deadline per scan, combined with the context's
}

// NewClamAV returns a Scanner for clamd at addr ("host:port"). timeout bounds
// each scan (connect + stream + verdict); 0 means 2 minutes.
func NewClamAV(addr string, timeout time.Duration) Scanner {
	if timeout <= 0 {
		timeout = defaultClamWait
	}
	return &ClamAV{Addr: addr, Timeout: timeout}
}

// Scan streams r to clamd: "zINSTREAM\0", then chunks each prefixed with a
// 4-byte big-endian length, then a zero-length terminator. The reply is
// NUL-terminated: "stream: OK", "stream: <sig> FOUND" or "... ERROR".
// An error (including timeouts) never yields a clean verdict.
func (c *ClamAV) Scan(ctx context.Context, r io.Reader) (Verdict, error) {
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", c.Addr)
	if err != nil {
		return Verdict{}, fmt.Errorf("attachments: clamd connect: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// Unblock reads/writes promptly if the context is cancelled.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	sendErr := c.send(conn, ctx, r)
	// clamd may reply early (e.g. size limit exceeded) and close the
	// connection, making our write fail; always try to read its reply.
	reply, readErr := readReply(conn)
	if readErr != nil {
		if ctxErr := ctxError(ctx, readErr); ctxErr != nil {
			return Verdict{}, fmt.Errorf("attachments: clamd scan: %w", ctxErr)
		}
		if sendErr != nil {
			return Verdict{}, fmt.Errorf("attachments: clamd send: %w", sendErr)
		}
		return Verdict{}, fmt.Errorf("attachments: clamd reply: %w", readErr)
	}
	v, perr := parseReply(reply)
	if perr == nil && sendErr != nil {
		// A verdict on a stream we could not finish sending is not trustworthy.
		return Verdict{}, fmt.Errorf("attachments: clamd send: %w", sendErr)
	}
	return v, perr
}

// ctxError reports the context error behind a failed read. The connection
// deadline equals the context deadline and may fire a moment before the
// context itself is marked done, so a net timeout counts as DeadlineExceeded.
func ctxError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return context.DeadlineExceeded
	}
	return nil
}

func (c *ClamAV) send(conn net.Conn, ctx context.Context, r io.Reader) error {
	w := bufio.NewWriterSize(conn, clamChunkSize+4)
	if _, err := w.WriteString("zINSTREAM\x00"); err != nil {
		return err
	}
	buf := make([]byte, clamChunkSize)
	var hdr [4]byte
	for {
		n, err := ctxReader{ctx, r}.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(hdr[:], uint32(n))
			if _, werr := w.Write(hdr[:]); werr != nil {
				return werr
			}
			if _, werr := w.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
	}
	binary.BigEndian.PutUint32(hdr[:], 0)
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	return w.Flush()
}

// readReply reads one NUL-terminated (or EOF-terminated) reply, bounded.
func readReply(conn net.Conn) (string, error) {
	data, err := io.ReadAll(io.LimitReader(conn, clamMaxReply))
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(data) == 0 {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// parseReply interprets clamd's verdict line.
func parseReply(reply string) (Verdict, error) {
	switch {
	case strings.Contains(reply, "size limit exceeded"):
		return Verdict{}, ErrScanTooLarge
	case strings.HasSuffix(reply, " ERROR"):
		return Verdict{}, fmt.Errorf("attachments: clamd error: %s", reply)
	case reply == "stream: OK":
		return Verdict{Clean: true}, nil
	case strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND"):
		sig := strings.TrimSuffix(strings.TrimPrefix(reply, "stream: "), " FOUND")
		return Verdict{Clean: false, Signature: sig}, nil
	}
	return Verdict{}, fmt.Errorf("attachments: unexpected clamd reply %q", reply)
}
