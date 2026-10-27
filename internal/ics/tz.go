package ics

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

// VTIMEZONE generation from Go's tz database.
//
// For every zone used we compute its real offset transitions (via
// time.Time.ZoneBounds) from one year before the earliest event time to one
// year after the latest (or after "now" for recurring events). Historic
// transitions are emitted as RDATE-based observances, which is exact for any
// rule changes in the past. If the zone's last full year follows a regular
// "nth/last weekday of month" rule that Go confirms for the next ten years,
// that year is emitted as RRULE-based DAYLIGHT/STANDARD observances so
// open-ended recurrences stay correct indefinitely; otherwise RDATEs are used
// for the whole span.

const ruleVerifyYears = 10

type zoneSpan struct {
	loc      *time.Location
	min, max time.Time
}

type zoneSet struct {
	now   time.Time
	zones map[string]*zoneSpan
}

func newZoneSet(now time.Time) *zoneSet {
	return &zoneSet{now: now, zones: map[string]*zoneSpan{}}
}

// use resolves an IANA zone and records the time span it must cover. It
// returns nil for UTC, empty or unknown zones; callers then emit UTC times.
func (z *zoneSet) use(name string, recurring bool, times []time.Time) *time.Location {
	name = strings.TrimSpace(name)
	switch name {
	case "", "UTC", "Etc/UTC", "Z", "Local":
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc == time.UTC {
		return nil
	}
	span, ok := z.zones[name]
	if !ok {
		span = &zoneSpan{loc: loc}
		z.zones[name] = span
	}
	if recurring {
		times = append(times, z.now)
	}
	for _, t := range times {
		if t.IsZero() {
			continue
		}
		if span.min.IsZero() || t.Before(span.min) {
			span.min = t
		}
		if t.After(span.max) {
			span.max = t
		}
	}
	return loc
}

// components returns one VTIMEZONE per used zone, sorted by TZID.
func (z *zoneSet) components() []*ical.Component {
	var out []*ical.Component
	for _, name := range sortedKeys(z.zones) {
		span := z.zones[name]
		minT, maxT := span.min, span.max
		if minT.IsZero() {
			minT, maxT = z.now, z.now
		}
		from := time.Date(minT.UTC().Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
		to := time.Date(maxT.UTC().Year()+2, 1, 1, 0, 0, 0, 0, time.UTC)
		out = append(out, buildVTimezone(name, span.loc, from, to))
	}
	return out
}

// transition is a change of UTC offset, abbreviation or DST flag.
type transition struct {
	at      time.Time // UTC instant
	fromOff int       // seconds east of UTC before the change
	toOff   int
	name    string
	isDST   bool
}

// zoneAt describes the zone period in effect at t.
func zoneAt(loc *time.Location, t time.Time) (name string, off int, dst bool) {
	lt := t.In(loc)
	name, off = lt.Zone()
	return name, off, lt.IsDST()
}

// transitions lists all transitions in [from, to).
func transitions(loc *time.Location, from, to time.Time) []transition {
	var out []transition
	t := from
	for t.Before(to) {
		_, end := t.In(loc).ZoneBounds()
		if end.IsZero() || !end.Before(to) || !end.After(t) {
			break
		}
		_, prevOff, _ := zoneAt(loc, end.Add(-time.Second))
		name, off, dst := zoneAt(loc, end)
		out = append(out, transition{at: end.UTC(), fromOff: prevOff, toOff: off, name: name, isDST: dst})
		t = end
	}
	return out
}

func buildVTimezone(tzid string, loc *time.Location, from, to time.Time) *ical.Component {
	tz := ical.NewComponent(ical.CompTimezone)
	setRaw(tz.Props, ical.PropTimezoneID, tzid)

	// The period already in effect at the start of the span.
	name, off, dst := zoneAt(loc, from)
	tz.Children = append(tz.Children, observance(transition{at: from, fromOff: off, toOff: off, name: name, isDST: dst}, nil, ""))

	all := transitions(loc, from, to)
	ruleYear := to.Year() - 1
	rules, ok := inferRules(loc, ruleYear)
	historic := all
	if ok {
		historic = nil
		for _, tr := range all {
			if tr.at.Add(time.Duration(tr.fromOff)*time.Second).Year() < ruleYear {
				historic = append(historic, tr)
			}
		}
	}
	tz.Children = append(tz.Children, rdateObservances(historic)...)
	for _, r := range rules {
		tz.Children = append(tz.Children, observance(r.first, nil, r.rrule()))
	}
	return tz
}

// rdateObservances groups transitions with identical offsets/name/DST flag
// into one observance each: DTSTART is the first, RDATEs the rest.
func rdateObservances(trs []transition) []*ical.Component {
	type key struct {
		fromOff, toOff int
		name           string
		dst            bool
	}
	groups := map[key][]transition{}
	var order []key
	for _, tr := range trs {
		k := key{tr.fromOff, tr.toOff, tr.name, tr.isDST}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], tr)
	}
	var out []*ical.Component
	for _, k := range order {
		g := groups[k]
		out = append(out, observance(g[0], g[1:], ""))
	}
	return out
}

// observance builds a STANDARD or DAYLIGHT component. Per RFC 5545, DTSTART
// and RDATE are local times in the offset in effect *before* the onset.
func observance(first transition, more []transition, rrule string) *ical.Component {
	name := ical.CompTimezoneStandard
	if first.isDST {
		name = ical.CompTimezoneDaylight
	}
	c := ical.NewComponent(name)
	setRaw(c.Props, ical.PropDateTimeStart, localOnset(first))
	setRaw(c.Props, ical.PropTimezoneOffsetFrom, formatOffset(first.fromOff))
	setRaw(c.Props, ical.PropTimezoneOffsetTo, formatOffset(first.toOff))
	if first.name != "" {
		setText(c.Props, ical.PropTimezoneName, first.name)
	}
	if rrule != "" {
		setRaw(c.Props, ical.PropRecurrenceRule, rrule)
	}
	if len(more) > 0 {
		vals := make([]string, len(more))
		for i, tr := range more {
			vals[i] = localOnset(tr)
		}
		setRaw(c.Props, ical.PropRecurrenceDates, strings.Join(vals, ","))
	}
	return c
}

func localOnset(tr transition) string {
	return tr.at.Add(time.Duration(tr.fromOff) * time.Second).UTC().Format(localLayout)
}

func formatOffset(sec int) string {
	sign := '+'
	if sec < 0 {
		sign, sec = '-', -sec
	}
	h, m, s := sec/3600, sec/60%60, sec%60
	if s != 0 {
		return fmt.Sprintf("%c%02d%02d%02d", sign, h, m, s)
	}
	return fmt.Sprintf("%c%02d%02d", sign, h, m)
}

func utcTimezone() *ical.Component {
	from := time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)
	return buildVTimezone("UTC", time.UTC, from, from)
}

// yearlyRule is a transition recurring on the nth (or last, n = -1) weekday
// of a month at a fixed local wall-clock time.
type yearlyRule struct {
	first   transition // the onset in the rule's first year (DTSTART)
	month   time.Month
	weekday time.Weekday
	nth     int
	tod     time.Duration // local time of day, in the pre-transition offset
}

var weekdayCodes = [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

func (r yearlyRule) rrule() string {
	return fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%d%s", int(r.month), r.nth, weekdayCodes[r.weekday])
}

// onset computes the rule's transition instant in year y.
func (r yearlyRule) onset(y int) time.Time {
	d := nthWeekday(y, r.month, r.weekday, r.nth)
	return d.Add(r.tod).Add(-time.Duration(r.first.fromOff) * time.Second)
}

func nthWeekday(y int, m time.Month, wd time.Weekday, nth int) time.Time {
	if nth > 0 {
		first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
		delta := (int(wd) - int(first.Weekday()) + 7) % 7
		return first.AddDate(0, 0, delta+7*(nth-1))
	}
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC)
	delta := (int(last.Weekday()) - int(wd) + 7) % 7
	return last.AddDate(0, 0, -delta)
}

// inferRules returns RRULE-based observances for year y if the zone has
// exactly two transitions that year (one into DST, one out) and both follow a
// weekday rule that Go's tz data confirms for the following years.
func inferRules(loc *time.Location, y int) ([]yearlyRule, bool) {
	start := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
	trs := transitions(loc, start, start.AddDate(1, 0, 0))
	if len(trs) != 2 || trs[0].isDST == trs[1].isDST {
		return nil, false
	}
	var rules []yearlyRule
	for _, tr := range trs {
		r, ok := matchRule(loc, tr, y)
		if !ok {
			return nil, false
		}
		rules = append(rules, r)
	}
	return rules, true
}

// matchRule tries "nth weekday" and "last weekday" interpretations of tr.
func matchRule(loc *time.Location, tr transition, y int) (yearlyRule, bool) {
	local := tr.at.Add(time.Duration(tr.fromOff) * time.Second).UTC()
	date := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	base := yearlyRule{first: tr, month: local.Month(), weekday: local.Weekday(), tod: local.Sub(date)}
	for _, nth := range []int{(local.Day()-1)/7 + 1, -1} {
		if nth == 5 {
			continue // "5th weekday" does not exist every year; only "last" can match
		}
		r := base
		r.nth = nth
		if nthWeekday(y, r.month, r.weekday, nth).Equal(date) && r.verify(loc, y) {
			return r, true
		}
	}
	return yearlyRule{}, false
}

// verify checks the rule against Go's tz data for the following years.
func (r yearlyRule) verify(loc *time.Location, y int) bool {
	for yy := y + 1; yy <= y+ruleVerifyYears; yy++ {
		at := r.onset(yy)
		_, before, _ := zoneAt(loc, at.Add(-time.Second))
		_, after, dst := zoneAt(loc, at)
		if before != r.first.fromOff || after != r.first.toOff || dst != r.first.isDST {
			return false
		}
	}
	return true
}
