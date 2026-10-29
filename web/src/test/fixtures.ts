import type { CalEvent, Comment, EventDetail } from '@/api/types'

export function makeEvent(overrides: Partial<CalEvent> = {}): CalEvent {
  return {
    id: 'ev1',
    title: 'Board game night',
    description: '',
    location: '',
    start: '2026-10-30T19:00:00',
    end: '2026-10-30T23:00:00',
    startUtc: '2026-10-30T23:00:00Z',
    endUtc: '2026-10-31T03:00:00Z',
    timeZone: 'America/New_York',
    allDay: false,
    rrule: '',
    exdates: [],
    status: 'confirmed',
    tags: [],
    eventTypeId: null,
    fields: {},
    owner: { id: 'u-dana', name: 'Dana', avatarUrl: '' },
    version: 1,
    permissions: { edit: true, delete: true, suggest: false, comment: true, attach: true, review: true },
    counts: { comments: 0, openSuggestions: 0, attachments: 0 },
    createdAt: '2026-10-01T00:00:00Z',
    updatedAt: '2026-10-01T00:00:00Z',
    occurrence: null,
    overridden: false,
    ...overrides,
  }
}

export function makeDetail(overrides: Partial<EventDetail> = {}): EventDetail {
  return { ...makeEvent(overrides), overrides: [], attachments: [], ...overrides }
}

export function makeComment(overrides: Partial<Comment> = {}): Comment {
  return {
    id: 'c1',
    eventId: 'ev1',
    parentId: null,
    author: { id: 'u-bob', name: 'Bob', avatarUrl: '' },
    body: 'Hello',
    mentions: [],
    createdAt: '2026-10-20T10:00:00Z',
    editedAt: null,
    deleted: false,
    attachments: [],
    permissions: { edit: true, delete: true, attach: true },
    ...overrides,
  }
}
