import type { EventType } from '@/api/types'
import { addDays } from '@/lib/time'
// Types only: formModel pulls in zod, which must stay out of the calendar's initial bundle.
import type { Draft, EventFormValues } from './formModel'

/** `YYYY-MM-DDTHH:mm[:ss]` wall time plus minutes, as a wall time with seconds. */
export function addMinutesToWall(wall: string, minutes: number) {
  return new Date(Date.parse(`${wall.slice(0, 16)}:00Z`) + minutes * 60_000).toISOString().slice(0, 19)
}

/**
 * What a type fixes about an event's timing: always all-day (optionally a single date), and always one repeat
 * rule or never repeating.
 * The server rejects anything else, so the form and calendar lock these instead of offering them.
 */
export function fixedTiming(type: EventType | undefined) {
  const allDay = !!type?.defaultAllDay
  const rrule = type?.defaultRrule ?? ''
  return { allDay, rrule, noRepeat: !!type?.neverRepeats, singleDay: allDay && !!type?.singleDay }
}

/** The form values a type's fixed settings force, whether the event is new or being changed to this type. */
export function fixedValuesFor(v: EventFormValues, type: EventType): Partial<EventFormValues> {
  const fixed = fixedTiming(type)
  const out: Partial<EventFormValues> = {}
  if (fixed.allDay) out.allDay = true
  if (fixed.rrule) out.rrule = fixed.rrule
  if (fixed.noRepeat) out.rrule = ''
  if (fixed.singleDay) out.endDate = v.startDate
  return out
}

/**
 * A new-event draft with a type's creation defaults: all-day (a timed slot becomes its date), the length of
 * timed events, and the default repeat. A draft that is already all-day stays all-day.
 */
export function typedDraft(d: Draft, type: EventType): Draft {
  let { start, end, allDay } = d
  if (type.defaultAllDay && !allDay) {
    allDay = true
    start = start.slice(0, 10)
    end = addDays(start, 1)
  } else if (!allDay) {
    end = addMinutesToWall(start, type.defaultDurationMinutes)
  }
  // One date only: a type that is always all-day and always repeats cannot span days.
  if (fixedTiming(type).singleDay && allDay) end = addDays(start.slice(0, 10), 1)
  return { ...d, start, end, allDay, eventTypeId: type.id, rrule: type.neverRepeats ? '' : type.defaultRrule || d.rrule }
}

/** The form values a type's defaults change for a new event; the user's repeat rule is never overwritten. */
export function typeDefaultsFor(v: EventFormValues, type: EventType): Partial<EventFormValues> {
  const out: Partial<EventFormValues> = {}
  if (type.defaultAllDay !== v.allDay) out.allDay = type.defaultAllDay
  if (type.defaultRrule && !v.rrule) out.rrule = type.defaultRrule
  if (!type.defaultAllDay) {
    const end = addMinutesToWall(`${v.startDate}T${v.startTime}`, type.defaultDurationMinutes)
    out.endDate = end.slice(0, 10)
    out.endTime = end.slice(11, 16)
  }
  return out
}
