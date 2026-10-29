package domain

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// Validation limits (server-side, mirrored by the UI).
const (
	maxTitle       = 200
	maxDescription = 20000
	maxLocation    = 1000
	maxTagsPerEvt  = 20
	maxEventLength = 366 * 24 * time.Hour
	maxRangeDays   = 400
	maxListMasters = 5000
	undoWindow     = 60 * time.Second
	trashRetention = 30 * 24 * time.Hour
)

const wallLayout = "2006-01-02T15:04:05"
const dateLayout = "2006-01-02"

// EventCounts are badges shown with an event.
type EventCounts struct {
	Comments        int64 `json:"comments"`
	OpenSuggestions int64 `json:"openSuggestions"`
	Attachments     int64 `json:"attachments"`
}

// EventDTO is the event payload described in the spec.
type EventDTO struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Location    string            `json:"location"`
	EventTypeID *string           `json:"eventTypeId"`
	Fields      map[string]any    `json:"fields"`
	Start       string            `json:"start"`
	End         string            `json:"end"`
	StartUTC    time.Time         `json:"startUtc"`
	EndUTC      time.Time         `json:"endUtc"`
	TimeZone    string            `json:"timeZone"`
	AllDay      bool              `json:"allDay"`
	RRule       string            `json:"rrule"`
	Exdates     []time.Time       `json:"exdates"`
	Status      string            `json:"status" enum:"confirmed,tentative"`
	Tags        []string          `json:"tags"`
	Owner       UserRef           `json:"owner"`
	Version     int64             `json:"version"`
	Permissions authz.Permissions `json:"permissions"`
	Counts      EventCounts       `json:"counts"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	DeletedAt   *time.Time        `json:"deletedAt,omitempty"`
	Occurrence  *time.Time        `json:"occurrence,omitempty"`
	// YearsSince counts whole years since the series' first occurrence, for
	// recurring events whose type has a year counter (omitted for the first year).
	YearsSince *int `json:"yearsSince,omitempty"`
	Overridden bool `json:"overridden,omitempty"`
}

// OverrideDTO describes one modified occurrence.
type OverrideDTO struct {
	ID           string     `json:"id"`
	RecurrenceID time.Time  `json:"recurrenceId"`
	Cancelled    bool       `json:"cancelled"`
	Title        *string    `json:"title,omitempty"`
	Description  *string    `json:"description,omitempty"`
	Location     *string    `json:"location,omitempty"`
	Start        *string    `json:"start,omitempty"`
	End          *string    `json:"end,omitempty"`
	StartUTC     *time.Time `json:"startUtc,omitempty"`
	EndUTC       *time.Time `json:"endUtc,omitempty"`
	AllDay       *bool      `json:"allDay,omitempty"`
	Status       *string    `json:"status,omitempty"`
	// TypeOverridden says this occurrence has its own type (nil = generic) and field values.
	TypeOverridden bool           `json:"typeOverridden,omitempty"`
	EventTypeID    *string        `json:"eventTypeId,omitempty"`
	Fields         map[string]any `json:"fields,omitempty"`
}

// EventDetail is a master event with its overrides and attachments.
type EventDetail struct {
	EventDTO
	Overrides   []OverrideDTO   `json:"overrides"`
	Attachments []AttachmentDTO `json:"attachments"`
}

// EventInput is the body of create, update and suggestion requests. Nil
// fields are left unchanged.
type EventInput struct {
	Title       *string         `json:"title,omitempty" maxLength:"200"`
	Start       *string         `json:"start,omitempty" maxLength:"40"`
	End         *string         `json:"end,omitempty" maxLength:"40"`
	TimeZone    *string         `json:"timeZone,omitempty" maxLength:"64"`
	AllDay      *bool           `json:"allDay,omitempty"`
	RRule       *string         `json:"rrule,omitempty" maxLength:"500"`
	Location    *string         `json:"location,omitempty" maxLength:"1000"`
	Description *string         `json:"description,omitempty" maxLength:"20000" doc:"Generic events only; typed events use fields"`
	EventTypeID *string         `json:"eventTypeId,omitempty" maxLength:"36" doc:"Event type id; an empty string makes the event generic again"`
	Fields      *map[string]any `json:"fields,omitempty" doc:"Values for the event type's fields, keyed by field key; replaces all values"`
	Tags        *[]string       `json:"tags,omitempty" maxItems:"20"`
	Status      *string         `json:"status,omitempty" enum:"confirmed,tentative"`
	OwnerID     *string         `json:"ownerId,omitempty" maxLength:"36" doc:"Admins only: transfer ownership"`
}

func (in EventInput) empty() bool {
	return in.Title == nil && in.Start == nil && in.End == nil && in.TimeZone == nil && in.AllDay == nil &&
		in.RRule == nil && in.Location == nil && in.Description == nil && in.EventTypeID == nil && in.Fields == nil && in.Tags == nil && in.Status == nil && in.OwnerID == nil
}

func authzEvent(e *store.Event) authz.Event {
	return authz.Event{OwnerID: e.OwnerID, Deleted: e.DeletedAt != nil}
}

// formatEventTime renders an instant as wall time in loc, or a date.
func formatEventTime(t time.Time, loc *time.Location, allDay bool) string {
	if allDay {
		return t.UTC().Format(dateLayout)
	}
	return t.In(loc).Format(wallLayout)
}

// parseEventTime accepts a date (all-day), an RFC 3339 instant, or a wall
// time in loc.
func parseEventTime(s string, loc *time.Location, allDay bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	if allDay {
		d, err := time.ParseInLocation(dateLayout, s, time.UTC)
		if err != nil {
			return time.Time{}, errMsg("use a date like 2026-10-30 for all-day events")
		}
		return d, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC().Truncate(time.Second), nil
	}
	for _, layout := range []string{wallLayout, "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, errMsg("use a date-time like 2026-10-30T19:00:00")
}

type errMsg string

func (e errMsg) Error() string { return string(e) }

// eventPatch is an EventInput resolved against the current event.
type eventPatch struct {
	title, description, location, status, timeZone, rrule *string
	start, end                                            *time.Time
	allDay                                                *bool
	tags                                                  *[]string
	ownerID                                               *string
	// typeSet reports that eventTypeID (nil = generic) replaces the type.
	typeSet     bool
	eventTypeID *string
	fields      *string // encoded field values
	// titleGenerated marks a title produced by the type's template rather than given by the user.
	titleGenerated bool
}

// resolveInput validates in against base (the event being changed, or a
// zero event when creating) and returns the resolved patch.
func (s *Service) resolveInput(db *gorm.DB, base *store.Event, in EventInput, creating bool) (*eventPatch, error) {
	return s.resolveInputWith(db, base, in, creating, true)
}

// resolveInputWith is resolveInput; enforceType false skips the event type's
// fixed all-day and repeat rules, for internal probes that build partial inputs.
func (s *Service) resolveInputWith(db *gorm.DB, base *store.Event, in EventInput, creating, enforceType bool) (*eventPatch, error) {
	var fe fieldErrors
	p := &eventPatch{}
	typeID := base.EventTypeID
	if in.EventTypeID != nil {
		p.typeSet = true
		if *in.EventTypeID == "" {
			typeID = nil
		} else {
			typeID = in.EventTypeID
		}
		p.eventTypeID = typeID
	}
	typeChanged := p.typeSet && !sameStrPtr(typeID, base.EventTypeID)
	if in.Description != nil {
		switch {
		case utf8.RuneCountInString(*in.Description) > maxDescription:
			fe.add("description", "description is limited to %d characters", maxDescription)
		case typeID != nil && strings.TrimSpace(*in.Description) != "":
			fe.add("description", "events with a type use the type's fields instead of a description")
		}
		p.description = in.Description
	}
	if typeID != nil && typeChanged {
		// Switching to a type replaces the free-form description.
		p.description = ptr("")
	}
	var et store.EventType
	typeFound := false
	if typeID != nil {
		if err := db.Where("id = ?", *typeID).Take(&et).Error; err != nil {
			if !isNotFound(err) {
				return nil, err
			}
			fe.add("eventTypeId", "unknown event type")
		} else {
			typeFound = true
		}
	}
	if typeID != nil && (typeChanged || in.Fields != nil) {
		if typeFound {
			// Values never carry over to a different type.
			vals := map[string]any{}
			if in.Fields != nil {
				vals = *in.Fields
			} else if !typeChanged {
				vals = parseFieldValues(base.FieldsJSON)
			}
			norm, ferrs, err := normalizeFields(db, parseFieldDefs(et.FieldsJSON), vals)
			if err != nil {
				return nil, err
			}
			fe = append(fe, ferrs...)
			p.fields = ptr(encodeFieldValues(norm))
			if et.TitleTemplate != "" {
				names, err := memberNames(db, et, norm)
				if err != nil {
					return nil, err
				}
				p.title, p.titleGenerated = ptr(renderTitle(et, norm, names, nil)), true
			}
		}
	} else if typeID == nil {
		if in.Fields != nil && len(*in.Fields) > 0 {
			fe.add("fields", "fields need an event type")
		}
		if in.Fields != nil || typeChanged {
			p.fields = ptr("{}")
		}
	}
	switch {
	case typeFound && et.TitleTemplate != "":
		// Titles of templated types are generated; a title in the request is ignored.
		if p.title == nil {
			vals := parseFieldValues(base.FieldsJSON)
			names, err := memberNames(db, et, vals)
			if err != nil {
				return nil, err
			}
			p.title, p.titleGenerated = ptr(renderTitle(et, vals, names, nil)), true
		}
	case in.Title != nil:
		t := strings.TrimSpace(*in.Title)
		if t == "" || utf8.RuneCountInString(t) > maxTitle {
			fe.add("title", "title must be 1–%d characters", maxTitle)
		}
		p.title = &t
	case creating:
		fe.add("title", "title is required")
	}
	if in.Location != nil {
		l := strings.TrimSpace(*in.Location)
		if utf8.RuneCountInString(l) > maxLocation {
			fe.add("location", "location is limited to %d characters", maxLocation)
		}
		p.location = &l
	}
	if in.Status != nil {
		if *in.Status != store.StatusConfirmed && *in.Status != store.StatusTentative {
			fe.add("status", "status must be confirmed or tentative")
		}
		p.status = in.Status
	}

	allDay := base.AllDay
	if in.AllDay != nil {
		allDay = *in.AllDay
		p.allDay = in.AllDay
	}
	tz := base.TimeZone
	if in.TimeZone != nil {
		tz = strings.TrimSpace(*in.TimeZone)
		p.timeZone = &tz
	}
	loc, err := loadLocation(tz)
	if err != nil {
		if creating && in.TimeZone == nil {
			fe.add("timeZone", "timeZone is required")
		} else {
			fe.add("timeZone", "unknown time zone")
		}
		loc = time.UTC
	}
	if creating && (in.Start == nil || in.End == nil) {
		fe.add("start", "start and end are required")
	}
	if !creating && in.AllDay != nil && *in.AllDay != base.AllDay && (in.Start == nil || in.End == nil) {
		fe.add("allDay", "changing all-day requires both start and end")
	}
	start, end := base.StartAt, base.EndAt
	if !creating && in.TimeZone != nil && in.Start == nil && in.End == nil && !allDay {
		// Changing only the zone keeps the wall-clock times.
		oldLoc := eventLoc(base)
		start = reinterpret(base.StartAt, oldLoc, loc)
		end = reinterpret(base.EndAt, oldLoc, loc)
	}
	if in.Start != nil {
		t, err := parseEventTime(*in.Start, loc, allDay)
		if err != nil {
			fe.add("start", "%v", err)
		}
		start = t
	}
	if in.End != nil {
		t, err := parseEventTime(*in.End, loc, allDay)
		if err != nil {
			fe.add("end", "%v", err)
		}
		end = t
	}
	if len(fe) == 0 {
		switch {
		case end.Before(start):
			fe.add("end", "end must not be before start")
		case end.Sub(start) > maxEventLength:
			fe.add("end", "events can last at most 366 days")
		case allDay && !end.After(start):
			fe.add("end", "all-day events end on a later date (end is exclusive)")
		case start.Year() < 1900 || start.Year() > 2200:
			fe.add("start", "start must be between 1900 and 2200")
		}
	}
	if in.Start != nil || in.End != nil || p.timeZone != nil || p.allDay != nil {
		p.start, p.end = &start, &end
	}

	rr := base.RRule
	if in.RRule != nil {
		rr = *in.RRule
	}
	if len(fe) == 0 && (in.RRule != nil || (rr != "" && p.start != nil)) {
		norm, err := normalizeRRule(rr, start, map[bool]*time.Location{true: time.UTC, false: loc}[allDay])
		if err != nil {
			fe.add("rrule", "%v", err)
		}
		p.rrule = &norm
	}

	// A type with fixed all-day or repeat settings holds every event of that type to them.
	if enforceType && typeFound && len(fe) == 0 && (creating || typeChanged || in.AllDay != nil || in.RRule != nil || in.Start != nil || in.End != nil) {
		fe = append(fe, typeRuleErrors(et, allDay, rr, start, end, loc)...)
	}

	if in.Tags != nil {
		tags := uniq(*in.Tags)
		if len(tags) > maxTagsPerEvt {
			fe.add("tags", "at most %d tags", maxTagsPerEvt)
		} else if len(tags) > 0 {
			var n int64
			if err := db.Model(&store.Tag{}).Where("id IN ?", tags).Count(&n).Error; err != nil {
				return nil, err
			}
			if int(n) != len(tags) {
				fe.add("tags", "unknown tag")
			}
		}
		p.tags = &tags
	}
	if in.OwnerID != nil {
		var u store.User
		err := db.Where("id = ? AND active = ?", *in.OwnerID, true).Take(&u).Error
		switch {
		case isNotFound(err):
			fe.add("ownerId", "owner not found or inactive")
		case err != nil:
			return nil, err
		case u.Role == string(authz.Viewer):
			fe.add("ownerId", "owner must be a writer or admin")
		}
		p.ownerID = in.OwnerID
	}
	if err := fe.err(); err != nil {
		return nil, err
	}
	return p, nil
}

func sameStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func reinterpret(t time.Time, from, to *time.Location) time.Time {
	w := t.In(from)
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, to).UTC()
}

// apply writes a resolved patch onto e and reports whether anything changed.
func (p *eventPatch) apply(e *store.Event) {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&e.Title, p.title)
	set(&e.Description, p.description)
	set(&e.Location, p.location)
	set(&e.Status, p.status)
	set(&e.TimeZone, p.timeZone)
	set(&e.RRule, p.rrule)
	set(&e.OwnerID, p.ownerID)
	set(&e.FieldsJSON, p.fields)
	if p.typeSet {
		e.EventTypeID = p.eventTypeID
	}
	if p.allDay != nil {
		e.AllDay = *p.allDay
	}
	if p.start != nil {
		e.StartAt = *p.start
	}
	if p.end != nil {
		e.EndAt = *p.end
	}
}

// eventSnapshot is what the activity log stores for an event.
type eventSnapshot struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Location    string    `json:"location"`
	Start       time.Time `json:"startUtc"`
	End         time.Time `json:"endUtc"`
	TimeZone    string    `json:"timeZone"`
	AllDay      bool      `json:"allDay"`
	RRule       string    `json:"rrule"`
	Status      string    `json:"status"`
	Tags        []string  `json:"tags"`
	EventTypeID *string   `json:"eventTypeId,omitempty"`
	Fields      any       `json:"fields,omitempty"`
	OwnerID     string    `json:"ownerId"`
	Version     int64     `json:"version"`
}

func snapshot(e *store.Event, tags []string) eventSnapshot {
	snap := eventSnapshot{Title: e.Title, Description: e.Description, Location: e.Location, Start: e.StartAt, End: e.EndAt,
		TimeZone: e.TimeZone, AllDay: e.AllDay, RRule: e.RRule, Status: e.Status, Tags: tags, EventTypeID: e.EventTypeID, OwnerID: e.OwnerID, Version: e.Version}
	if e.EventTypeID != nil {
		snap.Fields = parseFieldValues(e.FieldsJSON)
	}
	return snap
}

func (s *Service) loadEvent(db *gorm.DB, id string, includeDeleted bool) (*store.Event, error) {
	var e store.Event
	q := db.Where("id = ?", id)
	if !includeDeleted {
		q = q.Where("deleted_at IS NULL")
	}
	if err := q.Take(&e).Error; err != nil {
		if isNotFound(err) {
			return nil, notFound("event")
		}
		return nil, err
	}
	return &e, nil
}

func eventTags(db *gorm.DB, ids []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []store.EventTag
	if err := db.Where("event_id IN ?", ids).Order("event_id, position").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.EventID] = append(out[r.EventID], r.TagID)
	}
	return out, nil
}

func setEventTags(db *gorm.DB, eventID string, tags []string) error {
	if err := db.Where("event_id = ?", eventID).Delete(&store.EventTag{}).Error; err != nil {
		return err
	}
	for i, t := range tags {
		if err := db.Create(&store.EventTag{EventID: eventID, TagID: t, Position: i}).Error; err != nil {
			return err
		}
	}
	return nil
}

type countRow struct {
	EventID string
	N       int64
}

func countBy(db *gorm.DB, model any, where string, ids []string, args ...any) (map[string]int64, error) {
	out := map[string]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []countRow
	q := db.Model(model).Select("event_id, COUNT(*) AS n").Where("event_id IN ?", ids)
	if where != "" {
		q = q.Where(where, args...)
	}
	if err := q.Group("event_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.EventID] = r.N
	}
	return out, nil
}

// eventDTOs builds DTOs for masters with batched lookups.
func (s *Service) eventDTOs(db *gorm.DB, actor authz.Actor, evs []store.Event) ([]EventDTO, error) {
	ids := make([]string, len(evs))
	owners := make([]string, len(evs))
	for i := range evs {
		ids[i], owners[i] = evs[i].ID, evs[i].OwnerID
	}
	refs, err := userRefs(db, owners)
	if err != nil {
		return nil, err
	}
	tags, err := eventTags(db, ids)
	if err != nil {
		return nil, err
	}
	comments, err := countBy(db, &store.Comment{}, "deleted_at IS NULL", ids)
	if err != nil {
		return nil, err
	}
	sugg, err := countBy(db, &store.Suggestion{}, "status = ?", ids, store.SuggestionOpen)
	if err != nil {
		return nil, err
	}
	atts, err := countBy(db, &store.Attachment{}, "deleted_at IS NULL AND comment_id IS NULL AND scan_status IN ?", ids, []string{store.ScanClean, store.ScanSkipped})
	if err != nil {
		return nil, err
	}
	out := make([]EventDTO, len(evs))
	for i := range evs {
		e := &evs[i]
		loc := eventLoc(e)
		t := tags[e.ID]
		if t == nil {
			t = []string{}
		}
		out[i] = EventDTO{
			ID: e.ID, Title: e.Title, Description: e.Description, Location: e.Location,
			EventTypeID: e.EventTypeID, Fields: parseFieldValues(e.FieldsJSON),
			Start: formatEventTime(e.StartAt, loc, e.AllDay), End: formatEventTime(e.EndAt, loc, e.AllDay),
			StartUTC: e.StartAt.UTC(), EndUTC: e.EndAt.UTC(), TimeZone: e.TimeZone, AllDay: e.AllDay,
			RRule: e.RRule, Exdates: exdates(e), Status: e.Status, Tags: t, Owner: refs[e.OwnerID],
			Version: e.Version, Permissions: authz.EventPermissions(actor, authzEvent(e)),
			Counts:    EventCounts{Comments: comments[e.ID], OpenSuggestions: sugg[e.ID], Attachments: atts[e.ID]},
			CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt, DeletedAt: e.DeletedAt,
		}
		if out[i].Owner.ID == "" {
			out[i].Owner = UserRef{ID: e.OwnerID}
		}
	}
	return out, nil
}

func (s *Service) eventDTO(db *gorm.DB, actor authz.Actor, e *store.Event) (*EventDTO, error) {
	dtos, err := s.eventDTOs(db, actor, []store.Event{*e})
	if err != nil {
		return nil, err
	}
	return &dtos[0], nil
}

// ListQuery filters the range list.
type ListQuery struct {
	From, To time.Time
	Tags     []string
	Types    []string
	Owners   []string
	Q        string
}

// likeEscape escapes LIKE wildcards with '!' (portable across dialects).
func likeEscape(s string) string {
	r := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
	return "%" + r.Replace(strings.ToLower(s)) + "%"
}

// ListEvents returns every occurrence overlapping [From, To), with
// recurrences expanded server-side.
func (s *Service) ListEvents(ctx context.Context, actor authz.Actor, q ListQuery) ([]EventDTO, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	if !q.To.After(q.From) {
		return nil, invalidField("to", "to must be after from")
	}
	if q.To.Sub(q.From) > maxRangeDays*24*time.Hour {
		return nil, invalidField("to", "range is limited to %d days", maxRangeDays)
	}
	from, to := q.From.UTC(), q.To.UTC()
	db := s.DB.R.WithContext(ctx)
	tx := db.Model(&store.Event{}).
		Where("deleted_at IS NULL AND start_at < ?", to).
		Where("(rrule = '' AND (end_at > ? OR start_at >= ?)) OR (rrule <> '' AND (series_end_at IS NULL OR series_end_at > ?))", from, from, from)
	if len(q.Owners) > 0 {
		tx = tx.Where("owner_id IN ?", q.Owners)
	}
	if len(q.Tags) > 0 {
		tx = tx.Where("id IN (?)", db.Model(&store.EventTag{}).Select("event_id").Where("tag_id IN ?", q.Tags))
	}
	if len(q.Types) > 0 {
		// A series matches through its own type or an occurrence's; occurrences are filtered after expansion.
		tx = tx.Where("event_type_id IN ? OR id IN (?)", q.Types,
			db.Model(&store.EventOverride{}).Select("event_id").Where("type_overridden = ? AND event_type_id IN ?", true, q.Types))
	}
	if term := strings.TrimSpace(q.Q); term != "" {
		if utf8.RuneCountInString(term) > 200 {
			return nil, invalidField("q", "search is limited to 200 characters")
		}
		pat := likeEscape(term)
		tx = tx.Where("(LOWER(title) LIKE ? ESCAPE '!' OR LOWER(description) LIKE ? ESCAPE '!' OR LOWER(location) LIKE ? ESCAPE '!' OR (event_type_id IS NOT NULL AND LOWER(fields_json) LIKE ? ESCAPE '!') OR id IN (?))", pat, pat, pat, pat,
			db.Model(&store.EventOverride{}).Select("event_id").Where("type_overridden = ? AND LOWER(fields_json) LIKE ? ESCAPE '!'", true, pat))
	}
	term := strings.ToLower(strings.TrimSpace(q.Q))
	var masters []store.Event
	if err := tx.Order("start_at, id").Limit(maxListMasters).Find(&masters).Error; err != nil {
		return nil, err
	}
	var recurringIDs []string
	for _, m := range masters {
		if m.RRule != "" {
			recurringIDs = append(recurringIDs, m.ID)
		}
	}
	overrides := map[string][]store.EventOverride{}
	if len(recurringIDs) > 0 {
		var rows []store.EventOverride
		if err := db.Where("event_id IN ?", recurringIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, o := range rows {
			overrides[o.EventID] = append(overrides[o.EventID], o)
		}
	}
	dtos, err := s.eventDTOs(db, actor, masters)
	if err != nil {
		return nil, err
	}
	var counterRows []store.EventType
	if err := db.Select("id").Where("year_counter = ?", true).Find(&counterRows).Error; err != nil {
		return nil, err
	}
	counters := map[string]bool{}
	for _, r := range counterRows {
		counters[r.ID] = true
	}
	wanted := map[string]bool{}
	for _, id := range q.Types {
		wanted[id] = true
	}
	out := make([]EventDTO, 0, len(masters))
	for i := range masters {
		m := &masters[i]
		occs, err := expand(m, overrides[m.ID], from, to)
		if err != nil {
			s.Log.WarnContext(ctx, "skipping event with invalid recurrence", "event", m.ID, "err", err)
			continue
		}
		for _, o := range occs {
			d := occurrenceDTO(dtos[i], m, o, counters)
			if len(wanted) > 0 && (d.EventTypeID == nil || !wanted[*d.EventTypeID]) {
				continue
			}
			if term != "" && typeOverridden(overrides[m.ID]) && !occurrenceMatches(d, term) {
				// The series matched through another occurrence's type or fields.
				continue
			}
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartUTC.Before(out[j].StartUTC) })
	return out, nil
}

func typeOverridden(ovs []store.EventOverride) bool {
	for i := range ovs {
		if ovs[i].TypeOverridden {
			return true
		}
	}
	return false
}

// occurrenceMatches reports whether an occurrence's own text (which an override may have replaced) contains the lowercased term.
func occurrenceMatches(d EventDTO, term string) bool {
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), term) }
	if has(d.Title) || has(d.Description) || has(d.Location) {
		return true
	}
	if d.EventTypeID != nil {
		b, _ := json.Marshal(d.Fields)
		return has(string(b))
	}
	return false
}

// occurrenceDTO builds one occurrence's DTO from its master. counters holds the ids of types with a year counter.
func occurrenceDTO(master EventDTO, e *store.Event, o Occurrence, counters map[string]bool) EventDTO {
	d := master
	loc := eventLoc(e)
	allDay := e.AllDay
	if ov := o.Override; ov != nil {
		d.Overridden = true
		if ov.Title != nil {
			d.Title = *ov.Title
		}
		if ov.Description != nil {
			d.Description = *ov.Description
		}
		if ov.Location != nil {
			d.Location = *ov.Location
		}
		if ov.Status != nil {
			d.Status = *ov.Status
		}
		if ov.TypeOverridden {
			d.EventTypeID, d.Fields = ov.EventTypeID, parseFieldValues(ov.FieldsJSON)
		}
		if ov.AllDay != nil {
			allDay = *ov.AllDay
			d.AllDay = allDay
		}
	}
	d.Start, d.End = formatEventTime(o.Start, loc, allDay), formatEventTime(o.End, loc, allDay)
	d.StartUTC, d.EndUTC = o.Start, o.End
	d.Occurrence = o.RecurrenceID
	if d.EventTypeID != nil && counters[*d.EventTypeID] && e.RRule != "" {
		first, cur := e.StartAt, o.Start
		if !allDay {
			first, cur = first.In(loc), cur.In(loc)
		}
		if n := cur.Year() - first.Year(); n > 0 {
			d.YearsSince = &n
		}
	}
	return d
}

// GetEvent returns one master with overrides and visible attachments.
// Deleted events are visible to admins only (trash).
func (s *Service) GetEvent(ctx context.Context, actor authz.Actor, id string) (*EventDetail, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	db := s.DB.R.WithContext(ctx)
	e, err := s.loadEvent(db, id, authz.CanAdminister(actor))
	if err != nil {
		return nil, err
	}
	return s.eventDetail(db, actor, e)
}

func (s *Service) eventDetail(db *gorm.DB, actor authz.Actor, e *store.Event) (*EventDetail, error) {
	dto, err := s.eventDTO(db, actor, e)
	if err != nil {
		return nil, err
	}
	var ovs []store.EventOverride
	if err := db.Where("event_id = ?", e.ID).Order("recurrence_id").Find(&ovs).Error; err != nil {
		return nil, err
	}
	loc := eventLoc(e)
	ovDTOs := make([]OverrideDTO, len(ovs))
	for i, o := range ovs {
		allDay := e.AllDay
		if o.AllDay != nil {
			allDay = *o.AllDay
		}
		d := OverrideDTO{ID: o.ID, RecurrenceID: o.RecurrenceID.UTC(), Cancelled: o.Cancelled, Title: o.Title, Description: o.Description,
			Location: o.Location, AllDay: o.AllDay, Status: o.Status, StartUTC: o.StartAt, EndUTC: o.EndAt}
		if o.TypeOverridden {
			d.TypeOverridden, d.EventTypeID, d.Fields = true, o.EventTypeID, parseFieldValues(o.FieldsJSON)
		}
		if o.StartAt != nil {
			d.Start = ptr(formatEventTime(*o.StartAt, loc, allDay))
		}
		if o.EndAt != nil {
			d.End = ptr(formatEventTime(*o.EndAt, loc, allDay))
		}
		ovDTOs[i] = d
	}
	atts, err := s.listAttachments(db, actor, e, "comment_id IS NULL")
	if err != nil {
		return nil, err
	}
	return &EventDetail{EventDTO: *dto, Overrides: ovDTOs, Attachments: atts}, nil
}

// CreateEvent creates an event owned by the caller.
func (s *Service) CreateEvent(ctx context.Context, actor authz.Actor, in EventInput) (*EventDTO, error) {
	if !authz.CanCreateEvent(actor) {
		return nil, forbidden("only writers and admins can create events")
	}
	if in.OwnerID != nil && !authz.CanTransferOwnership(actor) {
		return nil, forbidden("only admins can set the owner")
	}
	var out *EventDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		base := &store.Event{Status: store.StatusConfirmed}
		if err := checkTypeAllowed(tx.db, actor, nil, in.EventTypeID); err != nil {
			return err
		}
		p, err := s.resolveInput(tx.db, base, in, true)
		if err != nil {
			return err
		}
		e := store.Event{ID: store.NewID(), OwnerID: actor.ID, Status: store.StatusConfirmed, Version: 1, ExdatesJSON: "[]", FieldsJSON: "{}", CreatedAt: tx.now, UpdatedAt: tx.now}
		p.apply(&e)
		if e.SeriesEndAt, err = seriesEnd(&e); err != nil {
			return invalidField("rrule", "%v", err)
		}
		if err := tx.db.Create(&e).Error; err != nil {
			return err
		}
		var tags []string
		if p.tags != nil {
			tags = *p.tags
			if err := setEventTags(tx.db, e.ID, tags); err != nil {
				return err
			}
		}
		if err := tx.record("event.created", "event", e.ID, &e.ID, nil, snapshot(&e, tags)); err != nil {
			return err
		}
		if out, err = s.eventDTO(tx.db, actor, &e); err != nil {
			return err
		}
		return tx.emit("event.created", e.ID, e.Version, out, "")
	})
	return out, err
}

// Scope selects which occurrences of a recurring event a change affects.
type Scope string

const (
	ScopeAll       Scope = "all"
	ScopeThis      Scope = "this"
	ScopeFollowing Scope = "following"
)

func parseScope(s string) (Scope, error) {
	switch Scope(s) {
	case "", ScopeAll:
		return ScopeAll, nil
	case ScopeThis, ScopeFollowing:
		return Scope(s), nil
	}
	return "", invalidField("scope", "scope must be this, following or all")
}

// checkEditable loads an event for editing and enforces authz and If-Match.
func (s *Service) checkEditable(tx *Tx, id string, ifMatch *int64) (*store.Event, error) {
	e, err := s.loadEvent(tx.db, id, false)
	if err != nil {
		return nil, err
	}
	switch authz.DecideEdit(tx.actor, authzEvent(e)) {
	case authz.Forbidden:
		return nil, forbidden("you cannot edit this event")
	case authz.SuggestOnly:
		return nil, &Error{Kind: KindForbidden, Msg: "only the owner or an admin can edit this event; send a suggestion instead", Suggest: true}
	}
	if ifMatch != nil && e.Version != *ifMatch {
		cur, err := s.eventDTO(tx.db, tx.actor, e)
		if err != nil {
			return nil, err
		}
		return nil, conflict("the event was changed by someone else", cur)
	}
	return e, nil
}

// UpdateEvent applies a change. ifMatch is required. For recurring events,
// scope selects this occurrence, this and following, or all; occurrence is
// the original start (RECURRENCE-ID) of the occurrence being edited.
func (s *Service) UpdateEvent(ctx context.Context, actor authz.Actor, id string, ifMatch *int64, scope string, occurrence *time.Time, in EventInput) (*EventDTO, error) {
	sc, err := parseScope(scope)
	if err != nil {
		return nil, err
	}
	if in.OwnerID != nil && !authz.CanTransferOwnership(actor) {
		return nil, forbidden("only admins can change the owner")
	}
	if in.empty() {
		return nil, invalid("nothing to change")
	}
	var out *EventDTO
	err = s.write(ctx, actor, func(tx *Tx) error {
		e, err := s.checkEditable(tx, id, ifMatch)
		if err != nil {
			return err
		}
		// Checked after authorization so non-owners learn they can suggest.
		if ifMatch == nil {
			return &Error{Kind: KindPrecondition, Msg: "If-Match with the event version is required"}
		}
		if err := checkTypeAllowed(tx.db, actor, e.EventTypeID, in.EventTypeID); err != nil {
			return err
		}
		out, err = s.applyUpdate(tx, e, sc, occurrence, in, "event.updated")
		return err
	})
	return out, err
}

// applyUpdate performs an authorized update inside tx. It is shared with
// suggestion acceptance.
func (s *Service) applyUpdate(tx *Tx, e *store.Event, sc Scope, occurrence *time.Time, in EventInput, action string) (*EventDTO, error) {
	if e.RRule == "" {
		sc, occurrence = ScopeAll, nil
	}
	if sc != ScopeAll && occurrence == nil {
		return nil, invalidField("occurrence", "occurrence is required for scope %s", sc)
	}
	if occurrence != nil {
		t := occurrence.UTC().Truncate(time.Second)
		occurrence = &t
		if !isEventOccurrence(e, t) {
			return nil, invalidField("occurrence", "not an occurrence of this event")
		}
	}
	if sc == ScopeFollowing && occurrence.Equal(e.StartAt) {
		sc = ScopeAll
	}
	switch sc {
	case ScopeThis:
		return s.updateOccurrence(tx, e, *occurrence, in)
	case ScopeFollowing:
		return s.splitAndUpdate(tx, e, *occurrence, in)
	}
	return s.updateSeries(tx, e, occurrence, in, action)
}

func (s *Service) updateSeries(tx *Tx, e *store.Event, occurrence *time.Time, in EventInput, action string) (*EventDTO, error) {
	tagMap, err := eventTags(tx.db, []string{e.ID})
	if err != nil {
		return nil, err
	}
	before := snapshot(e, tagMap[e.ID])

	var shift time.Duration
	if occurrence != nil && (in.Start != nil || in.End != nil) {
		// The client moved one occurrence and chose "all events": shift the
		// series by the same amount.
		occStart, occEnd := *occurrence, occurrence.Add(e.EndAt.Sub(e.StartAt))
		var ov store.EventOverride
		if err := tx.db.Where("event_id = ? AND recurrence_id = ?", e.ID, *occurrence).Take(&ov).Error; err == nil {
			if ov.StartAt != nil {
				occStart = *ov.StartAt
			}
			if ov.EndAt != nil {
				occEnd = *ov.EndAt
			}
		}
		probe := *e
		probe.StartAt, probe.EndAt = occStart, occEnd
		p, err := s.resolveInputWith(tx.db, &probe, EventInput{Start: in.Start, End: in.End, TimeZone: in.TimeZone, AllDay: in.AllDay, RRule: ptr("")}, false, false)
		if err != nil {
			return nil, err
		}
		shift = p.start.Sub(occStart)
		newStart := e.StartAt.Add(shift)
		if years := yearsMoved(tx.db, e, occStart, *p.start); years != 0 {
			// Counting years from the series start: a new year on an occurrence is the year the
			// series began, not a shift of the whole series, which would add to the old count.
			// Exceptions and overrides keep their place, so only the day within the year moves.
			loc := eventLoc(e)
			o, n := occStart.In(loc), p.start.In(loc)
			shift = n.AddDate(-years, 0, 0).Sub(o)
			newStart = time.Date(n.Year(), n.Month(), n.Day(), n.Hour(), n.Minute(), n.Second(), 0, loc).UTC()
		}
		newEnd := newStart.Add(p.end.Sub(*p.start))
		in.Start, in.End = ptr(newStart.Format(time.RFC3339)), ptr(newEnd.Format(time.RFC3339))
		if allDay := e.AllDay; (in.AllDay != nil && *in.AllDay) || (in.AllDay == nil && allDay) {
			in.Start, in.End = ptr(newStart.Format(dateLayout)), ptr(newEnd.Format(dateLayout))
		}
	} else if in.Start != nil && e.RRule != "" {
		if t, err := parseEventTime(*in.Start, eventLoc(e), e.AllDay && (in.AllDay == nil || *in.AllDay)); err == nil {
			shift = t.Sub(e.StartAt)
		}
	}
	if shift != 0 && e.RRule != "" && in.RRule == nil {
		// Moving the series to another day must move its day rules too,
		// or a weekly BYDAY=FR series dragged to Saturday stays on Fridays.
		loc := eventLoc(e)
		if days := dayDiff(e.StartAt.In(loc), e.StartAt.Add(shift).In(loc)); days != 0 {
			if rr, err := shiftRuleDays(e.RRule, days); err == nil {
				in.RRule = &rr
			}
		}
	}
	p, err := s.resolveInput(tx.db, e, in, false)
	if err != nil {
		return nil, err
	}
	p.apply(e)
	if shift != 0 && e.RRule != "" {
		// Keep exceptions attached to their (moved) occurrences.
		ex := exdates(e)
		for i := range ex {
			ex[i] = ex[i].Add(shift)
		}
		setExdates(e, ex)
		var ovs []store.EventOverride
		if err := tx.db.Where("event_id = ?", e.ID).Find(&ovs).Error; err != nil {
			return nil, err
		}
		for _, o := range ovs {
			if err := tx.db.Model(&store.EventOverride{}).Where("id = ?", o.ID).Update("recurrence_id", o.RecurrenceID.Add(shift)).Error; err != nil {
				return nil, err
			}
		}
	}
	if e.SeriesEndAt, err = seriesEnd(e); err != nil {
		return nil, invalidField("rrule", "%v", err)
	}
	if e.RRule == "" {
		// No longer recurring: exceptions are meaningless.
		setExdates(e, nil)
		if err := tx.db.Where("event_id = ?", e.ID).Delete(&store.EventOverride{}).Error; err != nil {
			return nil, err
		}
	}
	e.Version++
	e.UpdatedAt = tx.now
	if err := tx.db.Save(e).Error; err != nil {
		return nil, err
	}
	tags := before.Tags
	if p.tags != nil {
		tags = *p.tags
		if err := setEventTags(tx.db, e.ID, tags); err != nil {
			return nil, err
		}
	}
	if err := tx.record(action, "event", e.ID, &e.ID, before, snapshot(e, tags)); err != nil {
		return nil, err
	}
	dto, err := s.eventDTO(tx.db, tx.actor, e)
	if err != nil {
		return nil, err
	}
	return dto, tx.emit("event.updated", e.ID, e.Version, dto, "")
}

func (s *Service) updateOccurrence(tx *Tx, e *store.Event, rid time.Time, in EventInput) (*EventDTO, error) {
	if in.TimeZone != nil || in.RRule != nil || in.Tags != nil || in.OwnerID != nil || in.AllDay != nil {
		return nil, invalid("a single occurrence can change only title, description (generic events), location, status, start, end, type and fields")
	}
	var ov store.EventOverride
	err := tx.db.Where("event_id = ? AND recurrence_id = ?", e.ID, rid).Take(&ov).Error
	isNew := isNotFound(err)
	if err != nil && !isNew {
		return nil, err
	}
	before := ov
	probe := *e
	probe.RRule = ""
	probe.StartAt, probe.EndAt = rid, rid.Add(e.EndAt.Sub(e.StartAt))
	if ov.StartAt != nil {
		probe.StartAt = *ov.StartAt
	}
	if ov.EndAt != nil {
		probe.EndAt = *ov.EndAt
	}
	if ov.TypeOverridden {
		// Validate against what this occurrence is now, not against the master.
		probe.EventTypeID, probe.FieldsJSON = ov.EventTypeID, ov.FieldsJSON
	}
	p, err := s.resolveInput(tx.db, &probe, in, false)
	if err != nil {
		return nil, err
	}
	if isNew {
		ov = store.EventOverride{ID: store.NewID(), EventID: e.ID, RecurrenceID: rid, FieldsJSON: "{}", CreatedAt: tx.now}
	}
	if p.description != nil {
		ov.Description = p.description
	}
	if p.location != nil {
		ov.Location = p.location
	}
	if p.status != nil {
		ov.Status = p.status
	}
	if p.start != nil {
		ov.StartAt, ov.EndAt = p.start, p.end
	}
	if p.typeSet || p.fields != nil {
		// The occurrence now carries its own type and values: the new ones, or what it had.
		if !ov.TypeOverridden {
			ov.EventTypeID, ov.FieldsJSON = e.EventTypeID, e.FieldsJSON
		}
		if p.typeSet {
			ov.EventTypeID = p.eventTypeID
			if p.eventTypeID == nil && p.description == nil {
				ov.Description = nil // generic again: inherit the master's description
			}
		}
		if p.fields != nil {
			ov.FieldsJSON = *p.fields
		}
		ov.TypeOverridden = true
	}
	// A generated title only needs storing when the occurrence has its own values;
	// otherwise it equals the series' title and keeps following it.
	if p.title != nil && (!p.titleGenerated || ov.TypeOverridden) {
		ov.Title = p.title
	}
	ov.UpdatedAt = tx.now
	if err := tx.db.Save(&ov).Error; err != nil {
		return nil, err
	}
	e.Version++
	e.UpdatedAt = tx.now
	if err := tx.db.Model(&store.Event{}).Where("id = ?", e.ID).Updates(map[string]any{"version": e.Version, "updated_at": e.UpdatedAt}).Error; err != nil {
		return nil, err
	}
	var beforeAny any
	if !isNew {
		beforeAny = before
	}
	if err := tx.record("event.occurrence_updated", "event", e.ID, &e.ID, beforeAny, ov); err != nil {
		return nil, err
	}
	dto, err := s.eventDTO(tx.db, tx.actor, e)
	if err != nil {
		return nil, err
	}
	return dto, tx.emit("event.updated", e.ID, e.Version, dto, "")
}

// splitAndUpdate ends the series before rid and starts a new series at rid
// with the change applied (RFC 5545 "this and following").
func (s *Service) splitAndUpdate(tx *Tx, e *store.Event, rid time.Time, in EventInput) (*EventDTO, error) {
	head, tail, err := splitRule(e, rid)
	if err != nil {
		return nil, invalidField("occurrence", "%v", err)
	}
	tagMap, err := eventTags(tx.db, []string{e.ID})
	if err != nil {
		return nil, err
	}
	tags := tagMap[e.ID]
	beforeMaster := snapshot(e, tags)

	n := *e
	n.ID = store.NewID()
	n.StartAt, n.EndAt = rid, rid.Add(e.EndAt.Sub(e.StartAt))
	n.RRule = tail
	n.Version = 1
	n.CreatedAt, n.UpdatedAt = tx.now, tx.now
	var keep, move []time.Time
	for _, t := range exdates(e) {
		if t.Before(rid) {
			keep = append(keep, t)
		} else {
			move = append(move, t)
		}
	}
	setExdates(&n, move)
	if in.Start != nil && in.RRule == nil {
		// Moving the first occurrence of the new series moves the series.
		if t, err := parseEventTime(*in.Start, eventLoc(e), e.AllDay && (in.AllDay == nil || *in.AllDay)); err == nil {
			shift := t.Sub(rid)
			for i := range move {
				move[i] = move[i].Add(shift)
			}
			setExdates(&n, move)
			loc := eventLoc(e)
			if days := dayDiff(rid.In(loc), t.In(loc)); days != 0 {
				if rr, err := shiftRuleDays(tail, days); err == nil {
					in.RRule = &rr
				}
			}
		}
	}
	p, err := s.resolveInput(tx.db, &n, in, false)
	if err != nil {
		return nil, err
	}
	p.apply(&n)
	if n.SeriesEndAt, err = seriesEnd(&n); err != nil {
		return nil, invalidField("rrule", "%v", err)
	}
	if err := tx.db.Create(&n).Error; err != nil {
		return nil, err
	}
	newTags := tags
	if p.tags != nil {
		newTags = *p.tags
	}
	if err := setEventTags(tx.db, n.ID, newTags); err != nil {
		return nil, err
	}
	// Overrides from the split point on belong to the new series.
	shift := n.StartAt.Sub(rid)
	var ovs []store.EventOverride
	if err := tx.db.Where("event_id = ? AND recurrence_id >= ?", e.ID, rid).Find(&ovs).Error; err != nil {
		return nil, err
	}
	for _, o := range ovs {
		if err := tx.db.Model(&store.EventOverride{}).Where("id = ?", o.ID).
			Updates(map[string]any{"event_id": n.ID, "recurrence_id": o.RecurrenceID.Add(shift)}).Error; err != nil {
			return nil, err
		}
	}

	e.RRule = head
	setExdates(e, keep)
	if e.SeriesEndAt, err = seriesEnd(e); err != nil {
		return nil, err
	}
	e.Version++
	e.UpdatedAt = tx.now
	if err := tx.db.Save(e).Error; err != nil {
		return nil, err
	}
	if err := tx.record("event.updated", "event", e.ID, &e.ID, beforeMaster, snapshot(e, tags)); err != nil {
		return nil, err
	}
	if err := tx.record("event.created", "event", n.ID, &n.ID, nil, map[string]any{"splitFrom": e.ID, "event": snapshot(&n, newTags)}); err != nil {
		return nil, err
	}
	masterDTO, err := s.eventDTO(tx.db, tx.actor, e)
	if err != nil {
		return nil, err
	}
	if err := tx.emit("event.updated", e.ID, e.Version, masterDTO, ""); err != nil {
		return nil, err
	}
	dto, err := s.eventDTO(tx.db, tx.actor, &n)
	if err != nil {
		return nil, err
	}
	return dto, tx.emit("event.created", n.ID, n.Version, dto, "")
}

// DeleteEvent soft-deletes an event, or removes occurrences of a recurring
// one. ifMatch is optional for deletes.
func (s *Service) DeleteEvent(ctx context.Context, actor authz.Actor, id string, ifMatch *int64, scope string, occurrence *time.Time) error {
	sc, err := parseScope(scope)
	if err != nil {
		return err
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		e, err := s.checkEditable(tx, id, ifMatch)
		if err != nil {
			return err
		}
		return s.applyDelete(tx, e, sc, occurrence, "event.deleted")
	})
}

func (s *Service) applyDelete(tx *Tx, e *store.Event, sc Scope, occurrence *time.Time, action string) error {
	if e.RRule == "" {
		sc = ScopeAll
	}
	if sc != ScopeAll {
		if occurrence == nil {
			return invalidField("occurrence", "occurrence is required for scope %s", sc)
		}
		rid := occurrence.UTC().Truncate(time.Second)
		if !isEventOccurrence(e, rid) {
			return invalidField("occurrence", "not an occurrence of this event")
		}
		if sc == ScopeFollowing && rid.Equal(e.StartAt) {
			sc = ScopeAll
		} else {
			return s.deleteOccurrences(tx, e, sc, rid)
		}
	}
	tagMap, err := eventTags(tx.db, []string{e.ID})
	if err != nil {
		return err
	}
	before := snapshot(e, tagMap[e.ID])
	now := tx.now
	e.DeletedAt = &now
	actorID := tx.actor.ID
	e.DeletedByID = &actorID
	e.Version++
	e.UpdatedAt = now
	if err := tx.db.Save(e).Error; err != nil {
		return err
	}
	if err := tx.record(action, "event", e.ID, &e.ID, before, nil); err != nil {
		return err
	}
	if err := s.closeSuggestions(tx, e.ID); err != nil {
		return err
	}
	return tx.emit("event.deleted", e.ID, e.Version, map[string]string{"id": e.ID}, "")
}

func (s *Service) deleteOccurrences(tx *Tx, e *store.Event, sc Scope, rid time.Time) error {
	before := map[string]any{"rrule": e.RRule, "exdates": exdates(e)}
	if sc == ScopeThis {
		setExdates(e, append(exdates(e), rid))
		if err := tx.db.Where("event_id = ? AND recurrence_id = ?", e.ID, rid).Delete(&store.EventOverride{}).Error; err != nil {
			return err
		}
	} else {
		head, _, err := splitRule(e, rid)
		if err != nil {
			return invalidField("occurrence", "%v", err)
		}
		e.RRule = head
		var keep []time.Time
		for _, t := range exdates(e) {
			if t.Before(rid) {
				keep = append(keep, t)
			}
		}
		setExdates(e, keep)
		if err := tx.db.Where("event_id = ? AND recurrence_id >= ?", e.ID, rid).Delete(&store.EventOverride{}).Error; err != nil {
			return err
		}
		if e.SeriesEndAt, err = seriesEnd(e); err != nil {
			return err
		}
	}
	e.Version++
	e.UpdatedAt = tx.now
	if err := tx.db.Save(e).Error; err != nil {
		return err
	}
	after := map[string]any{"rrule": e.RRule, "exdates": exdates(e), "scope": sc, "occurrence": rid}
	if err := tx.record("event.occurrence_deleted", "event", e.ID, &e.ID, before, after); err != nil {
		return err
	}
	dto, err := s.eventDTO(tx.db, tx.actor, e)
	if err != nil {
		return err
	}
	return tx.emit("event.updated", e.ID, e.Version, dto, "")
}

// RestoreEvent brings an event back from the trash: admins within the
// retention period, the owner within the undo window.
func (s *Service) RestoreEvent(ctx context.Context, actor authz.Actor, id string) (*EventDTO, error) {
	var out *EventDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		e, err := s.loadEvent(tx.db, id, true)
		if err != nil {
			return err
		}
		if e.DeletedAt == nil {
			return invalid("event is not deleted")
		}
		within := tx.now.Sub(*e.DeletedAt) <= undoWindow && e.DeletedByID != nil && *e.DeletedByID == actor.ID
		if !authz.CanRestoreEvent(actor, authzEvent(e), within) {
			return forbidden("you cannot restore this event")
		}
		e.DeletedAt, e.DeletedByID = nil, nil
		e.Version++
		e.UpdatedAt = tx.now
		if err := tx.db.Save(e).Error; err != nil {
			return err
		}
		if err := tx.record("event.restored", "event", e.ID, &e.ID, nil, nil); err != nil {
			return err
		}
		if out, err = s.eventDTO(tx.db, actor, e); err != nil {
			return err
		}
		return tx.emit("event.created", e.ID, e.Version, out, "")
	})
	return out, err
}

// Trash lists soft-deleted events still within the retention window.
func (s *Service) Trash(ctx context.Context, actor authz.Actor) ([]EventDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	db := s.DB.R.WithContext(ctx)
	var evs []store.Event
	if err := db.Where("deleted_at IS NOT NULL AND deleted_at > ?", s.now().Add(-trashRetention)).Order("deleted_at DESC").Limit(1000).Find(&evs).Error; err != nil {
		return nil, err
	}
	return s.eventDTOs(db, actor, evs)
}

// EventExists reports whether a live event exists (used by presence).
func (s *Service) EventExists(ctx context.Context, id string) bool {
	if id == "" || len(id) > 36 {
		return false
	}
	_, err := s.loadEvent(s.DB.R.WithContext(ctx), id, false)
	return err == nil
}

// yearsMoved is how many calendar years an edit moves a yearly occurrence of
// a year-counting type, or 0 when the counter does not apply.
func yearsMoved(db *gorm.DB, e *store.Event, from, to time.Time) int {
	if e.EventTypeID == nil || !strings.Contains(e.RRule, "FREQ=YEARLY") {
		return 0
	}
	var counts int64
	if err := db.Model(&store.EventType{}).Where("id = ? AND year_counter = ?", *e.EventTypeID, true).Count(&counts).Error; err != nil || counts == 0 {
		return 0
	}
	loc := eventLoc(e)
	return to.In(loc).Year() - from.In(loc).Year()
}
