import { describe, expect, it } from 'vitest'
import { compareEvents } from './eventInput'

const ev = (title: string, start: string, style?: string, allDay = false) => ({
  title,
  allDay,
  start: new Date(start),
  end: new Date(new Date(start).getTime() + 3_600_000),
  extendedProps: { style },
})

describe('compareEvents', () => {
  it('puts banners first within a day, then orders by start and title', () => {
    const list = [
      ev('Lunch', '2026-10-30T12:00:00Z'),
      ev('Birthday', '2026-10-30T15:00:00Z', 'banner'),
      ev('Standup', '2026-10-30T09:00:00Z'),
      ev('Anniversary', '2026-10-30T18:00:00Z', 'banner'),
    ]
    expect([...list].sort(compareEvents).map((e) => e.title)).toEqual(['Birthday', 'Anniversary', 'Standup', 'Lunch'])
  })

  it('keeps FullCalendar’s default order for events without a banner', () => {
    const list = [ev('B', '2026-10-30T10:00:00Z'), ev('A', '2026-10-30T10:00:00Z'), ev('Early', '2026-10-30T08:00:00Z')]
    expect([...list].sort(compareEvents).map((e) => e.title)).toEqual(['Early', 'A', 'B'])
  })

  it('accepts timestamps as well as dates', () => {
    const a = { start: 2000, end: 3000, allDay: false, title: 'A', extendedProps: { style: 'banner' } }
    const b = { start: 1000, end: 2000, allDay: false, title: 'B', extendedProps: {} }
    expect(compareEvents(a, b)).toBeLessThan(0)
    expect(compareEvents({ start: null, title: 'x' }, { start: undefined, title: 'x' })).toBe(0)
  })
})
