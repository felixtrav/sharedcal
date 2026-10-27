package ics

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/teambition/rrule-go"
)

func writeAndParse(t *testing.T, c Calendar) (string, *ical.Calendar) {
	t.Helper()
	var buf bytes.Buffer
	if err := Write(&buf, c); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw := buf.String()
	checkLines(t, raw)
	cal, err := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	return raw, cal
}

// checkLines asserts CRLF endings, the 75-octet limit and that folding never
// split a UTF-8 sequence (each physical line must be valid UTF-8 on its own).
func checkLines(t *testing.T, raw string) {
	t.Helper()
	if !strings.HasSuffix(raw, "\r\n") {
		t.Fatal("output does not end with CRLF")
	}
	for i, line := range strings.Split(strings.TrimSuffix(raw, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Errorf("line %d has %d octets: %q", i, len(line), line)
		}
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("line %d contains bare CR/LF: %q", i, line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("line %d splits a UTF-8 sequence: %q", i, line)
		}
	}
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func text(t *testing.T, c *ical.Component, name string) string {
	t.Helper()
	p := c.Props.Get(name)
	if p == nil {
		t.Fatalf("%s missing %s", c.Name, name)
	}
	s, err := p.Text()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func children(c *ical.Component, name string) []*ical.Component {
	var out []*ical.Component
	for _, ch := range c.Children {
		if ch.Name == name {
			out = append(out, ch)
		}
	}
	return out
}

func TestCalendarHeaders(t *testing.T) {
	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	raw, cal := writeAndParse(t, Calendar{Name: "Family; Friends", Events: []Event{{
		UID: "0190-abc", Title: "Lunch", Start: start, End: start.Add(time.Hour),
	}}})
	for _, want := range []string{
		"PRODID:-//SharedCal//SharedCal 1.0//EN\r\n",
		"VERSION:2.0\r\n",
		"CALSCALE:GREGORIAN\r\n",
		"REFRESH-INTERVAL;VALUE=DURATION:PT15M\r\n",
		"X-PUBLISHED-TTL:PT15M\r\n",
		`X-WR-CALNAME:Family\; Friends` + "\r\n",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q in\n%s", want, raw)
		}
	}
	if got := text(t, cal.Component, "X-WR-CALNAME"); got != "Family; Friends" {
		t.Errorf("calname = %q", got)
	}
	ev := cal.Events()
	if len(ev) != 1 {
		t.Fatalf("got %d events", len(ev))
	}
	if ev[0].Props.Get(ical.PropDateTimeStamp) == nil {
		t.Error("DTSTAMP missing")
	}
	if got := ev[0].Props.Get(ical.PropDateTimeStart).Value; got != "20260501T100000Z" {
		t.Errorf("UTC DTSTART = %q", got)
	}
	if len(children(cal.Component, ical.CompTimezone)) != 0 {
		t.Error("UTC event must not emit a VTIMEZONE")
	}
}

func TestEventFields(t *testing.T) {
	created := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	updated := created.Add(48 * time.Hour)
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	_, cal := writeAndParse(t, Calendar{Events: []Event{{
		UID: "u-1", Title: "Board, meeting", Location: "Room 1; floor 2",
		Start: start, End: start.Add(2 * time.Hour), Status: "tentative",
		Categories: []string{"Work", "a,b"}, Sequence: 7, Created: created, Updated: updated,
		URL: "https://cal.example.com/?event=u-1",
	}}})
	ev := cal.Events()[0]
	if got := text(t, ev.Component, ical.PropSummary); got != "Board, meeting" {
		t.Errorf("summary %q", got)
	}
	if got := text(t, ev.Component, ical.PropLocation); got != "Room 1; floor 2" {
		t.Errorf("location %q", got)
	}
	if got := ev.Props.Get(ical.PropStatus).Value; got != "TENTATIVE" {
		t.Errorf("status %q", got)
	}
	cats, _ := ev.Props.Get(ical.PropCategories).TextList()
	if strings.Join(cats, "|") != "Work|a,b" {
		t.Errorf("categories %q", cats)
	}
	if got := ev.Props.Get(ical.PropSequence).Value; got != "7" {
		t.Errorf("sequence %q", got)
	}
	if got := ev.Props.Get(ical.PropDateTimeStamp).Value; got != "20260103T080000Z" {
		t.Errorf("DTSTAMP %q (want Updated)", got)
	}
	if got := ev.Props.Get(ical.PropCreated).Value; got != "20260101T080000Z" {
		t.Errorf("CREATED %q", got)
	}
	if got := ev.Props.Get(ical.PropURL).Value; got != "https://cal.example.com/?event=u-1" {
		t.Errorf("URL %q", got)
	}
	if ev.Props.Get(ical.PropDescription) != nil {
		t.Error("empty description should be omitted")
	}
	for _, forbidden := range []string{ical.PropAttach, ical.PropComment, ical.PropAttendee} {
		if ev.Props.Get(forbidden) != nil {
			t.Errorf("feed must not contain %s", forbidden)
		}
	}
}

func TestEscapingRoundTrip(t *testing.T) {
	desc := "semi;colon, comma \\ backslash\nnew line\r\nwindows\rold mac\x00nul\ttab"
	want := "semi;colon, comma \\ backslash\nnew line\nwindows\nold mac" + "nul\ttab"
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	raw, cal := writeAndParse(t, Calendar{Events: []Event{{
		UID: "u", Title: "T", Description: desc, Start: start, End: start,
	}}})
	if !strings.Contains(raw, `semi\;colon\, comma \\ backslash\nnew line`) {
		t.Errorf("escaping wrong:\n%s", raw)
	}
	if got := text(t, cal.Events()[0].Component, ical.PropDescription); got != want {
		t.Errorf("round trip:\n got %q\nwant %q", got, want)
	}
}

func TestNoContentLineInjection(t *testing.T) {
	evil := "x\r\nEND:VEVENT\r\nBEGIN:VEVENT\r\nUID:evil"
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	_, cal := writeAndParse(t, Calendar{Events: []Event{{
		UID: "u\r\nX-EVIL:1", Title: evil, Start: start, End: start, RRule: "FREQ=DAILY\r\nX-EVIL:1",
	}}})
	evs := cal.Events()
	if len(evs) != 1 {
		t.Fatalf("injection produced %d events", len(evs))
	}
	if evs[0].Props.Get("X-EVIL") != nil {
		t.Error("raw value injected a property")
	}
}

func TestFoldingLongEmoji(t *testing.T) {
	desc := strings.Repeat("😀", 500) + strings.Repeat("é", 101) + "日本語"
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	raw, cal := writeAndParse(t, Calendar{Events: []Event{{UID: "u", Title: desc, Description: desc, Start: start, End: start}}})
	if !strings.Contains(raw, "\r\n ") {
		t.Fatal("expected folded lines")
	}
	if got := text(t, cal.Events()[0].Component, ical.PropDescription); got != desc {
		t.Errorf("folded description did not round-trip (len %d vs %d)", len(got), len(desc))
	}
}

func TestAllDayMultiDay(t *testing.T) {
	start := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC) // exclusive: 10th-12th
	raw, cal := writeAndParse(t, Calendar{Events: []Event{
		{UID: "a", Title: "Camp", Start: start, End: end, AllDay: true, TimeZone: "Europe/Berlin"},
		{UID: "b", Title: "Zero length", Start: start, End: start, AllDay: true},
	}})
	if !strings.Contains(raw, "DTSTART;VALUE=DATE:20260710\r\n") || !strings.Contains(raw, "DTEND;VALUE=DATE:20260713\r\n") {
		t.Errorf("all-day dates wrong:\n%s", raw)
	}
	if !strings.Contains(raw, "DTEND;VALUE=DATE:20260711\r\n") {
		t.Errorf("zero-length all-day must end next day:\n%s", raw)
	}
	ev := cal.Events()[0]
	s, err := ev.DateTimeStart(nil)
	if err != nil || !s.Equal(start) {
		t.Errorf("start %v %v", s, err)
	}
	if strings.Contains(raw, "TZID=") {
		t.Error("all-day events must not use TZID")
	}
}

func TestRecurringOverrideAndCancelled(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	start := time.Date(2026, 10, 2, 19, 0, 0, 0, ny).UTC()
	week := 7 * 24 * time.Hour
	newTitle, newLoc := "Moved pizza", "Kitchen"
	movedStart := start.Add(2*week + time.Hour)
	movedEnd := movedStart.Add(30 * time.Minute)
	exdate := start.Add(4 * week)
	raw, cal := writeAndParse(t, Calendar{Events: []Event{{
		UID: "rec-1", Title: "Pizza", Location: "Office", Start: start, End: start.Add(time.Hour),
		TimeZone: "America/New_York", RRule: "FREQ=WEEKLY;BYDAY=FR;COUNT=10", Sequence: 3,
		ExDates: []time.Time{exdate},
		Overrides: []Override{
			{RecurrenceID: start.Add(week), Cancelled: true},
			{RecurrenceID: start.Add(2 * week), Title: &newTitle, Location: &newLoc, Start: &movedStart, End: &movedEnd},
		},
	}}})
	evs := cal.Events()
	if len(evs) != 2 {
		t.Fatalf("want master + 1 override, got %d\n%s", len(evs), raw)
	}
	master, ov := evs[0], evs[1]
	if master.Props.Get(ical.PropRecurrenceID) != nil {
		t.Fatal("first VEVENT should be the master")
	}
	if got := master.Props.Get(ical.PropRecurrenceRule).Value; got != "FREQ=WEEKLY;BYDAY=FR;COUNT=10" {
		t.Errorf("rrule %q", got)
	}
	var exVals []string
	for _, ex := range master.Props.Values(ical.PropExceptionDates) {
		if ex.Params.Get("TZID") != "America/New_York" {
			t.Errorf("EXDATE without TZID: %+v", ex)
		}
		exVals = append(exVals, ex.Value)
	}
	if strings.Join(exVals, ",") != "20261009T190000,20261030T190000" {
		t.Errorf("EXDATE = %v", exVals)
	}
	if uid := text(t, ov.Component, ical.PropUID); uid != "rec-1" {
		t.Errorf("override UID %q", uid)
	}
	rid := ov.Props.Get(ical.PropRecurrenceID)
	if rid.Params.Get("TZID") != "America/New_York" || rid.Value != "20261016T190000" {
		t.Errorf("RECURRENCE-ID = %+v", rid)
	}
	if got := ov.Props.Get(ical.PropDateTimeStart).Value; got != "20261016T200000" {
		t.Errorf("override DTSTART %q", got)
	}
	if text(t, ov.Component, ical.PropSummary) != newTitle || text(t, ov.Component, ical.PropLocation) != newLoc {
		t.Error("override fields not applied")
	}
	if ov.Props.Get(ical.PropRecurrenceRule) != nil || ov.Props.Get(ical.PropExceptionDates) != nil {
		t.Error("override must not carry RRULE/EXDATE")
	}
	if got := ov.Props.Get(ical.PropSequence).Value; got != "3" {
		t.Errorf("override sequence %q", got)
	}

	// Expand with go-ical (rrule-go): 10 occurrences minus 2 EXDATEs.
	set, err := master.RecurrenceSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(set.All()); n != 8 {
		t.Errorf("expanded %d occurrences, want 8", n)
	}
}

func TestAllDayRecurringOverrideUsesDate(t *testing.T) {
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	title := "Special"
	raw, _ := writeAndParse(t, Calendar{Events: []Event{{
		UID: "ad", Title: "Daily", Start: start, End: start.Add(24 * time.Hour), AllDay: true,
		TimeZone: "Europe/Paris", RRule: "FREQ=DAILY",
		Overrides: []Override{
			{RecurrenceID: start.Add(24 * time.Hour), Cancelled: true},
			{RecurrenceID: start.Add(48 * time.Hour), Title: &title},
		},
	}}})
	for _, want := range []string{"EXDATE;VALUE=DATE:20260106\r\n", "RECURRENCE-ID;VALUE=DATE:20260107\r\n"} {
		if !strings.Contains(raw, want) {
			t.Errorf("missing %q\n%s", want, raw)
		}
	}
}

func TestDSTWeeklyKeepsLocalTime(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	start := time.Date(2026, 10, 2, 19, 0, 0, 0, ny).UTC() // EDT
	_, cal := writeAndParse(t, Calendar{Events: []Event{{
		UID: "dst", Title: "Friday", Start: start, End: start.Add(time.Hour),
		TimeZone: "America/New_York", RRule: "FREQ=WEEKLY;BYDAY=FR;COUNT=8",
	}}})
	tzs := children(cal.Component, ical.CompTimezone)
	if len(tzs) != 1 || text(t, tzs[0], ical.PropTimezoneID) != "America/New_York" {
		t.Fatalf("expected one America/New_York VTIMEZONE, got %d", len(tzs))
	}
	if len(children(tzs[0], ical.CompTimezoneDaylight)) == 0 || len(children(tzs[0], ical.CompTimezoneStandard)) == 0 {
		t.Error("VTIMEZONE needs DAYLIGHT and STANDARD observances")
	}
	master := cal.Events()[0]
	set, err := master.RecurrenceSet(ny)
	if err != nil {
		t.Fatal(err)
	}
	occ := set.All()
	if len(occ) != 8 {
		t.Fatalf("got %d occurrences", len(occ))
	}
	sawEST := false
	for _, o := range occ {
		l := o.In(ny)
		if l.Hour() != 19 || l.Minute() != 0 {
			t.Errorf("occurrence %v is not 19:00 local", l)
		}
		if _, off := l.Zone(); off == -5*3600 {
			sawEST = true
		}
	}
	if !sawEST {
		t.Error("series should cross into EST")
	}
	// The emitted VTIMEZONE itself must give the right offsets, too.
	checkVTimezone(t, tzs[0], ny, occ[0].AddDate(-1, 0, 0), occ[0].AddDate(30, 0, 0))
}

// TestVTimezoneMatchesTZDB evaluates the generated observances (DTSTART,
// RDATE and RRULE onsets) and compares the resulting offset with Go's tz
// database across a range of zones with unusual histories.
func TestVTimezoneMatchesTZDB(t *testing.T) {
	zones := []string{
		"America/New_York", "Europe/Berlin", "Australia/Sydney", "Asia/Kolkata",
		"Europe/Moscow", "America/Sao_Paulo", "Pacific/Chatham", "Asia/Tehran",
		"Africa/Casablanca", "America/St_Johns", "Europe/Dublin", "Pacific/Apia",
	}
	from := time.Date(2008, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range zones {
		t.Run(name, func(t *testing.T) {
			loc := mustLoc(t, name)
			zs := newZoneSet(to)
			zs.use(name, true, []time.Time{from, to})
			comps := zs.components()
			if len(comps) != 1 {
				t.Fatal("no VTIMEZONE")
			}
			checkVTimezone(t, comps[0], loc, from, to.AddDate(15, 0, 0))
		})
	}
}

type onset struct {
	at  time.Time
	off int
}

func checkVTimezone(t *testing.T, tz *ical.Component, loc *time.Location, from, to time.Time) {
	t.Helper()
	var onsets []onset
	for _, obs := range tz.Children {
		fromOff := parseOffset(t, obs.Props.Get(ical.PropTimezoneOffsetFrom).Value)
		toOff := parseOffset(t, obs.Props.Get(ical.PropTimezoneOffsetTo).Value)
		dt := parseLocal(t, obs.Props.Get(ical.PropDateTimeStart).Value)
		walls := []time.Time{dt}
		for _, rd := range obs.Props.Values(ical.PropRecurrenceDates) {
			for _, v := range strings.Split(rd.Value, ",") {
				walls = append(walls, parseLocal(t, v))
			}
		}
		if rr := obs.Props.Get(ical.PropRecurrenceRule); rr != nil {
			opt, err := rrule.StrToROption(rr.Value)
			if err != nil {
				t.Fatal(err)
			}
			opt.Dtstart = dt
			r, err := rrule.NewRRule(*opt)
			if err != nil {
				t.Fatal(err)
			}
			walls = append(walls, r.Between(dt, to.AddDate(1, 0, 0), true)...)
		}
		for _, w := range walls {
			onsets = append(onsets, onset{at: w.Add(-time.Duration(fromOff) * time.Second), off: toOff})
		}
	}
	for at := from; at.Before(to); at = at.Add(97 * time.Hour) {
		best := onset{}
		for _, o := range onsets {
			if !o.at.After(at) && (best.at.IsZero() || o.at.After(best.at)) {
				best = o
			}
		}
		if best.at.IsZero() {
			t.Fatalf("no observance covers %v", at)
		}
		if _, want := at.In(loc).Zone(); best.off != want {
			t.Fatalf("at %v: VTIMEZONE offset %d, tzdb %d", at, best.off, want)
		}
	}
}

func parseLocal(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(localLayout, s)
	if err != nil {
		t.Fatalf("bad local time %q: %v", s, err)
	}
	return v
}

func parseOffset(t *testing.T, s string) int {
	t.Helper()
	if len(s) != 5 && len(s) != 7 {
		t.Fatalf("bad offset %q", s)
	}
	h, _ := strconv.Atoi(s[1:3])
	m, _ := strconv.Atoi(s[3:5])
	sec := 0
	if len(s) == 7 {
		sec, _ = strconv.Atoi(s[5:7])
	}
	v := h*3600 + m*60 + sec
	if s[0] == '-' {
		v = -v
	}
	return v
}

func TestTimeZoneFallbackToUTC(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	raw, _ := writeAndParse(t, Calendar{Events: []Event{
		{UID: "a", Title: "x", Start: start, End: start, TimeZone: "Not/AZone"},
		{UID: "b", Title: "y", Start: start, End: start, TimeZone: "UTC"},
		{UID: "c", Title: "z", Start: start, End: start, TimeZone: "Local"},
	}})
	if strings.Contains(raw, "TZID") {
		t.Errorf("unknown/UTC zones must fall back to UTC:\n%s", raw)
	}
}

func TestSharedTimezoneEmittedOnce(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	_, cal := writeAndParse(t, Calendar{Events: []Event{
		{UID: "a", Title: "x", Start: start, End: start, TimeZone: "Europe/Berlin"},
		{UID: "b", Title: "y", Start: start.AddDate(3, 0, 0), End: start.AddDate(3, 0, 0), TimeZone: "Europe/Berlin"},
		{UID: "c", Title: "z", Start: start, End: start, TimeZone: "Asia/Tokyo"},
	}})
	tzs := children(cal.Component, ical.CompTimezone)
	if len(tzs) != 2 {
		t.Fatalf("want 2 VTIMEZONEs, got %d", len(tzs))
	}
	if text(t, tzs[0], ical.PropTimezoneID) != "Asia/Tokyo" || text(t, tzs[1], ical.PropTimezoneID) != "Europe/Berlin" {
		t.Error("VTIMEZONEs not sorted/deduplicated")
	}
}

func TestEmptyCalendar(t *testing.T) {
	_, cal := writeAndParse(t, Calendar{Name: "Empty"})
	if len(cal.Events()) != 0 {
		t.Error("expected no events")
	}
}

func TestMissingUID(t *testing.T) {
	if err := Write(&bytes.Buffer{}, Calendar{Events: []Event{{Title: "x"}}}); err == nil {
		t.Error("expected error for missing UID")
	}
}

func TestFoldingWriterBoundaries(t *testing.T) {
	for n := 70; n <= 160; n++ {
		line := "X:" + strings.Repeat("€", n) // 3-byte runes hit every alignment
		var buf bytes.Buffer
		fw := &foldingWriter{w: &buf}
		fw.Write([]byte(line + "\r\n"))
		if err := fw.flush(); err != nil {
			t.Fatal(err)
		}
		checkLines(t, buf.String())
		unfolded := strings.ReplaceAll(strings.TrimSuffix(buf.String(), "\r\n"), "\r\n ", "")
		if unfolded != line {
			t.Fatalf("n=%d: unfold mismatch", n)
		}
	}
}
