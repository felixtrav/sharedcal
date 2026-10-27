package realtime

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrokerDropsSlowSubscribers(t *testing.T) {
	b := NewLocal(2)
	slow := b.Subscribe()
	fast := b.Subscribe()
	defer fast.Close()
	for i := 0; i < 3; i++ {
		b.Publish(Message{Seq: int64(i + 1), Type: "x"})
		<-fast.C
	}
	if b.Clients() != 1 {
		t.Fatalf("clients: %d", b.Clients())
	}
	n := 0
	for range slow.C {
		n++
	}
	if n != 2 {
		t.Fatalf("slow got %d before being closed", n)
	}
	slow.Close() // idempotent
}

func TestPresenceExpiry(t *testing.T) {
	b := NewLocal(16)
	sub := b.Subscribe()
	defer sub.Close()
	p := NewPresence(b)
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	p.Update("e1", Viewer{ID: "u1", State: PresenceViewing})
	if m := <-sub.C; m.Type != "presence" {
		t.Fatalf("type %s", m.Type)
	}
	p.Update("e1", Viewer{ID: "u1", State: PresenceViewing}) // heartbeat: no new message
	select {
	case m := <-sub.C:
		t.Fatalf("unchanged heartbeat published: %s", m.Payload)
	default:
	}
	p.Update("e1", Viewer{ID: "u1", State: PresenceEditing})
	<-sub.C
	now = now.Add(PresenceTTL + time.Second)
	p.sweep()
	m := <-sub.C
	var payload struct {
		Viewers []Viewer `json:"viewers"`
	}
	_ = json.Unmarshal(m.Payload, &payload)
	if len(payload.Viewers) != 0 || len(p.Viewers("e1")) != 0 {
		t.Fatalf("not expired: %s", m.Payload)
	}
}

func TestStreamAudienceAndRevocation(t *testing.T) {
	b := NewLocal(16)
	s := &Stream{Broker: b,
		Replay:       func(context.Context, int64) ([]Message, bool, error) { return nil, true, nil },
		SessionValid: func(context.Context, Conn) bool { return true },
		Heartbeat:    time.Hour, SessionCheck: time.Hour}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = s.Serve(w, r, Conn{UserID: "u1", SessionHash: "h1"})
	}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %s", ct)
	}
	for b.Clients() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	b.Publish(Message{Seq: 1, Type: "inbox.item", Payload: json.RawMessage(`{"for":"u2"}`), Audience: "u2"})
	b.Publish(Message{Seq: 2, Type: "inbox.item", Payload: json.RawMessage(`{"for":"u1"}`), Audience: "u1"})
	b.Publish(Message{Seq: 2, Type: "dup", Payload: json.RawMessage(`{}`)}) // duplicate seq dropped
	sc := bufio.NewScanner(resp.Body)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if strings.Contains(sc.Text(), `"for":"u1"`) {
			break
		}
	}
	// Revoking another session leaves the stream open; revoking ours closes it.
	b.Publish(Message{Type: TypeSessionsRevoked, Payload: json.RawMessage(`["other"]`)})
	b.Publish(Message{Seq: 3, Type: "after", Payload: json.RawMessage(`{}`)})
	b.Publish(Message{Type: TypeSessionsRevoked, Payload: json.RawMessage(`["h1"]`)})
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	out := strings.Join(lines, "\n")
	if strings.Contains(out, `"for":"u2"`) || !strings.Contains(out, `"for":"u1"`) || strings.Contains(out, "event: dup") {
		t.Fatalf("stream output:\n%s", out)
	}
	if !strings.Contains(out, "id: 2") {
		t.Fatalf("missing id line:\n%s", out)
	}
}

func TestStreamPerUserLimit(t *testing.T) {
	s := &Stream{MaxPerUser: 1}
	s.defaults()
	if !s.acquire("u") || s.acquire("u") {
		t.Fatal("limit not enforced")
	}
	s.release("u")
	if !s.acquire("u") {
		t.Fatal("release did not free a slot")
	}
}
