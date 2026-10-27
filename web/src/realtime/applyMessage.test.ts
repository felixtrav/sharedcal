import { QueryClient } from '@tanstack/react-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { RangeParams } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { CalEvent, Comment, EventDetail, Notification, Tag } from '@/api/types'
import { makeComment, makeDetail, makeEvent } from '@/test/fixtures'
import { applyStreamMessage, matchesFilters, overlaps } from './applyMessage'

const oct: RangeParams = { from: '2026-09-28T04:00:00Z', to: '2026-11-09T05:00:00Z' }
const nov: RangeParams = { from: '2026-11-09T05:00:00Z', to: '2026-12-14T05:00:00Z' }

type EventsData = { events: CalEvent[] }
const events = (qc: QueryClient, p: RangeParams) => qc.getQueryData<EventsData>(keys.events(p))?.events ?? []

describe('overlaps / filters', () => {
  it('detects overlap with half-open ranges', () => {
    expect(overlaps(makeEvent(), oct)).toBe(true)
    expect(overlaps(makeEvent(), nov)).toBe(false)
    expect(overlaps({ startUtc: nov.to, endUtc: '2026-12-15T00:00:00Z' }, nov)).toBe(false)
    expect(overlaps({ startUtc: nov.from, endUtc: nov.from }, nov)).toBe(true)
  })

  it('matches tag and owner filters locally, defers free text', () => {
    const ev = makeEvent({ tags: ['t1'] })
    expect(matchesFilters(ev, { ...oct, tag: ['t1'] })).toBe(true)
    expect(matchesFilters(ev, { ...oct, tag: ['t2'] })).toBe(false)
    expect(matchesFilters(ev, { ...oct, owner: ['someone'] })).toBe(false)
    expect(matchesFilters(ev, { ...oct, q: 'game' })).toBeNull()
  })
})

describe('applyStreamMessage', () => {
  let qc: QueryClient
  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    qc.setQueryData<EventsData>(keys.events(oct), { events: [makeEvent()] })
    qc.setQueryData<EventsData>(keys.events(nov), { events: [] })
  })

  it('patches an updated event in place and highlights it', () => {
    const onRemoteChange = vi.fn()
    applyStreamMessage(qc, 'event.updated', makeEvent({ title: 'Renamed', version: 2 }), { onRemoteChange })
    expect(events(qc, oct)[0]?.title).toBe('Renamed')
    expect(onRemoteChange).toHaveBeenCalledWith('ev1')
  })

  it('ignores stale versions', () => {
    qc.setQueryData<EventsData>(keys.events(oct), { events: [makeEvent({ version: 5, title: 'New' })] })
    const onRemoteChange = vi.fn()
    applyStreamMessage(qc, 'event.updated', makeEvent({ version: 4, title: 'Old' }), { onRemoteChange })
    expect(events(qc, oct)[0]?.title).toBe('New')
    expect(onRemoteChange).not.toHaveBeenCalled()
  })

  it('does not highlight own writes', () => {
    const onRemoteChange = vi.fn()
    applyStreamMessage(qc, 'event.updated', makeEvent({ version: 2 }), { onRemoteChange, isOwnWrite: () => true })
    expect(onRemoteChange).not.toHaveBeenCalled()
  })

  it('moves an event between ranges', () => {
    const moved = makeEvent({ version: 2, startUtc: '2026-11-20T23:00:00Z', endUtc: '2026-11-21T01:00:00Z' })
    applyStreamMessage(qc, 'event.updated', moved)
    expect(events(qc, oct)).toHaveLength(0)
    expect(events(qc, nov)).toHaveLength(1)
  })

  it('adds created events only to overlapping ranges', () => {
    applyStreamMessage(qc, 'event.created', makeEvent({ id: 'ev2' }))
    expect(events(qc, oct).map((e) => e.id)).toEqual(['ev1', 'ev2'])
    expect(events(qc, nov)).toHaveLength(0)
  })

  it('removes events that no longer match a tag filter', () => {
    const filtered: RangeParams = { ...oct, tag: ['t1'] }
    qc.setQueryData<EventsData>(keys.events(filtered), { events: [makeEvent({ tags: ['t1'] })] })
    applyStreamMessage(qc, 'event.updated', makeEvent({ version: 2, tags: ['t2'] }))
    expect(events(qc, filtered)).toHaveLength(0)
  })

  it('invalidates ranges for recurring events instead of patching', () => {
    const spy = vi.spyOn(qc, 'invalidateQueries')
    applyStreamMessage(qc, 'event.updated', makeEvent({ version: 2, rrule: 'FREQ=WEEKLY' }))
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.eventsAll })
  })

  it('invalidates free-text searches', () => {
    const searched: RangeParams = { ...oct, q: 'game' }
    qc.setQueryData<EventsData>(keys.events(searched), { events: [] })
    const spy = vi.spyOn(qc, 'invalidateQueries')
    applyStreamMessage(qc, 'event.created', makeEvent({ id: 'ev3' }))
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.events(searched), exact: true })
  })

  it('merges updates into the open event detail, keeping attachments', () => {
    qc.setQueryData<EventDetail>(keys.event('ev1'), makeDetail({ attachments: [] }))
    applyStreamMessage(qc, 'event.updated', makeEvent({ version: 3, location: 'Park' }))
    const d = qc.getQueryData<EventDetail>(keys.event('ev1'))
    expect(d?.location).toBe('Park')
    expect(d?.attachments).toEqual([])
  })

  it('removes deleted events everywhere and notifies', () => {
    const onEventDeleted = vi.fn()
    applyStreamMessage(qc, 'event.deleted', { id: 'ev1' }, { onEventDeleted })
    expect(events(qc, oct)).toHaveLength(0)
    expect(onEventDeleted).toHaveBeenCalledWith('ev1')
  })

  it('appends, updates and removes comments, keeping counts in step', () => {
    qc.setQueryData(keys.comments('ev1'), { comments: [] as Comment[] })
    applyStreamMessage(qc, 'comment.created', makeComment())
    applyStreamMessage(qc, 'comment.created', makeComment()) // replay is idempotent
    expect(qc.getQueryData<{ comments: Comment[] }>(keys.comments('ev1'))?.comments).toHaveLength(1)
    expect(events(qc, oct)[0]?.counts.comments).toBe(1)

    applyStreamMessage(qc, 'comment.updated', makeComment({ body: 'Edited', editedAt: '2026-10-21T00:00:00Z' }))
    expect(qc.getQueryData<{ comments: Comment[] }>(keys.comments('ev1'))?.comments[0]?.body).toBe('Edited')

    applyStreamMessage(qc, 'comment.deleted', { id: 'c1', eventId: 'ev1' })
    applyStreamMessage(qc, 'comment.deleted', { id: 'c1', eventId: 'ev1' })
    expect(qc.getQueryData<{ comments: Comment[] }>(keys.comments('ev1'))?.comments).toHaveLength(0)
    expect(events(qc, oct)[0]?.counts.comments).toBe(0)
  })

  it('patches event and comment attachments', () => {
    qc.setQueryData<EventDetail>(keys.event('ev1'), makeDetail())
    qc.setQueryData(keys.comments('ev1'), { comments: [makeComment()] })
    const att = {
      id: 'a1', eventId: 'ev1', commentId: null, filename: 'flyer.pdf', contentType: 'application/pdf', size: 10,
      width: null, height: null, isImage: false, url: '/x', thumbUrl: null, scanStatus: 'pending' as const,
      uploader: { id: 'u', name: 'U', avatarUrl: '' }, createdAt: '2026-10-01T00:00:00Z', permissions: { delete: true },
    }
    applyStreamMessage(qc, 'attachment.added', att)
    applyStreamMessage(qc, 'attachment.scanned', { ...att, scanStatus: 'clean' })
    const d = qc.getQueryData<EventDetail>(keys.event('ev1'))
    expect(d?.attachments).toHaveLength(1)
    expect(d?.attachments[0]?.scanStatus).toBe('clean')

    applyStreamMessage(qc, 'attachment.added', { ...att, id: 'a2', commentId: 'c1' })
    expect(qc.getQueryData<{ comments: Comment[] }>(keys.comments('ev1'))?.comments[0]?.attachments).toHaveLength(1)

    applyStreamMessage(qc, 'attachment.removed', { id: 'a1', eventId: 'ev1', commentId: null })
    expect(qc.getQueryData<EventDetail>(keys.event('ev1'))?.attachments).toHaveLength(0)
    applyStreamMessage(qc, 'attachment.removed', { id: 'a2', eventId: 'ev1', commentId: 'c1' })
    expect(qc.getQueryData<{ comments: Comment[] }>(keys.comments('ev1'))?.comments[0]?.attachments).toHaveLength(0)
  })

  it('invalidates suggestions on suggestion messages', () => {
    const spy = vi.spyOn(qc, 'invalidateQueries')
    applyStreamMessage(qc, 'suggestion.created', { id: 's1', eventId: 'ev1', eventOwnerId: 'u-dana' })
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.suggestionsAll('ev1') })
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.event('ev1') })
  })

  it('replaces tags', () => {
    const tags: Tag[] = [{ id: 't', name: 'Social', colorSlot: 'tag-3', sortOrder: 0 }]
    applyStreamMessage(qc, 'tag.changed', { tags })
    expect(qc.getQueryData(keys.tags)).toEqual({ tags })
  })

  it('prepends inbox items and updates the badge', () => {
    qc.setQueryData(keys.inbox, { items: [] as Notification[], unread: 0 })
    const item: Notification = {
      id: 'n1', kind: 'comment.mention', subjectType: 'comment', subjectId: 'c1', eventId: 'ev1', eventTitle: 'X',
      actor: null, readAt: null, createdAt: '2026-10-01T00:00:00Z',
    }
    applyStreamMessage(qc, 'inbox.item', { item, unread: 3 })
    expect(qc.getQueryData(keys.inbox)).toEqual({ items: [item], unread: 3 })
  })

  it('refetches me when my role changes', () => {
    const spy = vi.spyOn(qc, 'invalidateQueries')
    applyStreamMessage(qc, 'user.role_changed', { userId: 'me', role: 'viewer', active: true }, { meId: 'me' })
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.me })
    expect(spy).toHaveBeenCalledWith({ queryKey: keys.eventsAll })
  })

  it('reports presence without me', () => {
    const onPresence = vi.fn()
    applyStreamMessage(
      qc,
      'presence',
      { eventId: 'ev1', viewers: [{ id: 'me', name: 'Me', avatarUrl: '', state: 'viewing' }, { id: 'd', name: 'Dana', avatarUrl: '', state: 'editing' }] },
      { meId: 'me', onPresence },
    )
    expect(onPresence).toHaveBeenCalledWith('ev1', [{ id: 'd', name: 'Dana', avatarUrl: '', state: 'editing' }])
  })

  it('invalidates everything on resync', () => {
    const spy = vi.spyOn(qc, 'invalidateQueries')
    applyStreamMessage(qc, 'resync', {})
    expect(spy).toHaveBeenCalledWith()
  })
})
