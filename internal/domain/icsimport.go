package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"

	"github.com/felixtrav/sharedcal/internal/authz"
	"github.com/felixtrav/sharedcal/internal/store"
)

// maxICSEvents caps one import so a preview stays readable and the single
// transaction stays short.
const maxICSEvents = 500

// ICS import entry outcomes.
const (
	ImportNew       = "new"
	ImportDuplicate = "duplicate"
	ImportInvalid   = "invalid"
)

// ICSImportInput is an iCalendar file and what to turn its events into.
type ICSImportInput struct {
	ICS         string `json:"ics" maxLength:"2000000" doc:"The .ics file contents"`
	EventTypeID string `json:"eventTypeId,omitempty" maxLength:"36" doc:"Type for every imported event; empty imports generic events"`
	DryRun      bool   `json:"dryRun,omitempty" doc:"Validate and report without creating anything"`
}

// ICSEntryDTO is one VEVENT of the file and what became of it.
type ICSEntryDTO struct {
	UID     string `json:"uid"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end"`
	AllDay  bool   `json:"allDay"`
	RRule   string `json:"rrule"`
	Outcome string `json:"outcome" enum:"new,duplicate,invalid"`
	Error   string `json:"error,omitempty"`
}

// ICSImportResult reports an import or its dry run.
type ICSImportResult struct {
	DryRun     bool          `json:"dryRun"`
	Created    int           `json:"created" doc:"Events created, or that would be created on a dry run"`
	Duplicates int           `json:"duplicates" doc:"Skipped: an event from the same UID was imported before"`
	Invalid    int           `json:"invalid" doc:"Skipped: the event can't be created as an event of the chosen type"`
	Ignored    int           `json:"ignored" doc:"Cancelled events and single-occurrence overrides, which are not imported"`
	Entries    []ICSEntryDTO `json:"entries"`
}

type icsEntry struct {
	uid string
	in  EventInput
}

// parseICS turns the VEVENTs of an iCalendar file into event inputs. Entries
// that can't be read come back with their error in dto.
func parseICS(raw string) (entries []icsEntry, dtos []ICSEntryDTO, ignored int, err error) {
	cal, derr := ical.NewDecoder(strings.NewReader(raw)).Decode()
	if derr != nil {
		return nil, nil, 0, invalidField("ics", "this is not a valid iCalendar file")
	}
	for _, ev := range cal.Events() {
		if ev.Props.Get(ical.PropRecurrenceID) != nil {
			ignored++
			continue
		}
		if st, _ := ev.Props.Text(ical.PropStatus); strings.EqualFold(st, "CANCELLED") {
			ignored++
			continue
		}
		if len(dtos) >= maxICSEvents {
			return nil, nil, 0, invalidField("ics", "a file can hold at most %d events", maxICSEvents)
		}
		in, uid, dto, perr := icsEvent(&ev)
		if perr != nil {
			dto.Outcome, dto.Error = ImportInvalid, perr.Error()
			dtos = append(dtos, dto)
			entries = append(entries, icsEntry{})
			continue
		}
		dto.UID = uid
		dtos = append(dtos, dto)
		entries = append(entries, icsEntry{uid: uid, in: in})
	}
	return entries, dtos, ignored, nil
}

// icsEvent converts one VEVENT. The DTO is filled as far as the event could be read.
func icsEvent(ev *ical.Event) (EventInput, string, ICSEntryDTO, error) {
	var dto ICSEntryDTO
	title, _ := ev.Props.Text(ical.PropSummary)
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	dto.Title = title
	startProp := ev.Props.Get(ical.PropDateTimeStart)
	if startProp == nil {
		return EventInput{}, "", dto, errors.New("the event has no start")
	}
	start, err := startProp.DateTime(time.UTC)
	if err != nil {
		return EventInput{}, "", dto, errors.New("the start can't be read (unknown time zone?)")
	}
	allDay := startProp.ValueType() == ical.ValueDate || (startProp.ValueType() == ical.ValueDefault && len(startProp.Value) == 8)
	tz := "UTC"
	if name := startProp.Params.Get(ical.PropTimezoneID); name != "" {
		if _, lerr := time.LoadLocation(name); lerr == nil {
			tz = name
		}
	}
	end := start
	switch {
	case ev.Props.Get(ical.PropDateTimeEnd) != nil:
		if end, err = ev.Props.Get(ical.PropDateTimeEnd).DateTime(time.UTC); err != nil {
			return EventInput{}, "", dto, errors.New("the end can't be read")
		}
	case ev.Props.Get(ical.PropDuration) != nil:
		d, derr := ev.Props.Get(ical.PropDuration).Duration()
		if derr != nil {
			return EventInput{}, "", dto, errors.New("the duration can't be read")
		}
		end = start.Add(d)
	case allDay:
		end = start.AddDate(0, 0, 1)
	}
	if !end.After(start) && allDay {
		end = start.AddDate(0, 0, 1)
	}
	in := EventInput{Title: &title, TimeZone: &tz, AllDay: &allDay}
	if allDay {
		in.Start, in.End = ptr(start.Format(dateLayout)), ptr(end.Format(dateLayout))
	} else {
		in.Start, in.End = ptr(start.UTC().Format(time.RFC3339)), ptr(end.UTC().Format(time.RFC3339))
	}
	dto.Start, dto.End, dto.AllDay = *in.Start, *in.End, allDay
	if rp := ev.Props.Get(ical.PropRecurrenceRule); rp != nil {
		in.RRule = ptr(rp.Value)
		dto.RRule = rp.Value
	}
	if loc, _ := ev.Props.Text(ical.PropLocation); strings.TrimSpace(loc) != "" {
		in.Location = ptr(strings.TrimSpace(loc))
	}
	if st, _ := ev.Props.Text(ical.PropStatus); strings.EqualFold(st, "TENTATIVE") {
		in.Status = ptr(store.StatusTentative)
	}
	uid, _ := ev.Props.Text(ical.PropUID)
	uid = strings.TrimSpace(uid)
	if uid == "" {
		// No UID: derive a stable one so a re-import still recognises the event.
		sum := sha256.Sum256([]byte(title + "|" + *in.Start + "|" + dto.RRule))
		uid = "noid:" + hex.EncodeToString(sum[:8])
	}
	if len(uid) > 255 {
		sum := sha256.Sum256([]byte(uid))
		uid = "long:" + hex.EncodeToString(sum[:16])
	}
	return in, uid, dto, nil
}

// errDryRun rolls the import transaction back after a successful dry run.
var errDryRun = errors.New("dry run")

// ImportICS turns the events of an iCalendar file into events of one type.
// Events whose UID was imported before are skipped, so a file can be imported
// again safely. Events that don't fit the type are reported and skipped; the
// rest are created in one transaction.
func (s *Service) ImportICS(ctx context.Context, actor authz.Actor, in ICSImportInput) (*ICSImportResult, error) {
	if !authz.CanAdminister(actor) {
		return nil, forbidden("only admins can import events")
	}
	entries, dtos, ignored, err := parseICS(in.ICS)
	if err != nil {
		return nil, err
	}
	if len(dtos) == 0 {
		return nil, invalidField("ics", "the file contains no events to import")
	}
	res := &ICSImportResult{DryRun: in.DryRun, Ignored: ignored, Entries: dtos}
	err = s.write(ctx, actor, func(tx *Tx) error {
		res.Created, res.Duplicates, res.Invalid = 0, 0, 0
		if in.EventTypeID != "" {
			if _, err := take[store.EventType](tx.db, in.EventTypeID, "event type"); err != nil {
				return invalidField("eventTypeId", "unknown event type")
			}
		}
		var uids []string
		for _, e := range entries {
			if e.uid != "" {
				uids = append(uids, e.uid)
			}
		}
		seen := map[string]bool{}
		if len(uids) > 0 {
			var have []string
			if err := tx.db.Model(&store.Event{}).Where("source_uid IN ? AND deleted_at IS NULL", uids).Pluck("source_uid", &have).Error; err != nil {
				return err
			}
			for _, u := range have {
				seen[u] = true
			}
		}
		for i, e := range entries {
			d := &res.Entries[i]
			if d.Outcome == ImportInvalid {
				res.Invalid++
				continue
			}
			if seen[e.uid] {
				d.Outcome = ImportDuplicate
				res.Duplicates++
				continue
			}
			ev := e.in
			if in.EventTypeID != "" {
				ev.EventTypeID = ptr(in.EventTypeID)
			}
			if _, err := s.createEventTx(tx, actor, ev, e.uid); err != nil {
				var de *Error
				if errors.As(err, &de) && (de.Kind == KindInvalid || de.Kind == KindForbidden) {
					d.Outcome, d.Error = ImportInvalid, de.Msg
					res.Invalid++
					continue
				}
				return err
			}
			seen[e.uid] = true
			d.Outcome = ImportNew
			res.Created++
		}
		if in.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return nil, err
	}
	return res, nil
}
