import { describe, expect, it } from 'vitest'
import { viewerTime } from './fieldTime'

const at = (timeZone: string, clock24h: boolean) => ({ date: '2026-11-05', eventTz: 'America/New_York', fmt: { timeZone, clock24h } })

describe('viewerTime', () => {
  it('shows a time from the event zone in the viewer zone and clock', () => {
    expect(viewerTime('19:30', at('America/New_York', true))).toBe('19:30')
    expect(viewerTime('19:30', at('America/New_York', false))).toMatch(/^7:30\sPM$/)
    // Berlin is six hours ahead of New York in early November, Tokyo fourteen.
    expect(viewerTime('19:30', at('Europe/Berlin', true))).toBe('01:30')
    expect(viewerTime('19:30', at('Asia/Tokyo', false))).toMatch(/^9:30\sAM$/)
  })

  it('follows daylight saving on the event date', () => {
    // On 2026-07-01 New York is on EDT, five hours behind Berlin's CEST.
    expect(viewerTime('19:30', { date: '2026-07-01', eventTz: 'America/New_York', fmt: { timeZone: 'Europe/Berlin', clock24h: true } })).toBe('01:30')
    // A different date, same zones: Berlin is six hours ahead in November, so 18:00 becomes midnight.
    expect(viewerTime('18:00', at('Europe/Berlin', true))).toBe('00:00')
  })

  it('leaves values that are not HH:MM alone', () => {
    expect(viewerTime('soon', at('Europe/Berlin', true))).toBe('soon')
  })
})
