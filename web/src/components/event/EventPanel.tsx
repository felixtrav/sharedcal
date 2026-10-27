import { useQueryClient } from '@tanstack/react-query'
import { AlertCircleIcon, Trash2Icon, XIcon } from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { isApiError } from '@/api/client'
import { findCachedEvent } from '@/api/eventCache'
import { useEventDetail } from '@/api/queries'
import type { CalEvent, EventDetail } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { PanelTab } from '@/lib/search'
import { useStore } from '@/lib/store'
import type { FormatOptions } from '@/lib/time'
import { cn } from '@/lib/utils'
import { deletedEventsStore } from '@/realtime/stores'
import { CommentsTab } from './CommentsTab'
import { DetailsTab } from './DetailsTab'
import type { Draft } from './formModel'
import { HistoryTab } from './HistoryTab'
import { PresenceAvatars } from './PresenceAvatars'
import { SuggestionsTab } from './SuggestionsTab'
import { usePresence } from './usePresence'
import type { ScopeAsk } from './ScopeDialog'
import { EventForm } from './EventForm'

export type PanelMode = { kind: 'view'; id: string; occurrence: string | null } | { kind: 'create'; draft: Draft }

type Props = {
  mode: PanelMode
  tab: PanelTab
  onTab: (tab: PanelTab) => void
  onClose: () => void
  onCreated: (ev: CalEvent) => void
  fmt: FormatOptions
  scopePrompt: ScopeAsk
}

/** 420 px panel sliding over the calendar (full-height sheet on mobile). */
export function EventPanel(props: Props) {
  const { t } = useTranslation()
  const headingRef = useRef<HTMLHeadingElement>(null)

  useEffect(() => {
    headingRef.current?.focus({ preventScroll: true })
  }, [])

  return (
    <aside
      aria-labelledby="event-panel-title"
      className={cn(
        'sc-panel-enter fixed inset-y-0 end-0 z-50 flex w-full flex-col border-s border-border bg-card text-card-foreground shadow-2xl md:w-(--panel-width)',
      )}
    >
      {props.mode.kind === 'create' ? (
        <>
          <PanelHeader headingRef={headingRef} title={t('event.newTitle')} onClose={props.onClose} />
          <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-6">
            <EventForm
              mode={{ kind: 'create', draft: props.mode.draft }}
              fmt={props.fmt}
              onDone={(ev) => (ev ? props.onCreated(ev) : props.onClose())}
            />
          </div>
        </>
      ) : (
        <ViewPanel {...props} mode={props.mode} headingRef={headingRef} />
      )}
    </aside>
  )
}

function PanelHeader({
  headingRef,
  title,
  subtitle,
  onClose,
  extra,
}: {
  headingRef: React.RefObject<HTMLHeadingElement | null>
  title: string
  subtitle?: ReactNode
  onClose: () => void
  extra?: ReactNode
}) {
  const { t } = useTranslation()
  return (
    <header className="flex items-start gap-3 px-5 pt-5 pb-3">
      <div className="min-w-0 flex-1">
        <h2 id="event-panel-title" ref={headingRef} tabIndex={-1} className="text-lg leading-snug font-semibold break-words outline-none">
          {title}
        </h2>
        {subtitle ? <div className="mt-1 text-sm text-muted-foreground">{subtitle}</div> : null}
        {extra ? <div className="mt-2">{extra}</div> : null}
      </div>
      <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label={t('event.closePanel')} aria-keyshortcuts="Escape">
        <XIcon />
      </Button>
    </header>
  )
}

function ViewPanel({
  mode,
  tab,
  onTab,
  onClose,
  fmt,
  scopePrompt,
  headingRef,
}: Props & { mode: Extract<PanelMode, { kind: 'view' }>; headingRef: React.RefObject<HTMLHeadingElement | null> }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const detailQ = useEventDetail(mode.id)
  const deleted = useStore(deletedEventsStore).has(mode.id)
  const [dirty, setDirty] = useState(false)
  usePresence(deleted ? null : mode.id, dirty)

  const event: EventDetail | undefined = useMemo(() => {
    const d = detailQ.data
    if (!d) return undefined
    if (!mode.occurrence) return d
    const occ = findCachedEvent(qc, mode.id, mode.occurrence)
    return occ ? { ...d, ...occ, attachments: d.attachments, overrides: d.overrides, permissions: d.permissions, version: d.version } : d
  }, [detailQ.data, mode.id, mode.occurrence, qc])

  if (deleted) {
    return (
      <>
        <PanelHeader headingRef={headingRef} title={detailQ.data?.title ?? t('event.deletedTitle')} onClose={onClose} />
        <div className="m-5 flex items-start gap-3 rounded-lg border border-border bg-muted p-4 text-sm" role="status">
          <Trash2Icon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
          {t('event.deletedRemotely')}
        </div>
      </>
    )
  }

  if (detailQ.isError) {
    const notFound = isApiError(detailQ.error) && (detailQ.error.status === 404 || detailQ.error.status === 403)
    return (
      <>
        <PanelHeader headingRef={headingRef} title={notFound ? t('event.notFoundTitle') : t('errors.loadFailed')} onClose={onClose} />
        <div className="m-5 flex items-start gap-3 rounded-lg border border-border bg-muted p-4 text-sm" role="alert">
          <AlertCircleIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
          <div className="grid gap-3">
            <p>{notFound ? t('event.notFoundBody') : t('errors.generic')}</p>
            {!notFound ? (
              <Button size="sm" variant="outline" className="w-fit" onClick={() => void detailQ.refetch()}>
                {t('common.retry')}
              </Button>
            ) : null}
          </div>
        </div>
      </>
    )
  }

  if (!event) {
    return (
      <>
        <PanelHeader headingRef={headingRef} title={t('common.loading')} onClose={onClose} />
        <div className="grid gap-3 px-5" aria-hidden>
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-5 w-2/3" />
          <Skeleton className="h-5 w-1/2" />
          <Skeleton className="h-32 w-full" />
        </div>
      </>
    )
  }

  const occurrence = mode.occurrence

  return (
    <>
      <PanelHeader
        headingRef={headingRef}
        title={event.title || t('event.untitled')}
        onClose={onClose}
        extra={<PresenceAvatars eventId={event.id} />}
      />
      <Tabs value={tab} onValueChange={(v) => onTab(v as PanelTab)} className="flex min-h-0 flex-1 flex-col">
        <div className="px-5">
          <TabsList className="w-full">
            <TabsTrigger value="details">{t('event.tabs.details')}</TabsTrigger>
            <TabsTrigger value="comments">
              {t('event.tabs.comments')}
              {event.counts.comments ? <Count n={event.counts.comments} /> : null}
            </TabsTrigger>
            <TabsTrigger value="suggestions">
              {t('event.tabs.suggestions')}
              {event.counts.openSuggestions ? <Count n={event.counts.openSuggestions} highlight={event.permissions.review} /> : null}
            </TabsTrigger>
            <TabsTrigger value="history">{t('event.tabs.history')}</TabsTrigger>
          </TabsList>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <TabsContent value="details" className="px-5 pt-4 pb-8">
            <DetailsTab event={event} occurrence={occurrence} fmt={fmt} onDirtyChange={setDirty} onDeleted={onClose} scopePrompt={scopePrompt} />
          </TabsContent>
          <TabsContent value="comments" className="h-full">
            <CommentsTab event={event} fmt={fmt} />
          </TabsContent>
          <TabsContent value="suggestions" className="px-5 pt-4 pb-8">
            <SuggestionsTab event={event} fmt={fmt} onEventDeleted={onClose} />
          </TabsContent>
          <TabsContent value="history" className="px-5 pt-4 pb-8">
            <HistoryTab eventId={event.id} fmt={fmt} />
          </TabsContent>
        </div>
      </Tabs>
    </>
  )
}

function Count({ n, highlight }: { n: number; highlight?: boolean }) {
  return (
    <span
      className={cn(
        'rounded-full px-1.5 text-[0.6875rem] leading-4 font-semibold tabular-nums',
        highlight ? 'bg-primary text-primary-foreground' : 'bg-muted-foreground/15 text-foreground',
      )}
    >
      {n}
    </span>
  )
}
