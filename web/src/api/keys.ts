import type { RangeParams } from './endpoints'

/** Every TanStack Query key in the app, in one place. */
export const keys = {
  me: ['me'] as const,
  eventsAll: ['events'] as const,
  events: (p: RangeParams) => ['events', p] as const,
  eventAll: ['event'] as const,
  event: (id: string) => ['event', id] as const,
  comments: (eventId: string) => ['comments', eventId] as const,
  suggestionsAll: (eventId: string) => ['suggestions', eventId] as const,
  suggestions: (eventId: string, status: 'open' | 'all') => ['suggestions', eventId, status] as const,
  history: (eventId: string) => ['history', eventId] as const,
  tags: ['tags'] as const,
  directory: ['directory'] as const,
  inbox: ['inbox'] as const,
  themes: (all: boolean) => ['themes', all] as const,
  themesAll: ['themes'] as const,
  users: ['users'] as const,
  sessions: (userId: string) => ['sessions', userId] as const,
  activity: (filters: Record<string, string | undefined>) => ['activity', filters] as const,
  settings: ['settings'] as const,
  trash: ['trash'] as const,
  attachmentTrash: ['trash', 'attachments'] as const,
  feedToken: ['feedToken'] as const,
}
