import { describe, expect, it } from 'vitest'
import { addDays, dateInZone, fromWall, parseWallIso, toUtcString, toWall } from './time'
import { eventWhen } from './eventText'
import type { TFunction } from 'i18next'

const t = ((key: string, o?: Record<string, string>) => `${key}:${JSON.stringify(o ?? {})}`) as unknown as TFunction

describe('time helpers', () => {
  it('converts between instants and wall times', () => {
    const ms = Date.parse('2026-10-30T23:00:00Z')
    expect(toWall(ms, 'America/New_York')).toMatchObject({ year: 2026, month: 10, day: 30, hour: 19, minute: 0 })
    expect(fromWall(parseWallIso('2026-10-30T19:00'), 'America/New_York')).toBe(ms)
    expect(dateInZone(ms, 'Asia/Tokyo')).toBe('2026-10-31')
  })

  it('handles DST transitions', () => {
    // 2026-11-01 01:30 happens twice in New York; we pick the earlier (EDT) instant.
    expect(toUtcString(fromWall(parseWallIso('2026-11-01T01:30'), 'America/New_York'))).toBe('2026-11-01T05:30:00Z')
  })

  it('adds days across months', () => {
    expect(addDays('2026-10-31', 1)).toBe('2026-11-01')
    expect(addDays('2026-03-01', -1)).toBe('2026-02-28')
  })

  it('describes a same-day event like the spec example', () => {
    const s = eventWhen(
      { allDay: false, start: '', end: '', startUtc: '2026-10-30T23:00:00Z', endUtc: '2026-10-31T03:00:00Z' },
      { timeZone: 'America/New_York', clock24h: false, locale: 'en-US' },
      t,
    )
    expect(s).toBe('event.when.sameDay:{"day":"Friday","start":"7","end":"11 PM"}')
  })
})
