import {
  type CalendarRef,
  type DateSelectInfo,
  type DatesSetInfo,
  type EventClickInfo,
  type EventDropInfo,
  type EventResizeDoneInfo,
  useCalendarController,
} from '@fullcalendar/react'
import dayGridPlugin from '@fullcalendar/react/daygrid'
import interactionPlugin from '@fullcalendar/react/interaction'
import listPlugin from '@fullcalendar/react/list'
import timeGridPlugin from '@fullcalendar/react/timegrid'
import { useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { XIcon } from 'lucide-react'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { isApiError } from '@/api/client'
import { patchEventInCache, restoreSnapshot, snapshotEvent, storeEvent } from '@/api/eventCache'
import { endpoints, type RangeParams } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useEvents, useMeData, useTags } from '@/api/queries'
import type { CalEvent, CalendarView, EditScope, EventWrite, Me, Tag } from '@/api/types'
import type { PanelMode } from '@/components/event/EventPanel'
import { useScopePrompt } from '@/components/event/ScopeDialog'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { eventWhen } from '@/lib/eventText'
import { useHotkeys } from '@/lib/hotkeys'
import { useStore } from '@/lib/store'
import { addDays, dateInZone, displayTimeZone, fromWall, toUtcString, toWall, wallToIso, type FormatOptions } from '@/lib/time'
import { useIsMobile } from '@/lib/useMediaQuery'
import { clearOwnWrite, glowStore, markOwnWrite } from '@/realtime/stores'
import { CalendarSkeleton } from './CalendarSkeleton'
import { CalendarToolbar, type Filters } from './CalendarToolbar'
import { EventCalendarViews } from './event-calendar-views'
import { ghostInput, renderEventContent, toEventInput, type ChipProps, type Ghost } from './eventInput'
import { QuickCreatePopover, type Slot } from './QuickCreatePopover'

const route = getRouteApi('/')

// The side panel (forms, Zod, React Hook Form, comments, suggestions) loads on first open.
const EventPanel = lazy(() => import('@/components/event/EventPanel').then((m) => ({ default: m.EventPanel })))

const FC_VIEW: Record<CalendarView, string> = {
  month: 'dayGridMonth',
  week: 'timeGridWeek',
  day: 'timeGridDay',
  agenda: 'agenda',
}
const fromFcView = (type: string): CalendarView =>
  (Object.entries(FC_VIEW).find(([, v]) => v === type)?.[0] as CalendarView | undefined) ?? 'month'

const plugins = [dayGridPlugin, timeGridPlugin, listPlugin, interactionPlugin]

type GhostState = Ghost & { body: Partial<EventWrite> }

export function CalendarPage() {
  const { t } = useTranslation()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const me = useMeData()
  const qc = useQueryClient()
  const isMobile = useIsMobile()
  const prefs = me.preferences
  const tz = displayTimeZone(prefs.timeZone)
  const fmt: FormatOptions = useMemo(() => ({ timeZone: tz, clock24h: prefs.clock24h }), [tz, prefs.clock24h])
  const view: CalendarView = search.view ?? (isMobile ? 'agenda' : prefs.defaultView)

  const calRef = useRef<CalendarRef>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const controller = useCalendarController()
  const scopePrompt = useScopePrompt()

  const [range, setRange] = useState<{ from: string; to: string } | null>(null)
  const [slot, setSlot] = useState<Slot | null>(null)
  const [ghost, setGhost] = useState<GhostState | null>(null)
  const [draft, setDraft] = useState<PanelMode | null>(null)
  const [creating, setCreating] = useState(false)

  const filters: Filters = useMemo(() => ({ tags: search.tags ?? [], owners: search.owners ?? [] }), [search.tags, search.owners])
  const params: RangeParams | null = range
    ? {
        from: range.from,
        to: range.to,
        ...(filters.tags.length ? { tag: filters.tags } : {}),
        ...(filters.owners.length ? { owner: filters.owners } : {}),
        ...(search.q ? { q: search.q } : {}),
      }
    : null
  const eventsQ = useEvents(params)
  const { data: tagsData } = useTags()
  const tagsById = useMemo(() => new Map<string, Tag>((tagsData?.tags ?? []).map((tg) => [tg.id, tg])), [tagsData])
  const glowing = useStore(glowStore)

  const events = useMemo(() => {
    const list = (eventsQ.data?.events ?? []).map((ev) => toEventInput(ev, { tagsById, glowing, fmt, t }))
    if (ghost) list.push(ghostInput(ghost, { tagsById, t }))
    return list
  }, [eventsQ.data, tagsById, glowing, fmt, t, ghost])

  const api = () => calRef.current?.getApi()

  // --- URL ⇄ calendar --------------------------------------------------------

  const onDatesSet = useCallback(
    (info: DatesSetInfo) => {
      setRange({ from: toUtcString(info.view.activeStart), to: toUtcString(info.view.activeEnd) })
      const a = calRef.current?.getApi()
      const date = a ? dateInZone(a.getDate().getTime(), tz) : undefined
      const v = fromFcView(info.view.type)
      if (v !== search.view || date !== search.date) {
        void navigate({ search: (prev) => ({ ...prev, view: v, date }), replace: true })
      }
    },
    [navigate, search.view, search.date, tz],
  )

  // Apply URL changes (back/forward, deep links) to the calendar.
  useEffect(() => {
    const a = api()
    if (!a) return
    const wantType = FC_VIEW[view]
    const currentDate = dateInZone(a.getDate().getTime(), tz)
    if (a.view.type !== wantType) a.changeView(wantType, search.date ?? undefined)
    else if (search.date && search.date !== currentDate) a.gotoDate(search.date)
  }, [view, search.date, tz])

  const changeView = (v: CalendarView) => {
    api()?.changeView(FC_VIEW[v])
    if (v !== prefs.defaultView && !(isMobile && v === 'agenda')) void savePreferredView(qc, me, v)
  }

  // --- Panel -----------------------------------------------------------------

  const panel: PanelMode | null = search.event
    ? { kind: 'view', id: search.event, occurrence: search.occurrence ?? null }
    : draft

  const closePanel = () => {
    setDraft(null)
    if (search.event) {
      void navigate({ search: (prev) => ({ ...prev, event: undefined, occurrence: undefined, tab: undefined }) })
    }
  }

  const openNew = (init?: Partial<{ title: string; start: string; end: string; allDay: boolean }>) => {
    void navigate({ search: (prev) => ({ ...prev, event: undefined, occurrence: undefined, tab: undefined }) })
    const base = defaultNewTimes(tz, search.date)
    setDraft({
      kind: 'create',
      draft: {
        title: init?.title ?? '',
        allDay: init?.allDay ?? false,
        start: init?.start ?? base.start,
        end: init?.end ?? base.end,
        timeZone: tz,
      },
    })
  }

  // --- Keyboard --------------------------------------------------------------

  useHotkeys({
    t: () => api()?.today(),
    ArrowLeft: () => api()?.prev(),
    ArrowRight: () => api()?.next(),
    m: () => changeView('month'),
    w: () => changeView('week'),
    d: () => changeView('day'),
    a: () => changeView('agenda'),
    n: () => me.permissions.createEvent && openNew(),
    '/': () => searchRef.current?.focus(),
    Escape: () => {
      if (slot) setSlot(null)
      else if (panel) closePanel()
    },
  })

  // --- Interactions ----------------------------------------------------------

  const onEventClick = (info: EventClickInfo) => {
    info.jsEvent.preventDefault()
    const props = info.event.extendedProps as ChipProps
    if (!props.ev) return
    setDraft(null)
    void navigate({
      search: (prev) => ({ ...prev, event: props.ev?.id, occurrence: props.ev?.occurrence ?? undefined, tab: undefined }),
    })
  }

  const onSelect = (info: DateSelectInfo) => {
    if (!me.permissions.createEvent) return
    const allDay = info.allDay
    const start = allDay ? info.startStr.slice(0, 10) : toUtcString(info.start)
    const end = allDay ? info.endStr.slice(0, 10) : toUtcString(info.end)
    const js = info.jsEvent
    const x = js?.clientX ?? window.innerWidth / 2
    const y = js?.clientY ?? window.innerHeight / 3
    setSlot({ start, end, allDay, x, y })
  }

  const closeSlot = () => {
    setSlot(null)
    api()?.unselect()
  }

  const slotToDraft = (s: Slot) =>
    s.allDay
      ? { start: s.start, end: s.end, allDay: true }
      : {
          start: wallToIso(toWall(Date.parse(s.start), tz)),
          end: wallToIso(toWall(Date.parse(s.end), tz)),
          allDay: false,
        }

  const quickCreate = async (title: string) => {
    if (!slot) return
    setCreating(true)
    try {
      const body: EventWrite = { title, start: slot.start, end: slot.end, allDay: slot.allDay, timeZone: tz }
      const created = await endpoints.createEvent(body)
      markOwnWrite(created.id, created.version)
      storeEvent(qc, created)
      closeSlot()
      toast({
        title: t('calendar.created', { title: created.title }),
        action: {
          label: t('common.open'),
          onClick: () => void navigate({ search: (prev) => ({ ...prev, event: created.id, occurrence: undefined }) }),
        },
      })
    } catch (err) {
      toast.error(t('calendar.createFailed'), errorMessage(err, t))
    } finally {
      setCreating(false)
    }
  }

  const onTimesChanged = async (info: EventDropInfo | EventResizeDoneInfo) => {
    const ev = (info.event.extendedProps as ChipProps).ev
    if (!ev || !info.event.start) {
      info.revert()
      return
    }
    const allDay = info.event.allDay
    let start: string
    let end: string
    if (allDay) {
      start = info.event.startStr.slice(0, 10)
      end = info.event.endStr ? info.event.endStr.slice(0, 10) : addDays(start, 1)
    } else {
      start = toUtcString(info.event.start)
      end = toUtcString(info.event.end ?? new Date(info.event.start.getTime() + 3_600_000))
    }
    const body: Partial<EventWrite> = { start, end }
    if (allDay !== ev.allDay) {
      body.allDay = allDay
      if (!allDay) body.timeZone = ev.timeZone || tz
    }
    const recurring = !!(ev.rrule || ev.occurrence)

    if (!ev.permissions.edit) {
      info.revert()
      if (!ev.permissions.suggest) return
      if (recurring) {
        toast({ title: t('calendar.suggestRecurringHint') })
        return
      }
      setGhost({ ev, start, end, allDay, body })
      return
    }

    let scope: EditScope | undefined
    if (recurring) {
      const chosen = await scopePrompt.ask('edit', { disableThis: allDay !== ev.allDay })
      if (!chosen) {
        info.revert()
        return
      }
      scope = chosen
    }

    const snap = snapshotEvent(qc, ev.id)
    patchEventInCache(
      qc,
      ev.id,
      recurring ? ev.occurrence : null,
      allDay ? { start, end, allDay: true } : { startUtc: start, endUtc: end, allDay: false },
    )
    markOwnWrite(ev.id)
    try {
      const updated = await endpoints.updateEvent(ev.id, ev.version, body, scope ? { scope, occurrence: ev.occurrence } : undefined)
      markOwnWrite(updated.id, updated.version)
      storeEvent(qc, updated)
      if (recurring) void qc.invalidateQueries({ queryKey: keys.eventsAll })
    } catch (err) {
      restoreSnapshot(qc, snap)
      info.revert()
      if (isApiError(err) && err.status === 409) {
        if (err.problem.current) storeEvent(qc, err.problem.current)
        else void qc.invalidateQueries({ queryKey: keys.eventsAll })
        toast.error(t('calendar.moveConflictTitle'), t('calendar.moveConflictBody'))
      } else if (isApiError(err) && err.status === 403 && err.problem.suggest && !recurring) {
        setGhost({ ev, start, end, allDay, body })
      } else {
        toast.error(t('calendar.moveFailed'), errorMessage(err, t))
      }
    } finally {
      clearOwnWrite(ev.id)
    }
  }

  const sendGhostSuggestion = async () => {
    if (!ghost) return
    const g = ghost
    setGhost(null)
    try {
      await endpoints.suggestEdit(g.ev.id, g.ev.version, g.body)
      void qc.invalidateQueries({ queryKey: keys.suggestionsAll(g.ev.id) })
      toast.success(t('suggestions.sent'), t('suggestions.sentBody', { name: g.ev.owner.name }))
    } catch (err) {
      toast.error(t('suggestions.sendFailed'), errorMessage(err, t))
    }
  }

  const initialDate = search.date
  const showSkeleton = eventsQ.isPending && !eventsQ.data

  return (
    <div className="flex h-full min-h-0 flex-col">
      <CalendarToolbar
        ref={searchRef}
        title={controller.view?.title ?? ''}
        view={view}
        onView={changeView}
        onToday={() => api()?.today()}
        onPrev={() => api()?.prev()}
        onNext={() => api()?.next()}
        query={search.q ?? ''}
        onQuery={(q) => void navigate({ search: (prev) => ({ ...prev, q: q || undefined }), replace: true })}
        filters={filters}
        onFilters={(f) =>
          void navigate({
            search: (prev) => ({ ...prev, tags: f.tags.length ? f.tags : undefined, owners: f.owners.length ? f.owners : undefined }),
            replace: true,
          })
        }
        canCreate={me.permissions.createEvent}
        onNew={() => openNew()}
      />

      {search.q || filters.tags.length || filters.owners.length ? (
        <div className="flex items-center gap-2 px-4 pb-2 text-sm text-muted-foreground sm:px-6" role="status">
          {t('calendar.filteredResults', { count: eventsQ.data?.events.length ?? 0 })}
          <Button
            variant="ghost"
            size="xs"
            onClick={() =>
              void navigate({ search: (prev) => ({ ...prev, q: undefined, tags: undefined, owners: undefined }), replace: true })
            }
          >
            <XIcon aria-hidden />
            {t('calendar.clearAll')}
          </Button>
        </div>
      ) : null}

      {eventsQ.isError ? (
        <div role="alert" className="mx-4 mb-2 flex items-center justify-between gap-3 rounded-lg border border-destructive/40 bg-destructive/5 px-4 py-2 text-sm sm:mx-6">
          <span>{t('calendar.loadFailed')}</span>
          <Button size="xs" variant="outline" onClick={() => void eventsQ.refetch()}>
            {t('common.retry')}
          </Button>
        </div>
      ) : null}

      <div className="relative min-h-0 flex-1 px-2 pb-2 sm:px-4 sm:pb-4">
        <div className="sc-calendar relative h-full overflow-hidden rounded-xl border border-border bg-background" aria-busy={showSkeleton}>
          <EventCalendarViews
            ref={calRef}
            controller={controller}
            plugins={plugins}
            initialView={FC_VIEW[view]}
            initialDate={initialDate}
            views={{
              agenda: { type: 'list', duration: { days: 30 }, titleFormat: { month: 'short', day: 'numeric', year: 'numeric' } },
              list: { listItemEventTimeClass: 'shrink-0 w-28 sm:w-40 whitespace-nowrap overflow-hidden text-ellipsis tabular-nums' },
            }}
            timeZone={tz}
            firstDay={prefs.weekStart}
            direction={document.documentElement.dir === 'rtl' ? 'rtl' : 'ltr'}
            height="100%"
            borderless
            nowIndicator
            navLinks
            dayMaxEvents
            editable
            selectable={me.permissions.createEvent}
            selectMirror
            eventInteractive
            events={events}
            eventContent={renderEventContent}
            eventDidMount={(info) => {
              const ev = (info.event.extendedProps as ChipProps).ev
              if (ev) info.el.dataset.eventId = ev.id
            }}
            eventTimeFormat={{ hour: 'numeric', minute: '2-digit', hour12: !prefs.clock24h, meridiem: 'short' }}
            slotHeaderFormat={{ hour: 'numeric', minute: '2-digit', omitZeroMinute: !prefs.clock24h, hour12: !prefs.clock24h }}
            slotMinHeight={prefs.density === 'compact' ? 28 : 40}
            scrollTime="07:30:00"
            datesSet={onDatesSet}
            eventClick={onEventClick}
            select={onSelect}
            eventDrop={(i) => void onTimesChanged(i)}
            eventResize={(i) => void onTimesChanged(i)}
            noEventsText={t('calendar.noEvents')}
            popoverCloseContent={() => <XIcon className="size-5 text-muted-foreground" />}
          />
          {showSkeleton ? <CalendarSkeleton view={view} /> : null}
        </div>
      </div>

      <QuickCreatePopover
        slot={slot}
        whenText={
          slot ? eventWhen({ allDay: slot.allDay, start: slot.start, end: slot.end, startUtc: slot.start, endUtc: slot.end }, fmt, t) : ''
        }
        pending={creating}
        onClose={closeSlot}
        onCreate={(title) => void quickCreate(title)}
        onMore={(title) => {
          if (!slot) return
          const d = slotToDraft(slot)
          closeSlot()
          openNew({ title, ...d })
        }}
      />

      <ConfirmDialog
        open={!!ghost}
        onOpenChange={(open) => !open && setGhost(null)}
        title={t('calendar.suggestMoveTitle')}
        description={
          ghost
            ? t('calendar.suggestMoveBody', {
                name: ghost.ev.owner.name,
                when: eventWhen(
                  { allDay: ghost.allDay, start: ghost.start, end: ghost.end, startUtc: ghost.start, endUtc: ghost.end },
                  fmt,
                  t,
                ),
              })
            : undefined
        }
        confirmLabel={t('calendar.sendSuggestion')}
        onConfirm={() => void sendGhostSuggestion()}
      />

      {scopePrompt.element}

      {panel ? (
        <Suspense fallback={<PanelFallback />}>
        <EventPanel
          key={panel.kind === 'view' ? `${panel.id}|${panel.occurrence ?? ''}` : 'new'}
          mode={panel}
          tab={search.tab ?? 'details'}
          onTab={(tab) => void navigate({ search: (prev) => ({ ...prev, tab: tab === 'details' ? undefined : tab }), replace: true })}
          onClose={closePanel}
          onCreated={(ev: CalEvent) => {
            setDraft(null)
            void navigate({ search: (prev) => ({ ...prev, event: ev.id, occurrence: undefined, tab: undefined }) })
          }}
          fmt={fmt}
          scopePrompt={scopePrompt.ask}
        />
        </Suspense>
      ) : null}
    </div>
  )
}

function PanelFallback() {
  return (
    <aside className="sc-panel-enter fixed inset-y-0 end-0 z-50 flex w-full flex-col gap-3 border-s border-border bg-card p-5 shadow-2xl md:w-(--panel-width)" aria-busy="true">
      <div className="h-7 w-2/3 animate-pulse rounded-md bg-muted motion-reduce:animate-none" />
      <div className="h-9 w-full animate-pulse rounded-md bg-muted motion-reduce:animate-none" />
      <div className="h-40 w-full animate-pulse rounded-md bg-muted motion-reduce:animate-none" />
    </aside>
  )
}

/** Next full hour today (or 9:00 on another date), one hour long, as wall times in `tz`. */
function defaultNewTimes(tz: string, date?: string) {
  const now = Date.now()
  const today = dateInZone(now, tz)
  if (date && date !== today) {
    const [y, m, d] = date.split('-').map(Number)
    const start = { year: y ?? 1970, month: m ?? 1, day: d ?? 1, hour: 9, minute: 0, second: 0 }
    return { start: wallToIso(start), end: wallToIso({ ...start, hour: 10 }) }
  }
  const w = toWall(now, tz)
  const startMs = fromWall({ ...w, minute: 0, second: 0 }, tz) + 3_600_000
  return { start: wallToIso(toWall(startMs, tz)), end: wallToIso(toWall(startMs + 3_600_000, tz)) }
}

async function savePreferredView(qc: ReturnType<typeof useQueryClient>, me: Me, v: CalendarView) {
  const next = { ...me.preferences, defaultView: v }
  qc.setQueryData<Me>(keys.me, (prev) => (prev ? { ...prev, preferences: next } : prev))
  try {
    await endpoints.savePreferences(next)
  } catch {
    /* non-critical; the URL still carries the view */
  }
}

