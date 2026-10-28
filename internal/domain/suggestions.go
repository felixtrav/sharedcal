package domain

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// Suggestion fields, in display order.
var suggestionFields = []string{"title", "description", "location", "start", "end", "timeZone", "allDay", "rrule", "status", "tags"}

const maxReviewNote = 2000

// FieldChange is one proposed field in a suggestion.
type FieldChange struct {
	Base     any  `json:"base"`
	Proposed any  `json:"proposed"`
	Current  any  `json:"current"`
	Conflict bool `json:"conflict"`
}

// SuggestionDTO is a suggestion as sent to clients.
type SuggestionDTO struct {
	ID             string                      `json:"id"`
	EventID        string                      `json:"eventId"`
	Author         UserRef                     `json:"author"`
	Kind           string                      `json:"kind" enum:"edit,delete"`
	BaseVersion    int64                       `json:"baseVersion"`
	Changes        map[string]FieldChange      `json:"changes"`
	Status         string                      `json:"status" enum:"open,accepted,partially_accepted,rejected,withdrawn,closed"`
	Reviewer       *UserRef                    `json:"reviewer"`
	ReviewNote     string                      `json:"reviewNote"`
	AcceptedFields []string                    `json:"acceptedFields" enum:"title,description,location,start,end,timeZone,allDay,rrule,status,tags"`
	CreatedAt      time.Time                   `json:"createdAt"`
	ResolvedAt     *time.Time                  `json:"resolvedAt"`
	EventOwnerID   string                      `json:"eventOwnerId"`
	Permissions    authz.SuggestionPermissions `json:"permissions"`
}

// SuggestionInput proposes a change.
type SuggestionInput struct {
	Kind        string     `json:"kind" enum:"edit,delete"`
	BaseVersion int64      `json:"baseVersion"`
	Changes     EventInput `json:"changes,omitempty"`
}

// AcceptInput selects which fields to apply and how to resolve conflicts.
type AcceptInput struct {
	Fields      []string          `json:"fields,omitempty" maxItems:"10"`
	Resolutions map[string]string `json:"resolutions,omitempty"`
}

// storedPatch is the patch_json format: only changed fields.
type storedPatch struct {
	Changes  map[string]any `json:"changes"`
	Base     map[string]any `json:"base"`
	Accepted []string       `json:"accepted,omitempty"`
}

// canonical returns the comparable value of every suggestion field.
// Times are UTC instants for timed events and dates for all-day events.
func canonical(e *store.Event, tags []string) map[string]any {
	t := func(x time.Time) string {
		if e.AllDay {
			return x.UTC().Format(dateLayout)
		}
		return x.UTC().Format(time.RFC3339)
	}
	if tags == nil {
		tags = []string{}
	}
	return map[string]any{
		"title": e.Title, "description": e.Description, "location": e.Location,
		"start": t(e.StartAt), "end": t(e.EndAt), "timeZone": e.TimeZone, "allDay": e.AllDay,
		"rrule": e.RRule, "status": e.Status, "tags": tags,
	}
}

// normalizeJSON round-trips through JSON so values compare consistently
// with what was stored.
func normalizeJSON(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func equalValues(a, b any) bool { return reflect.DeepEqual(normalizeJSON(a), normalizeJSON(b)) }

func authzSuggestion(sg *store.Suggestion, ownerID string) authz.Suggestion {
	return authz.Suggestion{AuthorID: sg.AuthorID, EventOwnerID: ownerID, Open: sg.Status == store.SuggestionOpen}
}

func (s *Service) suggestionDTOs(db *gorm.DB, actor authz.Actor, sgs []store.Suggestion, ev *store.Event) ([]SuggestionDTO, error) {
	var ids []string
	for _, sg := range sgs {
		ids = append(ids, sg.AuthorID)
		if sg.ReviewerID != nil {
			ids = append(ids, *sg.ReviewerID)
		}
	}
	refs, err := userRefs(db, ids)
	if err != nil {
		return nil, err
	}
	tagMap, err := eventTags(db, []string{ev.ID})
	if err != nil {
		return nil, err
	}
	current := canonical(ev, tagMap[ev.ID])
	out := make([]SuggestionDTO, len(sgs))
	for i := range sgs {
		sg := &sgs[i]
		var p storedPatch
		_ = json.Unmarshal([]byte(sg.PatchJSON), &p)
		changes := map[string]FieldChange{}
		for f, v := range p.Changes {
			changes[f] = FieldChange{Base: p.Base[f], Proposed: v, Current: current[f], Conflict: sg.Status == store.SuggestionOpen && !equalValues(current[f], p.Base[f])}
		}
		d := SuggestionDTO{
			ID: sg.ID, EventID: sg.EventID, Author: refs[sg.AuthorID], Kind: sg.Kind, BaseVersion: sg.BaseVersion,
			Changes: changes, Status: sg.Status, ReviewNote: sg.ReviewNote, AcceptedFields: p.Accepted,
			CreatedAt: sg.CreatedAt, ResolvedAt: sg.ResolvedAt, EventOwnerID: ev.OwnerID,
			Permissions: authz.SuggestionPerms(actor, authzSuggestion(sg, ev.OwnerID)),
		}
		if d.AcceptedFields == nil {
			d.AcceptedFields = []string{}
		}
		if sg.ReviewerID != nil {
			r := refs[*sg.ReviewerID]
			d.Reviewer = &r
		}
		out[i] = d
	}
	return out, nil
}

// ListSuggestions lists an event's suggestions (open only unless all).
func (s *Service) ListSuggestions(ctx context.Context, actor authz.Actor, eventID string, all bool) ([]SuggestionDTO, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	ev, err := s.loadEvent(db, eventID, false)
	if err != nil {
		return nil, err
	}
	q := db.Where("event_id = ?", eventID)
	if !all {
		q = q.Where("status = ?", store.SuggestionOpen)
	}
	var rows []store.Suggestion
	if err := q.Order("created_at, id").Limit(1000).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.suggestionDTOs(db, actor, rows, ev)
}

// CreateSuggestion stores a field-level patch (or a delete proposal) against
// someone else's event.
func (s *Service) CreateSuggestion(ctx context.Context, actor authz.Actor, eventID string, in SuggestionInput) (*SuggestionDTO, error) {
	if in.Kind != store.SuggestionEdit && in.Kind != store.SuggestionDelete {
		return nil, invalidField("kind", "kind must be edit or delete")
	}
	if in.Changes.OwnerID != nil {
		return nil, invalidField("changes.ownerId", "ownership cannot be suggested")
	}
	var out *SuggestionDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		ev, err := s.loadEvent(tx.db, eventID, false)
		if err != nil {
			return err
		}
		if !authz.CanSuggest(actor, authzEvent(ev)) {
			if authz.CanEditEvent(actor, authzEvent(ev)) {
				return forbidden("you own this event; edit it directly")
			}
			return forbidden("you cannot suggest changes to this event")
		}
		tagMap, err := eventTags(tx.db, []string{ev.ID})
		if err != nil {
			return err
		}
		current := canonical(ev, tagMap[ev.ID])
		p := storedPatch{Changes: map[string]any{}, Base: map[string]any{}}
		if in.Kind == store.SuggestionEdit {
			if in.Changes.empty() {
				return invalid("a suggestion needs at least one change")
			}
			// Validate the proposal exactly as an edit would be validated.
			probe := *ev
			resolved, err := s.resolveInput(tx.db, &probe, in.Changes, false)
			if err != nil {
				return err
			}
			resolved.apply(&probe)
			newTags := tagMap[ev.ID]
			if resolved.tags != nil {
				newTags = *resolved.tags
			}
			proposed := canonical(&probe, newTags)
			for _, f := range suggestionFields {
				if !equalValues(proposed[f], current[f]) {
					p.Changes[f], p.Base[f] = proposed[f], current[f]
				}
			}
			if len(p.Changes) == 0 {
				return invalid("the suggestion does not change anything")
			}
		}
		data, _ := json.Marshal(p)
		sg := store.Suggestion{
			ID: store.NewID(), EventID: ev.ID, AuthorID: actor.ID, Kind: in.Kind, PatchJSON: string(data),
			BaseVersion: ev.Version, Status: store.SuggestionOpen, CreatedAt: tx.now, UpdatedAt: tx.now,
		}
		if in.BaseVersion > 0 && in.BaseVersion != ev.Version {
			// The form was built from an older version: make the author look
			// at the current event rather than propose against stale values.
			cur, err := s.eventDTO(tx.db, actor, ev)
			if err != nil {
				return err
			}
			return conflict("the event changed since you opened it", cur)
		}
		if err := tx.db.Create(&sg).Error; err != nil {
			return err
		}
		if err := tx.record("suggestion.created", "suggestion", sg.ID, &ev.ID, nil, p); err != nil {
			return err
		}
		if err := tx.notify(ev.OwnerID, "suggestion.created", "suggestion", sg.ID, &ev.ID); err != nil {
			return err
		}
		dtos, err := s.suggestionDTOs(tx.db, actor, []store.Suggestion{sg}, ev)
		if err != nil {
			return err
		}
		out = &dtos[0]
		return tx.emit("suggestion.created", sg.ID, 0, out, "")
	})
	return out, err
}

func (s *Service) loadSuggestion(db *gorm.DB, id string) (*store.Suggestion, *store.Event, error) {
	sg, err := take[store.Suggestion](db, id, "suggestion")
	if err != nil {
		return nil, nil, err
	}
	ev, err := s.loadEvent(db, sg.EventID, true)
	if err != nil {
		return nil, nil, err
	}
	return &sg, ev, nil
}

// AcceptResult is returned by AcceptSuggestion.
type AcceptResult struct {
	Suggestion SuggestionDTO `json:"suggestion"`
	Event      *EventDTO     `json:"event"`
}

// AcceptSuggestion applies all or some fields as an edit attributed to the
// reviewer, crediting the suggester in the activity log. Conflicting fields
// (changed since the suggestion) need an explicit resolution.
func (s *Service) AcceptSuggestion(ctx context.Context, actor authz.Actor, id string, in AcceptInput) (*AcceptResult, error) {
	var out AcceptResult
	err := s.write(ctx, actor, func(tx *Tx) error {
		sg, ev, err := s.loadSuggestion(tx.db, id)
		if err != nil {
			return err
		}
		if !authz.CanReviewSuggestion(actor, authzSuggestion(sg, ev.OwnerID)) {
			return forbidden("only the event owner or an admin can review suggestions")
		}
		if ev.DeletedAt != nil {
			return invalid("the event was deleted")
		}
		var p storedPatch
		if err := json.Unmarshal([]byte(sg.PatchJSON), &p); err != nil {
			return err
		}
		credit := map[string]any{"suggestionId": sg.ID, "suggestedBy": sg.AuthorID}
		// accepted logs the acceptance and fills in the resolved suggestion.
		accepted := func() error {
			if err := tx.record("suggestion.accepted", "suggestion", sg.ID, &ev.ID, nil, credit); err != nil {
				return err
			}
			dtos, err := s.suggestionDTOs(tx.db, actor, []store.Suggestion{*sg}, ev)
			if err != nil {
				return err
			}
			out.Suggestion = dtos[0]
			return nil
		}

		if sg.Kind == store.SuggestionDelete {
			sg.Status = store.SuggestionAccepted
			if err := s.resolveSuggestion(tx, sg, ev); err != nil {
				return err
			}
			if err := s.applyDelete(tx, ev, ScopeAll, nil, "event.deleted"); err != nil {
				return err
			}
			return accepted()
		}

		fields := in.Fields
		if len(fields) == 0 {
			for f := range p.Changes {
				fields = append(fields, f)
			}
		}
		tagMap, err := eventTags(tx.db, []string{ev.ID})
		if err != nil {
			return err
		}
		current := canonical(ev, tagMap[ev.ID])
		var fe fieldErrors
		var apply []string
		for _, f := range uniq(fields) {
			if _, ok := p.Changes[f]; !ok {
				fe.add("fields", "%s is not part of this suggestion", f)
				continue
			}
			if !equalValues(current[f], p.Base[f]) {
				switch in.Resolutions[f] {
				case "proposed":
				case "current":
					continue
				default:
					fe.add("resolutions."+f, "%s changed since the suggestion; choose current or proposed", f)
					continue
				}
			}
			apply = append(apply, f)
		}
		if err := fe.err(); err != nil {
			return err
		}
		if len(apply) == 0 {
			return invalid("no fields selected to apply")
		}
		input := patchToInput(p.Changes, apply)
		slices.Sort(apply)
		p.Accepted = apply
		data, _ := json.Marshal(p)
		sg.PatchJSON = string(data)
		sg.Status = store.SuggestionAccepted
		if len(apply) < len(p.Changes) {
			sg.Status = store.SuggestionPartiallyAccepted
		}
		if err := s.resolveSuggestion(tx, sg, ev); err != nil {
			return err
		}
		dto, err := s.applyUpdate(tx, ev, ScopeAll, nil, input, "event.updated")
		if err != nil {
			return err
		}
		out.Event = dto
		credit["fields"] = apply
		return accepted()
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// patchToInput converts stored canonical values back into an EventInput.
func patchToInput(changes map[string]any, fields []string) EventInput {
	var in EventInput
	str := func(v any) *string {
		s, _ := v.(string)
		return &s
	}
	for _, f := range fields {
		v := changes[f]
		switch f {
		case "title":
			in.Title = str(v)
		case "description":
			in.Description = str(v)
		case "location":
			in.Location = str(v)
		case "start":
			in.Start = str(v)
		case "end":
			in.End = str(v)
		case "timeZone":
			in.TimeZone = str(v)
		case "rrule":
			in.RRule = str(v)
		case "status":
			in.Status = str(v)
		case "allDay":
			b, _ := v.(bool)
			in.AllDay = &b
		case "tags":
			var tags []string
			if list, ok := v.([]any); ok {
				for _, t := range list {
					if s, ok := t.(string); ok {
						tags = append(tags, s)
					}
				}
			}
			if tags == nil {
				tags = []string{}
			}
			in.Tags = &tags
		}
	}
	return in
}

// RejectSuggestion closes a suggestion with an optional note.
func (s *Service) RejectSuggestion(ctx context.Context, actor authz.Actor, id, note string) (*SuggestionDTO, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > maxReviewNote {
		return nil, invalidField("note", "note is limited to %d characters", maxReviewNote)
	}
	return s.closeOne(ctx, actor, id, func(sg *store.Suggestion, ev *store.Event) error {
		if !authz.CanReviewSuggestion(actor, authzSuggestion(sg, ev.OwnerID)) {
			return forbidden("only the event owner or an admin can review suggestions")
		}
		sg.Status = store.SuggestionRejected
		sg.ReviewNote = note
		return nil
	}, "suggestion.rejected")
}

// WithdrawSuggestion lets the author retract an open suggestion.
func (s *Service) WithdrawSuggestion(ctx context.Context, actor authz.Actor, id string) (*SuggestionDTO, error) {
	return s.closeOne(ctx, actor, id, func(sg *store.Suggestion, ev *store.Event) error {
		if !authz.CanWithdrawSuggestion(actor, authzSuggestion(sg, ev.OwnerID)) {
			return forbidden("only the author can withdraw an open suggestion")
		}
		sg.Status = store.SuggestionWithdrawn
		return nil
	}, "suggestion.withdrawn")
}

func (s *Service) closeOne(ctx context.Context, actor authz.Actor, id string, decide func(*store.Suggestion, *store.Event) error, action string) (*SuggestionDTO, error) {
	var out *SuggestionDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		sg, ev, err := s.loadSuggestion(tx.db, id)
		if err != nil {
			return err
		}
		if err := decide(sg, ev); err != nil {
			return err
		}
		if err := s.resolveSuggestion(tx, sg, ev); err != nil {
			return err
		}
		if err := tx.record(action, "suggestion", sg.ID, &ev.ID, nil, map[string]string{"note": sg.ReviewNote}); err != nil {
			return err
		}
		dtos, err := s.suggestionDTOs(tx.db, actor, []store.Suggestion{*sg}, ev)
		if err != nil {
			return err
		}
		out = &dtos[0]
		return nil
	})
	return out, err
}

// resolveSuggestion saves a decided suggestion, notifies its author and
// broadcasts suggestion.resolved.
func (s *Service) resolveSuggestion(tx *Tx, sg *store.Suggestion, ev *store.Event) error {
	now := tx.now
	sg.ResolvedAt = &now
	sg.UpdatedAt = now
	if sg.Status != store.SuggestionWithdrawn && sg.Status != store.SuggestionClosed && tx.actor.ID != "" {
		r := tx.actor.ID
		sg.ReviewerID = &r
	}
	res := tx.db.Model(&store.Suggestion{}).Where("id = ? AND status = ?", sg.ID, store.SuggestionOpen).Updates(map[string]any{
		"status": sg.Status, "review_note": sg.ReviewNote, "reviewer_id": sg.ReviewerID, "resolved_at": sg.ResolvedAt,
		"patch_json": sg.PatchJSON, "updated_at": now,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return invalid("the suggestion is no longer open")
	}
	if sg.Status != store.SuggestionWithdrawn {
		if err := tx.notify(sg.AuthorID, "suggestion.resolved", "suggestion", sg.ID, &ev.ID); err != nil {
			return err
		}
	}
	dtos, err := s.suggestionDTOs(tx.db, tx.actor, []store.Suggestion{*sg}, ev)
	if err != nil {
		return err
	}
	return tx.emit("suggestion.resolved", sg.ID, 0, dtos[0], "")
}

// closeSuggestions closes every open suggestion of a deleted event.
func (s *Service) closeSuggestions(tx *Tx, eventID string) error {
	var open []store.Suggestion
	if err := tx.db.Where("event_id = ? AND status = ?", eventID, store.SuggestionOpen).Find(&open).Error; err != nil {
		return err
	}
	if len(open) == 0 {
		return nil
	}
	ev, err := s.loadEvent(tx.db, eventID, true)
	if err != nil {
		return err
	}
	for i := range open {
		open[i].Status = store.SuggestionClosed
		if err := s.resolveSuggestion(tx, &open[i], ev); err != nil {
			return err
		}
	}
	return nil
}
