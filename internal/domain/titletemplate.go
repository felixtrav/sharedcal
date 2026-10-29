package domain

import (
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/felixtrav/sharedcal/internal/store"
)

const maxTitleTemplate = 200

// placeholderRE matches {{key}} references to a field of the event type.
var placeholderRE = regexp.MustCompile(`\{\{\s*([a-z][a-z0-9_]*)\s*\}\}`)

// sectionTagRE matches the {{#key}} and {{/key}} tags around an optional part
// of a template, which is shown only when the field has a value.
var sectionTagRE = regexp.MustCompile(`\{\{([#/])([a-z][a-z0-9_]*)\}\}`)

// titleFieldKinds are the kinds whose values read naturally inside a title.
func titleFieldKind(kind string) bool {
	return kind == FieldText || kind == FieldNumber || kind == FieldSelect || kind == FieldMembers || kind == FieldTime
}

// applySections keeps the parts of tpl whose field `has` a value and drops the
// rest, tags included. It errors on tags that don't pair up.
func applySections(tpl string, has func(key string) bool) (string, error) {
	var out strings.Builder
	var open []string // keys of the sections we are inside
	hidden := 0       // how many of them are dropped
	var visible []bool
	last := 0
	for _, m := range sectionTagRE.FindAllStringSubmatchIndex(tpl, -1) {
		if hidden == 0 {
			out.WriteString(tpl[last:m[0]])
		}
		last = m[1]
		key := tpl[m[4]:m[5]]
		if tpl[m[2]:m[3]] == "#" {
			show := has(key)
			open, visible = append(open, key), append(visible, show)
			if !show {
				hidden++
			}
			continue
		}
		if len(open) == 0 || open[len(open)-1] != key {
			return "", errUnbalanced
		}
		if !visible[len(visible)-1] {
			hidden--
		}
		open, visible = open[:len(open)-1], visible[:len(visible)-1]
	}
	if len(open) > 0 {
		return "", errUnbalanced
	}
	if hidden == 0 {
		out.WriteString(tpl[last:])
	}
	return out.String(), nil
}

var errUnbalanced = errors.New("every {{#field}} needs a matching {{/field}}")

// checkTitleTemplate validates tpl against the type's fields: it needs at
// least one placeholder, and every placeholder must name a short-value field.
func checkTitleTemplate(tpl string, defs []FieldDef) (string, error) {
	tpl = strings.TrimSpace(tpl)
	if tpl == "" {
		return "", nil
	}
	if utf8.RuneCountInString(tpl) > maxTitleTemplate {
		return "", invalidField("titleTemplate", "the title template is limited to %d characters", maxTitleTemplate)
	}
	if !slices.ContainsFunc(defs, func(d FieldDef) bool { return d.Required }) {
		return "", invalidField("titleTemplate", "a type with an automatic title needs at least one required field")
	}
	if _, err := applySections(tpl, func(string) bool { return true }); err != nil {
		return "", invalidField("titleTemplate", "%v", err)
	}
	// With every optional part dropped there must still be a field left, or the title would be empty.
	always, _ := applySections(tpl, func(string) bool { return false })
	if !placeholderRE.MatchString(always) {
		return "", invalidField("titleTemplate", "the title template must use at least one field outside optional parts, like {{person}}")
	}
	// Placeholders and the fields behind optional parts must all be short-value fields of this type.
	var refs []string
	for _, m := range placeholderRE.FindAllStringSubmatch(tpl, -1) {
		refs = append(refs, m[1])
	}
	for _, m := range sectionTagRE.FindAllStringSubmatch(tpl, -1) {
		refs = append(refs, m[2])
	}
	for _, key := range refs {
		ok := false
		for _, d := range defs {
			if d.Key == key {
				ok = titleFieldKind(d.Kind)
				break
			}
		}
		if !ok {
			return "", invalidField("titleTemplate", "%q is not a text, number, time, choice or people field of this type", key)
		}
	}
	return tpl, nil
}

// fieldTitleText is a field value as it reads inside a title. People are
// stored by id and shown by their current name; unknown ids are left out.
func fieldTitleText(kind string, v any, names map[string]string) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case []string:
		return joinTitleList(kind, x, names)
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		return joinTitleList(kind, parts, names)
	}
	return ""
}

func joinTitleList(kind string, items []string, names map[string]string) string {
	if kind == FieldMembers {
		resolved := make([]string, 0, len(items))
		for _, id := range items {
			if n := names[id]; n != "" {
				resolved = append(resolved, n)
			}
		}
		items = resolved
	}
	return strings.Join(items, ", ")
}

// memberNames loads the current display names of every person in the values
// of t's members fields.
func memberNames(db *gorm.DB, t store.EventType, values ...map[string]any) (map[string]string, error) {
	var ids []string
	for _, d := range parseFieldDefs(t.FieldsJSON) {
		if d.Kind != FieldMembers {
			continue
		}
		for _, v := range values {
			switch x := v[d.Key].(type) {
			case []string:
				ids = append(ids, x...)
			case []any:
				for _, e := range x {
					if s, ok := e.(string); ok {
						ids = append(ids, s)
					}
				}
			}
		}
	}
	names := map[string]string{}
	if len(ids) == 0 {
		return names, nil
	}
	var users []store.User
	if err := db.Where("id IN ?", uniq(ids)).Find(&users).Error; err != nil {
		return nil, err
	}
	for i := range users {
		names[users[i].ID] = refOf(&users[i]).Name
	}
	return names, nil
}

// renderTitle fills the type's title template from field values. Missing
// values render empty and spaces collapse; with nothing left the type's name
// is the title. timeText, when set, rewrites time values (stored as HH:MM in the event's zone) for a viewer; the
// stored title passes nil and keeps them as they are. The web client mirrors this in lib/titleTemplate.ts.
func renderTitle(t store.EventType, values map[string]any, names map[string]string, timeText func(hhmm string) string) string {
	kinds := map[string]string{}
	for _, d := range parseFieldDefs(t.FieldsJSON) {
		kinds[d.Key] = d.Kind
	}
	text := func(key string) string {
		s := fieldTitleText(kinds[key], values[key], names)
		if s != "" && kinds[key] == FieldTime && timeText != nil {
			return timeText(s)
		}
		return s
	}
	out, err := applySections(t.TitleTemplate, func(key string) bool { return text(key) != "" })
	if err != nil {
		out = t.TitleTemplate // saved templates are validated; show it plainly rather than lose the title
	}
	out = placeholderRE.ReplaceAllStringFunc(out, func(m string) string { return text(placeholderRE.FindStringSubmatch(m)[1]) })
	out = strings.Join(strings.Fields(out), " ")
	if out == "" {
		out = t.Name
	}
	if utf8.RuneCountInString(out) > maxTitle {
		out = string([]rune(out)[:maxTitle])
	}
	return out
}

// retitleType regenerates the titles of the events of t after its template,
// fields or a person's name changed. With onlyUser set, only events that
// mention that person are touched. Events get a new version so open editors
// see a conflict instead of overwriting the new title; occurrences that
// inherit their fields lose their own title and follow the series again.
func (tx *Tx) retitleType(t store.EventType, onlyUser string) error {
	if t.TitleTemplate == "" {
		return nil
	}
	like := "%" + onlyUser + "%"
	var evs []store.Event
	q := tx.db.Where("event_type_id = ?", t.ID)
	if onlyUser != "" {
		q = q.Where("fields_json LIKE ?", like)
	}
	err := q.FindInBatches(&evs, 200, func(db *gorm.DB, _ int) error {
		for _, e := range evs {
			vals := parseFieldValues(e.FieldsJSON)
			names, err := memberNames(tx.db, t, vals)
			if err != nil {
				return err
			}
			title := renderTitle(t, vals, names, nil)
			if title == e.Title {
				continue
			}
			if err := db.Model(&store.Event{}).Where("id = ?", e.ID).Updates(map[string]any{"title": title, "version": e.Version + 1, "updated_at": tx.now}).Error; err != nil {
				return err
			}
		}
		return nil
	}).Error
	if err != nil {
		return err
	}
	if onlyUser == "" {
		sub := tx.db.Model(&store.Event{}).Select("id").Where("event_type_id = ?", t.ID)
		if err := tx.db.Model(&store.EventOverride{}).Where("event_id IN (?) AND type_overridden = ?", sub, false).Update("title", nil).Error; err != nil {
			return err
		}
	}
	var ovs []store.EventOverride
	oq := tx.db.Where("type_overridden = ? AND event_type_id = ?", true, t.ID)
	if onlyUser != "" {
		oq = oq.Where("fields_json LIKE ?", like)
	}
	if err := oq.Find(&ovs).Error; err != nil {
		return err
	}
	for _, ov := range ovs {
		vals := parseFieldValues(ov.FieldsJSON)
		names, err := memberNames(tx.db, t, vals)
		if err != nil {
			return err
		}
		if err := tx.db.Model(&store.EventOverride{}).Where("id = ?", ov.ID).Updates(map[string]any{"title": renderTitle(t, vals, names, nil), "updated_at": tx.now}).Error; err != nil {
			return err
		}
	}
	return nil
}

// retitleForPerson refreshes generated titles that include the person after
// their display name changed (or they were erased).
func (tx *Tx) retitleForPerson(userID string) error {
	var types []store.EventType
	if err := tx.db.Where("title_template <> ?", "").Find(&types).Error; err != nil {
		return err
	}
	for _, t := range types {
		if err := tx.retitleType(t, userID); err != nil {
			return err
		}
	}
	return nil
}

// viewerClock is how one person reads times: their zone (nil keeps the event's own) and 12- or 24-hour clock.
type viewerClock struct {
	Loc      *time.Location
	Clock24h bool
}

// viewerTimeText converts a stored HH:MM, which is a wall time in the event's zone on the day of `at`, to the
// viewer's zone and clock style. Only the time is shown, so a conversion across midnight reads as the other day's time.
func viewerTimeText(e *store.Event, at time.Time, v viewerClock) func(string) string {
	evLoc, err := loadLocation(e.TimeZone)
	if err != nil {
		evLoc = time.UTC
	}
	day := at.In(evLoc)
	if e.AllDay {
		day = at.UTC()
	}
	out := evLoc
	if v.Loc != nil {
		out = v.Loc
	}
	layout := "3:04 PM"
	if v.Clock24h {
		layout = "15:04"
	}
	return func(hhmm string) string {
		wall, err := time.Parse("15:04", hhmm)
		if err != nil {
			return hhmm
		}
		return time.Date(day.Year(), day.Month(), day.Day(), wall.Hour(), wall.Minute(), 0, 0, evLoc).In(out).Format(layout)
	}
}

// viewerClockFor loads a person's zone and clock preference.
func viewerClockFor(db *gorm.DB, userID string) viewerClock {
	var p store.UserPreference
	if err := db.Where("user_id = ?", userID).Take(&p).Error; err != nil {
		return viewerClock{}
	}
	v := viewerClock{Clock24h: p.Clock24h}
	if loc, err := loadLocation(p.TimeZone); err == nil && p.TimeZone != "" {
		v.Loc = loc
	}
	return v
}
