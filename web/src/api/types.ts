// Hand-written types mirroring docs/api.md. Once the backend publishes
// `openapi.json`, run `npm run gen:api` and reconcile against src/api/schema.d.ts.

type UUID = string
/** RFC 3339 instant in UTC, e.g. `2026-10-30T23:00:00Z`. */
type Instant = string

/** `system` marks the calendar itself, the owner of events whose type has no personal owner. */
export type UserRef = { id: UUID; name: string; avatarUrl: string; system?: boolean }

export type Role = 'viewer' | 'writer' | 'admin'

type Permissions = {
  edit: boolean
  delete: boolean
  suggest: boolean
  comment: boolean
  attach: boolean
  review: boolean
}

export type EventStatus = 'confirmed' | 'tentative'

/** Values of a typed event's fields, keyed by field key. Checkboxes are `true`, members are user ids; empty values are absent. */
export type EventFieldValues = Record<string, string | number | boolean | string[]>

export type CalEvent = {
  id: UUID
  title: string
  description: string
  location: string
  start: string
  end: string
  startUtc: Instant
  endUtc: Instant
  timeZone: string
  allDay: boolean
  rrule: string
  exdates: Instant[]
  status: EventStatus
  tags: UUID[]
  /** Set on typed events, which use `fields` instead of `description`. */
  eventTypeId: UUID | null
  fields: EventFieldValues
  owner: UserRef
  version: number
  permissions: Permissions
  counts: { comments: number; openSuggestions: number; attachments: number }
  createdAt: Instant
  updatedAt: Instant
  occurrence?: Instant | null
  /** Whole years since the series' first occurrence; set when the event's type has a year counter. */
  yearsSince?: number
  overridden?: boolean
  deletedAt?: Instant
}

type Override = {
  id: UUID
  /** This occurrence has its own type (`eventTypeId`, null = generic) and `fields`. */
  typeOverridden?: boolean
  eventTypeId?: UUID | null
  fields?: EventFieldValues
  recurrenceId: Instant
  cancelled: boolean
  title?: string
  description?: string
  location?: string
  start?: string
  end?: string
  startUtc?: Instant
  endUtc?: Instant
  allDay?: boolean
  status?: string
}

type ScanStatus = 'pending' | 'clean' | 'infected' | 'skipped'

export type Attachment = {
  id: UUID
  eventId: UUID
  commentId: UUID | null
  filename: string
  contentType: string
  size: number
  width: number | null
  height: number | null
  isImage: boolean
  url: string
  thumbUrl: string | null
  scanStatus: ScanStatus
  uploader: UserRef
  createdAt: Instant
  permissions: { delete: boolean }
}

export type EventDetail = CalEvent & {
  overrides: Override[]
  attachments: Attachment[]
}

export type Comment = {
  id: UUID
  eventId: UUID
  parentId: UUID | null
  author: UserRef
  body: string
  mentions: UUID[]
  createdAt: Instant
  editedAt: Instant | null
  deleted: boolean
  attachments: Attachment[]
  permissions: { edit: boolean; delete: boolean; attach: boolean }
}

export const SUGGESTION_FIELDS = [
  'title',
  'description',
  'location',
  'start',
  'end',
  'timeZone',
  'allDay',
  'rrule',
  'status',
  'tags',
  'eventTypeId',
  'fields',
] as const
export type SuggestionField = (typeof SUGGESTION_FIELDS)[number]

type SuggestionChange = {
  base: unknown
  proposed: unknown
  current: unknown
  conflict: boolean
}

type SuggestionStatus = 'open' | 'accepted' | 'partially_accepted' | 'rejected' | 'withdrawn' | 'closed'

export type Suggestion = {
  id: UUID
  eventId: UUID
  author: UserRef
  kind: 'edit' | 'delete'
  baseVersion: number
  changes: Partial<Record<SuggestionField, SuggestionChange>>
  status: SuggestionStatus
  reviewer: UserRef | null
  reviewNote: string
  acceptedFields: SuggestionField[]
  createdAt: Instant
  resolvedAt: Instant | null
  permissions: { review: boolean; withdraw: boolean }
}

export const TAG_SLOTS = ['tag-1', 'tag-2', 'tag-3', 'tag-4', 'tag-5', 'tag-6', 'tag-7', 'tag-8'] as const
export type TagSlot = (typeof TAG_SLOTS)[number]

export type Tag = {
  id: UUID
  name: string
  colorSlot: TagSlot
  sortOrder: number
}

export const FIELD_KINDS = ['text', 'longtext', 'number', 'date', 'time', 'select', 'url', 'checkbox', 'members'] as const
export type FieldKind = (typeof FIELD_KINDS)[number]

export type EventTypeField = {
  key: string
  label: string
  kind: FieldKind
  required?: boolean
  /** Choices for `select` fields. */
  options?: string[]
  /** `select` and `members` fields: several may be picked. Unset means one choice for select, several people for members. */
  multiple?: boolean
}

/** Keep in sync with `typeIcons` in internal/domain/eventtypes.go and src/lib/typeIcons.ts. */
export const EVENT_TYPE_STYLES = ['default', 'banner', 'outline', 'badge'] as const
export type EventTypeStyle = (typeof EVENT_TYPE_STYLES)[number]

export const TYPE_ICONS = [
  'calendar',
  'cake',
  'gift',
  'party-popper',
  'flag',
  'plane',
  'heart',
  'star',
  'briefcase',
  'graduation-cap',
  'utensils',
  'music',
  'trophy',
  'stethoscope',
  'home',
  'users',
  'bell',
] as const
export type TypeIcon = (typeof TYPE_ICONS)[number]

/** An admin-defined kind of event: calendar look, creation defaults and predefined fields. */
export type EventType = {
  id: UUID
  name: string
  icon: TypeIcon
  colorSlot: TagSlot
  /** How the calendar draws events of this type. */
  style: EventTypeStyle
  /** Show the years since the series' first occurrence (birthdays, anniversaries). */
  yearCounter: boolean
  defaultAllDay: boolean
  /** Length of new timed events in minutes; 0 for instants such as deadlines. */
  defaultDurationMinutes: number
  /** RRULE pre-filled for new events of this type, e.g. `FREQ=YEARLY`. */
  defaultRrule: string
  /** Events of this type never repeat; excludes `defaultRrule`. */
  /** With `defaultAllDay`: events last a single date rather than a range. */
  singleDay: boolean
  neverRepeats: boolean
  /** Only admins can create events of this type or switch events to it. */
  adminOnly: boolean
  /** Events of this type belong to the calendar, not a person; only admins edit them. */
  ownerless: boolean
  /** Generates event titles from `{{field_key}}` placeholders; empty means titles are typed by hand. */
  titleTemplate: string
  /** Guidance shown while creating an event of this type. */
  instructions: string
  fields: EventTypeField[]
  sortOrder: number
}

export type EventTypeWrite = Partial<
  Pick<
    EventType,
    | 'name'
    | 'icon'
    | 'colorSlot'
    | 'style'
    | 'yearCounter'
    | 'defaultAllDay'
    | 'defaultDurationMinutes'
    | 'defaultRrule'
    | 'singleDay'
    | 'neverRepeats'
    | 'adminOnly'
    | 'ownerless'
    | 'titleTemplate'
    | 'instructions'
    | 'fields'
  >
>

type NotificationKind = 'suggestion.created' | 'suggestion.resolved' | 'comment.reply' | 'comment.mention'

export type Notification = {
  id: UUID
  kind: NotificationKind
  subjectType: 'suggestion' | 'comment'
  subjectId: UUID
  eventId: UUID | null
  eventTitle: string
  actor: UserRef | null
  readAt: Instant | null
  createdAt: Instant
}

export type ActivityItem = {
  id: UUID
  actor: UserRef | null
  action: string
  subjectType: string
  subjectId: UUID
  eventId: UUID | null
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  createdAt: Instant
}

export type CalendarView = 'month' | 'week' | 'day' | 'agenda'
export const CALENDAR_VIEWS: readonly CalendarView[] = ['month', 'week', 'day', 'agenda']

export type ColorMode = 'light' | 'dark' | 'system'

export type Preferences = {
  themeId: string
  colorMode: ColorMode
  timeZone: string
  weekStart: 0 | 1 | 2 | 3 | 4 | 5 | 6
  clock24h: boolean
  defaultView: CalendarView
  density: 'comfortable' | 'compact'
}

export type ThemeTokens = Record<string, string>

export type Theme = {
  id: string
  name: string
  author: string
  builtin: boolean
  modes: { light: ThemeTokens; dark: ThemeTokens }
}

export type Me = {
  user: UserRef & { email: string; role: Role }
  preferences: Preferences
  csrfToken: string
  permissions: {
    createEvent: boolean
    attachToComments: boolean
    admin: boolean
  }
  instance: {
    calendarName: string
    defaultTimeZone: string
    accentColor: string
    hasLogo: boolean
    logoUrl: string
    limits: { maxSize: number; maxPerEvent: number; maxPerComment: number }
  }
}

export type FeedTokenStatus = {
  exists: boolean
  createdAt: Instant | null
  lastUsedAt: Instant | null
}

export type AdminUser = UserRef & {
  email: string
  role: Role
  roleSource: 'idp' | 'manual'
  active: boolean
  groups: string[]
  lastLoginAt: Instant | null
  sessionCount: number
  /** Personal data erased by an admin; the row only remains for history. */
  erased: boolean
}

export type EraseResult = {
  eventsTransferred: number
  commentsRemoved: number
  attachmentsRemoved: number
  suggestionsRemoved: number
}

export type TrashedAttachment = Attachment & {
  eventTitle: string
  deletedAt: Instant
}

export type Session = {
  id: string
  createdAt: Instant
  lastSeenAt: Instant
  expiresAt: Instant
  ip: string
  userAgent: string
  current: boolean
}

export type Settings = {
  calendarName: string
  defaultTimeZone: string
  defaultRole: Role
  accentColor: string
  enabledThemes: string[]
  attachmentMaxSize: number
  attachmentMaxPerEvent: number
  attachmentMaxPerComment: number
  attachmentQuota: number
  storageUsed: number
}

/** Body for POST /api/events and (as a partial) PATCH /api/events/{id}. */
export type EventWrite = {
  title: string
  start: string
  end: string
  timeZone: string
  allDay: boolean
  rrule?: string
  location?: string
  description?: string
  tags?: UUID[]
  status?: EventStatus
  /** Empty string turns a typed event back into a generic one. */
  eventTypeId?: UUID | ''
  /** Replaces all field values; only for typed events. */
  fields?: EventFieldValues
  /** Admins only: hand the event to another writer or admin. */
  ownerId?: UUID
}

export type EditScope = 'this' | 'following' | 'all'

export type PresenceViewer = UserRef & { state: 'viewing' | 'editing' }

export type IcsImportRequest = { ics: string; eventTypeId?: string; dryRun?: boolean }

export type IcsImportEntry = {
  uid: string
  title: string
  start: string
  end: string
  allDay: boolean
  rrule: string
  outcome: 'new' | 'duplicate' | 'invalid'
  error?: string
}

/** Outcome of an .ics import, or of its dry run. */
export type IcsImportResult = {
  dryRun: boolean
  created: number
  duplicates: number
  invalid: number
  ignored: number
  entries: IcsImportEntry[]
}
