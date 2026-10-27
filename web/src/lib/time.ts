/** Time-zone helpers built on Intl (no extra date library in the bundle). */

export function browserTimeZone() {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

/** The zone the calendar is displayed in: the user's preference, else the browser's. */
export function displayTimeZone(pref: string | undefined) {
  return pref && isValidTimeZone(pref) ? pref : browserTimeZone()
}

export function isValidTimeZone(tz: string) {
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: tz })
    return true
  } catch {
    return false
  }
}

let zoneList: string[] | null = null
export function allTimeZones(): string[] {
  if (zoneList) return zoneList
  const intl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] }
  const list = intl.supportedValuesOf?.('timeZone') ?? []
  zoneList = list.includes('UTC') ? list : ['UTC', ...list]
  return zoneList
}

export type Wall = { year: number; month: number; day: number; hour: number; minute: number; second: number }

const partsCache = new Map<string, Intl.DateTimeFormat>()
function partsFormatter(tz: string) {
  let f = partsCache.get(tz)
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone: tz,
      hourCycle: 'h23',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
    partsCache.set(tz, f)
  }
  return f
}

/** Wall-clock fields of an instant in a zone. */
export function toWall(ms: number, tz: string): Wall {
  const parts = partsFormatter(tz).formatToParts(new Date(ms))
  const get = (type: string) => Number(parts.find((p) => p.type === type)?.value ?? 0)
  return {
    year: get('year'),
    month: get('month'),
    day: get('day'),
    hour: get('hour') % 24,
    minute: get('minute'),
    second: get('second'),
  }
}

function wallAsUtc(w: Wall) {
  return Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second)
}

/** The instant (ms) a wall-clock time denotes in a zone. Gaps resolve forward, overlaps to the earlier instant. */
export function fromWall(w: Wall, tz: string): number {
  const guess = wallAsUtc(w)
  const offset1 = wallAsUtc(toWall(guess, tz)) - guess
  let result = guess - offset1
  const offset2 = wallAsUtc(toWall(result, tz)) - result
  if (offset2 !== offset1) result = guess - offset2
  return result
}

const pad = (n: number, w = 2) => String(n).padStart(w, '0')

export function wallToIso(w: Wall) {
  return `${pad(w.year, 4)}-${pad(w.month)}-${pad(w.day)}T${pad(w.hour)}:${pad(w.minute)}:${pad(w.second)}`
}

export function parseWallIso(s: string): Wall {
  const m = /^(\d{4})-(\d{2})-(\d{2})(?:T(\d{2}):(\d{2})(?::(\d{2}))?)?/.exec(s)
  if (!m) throw new Error(`Bad date-time: ${s}`)
  return {
    year: Number(m[1]),
    month: Number(m[2]),
    day: Number(m[3]),
    hour: Number(m[4] ?? 0),
    minute: Number(m[5] ?? 0),
    second: Number(m[6] ?? 0),
  }
}

/** `YYYY-MM-DD` of an instant in a zone. */
export function dateInZone(ms: number, tz: string) {
  const w = toWall(ms, tz)
  return `${pad(w.year, 4)}-${pad(w.month)}-${pad(w.day)}`
}

export function todayInZone(tz: string) {
  return dateInZone(Date.now(), tz)
}

export function addDays(date: string, n: number) {
  const [y, m, d] = date.split('-').map(Number)
  const t = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, (d ?? 1) + n))
  return t.toISOString().slice(0, 10)
}

export function isDateString(s: string) {
  return /^\d{4}-\d{2}-\d{2}$/.test(s) && !Number.isNaN(Date.parse(`${s}T00:00:00Z`))
}

/** UTC instant string without milliseconds, as the API expects. */
export function toUtcString(d: Date | number) {
  return new Date(d).toISOString().replace(/\.\d{3}Z$/, 'Z')
}

// ---------------------------------------------------------------------------
// Display formatting

export type FormatOptions = { timeZone: string; clock24h: boolean; locale?: string }

const fmtCache = new Map<string, Intl.DateTimeFormat>()
function fmt(o: FormatOptions, opts: Intl.DateTimeFormatOptions) {
  const key = JSON.stringify([o.locale, o.timeZone, o.clock24h, opts])
  let f = fmtCache.get(key)
  if (!f) {
    const timeOpts = 'hour' in opts ? { hourCycle: o.clock24h ? ('h23' as const) : ('h12' as const) } : {}
    f = new Intl.DateTimeFormat(o.locale, { timeZone: o.timeZone, ...opts, ...timeOpts })
    fmtCache.set(key, f)
  }
  return f
}

export function formatTime(ms: number, o: FormatOptions) {
  return fmt(o, { hour: 'numeric', minute: '2-digit' }).format(ms)
}

/** Compact time for labels: "7 PM" / "7:30 PM" / "19:00". */
export function formatTimeShort(ms: number, o: FormatOptions) {
  const w = toWall(ms, o.timeZone)
  if (!o.clock24h && w.minute === 0) return fmt(o, { hour: 'numeric' }).format(ms)
  return formatTime(ms, o)
}

export function formatDateLong(ms: number, o: FormatOptions) {
  return fmt(o, { weekday: 'long', month: 'long', day: 'numeric', year: 'numeric' }).format(ms)
}

export function formatDateMedium(ms: number, o: FormatOptions) {
  return fmt(o, { weekday: 'short', month: 'short', day: 'numeric' }).format(ms)
}

export function formatDateTime(ms: number, o: FormatOptions) {
  return fmt(o, { month: 'short', day: 'numeric', year: 'numeric', hour: 'numeric', minute: '2-digit' }).format(ms)
}

export function formatWeekday(ms: number, o: FormatOptions) {
  return fmt(o, { weekday: 'long' }).format(ms)
}

/** A calendar date (YYYY-MM-DD) formatted without zone shifts. */
export function formatPlainDate(date: string, opts: Intl.DateTimeFormatOptions = { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' }, locale?: string) {
  const [y, m, d] = date.split('-').map(Number)
  return new Intl.DateTimeFormat(locale, { ...opts, timeZone: 'UTC' }).format(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1))
}

export function formatRelative(iso: string, locale?: string, now = Date.now()) {
  const diff = Date.parse(iso) - now
  const abs = Math.abs(diff)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const units: [Intl.RelativeTimeFormatUnit, number][] = [
    ['year', 365 * 864e5],
    ['month', 30 * 864e5],
    ['week', 7 * 864e5],
    ['day', 864e5],
    ['hour', 36e5],
    ['minute', 6e4],
  ]
  for (const [unit, size] of units) if (abs >= size) return rtf.format(Math.round(diff / size), unit)
  return rtf.format(0, 'minute')
}

export function timeZoneLabel(tz: string, at = Date.now()) {
  try {
    const parts = new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'shortOffset' }).formatToParts(at)
    const off = parts.find((p) => p.type === 'timeZoneName')?.value ?? ''
    return `${tz.replace(/_/g, ' ')} (${off})`
  } catch {
    return tz
  }
}
