import { describe, expect, it } from 'vitest'
import { makeEvent } from '@/test/fixtures'
import { changedFields, eventFormSchema, formFromDraft, formFromEvent, formToPayload, needsSeriesScope } from './formModel'

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
})
