import type { TFunction } from 'i18next'
import type { CalEvent } from '@/api/types'
import { addDays, dateInZone, formatDateMedium, formatPlainDate, formatTimeShort, formatWeekday, type FormatOptions } from './time'

type Timing = Pick<CalEvent, 'allDay' | 'start' | 'end' | 'startUtc' | 'endUtc'>

/** Human description of when an event happens, e.g. "Friday 7 to 11 PM". */
export function eventWhen(ev: Timing, o: FormatOptions, t: TFunction): string {
  try {
    return eventWhenUnsafe(ev, o, t)
  } catch {
    // Malformed dates from the server must never take the calendar down.
    return `${ev.start} – ${ev.end}`
  }
}

function eventWhenUnsafe(ev: Timing, o: FormatOptions, t: TFunction): string {
  if (ev.allDay) {
    const first = ev.start.slice(0, 10)
    const last = addDays(ev.end.slice(0, 10), -1)
    const fmt = { weekday: 'long', month: 'long', day: 'numeric' } as const
    if (last <= first) return t('event.when.allDay', { date: formatPlainDate(first, fmt, o.locale) })
    return t('event.when.allDayRange', {
      start: formatPlainDate(first, fmt, o.locale),
      end: formatPlainDate(last, fmt, o.locale),
    })
  }
  const s = Date.parse(ev.startUtc)
  const e = Date.parse(ev.endUtc)
  const sameDay = dateInZone(s, o.timeZone) === dateInZone(Math.max(s, e - 1), o.timeZone)
  if (sameDay) {
    const startText = formatTimeShort(s, o)
    const endText = formatTimeShort(e, o)
    // "7 to 11 PM" when both share the meridiem, as in the spec's example.
    const sm = startText.match(/\s?(AM|PM)$/i)
    const em = endText.match(/\s?(AM|PM)$/i)
    const startShown = sm && em && sm[1] === em[1] ? startText.replace(/\s?(AM|PM)$/i, '') : startText
    if (s === e) return t('event.when.at', { day: formatWeekday(s, o), time: startText })
    return t('event.when.sameDay', { day: formatWeekday(s, o), start: startShown, end: endText })
  }
  return t('event.when.multiDay', {
    start: `${formatDateMedium(s, o)} ${formatTimeShort(s, o)}`,
    end: `${formatDateMedium(e, o)} ${formatTimeShort(e, o)}`,
  })
}

/** Screen-reader label for an event chip. */
export function eventAriaLabel(ev: CalEvent, o: FormatOptions, t: TFunction, typeName?: string): string {
  const parts = [ev.title || t('event.untitled'), eventWhen(ev, o, t), t('event.ownedBy', { name: ev.owner.name })]
  if (typeName) parts.unshift(typeName)
  if (ev.yearsSince) parts.push(t('event.yearsSince', { count: ev.yearsSince }))
  if (ev.status === 'tentative') parts.push(t('event.status.tentative'))
  if (ev.rrule || ev.occurrence) parts.push(t('event.recurring'))
  if (ev.counts.openSuggestions > 0 && ev.permissions.review) {
    parts.push(t('event.openSuggestions', { count: ev.counts.openSuggestions }))
  }
  return parts.join(', ')
}

export function formatFileSize(bytes: number, locale?: string) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytes
  let i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  const digits = i === 0 || n >= 10 ? 0 : 1
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: digits }).format(n)} ${units[i]}`
}
