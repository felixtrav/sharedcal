import { describe, expect, it } from 'vitest'
import { makeEvent } from '@/test/fixtures'
import type { EventType } from '@/api/types'
import { changedFields, eventFormSchema, fieldsToPayload, formFromDraft, formFromEvent, formToPayload, missingRequired, needsSeriesScope } from './formModel'
import { addMinutesToWall, fixedTiming, fixedValuesFor, typedDraft, typeDefaultsFor } from './typeDefaults'

describe('event form model', () => {
  it('round-trips a timed event', () => {
    const ev = makeEvent()
    const v = formFromEvent(ev)
    expect(v).toMatchObject({ startDate: '2026-10-30', startTime: '19:00', endDate: '2026-10-30', endTime: '23:00' })
    const p = formToPayload(v)
    expect(p.start).toBe('2026-10-30T19:00:00')
    expect(p.end).toBe('2026-10-30T23:00:00')
    expect(changedFields(p, formToPayload(formFromEvent(ev)))).toEqual({})
  })

  it('shows all-day end dates inclusively and sends them exclusively', () => {
    const v = formFromDraft({ title: 'Trip', allDay: true, start: '2026-10-30', end: '2026-11-02', timeZone: 'UTC' })
    expect(v.endDate).toBe('2026-11-01')
    expect(formToPayload(v)).toMatchObject({ start: '2026-10-30', end: '2026-11-02', allDay: true })
  })

  it('sends time fields together when one changes', () => {
    const before = formToPayload(formFromEvent(makeEvent()))
    const after = { ...before, end: '2026-10-30T23:30:00' }
    expect(changedFields(before, after)).toEqual({
      start: before.start,
      end: '2026-10-30T23:30:00',
      timeZone: before.timeZone,
      allDay: false,
    })
    expect(changedFields(before, { ...before, title: 'New' })).toEqual({ title: 'New' })
  })

  it('sends a strict diff for owner edits and knows which fields need the series', () => {
    const before = formToPayload(formFromEvent(makeEvent()))
    const timeOnly = changedFields(before, { ...before, end: '2026-10-30T23:30:00' }, false)
    expect(timeOnly).toEqual({ end: '2026-10-30T23:30:00' })
    expect(needsSeriesScope(timeOnly)).toBe(false)
    expect(needsSeriesScope(changedFields(before, { ...before, tags: ['t1'] }, false))).toBe(true)
  })

  it('validates like the server', () => {
    const ok = formFromEvent(makeEvent())
    expect(eventFormSchema.safeParse(ok).success).toBe(true)
    const issues = (v: Partial<typeof ok>) => eventFormSchema.safeParse({ ...ok, ...v }).error?.issues.map((i) => i.message) ?? []
    expect(issues({ title: '  ' })).toContain('validation.titleRequired')
    expect(issues({ title: 'x'.repeat(201) })).toContain('validation.titleTooLong')
    expect(issues({ endTime: '18:00' })).toContain('validation.endBeforeStart')
    expect(issues({ endDate: '2027-12-01' })).toContain('validation.tooLong')
    expect(issues({ timeZone: 'Mars/Olympus' })).toContain('validation.timeZoneInvalid')
    expect(issues({ location: 'x'.repeat(1001) })).toContain('validation.locationTooLong')
  })

  describe('typed events', () => {
    const birthday: EventType = {
      id: 'ty1',
      name: 'Birthday',
      icon: 'cake',
      colorSlot: 'tag-3',
      style: 'default',
      yearCounter: false,
      titleTemplate: '',
      instructions: '',
      singleDay: false,
      neverRepeats: false,
      adminOnly: false,
      defaultAllDay: true,
      defaultDurationMinutes: 60,
      defaultRrule: 'FREQ=YEARLY',
      sortOrder: 0,
      fields: [
        { key: 'person', label: 'Person', kind: 'text', required: true },
        { key: 'born', label: 'Year born', kind: 'number' },
        { key: 'party', label: 'Party', kind: 'checkbox' },
      ],
    }

    it('turns form inputs into typed values and drops empty ones', () => {
      expect(fieldsToPayload({ person: ' Sam ', born: '1990', party: false }, birthday)).toEqual({ person: 'Sam', born: 1990 })
      expect(fieldsToPayload({ person: '', born: ' ', party: true }, birthday)).toEqual({ party: true })
    })

    it('keeps multiple choices as a list in option order, and the first choice when single', () => {
      const t = { fields: [{ key: 'tags', label: 'Tags', kind: 'select', options: ['A', 'B', 'C'], multiple: true }] } as unknown as EventType
      expect(fieldsToPayload({ tags: ['C', 'A'] }, t)).toEqual({ tags: ['A', 'C'] })
      expect(fieldsToPayload({ tags: 'B' }, t)).toEqual({ tags: ['B'] })
      expect(fieldsToPayload({ tags: [] }, t)).toEqual({})
      const single = { fields: [{ key: 'tags', label: 'Tags', kind: 'select', options: ['A', 'B', 'C'] }] } as unknown as EventType
      expect(fieldsToPayload({ tags: ['C', 'A'] }, single)).toEqual({ tags: 'C' })

      const people = (multiple?: boolean) => ({ fields: [{ key: 'g', label: 'G', kind: 'members', multiple }] }) as unknown as EventType
      expect(fieldsToPayload({ g: ['x', 'y'] }, people())).toEqual({ g: ['x', 'y'] })
      expect(fieldsToPayload({ g: ['x', 'y'] }, people(false))).toEqual({ g: ['x'] })
      expect(fieldsToPayload({ person: 'x' }, undefined)).toEqual({})
    })

    it('keeps members as a list of user ids', () => {
      const withGuests: EventType = { ...birthday, fields: [...birthday.fields, { key: 'guests', label: 'Guests', kind: 'members' }] }
      expect(fieldsToPayload({ guests: ['u1', 'u2'] }, withGuests)).toEqual({ guests: ['u1', 'u2'] })
      expect(fieldsToPayload({ guests: [] }, withGuests)).toEqual({})
      expect(formFromEvent(makeEvent({ eventTypeId: 'ty1', fields: { guests: ['u1'] } })).fields).toEqual({ guests: ['u1'] })
    })

    it('reports missing required fields', () => {
      expect(missingRequired({ born: '1990' }, birthday).map((d) => d.key)).toEqual(['person'])
      expect(missingRequired({ person: 'Sam' }, birthday)).toEqual([])
    })

    it('sends fields instead of a description and round-trips from the event', () => {
      const ev = makeEvent({ eventTypeId: 'ty1', fields: { person: 'Sam', born: 1990, party: true } })
      const v = formFromEvent(ev)
      expect(v.fields).toEqual({ person: 'Sam', born: '1990', party: true })
      const p = formToPayload({ ...v, description: 'stale text' }, birthday)
      expect(p).toMatchObject({ eventTypeId: 'ty1', description: '', fields: { born: 1990, party: true, person: 'Sam' } })
      expect(changedFields(p, formToPayload(formFromEvent(ev), birthday))).toEqual({})
    })

    it('diffs a change of fields or type, which one occurrence can make on its own', () => {
      const ev = makeEvent({ eventTypeId: 'ty1', fields: { person: 'Sam' } })
      const before = formToPayload(formFromEvent(ev), birthday)
      const edited = changedFields(before, formToPayload({ ...formFromEvent(ev), fields: { person: 'Samantha' } }, birthday), false)
      expect(edited).toEqual({ fields: { person: 'Samantha' } })
      expect(needsSeriesScope(edited)).toBe(false)
      const generic = changedFields(before, formToPayload({ ...formFromEvent(ev), eventTypeId: '', fields: {} }), false)
      expect(generic).toMatchObject({ eventTypeId: '' })
    })

    it('accepts inputs the user never touched', () => {
      const v = { ...formFromEvent(makeEvent()), eventTypeId: 'ty1', fields: { person: undefined, born: '1990' } }
      expect(eventFormSchema.safeParse(v).success).toBe(true)
    })

    it('leaves generic events untouched', () => {
      const p = formToPayload(formFromEvent(makeEvent({ description: 'notes' })))
      expect(p).toMatchObject({ eventTypeId: '', description: 'notes', fields: {} })
    })
  })

  describe('type defaults', () => {
    const base: EventType = {
      id: 'ty1',
      name: 'Deadline',
      icon: 'flag',
      colorSlot: 'tag-6',
      style: 'outline',
      yearCounter: false,
      titleTemplate: '',
      instructions: '',
      singleDay: false,
      neverRepeats: false,
      adminOnly: false,
      defaultAllDay: false,
      defaultDurationMinutes: 0,
      defaultRrule: '',
      sortOrder: 0,
      fields: [],
    }
    const birthday: EventType = { ...base, id: 'ty2', defaultAllDay: true, singleDay: true, defaultRrule: 'FREQ=YEARLY', defaultDurationMinutes: 60 }
    const slot = { title: '', start: '2026-10-30T19:00:00', end: '2026-10-30T19:30:00', allDay: false, timeZone: 'UTC' }

    it('adds minutes to a wall time across midnight', () => {
      expect(addMinutesToWall('2026-10-30T23:30:00', 45)).toBe('2026-10-31T00:15:00')
      expect(addMinutesToWall('2026-10-30T19:00', 0)).toBe('2026-10-30T19:00:00')
    })

    it('turns a timed slot into an instant for a zero-length type', () => {
      expect(typedDraft(slot, base)).toMatchObject({ start: '2026-10-30T19:00:00', end: '2026-10-30T19:00:00', allDay: false, eventTypeId: 'ty1' })
    })

    it('turns a timed slot into a yearly all-day event for a birthday', () => {
      expect(typedDraft(slot, birthday)).toMatchObject({ start: '2026-10-30', end: '2026-10-31', allDay: true, rrule: 'FREQ=YEARLY', eventTypeId: 'ty2' })
    })

    it('reduces a multi-day drag to one date when the type is a single date', () => {
      const drag = { ...slot, start: '2026-10-30', end: '2026-11-03', allDay: true }
      expect(typedDraft(drag, birthday)).toMatchObject({ start: '2026-10-30', end: '2026-10-31', rrule: 'FREQ=YEARLY' })
      // The repeat is separate: a single date without one still holds, and a range type keeps the span.
      expect(typedDraft(drag, { ...birthday, defaultRrule: '', neverRepeats: true }).end).toBe('2026-10-31')
      expect(typedDraft(drag, { ...birthday, singleDay: false }).end).toBe('2026-11-03')
    })

    it('keeps a never-repeating type from repeating', () => {
      const release = { ...base, neverRepeats: true }
      expect(fixedTiming(release).noRepeat).toBe(true)
      expect(typedDraft(slot, release).rrule).toBe('')
      const v = { ...formFromDraft(slot), rrule: 'FREQ=WEEKLY' }
      expect(fixedValuesFor(v, release)).toEqual({ rrule: '' })
    })

    it('says what a type fixes and the values it forces', () => {
      expect(fixedTiming(birthday)).toEqual({ allDay: true, rrule: 'FREQ=YEARLY', noRepeat: false, singleDay: true })
      expect(fixedTiming({ ...birthday, singleDay: false }).singleDay).toBe(false)
      expect(fixedTiming({ ...birthday, defaultAllDay: false }).singleDay).toBe(false)
      expect(fixedTiming(undefined)).toEqual({ allDay: false, rrule: '', noRepeat: false, singleDay: false })
      const v = formFromDraft({ ...slot, start: '2026-10-30T19:00:00', end: '2026-11-02T19:00:00' })
      expect(fixedValuesFor(v, birthday)).toEqual({ allDay: true, rrule: 'FREQ=YEARLY', endDate: v.startDate })
    })

    it('keeps an all-day slot all-day and lengthens a timed type', () => {
      expect(typedDraft({ ...slot, start: '2026-10-30', end: '2026-10-31', allDay: true }, base)).toMatchObject({
        allDay: true,
        start: '2026-10-30',
        end: '2026-10-31',
      })
      expect(typedDraft(slot, { ...base, defaultDurationMinutes: 90 }).end).toBe('2026-10-30T20:30:00')
    })

    it('computes form changes without overwriting a repeat the user chose', () => {
      const v = formFromDraft(slot)
      expect(typeDefaultsFor(v, birthday)).toEqual({ allDay: true, rrule: 'FREQ=YEARLY' })
      expect(typeDefaultsFor({ ...v, rrule: 'FREQ=DAILY' }, birthday)).toEqual({ allDay: true })
      expect(typeDefaultsFor(v, base)).toEqual({ endDate: '2026-10-30', endTime: '19:00' })
    })
  })
})
