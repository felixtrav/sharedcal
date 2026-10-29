import { useCallback } from 'react'
import { useDirectory, useEventTypes } from '@/api/queries'
import type { CalEvent } from '@/api/types'
import { viewerTime } from '@/lib/fieldTime'
import type { FormatOptions } from '@/lib/time'
import { renderTitle } from '@/lib/titleTemplate'

/**
 * The title to show a viewer. A generated title that contains a time is built again here so the time follows the
 * viewer's zone and clock; the stored title (used by search, history and notifications) keeps the event's own zone.
 */
export function useEventTitle(fmt: FormatOptions): (ev: Pick<CalEvent, 'title' | 'eventTypeId' | 'fields' | 'start' | 'timeZone'>) => string {
  const { data: types } = useEventTypes()
  const { data: dir } = useDirectory()
  return useCallback(
    (ev) => {
      const type = ev.eventTypeId ? types?.eventTypes.find((et) => et.id === ev.eventTypeId) : undefined
      if (!type?.titleTemplate || !type.fields.some((f) => f.kind === 'time' && typeof ev.fields[f.key] === 'string')) return ev.title
      const nameOf = (id: string) => dir?.users.find((u) => u.id === id)?.name
      return renderTitle(type, ev.fields, nameOf, (hhmm) => viewerTime(hhmm, { date: ev.start.slice(0, 10), eventTz: ev.timeZone, fmt }))
    },
    [types, dir, fmt],
  )
}
