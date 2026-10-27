package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/felixtrav/sharedcal/internal/store"
)

func TestParseRRuleRefusals(t *testing.T) {
	for _, rule := range []string{
		"FREQ=HOURLY", "FREQ=MINUTELY", "FREQ=SECONDLY",
		"FREQ=DAILY;BYHOUR=1,2,3", "FREQ=DAILY;BYMINUTE=0", "FREQ=DAILY;BYSECOND=0", "FREQ=YEARLY;BYEASTER=0",
		"FREQ=DAILY;COUNT=99999", "FREQ=DAILY;INTERVAL=5000", "FREQ=DAILY;COUNT=3;UNTIL=20270101T000000Z",
		"DTSTART:20260101T000000Z\nRRULE:FREQ=DAILY", "FREQ=DAILY;X-FOO=1", "INTERVAL=2", strings.Repeat("A", 600),
	} {
		if _, err := parseRRule(rule); err == nil {
			t.Errorf("%q accepted", rule)
		}
	}
	for _, rule := range []string{"FREQ=WEEKLY;BYDAY=FR", "RRULE:FREQ=MONTHLY;BYDAY=2TU", "freq=yearly", "FREQ=DAILY;UNTIL=20270101T000000Z"} {
		if _, err := parseRRule(rule); err != nil {
			t.Errorf("%q refused: %v", rule, err)
		}
	}
}

func TestNormalizeRRule(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := normalizeRRule("FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=30", start, time.UTC); err == nil {
		t.Error("Feb 30 accepted")
	}
	if _, err := normalizeRRule("FREQ=DAILY;UNTIL=20250101T000000Z", start, time.UTC); err == nil {
		t.Error("UNTIL before start accepted")
	}
	if _, err := normalizeRRule("FREQ=DAILY;UNTIL=24000101T000000Z", start, time.UTC); err == nil {
		t.Error("UNTIL centuries away accepted")
	}
	got, err := normalizeRRule("freq=weekly;byday=fr", start, time.UTC)
	if err != nil || got != "FREQ=WEEKLY;BYDAY=FR" {
		t.Errorf("normalized %q, %v", got, err)
	}
}

func weekly(start time.Time, rule string) *store.Event {
	return &store.Event{ID: "e", StartAt: start, EndAt: start.Add(2 * time.Hour), TimeZone: "America/New_York", RRule: rule, ExdatesJSON: "[]"}
}

func TestExpandOverridesAndExdates(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	start := time.Date(2026, 10, 23, 19, 0, 0, 0, ny).UTC()
	e := weekly(start, "FREQ=WEEKLY;BYDAY=FR")
	occs, err := expand(e, nil, start, start.AddDate(0, 0, 28))
	if err != nil || len(occs) != 4 {
		t.Fatalf("plain: %d %v", len(occs), err)
	}
	for _, o := range occs {
		if o.Start.In(ny).Hour() != 19 {
			t.Errorf("DST drift: %v", o.Start.In(ny))
		}
	}
	// Exclude the second, move the third out of the range and pull a
	// later one into it.
	second, third, fifth := *occs[1].RecurrenceID, *occs[2].RecurrenceID, start.AddDate(0, 0, 28)
	fifth = time.Date(fifth.In(ny).Year(), fifth.In(ny).Month(), fifth.In(ny).Day(), 19, 0, 0, 0, ny).UTC()
	setExdates(e, []time.Time{second})
	far := third.AddDate(1, 0, 0)
	near := start.Add(time.Hour)
	ovs := []store.EventOverride{
		{RecurrenceID: third, StartAt: &far, EndAt: ptr(far.Add(time.Hour))},
		{RecurrenceID: fifth, StartAt: &near, EndAt: ptr(near.Add(time.Hour)), Title: ptr("moved in")},
	}
	occs, err = expand(e, ovs, start, start.AddDate(0, 0, 28))
	if err != nil {
		t.Fatal(err)
	}
	if len(occs) != 3 {
		t.Fatalf("got %d occurrences: %+v", len(occs), occs)
	}
	var sawMovedIn bool
	for _, o := range occs {
		if o.RecurrenceID.Equal(second) || o.RecurrenceID.Equal(third) {
			t.Errorf("excluded/moved occurrence present: %v", o.RecurrenceID)
		}
		if o.Override != nil && o.RecurrenceID.Equal(fifth) {
			sawMovedIn = true
		}
	}
	if !sawMovedIn {
		t.Error("override moved into the range is missing")
	}
	// Cancelled override.
	occs, _ = expand(e, []store.EventOverride{{RecurrenceID: *occs[0].RecurrenceID, Cancelled: true}}, start, start.AddDate(0, 0, 28))
	for _, o := range occs {
		if o.RecurrenceID.Equal(start) {
			t.Error("cancelled occurrence present")
		}
	}
}

func TestSplitRuleAndSeriesEnd(t *testing.T) {
	start := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	e := &store.Event{StartAt: start, EndAt: start.Add(time.Hour), TimeZone: "UTC", RRule: "FREQ=DAILY;COUNT=10", ExdatesJSON: "[]"}
	end, err := seriesEnd(e)
	if err != nil || !end.Equal(start.AddDate(0, 0, 9).Add(time.Hour)) {
		t.Fatalf("seriesEnd count: %v %v", end, err)
	}
	at := start.AddDate(0, 0, 4)
	head, tail, err := splitRule(e, at)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(head, "UNTIL=20260109T085959Z") || strings.Contains(head, "COUNT") {
		t.Errorf("head: %s", head)
	}
	if !strings.Contains(tail, "COUNT=6") {
		t.Errorf("tail: %s", tail)
	}
	e2 := &store.Event{StartAt: start, EndAt: start.Add(time.Hour), TimeZone: "UTC", RRule: "FREQ=DAILY;UNTIL=20260131T090000Z", ExdatesJSON: "[]"}
	_, tail, _ = splitRule(e2, at)
	if !strings.Contains(tail, "UNTIL=20260131T090000Z") {
		t.Errorf("tail keeps UNTIL: %s", tail)
	}
	if end, _ := seriesEnd(e2); !end.Equal(time.Date(2026, 1, 31, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("seriesEnd until: %v", end)
	}
	if end, _ := seriesEnd(&store.Event{StartAt: start, EndAt: start, RRule: "FREQ=DAILY", ExdatesJSON: "[]"}); end != nil {
		t.Error("unbounded series has an end")
	}
	if _, _, err := splitRule(e, start.AddDate(0, 0, 20)); err == nil {
		t.Error("split after the last COUNT occurrence accepted")
	}
}

func TestShiftRuleDays(t *testing.T) {
	cases := map[string]string{
		"FREQ=WEEKLY;BYDAY=FR":       "FREQ=WEEKLY;BYDAY=SA",
		"FREQ=WEEKLY;BYDAY=SU,MO":    "FREQ=WEEKLY;BYDAY=MO,TU",
		"FREQ=MONTHLY;BYDAY=2FR":     "FREQ=MONTHLY;BYDAY=+2SA",
		"FREQ=MONTHLY;BYMONTHDAY=31": "FREQ=MONTHLY;BYMONTHDAY=1",
	}
	for in, want := range cases {
		got, err := shiftRuleDays(in, 1)
		if err != nil || got != want {
			t.Errorf("%s +1d = %s (%v), want %s", in, got, err, want)
		}
	}
	if got, _ := shiftRuleDays("FREQ=WEEKLY;BYDAY=MO", -1); got != "FREQ=WEEKLY;BYDAY=SU" {
		t.Errorf("-1d: %s", got)
	}
}

func TestParseEventTime(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	cases := []struct {
		in     string
		allDay bool
		want   time.Time
		ok     bool
	}{
		{"2026-10-30T19:00:00", false, time.Date(2026, 10, 30, 23, 0, 0, 0, time.UTC), true},
		{"2026-10-30T19:00", false, time.Date(2026, 10, 30, 23, 0, 0, 0, time.UTC), true},
		{"2026-10-30T23:00:00Z", false, time.Date(2026, 10, 30, 23, 0, 0, 0, time.UTC), true},
		{"2026-10-30T19:00:00-04:00", false, time.Date(2026, 10, 30, 23, 0, 0, 0, time.UTC), true},
		{"2026-10-30", true, time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), true},
		{"2026-10-30T00:00:00Z", true, time.Time{}, false},
		{"tomorrow", false, time.Time{}, false},
	}
	for _, c := range cases {
		got, err := parseEventTime(c.in, ny, c.allDay)
		if (err == nil) != c.ok || (c.ok && !got.Equal(c.want)) {
			t.Errorf("%q: %v %v", c.in, got, err)
		}
	}
}

func TestLikeEscape(t *testing.T) {
	if got := likeEscape("50%_off!"); got != "%50!%!_off!!%" {
		t.Errorf("likeEscape: %s", got)
	}
}

func TestLoadLocationRejectsOddNames(t *testing.T) {
	for _, n := range []string{"", "Local", "../etc/passwd", "Europe/../../x", "a b"} {
		if _, err := loadLocation(n); err == nil {
			t.Errorf("%q accepted", n)
		}
	}
}
