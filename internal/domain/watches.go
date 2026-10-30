package domain

import (
	"context"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// Inbox kinds raised for people watching an event. Each is folded per event:
// while a watcher still has an unread item of a kind for an event, further
// activity of that kind replaces it with one whose count is a step higher
// rather than adding a row, so a busy comment thread is one inbox entry.
const (
	kindWatchUpdated  = "watch.updated"
	kindWatchComments = "watch.comments"
	kindWatchDeleted  = "watch.deleted"
)

// Watch makes the caller a watcher of an event. Watching twice is a no-op.
func (s *Service) Watch(ctx context.Context, actor authz.Actor, eventID string) error {
	if !authz.CanView(actor) {
		return forbidden("not allowed")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		if _, err := s.loadEvent(tx.db, eventID, false); err != nil {
			return err
		}
		return tx.addWatch(actor.ID, eventID)
	})
}

// addWatch records a watch unless one exists. Owners get theirs this way when
// an event is created or handed to them; they can stop watching afterwards.
func (t *Tx) addWatch(userID, eventID string) error {
	if userID == "" || userID == store.SystemUserID {
		return nil
	}
	var n int64
	if err := t.db.Model(&store.EventWatch{}).Where("user_id = ? AND event_id = ?", userID, eventID).Count(&n).Error; err != nil || n > 0 {
		return err
	}
	return t.db.Create(&store.EventWatch{UserID: userID, EventID: eventID, CreatedAt: t.now}).Error
}

// Unwatch stops the caller's watch. It also works on events that are gone
// from the trash or purged, and when nothing was watched.
func (s *Service) Unwatch(ctx context.Context, actor authz.Actor, eventID string) error {
	if !authz.CanView(actor) {
		return forbidden("not allowed")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		return tx.db.Where("user_id = ? AND event_id = ?", actor.ID, eventID).Delete(&store.EventWatch{}).Error
	})
}

// notifyWatchers raises an inbox item of kind for everyone watching the
// event except the actor and anyone already told about it in this
// transaction (a mention, reply or suggestion decision is more specific).
func (s *Service) notifyWatchers(tx *Tx, eventID, kind string) error {
	var ids []string
	err := tx.db.Model(&store.EventWatch{}).
		Joins("JOIN users ON users.id = event_watches.user_id").
		Where("event_watches.event_id = ? AND users.active = ? AND users.id <> ?", eventID, true, store.SystemUserID).
		Pluck("event_watches.user_id", &ids).Error
	if err != nil {
		return err
	}
	for _, uid := range ids {
		if tx.told[uid+"|"+eventID] {
			continue
		}
		if err := tx.notifyWatch(uid, kind, eventID); err != nil {
			return err
		}
	}
	return nil
}

// notifyWatch adds one watcher's item, folding it into their unread item of
// the same kind for the event if there is one.
func (t *Tx) notifyWatch(userID, kind, eventID string) error {
	if userID == t.actor.ID {
		return nil
	}
	n := store.Notification{ItemCount: 1}
	err := t.db.Where("user_id = ? AND kind = ? AND event_id = ? AND read_at IS NULL", userID, kind, eventID).Order("id DESC").Take(&n).Error
	replaces := ""
	switch {
	case err == nil:
		replaces = n.ID
		if err := t.db.Delete(&store.Notification{}, "id = ?", n.ID).Error; err != nil {
			return err
		}
		n.ItemCount++
	case !isNotFound(err):
		return err
	}
	return t.deliver(store.Notification{
		UserID: userID, Kind: kind, SubjectType: "event", SubjectID: eventID, EventID: &eventID, ItemCount: n.ItemCount,
	}, replaces)
}

// isWatching reports whether the user watches the event.
func (s *Service) isWatching(db *gorm.DB, userID, eventID string) (bool, error) {
	var n int64
	err := db.Model(&store.EventWatch{}).Where("user_id = ? AND event_id = ?", userID, eventID).Count(&n).Error
	return n > 0, err
}
