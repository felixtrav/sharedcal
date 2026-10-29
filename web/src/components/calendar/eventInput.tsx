import type { EventInput } from '@fullcalendar/react'
import type { TFunction } from 'i18next'
import { CircleHelpIcon, MessageSquareDiffIcon, RepeatIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import type { CalEvent, EventType, EventTypeStyle, Tag } from '@/api/types'
import { eventAriaLabel } from '@/lib/eventText'
import type { FormatOptions } from '@/lib/time'
import { TypeGlyph } from '@/lib/typeIcons'
import { cn } from '@/lib/utils'
import { fixedTiming } from '../event/typeDefaults'
import { ChipPresence } from './ChipPresence'

export type Ghost = { ev: CalEvent; start: string; end: string; allDay: boolean }

export type ChipProps = {
  ev?: CalEvent
  ghost?: boolean
  label: string
  /** The event type's icon, so typed events are recognizable without relying on color. */
  icon?: EventType['icon']
  style?: EventTypeStyle
  yearsSince?: number
  /** The type is always all-day, so the chip can't be dragged into the timed grid. */
  fixedAllDay?: boolean
}

/** FullCalendar event IDs must be unique per occurrence. */
function fcId(ev: Pick<CalEvent, 'id' | 'occurrence'>) {
  return ev.occurrence ? `${ev.id}|${ev.occurrence}` : ev.id
}

type Lookups = { tagsById: Map<string, Tag>; typesById: Map<string, EventType> }

/** An event's type decides its color; untyped events use their first tag. */
function colorVars(ev: Pick<CalEvent, 'tags' | 'eventTypeId'>, { tagsById, typesById }: Lookups) {
  const slot = (ev.eventTypeId ? typesById.get(ev.eventTypeId)?.colorSlot : undefined) ?? (ev.tags[0] ? tagsById.get(ev.tags[0])?.colorSlot : undefined)
  return slot
    ? { color: `var(--${slot})`, contrastColor: `var(--${slot}-foreground)` }
    : { color: 'var(--primary)', contrastColor: 'var(--primary-foreground)' }
}

export function toEventInput(
  ev: CalEvent,
  ctx: Lookups & { glowing: ReadonlySet<string>; fmt: FormatOptions; t: TFunction; titleOf: (ev: CalEvent) => string },
): EventInput {
  const canMove = ev.permissions.edit || ev.permissions.suggest
  const type = ev.eventTypeId ? ctx.typesById.get(ev.eventTypeId) : undefined
  const title = ctx.titleOf(ev)
  const label = eventAriaLabel({ ...ev, title }, ctx.fmt, ctx.t, type?.name)
  return {
    id: fcId(ev),
    title,
    start: ev.allDay ? ev.start.slice(0, 10) : ev.startUtc,
    end: ev.allDay ? ev.end.slice(0, 10) : ev.endUtc,
    allDay: ev.allDay,
    ...colorVars(ev, ctx),
    startEditable: canMove,
    // One date only (always all-day and always repeating): the length can't be stretched.
    durationEditable: canMove && !fixedTiming(type).singleDay,
    className: cn(ev.status === 'tentative' && 'sc-tentative', ctx.glowing.has(ev.id) && 'sc-glow', type && `sc-style-${type.style}`),
    extendedProps: { ev, label, icon: type?.icon, style: type?.style, yearsSince: ev.yearsSince, fixedAllDay: fixedTiming(type).allDay } satisfies ChipProps,
  }
}

export function ghostInput(g: Ghost, ctx: Lookups & { t: TFunction }): EventInput {
  return {
    id: '__ghost__',
    title: g.ev.title,
    start: g.start,
    end: g.end,
    allDay: g.allDay,
    ...colorVars(g.ev, ctx),
    editable: false,
    interactive: false,
    className: 'sc-ghost',
    extendedProps: { ghost: true, label: ctx.t('calendar.ghostLabel', { title: g.ev.title }) } satisfies ChipProps,
  }
}

/** Banner events sort first within a day, so they read as a strip at the top of it (also in the agenda). */
type Millis = Date | number | null | undefined
type Orderable = { start?: Millis; end?: Millis; allDay?: boolean; title?: string; extendedProps?: Record<string, unknown> }

const ms = (v: Millis) => (v instanceof Date ? v.getTime() : (v ?? 0))

/** FullCalendar's default order (start, longest first, all-day, title) with banners in front. */
export function compareEvents(a: Orderable, b: Orderable) {
  const rank = (e: Orderable) => ((e.extendedProps as Partial<ChipProps> | undefined)?.style === 'banner' ? 0 : 1)
  const span = (e: Orderable) => ms(e.end) - ms(e.start)
  return (
    rank(a) - rank(b) ||
    ms(a.start) - ms(b.start) ||
    span(b) - span(a) ||
    Number(!!b.allDay) - Number(!!a.allDay) ||
    (a.title ?? '').localeCompare(b.title ?? '')
  )
}

/** Custom content: same structure as FullCalendar's default, plus status icons and a full SR label. */
export function renderEventContent(info: {
  event: { title: string; extendedProps: Record<string, unknown> }
  timeText: string
  timeClass: string
  titleClass: string
}): ReactNode {
  const props = info.event.extendedProps as ChipProps
  const ev = props.ev
  const tentative = ev?.status === 'tentative'
  const recurring = !!(ev?.rrule || ev?.occurrence)
  const suggestions = ev && ev.permissions.review && ev.counts.openSuggestions > 0
  return (
    <>
      <span className="sr-only">{props.label}</span>
      {info.timeText ? (
        <div aria-hidden className={cn(info.timeClass, 'tabular-nums')}>
          {info.timeText}
        </div>
      ) : null}
      <div aria-hidden className={cn(info.titleClass, 'inline-flex min-w-0 items-center gap-1')}>
        {props.icon ? (
          props.style === 'badge' ? (
            // A chip in the inverse colors, so it stays readable on every type color.
            <span className="inline-flex shrink-0 rounded-sm bg-[var(--fc-event-contrast-color,var(--background))] p-0.5 text-(--fc-event-color) ring-1 ring-(--fc-event-color)">
              <TypeGlyph name={props.icon} className="size-3" />
            </span>
          ) : (
            <TypeGlyph name={props.icon} className="size-3 shrink-0" />
          )
        ) : null}
        {tentative ? <CircleHelpIcon className="size-3 shrink-0" /> : null}
        <span className="truncate">{info.event.title || ' '}</span>
        {props.yearsSince ? <span className="shrink-0 tabular-nums opacity-90">· {props.yearsSince}</span> : null}
        {recurring ? <RepeatIcon className="size-3 shrink-0 opacity-80" /> : null}
        {suggestions ? <MessageSquareDiffIcon className="size-3 shrink-0" /> : null}
      </div>
      {ev ? <ChipPresence eventId={ev.id} /> : null}
    </>
  )
}
