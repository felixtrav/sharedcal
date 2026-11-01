import {
  CalendarDaysIcon,
  FileUpIcon,
  HistoryIcon,
  InboxIcon,
  LightbulbIcon,
  MessageSquarePlusIcon,
  RssIcon,
  Settings2Icon,
  ShapesIcon,
  SlidersHorizontalIcon,
  TagsIcon,
  Trash2Icon,
  UsersIcon,
  type LucideIcon,
} from 'lucide-react'

export type NavItem = {
  to:
    | '/'
    | '/inbox'
    | '/preferences'
    | '/subscribe'
    | '/activity'
    | '/catalog'
    | '/admin/requests'
    | '/admin/people'
    | '/admin/tags'
    | '/admin/event-types'
    | '/admin/import'
    | '/admin/settings'
    | '/admin/trash'
  labelKey: string
  icon: LucideIcon
  badge?: 'inbox' | 'requests'
}

export const MAIN_NAV: NavItem[] = [
  { to: '/', labelKey: 'nav.calendar', icon: CalendarDaysIcon },
  { to: '/inbox', labelKey: 'nav.inbox', icon: InboxIcon, badge: 'inbox' },
  { to: '/preferences', labelKey: 'nav.preferences', icon: SlidersHorizontalIcon },
  { to: '/subscribe', labelKey: 'nav.subscribe', icon: RssIcon },
]

/** Shown to non-admins (their own activity); admins get it in the admin group. */
export const MY_ACTIVITY_NAV: NavItem = { to: '/activity', labelKey: 'nav.myActivity', icon: HistoryIcon }

/** Shown to non-admins: browse tags and event types and suggest new ones. */
export const CATALOG_NAV: NavItem = { to: '/catalog', labelKey: 'nav.catalog', icon: LightbulbIcon }

export const ADMIN_NAV: NavItem[] = [
  { to: '/admin/people', labelKey: 'nav.people', icon: UsersIcon },
  { to: '/admin/tags', labelKey: 'nav.tags', icon: TagsIcon },
  { to: '/admin/event-types', labelKey: 'nav.eventTypes', icon: ShapesIcon },
  { to: '/admin/requests', labelKey: 'nav.requests', icon: MessageSquarePlusIcon, badge: 'requests' },
  { to: '/admin/import', labelKey: 'nav.import', icon: FileUpIcon },
  { to: '/activity', labelKey: 'nav.activity', icon: HistoryIcon },
  { to: '/admin/settings', labelKey: 'nav.settings', icon: Settings2Icon },
  { to: '/admin/trash', labelKey: 'nav.trash', icon: Trash2Icon },
]
