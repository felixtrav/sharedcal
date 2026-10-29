import type { QueryClient } from '@tanstack/react-query'
import type { RangeParams } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { Attachment, CalEvent, Comment, EventDetail, EventType, Notification, PresenceViewer, Suggestion, Tag } from '@/api/types'

type EventsData = { events: CalEvent[] }
type CommentsData = { comments: Comment[] }
type InboxData = { items: Notification[]; unread: number }

type ApplyContext = {
  meId?: string
  /** Called for events changed by someone else, to highlight them. */
  onRemoteChange?: (eventId: string) => void
  isOwnWrite?: (eventId: string, version?: number) => boolean
  onEventDeleted?: (eventId: string) => void
  onPresence?: (eventId: string, viewers: PresenceViewer[]) => void
}

const t = (iso: string) => Date.parse(iso)

/** Whether an event's UTC span intersects a cached range. Zero-length events count at their start. */
export function overlaps(ev: Pick<CalEvent, 'startUtc' | 'endUtc'>, range: Pick<RangeParams, 'from' | 'to'>) {
  const s = t(ev.startUtc)
  const e = t(ev.endUtc)
  const from = t(range.from)
  const to = t(range.to)
  if (e <= s) return s >= from && s < to
  return s < to && e > from
}

/** Whether an event matches the range query's tag/owner filters. `null` = can't tell locally (free text). */
export function matchesFilters(ev: CalEvent, p: RangeParams): boolean | null {
  if (p.q) return null
  if (p.tag?.length && !ev.tags.some((id) => p.tag?.includes(id))) return false
  if (p.type?.length && !(ev.eventTypeId && p.type.includes(ev.eventTypeId))) return false
  if (p.owner?.length && !p.owner.includes(ev.owner.id)) return false
  return true
}

function isRecurring(ev: CalEvent) {
  return ev.rrule !== ''
}

function eventRangeQueries(qc: QueryClient) {
  return qc.getQueryCache().findAll({ queryKey: keys.eventsAll })
}

function applyEventUpsert(qc: QueryClient, ev: CalEvent, ctx: ApplyContext) {
  let changed = false
  let needsInvalidate = isRecurring(ev)

  if (!needsInvalidate) {
    for (const q of eventRangeQueries(qc)) {
      const params = q.queryKey[1] as RangeParams | 'none' | undefined
      const data = q.state.data as EventsData | undefined
      if (!params || params === 'none' || !data) continue
      // A previously recurring event becoming single: its occurrences need a refetch.
      if (data.events.some((x) => x.id === ev.id && x.occurrence)) {
        needsInvalidate = true
        break
      }
      const match = matchesFilters(ev, params)
      if (match === null) {
        void qc.invalidateQueries({ queryKey: q.queryKey, exact: true })
        changed = true
        continue
      }
      const idx = data.events.findIndex((x) => x.id === ev.id)
      const existing = idx >= 0 ? data.events[idx] : undefined
      if (existing && existing.version >= ev.version) continue
      const belongs = match && overlaps(ev, params)
      let next: CalEvent[] | null = null
      if (belongs) {
        const item: CalEvent = { ...ev, occurrence: null, overridden: false }
        next = existing ? data.events.map((x, i) => (i === idx ? item : x)) : [...data.events, item]
      } else if (existing) {
        next = data.events.filter((x) => x.id !== ev.id)
      }
      if (next) {
        qc.setQueryData<EventsData>(q.queryKey, { ...data, events: next })
        changed = true
      }
    }
  }

  if (needsInvalidate) {
    void qc.invalidateQueries({ queryKey: keys.eventsAll })
    changed = true
  }

  const detail = qc.getQueryData<EventDetail>(keys.event(ev.id))
  if (detail) {
    if (detail.version < ev.version) {
      if (isRecurring(ev) || isRecurring(detail)) void qc.invalidateQueries({ queryKey: keys.event(ev.id) })
      else qc.setQueryData<EventDetail>(keys.event(ev.id), { ...detail, ...ev })
      void qc.invalidateQueries({ queryKey: keys.history(ev.id) })
      changed = true
    }
  }

  if (changed && !ctx.isOwnWrite?.(ev.id, ev.version)) ctx.onRemoteChange?.(ev.id)
}

function applyEventDeleted(qc: QueryClient, id: string, ctx: ApplyContext) {
  for (const q of eventRangeQueries(qc)) {
    const data = q.state.data as EventsData | undefined
    if (!data || !data.events.some((x) => x.id === id)) continue
    qc.setQueryData<EventsData>(q.queryKey, { ...data, events: data.events.filter((x) => x.id !== id) })
  }
  if (!ctx.isOwnWrite?.(id)) ctx.onEventDeleted?.(id)
  qc.removeQueries({ queryKey: keys.suggestionsAll(id) })
}

function bumpCount(qc: QueryClient, eventId: string, field: keyof CalEvent['counts'], delta: number) {
  const fix = (ev: CalEvent): CalEvent => (ev.id === eventId ? { ...ev, counts: { ...ev.counts, [field]: Math.max(0, ev.counts[field] + delta) } } : ev)
  for (const q of eventRangeQueries(qc)) {
    const data = q.state.data as EventsData | undefined
    if (!data || !data.events.some((x) => x.id === eventId)) continue
    qc.setQueryData<EventsData>(q.queryKey, { ...data, events: data.events.map(fix) })
  }
  const detail = qc.getQueryData<EventDetail>(keys.event(eventId))
  if (detail) qc.setQueryData<EventDetail>(keys.event(eventId), fix(detail) as EventDetail)
}

function sortComments(list: Comment[]) {
  return [...list].sort((a, b) => t(a.createdAt) - t(b.createdAt))
}

function applyCommentUpsert(qc: QueryClient, c: Comment) {
  const key = keys.comments(c.eventId)
  const data = qc.getQueryData<CommentsData>(key)
  const isNew = !data?.comments.some((x) => x.id === c.id)
  if (data) {
    const list = isNew ? [...data.comments, c] : data.comments.map((x) => (x.id === c.id ? c : x))
    qc.setQueryData<CommentsData>(key, { comments: sortComments(list) })
  }
  if (isNew) bumpCount(qc, c.eventId, 'comments', 1)
}

function applyCommentDeleted(qc: QueryClient, p: { id: string; eventId: string }) {
  const key = keys.comments(p.eventId)
  const data = qc.getQueryData<CommentsData>(key)
  const present = data?.comments.some((x) => x.id === p.id)
  if (data && present) {
    qc.setQueryData<CommentsData>(key, { comments: data.comments.filter((x) => x.id !== p.id) })
  }
  // Only count it once: when it was in our list, or when the list isn't loaded.
  if (present || !data) bumpCount(qc, p.eventId, 'comments', -1)
}

function upsertAttachment(list: Attachment[], a: Attachment) {
  return list.some((x) => x.id === a.id) ? list.map((x) => (x.id === a.id ? a : x)) : [...list, a]
}

function applyAttachment(qc: QueryClient, a: Attachment) {
  if (a.commentId === null) {
    const detail = qc.getQueryData<EventDetail>(keys.event(a.eventId))
    const isNew = !detail?.attachments.some((x) => x.id === a.id)
    if (detail) {
      qc.setQueryData<EventDetail>(keys.event(a.eventId), { ...detail, attachments: upsertAttachment(detail.attachments, a) })
    }
    if (isNew) bumpCount(qc, a.eventId, 'attachments', 1)
    return
  }
  const key = keys.comments(a.eventId)
  const data = qc.getQueryData<CommentsData>(key)
  if (!data) return
  qc.setQueryData<CommentsData>(key, {
    comments: data.comments.map((c) => (c.id === a.commentId ? { ...c, attachments: upsertAttachment(c.attachments, a) } : c)),
  })
}

function applyAttachmentRemoved(qc: QueryClient, p: { id: string; eventId: string; commentId: string | null }) {
  if (p.commentId === null) {
    const detail = qc.getQueryData<EventDetail>(keys.event(p.eventId))
    const present = detail?.attachments.some((x) => x.id === p.id)
    if (detail && present) {
      qc.setQueryData<EventDetail>(keys.event(p.eventId), {
        ...detail,
        attachments: detail.attachments.filter((x) => x.id !== p.id),
      })
    }
    if (present || !detail) bumpCount(qc, p.eventId, 'attachments', -1)
    return
  }
  const key = keys.comments(p.eventId)
  const data = qc.getQueryData<CommentsData>(key)
  if (!data) return
  qc.setQueryData<CommentsData>(key, {
    comments: data.comments.map((c) => (c.id === p.commentId ? { ...c, attachments: c.attachments.filter((x) => x.id !== p.id) } : c)),
  })
}

function applyInboxItem(qc: QueryClient, p: { item: Notification; unread: number }) {
  const data = qc.getQueryData<InboxData>(keys.inbox)
  if (!data) {
    void qc.invalidateQueries({ queryKey: keys.inbox })
    return
  }
  const items = data.items.some((x) => x.id === p.item.id) ? data.items : [p.item, ...data.items]
  qc.setQueryData<InboxData>(keys.inbox, { items, unread: p.unread })
}

/** Apply one stream message to the query cache, following the table in docs/api.md. */
export function applyStreamMessage(qc: QueryClient, type: string, payload: unknown, ctx: ApplyContext = {}) {
  switch (type) {
    case 'event.created':
    case 'event.updated':
      applyEventUpsert(qc, payload as CalEvent, ctx)
      return
    case 'event.deleted':
      applyEventDeleted(qc, (payload as { id: string }).id, ctx)
      return
    case 'comment.created':
    case 'comment.updated':
      applyCommentUpsert(qc, payload as Comment)
      return
    case 'comment.deleted':
      applyCommentDeleted(qc, payload as { id: string; eventId: string })
      return
    case 'attachment.added':
    case 'attachment.scanned':
      applyAttachment(qc, payload as Attachment)
      return
    case 'attachment.removed':
      applyAttachmentRemoved(qc, payload as { id: string; eventId: string; commentId: string | null })
      return
    case 'suggestion.created':
    case 'suggestion.resolved': {
      const s = payload as Suggestion
      void qc.invalidateQueries({ queryKey: keys.suggestionsAll(s.eventId) })
      void qc.invalidateQueries({ queryKey: keys.event(s.eventId) })
      void qc.invalidateQueries({ queryKey: keys.history(s.eventId) })
      void qc.invalidateQueries({ queryKey: keys.eventsAll })
      return
    }
    case 'tag.changed':
      qc.setQueryData<{ tags: Tag[] }>(keys.tags, { tags: (payload as { tags: Tag[] }).tags })
      return
    case 'eventtype.changed': {
      const next = (payload as { eventTypes: EventType[] }).eventTypes
      const prev = qc.getQueryData<{ eventTypes: EventType[] }>(keys.eventTypes)?.eventTypes
      qc.setQueryData<{ eventTypes: EventType[] }>(keys.eventTypes, { eventTypes: next })
      // Changing a title template retitles that type's events on the server.
      if (prev && next.some((et) => et.titleTemplate && et.titleTemplate !== prev.find((p) => p.id === et.id)?.titleTemplate)) {
        void qc.invalidateQueries({ queryKey: keys.eventsAll })
      }
      return
    }
    case 'user.role_changed': {
      const p = payload as { userId: string }
      if (p.userId === ctx.meId) {
        void qc.invalidateQueries({ queryKey: keys.me })
        void qc.invalidateQueries({ queryKey: keys.eventsAll })
        void qc.invalidateQueries({ queryKey: keys.eventAll })
      }
      void qc.invalidateQueries({ queryKey: keys.users })
      return
    }
    case 'inbox.item':
      applyInboxItem(qc, payload as { item: Notification; unread: number })
      return
    case 'presence': {
      const p = payload as { eventId: string; viewers: PresenceViewer[] }
      ctx.onPresence?.(
        p.eventId,
        p.viewers.filter((v) => v.id !== ctx.meId),
      )
      return
    }
    case 'settings.changed':
      void qc.invalidateQueries({ queryKey: keys.me })
      void qc.invalidateQueries({ queryKey: keys.themesAll })
      void qc.invalidateQueries({ queryKey: keys.settings })
      return
    case 'resync':
      void qc.invalidateQueries()
      return
    default:
      return
  }
}

export const STREAM_TYPES = [
  'event.created',
  'event.updated',
  'event.deleted',
  'comment.created',
  'comment.updated',
  'comment.deleted',
  'attachment.added',
  'attachment.removed',
  'attachment.scanned',
  'suggestion.created',
  'suggestion.resolved',
  'tag.changed',
  'eventtype.changed',
  'user.role_changed',
  'inbox.item',
  'presence',
  'settings.changed',
  'resync',
] as const
