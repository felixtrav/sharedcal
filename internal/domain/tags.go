package domain

import (
	"context"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

const maxTags = 200

// TagDTO is a tag as sent to clients. ColorSlot is a theme slot, never a hex
// value, so tag colors stay readable in every theme.
type TagDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ColorSlot string `json:"colorSlot" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
	SortOrder int    `json:"sortOrder"`
}

// TagInput creates or changes a tag.
type TagInput struct {
	Name      *string `json:"name,omitempty" maxLength:"64"`
	ColorSlot *string `json:"colorSlot,omitempty" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
}

func validSlot(s string) bool {
	return len(s) == 5 && strings.HasPrefix(s, "tag-") && s[4] >= '1' && s[4] <= '8'
}

func tagDTO(t store.Tag) TagDTO {
	return TagDTO{ID: t.ID, Name: t.Name, ColorSlot: t.ColorSlot, SortOrder: t.SortOrder}
}

func listTags(db *gorm.DB) ([]TagDTO, error) {
	var tags []store.Tag
	if err := db.Order("sort_order, name, id").Find(&tags).Error; err != nil {
		return nil, err
	}
	out := make([]TagDTO, len(tags))
	for i, t := range tags {
		out[i] = tagDTO(t)
	}
	return out, nil
}

// ListTags returns every tag in display order.
func (s *Service) ListTags(ctx context.Context, actor authz.Actor) ([]TagDTO, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	return listTags(s.DB.R.WithContext(ctx))
}

func (s *Service) checkTagName(db *gorm.DB, name, exceptID string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > 64 {
		return "", invalidField("name", "tag name must be 1–64 characters")
	}
	var count int64
	q := db.Model(&store.Tag{}).Where("LOWER(name) = ?", strings.ToLower(n))
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	if err := q.Count(&count).Error; err != nil {
		return "", err
	}
	if count > 0 {
		return "", invalidField("name", "a tag with this name already exists")
	}
	return n, nil
}

func (tx *Tx) emitTags() error {
	tags, err := listTags(tx.db)
	if err != nil {
		return err
	}
	return tx.emit("tag.changed", "tags", 0, map[string]any{"tags": tags}, "")
}

// CreateTag adds a tag at the end of the list.
func (s *Service) CreateTag(ctx context.Context, actor authz.Actor, in TagInput) (*TagDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	if in.Name == nil || in.ColorSlot == nil || !validSlot(*in.ColorSlot) {
		return nil, invalid("name and a colorSlot (tag-1 … tag-8) are required")
	}
	var out TagDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		name, err := s.checkTagName(tx.db, *in.Name, "")
		if err != nil {
			return err
		}
		var n int64
		if err := tx.db.Model(&store.Tag{}).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxTags {
			return limit("too many tags")
		}
		var maxOrder struct{ M *int }
		if err := tx.db.Model(&store.Tag{}).Select("MAX(sort_order) AS m").Scan(&maxOrder).Error; err != nil {
			return err
		}
		order := 0
		if maxOrder.M != nil {
			order = *maxOrder.M + 1
		}
		t := store.Tag{ID: store.NewID(), Name: name, ColorSlot: *in.ColorSlot, SortOrder: order, CreatedAt: tx.now, UpdatedAt: tx.now}
		if err := tx.db.Create(&t).Error; err != nil {
			return err
		}
		out = tagDTO(t)
		if err := tx.record("tag.created", "tag", t.ID, nil, nil, out); err != nil {
			return err
		}
		return tx.emitTags()
	})
	return &out, err
}

// UpdateTag renames or recolors a tag.
func (s *Service) UpdateTag(ctx context.Context, actor authz.Actor, id string, in TagInput) (*TagDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var out TagDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		var t store.Tag
		if err := tx.db.Where("id = ?", id).Take(&t).Error; err != nil {
			if isNotFound(err) {
				return notFound("tag")
			}
			return err
		}
		before := tagDTO(t)
		if in.Name != nil {
			name, err := s.checkTagName(tx.db, *in.Name, t.ID)
			if err != nil {
				return err
			}
			t.Name = name
		}
		if in.ColorSlot != nil {
			if !validSlot(*in.ColorSlot) {
				return invalidField("colorSlot", "colorSlot must be tag-1 … tag-8")
			}
			t.ColorSlot = *in.ColorSlot
		}
		t.UpdatedAt = tx.now
		if err := tx.db.Save(&t).Error; err != nil {
			return err
		}
		out = tagDTO(t)
		if err := tx.record("tag.updated", "tag", t.ID, nil, before, out); err != nil {
			return err
		}
		return tx.emitTags()
	})
	return &out, err
}

// DeleteTag removes a tag from every event.
func (s *Service) DeleteTag(ctx context.Context, actor authz.Actor, id string) error {
	if !authz.CanAdminister(actor) {
		return forbidden("admins only")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		var t store.Tag
		if err := tx.db.Where("id = ?", id).Take(&t).Error; err != nil {
			if isNotFound(err) {
				return notFound("tag")
			}
			return err
		}
		if err := tx.db.Where("tag_id = ?", id).Delete(&store.EventTag{}).Error; err != nil {
			return err
		}
		if err := tx.db.Delete(&t).Error; err != nil {
			return err
		}
		if err := tx.record("tag.deleted", "tag", t.ID, nil, tagDTO(t), nil); err != nil {
			return err
		}
		return tx.emitTags()
	})
}

// ReorderTags sets the display order; ids must list every tag once.
func (s *Service) ReorderTags(ctx context.Context, actor authz.Actor, ids []string) ([]TagDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var out []TagDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		var all []string
		if err := tx.db.Model(&store.Tag{}).Pluck("id", &all).Error; err != nil {
			return err
		}
		if len(uniq(ids)) != len(ids) || len(ids) != len(all) {
			return invalidField("ids", "list every tag exactly once")
		}
		known := map[string]bool{}
		for _, id := range all {
			known[id] = true
		}
		for i, id := range ids {
			if !known[id] {
				return invalidField("ids", "unknown tag")
			}
			if err := tx.db.Model(&store.Tag{}).Where("id = ?", id).Updates(map[string]any{"sort_order": i, "updated_at": tx.now}).Error; err != nil {
				return err
			}
		}
		if err := tx.record("tag.reordered", "tag", "tags", nil, nil, ids); err != nil {
			return err
		}
		var err error
		if out, err = listTags(tx.db); err != nil {
			return err
		}
		return tx.emitTags()
	})
	return out, err
}
