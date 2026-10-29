import { z } from 'zod'
import type { CalEvent, EventFieldValues, EventStatus, EventType, EventWrite } from '@/api/types'
import { addDays, fromWall, isValidTimeZone, parseWallIso } from '@/lib/time'
import { allowsMany } from './fieldKinds'

/** A new event before it exists: all-day → dates (end exclusive); timed → wall times in `timeZone`. */
export type Draft = { title: string; start: string; end: string; allDay: boolean; timeZone: string; eventTypeId?: string; rrule?: string }

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
    /** '' = generic event (free-form description). */
    eventTypeId: z.string(),
    /** Raw inputs for the type's fields: strings, or booleans for checkboxes. Untouched inputs are absent. */
    fields: z.record(z.string(), z.union([z.string(), z.boolean(), z.array(z.string())]).optional()),
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
    eventTypeId: ev.eventTypeId ?? '',
    fields: fieldsToForm(ev.fields),
  }
}

function fieldsToForm(values: EventFieldValues): EventFormValues['fields'] {
  return Object.fromEntries(Object.entries(values).map(([k, v]) => [k, typeof v === 'boolean' || Array.isArray(v) ? v : String(v)]))
}

/** Form inputs → API values, dropping empty ones. Keys are sorted so before/after compare equal. */
export function fieldsToPayload(raw: EventFormValues['fields'], type: EventType | undefined): EventFieldValues {
  const out: EventFieldValues = {}
  if (!type) return out
  for (const def of [...type.fields].sort((a, b) => a.key.localeCompare(b.key))) {
    const v = raw[def.key]
    if (v === undefined || v === false) continue
    if (def.kind === 'members' || def.kind === 'select') {
      // Values saved before a field was switched between one and several arrive in the other shape.
      let list = typeof v === 'string' ? (v.trim() ? [v.trim()] : []) : Array.isArray(v) ? v : []
      if (def.kind === 'select') {
        const valid = list.filter((o) => def.options?.includes(o))
        // Several keep option order; one keeps the first value that was saved.
        list = allowsMany(def) ? (def.options ?? []).filter((o) => valid.includes(o)) : valid
      }
      if (!list.length) continue
      if (!allowsMany(def)) list = list.slice(0, 1)
      // Several → a list; one choice → a plain string, one person → a one-element list.
      out[def.key] = def.kind === 'select' && !allowsMany(def) ? list[0]! : [...list]
      continue
    }
    if (Array.isArray(v)) continue
    if (def.kind === 'checkbox') {
      if (v === true) out[def.key] = true
      continue
    }
    const s = String(v).trim()
    if (s === '') continue
    if (def.kind === 'number') {
      const n = Number(s)
      out[def.key] = Number.isFinite(n) ? n : s
    } else out[def.key] = s
  }
  return out
}

/** Fields that are required but empty, as `[key, label]`. Mirrors the server's check so the form can say so early. */
export function missingRequired(raw: EventFormValues['fields'], type: EventType | undefined): EventType['fields'] {
  if (!type) return []
  const have = fieldsToPayload(raw, type)
  return type.fields.filter((d) => d.required && have[d.key] === undefined)
}

export function formFromDraft(d: Draft): EventFormValues {
  return {
    title: d.title,
    allDay: d.allDay,
    ...timesToForm(d.allDay, d.start, d.end),
    timeZone: d.timeZone,
    rrule: d.rrule ?? '',
    location: '',
    description: '',
    tags: [],
    status: 'confirmed',
    eventTypeId: d.eventTypeId ?? '',
    fields: {},
  }
}

/** Form values → API body. All-day end dates become exclusive. */
export function formToPayload(v: EventFormValues, type?: EventType): EventWrite {
  return {
    title: v.title.trim(),
    allDay: v.allDay,
    start: v.allDay ? v.startDate : `${v.startDate}T${v.startTime}:00`,
    end: v.allDay ? addDays(v.endDate, 1) : `${v.endDate}T${v.endTime}:00`,
    timeZone: v.timeZone,
    rrule: v.rrule,
    location: v.location,
    // Typed events have fields instead of a description.
    description: v.eventTypeId ? '' : v.description,
    tags: v.tags,
    status: v.status as EventStatus,
    eventTypeId: v.eventTypeId,
    fields: v.eventTypeId ? fieldsToPayload(v.fields, type) : {},
  }
}

/** Fields a single occurrence can't change on its own (server rule; see docs/decisions.md). */
const SERIES_ONLY_FIELDS = ['timeZone', 'allDay', 'rrule', 'tags'] as const

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
