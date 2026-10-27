import { describe, expect, it } from 'vitest'
import { buildRRule, detectPreset, nthOfMonth, parseDate, parseRRule, presetToRRule, weekdayOf } from './recurrence'

// Friday, October 30, 2026 — the 5th Friday of the month (so "last").
const fri30 = parseDate('2026-10-30')
// Thursday, October 15, 2026 — the 3rd Thursday.
const thu15 = parseDate('2026-10-15')

describe('date helpers', () => {
  it('computes weekday and nth', () => {
    expect(weekdayOf(fri30)).toBe('FR')
    expect(weekdayOf(thu15)).toBe('TH')
    expect(nthOfMonth(thu15)).toBe(3)
    expect(nthOfMonth(fri30)).toBe(-1)
  })
})

describe('presets → RRULE', () => {
  it.each([
    ['none', ''],
    ['daily', 'FREQ=DAILY'],
    ['weekdays', 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR'],
    ['weekly', 'FREQ=WEEKLY;BYDAY=TH'],
    ['monthly', 'FREQ=MONTHLY;BYDAY=3TH'],
    ['yearly', 'FREQ=YEARLY'],
  ] as const)('%s', (preset, rrule) => {
    expect(presetToRRule(preset, thu15)).toBe(rrule)
  })

  it('uses "last" for the 5th weekday', () => {
    expect(presetToRRule('monthly', fri30)).toBe('FREQ=MONTHLY;BYDAY=-1FR')
  })
})

describe('RRULE → preset', () => {
  it.each([
    ['', 'none'],
    ['FREQ=DAILY', 'daily'],
    ['FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR', 'weekdays'],
    ['FREQ=WEEKLY;BYDAY=FR,MO,TU,WE,TH', 'weekdays'],
    ['FREQ=WEEKLY;BYDAY=TH', 'weekly'],
    ['FREQ=WEEKLY', 'weekly'],
    ['FREQ=MONTHLY;BYDAY=3TH', 'monthly'],
    ['FREQ=YEARLY', 'yearly'],
    ['FREQ=WEEKLY;BYDAY=FR', 'custom'],
    ['FREQ=DAILY;INTERVAL=2', 'custom'],
    ['FREQ=DAILY;COUNT=5', 'custom'],
    ['FREQ=MONTHLY;BYMONTHDAY=15', 'custom'],
    ['FREQ=HOURLY', 'custom'],
  ] as const)('%s → %s', (rrule, preset) => {
    expect(detectPreset(rrule, thu15)).toBe(preset)
  })

  it('round-trips every preset', () => {
    for (const p of ['daily', 'weekdays', 'weekly', 'monthly', 'yearly'] as const) {
      expect(detectPreset(presetToRRule(p, fri30), fri30)).toBe(p)
    }
  })
})

describe('custom builder', () => {
  it('builds interval, days, count', () => {
    const rule = parseRRule('FREQ=WEEKLY', thu15)
    expect(rule).not.toBeNull()
    const r = { ...rule!, interval: 2, byDay: ['WE', 'MO'] as const, end: { type: 'count', count: 10 } as const }
    expect(buildRRule({ ...r, byDay: [...r.byDay] })).toBe('FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;COUNT=10')
  })

  it('builds UNTIL as a UTC instant for timed and a date for all-day events', () => {
    const rule = { ...parseRRule('FREQ=DAILY', thu15)!, end: { type: 'until', until: '2026-12-31' } as const }
    expect(buildRRule(rule)).toBe('FREQ=DAILY;UNTIL=20261231T235959Z')
    expect(buildRRule(rule, true)).toBe('FREQ=DAILY;UNTIL=20261231')
  })

  it('parses UNTIL back to a date', () => {
    expect(parseRRule('FREQ=DAILY;UNTIL=20261231T235959Z', thu15)?.end).toEqual({ type: 'until', until: '2026-12-31' })
  })

  it('parses monthly by day of month', () => {
    const r = parseRRule('FREQ=MONTHLY;BYMONTHDAY=15', thu15)
    expect(r?.monthlyMode).toBe('day')
    expect(r?.monthDay).toBe(15)
    expect(buildRRule(r!)).toBe('FREQ=MONTHLY;BYMONTHDAY=15')
  })

  it('rejects what it cannot represent', () => {
    expect(parseRRule('FREQ=WEEKLY;BYHOUR=9', thu15)).toBeNull()
    expect(parseRRule('FREQ=DAILY;COUNT=2;UNTIL=20261231', thu15)).toBeNull()
    expect(parseRRule('FREQ=SECONDLY', thu15)).toBeNull()
  })
})
