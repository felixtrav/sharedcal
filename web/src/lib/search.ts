import { CALENDAR_VIEWS, type CalendarView } from '@/api/types'
import { isDateString } from './time'

const PANEL_TABS = ['details', 'comments', 'suggestions', 'history'] as const
export type PanelTab = (typeof PANEL_TABS)[number]

type CalendarSearch = {
  view?: CalendarView
  date?: string
  event?: string
  occurrence?: string
  tab?: PanelTab
  q?: string
  tags?: string[]
  owners?: string[]
}

const str = (v: unknown) => (typeof v === 'string' && v.length > 0 && v.length < 300 ? v : undefined)
const strList = (v: unknown) => {
  const list = Array.isArray(v) ? v : typeof v === 'string' ? v.split(',') : []
  const out = list.filter((x): x is string => typeof x === 'string' && x.length > 0 && x.length < 100).slice(0, 50)
  return out.length ? out : undefined
}

export function validateCalendarSearch(raw: Record<string, unknown>): CalendarSearch {
  const view = CALENDAR_VIEWS.includes(raw.view as CalendarView) ? (raw.view as CalendarView) : undefined
  const date = typeof raw.date === 'string' && isDateString(raw.date) ? raw.date : undefined
  const tab = PANEL_TABS.includes(raw.tab as PanelTab) ? (raw.tab as PanelTab) : undefined
  return {
    view,
    date,
    event: str(raw.event),
    occurrence: str(raw.occurrence),
    tab,
    q: str(raw.q),
    tags: strList(raw.tags),
    owners: strList(raw.owners),
  }
}
