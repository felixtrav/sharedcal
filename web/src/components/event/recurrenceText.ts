import type { TFunction } from 'i18next'
import { parseDate, parseRRule, WEEKDAYS, type RecurrenceRule, type Weekday } from '@/lib/recurrence'
import { formatPlainDate } from '@/lib/time'

/** Localized weekday name; 2024-01-01 was a Monday. */
export function weekdayName(d: Weekday, style: 'long' | 'short' = 'long', locale?: string) {
  const idx = WEEKDAYS.indexOf(d)
  return new Intl.DateTimeFormat(locale, { weekday: style, timeZone: 'UTC' }).format(Date.UTC(2024, 0, 1 + idx))
}

export function describeRule(rule: RecurrenceRule, t: TFunction, locale?: string): string {
  const count = rule.interval
  let text = t(`recurrence.every.${rule.freq.toLowerCase()}`, { count })
  if (rule.freq === 'WEEKLY' && rule.byDay.length) {
    const ordered = WEEKDAYS.filter((d) => rule.byDay.includes(d))
    const isWeekdays = ordered.join() === 'MO,TU,WE,TH,FR'
    text += ' ' + (isWeekdays ? t('recurrence.onWeekdays') : t('recurrence.onDays', { days: new Intl.ListFormat(locale, { type: 'conjunction' }).format(ordered.map((d) => weekdayName(d, 'long', locale))) }))
  }
  if (rule.freq === 'MONTHLY') {
    if (rule.monthlyMode === 'nth' && rule.nth && rule.nthWeekday) {
      text += ' ' + t('recurrence.onNth', { nth: t(`recurrence.nth.${rule.nth}`), day: weekdayName(rule.nthWeekday, 'long', locale) })
    } else if (rule.monthDay) {
      text += ' ' + t('recurrence.onMonthDay', { day: rule.monthDay })
    }
  }
  if (rule.end.type === 'count') text += ', ' + t('recurrence.times', { count: rule.end.count })
  if (rule.end.type === 'until') text += ', ' + t('recurrence.until', { date: formatPlainDate(rule.end.until, { month: 'short', day: 'numeric', year: 'numeric' }, locale) })
  return text
}

export function describeRRule(rrule: string, startDate: string, t: TFunction, locale?: string): string {
  if (!rrule) return t('recurrence.none')
  const rule = parseRRule(rrule, parseDate(startDate))
  return rule ? describeRule(rule, t, locale) : t('recurrence.customRaw', { rule: rrule })
}
