// Package ics serializes SharedCal events as iCalendar (RFC 5545) for the
// read-only feed and, later, CalDAV.
//
// The feed deliberately has no fields for comments, suggestions or
// attachments: they can never leak into a calendar subscription.
package ics

import (
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/emersion/go-ical"
)

// ProdID is the default PRODID.
const ProdID = "-//SharedCal//SharedCal 1.0//EN"

// refreshInterval tells clients how often to re-poll the feed.
const refreshInterval = "PT15M"

// Event is one master event as stored in the events table.
type Event struct {
	UID                          string
	Title, Description, Location string
	// Start and End are UTC instants. For AllDay events the dates are taken
	// from Start/End in UTC, End exclusive.
	Start, End time.Time
	AllDay     bool
	// TimeZone is an IANA name. Timed DTSTART/DTEND are emitted with this
	// TZID so recurrences keep local wall time across DST. Empty or unknown
	// zones fall back to UTC.
	TimeZone   string
	RRule      string      // e.g. "FREQ=WEEKLY;BYDAY=FR" or ""
	ExDates    []time.Time // UTC instants of excluded occurrences
	Status     string      // "confirmed" | "tentative"
	Categories []string    // tag names
	Sequence   int64       // event version
	Created    time.Time
	Updated    time.Time
	URL        string     // link back to the app (optional)
	Overrides  []Override // emitted as VEVENTs with the same UID and RECURRENCE-ID
	// Cancelled marks a deleted event. It is emitted as a bare STATUS:CANCELLED
	// VEVENT (no title, description, location, tags or overrides) so clients
	// that missed the deletion remove it, without exposing deleted content.
	Cancelled bool
}

// Override is a modified or cancelled occurrence of a recurring event.
// Nil fields inherit from the master.
type Override struct {
	RecurrenceID time.Time // original occurrence start (UTC)
	// Cancelled occurrences become EXDATEs on the master instead of VEVENTs.
	Cancelled                    bool
	Title, Description, Location *string
	Start, End                   *time.Time
	AllDay                       *bool
	Status                       *string
}

// Calendar is a whole feed.
type Calendar struct {
	Name   string
	ProdID string // defaults to ProdID
	Events []Event
}

// Write serializes c as an iCalendar stream with CRLF line endings and
// 75-octet line folding.
func Write(w io.Writer, c Calendar) error {
	cal, err := build(c, time.Now().UTC())
	if err != nil {
		return err
	}
	fw := &foldingWriter{w: w}
	if err := ical.NewEncoder(fw).Encode(cal); err != nil {
		return fmt.Errorf("ics: encode: %w", err)
	}
	return fw.flush()
}

func build(c Calendar, now time.Time) (*ical.Calendar, error) {
	cal := ical.NewCalendar()
	prodID := c.ProdID
	if prodID == "" {
		prodID = ProdID
	}
	setRaw(cal.Props, ical.PropProductID, prodID)
	setRaw(cal.Props, ical.PropVersion, "2.0")
	setRaw(cal.Props, ical.PropCalendarScale, "GREGORIAN")
	if c.Name != "" {
		setText(cal.Props, "X-WR-CALNAME", c.Name)
	}
	refresh := ical.NewProp(ical.PropRefreshInterval)
	refresh.Params.Set("VALUE", string(ical.ValueDuration))
	refresh.Value = refreshInterval
	cal.Props.Set(refresh)
	setRaw(cal.Props, "X-PUBLISHED-TTL", refreshInterval)

	zones := newZoneSet(now)
	var events []*ical.Component
	for i := range c.Events {
		comps, err := buildEvent(&c.Events[i], zones, now)
		if err != nil {
			return nil, err
		}
		events = append(events, comps...)
	}
	// VTIMEZONEs first, as most clients expect them before use.
	cal.Children = append(zones.components(), events...)
	if len(cal.Children) == 0 {
		// go-ical refuses an empty VCALENDAR; a lone UTC VTIMEZONE keeps an
		// empty feed valid for every client.
		cal.Children = []*ical.Component{utcTimezone()}
	}
	return cal, nil
}

// buildEvent returns the master VEVENT followed by one VEVENT per
// non-cancelled override.
func buildEvent(e *Event, zones *zoneSet, now time.Time) ([]*ical.Component, error) {
	if e.UID == "" {
		return nil, fmt.Errorf("ics: event without UID")
	}
	tf := newTimeFormat(e, zones)
	master := ical.NewComponent(ical.CompEvent)
	stamp := e.Updated
	if stamp.IsZero() {
		stamp = now
	}
	setUTC(master.Props, ical.PropDateTimeStamp, stamp)
	setRaw(master.Props, ical.PropUID, cleanRaw(e.UID))
	if e.Cancelled {
		return []*ical.Component{cancelledEvent(master, e, tf)}, nil
	}
	addCommon(master.Props, e.Title, e.Description, e.Location, e.Status, e)
	tf.setRange(master.Props, e.Start, e.End, e.AllDay)

	recurring := strings.TrimSpace(e.RRule) != ""
	if recurring {
		setRaw(master.Props, ical.PropRecurrenceRule, normalizeRRule(e.RRule))
	}
	exdates := append([]time.Time(nil), e.ExDates...)
	var overrides []*ical.Component
	for i := range e.Overrides {
		o := &e.Overrides[i]
		if !recurring {
			continue // overrides only make sense for recurring masters
		}
		if o.Cancelled {
			exdates = append(exdates, o.RecurrenceID)
			continue
		}
		overrides = append(overrides, buildOverride(e, o, tf, stamp))
	}
	if recurring {
		// One EXDATE property per date: equivalent per RFC 5545, and go-ical
		// (used by go-webdav's CalDAV) cannot parse comma-separated EXDATEs.
		for _, x := range sortedUnique(exdates) {
			master.Props.Add(tf.list(ical.PropExceptionDates, []time.Time{x}, e.AllDay))
		}
	}
	return append([]*ical.Component{master}, overrides...), nil
}

// cancelledEvent completes a deleted event's VEVENT with only what a client
// needs to match and drop it: its times, recurrence, SEQUENCE and status.
func cancelledEvent(c *ical.Component, e *Event, tf *timeFormat) *ical.Component {
	tf.setRange(c.Props, e.Start, e.End, e.AllDay)
	if strings.TrimSpace(e.RRule) != "" {
		setRaw(c.Props, ical.PropRecurrenceRule, normalizeRRule(e.RRule))
	}
	setRaw(c.Props, ical.PropStatus, "CANCELLED")
	setRaw(c.Props, ical.PropSequence, fmt.Sprint(e.Sequence))
	if !e.Updated.IsZero() {
		setUTC(c.Props, ical.PropLastModified, e.Updated)
	}
	return c
}

func buildOverride(e *Event, o *Override, tf *timeFormat, stamp time.Time) *ical.Component {
	c := ical.NewComponent(ical.CompEvent)
	setUTC(c.Props, ical.PropDateTimeStamp, stamp)
	setRaw(c.Props, ical.PropUID, cleanRaw(e.UID))
	// RECURRENCE-ID uses the master's DTSTART value type and TZID.
	c.Props.Set(tf.list(ical.PropRecurrenceID, []time.Time{o.RecurrenceID}, e.AllDay))

	allDay := deref(o.AllDay, e.AllDay)
	start := deref(o.Start, o.RecurrenceID)
	end := deref(o.End, start.Add(e.End.Sub(e.Start)))
	addCommon(c.Props,
		deref(o.Title, e.Title), deref(o.Description, e.Description),
		deref(o.Location, e.Location), deref(o.Status, e.Status), e)
	tf.setRange(c.Props, start, end, allDay)
	return c
}

// addCommon sets the descriptive properties shared by masters and overrides.
func addCommon(props ical.Props, title, desc, loc, status string, e *Event) {
	setText(props, ical.PropSummary, title)
	if desc != "" {
		setText(props, ical.PropDescription, desc)
	}
	if loc != "" {
		setText(props, ical.PropLocation, loc)
	}
	setRaw(props, ical.PropStatus, mapStatus(status))
	if len(e.Categories) > 0 {
		p := ical.NewProp(ical.PropCategories)
		p.SetTextList(cleanTexts(e.Categories))
		p.Params.Del("VALUE")
		props.Set(p)
	}
	setRaw(props, ical.PropSequence, fmt.Sprint(e.Sequence))
	if !e.Created.IsZero() {
		setUTC(props, ical.PropCreated, e.Created)
	}
	if !e.Updated.IsZero() {
		setUTC(props, ical.PropLastModified, e.Updated)
	}
	if u, err := url.Parse(e.URL); err == nil && e.URL != "" && (u.Scheme == "https" || u.Scheme == "http") {
		p := ical.NewProp(ical.PropURL)
		p.SetURI(u)
		props.Set(p)
	}
}

func mapStatus(s string) string {
	switch strings.ToLower(s) {
	case "tentative":
		return "TENTATIVE"
	case "cancelled", "canceled":
		return "CANCELLED"
	}
	return "CONFIRMED"
}

// normalizeRRule strips an optional "RRULE:" prefix and any line breaks.
func normalizeRRule(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 6 && strings.EqualFold(s[:6], "RRULE:") {
		s = s[6:]
	}
	return cleanRaw(strings.ToUpper(s))
}

func deref[T any](p *T, def T) T {
	if p != nil {
		return *p
	}
	return def
}

func setRaw(props ical.Props, name, value string) {
	p := ical.NewProp(name)
	p.Value = value
	props.Set(p)
}

func setText(props ical.Props, name, value string) {
	p := ical.NewProp(name)
	p.SetText(cleanText(value))
	p.Params.Del("VALUE") // TEXT is the default type; keep output minimal
	props.Set(p)
}

func setUTC(props ical.Props, name string, t time.Time) {
	p := ical.NewProp(name)
	p.SetDateTime(t.UTC())
	// DATE-TIME is the default type for these properties; omit VALUE=.
	p.Params.Del("VALUE")
	props.Set(p)
}

// cleanText normalizes newlines to LF (escaped as \n by go-ical) and drops
// other control characters, which RFC 5545 TEXT does not allow.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

func cleanTexts(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = cleanText(s)
	}
	return out
}

// cleanRaw strips every control character from values written unescaped
// (UID, RRULE), so they cannot inject extra content lines.
func cleanRaw(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// sortedUnique sorts instants and removes duplicates.
func sortedUnique(ts []time.Time) []time.Time {
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	out := ts[:0]
	for i, t := range ts {
		if i == 0 || !t.Equal(ts[i-1]) {
			out = append(out, t)
		}
	}
	return out
}

// sortedKeys returns map keys in deterministic order.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
