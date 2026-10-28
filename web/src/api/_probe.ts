import type { components } from './schema'
import type * as T from './types'
type S = components['schemas']
declare const ev: S['EventDTO']
export const e1: Omit<T.CalEvent, 'occurrence' | 'overridden' | 'deletedAt'> = ev
declare const at: S['AttachmentDTO']
export const a1: T.Attachment = at
declare const cm: S['CommentDTO']
export const c1: T.Comment = cm
declare const sg: S['SuggestionDTO']
export const s1: Omit<T.Suggestion, 'changes'> = sg
declare const no: S['NotificationDTO']
export const n1: T.Notification = no
declare const pr: S['Preferences']
export const p1: T.Preferences = pr
declare const au: S['AdminUser']
export const u1: T.AdminUser = au
