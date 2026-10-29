package domain

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/attachments"
	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
)

// Options configures the domain service.
type Options struct {
	Roles          auth.RoleMapping
	BootstrapEmail string
	BootstrapSub   string
	AppURL         string
	DataDir        string

	AttachmentMaxSize       int64
	AttachmentMaxPerEvent   int
	AttachmentMaxPerComment int
	AttachmentQuota         int64

	// BuiltinThemes holds the shipped theme JSON files (web/src/themes).
	BuiltinThemes fs.FS
}

// Service is the domain layer. It is safe for concurrent use.
type Service struct {
	DB       *store.DB
	Broker   realtime.Broker
	Presence *realtime.Presence
	Log      *slog.Logger
	Opts     Options
	Now      func() time.Time

	// Attachments is the upload pipeline; Scanner is nil unless CLAMAV_ADDR is set.
	Attachments *attachments.Pipeline
	Blobs       attachments.BlobStore
	Scanner     attachments.Scanner

	// Sessions lets user administration revoke sessions.
	Sessions *auth.Sessions

	// writeMu serializes commit+publish so messages leave in seq order and
	// a client's Last-Event-ID never skips an earlier, later-published change.
	writeMu sync.Mutex
	bg      sync.WaitGroup
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Wait blocks until background work (virus scans) has finished.
func (s *Service) Wait() { s.bg.Wait() }

// Tx is a write transaction that collects change messages for publication
// after commit.
type Tx struct {
	db    *gorm.DB
	s     *Service
	actor authz.Actor
	now   time.Time
	msgs  []realtime.Message
	after []func()
}

// write runs fn in a transaction, then publishes its messages in order.
func (s *Service) write(ctx context.Context, actor authz.Actor, fn func(tx *Tx) error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	var t *Tx
	err := s.DB.W.WithContext(ctx).Transaction(func(g *gorm.DB) error {
		t = &Tx{db: g, s: s, actor: actor, now: s.now()}
		return fn(t)
	})
	if err != nil {
		return err
	}
	for _, m := range t.msgs {
		s.Broker.Publish(m)
	}
	for _, f := range t.after {
		f()
	}
	return nil
}

// emit appends a change_log row and queues the SSE message. audience limits
// delivery to one user ("" = everyone).
func (t *Tx) emit(typ, subjectID string, version int64, payload any, audience string) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	row := store.ChangeLog{Type: typ, SubjectID: subjectID, PayloadJSON: string(data), CreatedAt: t.now}
	if audience != "" {
		row.AudienceUserID = &audience
	}
	if err := t.db.Create(&row).Error; err != nil {
		return err
	}
	t.msgs = append(t.msgs, realtime.Message{Seq: row.Seq, Type: typ, ID: subjectID, Version: version, Payload: data, Audience: audience})
	return nil
}

// record writes an activity row attributed to the transaction's actor.
func (t *Tx) record(action, subjectType, subjectID string, eventID *string, before, after any) error {
	return t.recordAs(t.actor.ID, action, subjectType, subjectID, eventID, before, after)
}

func (t *Tx) recordAs(actorID, action, subjectType, subjectID string, eventID *string, before, after any) error {
	row := store.Activity{
		ID: store.NewID(), Action: action, SubjectType: subjectType, SubjectID: subjectID,
		EventID: eventID, BeforeJSON: jsonOrNull(before), AfterJSON: jsonOrNull(after), CreatedAt: t.now,
	}
	if actorID != "" {
		row.ActorID = &actorID
	}
	return t.db.Create(&row).Error
}

// notify creates an inbox notification and pushes it to its recipient.
// Users never notify themselves.
func (t *Tx) notify(userID, kind, subjectType, subjectID string, eventID *string) error {
	if userID == "" || userID == t.actor.ID || userID == store.SystemUserID {
		return nil
	}
	n := store.Notification{
		ID: store.NewID(), UserID: userID, Kind: kind, SubjectType: subjectType, SubjectID: subjectID,
		EventID: eventID, CreatedAt: t.now, UpdatedAt: t.now,
	}
	if t.actor.ID != "" {
		a := t.actor.ID
		n.ActorID = &a
	}
	if err := t.db.Create(&n).Error; err != nil {
		return err
	}
	item, err := t.s.notificationDTOs(t.db, []store.Notification{n})
	if err != nil {
		return err
	}
	var unread int64
	if err := t.db.Model(&store.Notification{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&unread).Error; err != nil {
		return err
	}
	return t.emit("inbox.item", n.ID, 0, map[string]any{"item": item[0], "unread": unread}, userID)
}

func jsonOrNull(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func isNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }

// take loads the row with this ID, reporting a missing one as "<what> not found".
func take[T any](db *gorm.DB, id, what string) (T, error) {
	var row T
	err := db.Where("id = ?", id).Take(&row).Error
	if isNotFound(err) {
		err = notFound(what)
	}
	return row, err
}

func ptr[T any](v T) *T { return &v }
