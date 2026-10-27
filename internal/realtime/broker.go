// Package realtime fans out change messages to connected browsers over
// Server-Sent Events.
//
// The Broker interface is the only piece that changes when SharedCal runs as
// several replicas (a PostgreSQL LISTEN/NOTIFY or Redis implementation).
package realtime

import (
	"encoding/json"
	"sync"
)

// Message is one change notification. Seq is the change_log sequence number
// (zero for ephemeral messages such as presence, which are not replayed).
type Message struct {
	Seq     int64           `json:"seq,omitempty"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Version int64           `json:"version,omitempty"`
	Payload json.RawMessage `json:"payload"`
	// Audience restricts delivery to a single user ID; empty means everyone.
	Audience string `json:"-"`
}

// Internal control message types; never sent to browsers.
const (
	// TypeSessionsRevoked carries a JSON array of session hashes whose
	// streams must be closed.
	TypeSessionsRevoked = "_sessions.revoked"
)

// Broker publishes messages to subscribers.
type Broker interface {
	Publish(Message)
	Subscribe() *Subscription
	// Clients returns the number of open subscriptions (for metrics).
	Clients() int
}

// Subscription receives messages until closed. If the subscriber falls too
// far behind, the broker closes C; the client reconnects and catches up from
// the change log using Last-Event-ID.
type Subscription struct {
	C      <-chan Message
	c      chan Message
	broker *Local
	once   sync.Once
}

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() { s.broker.remove(s) }

// Local is the in-process broker for a single container.
type Local struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
	buf  int
}

// NewLocal returns an in-process broker; buf is each subscriber's queue size.
func NewLocal(buf int) *Local {
	if buf <= 0 {
		buf = 256
	}
	return &Local{subs: map[*Subscription]struct{}{}, buf: buf}
}

func (b *Local) Subscribe() *Subscription {
	c := make(chan Message, b.buf)
	s := &Subscription{C: c, c: c, broker: b}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Local) remove(s *Subscription) {
	b.mu.Lock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		s.once.Do(func() { close(s.c) })
	}
	b.mu.Unlock()
}

// Publish never blocks: a subscriber whose queue is full is disconnected.
func (b *Local) Publish(m Message) {
	var slow []*Subscription
	b.mu.RLock()
	for s := range b.subs {
		select {
		case s.c <- m:
		default:
			slow = append(slow, s)
		}
	}
	b.mu.RUnlock()
	for _, s := range slow {
		b.remove(s)
	}
}

func (b *Local) Clients() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
