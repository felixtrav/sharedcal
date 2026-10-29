package domain

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

const (
	maxCommentBody   = 10000
	maxMentions      = 20
	maxCommentsEvent = 5000
)

// CommentDTO is a comment as sent to clients.
type CommentDTO struct {
	ID          string                   `json:"id"`
	EventID     string                   `json:"eventId"`
	ParentID    *string                  `json:"parentId"`
	Author      UserRef                  `json:"author"`
	Body        string                   `json:"body"`
	Mentions    []string                 `json:"mentions"`
	CreatedAt   time.Time                `json:"createdAt"`
	EditedAt    *time.Time               `json:"editedAt"`
	Deleted     bool                     `json:"deleted"`
	Attachments []AttachmentDTO          `json:"attachments"`
	Permissions authz.CommentPermissions `json:"permissions"`
}

// CommentInput is the body of create/edit requests.
type CommentInput struct {
	Body     string   `json:"body" maxLength:"10000"`
	ParentID *string  `json:"parentId,omitempty" maxLength:"36"`
	Mentions []string `json:"mentions,omitempty" maxItems:"20"`
}

func authzComment(c *store.Comment) authz.Comment {
	return authz.Comment{AuthorID: c.AuthorID, Deleted: c.DeletedAt != nil}
}

func (s *Service) commentDTOs(db *gorm.DB, actor authz.Actor, cs []store.Comment, ev *store.Event) ([]CommentDTO, error) {
	ids := make([]string, len(cs))
	authors := make([]string, len(cs))
	for i := range cs {
		ids[i], authors[i] = cs[i].ID, cs[i].AuthorID
	}
	refs, err := userRefs(db, authors)
	if err != nil {
		return nil, err
	}
	mentions := map[string][]string{}
	if len(ids) > 0 {
		var ns []store.Notification
		if err := db.Where("subject_type = ? AND kind = ? AND subject_id IN ?", "comment", "comment.mention", ids).Order("created_at").Find(&ns).Error; err != nil {
			return nil, err
		}
		for _, n := range ns {
			mentions[n.SubjectID] = append(mentions[n.SubjectID], n.UserID)
		}
	}
	atts := map[string][]AttachmentDTO{}
	if len(ids) > 0 && ev != nil {
		list, err := s.listAttachments(db, actor, ev, "comment_id IN ?", ids)
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			atts[*a.CommentID] = append(atts[*a.CommentID], a)
		}
	}
	out := make([]CommentDTO, len(cs))
	for i := range cs {
		c := &cs[i]
		d := CommentDTO{
			ID: c.ID, EventID: c.EventID, ParentID: c.ParentID, Author: refs[c.AuthorID], Body: c.Body,
			Mentions: mentions[c.ID], CreatedAt: c.CreatedAt, EditedAt: c.EditedAt,
			Attachments: atts[c.ID], Permissions: authz.CommentPerms(actor, authzComment(c)),
		}
		if d.Mentions == nil {
			d.Mentions = []string{}
		}
		if d.Attachments == nil {
			d.Attachments = []AttachmentDTO{}
		}
		if c.DeletedAt != nil {
			d.Deleted, d.Body, d.Mentions, d.Attachments = true, "", []string{}, []AttachmentDTO{}
		}
		out[i] = d
	}
	return out, nil
}

// ListComments returns an event's comments oldest first. Deleted comments
// are omitted unless they still have visible replies (then a placeholder).
func (s *Service) ListComments(ctx context.Context, actor authz.Actor, eventID string) ([]CommentDTO, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	ev, err := s.loadEvent(db, eventID, false)
	if err != nil {
		return nil, err
	}
	var all []store.Comment
	if err := db.Where("event_id = ?", eventID).Order("created_at, id").Limit(maxCommentsEvent).Find(&all).Error; err != nil {
		return nil, err
	}
	hasReplies := map[string]bool{}
	for _, c := range all {
		if c.ParentID != nil && c.DeletedAt == nil {
			hasReplies[*c.ParentID] = true
		}
	}
	visible := all[:0]
	for _, c := range all {
		if c.DeletedAt == nil || (c.ParentID == nil && hasReplies[c.ID]) {
			visible = append(visible, c)
		}
	}
	return s.commentDTOs(db, actor, visible, ev)
}

func validateBody(body string) (string, error) {
	b := strings.TrimSpace(body)
	if b == "" || utf8.RuneCountInString(b) > maxCommentBody {
		return "", invalidField("body", "comment must be 1–%d characters", maxCommentBody)
	}
	return b, nil
}

func validMentions(db *gorm.DB, ids []string) ([]string, error) {
	ids = uniq(ids)
	if len(ids) > maxMentions {
		return nil, invalidField("mentions", "at most %d mentions", maxMentions)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var found []string
	if err := db.Model(&store.User{}).Where("id IN ? AND active = ?", ids, true).Pluck("id", &found).Error; err != nil {
		return nil, err
	}
	return found, nil
}

// CreateComment adds a comment or a reply (one level deep).
func (s *Service) CreateComment(ctx context.Context, actor authz.Actor, eventID string, in CommentInput) (*CommentDTO, error) {
	body, err := validateBody(in.Body)
	if err != nil {
		return nil, err
	}
	var out *CommentDTO
	err = s.write(ctx, actor, func(tx *Tx) error {
		ev, err := s.loadEvent(tx.db, eventID, false)
		if err != nil {
			return err
		}
		if !authz.CanComment(actor, authzEvent(ev)) {
			return forbidden("you cannot comment on this event")
		}
		var parent *store.Comment
		if in.ParentID != nil {
			var p store.Comment
			if err := tx.db.Where("id = ? AND event_id = ?", *in.ParentID, eventID).Take(&p).Error; err != nil {
				if isNotFound(err) {
					return invalidField("parentId", "parent comment not found")
				}
				return err
			}
			if p.ParentID != nil {
				return invalidField("parentId", "replies are one level deep; reply to the top-level comment")
			}
			if p.DeletedAt != nil {
				return invalidField("parentId", "parent comment was deleted")
			}
			parent = &p
		}
		mentions, err := validMentions(tx.db, in.Mentions)
		if err != nil {
			return err
		}
		c := store.Comment{ID: store.NewID(), EventID: eventID, AuthorID: actor.ID, ParentID: in.ParentID, Body: body, CreatedAt: tx.now, UpdatedAt: tx.now}
		if err := tx.db.Create(&c).Error; err != nil {
			return err
		}
		if err := tx.record("comment.created", "comment", c.ID, &eventID, nil, map[string]any{"body": body, "parentId": in.ParentID}); err != nil {
			return err
		}
		notified := map[string]bool{}
		for _, uid := range mentions {
			if uid == actor.ID {
				continue
			}
			if err := tx.notify(uid, "comment.mention", "comment", c.ID, &eventID); err != nil {
				return err
			}
			notified[uid] = true
		}
		if parent != nil && !notified[parent.AuthorID] {
			if err := tx.notify(parent.AuthorID, "comment.reply", "comment", c.ID, &eventID); err != nil {
				return err
			}
		}
		if err := s.notifyWatchers(tx, eventID, kindWatchComments); err != nil {
			return err
		}
		dtos, err := s.commentDTOs(tx.db, actor, []store.Comment{c}, ev)
		if err != nil {
			return err
		}
		out = &dtos[0]
		return tx.emit("comment.created", c.ID, 0, out, "")
	})
	return out, err
}

// UpdateComment edits the caller's own comment.
func (s *Service) UpdateComment(ctx context.Context, actor authz.Actor, id string, in CommentInput) (*CommentDTO, error) {
	body, err := validateBody(in.Body)
	if err != nil {
		return nil, err
	}
	var out *CommentDTO
	err = s.write(ctx, actor, func(tx *Tx) error {
		c, ev, err := s.loadComment(tx.db, id)
		if err != nil {
			return err
		}
		if !authz.CanEditComment(actor, authzComment(c)) {
			return forbidden("you can only edit your own comments")
		}
		before := c.Body
		now := tx.now
		c.Body, c.EditedAt, c.UpdatedAt = body, &now, now
		if err := tx.db.Save(c).Error; err != nil {
			return err
		}
		if in.Mentions != nil {
			mentions, err := validMentions(tx.db, in.Mentions)
			if err != nil {
				return err
			}
			var existing []string
			if err := tx.db.Model(&store.Notification{}).Where("subject_type = ? AND subject_id = ? AND kind = ?", "comment", c.ID, "comment.mention").Pluck("user_id", &existing).Error; err != nil {
				return err
			}
			have := map[string]bool{}
			for _, u := range existing {
				have[u] = true
			}
			for _, uid := range mentions {
				if !have[uid] {
					if err := tx.notify(uid, "comment.mention", "comment", c.ID, &c.EventID); err != nil {
						return err
					}
				}
			}
		}
		if err := tx.record("comment.updated", "comment", c.ID, &c.EventID, map[string]string{"body": before}, map[string]string{"body": body}); err != nil {
			return err
		}
		dtos, err := s.commentDTOs(tx.db, actor, []store.Comment{*c}, ev)
		if err != nil {
			return err
		}
		out = &dtos[0]
		return tx.emit("comment.updated", c.ID, 0, out, "")
	})
	return out, err
}

// DeleteComment soft-deletes a comment (author or admin), along with its
// attachments.
func (s *Service) DeleteComment(ctx context.Context, actor authz.Actor, id string) error {
	return s.write(ctx, actor, func(tx *Tx) error {
		c, _, err := s.loadComment(tx.db, id)
		if err != nil {
			return err
		}
		if !authz.CanDeleteComment(actor, authzComment(c)) {
			return forbidden("you cannot delete this comment")
		}
		now := tx.now
		if err := tx.db.Model(&store.Comment{}).Where("id = ?", c.ID).Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.db.Model(&store.Attachment{}).Where("comment_id = ? AND deleted_at IS NULL", c.ID).Updates(map[string]any{"deleted_at": now, "updated_at": now}).Error; err != nil {
			return err
		}
		if err := tx.record("comment.deleted", "comment", c.ID, &c.EventID, map[string]string{"body": c.Body}, nil); err != nil {
			return err
		}
		return tx.emit("comment.deleted", c.ID, 0, map[string]string{"id": c.ID, "eventId": c.EventID}, "")
	})
}

func (s *Service) loadComment(db *gorm.DB, id string) (*store.Comment, *store.Event, error) {
	var c store.Comment
	if err := db.Where("id = ? AND deleted_at IS NULL", id).Take(&c).Error; err != nil {
		if isNotFound(err) {
			return nil, nil, notFound("comment")
		}
		return nil, nil, err
	}
	ev, err := s.loadEvent(db, c.EventID, false)
	if err != nil {
		return nil, nil, err
	}
	return &c, ev, nil
}
