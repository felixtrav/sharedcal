package realtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Viewer identity of one SSE connection.
type Conn struct {
	UserID      string
	SessionHash string
}

// ReplayFunc returns every message after seq that the log still holds.
// ok is false when seq is older than the oldest retained row, in which case
// the client must resync.
type ReplayFunc func(ctx context.Context, after int64) (msgs []Message, ok bool, err error)

// PersonalizeFunc adapts a payload for one recipient (e.g. computes the
// permissions object). Returning false skips the message for that user.
type PersonalizeFunc func(ctx context.Context, m Message, c Conn) (json.RawMessage, bool)

// SessionCheckFunc reports whether a connection's session is still valid.
type SessionCheckFunc func(ctx context.Context, c Conn) bool

// Stream serves /api/stream.
type Stream struct {
	Broker       Broker
	Replay       ReplayFunc
	Personalize  PersonalizeFunc
	SessionValid SessionCheckFunc
	Log          *slog.Logger

	Heartbeat     time.Duration // default 25s
	SessionCheck  time.Duration // default 60s
	WriteTimeout  time.Duration // default 10s
	MaxPerUser    int           // default 20
	MaxReplayRows int           // default 5000

	mu      sync.Mutex
	perUser map[string]int
}

func (s *Stream) defaults() {
	if s.Heartbeat == 0 {
		s.Heartbeat = 25 * time.Second
	}
	if s.SessionCheck == 0 {
		s.SessionCheck = 60 * time.Second
	}
	if s.WriteTimeout == 0 {
		s.WriteTimeout = 10 * time.Second
	}
	if s.MaxPerUser == 0 {
		s.MaxPerUser = 20
	}
	if s.MaxReplayRows == 0 {
		s.MaxReplayRows = 5000
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
}

func (s *Stream) acquire(user string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perUser == nil {
		s.perUser = map[string]int{}
	}
	if s.perUser[user] >= s.MaxPerUser {
		return false
	}
	s.perUser[user]++
	return true
}

func (s *Stream) release(user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.perUser[user]--; s.perUser[user] <= 0 {
		delete(s.perUser, user)
	}
}

// ErrTooManyStreams is returned when a user has too many open streams.
var ErrTooManyStreams = errors.New("too many open streams")

// Serve streams messages to one authenticated connection until the client
// goes away, the session ends, or the subscriber falls behind.
func (s *Stream) Serve(w http.ResponseWriter, r *http.Request, c Conn) error {
	s.defaults()
	if !s.acquire(c.UserID) {
		return ErrTooManyStreams
	}
	defer s.release(c.UserID)

	ctx := r.Context()
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("X-Accel-Buffering", "no") // nginx: never buffer this response
	w.WriteHeader(http.StatusOK)

	bw := bufio.NewWriterSize(w, 8<<10)
	flush := func() error {
		_ = rc.SetWriteDeadline(time.Now().Add(s.WriteTimeout))
		if err := bw.Flush(); err != nil {
			return err
		}
		return rc.Flush()
	}

	// Subscribe before replaying so nothing published in between is lost;
	// duplicates are dropped by sequence number below.
	sub := s.Broker.Subscribe()
	defer sub.Close()

	fmt.Fprint(bw, "retry: 3000\n\n")
	var last int64
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		after, err := strconv.ParseInt(id, 10, 64)
		if err != nil || after < 0 {
			writeResync(bw)
		} else {
			msgs, ok, err := s.Replay(ctx, after)
			switch {
			case err != nil:
				return err
			case !ok || len(msgs) > s.MaxReplayRows:
				writeResync(bw)
			default:
				for _, m := range msgs {
					s.write(ctx, bw, m, c)
					last = m.Seq
				}
			}
			if last == 0 {
				last = after
			}
		}
	}
	if err := flush(); err != nil {
		return nil
	}

	hb := time.NewTicker(s.Heartbeat)
	defer hb.Stop()
	check := time.NewTicker(s.SessionCheck)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-hb.C:
			fmt.Fprint(bw, ": heartbeat\n\n")
			if err := flush(); err != nil {
				return nil
			}
		case <-check.C:
			if !s.SessionValid(ctx, c) {
				return nil
			}
		case m, open := <-sub.C:
			if !open {
				return nil // fell behind; the client reconnects and replays
			}
			switch {
			case m.Type == TypeSessionsRevoked:
				var hashes []string
				if json.Unmarshal(m.Payload, &hashes) == nil && slices.Contains(hashes, c.SessionHash) {
					return nil
				}
			case m.Seq != 0 && m.Seq <= last:
				// Already delivered by the replay.
			default:
				if m.Seq != 0 {
					last = m.Seq
				}
				s.write(ctx, bw, m, c)
			}
			// Coalesce bursts: flush once the queue is drained, whatever
			// the last message was.
			if len(sub.C) == 0 && bw.Buffered() > 0 {
				if err := flush(); err != nil {
					return nil
				}
			}
		}
	}
}

func writeResync(w *bufio.Writer) {
	fmt.Fprint(w, "event: resync\ndata: {}\n\n")
}

func (s *Stream) write(ctx context.Context, w *bufio.Writer, m Message, c Conn) {
	if m.Audience != "" && m.Audience != c.UserID {
		return
	}
	payload := m.Payload
	if s.Personalize != nil {
		p, ok := s.Personalize(ctx, m, c)
		if !ok {
			return
		}
		payload = p
	}
	data, err := json.Marshal(Message{Seq: m.Seq, Type: m.Type, ID: m.ID, Version: m.Version, Payload: payload})
	if err != nil {
		s.Log.ErrorContext(ctx, "sse marshal", "err", err)
		return
	}
	if m.Seq != 0 {
		fmt.Fprintf(w, "id: %d\n", m.Seq)
	}
	// Type names are server-defined constants and JSON never contains raw
	// newlines, so neither can break SSE framing.
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Type, data)
}
