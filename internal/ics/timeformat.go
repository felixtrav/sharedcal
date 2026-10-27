package ics

import (
	"strings"
	"time"

	"github.com/emersion/go-ical"
)

const (
	dateLayout  = "20060102"
	localLayout = "20060102T150405"
	utcLayout   = "20060102T150405Z"
	day         = 24 * time.Hour
)

// timeFormat formats all DATE/DATE-TIME values of one event consistently:
// DTSTART, DTEND, EXDATE and RECURRENCE-ID share the TZID (or UTC) and value
// type, as RFC 5545 requires for EXDATE/RECURRENCE-ID matching.
type timeFormat struct {
	loc  *time.Location // nil means UTC ("Z" form)
	tzid string
}

func newTimeFormat(e *Event, zones *zoneSet) *timeFormat {
	times := []time.Time{e.Start, e.End}
	times = append(times, e.ExDates...)
	for _, o := range e.Overrides {
		times = append(times, o.RecurrenceID)
		if o.Start != nil {
			times = append(times, *o.Start)
		}
		if o.End != nil {
			times = append(times, *o.End)
		}
	}
	loc := zones.use(e.TimeZone, strings.TrimSpace(e.RRule) != "", times)
	if loc == nil {
		return &timeFormat{}
	}
	return &timeFormat{loc: loc, tzid: loc.String()}
}

// setRange sets DTSTART and DTEND. All-day ranges use VALUE=DATE with an
// exclusive end of at least one day; timed ranges never end before they start.
func (tf *timeFormat) setRange(props ical.Props, start, end time.Time, allDay bool) {
	if allDay {
		s := utcDate(start)
		e := utcDate(end)
		if !e.After(s) {
			e = s.Add(day)
		}
		props.Set(dateProp(ical.PropDateTimeStart, s))
		props.Set(dateProp(ical.PropDateTimeEnd, e))
		return
	}
	if end.Before(start) {
		end = start
	}
	props.Set(tf.list(ical.PropDateTimeStart, []time.Time{start}, false))
	props.Set(tf.list(ical.PropDateTimeEnd, []time.Time{end}, false))
}

// list builds a property holding one or more comma-separated values.
func (tf *timeFormat) list(name string, ts []time.Time, allDay bool) *ical.Prop {
	p := ical.NewProp(name)
	vals := make([]string, len(ts))
	for i, t := range ts {
		vals[i] = tf.format(t, allDay)
	}
	p.Value = strings.Join(vals, ",")
	switch {
	case allDay:
		p.SetValueType(ical.ValueDate)
	case tf.loc != nil:
		p.Params.Set(ical.PropTimezoneID, tf.tzid)
	}
	return p
}

func (tf *timeFormat) format(t time.Time, allDay bool) string {
	switch {
	case allDay:
		return t.UTC().Format(dateLayout)
	case tf.loc == nil:
		return t.UTC().Format(utcLayout)
	default:
		return t.In(tf.loc).Format(localLayout)
	}
}

func dateProp(name string, t time.Time) *ical.Prop {
	p := ical.NewProp(name)
	p.SetValueType(ical.ValueDate)
	p.Value = t.Format(dateLayout)
	return p
}

func utcDate(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
