package domain

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

const (
	maxEventTypes    = 50
	maxTypeFields    = 20
	maxSelectOptions = 20
	maxInstructions  = 1000
	maxTextValue     = 200
	maxLongTextValue = 5000
	maxURLValue      = 2000
	maxMembers       = 50
)

// Field kinds an event type can declare.
const (
	FieldText     = "text"
	FieldLongText = "longtext"
	FieldNumber   = "number"
	FieldDate     = "date"
	FieldTime     = "time"
	FieldSelect   = "select"
	FieldURL      = "url"
	FieldCheckbox = "checkbox"
	FieldMembers  = "members"
)

// typeIcons are the icons an admin can pick; the web app maps each name to a
// glyph. Keep in sync with web/src/lib/typeIcons.ts.
var typeIcons = []string{
	"calendar", "cake", "gift", "party-popper", "flag", "plane", "heart", "star", "briefcase",
	"graduation-cap", "utensils", "music", "trophy", "stethoscope", "home", "users", "bell",
}

// Styles an event type can use on the calendar.
var typeStyles = []string{"default", "banner", "outline", "badge"}

var fieldKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// FieldDef declares one predefined field of an event type.
type FieldDef struct {
	Key      string   `json:"key" maxLength:"32" pattern:"^[a-z][a-z0-9_]{0,31}$" doc:"Stable identifier used in event values"`
	Label    string   `json:"label" maxLength:"64"`
	Kind     string   `json:"kind" enum:"text,longtext,number,date,time,select,url,checkbox,members" doc:"members: a list of user ids"`
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty" maxItems:"20" doc:"Choices for select fields"`
	Multiple *bool    `json:"multiple,omitempty" doc:"select and members fields: allow several selections. Unset means one choice for select and several people for members"`
}

// allowsMany reports whether the field takes several values. Members fields
// predate the flag and default to several; select fields default to one.
func (d FieldDef) allowsMany() bool {
	if d.Multiple != nil {
		return *d.Multiple
	}
	return d.Kind == FieldMembers
}

// EventTypeDTO is an event type as sent to clients. ColorSlot is a theme slot,
// never a hex value, so type colors stay readable in every theme.
type EventTypeDTO struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	Icon                   string     `json:"icon" enum:"calendar,cake,gift,party-popper,flag,plane,heart,star,briefcase,graduation-cap,utensils,music,trophy,stethoscope,home,users,bell"`
	ColorSlot              string     `json:"colorSlot" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
	Style                  string     `json:"style" enum:"default,banner,outline,badge"`
	YearCounter            bool       `json:"yearCounter"`
	DefaultAllDay          bool       `json:"defaultAllDay"`
	DefaultDurationMinutes int        `json:"defaultDurationMinutes"`
	DefaultRRule           string     `json:"defaultRrule" doc:"When set, every event of the type repeats with exactly this rule"`
	SingleDay              bool       `json:"singleDay" doc:"With defaultAllDay: events last a single date"`
	NeverRepeats           bool       `json:"neverRepeats" doc:"Events of the type never repeat; excludes defaultRrule"`
	AdminOnly              bool       `json:"adminOnly" doc:"Only admins can create events of this type or switch events to it"`
	TitleTemplate          string     `json:"titleTemplate" doc:"When set, event titles are generated from field values and cannot be set by hand"`
	Instructions           string     `json:"instructions" doc:"Guidance shown while creating an event of this type"`
	Fields                 []FieldDef `json:"fields"`
	SortOrder              int        `json:"sortOrder"`
}

// EventTypeInput creates or changes an event type. Nil fields are left
// unchanged (creation requires name and colorSlot).
type EventTypeInput struct {
	Name                   *string     `json:"name,omitempty" maxLength:"64"`
	Icon                   *string     `json:"icon,omitempty" enum:"calendar,cake,gift,party-popper,flag,plane,heart,star,briefcase,graduation-cap,utensils,music,trophy,stethoscope,home,users,bell"`
	ColorSlot              *string     `json:"colorSlot,omitempty" enum:"tag-1,tag-2,tag-3,tag-4,tag-5,tag-6,tag-7,tag-8"`
	Style                  *string     `json:"style,omitempty" enum:"default,banner,outline,badge"`
	YearCounter            *bool       `json:"yearCounter,omitempty" doc:"Show years since the first occurrence of a recurring event"`
	DefaultAllDay          *bool       `json:"defaultAllDay,omitempty"`
	DefaultDurationMinutes *int        `json:"defaultDurationMinutes,omitempty" minimum:"0" maximum:"1440" doc:"Length of new timed events; 0 for instants such as deadlines"`
	DefaultRRule           *string     `json:"defaultRrule,omitempty" maxLength:"500" doc:"RRULE every event of this type must use"`
	SingleDay              *bool       `json:"singleDay,omitempty" doc:"With defaultAllDay: events last a single date rather than a range"`
	NeverRepeats           *bool       `json:"neverRepeats,omitempty" doc:"Events of this type can't repeat; cannot be combined with defaultRrule"`
	AdminOnly              *bool       `json:"adminOnly,omitempty" doc:"Only admins can create events of this type or switch events to it"`
	TitleTemplate          *string     `json:"titleTemplate,omitempty" maxLength:"200" doc:"Generates event titles from {{field_key}} placeholders; empty turns it off"`
	Instructions           *string     `json:"instructions,omitempty" maxLength:"1000" doc:"Guidance shown while creating an event of this type"`
	Fields                 *[]FieldDef `json:"fields,omitempty" maxItems:"20"`
}

func validStyle(s string) bool { return slices.Contains(typeStyles, s) }

func checkDuration(m int) error {
	if m < 0 || m > 1440 {
		return invalidField("defaultDurationMinutes", "duration must be 0–1440 minutes")
	}
	return nil
}

// applyLook copies the appearance and creation defaults from in onto t.
func applyLook(t *store.EventType, in EventTypeInput) error {
	if in.Style != nil {
		if !validStyle(*in.Style) {
			return invalidField("style", "style must be one of %s", strings.Join(typeStyles, ", "))
		}
		t.Style = *in.Style
	}
	if in.YearCounter != nil {
		t.YearCounter = *in.YearCounter
	}
	if in.NeverRepeats != nil {
		t.NeverRepeats = *in.NeverRepeats
	}
	if in.SingleDay != nil {
		t.SingleDay = *in.SingleDay
	}
	if in.AdminOnly != nil {
		t.AdminOnly = *in.AdminOnly
	}
	if in.DefaultAllDay != nil {
		t.DefaultAllDay = *in.DefaultAllDay
	}
	if in.Instructions != nil {
		ins := strings.TrimSpace(*in.Instructions)
		if utf8.RuneCountInString(ins) > maxInstructions {
			return invalidField("instructions", "instructions are limited to %d characters", maxInstructions)
		}
		t.Instructions = ins
	}
	if in.DefaultDurationMinutes != nil {
		if err := checkDuration(*in.DefaultDurationMinutes); err != nil {
			return err
		}
		t.DefaultDurationMinutes = *in.DefaultDurationMinutes
	}
	return nil
}

// checkTypeAllowed rejects actors who aren't admins when in switches an event
// from the type current (nil = generic) to an admin-only type.
func checkTypeAllowed(db *gorm.DB, actor authz.Actor, current, requested *string) error {
	if requested == nil || *requested == "" || sameStrPtr(current, requested) || authz.CanUseAdminOnlyTypes(actor) {
		return nil
	}
	var n int64
	if err := db.Model(&store.EventType{}).Where("id = ? AND admin_only = ?", *requested, true).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return forbidden("only admins can use this event type")
	}
	return nil
}

func validIcon(s string) bool {
	for _, i := range typeIcons {
		if i == s {
			return true
		}
	}
	return false
}

func parseFieldDefs(raw string) []FieldDef {
	var defs []FieldDef
	_ = json.Unmarshal([]byte(raw), &defs)
	if defs == nil {
		defs = []FieldDef{}
	}
	return defs
}

func eventTypeDTO(t store.EventType) EventTypeDTO {
	return EventTypeDTO{
		ID: t.ID, Name: t.Name, Icon: t.Icon, ColorSlot: t.ColorSlot, Style: t.Style, YearCounter: t.YearCounter, TitleTemplate: t.TitleTemplate, Instructions: t.Instructions, DefaultAllDay: t.DefaultAllDay, DefaultDurationMinutes: t.DefaultDurationMinutes,
		DefaultRRule: t.DefaultRRule, NeverRepeats: t.NeverRepeats, AdminOnly: t.AdminOnly, SingleDay: t.SingleDay, Fields: parseFieldDefs(t.FieldsJSON), SortOrder: t.SortOrder,
	}
}

func listEventTypes(db *gorm.DB) ([]EventTypeDTO, error) {
	var types []store.EventType
	if err := db.Order("sort_order, name, id").Find(&types).Error; err != nil {
		return nil, err
	}
	out := make([]EventTypeDTO, len(types))
	for i, t := range types {
		out[i] = eventTypeDTO(t)
	}
	return out, nil
}

// validateFieldDefs checks a type's field list and returns it cleaned up.
func validateFieldDefs(defs []FieldDef) ([]FieldDef, fieldErrors) {
	var fe fieldErrors
	if len(defs) > maxTypeFields {
		fe.add("fields", "at most %d fields", maxTypeFields)
		return nil, fe
	}
	seen := map[string]bool{}
	out := make([]FieldDef, len(defs))
	for i, d := range defs {
		at := "fields." + strconv.Itoa(i)
		d.Label = strings.TrimSpace(d.Label)
		if !fieldKeyRe.MatchString(d.Key) {
			fe.add(at+".key", "key must start with a lowercase letter and use only a–z, 0–9 and _ (up to 32 characters)")
		} else if seen[d.Key] {
			fe.add(at+".key", "duplicate field key %q", d.Key)
		}
		seen[d.Key] = true
		if d.Label == "" || utf8.RuneCountInString(d.Label) > 64 {
			fe.add(at+".label", "label must be 1–64 characters")
		}
		switch d.Kind {
		case FieldText, FieldLongText, FieldNumber, FieldDate, FieldTime, FieldURL, FieldMembers:
		case FieldCheckbox:
			if d.Required {
				fe.add(at+".required", "a checkbox cannot be required")
			}
		case FieldSelect:
			opts := make([]string, 0, len(d.Options))
			dup := map[string]bool{}
			for _, o := range d.Options {
				o = strings.TrimSpace(o)
				if o == "" || utf8.RuneCountInString(o) > 64 {
					fe.add(at+".options", "each option must be 1–64 characters")
					break
				}
				if !dup[o] {
					dup[o] = true
					opts = append(opts, o)
				}
			}
			if len(opts) == 0 || len(opts) > maxSelectOptions {
				fe.add(at+".options", "a select field needs 1–%d options", maxSelectOptions)
			}
			d.Options = opts
		default:
			fe.add(at+".kind", "unknown field kind")
		}
		if d.Kind != FieldSelect && d.Kind != FieldMembers {
			d.Multiple = nil
		}
		if d.Kind != FieldSelect {
			d.Options = nil
		}
		out[i] = d
	}
	return out, fe
}

// normalizeFields validates event field values against defs. Empty values
// are dropped, so the result only holds fields that carry information.
func normalizeFields(db *gorm.DB, defs []FieldDef, in map[string]any) (map[string]any, fieldErrors, error) {
	var fe fieldErrors
	byKey := make(map[string]FieldDef, len(defs))
	for _, d := range defs {
		byKey[d.Key] = d
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := byKey[k]; !ok {
			fe.add("fields."+k, "unknown field %q for this event type", k)
		}
	}
	out := map[string]any{}
	for _, d := range defs {
		at := "fields." + d.Key
		v, present := in[d.Key]
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			present = false
		}
		if v == nil {
			present = false
		}
		if !present {
			if d.Required {
				fe.add(at, "%s is required", d.Label)
			}
			continue
		}
		nv, msg := normalizeValue(d, v)
		if msg != "" {
			fe.add(at, "%s %s", d.Label, msg)
			continue
		}
		if nv != nil {
			out[d.Key] = nv
		}
	}
	// Members must be existing users (inactive ones stay valid, so old events remain editable).
	for _, d := range defs {
		ids, _ := out[d.Key].([]string)
		if d.Kind != FieldMembers || len(ids) == 0 {
			continue
		}
		var n int64
		if err := db.Model(&store.User{}).Where("id IN ?", ids).Count(&n).Error; err != nil {
			return nil, nil, err
		}
		if int(n) != len(ids) {
			fe.add("fields."+d.Key, "%s includes an unknown person", d.Label)
			delete(out, d.Key)
		}
	}
	return out, fe, nil
}

// normalizeValue returns the canonical value, or an error message. A nil
// value with no message means "nothing to store".
func normalizeValue(d FieldDef, v any) (any, string) {
	switch d.Kind {
	case FieldText, FieldLongText:
		s, ok := v.(string)
		if !ok {
			return nil, "must be text"
		}
		s = strings.TrimSpace(s)
		max := maxTextValue
		if d.Kind == FieldLongText {
			max = maxLongTextValue
		}
		if utf8.RuneCountInString(s) > max {
			return nil, "is limited to " + strconv.Itoa(max) + " characters"
		}
		return s, ""
	case FieldNumber:
		f, ok := v.(float64)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > 1e15 {
			return nil, "must be a number"
		}
		return f, ""
	case FieldDate:
		s, _ := v.(string)
		if _, err := time.Parse(dateLayout, strings.TrimSpace(s)); err != nil {
			return nil, "must be a date like 2026-10-30"
		}
		return strings.TrimSpace(s), ""
	case FieldTime:
		s, _ := v.(string)
		s = strings.TrimSpace(s)
		if _, err := time.Parse("15:04", s); err != nil || len(s) != 5 {
			return nil, "must be a time like 19:30"
		}
		return s, ""
	case FieldSelect:
		return normalizeSelect(d, v)
	case FieldURL:
		s, _ := v.(string)
		s = strings.TrimSpace(s)
		u, err := url.Parse(s)
		if err != nil || len(s) > maxURLValue || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, "must be an http or https link"
		}
		return s, ""
	case FieldMembers:
		list, ok := v.([]any)
		if !ok {
			if ss, isStrs := v.([]string); isStrs {
				for _, s := range ss {
					list = append(list, s)
				}
			} else {
				return nil, "must be a list of people"
			}
		}
		ids := make([]string, 0, len(list))
		for _, x := range list {
			s, ok := x.(string)
			if !ok || s == "" || len(s) > 36 {
				return nil, "must be a list of people"
			}
			ids = append(ids, s)
		}
		ids = uniq(ids)
		if len(ids) > maxMembers {
			return nil, "is limited to " + strconv.Itoa(maxMembers) + " people"
		}
		if len(ids) == 0 {
			return nil, ""
		}
		if len(ids) > 1 && !d.allowsMany() {
			return nil, "allows only one person"
		}
		return ids, ""
	case FieldCheckbox:
		b, ok := v.(bool)
		if !ok {
			return nil, "must be true or false"
		}
		if !b {
			return nil, ""
		}
		return true, ""
	}
	return nil, "has an unknown kind"
}

func parseFieldValues(raw string) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func encodeFieldValues(v map[string]any) string {
	if len(v) == 0 {
		return "{}"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// fieldSummary renders a typed event's values as "Label: value" lines, for
// the ICS feed and plain-text contexts. Stale keys (no longer declared) are
// skipped.
// normalizeSelect accepts a string or a list of strings either way, so values
// saved before a field was switched between single and multiple stay editable.
// Multiple fields store a list in option order; single fields store the first choice.
func normalizeSelect(d FieldDef, v any) (any, string) {
	var picked []string
	switch x := v.(type) {
	case string:
		picked = []string{x}
	case []string:
		picked = x
	case []any:
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, "must be one of the listed options"
			}
			picked = append(picked, s)
		}
	default:
		return nil, "must be one of the listed options"
	}
	for _, s := range picked {
		if !slices.Contains(d.Options, s) {
			return nil, "must be one of the listed options"
		}
	}
	if len(picked) == 0 {
		return nil, ""
	}
	if !d.allowsMany() {
		if len(picked) > 1 {
			return nil, "allows only one choice"
		}
		return picked[0], ""
	}
	out := make([]string, 0, len(picked))
	for _, o := range d.Options {
		if slices.Contains(picked, o) {
			out = append(out, o)
		}
	}
	return out, ""
}

func fieldSummary(defs []FieldDef, values map[string]any, names map[string]string) string {
	var lines []string
	for _, d := range defs {
		v, ok := values[d.Key]
		if !ok {
			continue
		}
		var text string
		switch x := v.(type) {
		case string:
			text = x
		case float64:
			text = strconv.FormatFloat(x, 'f', -1, 64)
		case bool:
			if x {
				text = "Yes"
			}
		case []any:
			var who []string
			for _, id := range x {
				s, _ := id.(string)
				if s == "" {
					continue
				}
				if d.Kind == FieldSelect {
					who = append(who, s)
				} else {
					who = append(who, names[s])
				}
			}
			text = strings.Join(slices.DeleteFunc(who, func(s string) bool { return s == "" }), ", ")
		}
		if text != "" {
			lines = append(lines, d.Label+": "+text)
		}
	}
	return strings.Join(lines, "\n")
}

// ListEventTypes returns every event type in display order.
func (s *Service) ListEventTypes(ctx context.Context, actor authz.Actor) ([]EventTypeDTO, error) {
	if !authz.CanView(actor) {
		return nil, forbidden("not allowed")
	}
	return listEventTypes(s.DB.R.WithContext(ctx))
}

func (s *Service) checkTypeName(db *gorm.DB, name, exceptID string) (string, error) {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > 64 {
		return "", invalidField("name", "type name must be 1–64 characters")
	}
	var count int64
	q := db.Model(&store.EventType{}).Where("LOWER(name) = ?", strings.ToLower(n))
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	if err := q.Count(&count).Error; err != nil {
		return "", err
	}
	if count > 0 {
		return "", invalidField("name", "an event type with this name already exists")
	}
	return n, nil
}

func checkDefaultRRule(rr string) (string, error) {
	norm, err := normalizeRRule(rr, time.Now().UTC(), time.UTC)
	if err != nil {
		return "", invalidField("defaultRrule", "%v", err)
	}
	return norm, nil
}

func (tx *Tx) emitEventTypes() error {
	types, err := listEventTypes(tx.db)
	if err != nil {
		return err
	}
	return tx.emit("eventtype.changed", "eventtypes", 0, map[string]any{"eventTypes": types}, "")
}

// CreateEventType adds an event type at the end of the list.
func (s *Service) CreateEventType(ctx context.Context, actor authz.Actor, in EventTypeInput) (*EventTypeDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	if in.Name == nil || in.ColorSlot == nil || !validSlot(*in.ColorSlot) {
		return nil, invalid("name and a colorSlot (tag-1 … tag-8) are required")
	}
	icon := "calendar"
	if in.Icon != nil {
		icon = *in.Icon
	}
	if !validIcon(icon) {
		return nil, invalidField("icon", "unknown icon")
	}
	var out EventTypeDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		name, err := s.checkTypeName(tx.db, *in.Name, "")
		if err != nil {
			return err
		}
		t := store.EventType{ID: store.NewID(), Name: name, Icon: icon, ColorSlot: *in.ColorSlot, Style: "default", DefaultDurationMinutes: 60, CreatedAt: tx.now, UpdatedAt: tx.now}
		if err := applyLook(&t, in); err != nil {
			return err
		}
		if in.DefaultRRule != nil {
			if t.DefaultRRule, err = checkDefaultRRule(*in.DefaultRRule); err != nil {
				return err
			}
		}
		if t.NeverRepeats && t.DefaultRRule != "" {
			return invalidField("neverRepeats", "a type that never repeats can't also have a fixed repeat")
		}
		// A single date only means something for events that are always all-day.
		t.SingleDay = t.SingleDay && t.DefaultAllDay
		defs := []FieldDef{}
		if in.Fields != nil {
			var fe fieldErrors
			if defs, fe = validateFieldDefs(*in.Fields); len(fe) > 0 {
				return fe.err()
			}
		}
		b, _ := json.Marshal(defs)
		t.FieldsJSON = string(b)
		if in.TitleTemplate != nil {
			if t.TitleTemplate, err = checkTitleTemplate(*in.TitleTemplate, defs); err != nil {
				return err
			}
		}
		var n int64
		if err := tx.db.Model(&store.EventType{}).Count(&n).Error; err != nil {
			return err
		}
		if n >= maxEventTypes {
			return limit("too many event types")
		}
		var maxOrder struct{ M *int }
		if err := tx.db.Model(&store.EventType{}).Select("MAX(sort_order) AS m").Scan(&maxOrder).Error; err != nil {
			return err
		}
		if maxOrder.M != nil {
			t.SortOrder = *maxOrder.M + 1
		}
		if err := tx.db.Create(&t).Error; err != nil {
			return err
		}
		out = eventTypeDTO(t)
		if err := tx.record("eventtype.created", "event_type", t.ID, nil, nil, out); err != nil {
			return err
		}
		return tx.emitEventTypes()
	})
	return &out, err
}

// UpdateEventType changes a type's name, look, defaults or fields. Existing
// events keep their stored values; values for removed fields are hidden and
// dropped on the event's next edit.
func (s *Service) UpdateEventType(ctx context.Context, actor authz.Actor, id string, in EventTypeInput) (*EventTypeDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var out EventTypeDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		t, err := take[store.EventType](tx.db, id, "event type")
		if err != nil {
			return err
		}
		before := eventTypeDTO(t)
		if in.Name != nil {
			if t.Name, err = s.checkTypeName(tx.db, *in.Name, t.ID); err != nil {
				return err
			}
		}
		if in.Icon != nil {
			if !validIcon(*in.Icon) {
				return invalidField("icon", "unknown icon")
			}
			t.Icon = *in.Icon
		}
		if in.ColorSlot != nil {
			if !validSlot(*in.ColorSlot) {
				return invalidField("colorSlot", "colorSlot must be tag-1 … tag-8")
			}
			t.ColorSlot = *in.ColorSlot
		}
		if err := applyLook(&t, in); err != nil {
			return err
		}
		if in.DefaultRRule != nil {
			if t.DefaultRRule, err = checkDefaultRRule(*in.DefaultRRule); err != nil {
				return err
			}
		}
		if t.NeverRepeats && t.DefaultRRule != "" {
			return invalidField("neverRepeats", "a type that never repeats can't also have a fixed repeat")
		}
		// A single date only means something for events that are always all-day.
		t.SingleDay = t.SingleDay && t.DefaultAllDay
		if in.Fields != nil {
			defs, fe := validateFieldDefs(*in.Fields)
			if len(fe) > 0 {
				return fe.err()
			}
			b, _ := json.Marshal(defs)
			t.FieldsJSON = string(b)
		}
		if in.TitleTemplate != nil {
			t.TitleTemplate = *in.TitleTemplate
		}
		if in.TitleTemplate != nil || in.Fields != nil {
			if t.TitleTemplate, err = checkTitleTemplate(t.TitleTemplate, parseFieldDefs(t.FieldsJSON)); err != nil {
				return err
			}
		}
		t.UpdatedAt = tx.now
		if err := tx.db.Save(&t).Error; err != nil {
			return err
		}
		if t.TitleTemplate != before.TitleTemplate || in.Fields != nil {
			if err := tx.retitleType(t, ""); err != nil {
				return err
			}
		}
		out = eventTypeDTO(t)
		if err := tx.record("eventtype.updated", "event_type", t.ID, nil, before, out); err != nil {
			return err
		}
		return tx.emitEventTypes()
	})
	return &out, err
}

// DeleteEventType removes an unused type. Types still used by an event
// (including events in the trash) cannot be deleted, because their field
// values would lose their meaning.
func (s *Service) DeleteEventType(ctx context.Context, actor authz.Actor, id string) error {
	if !authz.CanAdminister(actor) {
		return forbidden("admins only")
	}
	return s.write(ctx, actor, func(tx *Tx) error {
		t, err := take[store.EventType](tx.db, id, "event type")
		if err != nil {
			return err
		}
		var n, m int64
		if err := tx.db.Model(&store.Event{}).Where("event_type_id = ?", id).Count(&n).Error; err != nil {
			return err
		}
		if err := tx.db.Model(&store.EventOverride{}).Where("event_type_id = ?", id).Count(&m).Error; err != nil {
			return err
		}
		if n+m > 0 {
			return &Error{Kind: KindConflict, Msg: "events still use this type; change or delete them first"}
		}
		if err := tx.db.Delete(&t).Error; err != nil {
			return err
		}
		if err := tx.record("eventtype.deleted", "event_type", t.ID, nil, eventTypeDTO(t), nil); err != nil {
			return err
		}
		return tx.emitEventTypes()
	})
}

// ReorderEventTypes sets the display order; ids must list every type once.
func (s *Service) ReorderEventTypes(ctx context.Context, actor authz.Actor, ids []string) ([]EventTypeDTO, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("admins only")
	}
	var out []EventTypeDTO
	err := s.write(ctx, actor, func(tx *Tx) error {
		var all []string
		if err := tx.db.Model(&store.EventType{}).Pluck("id", &all).Error; err != nil {
			return err
		}
		if len(uniq(ids)) != len(ids) || len(ids) != len(all) {
			return invalidField("ids", "list every event type exactly once")
		}
		known := map[string]bool{}
		for _, id := range all {
			known[id] = true
		}
		for i, id := range ids {
			if !known[id] {
				return invalidField("ids", "unknown event type")
			}
			if err := tx.db.Model(&store.EventType{}).Where("id = ?", id).Updates(map[string]any{"sort_order": i, "updated_at": tx.now}).Error; err != nil {
				return err
			}
		}
		if err := tx.record("eventtype.reordered", "event_type", "eventtypes", nil, nil, ids); err != nil {
			return err
		}
		var err error
		if out, err = listEventTypes(tx.db); err != nil {
			return err
		}
		return tx.emitEventTypes()
	})
	return out, err
}

// typeRuleErrors reports how an event's timing breaks its type's fixed
// settings. A type that is always all-day rules out timed events; one with a
// fixed repeat rules out other repeats; both together make a single date.
func typeRuleErrors(t store.EventType, allDay bool, rrule string, start, end time.Time, loc *time.Location) fieldErrors {
	var fe fieldErrors
	if t.DefaultAllDay && !allDay {
		fe.add("allDay", "%s events are always all-day", t.Name)
	}
	if t.NeverRepeats && rrule != "" {
		fe.add("rrule", "%s events never repeat", t.Name)
	}
	if t.DefaultRRule != "" {
		zone := loc
		if allDay {
			zone = time.UTC
		}
		want, err := normalizeRRule(t.DefaultRRule, start, zone)
		got, gerr := normalizeRRule(rrule, start, zone)
		if rrule == "" || err != nil || gerr != nil || got != want {
			fe.add("rrule", "%s events always repeat as %s", t.Name, t.DefaultRRule)
		}
	}
	if t.DefaultAllDay && t.SingleDay && allDay && !end.Equal(start.AddDate(0, 0, 1)) {
		fe.add("end", "%s events last a single day", t.Name)
	}
	return fe
}
