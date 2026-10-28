import type { QueryClient, QueryKey } from '@tanstack/react-query'
import type { RangeParams } from './endpoints'
import { keys } from './keys'
import type { CalEvent, EventDetail } from './types'
import { overlaps } from '@/realtime/applyMessage'

type EventsData = { events: CalEvent[] }

export type CacheSnapshot = [QueryKey, unknown][]

export function snapshotEvent(qc: QueryClient, id: string): CacheSnapshot {
  const out: CacheSnapshot = []
  for (const q of qc.getQueryCache().findAll({ queryKey: keys.eventsAll })) out.push([q.queryKey, q.state.data])
  out.push([keys.event(id), qc.getQueryData(keys.event(id))])
  return out
}

export function restoreSnapshot(qc: QueryClient, snap: CacheSnapshot) {
  for (const [key, data] of snap) if (data !== undefined) qc.setQueryData(key, data)
}

const sameOccurrence = (a: CalEvent, id: string, occurrence: string | null | undefined) => a.id === id && (occurrence ? a.occurrence === occurrence : true)

/** Optimistically patch an event (or one occurrence) in every cached range and the detail. */
export function patchEventInCache(qc: QueryClient, id: string, occurrence: string | null | undefined, patch: Partial<CalEvent>) {
  for (const q of qc.getQueryCache().findAll({ queryKey: keys.eventsAll })) {
    const data = q.state.data as EventsData | undefined
    if (!data?.events.some((x) => sameOccurrence(x, id, occurrence))) continue
    qc.setQueryData<EventsData>(q.queryKey, {
      ...data,
      events: data.events.map((x) => (sameOccurrence(x, id, occurrence) ? { ...x, ...patch } : x)),
    })
  }
  if (!occurrence) {
    const detail = qc.getQueryData<EventDetail>(keys.event(id))
    if (detail) qc.setQueryData<EventDetail>(keys.event(id), { ...detail, ...patch })
  }
}

/** Put a server-confirmed event into the cache (or refetch for recurring series). */
export function storeEvent(qc: QueryClient, ev: CalEvent) {
  const recurring = ev.rrule !== ''
  if (recurring) {
    void qc.invalidateQueries({ queryKey: keys.eventsAll })
    void qc.invalidateQueries({ queryKey: keys.event(ev.id) })
    return
  }
  for (const q of qc.getQueryCache().findAll({ queryKey: keys.eventsAll })) {
    const params = q.queryKey[1] as RangeParams | 'none' | undefined
    const data = q.state.data as EventsData | undefined
    if (!data || !params || params === 'none') continue
    const has = data.events.some((x) => x.id === ev.id)
    const inRange = overlaps(ev, params)
    if (has && data.events.some((x) => x.id === ev.id && x.occurrence)) {
      void qc.invalidateQueries({ queryKey: q.queryKey, exact: true })
      continue
    }
    const item = { ...ev, occurrence: null, overridden: false }
    let next: CalEvent[] | null = null
    if (has && inRange) next = data.events.map((x) => (x.id === ev.id ? item : x))
    else if (has) next = data.events.filter((x) => x.id !== ev.id)
    else if (inRange && !params.q && !params.tag?.length && !params.owner?.length) next = [...data.events, item]
    else if (inRange) void qc.invalidateQueries({ queryKey: q.queryKey, exact: true })
    if (next) qc.setQueryData<EventsData>(q.queryKey, { ...data, events: next })
  }
  const detail = qc.getQueryData<EventDetail>(keys.event(ev.id))
  if (detail) qc.setQueryData<EventDetail>(keys.event(ev.id), { ...detail, ...ev })
  void qc.invalidateQueries({ queryKey: keys.history(ev.id) })
}

export function removeEventFromCache(qc: QueryClient, id: string, occurrence?: string | null) {
  for (const q of qc.getQueryCache().findAll({ queryKey: keys.eventsAll })) {
    const data = q.state.data as EventsData | undefined
    if (!data?.events.some((x) => sameOccurrence(x, id, occurrence))) continue
    qc.setQueryData<EventsData>(q.queryKey, { ...data, events: data.events.filter((x) => !sameOccurrence(x, id, occurrence)) })
  }
}

/** Find an occurrence (or the event) among cached ranges. */
export function findCachedEvent(qc: QueryClient, id: string, occurrence?: string | null): CalEvent | undefined {
  for (const q of qc.getQueryCache().findAll({ queryKey: keys.eventsAll })) {
    const data = q.state.data as EventsData | undefined
    const hit = data?.events.find((x) => x.id === id && (occurrence ? x.occurrence === occurrence : true))
    if (hit) return hit
  }
  return undefined
}
