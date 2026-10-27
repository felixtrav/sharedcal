package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/felixtrav/sharedcal/internal/store"
)

// Recurrence limits keep expansion cheap and bounded no matter what a client
// sends.
const (
	maxRRuleLen         = 500
	maxRRuleCount       = 5000
	maxRRuleInterval    = 1000
	maxOccurrencesQuery = 2000
	maxSeriesYears      = 200
)

var locCache sync.Map // name → *time.Location

func loadLocation(name string) (*time.Location, error) {
	if name == "" || len(name) > 64 || strings.ContainsAny(name, " \\") || strings.Contains(name, "..") {
		return nil, fmt.Errorf("invalid time zone %q", name)
	}
	if v, ok := locCache.Load(name); ok {
		return v.(*time.Location), nil
	}
	if strings.EqualFold(name, "local") {
		return nil, fmt.Errorf("invalid time zone %q", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locCache.Store(name, loc)
	return loc, nil
}

// parseRRule validates a client-supplied RRULE (without DTSTART) and returns
// its options. Only DAILY..YEARLY frequencies are accepted, and BYHOUR,
// BYMINUTE, BYSECOND and BYEASTER are refused: they can multiply occurrences
// far beyond what a shared calendar needs.
func parseRRule(s string) (*rrule.ROption, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "RRULE:")
	if len(s) > maxRRuleLen {
		return nil, fmt.Errorf("recurrence rule is too long")
	}
	if strings.ContainsAny(s, "\r\n") {
		return nil, fmt.Errorf("recurrence rule must be a single RRULE")
	}
	for _, part := range strings.Split(s, ";") {
		key, _, _ := strings.Cut(part, "=")
		switch strings.ToUpper(key) {
		case "FREQ", "INTERVAL", "WKST", "COUNT", "UNTIL", "BYSETPOS", "BYMONTH", "BYMONTHDAY", "BYYEARDAY", "BYWEEKNO", "BYDAY":
		default:
			return nil, fmt.Errorf("unsupported recurrence part %q", key)
		}
	}
	opt, err := rrule.StrToROption(strings.ToUpper(s))
	if err != nil {
		return nil, fmt.Errorf("invalid recurrence rule: %v", err)
	}
	switch opt.Freq {
	case rrule.DAILY, rrule.WEEKLY, rrule.MONTHLY, rrule.YEARLY:
	default:
		return nil, fmt.Errorf("recurrence must repeat daily, weekly, monthly or yearly")
	}
	if opt.Interval < 0 || opt.Interval > maxRRuleInterval {
		return nil, fmt.Errorf("recurrence interval must be 1–%d", maxRRuleInterval)
	}
	if opt.Count < 0 || opt.Count > maxRRuleCount {
		return nil, fmt.Errorf("recurrence count must be 1–%d", maxRRuleCount)
	}
	if opt.Count > 0 && !opt.Until.IsZero() {
		return nil, fmt.Errorf("recurrence cannot have both COUNT and UNTIL")
	}
	return opt, nil
}

// normalizeRRule validates s against the event start and returns its
// canonical form.
func normalizeRRule(s string, start time.Time, loc *time.Location) (string, error) {
	if strings.TrimSpace(s) == "" {
		return "", nil
	}
	opt, err := parseRRule(s)
	if err != nil {
		return "", err
	}
	if !opt.Until.IsZero() {
		if opt.Until.Before(start) {
			return "", fmt.Errorf("recurrence ends before the event starts")
		}
		if opt.Until.After(start.AddDate(maxSeriesYears, 0, 0)) {
			return "", fmt.Errorf("recurrence end is too far in the future")
		}
	}
	r, err := buildRule(opt, start, loc)
	if err != nil {
		return "", err
	}
	// Reject rules that can never (or almost never) match, e.g. Feb 30:
	// expansion would otherwise scan centuries looking for one.
	if len(r.Between(start, start.AddDate(8, 0, 0), true)) == 0 {
		return "", fmt.Errorf("recurrence rule never occurs")
	}
	return opt.RRuleString(), nil
}

func buildRule(opt *rrule.ROption, start time.Time, loc *time.Location) (*rrule.RRule, error) {
	o := *opt
	// DTSTART in the event's zone keeps the wall-clock time across DST.
	o.Dtstart = start.In(loc)
	return rrule.NewRRule(o)
}

// eventLoc returns the location recurrence is computed in: the event's zone
// for timed events, UTC for all-day events (whose dates are stored as UTC
// midnights).
func eventLoc(e *store.Event) *time.Location {
	if e.AllDay {
		return time.UTC
	}
	if loc, err := loadLocation(e.TimeZone); err == nil {
		return loc
	}
	return time.UTC
}

func exdates(e *store.Event) []time.Time {
	var raw []time.Time
	_ = json.Unmarshal([]byte(e.ExdatesJSON), &raw)
	for i := range raw {
		raw[i] = raw[i].UTC()
	}
	return raw
}

func setExdates(e *store.Event, ts []time.Time) {
	seen := map[int64]bool{}
	out := make([]time.Time, 0, len(ts))
	for _, t := range ts {
		t = t.UTC().Truncate(time.Second)
		if !seen[t.Unix()] {
			seen[t.Unix()] = true
			out = append(out, t)
		}
	}
	b, _ := json.Marshal(out)
	e.ExdatesJSON = string(b)
}

// Occurrence is one expanded instance of an event.
type Occurrence struct {
	Start, End   time.Time
	RecurrenceID *time.Time // nil for non-recurring events
	Override     *store.EventOverride
}

// expand returns the occurrences of e overlapping [from, to).
func expand(e *store.Event, overrides []store.EventOverride, from, to time.Time) ([]Occurrence, error) {
	dur := e.EndAt.Sub(e.StartAt)
	if e.RRule == "" {
		if overlaps(e.StartAt, e.EndAt, from, to) {
			return []Occurrence{{Start: e.StartAt, End: e.EndAt}}, nil
		}
		return nil, nil
	}
	opt, err := parseRRule(e.RRule)
	if err != nil {
		return nil, err
	}
	r, err := buildRule(opt, e.StartAt, eventLoc(e))
	if err != nil {
		return nil, err
	}
	ex := map[int64]bool{}
	for _, t := range exdates(e) {
		ex[t.Unix()] = true
	}
	byRID := map[int64]*store.EventOverride{}
	for i := range overrides {
		byRID[overrides[i].RecurrenceID.UTC().Unix()] = &overrides[i]
	}
	var out []Occurrence
	seen := map[int64]bool{}
	for _, st := range r.Between(from.Add(-dur), to, true) {
		st = st.UTC()
		if len(out) >= maxOccurrencesQuery {
			break
		}
		if ex[st.Unix()] {
			continue
		}
		rid := st
		occ := Occurrence{Start: st, End: st.Add(dur), RecurrenceID: &rid}
		if ov := byRID[st.Unix()]; ov != nil {
			seen[st.Unix()] = true
			if ov.Cancelled {
				continue
			}
			applyOverrideTimes(&occ, ov)
			occ.Override = ov
		}
		if overlaps(occ.Start, occ.End, from, to) {
			out = append(out, occ)
		}
	}
	// Overrides moved into the range from an occurrence outside it.
	for i := range overrides {
		ov := &overrides[i]
		rid := ov.RecurrenceID.UTC()
		if seen[rid.Unix()] || ov.Cancelled || ex[rid.Unix()] || ov.StartAt == nil {
			continue
		}
		occ := Occurrence{Start: rid, End: rid.Add(dur), RecurrenceID: &rid, Override: ov}
		applyOverrideTimes(&occ, ov)
		if overlaps(occ.Start, occ.End, from, to) && isOccurrence(r, rid) {
			out = append(out, occ)
		}
	}
	return out, nil
}

func applyOverrideTimes(o *Occurrence, ov *store.EventOverride) {
	if ov.StartAt != nil {
		o.Start = ov.StartAt.UTC()
	}
	if ov.EndAt != nil {
		o.End = ov.EndAt.UTC()
	}
}

func isOccurrence(r *rrule.RRule, t time.Time) bool {
	got := r.After(t.Add(-time.Second), false)
	return !got.IsZero() && got.UTC().Equal(t.UTC())
}

// isEventOccurrence reports whether t is an occurrence start of e.
func isEventOccurrence(e *store.Event, t time.Time) bool {
	if e.RRule == "" {
		return false
	}
	opt, err := parseRRule(e.RRule)
	if err != nil {
		return false
	}
	r, err := buildRule(opt, e.StartAt, eventLoc(e))
	if err != nil {
		return false
	}
	return isOccurrence(r, t)
}

// overlaps treats zero-length events as instants that overlap a range when
// they start inside it.
func overlaps(start, end, from, to time.Time) bool {
	if !start.Before(to) {
		return false
	}
	return end.After(from) || (end.Equal(start) && !start.Before(from))
}

// seriesEnd computes the end of the last occurrence, or nil when unbounded.
func seriesEnd(e *store.Event) (*time.Time, error) {
	if e.RRule == "" {
		return nil, nil
	}
	opt, err := parseRRule(e.RRule)
	if err != nil {
		return nil, err
	}
	if opt.Count == 0 && opt.Until.IsZero() {
		return nil, nil
	}
	r, err := buildRule(opt, e.StartAt, eventLoc(e))
	if err != nil {
		return nil, err
	}
	var last time.Time
	if opt.Count > 0 {
		all := r.All()
		if len(all) > 0 {
			last = all[len(all)-1]
		}
	} else {
		last = r.Before(opt.Until.Add(time.Second), true)
	}
	if last.IsZero() {
		last = e.StartAt
	}
	end := last.UTC().Add(e.EndAt.Sub(e.StartAt))
	return &end, nil
}

// splitRule returns the rule for the part of a series before `at` and the
// rule for the part from `at` on (RFC 5545 "this and following").
func splitRule(e *store.Event, at time.Time) (head, tail string, err error) {
	opt, err := parseRRule(e.RRule)
	if err != nil {
		return "", "", err
	}
	r, err := buildRule(opt, e.StartAt, eventLoc(e))
	if err != nil {
		return "", "", err
	}
	before := len(r.Between(e.StartAt, at.Add(-time.Second), true))
	h, t := *opt, *opt
	h.Count, h.Until = 0, at.Add(-time.Second).UTC()
	if opt.Count > 0 {
		t.Count = opt.Count - before
		if t.Count <= 0 {
			return "", "", fmt.Errorf("occurrence is past the end of the series")
		}
	}
	return h.RRuleString(), t.RRuleString(), nil
}

// dayDiff is the number of calendar days from a to b (wall clock dates).
func dayDiff(a, b time.Time) int {
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(db.Sub(da).Hours() / 24)
}

var weekdays = []rrule.Weekday{rrule.MO, rrule.TU, rrule.WE, rrule.TH, rrule.FR, rrule.SA, rrule.SU}

// shiftRuleDays moves a rule's day selectors (BYDAY, positive BYMONTHDAY and
// BYYEARDAY) by days, keeping the rule's shape.
func shiftRuleDays(s string, days int) (string, error) {
	opt, err := parseRRule(s)
	if err != nil {
		return "", err
	}
	for i, wd := range opt.Byweekday {
		d := ((wd.Day()+days)%7 + 7) % 7
		opt.Byweekday[i] = weekdays[d].Nth(wd.N())
	}
	shiftInts := func(vals []int, max int) {
		for i, v := range vals {
			if v > 0 {
				vals[i] = ((v-1+days)%max+max)%max + 1
			}
		}
	}
	shiftInts(opt.Bymonthday, 31)
	shiftInts(opt.Byyearday, 366)
	return opt.RRuleString(), nil
}
