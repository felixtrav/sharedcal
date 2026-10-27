package realtime

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

// Presence states.
const (
	PresenceViewing = "viewing"
	PresenceEditing = "editing"
	PresenceLeft    = "left"
)

// PresenceTTL is how long a heartbeat keeps a viewer visible.
const PresenceTTL = 45 * time.Second

// Viewer is one user looking at an event's panel.
type Viewer struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl"`
	State     string `json:"state"`
}

type presenceEntry struct {
	viewer  Viewer
	expires time.Time
}

// Presence is in-memory "who is viewing which event" state. It is never
// persisted and expires after PresenceTTL of silence.
type Presence struct {
	mu     sync.Mutex
	events map[string]map[string]presenceEntry // eventID → userID → entry
	broker Broker
	now    func() time.Time
}

// NewPresence returns a presence tracker publishing through broker.
func NewPresence(b Broker) *Presence {
	return &Presence{events: map[string]map[string]presenceEntry{}, broker: b, now: time.Now}
}

// Update records a heartbeat and publishes the event's viewer list when it
// changed.
func (p *Presence) Update(eventID string, v Viewer) {
	p.mu.Lock()
	users := p.events[eventID]
	prev, existed := users[v.ID]
	changed := false
	if v.State == PresenceLeft {
		if existed {
			delete(users, v.ID)
			if len(users) == 0 {
				delete(p.events, eventID)
			}
			changed = true
		}
	} else {
		if users == nil {
			users = map[string]presenceEntry{}
			p.events[eventID] = users
		}
		users[v.ID] = presenceEntry{viewer: v, expires: p.now().Add(PresenceTTL)}
		changed = !existed || prev.viewer.State != v.State
	}
	var list []Viewer
	if changed {
		list = p.listLocked(eventID)
	}
	p.mu.Unlock()
	if changed {
		p.publish(eventID, list)
	}
}

// Viewers returns the current viewers of an event.
func (p *Presence) Viewers(eventID string) []Viewer {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.listLocked(eventID)
}

func (p *Presence) listLocked(eventID string) []Viewer {
	list := make([]Viewer, 0, len(p.events[eventID]))
	for _, e := range p.events[eventID] {
		list = append(list, e.viewer)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// Run expires stale entries until ctx is done.
func (p *Presence) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.sweep()
		}
	}
}

func (p *Presence) sweep() {
	now := p.now()
	changed := map[string][]Viewer{}
	p.mu.Lock()
	for eventID, users := range p.events {
		dirty := false
		for id, e := range users {
			if now.After(e.expires) {
				delete(users, id)
				dirty = true
			}
		}
		if dirty {
			changed[eventID] = p.listLocked(eventID)
		}
		if len(users) == 0 {
			delete(p.events, eventID)
		}
	}
	p.mu.Unlock()
	for eventID, list := range changed {
		p.publish(eventID, list)
	}
}

func (p *Presence) publish(eventID string, viewers []Viewer) {
	payload, _ := json.Marshal(struct {
		EventID string   `json:"eventId"`
		Viewers []Viewer `json:"viewers"`
	}{eventID, viewers})
	p.broker.Publish(Message{Type: "presence", ID: eventID, Payload: payload})
}
