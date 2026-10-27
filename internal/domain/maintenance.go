package domain

import (
	"context"
	"encoding/json"
	"time"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/realtime"
	"github.com/felixtrav/sharedcal/internal/store"
)

// changeLogRetention is how long SSE messages can be replayed.
const changeLogRetention = 24 * time.Hour

// Replay implements realtime.ReplayFunc from the change_log table.
func (s *Service) Replay(ctx context.Context, after int64) ([]realtime.Message, bool, error) {
	db := s.DB.R.WithContext(ctx)
	var oldest struct{ Seq *int64 }
	if err := db.Model(&store.ChangeLog{}).Select("MIN(seq) AS seq").Scan(&oldest).Error; err != nil {
		return nil, false, err
	}
	if oldest.Seq == nil {
		// Nothing has ever changed (pruning always keeps the newest row).
		return nil, after == 0, nil
	}
	if after < *oldest.Seq-1 {
		return nil, false, nil
	}
	var rows []store.ChangeLog
	if err := db.Where("seq > ?", after).Order("seq").Limit(5001).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	out := make([]realtime.Message, len(rows))
	for i, r := range rows {
		m := realtime.Message{Seq: r.Seq, Type: r.Type, ID: r.SubjectID, Payload: json.RawMessage(r.PayloadJSON)}
		if r.AudienceUserID != nil {
			m.Audience = *r.AudienceUserID
		}
		var v struct {
			Version int64 `json:"version"`
		}
		if json.Unmarshal(m.Payload, &v) == nil {
			m.Version = v.Version
		}
		out[i] = m
	}
	return out, true, nil
}

// LatestSeq returns the newest change_log sequence number (0 if empty).
func (s *Service) LatestSeq(ctx context.Context) (int64, error) {
	var latest struct{ Seq *int64 }
	if err := s.DB.R.WithContext(ctx).Model(&store.ChangeLog{}).Select("MAX(seq) AS seq").Scan(&latest).Error; err != nil || latest.Seq == nil {
		return 0, err
	}
	return *latest.Seq, nil
}

// RunMaintenance performs periodic cleanup until ctx ends.
func (s *Service) RunMaintenance(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		s.Maintain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Maintain prunes the change log, expired sessions and the trash.
func (s *Service) Maintain(ctx context.Context) {
	now := s.now()
	w := s.DB.W.WithContext(ctx)
	// Keep the newest row so Replay can always tell "up to date" from
	// "missed pruned changes".
	latest, err := s.LatestSeq(ctx)
	if err == nil {
		err = w.Where("created_at < ? AND seq < ?", now.Add(-changeLogRetention), latest).Delete(&store.ChangeLog{}).Error
	}
	if err != nil {
		s.Log.ErrorContext(ctx, "pruning change_log failed", "err", err)
	}
	if s.Sessions != nil {
		if err := s.Sessions.PurgeExpired(ctx); err != nil {
			s.Log.ErrorContext(ctx, "purging sessions failed", "err", err)
		}
	}
	s.purgeTrash(ctx, now.Add(-trashRetention))
}

// purgeTrash hard-deletes events and attachments soft-deleted before cutoff
// and garbage-collects their blobs.
func (s *Service) purgeTrash(ctx context.Context, cutoff time.Time) {
	var keys []string
	err := s.write(ctx, authz.Actor{}, func(tx *Tx) error {
		var eventIDs []string
		if err := tx.db.Model(&store.Event{}).Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff).Limit(1000).Pluck("id", &eventIDs).Error; err != nil {
			return err
		}
		var atts []store.Attachment
		q := tx.db.Where("deleted_at IS NOT NULL AND deleted_at < ?", cutoff)
		if len(eventIDs) > 0 {
			q = tx.db.Where("(deleted_at IS NOT NULL AND deleted_at < ?) OR event_id IN ?", cutoff, eventIDs)
		}
		if err := q.Find(&atts).Error; err != nil {
			return err
		}
		for _, a := range atts {
			keys = append(keys, a.StorageKey, deref(a.ThumbKey))
		}
		if len(atts) > 0 {
			ids := make([]string, len(atts))
			for i := range atts {
				ids[i] = atts[i].ID
			}
			if err := tx.db.Where("id IN ?", ids).Delete(&store.Attachment{}).Error; err != nil {
				return err
			}
		}
		if len(eventIDs) > 0 {
			// Child rows (overrides, tags, comments, suggestions) cascade.
			if err := tx.db.Where("id IN ?", eventIDs).Delete(&store.Event{}).Error; err != nil {
				return err
			}
			for _, id := range eventIDs {
				if err := tx.recordAs("", "event.purged", "event", id, &id, nil, nil); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		s.Log.ErrorContext(ctx, "purging trash failed", "err", err)
		return
	}
	s.gcBlobs(ctx, keys)
}
