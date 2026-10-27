import type { EventInput } from '@fullcalendar/react'
import type { TFunction } from 'i18next'
import { CircleHelpIcon, MessageSquareDiffIcon, RepeatIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import type { CalEvent, Tag } from '@/api/types'
import { eventAriaLabel } from '@/lib/eventText'
import type { FormatOptions } from '@/lib/time'
import { cn } from '@/lib/utils'
import { ChipPresence } from './ChipPresence'

export type Ghost = { ev: CalEvent; start: string; end: string; allDay: boolean }

export type ChipProps = {
  ev?: CalEvent
  ghost?: boolean
  label: string
}

/** FullCalendar event IDs must be unique per occurrence. */
export function fcId(ev: Pick<CalEvent, 'id' | 'occurrence'>) {
  return ev.occurrence ? `${ev.id}|${ev.occurrence}` : ev.id
}

export function tagColorVars(ev: Pick<CalEvent, 'tags'>, tagsById: Map<string, Tag>) {
  const first = ev.tags[0] ? tagsById.get(ev.tags[0]) : undefined
  return first
    ? { color: `var(--${first.colorSlot})`, contrastColor: `var(--${first.colorSlot}-foreground)` }
    : { color: 'var(--primary)', contrastColor: 'var(--primary-foreground)' }
}

export function toEventInput(
  ev: CalEvent,
  ctx: { tagsById: Map<string, Tag>; glowing: ReadonlySet<string>; fmt: FormatOptions; t: TFunction },
): EventInput {
  const canMove = ev.permissions.edit || ev.permissions.suggest
  const label = eventAriaLabel(ev, ctx.fmt, ctx.t)
  return {
    id: fcId(ev),
    title: ev.title,
    start: ev.allDay ? ev.start.slice(0, 10) : ev.startUtc,
    end: ev.allDay ? ev.end.slice(0, 10) : ev.endUtc,
    allDay: ev.allDay,
    ...tagColorVars(ev, ctx.tagsById),
    startEditable: canMove,
    durationEditable: canMove,
    className: cn(ev.status === 'tentative' && 'sc-tentative', ctx.glowing.has(ev.id) && 'sc-glow'),
    extendedProps: { ev, label } satisfies ChipProps,
  }
}

export function ghostInput(g: Ghost, ctx: { tagsById: Map<string, Tag>; t: TFunction }): EventInput {
  return {
    id: '__ghost__',
    title: g.ev.title,
    start: g.start,
    end: g.end,
    allDay: g.allDay,
    ...tagColorVars(g.ev, ctx.tagsById),
    editable: false,
    interactive: false,
    className: 'sc-ghost',
    extendedProps: { ghost: true, label: ctx.t('calendar.ghostLabel', { title: g.ev.title }) } satisfies ChipProps,
  }
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
        {tentative ? <CircleHelpIcon className="size-3 shrink-0" /> : null}
        <span className="truncate">{info.event.title || ' '}</span>
        {recurring ? <RepeatIcon className="size-3 shrink-0 opacity-80" /> : null}
        {suggestions ? <MessageSquareDiffIcon className="size-3 shrink-0" /> : null}
      </div>
      {ev ? <ChipPresence eventId={ev.id} /> : null}
    </>
  )
}
