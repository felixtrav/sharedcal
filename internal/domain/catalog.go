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
	maxCatalogOther     = 500
	maxOpenCatalogReqs  = 20
	maxCatalogListLimit = 500
)

// CatalogRequestDTO is a non-admin's request for a new tag or event type.
// Icon is empty for tags.
type CatalogRequestDTO struct {
	ID            string                          `json:"id"`
	Kind          string                          `json:"kind" enum:"tag,event_type"`
	Name          string                          `json:"name"`
	Icon          string                          `json:"icon"`
	ColorSlot     string                          `json:"colorSlot" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
	Other         string                          `json:"other" doc:"The requester's optional note to the admins"`
	Author        UserRef                         `json:"author"`
	Status        string                          `json:"status" enum:"open,approved,rejected,withdrawn"`
	Reviewer      *UserRef                        `json:"reviewer"`
	ReviewNote    string                          `json:"reviewNote"`
	CreatedItemID *string                         `json:"createdItemId" doc:"The tag or event type an approval created"`
	CreatedAt     time.Time                       `json:"createdAt"`
	ResolvedAt    *time.Time                      `json:"resolvedAt"`
	Permissions   authz.CatalogRequestPermissions `json:"permissions"`
}

// CatalogRequestInput asks for a new tag or event type. A request carries a
// name, a color and (for event types) an icon, plus an optional note; every
// other setting is up to the admin who approves it.
type CatalogRequestInput struct {
	Kind      string  `json:"kind" enum:"tag,event_type"`
	Name      string  `json:"name" maxLength:"64"`
	ColorSlot string  `json:"colorSlot" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
	Icon      *string `json:"icon,omitempty" enum:"calendar,cake,gift,party-popper,flag,plane,heart,star,briefcase,graduation-cap,utensils,music,trophy,stethoscope,home,users,bell,gamepad,film,ball" doc:"Event types only"`
	Other     string  `json:"other,omitempty" maxLength:"500" doc:"Optional note to the admins"`
}

// CatalogApproveInput lets the approving admin adjust the request first.
type CatalogApproveInput struct {
	Name      *string `json:"name,omitempty" maxLength:"64"`
	ColorSlot *string `json:"colorSlot,omitempty" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
	Icon      *string `json:"icon,omitempty" enum:"calendar,cake,gift,party-popper,flag,plane,heart,star,briefcase,graduation-cap,utensils,music,trophy,stethoscope,home,users,bell,gamepad,film,ball" doc:"Event types only"`
}

func authzCatalog(r *store.CatalogRequest) authz.CatalogRequest {
	return authz.CatalogRequest{AuthorID: r.AuthorID, Open: r.Status == store.CatalogOpen}
}

func (s *Service) catalogDTOs(db *gorm.DB, actor authz.Actor, rows []store.CatalogRequest) ([]CatalogRequestDTO, error) {
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.AuthorID)
		if r.ReviewerID != nil {
			ids = append(ids, *r.ReviewerID)
		}
	}
	refs, err := userRefs(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogRequestDTO, len(rows))
	for i := range rows {
		r := &rows[i]
		d := CatalogRequestDTO{
			ID: r.ID, Kind: r.Kind, Name: r.Name, Icon: r.Icon, ColorSlot: r.ColorSlot, Other: r.OtherText,
			Author: refs[r.AuthorID], Status: r.Status, ReviewNote: r.ReviewNote, CreatedItemID: r.CreatedItemID,
			CreatedAt: r.CreatedAt, ResolvedAt: r.ResolvedAt, Permissions: authz.CatalogRequestPerms(actor, authzCatalog(r)),
		}
		if r.ReviewerID != nil {
			ref := refs[*r.ReviewerID]
			d.Reviewer = &ref
		}
		out[i] = d
	}
	return out, nil
}

// emitCatalogRequests tells open browsers to refetch their request lists.
// The message carries no request data, because requests are private to their
// author and the admins while the stream reaches everyone.
func (tx *Tx) emitCatalogRequests(id string) error {
	return tx.emit("catalogrequest.changed", id, 0, map[string]string{"id": id}, "")
}

// ListCatalogRequests lists requests, newest first. Admins see everyone's;
// everyone else sees their own. Unless all is set, only open requests are
// listed, oldest first.
func (s *Service) ListCatalogRequests(ctx context.Context, actor authz.Actor, all bool) ([]CatalogRequestDTO, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	q := db.Model(&store.CatalogRequest{})
	if actor.Role != authz.Admin {
		q = q.Where("author_id = ?", actor.ID)
	}
	if all {
		q = q.Order("created_at DESC, id")
	} else {
		q = q.Where("status = ?", store.CatalogOpen).Order("created_at, id")
	}
	var rows []store.CatalogRequest
	if err := q.Limit(maxCatalogListLimit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.catalogDTOs(db, actor, rows)
}

// CountOpenCatalogRequests is the size of the review queue, for admins.
func (s *Service) CountOpenCatalogRequests(ctx context.Context, actor authz.Actor) (int64, error) {
	if !authz.CanAdminister(actor) {
		return 0, forbidden("admins only")
	}
	var n int64
	err := s.DB.R.WithContext(ctx).Model(&store.CatalogRequest{}).Where("status = ?", store.CatalogOpen).Count(&n).Error
	return n, err
}

func validCatalogKind(k string) bool { return k == store.CatalogTag || k == store.CatalogEventType }

// checkCatalogName validates a requested name against the existing tags or
// event types of the same kind.
func (s *Service) checkCatalogName(db *gorm.DB, kind, name string) (string, error) {
	if kind == store.CatalogTag {
		return s.checkTagName(db, name, "")
	}
	return s.checkTypeName(db, name, "")
}

// catalogIcon returns the icon a request stores: none for tags, a known icon
// (calendar by default) for event types.
func catalogIcon(kind string, icon *string) (string, error) {
	if kind == store.CatalogTag {
		if icon != nil && *icon != "" {
			return "", invalidField("icon", "tags have no icon")
		}
		return "", nil
	}
	if icon == nil || *icon == "" {
		return "calendar", nil
	}
	if !validIcon(*icon) {
		return "", invalidField("icon", "unknown icon")
	}
	return *icon, nil
}

// CreateCatalogRequest files a request for a new tag or event type.
func (s *Service) CreateCatalogRequest(ctx context.Context, actor authz.Actor, in CatalogRequestInput) (*CatalogRequestDTO, error) {
	if !authz.CanRequestCatalogItem(actor) {
		if authz.CanAdminister(actor) {
			return nil, forbidden("admins create tags and event types directly")
		}
		return nil, forbidden("not allowed")
	}
	if !validCatalogKind(in.Kind) {
		return nil, invalidField("kind", "kind must be tag or event_type")
	}
	if !validSlot(in.ColorSlot) {
		return nil, invalidField("colorSlot", "colorSlot must be tag-1 … tag-8")
	}
	icon, err := catalogIcon(in.Kind, in.Icon)
	if err != nil {
		return nil, err
	}
	other := strings.TrimSpace(in.Other)
	if utf8.RuneCountInString(other) > maxCatalogOther {
		return nil, invalidField("other", "the note is limited to %d characters", maxCatalogOther)
	}
	var out *CatalogRequestDTO
	err = s.write(ctx, actor, func(tx *Tx) error {
		name, err := s.checkCatalogName(tx.db, in.Kind, in.Name)
		if err != nil {
			return err
		}
		var dup int64
		if err := tx.db.Model(&store.CatalogRequest{}).
			Where("kind = ? AND status = ? AND LOWER(name) = ?", in.Kind, store.CatalogOpen, strings.ToLower(name)).
			Count(&dup).Error; err != nil {
			return err
		}
		if dup > 0 {
			return invalidField("name", "this has already been requested")
		}
		var mine int64
		if err := tx.db.Model(&store.CatalogRequest{}).Where("author_id = ? AND status = ?", actor.ID, store.CatalogOpen).Count(&mine).Error; err != nil {
			return err
		}
		if mine >= maxOpenCatalogReqs {
			return limit("you have too many open requests; wait for a review or withdraw some")
		}
		r := store.CatalogRequest{
			ID: store.NewID(), Kind: in.Kind, Name: name, Icon: icon, ColorSlot: in.ColorSlot, OtherText: other,
			AuthorID: actor.ID, Status: store.CatalogOpen, CreatedAt: tx.now, UpdatedAt: tx.now,
		}
		if err := tx.db.Create(&r).Error; err != nil {
			return err
		}
		dtos, err := s.catalogDTOs(tx.db, actor, []store.CatalogRequest{r})
		if err != nil {
			return err
		}
		out = &dtos[0]
		if err := tx.record("catalogrequest.created", "catalog_request", r.ID, nil, nil, out); err != nil {
			return err
		}
		return tx.emitCatalogRequests(r.ID)
	})
	return out, err
}

// ApproveCatalogRequest creates the requested tag or event type. The admin may
// adjust the name, color or icon first. If the name has been taken since, the
// admin must rename it or reject the request.
func (s *Service) ApproveCatalogRequest(ctx context.Context, actor authz.Actor, id string, in CatalogApproveInput) (*CatalogRequestDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var out *CatalogRequestDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		r, err := take[store.CatalogRequest](tx.db, id, "request")
		if err != nil {
			return err
		}
		if !authz.CanReviewCatalogRequest(actor, authzCatalog(&r)) {
			return invalid("the request is no longer open")
		}
		if in.Name != nil {
			r.Name = *in.Name
		}
		if in.ColorSlot != nil {
			if !validSlot(*in.ColorSlot) {
				return invalidField("colorSlot", "colorSlot must be tag-1 … tag-8")
			}
			r.ColorSlot = *in.ColorSlot
		}
		if in.Icon != nil {
			if r.Icon, err = catalogIcon(r.Kind, in.Icon); err != nil {
				return err
			}
		}
		var itemID string
		if r.Kind == store.CatalogTag {
			t, err := s.insertTag(tx, r.Name, r.ColorSlot)
			if err != nil {
				return err
			}
			itemID = t.ID
		} else {
			name, color, icon := r.Name, r.ColorSlot, r.Icon
			t, err := s.insertEventType(tx, EventTypeInput{Name: &name, ColorSlot: &color}, icon)
			if err != nil {
				return err
			}
			itemID = t.ID
		}
		r.Status, r.CreatedItemID = store.CatalogApproved, &itemID
		if err := s.resolveCatalogRequest(tx, &r); err != nil {
			return err
		}
		dtos, err := s.catalogDTOs(tx.db, actor, []store.CatalogRequest{r})
		if err != nil {
			return err
		}
		out = &dtos[0]
		if err := tx.record("catalogrequest.approved", "catalog_request", r.ID, nil, nil, map[string]any{"requestedBy": r.AuthorID, "createdItemId": itemID}); err != nil {
			return err
		}
		return tx.emitCatalogRequests(r.ID)
	})
	return out, err
}

// RejectCatalogRequest closes a request with an optional note.
func (s *Service) RejectCatalogRequest(ctx context.Context, actor authz.Actor, id, note string) (*CatalogRequestDTO, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxReviewNote {
		return nil, invalidField("note", "note is limited to %d characters", maxReviewNote)
	}
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	return s.closeCatalogRequest(ctx, actor, id, "catalogrequest.rejected", func(r *store.CatalogRequest) error {
		if !authz.CanReviewCatalogRequest(actor, authzCatalog(r)) {
			return invalid("the request is no longer open")
		}
		r.Status, r.ReviewNote = store.CatalogRejected, note
		return nil
	})
}

// WithdrawCatalogRequest lets the author retract an open request.
func (s *Service) WithdrawCatalogRequest(ctx context.Context, actor authz.Actor, id string) (*CatalogRequestDTO, error) {
	return s.closeCatalogRequest(ctx, actor, id, "catalogrequest.withdrawn", func(r *store.CatalogRequest) error {
		if !authz.CanWithdrawCatalogRequest(actor, authzCatalog(r)) {
			return forbidden("only the author can withdraw an open request")
		}
		r.Status = store.CatalogWithdrawn
		return nil
	})
}

func (s *Service) closeCatalogRequest(ctx context.Context, actor authz.Actor, id, action string, decide func(*store.CatalogRequest) error) (*CatalogRequestDTO, error) {
	var out *CatalogRequestDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		r, err := take[store.CatalogRequest](tx.db, id, "request")
		if err != nil {
			return err
		}
		if err := decide(&r); err != nil {
			return err
		}
		if err := s.resolveCatalogRequest(tx, &r); err != nil {
			return err
		}
		dtos, err := s.catalogDTOs(tx.db, actor, []store.CatalogRequest{r})
		if err != nil {
			return err
		}
		out = &dtos[0]
		if err := tx.record(action, "catalog_request", r.ID, nil, nil, map[string]string{"note": r.ReviewNote}); err != nil {
			return err
		}
		return tx.emitCatalogRequests(r.ID)
	})
	return out, err
}

// resolveCatalogRequest saves a decided request. The status guard makes a
// concurrent second decision fail rather than overwrite the first.
func (s *Service) resolveCatalogRequest(tx *Tx, r *store.CatalogRequest) error {
	now := tx.now
	r.ResolvedAt, r.UpdatedAt = &now, now
	if r.Status != store.CatalogWithdrawn && tx.actor.ID != "" {
		reviewer := tx.actor.ID
		r.ReviewerID = &reviewer
	}
	res := tx.db.Model(&store.CatalogRequest{}).Where("id = ? AND status = ?", r.ID, store.CatalogOpen).Updates(map[string]any{
		"name": r.Name, "icon": r.Icon, "color_slot": r.ColorSlot, "status": r.Status, "review_note": r.ReviewNote,
		"reviewer_id": r.ReviewerID, "created_item_id": r.CreatedItemID, "resolved_at": r.ResolvedAt, "updated_at": now,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return invalid("the request is no longer open")
	}
	return nil
}
