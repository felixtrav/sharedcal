/**
 * Compile-time contract between the hand-written client types and the
 * server's generated OpenAPI schema (`npm run gen:api`). `tsc` fails here if
 * the backend changes a response the UI relies on, or the UI sends a body the
 * server would not accept. This module has no runtime code.
 */
import type { components } from './schema'
import type * as T from './types'

type S = components['schemas']
type Assert<Cond extends true> = Cond
type Extends<A, B> = [A] extends [B] ? true : false

// Responses: what the server sends must satisfy what the UI reads.
export type ResponseContract = [
  // `fields` is an open map on the server (its shape depends on the event type), so the UI narrows it itself.
  Assert<Extends<S['EventDTO'], Omit<T.CalEvent, 'occurrence' | 'overridden' | 'deletedAt' | 'fields'>>>,
  Assert<Extends<S['AttachmentDTO'], T.Attachment>>,
  Assert<Extends<S['CommentDTO'], T.Comment>>,
  Assert<Extends<S['SuggestionDTO'], Omit<T.Suggestion, 'changes'>>>,
  Assert<Extends<S['NotificationDTO'], T.Notification>>,
  Assert<Extends<S['Preferences'], T.Preferences>>,
  Assert<Extends<S['AdminUser'], T.AdminUser>>,
  Assert<Extends<S['SessionInfo'], T.Session>>,
  Assert<Extends<S['FeedStatus'], T.FeedTokenStatus>>,
]

// Requests: what the UI sends must be accepted by the server.
export type RequestContract = [Assert<Extends<T.EventWrite, S['EventInput']>>, Assert<Extends<T.Preferences, S['Preferences']>>]
