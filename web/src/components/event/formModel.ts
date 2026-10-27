import { z } from 'zod'
import type { CalEvent, EventStatus, EventWrite } from '@/api/types'
import { addDays, fromWall, isValidTimeZone, parseWallIso } from '@/lib/time'

/** A new event before it exists: all-day → dates (end exclusive); timed → wall times in `timeZone`. */
export type Draft = { title: string; start: string; end: string; allDay: boolean; timeZone: string }

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/
const TIME_RE = /^([01]\d|2[0-3]):[0-5]\d$/
const MAX_DAYS = 366

/** Zod schema mirroring the server's validation. Messages are i18n keys. */
export const eventFormSchema = z
  .object({
    title: z.string().trim().min(1, 'validation.titleRequired').max(200, 'validation.titleTooLong'),
    allDay: z.boolean(),
    startDate: z.string().regex(DATE_RE, 'validation.dateInvalid'),
    startTime: z.string(),
    endDate: z.string().regex(DATE_RE, 'validation.dateInvalid'),
    endTime: z.string(),
    timeZone: z.string().refine(isValidTimeZone, 'validation.timeZoneInvalid'),
    rrule: z.string().max(500),
    location: z.string().max(1000, 'validation.locationTooLong'),
    description: z.string().max(20000, 'validation.descriptionTooLong'),
    tags: z.array(z.string()),
    status: z.enum(['confirmed', 'tentative']),
  })
  .superRefine((v, ctx) => {
    if (!v.allDay) {
      if (!TIME_RE.test(v.startTime)) ctx.addIssue({ code: 'custom', path: ['startTime'], message: 'validation.timeInvalid' })
      if (!TIME_RE.test(v.endTime)) ctx.addIssue({ code: 'custom', path: ['endTime'], message: 'validation.timeInvalid' })
    }
    if (!DATE_RE.test(v.startDate) || !DATE_RE.test(v.endDate)) return
    const span = spanMs(v)
    if (span === null) return
    if (span < 0) ctx.addIssue({ code: 'custom', path: ['endDate'], message: 'validation.endBeforeStart' })
    else if (span > MAX_DAYS * 86_400_000) ctx.addIssue({ code: 'custom', path: ['endDate'], message: 'validation.tooLong' })
  })

export type EventFormValues = z.infer<typeof eventFormSchema>

function spanMs(v: Pick<EventFormValues, 'allDay' | 'startDate' | 'endDate' | 'startTime' | 'endTime' | 'timeZone'>) {
  if (v.allDay) return Date.parse(`${addDays(v.endDate, 1)}T00:00:00Z`) - Date.parse(`${v.startDate}T00:00:00Z`)
  if (!TIME_RE.test(v.startTime) || !TIME_RE.test(v.endTime) || !isValidTimeZone(v.timeZone)) return null
  const s = fromWall(parseWallIso(`${v.startDate}T${v.startTime}`), v.timeZone)
  const e = fromWall(parseWallIso(`${v.endDate}T${v.endTime}`), v.timeZone)
  return e - s
}

function splitWall(s: string) {
  return { date: s.slice(0, 10), time: s.length >= 16 ? s.slice(11, 16) : '09:00' }
}

function timesToForm(allDay: boolean, start: string, end: string) {
  if (allDay) {
    const sd = start.slice(0, 10)
    const ed = addDays(end.slice(0, 10), -1)
    return { startDate: sd, endDate: ed < sd ? sd : ed, startTime: '09:00', endTime: '10:00' }
  }
  const s = splitWall(start)
  const e = splitWall(end)
  return { startDate: s.date, startTime: s.time, endDate: e.date, endTime: e.time }
}

export function formFromEvent(ev: CalEvent): EventFormValues {
  return {
    title: ev.title,
    allDay: ev.allDay,
    ...timesToForm(ev.allDay, ev.start, ev.end),
    timeZone: ev.timeZone,
    rrule: ev.rrule,
    location: ev.location,
    description: ev.description,
    tags: [...ev.tags],
    status: ev.status,
  }
}

export function formFromDraft(d: Draft): EventFormValues {
  return {
    title: d.title,
    allDay: d.allDay,
    ...timesToForm(d.allDay, d.start, d.end),
    timeZone: d.timeZone,
    rrule: '',
    location: '',
    description: '',
    tags: [],
    status: 'confirmed',
  }
}

/** Form values → API body. All-day end dates become exclusive. */
export function formToPayload(v: EventFormValues): EventWrite {
  return {
    title: v.title.trim(),
    allDay: v.allDay,
    start: v.allDay ? v.startDate : `${v.startDate}T${v.startTime}:00`,
    end: v.allDay ? addDays(v.endDate, 1) : `${v.endDate}T${v.endTime}:00`,
    timeZone: v.timeZone,
    rrule: v.rrule,
    location: v.location,
    description: v.description,
    tags: v.tags,
    status: v.status as EventStatus,
  }
}

/** Fields a single occurrence can't change on its own (server rule; see docs/decisions.md). */
export const SERIES_ONLY_FIELDS = ['timeZone', 'allDay', 'rrule', 'tags'] as const

export function needsSeriesScope(diff: Partial<EventWrite>) {
  return SERIES_ONLY_FIELDS.some((k) => k in diff)
}

/**
 * Only the fields that changed. With `groupTimes` (used for suggestions), times travel together (`start`, `end`,
 * `timeZone`, `allDay`) because the server validates them as a unit.
 */
export function changedFields(before: EventWrite, after: EventWrite, groupTimes = true): Partial<EventWrite> {
  const out: Partial<EventWrite> = {}
  const keys = Object.keys(after) as (keyof EventWrite)[]
  for (const k of keys) {
    if (JSON.stringify(before[k] ?? '') !== JSON.stringify(after[k] ?? '')) (out as Record<string, unknown>)[k] = after[k]
  }
  const timeKeys = ['start', 'end', 'timeZone', 'allDay'] as const
  if (groupTimes && timeKeys.some((k) => k in out)) for (const k of timeKeys) (out as Record<string, unknown>)[k] = after[k]
  return out
}
