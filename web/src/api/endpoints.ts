import { api, upload } from './client'
import type {
  ActivityItem,
  AdminUser,
  Attachment,
  CalEvent,
  Comment,
  EditScope,
  EraseResult,
  EventDetail,
  EventWrite,
  FeedTokenStatus,
  IcsImportRequest,
  IcsImportResult,
  Me,
  Notification,
  Preferences,
  Role,
  Session,
  Settings,
  Suggestion,
  SuggestionField,
  EventType,
  EventTypeWrite,
  Tag,
  TrashedAttachment,
  TagSlot,
  Theme,
  UserRef,
} from './types'

export type RangeParams = {
  from: string
  to: string
  tag?: string[]
  type?: string[]
  owner?: string[]
  q?: string
}

type ScopeParams = { scope?: EditScope; occurrence?: string | null }

const enc = encodeURIComponent

function scopeQuery(s?: ScopeParams) {
  if (!s?.scope) return undefined
  return { scope: s.scope, occurrence: s.occurrence ?? undefined }
}

export const endpoints = {
  me: () => api.get<Me>('/api/me'),
  /** Probe without redirecting on 401. */
  probeMe: () => api.get<Me>('/api/me', { noAuthRedirect: true }),
  logout: () => api.post<{ logoutUrl: string | null }>('/auth/logout'),
  savePreferences: (p: Preferences) => api.put<Preferences>('/api/me/preferences', p),

  feedToken: () => api.get<FeedTokenStatus>('/api/me/feed-token'),
  createFeedToken: () => api.post<{ url: string }>('/api/me/feed-token'),
  revokeFeedToken: () => api.del('/api/me/feed-token'),

  events: (p: RangeParams, signal?: AbortSignal) => api.get<{ events: CalEvent[] }>('/api/events', { query: p, signal }),
  event: (id: string) => api.get<EventDetail>(`/api/events/${enc(id)}`),
  createEvent: (body: EventWrite) => api.post<CalEvent>('/api/events', body),
  updateEvent: (id: string, version: number, body: Partial<EventWrite>, scope?: ScopeParams) =>
    api.patch<CalEvent>(`/api/events/${enc(id)}`, body, {
      headers: { 'If-Match': String(version) },
      query: scopeQuery(scope),
    }),
  deleteEvent: (id: string, version?: number, scope?: ScopeParams) =>
    api.del(`/api/events/${enc(id)}`, {
      headers: version !== undefined ? { 'If-Match': String(version) } : undefined,
      query: scopeQuery(scope),
    }),
  watchEvent: (id: string) => api.put<void>(`/api/events/${enc(id)}/watch`),
  unwatchEvent: (id: string) => api.del(`/api/events/${enc(id)}/watch`),
  restoreEvent: (id: string) => api.post<CalEvent>(`/api/events/${enc(id)}/restore`),
  trash: () => api.get<{ events: (CalEvent & { deletedAt: string })[] }>('/api/trash'),
  history: (id: string) => api.get<{ items: ActivityItem[] }>(`/api/events/${enc(id)}/history`),

  comments: (eventId: string) => api.get<{ comments: Comment[] }>(`/api/events/${enc(eventId)}/comments`),
  createComment: (eventId: string, body: { body: string; parentId?: string; mentions?: string[] }) =>
    api.post<Comment>(`/api/events/${enc(eventId)}/comments`, body),
  updateComment: (id: string, body: { body: string; mentions?: string[] }) => api.patch<Comment>(`/api/comments/${enc(id)}`, body),
  deleteComment: (id: string) => api.del(`/api/comments/${enc(id)}`),

  suggestions: (eventId: string, status: 'open' | 'all' = 'open') =>
    api.get<{ suggestions: Suggestion[] }>(`/api/events/${enc(eventId)}/suggestions`, { query: { status } }),
  suggestEdit: (eventId: string, baseVersion: number, changes: Partial<EventWrite>) =>
    api.post<Suggestion>(`/api/events/${enc(eventId)}/suggestions`, {
      kind: 'edit',
      baseVersion,
      changes,
    }),
  suggestDelete: (eventId: string, baseVersion: number) =>
    api.post<Suggestion>(`/api/events/${enc(eventId)}/suggestions`, {
      kind: 'delete',
      baseVersion,
    }),
  acceptSuggestion: (
    id: string,
    body: {
      fields?: SuggestionField[]
      resolutions?: Partial<Record<SuggestionField, 'current' | 'proposed'>>
    },
  ) => api.post<{ suggestion: Suggestion; event: CalEvent | null }>(`/api/suggestions/${enc(id)}/accept`, body),
  rejectSuggestion: (id: string, note?: string) => api.post<Suggestion>(`/api/suggestions/${enc(id)}/reject`, note ? { note } : {}),
  withdrawSuggestion: (id: string) => api.post<Suggestion>(`/api/suggestions/${enc(id)}/withdraw`),

  uploadEventAttachment: (eventId: string, file: File, onProgress?: (f: number) => void) =>
    upload<Attachment>(`/api/events/${enc(eventId)}/attachments`, file, onProgress),
  uploadCommentAttachment: (commentId: string, file: File, onProgress?: (f: number) => void) =>
    upload<Attachment>(`/api/comments/${enc(commentId)}/attachments`, file, onProgress),
  deleteAttachment: (id: string) => api.del(`/api/attachments/${enc(id)}`),
  restoreAttachment: (id: string) => api.post<Attachment>(`/api/attachments/${enc(id)}/restore`),
  attachmentTrash: () => api.get<{ attachments: TrashedAttachment[] }>('/api/trash/attachments'),

  inbox: (params: { unread?: boolean; before?: string; limit?: number } = {}) =>
    api.get<{ items: Notification[]; unread: number }>('/api/inbox', {
      query: { limit: 50, ...params },
    }),
  markRead: (body: { ids?: string[]; all?: boolean }) => api.post<{ unread: number }>('/api/inbox/read', body),
  presence: (eventId: string, state: 'viewing' | 'editing' | 'left') => api.post<void>('/api/presence', { eventId, state }),

  tags: () => api.get<{ tags: Tag[] }>('/api/tags'),
  createTag: (body: { name: string; colorSlot: TagSlot }) => api.post<Tag>('/api/tags', body),
  updateTag: (id: string, body: { name?: string; colorSlot?: TagSlot }) => api.patch<Tag>(`/api/tags/${enc(id)}`, body),
  deleteTag: (id: string) => api.del(`/api/tags/${enc(id)}`),
  reorderTags: (ids: string[]) => api.post<{ tags: Tag[] }>('/api/tags/reorder', { ids }),

  eventTypes: () => api.get<{ eventTypes: EventType[] }>('/api/event-types'),
  importIcs: (body: IcsImportRequest) => api.post<IcsImportResult>('/api/import/ics', body),
  createEventType: (body: EventTypeWrite & Pick<EventType, 'name' | 'colorSlot'>) => api.post<EventType>('/api/event-types', body),
  updateEventType: (id: string, body: EventTypeWrite) => api.patch<EventType>(`/api/event-types/${enc(id)}`, body),
  deleteEventType: (id: string) => api.del(`/api/event-types/${enc(id)}`),
  reorderEventTypes: (ids: string[]) => api.post<{ eventTypes: EventType[] }>('/api/event-types/reorder', { ids }),

  directory: () => api.get<{ users: UserRef[] }>('/api/users/directory'),
  users: () => api.get<{ users: AdminUser[] }>('/api/users'),
  updateUser: (id: string, body: { role?: Role; roleSource?: 'idp' | 'manual'; active?: boolean }) => api.patch<AdminUser>(`/api/users/${enc(id)}`, body),
  sessions: (userId: string) => api.get<{ sessions: Session[] }>(`/api/users/${enc(userId)}/sessions`),
  revokeSessions: (userId: string, sessionId?: string) => api.del(`/api/users/${enc(userId)}/sessions`, { query: { id: sessionId } }),
  transferOwnership: (userId: string, toUserId: string) =>
    api.post<{ moved: number }>(`/api/users/${enc(userId)}/transfer`, {
      toUserId,
    }),
  eraseUser: (userId: string, transferTo?: string) =>
    api.del<EraseResult>(`/api/users/${enc(userId)}`, {
      query: { transferTo },
    }),

  activity: (params: { actor?: string; action?: string; subjectType?: string; eventId?: string; before?: string; limit?: number }) =>
    api.get<{ items: ActivityItem[] }>('/api/activity', {
      query: { limit: 50, ...params },
    }),

  settings: () => api.get<Settings>('/api/settings'),
  saveSettings: (s: Settings) => api.put<Settings>('/api/settings', s),
  uploadLogo: (file: File, onProgress?: (f: number) => void) => upload<{ logoUrl: string }>('/api/settings/logo', file, onProgress),
  deleteLogo: () => api.del('/api/settings/logo'),

  themes: (all = false) =>
    api.get<{ themes: Theme[] }>('/api/themes', {
      query: { all: all || undefined },
    }),
  uploadTheme: (theme: Omit<Theme, 'builtin'>) => api.post<Theme>('/api/themes', theme),
  deleteTheme: (id: string) => api.del(`/api/themes/${enc(id)}`),
}
