package domain

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"time"

	"github.com/felixtrav/sharedcal/internal/auth"
	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/ics"
	"github.com/felixtrav/sharedcal/internal/store"
)

// feedWindow bounds how far back the ICS feed reaches for one-off events.
const feedWindow = 366 * 24 * time.Hour

// FeedStatus describes the caller's personal ICS feed.
type FeedStatus struct {
	Exists     bool       `json:"exists"`
	CreatedAt  *time.Time `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// FeedToken returns the status of the caller's feed token.
func (s *Service) FeedToken(ctx context.Context, actor authz.Actor) (FeedStatus, error) {
	if !authz.CanSubscribeFeed(actor) {
		return FeedStatus{}, forbidden("not allowed")
	}
	var t store.FeedToken
	err := s.DB.R.WithContext(ctx).Where("user_id = ?", actor.ID).Take(&t).Error
	if isNotFound(err) {
		return FeedStatus{}, nil
	}
	if err != nil {
		return FeedStatus{}, err
	}
	return FeedStatus{Exists: true, CreatedAt: &t.CreatedAt, LastUsedAt: t.LastUsedAt}, nil
}

// RotateFeedToken creates (or replaces) the caller's feed secret and returns
// the full feed URL. The raw token is shown once and stored only hashed.
func (s *Service) RotateFeedToken(ctx context.Context, actor authz.Actor) (string, error) {
	if !authz.CanSubscribeFeed(actor) {
		return "", forbidden("not allowed")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	err := s.write(ctx, actor, func(tx *Tx) error {
		if err := tx.db.Where("user_id = ?", actor.ID).Delete(&store.FeedToken{}).Error; err != nil {
			return err
		}
		row := store.FeedToken{ID: store.NewID(), UserID: actor.ID, TokenHash: auth.HashToken(raw), CreatedAt: tx.now, UpdatedAt: tx.now}
		if err := tx.db.Create(&row).Error; err != nil {
			return err
		}
		return tx.record("feed.rotated", "user", actor.ID, nil, nil, nil)
	})
	if err != nil {
		return "", err
	}
	return s.Opts.AppURL + "/ics/" + raw + ".ics", nil
}

// RevokeFeedToken deletes the caller's feed secret.
func (s *Service) RevokeFeedToken(ctx context.Context, actor authz.Actor) error {
	if !authz.CanSubscribeFeed(actor) {
		return forbidden("not allowed")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		if err := tx.db.Where("user_id = ?", actor.ID).Delete(&store.FeedToken{}).Error; err != nil {
			return err
		}
		return tx.record("feed.revoked", "user", actor.ID, nil, nil, nil)
	})
}

// Feed resolves a feed token to a read-only calendar. Comments, suggestions
// and attachments are never included.
func (s *Service) Feed(ctx context.Context, raw string) (*ics.Calendar, error) {
	if len(raw) < 40 || len(raw) > 64 {
		return nil, notFound("feed")
	}
	db := s.DB.R.WithContext(ctx)
	var tok store.FeedToken
	if err := db.Where("token_hash = ?", auth.HashToken(raw)).Take(&tok).Error; err != nil {
		if isNotFound(err) {
			return nil, notFound("feed")
		}
		return nil, err
	}
	var u store.User
	if err := db.Where("id = ?", tok.UserID).Take(&u).Error; err != nil || !u.Active {
		return nil, notFound("feed")
	}
	actor := authz.Actor{ID: u.ID, Role: authz.Role(u.Role), Active: u.Active}
	if !authz.CanSubscribeFeed(actor) {
		return nil, notFound("feed")
	}
	now := s.now()
	if tok.LastUsedAt == nil || now.Sub(*tok.LastUsedAt) > time.Hour {
		_ = s.DB.W.WithContext(ctx).Model(&store.FeedToken{}).Where("id = ?", tok.ID).Update("last_used_at", now).Error
	}

	// Events still in the trash are kept as cancelled entries so subscribers
	// that missed the deletion drop them; they leave the feed when purged.
	var evs []store.Event
	if err := db.Where("deleted_at IS NULL OR deleted_at > ?", now.Add(-trashRetention)).
		Where("(rrule = '' AND end_at > ?) OR (rrule <> '' AND (series_end_at IS NULL OR series_end_at > ?))", now.Add(-feedWindow), now.Add(-feedWindow)).
		Order("start_at").Limit(10000).Find(&evs).Error; err != nil {
		return nil, err
	}
	ids := make([]string, len(evs))
	for i := range evs {
		ids[i] = evs[i].ID
	}
	tagIDs, err := eventTags(db, ids)
	if err != nil {
		return nil, err
	}
	var tags []store.Tag
	if err := db.Find(&tags).Error; err != nil {
		return nil, err
	}
	tagName := map[string]string{}
	for _, t := range tags {
		tagName[t.ID] = t.Name
	}
	overrides := map[string][]store.EventOverride{}
	if len(ids) > 0 {
		var rows []store.EventOverride
		if err := db.Where("event_id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, o := range rows {
			overrides[o.EventID] = append(overrides[o.EventID], o)
		}
	}
	st, err := s.loadSettings(db)
	if err != nil {
		return nil, err
	}
	cal := &ics.Calendar{Name: st.CalendarName}
	for i := range evs {
		e := &evs[i]
		if e.DeletedAt != nil {
			cal.Events = append(cal.Events, ics.Event{
				UID: e.ID, Start: e.StartAt, End: e.EndAt, AllDay: e.AllDay, TimeZone: e.TimeZone, RRule: e.RRule,
				Sequence: e.Version, Updated: e.UpdatedAt, Cancelled: true,
			})
			continue
		}
		ie := ics.Event{
			UID: e.ID, Title: e.Title, Description: e.Description, Location: e.Location,
			Start: e.StartAt, End: e.EndAt, AllDay: e.AllDay, TimeZone: e.TimeZone, RRule: e.RRule,
			ExDates: exdates(e), Status: e.Status, Sequence: e.Version, Created: e.CreatedAt, Updated: e.UpdatedAt,
			URL: s.Opts.AppURL + "/?event=" + e.ID,
		}
		for _, t := range tagIDs[e.ID] {
			if n := tagName[t]; n != "" {
				ie.Categories = append(ie.Categories, n)
			}
		}
		for _, o := range overrides[e.ID] {
			ie.Overrides = append(ie.Overrides, ics.Override{
				RecurrenceID: o.RecurrenceID.UTC(), Cancelled: o.Cancelled, Title: o.Title, Description: o.Description,
				Location: o.Location, Start: o.StartAt, End: o.EndAt, AllDay: o.AllDay, Status: o.Status,
			})
		}
		cal.Events = append(cal.Events, ie)
	}
	return cal, nil
}
