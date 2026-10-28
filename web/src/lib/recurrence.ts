/** Recurrence presets and a small RRULE subset (RFC 5545) used by the event form. */

export const WEEKDAYS = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU'] as const
export type Weekday = (typeof WEEKDAYS)[number]

export type Freq = 'DAILY' | 'WEEKLY' | 'MONTHLY' | 'YEARLY'

type RecurrenceEnd = { type: 'never' } | { type: 'count'; count: number } | { type: 'until'; until: string /* YYYY-MM-DD */ }

export type RecurrenceRule = {
  freq: Freq
  interval: number
  /** WEEKLY: days of the week. */
  byDay: Weekday[]
  /** MONTHLY: `day` = same day of month, `nth` = nth weekday (e.g. 3rd Friday, -1 = last). */
  monthlyMode: 'day' | 'nth'
  monthDay?: number
  nth?: number
  nthWeekday?: Weekday
  end: RecurrenceEnd
}

export type Preset = 'none' | 'daily' | 'weekdays' | 'weekly' | 'monthly' | 'yearly' | 'custom'

type DateParts = { year: number; month: number; day: number }

export function parseDate(s: string): DateParts {
  const [y, m, d] = s.slice(0, 10).split('-').map(Number)
  return { year: y ?? 1970, month: m ?? 1, day: d ?? 1 }
}

/** Monday = 0 … Sunday = 6. */
function weekdayIndex(p: DateParts) {
  const js = new Date(Date.UTC(p.year, p.month - 1, p.day)).getUTCDay()
  return (js + 6) % 7
}

export function weekdayOf(p: DateParts): Weekday {
  return WEEKDAYS[weekdayIndex(p)] as Weekday
}

/** Which occurrence of its weekday a date is within its month: 1..4, or -1 when it's the 5th (last). */
export function nthOfMonth(p: DateParts) {
  const n = Math.ceil(p.day / 7)
  return n >= 5 ? -1 : n
}

export function defaultRule(start: DateParts, freq: Freq = 'WEEKLY'): RecurrenceRule {
  return {
    freq,
    interval: 1,
    byDay: [weekdayOf(start)],
    monthlyMode: 'nth',
    monthDay: start.day,
    nth: nthOfMonth(start),
    nthWeekday: weekdayOf(start),
    end: { type: 'never' },
  }
}

function untilToRRule(until: string, allDay: boolean) {
  const compact = until.replace(/-/g, '')
  return allDay ? compact : `${compact}T235959Z`
}

function untilFromRRule(v: string) {
  const m = /^(\d{4})(\d{2})(\d{2})/.exec(v)
  return m ? `${m[1]}-${m[2]}-${m[3]}` : ''
}

export function buildRRule(rule: RecurrenceRule, allDay = false): string {
  const parts = [`FREQ=${rule.freq}`]
  if (rule.interval > 1) parts.push(`INTERVAL=${Math.floor(rule.interval)}`)
  if (rule.freq === 'WEEKLY' && rule.byDay.length) {
    const ordered = WEEKDAYS.filter((d) => rule.byDay.includes(d))
    parts.push(`BYDAY=${ordered.join(',')}`)
  }
  if (rule.freq === 'MONTHLY') {
    if (rule.monthlyMode === 'nth' && rule.nth && rule.nthWeekday) parts.push(`BYDAY=${rule.nth}${rule.nthWeekday}`)
    else if (rule.monthlyMode === 'day' && rule.monthDay) parts.push(`BYMONTHDAY=${rule.monthDay}`)
  }
  if (rule.end.type === 'count') parts.push(`COUNT=${Math.max(1, Math.floor(rule.end.count))}`)
  if (rule.end.type === 'until' && rule.end.until) parts.push(`UNTIL=${untilToRRule(rule.end.until, allDay)}`)
  return parts.join(';')
}

/** Parse the subset the builder can edit. Returns null for anything it can't represent faithfully. */
export function parseRRule(rrule: string, start: DateParts): RecurrenceRule | null {
  if (!rrule) return null
  const map = new Map<string, string>()
  for (const part of rrule.replace(/^RRULE:/i, '').split(';')) {
    if (!part) continue
    const [k, v] = part.split('=')
    if (!k || v === undefined) return null
    map.set(k.toUpperCase(), v.toUpperCase())
  }
  const freq = map.get('FREQ') as Freq | undefined
  if (!freq || !['DAILY', 'WEEKLY', 'MONTHLY', 'YEARLY'].includes(freq)) return null
  const known = new Set(['FREQ', 'INTERVAL', 'BYDAY', 'BYMONTHDAY', 'COUNT', 'UNTIL', 'WKST'])
  for (const k of map.keys()) if (!known.has(k)) return null

  const rule = defaultRule(start, freq)
  const interval = Number(map.get('INTERVAL') ?? '1')
  if (!Number.isInteger(interval) || interval < 1) return null
  rule.interval = interval

  const byDay = map.get('BYDAY')
  if (byDay) {
    if (freq === 'WEEKLY') {
      const days = byDay.split(',')
      if (!days.every((d) => (WEEKDAYS as readonly string[]).includes(d))) return null
      rule.byDay = days as Weekday[]
    } else if (freq === 'MONTHLY') {
      const m = /^(-1|[1-5])(MO|TU|WE|TH|FR|SA|SU)$/.exec(byDay)
      if (!m) return null
      rule.monthlyMode = 'nth'
      rule.nth = Number(m[1]) === 5 ? -1 : Number(m[1])
      rule.nthWeekday = m[2] as Weekday
    } else {
      return null
    }
  } else if (freq === 'WEEKLY') {
    rule.byDay = [weekdayOf(start)]
  }

  const monthDay = map.get('BYMONTHDAY')
  if (monthDay) {
    if (freq !== 'MONTHLY' || byDay) return null
    const n = Number(monthDay)
    if (!Number.isInteger(n) || n < 1 || n > 31) return null
    rule.monthlyMode = 'day'
    rule.monthDay = n
  } else if (freq === 'MONTHLY' && !byDay) {
    rule.monthlyMode = 'day'
    rule.monthDay = start.day
  }

  const count = map.get('COUNT')
  const until = map.get('UNTIL')
  if (count && until) return null
  if (count) {
    const n = Number(count)
    if (!Number.isInteger(n) || n < 1) return null
    rule.end = { type: 'count', count: n }
  } else if (until) {
    const d = untilFromRRule(until)
    if (!d) return null
    rule.end = { type: 'until', until: d }
  }
  return rule
}

export function presetToRRule(preset: Preset, start: DateParts): string {
  switch (preset) {
    case 'none':
    case 'custom':
      return ''
    case 'daily':
      return 'FREQ=DAILY'
    case 'weekdays':
      return 'FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR'
    case 'weekly':
      return `FREQ=WEEKLY;BYDAY=${weekdayOf(start)}`
    case 'monthly':
      return `FREQ=MONTHLY;BYDAY=${nthOfMonth(start)}${weekdayOf(start)}`
    case 'yearly':
      return 'FREQ=YEARLY'
  }
}

/** Which preset an RRULE corresponds to for a given start date ('custom' if none match). */
export function detectPreset(rrule: string, start: DateParts): Preset {
  if (!rrule) return 'none'
  const rule = parseRRule(rrule, start)
  if (!rule) return 'custom'
  const canonical = buildRRule(rule)
  for (const p of ['daily', 'weekdays', 'weekly', 'monthly', 'yearly'] as const) {
    if (canonical === presetToRRule(p, start)) return p
  }
  return 'custom'
}
