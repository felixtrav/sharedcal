package domain

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// ActivityDTO is one audit-log row.
type ActivityDTO struct {
	ID          string          `json:"id"`
	Actor       *UserRef        `json:"actor"`
	Action      string          `json:"action"`
	SubjectType string          `json:"subjectType"`
	SubjectID   string          `json:"subjectId"`
	EventID     *string         `json:"eventId"`
	Before      json.RawMessage `json:"before"`
	After       json.RawMessage `json:"after"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// ActivityFilter narrows the audit log.
type ActivityFilter struct {
	ActorID     string
	Action      string
	SubjectType string
	EventID     string
	Before      string // cursor: an activity ID (UUIDv7 IDs sort by time)
	Limit       int
}

func (s *Service) activityDTOs(db *gorm.DB, rows []store.Activity) ([]ActivityDTO, error) {
	var ids []string
	for _, r := range rows {
		if r.ActorID != nil {
			ids = append(ids, *r.ActorID)
		}
	}
	refs, err := userRefs(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ActivityDTO, len(rows))
	for i, r := range rows {
		d := ActivityDTO{ID: r.ID, Action: r.Action, SubjectType: r.SubjectType, SubjectID: r.SubjectID, EventID: r.EventID,
			Before: json.RawMessage(r.BeforeJSON), After: json.RawMessage(r.AfterJSON), CreatedAt: r.CreatedAt}
		if r.ActorID != nil {
			ref := refs[*r.ActorID]
			if ref.ID == "" {
				ref.ID = *r.ActorID
			}
			d.Actor = &ref
		}
		out[i] = d
	}
	return out, nil
}

// ListActivity returns the audit log: everything for admins, the caller's
// own activity for everyone else.
func (s *Service) ListActivity(ctx context.Context, actor authz.Actor, f ActivityFilter) ([]ActivityDTO, error) {
	scope := authz.ActivityAccess(actor)
	if scope == authz.ActivityNone {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	q := db.Model(&store.Activity{})
	if scope == authz.ActivityOwn {
		q = q.Where("actor_id = ?", actor.ID)
	} else if f.ActorID != "" {
		q = q.Where("actor_id = ?", f.ActorID)
	}
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.SubjectType != "" {
		q = q.Where("subject_type = ?", f.SubjectType)
	}
	if f.EventID != "" {
		q = q.Where("event_id = ?", f.EventID)
	}
	if f.Before != "" {
		q = q.Where("id < ?", f.Before)
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	var rows []store.Activity
	if err := q.Order("id DESC").Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.activityDTOs(db, rows)
}

// EventHistory returns the activity of one event, oldest first.
func (s *Service) EventHistory(ctx context.Context, actor authz.Actor, eventID string) ([]ActivityDTO, error) {
	if !authz.CanViewEventHistory(actor) {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	if _, err := s.loadEvent(db, eventID, authz.CanAdminister(actor)); err != nil {
		return nil, err
	}
	var rows []store.Activity
	if err := db.Where("event_id = ?", eventID).Order("id").Limit(1000).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.activityDTOs(db, rows)
}

// NotificationDTO is one inbox item.
type NotificationDTO struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind" enum:"suggestion.created,suggestion.resolved,comment.reply,comment.mention"`
	SubjectType string     `json:"subjectType" enum:"suggestion,comment"`
	SubjectID   string     `json:"subjectId"`
	EventID     *string    `json:"eventId"`
	EventTitle  string     `json:"eventTitle"`
	Actor       *UserRef   `json:"actor"`
	ReadAt      *time.Time `json:"readAt"`
	CreatedAt   time.Time  `json:"createdAt"`
}

func (s *Service) notificationDTOs(db *gorm.DB, rows []store.Notification) ([]NotificationDTO, error) {
	var actors, events []string
	for _, n := range rows {
		if n.ActorID != nil {
			actors = append(actors, *n.ActorID)
		}
		if n.EventID != nil {
			events = append(events, *n.EventID)
		}
	}
	refs, err := userRefs(db, actors)
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	if ids := uniq(events); len(ids) > 0 {
		var evs []store.Event
		if err := db.Select("id", "title").Where("id IN ?", ids).Find(&evs).Error; err != nil {
			return nil, err
		}
		for _, e := range evs {
			titles[e.ID] = e.Title
		}
	}
	out := make([]NotificationDTO, len(rows))
	for i, n := range rows {
		d := NotificationDTO{ID: n.ID, Kind: n.Kind, SubjectType: n.SubjectType, SubjectID: n.SubjectID, EventID: n.EventID, ReadAt: n.ReadAt, CreatedAt: n.CreatedAt}
		if n.EventID != nil {
			d.EventTitle = titles[*n.EventID]
		}
		if n.ActorID != nil {
			r := refs[*n.ActorID]
			d.Actor = &r
		}
		out[i] = d
	}
	return out, nil
}

// Inbox lists the caller's notifications, newest first.
func (s *Service) Inbox(ctx context.Context, actor authz.Actor, unreadOnly bool, before string, limit int) ([]NotificationDTO, int64, error) {
	if !authz.CanView(actor) {
		return nil, 0, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	q := db.Where("user_id = ?", actor.ID)
	if unreadOnly {
		q = q.Where("read_at IS NULL")
	}
	if before != "" {
		q = q.Where("id < ?", before)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []store.Notification
	if err := q.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items, err := s.notificationDTOs(db, rows)
	if err != nil {
		return nil, 0, err
	}
	unread, err := s.unread(db, actor.ID)
	return items, unread, err
}

func (s *Service) unread(db *gorm.DB, userID string) (int64, error) {
	var n int64
	err := db.Model(&store.Notification{}).Where("user_id = ? AND read_at IS NULL", userID).Count(&n).Error
	return n, err
}

// MarkRead marks some (or all) of the caller's notifications read.
func (s *Service) MarkRead(ctx context.Context, actor authz.Actor, ids []string, all bool) (int64, error) {
	if !authz.CanView(actor) {
		return 0, forbidden("not allowed")
	}
	if !all && len(ids) == 0 {
		return 0, invalid("pass ids or all")
	}
	if len(ids) > 500 {
		return 0, invalidField("ids", "too many ids")
	}
	var unread int64
	err := s.write(ctx, actor, func(tx *Tx) error {
		q := tx.db.Model(&store.Notification{}).Where("user_id = ? AND read_at IS NULL", actor.ID)
		if !all {
			q = q.Where("id IN ?", ids)
		}
		if err := q.Updates(map[string]any{"read_at": tx.now, "updated_at": tx.now}).Error; err != nil {
			return err
		}
		var err error
		unread, err = s.unread(tx.db, actor.ID)
		return err
	})
	return unread, err
}
