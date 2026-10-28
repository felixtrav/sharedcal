import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { AtSignIcon, CheckCheckIcon, CheckIcon, InboxIcon, MessageSquareDiffIcon, ReplyIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useInbox } from '@/api/queries'
import type { Notification } from '@/api/types'
import { EmptyState, LoadError, Page, PageHeader } from '@/components/layout/PageHeader'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatDateTime, formatRelative } from '@/lib/time'
import { useFmt } from '@/lib/useFmt'
import { cn } from '@/lib/utils'

type InboxData = { items: Notification[]; unread: number }

const ICONS = {
  'suggestion.created': MessageSquareDiffIcon,
  'suggestion.resolved': CheckIcon,
  'comment.reply': ReplyIcon,
  'comment.mention': AtSignIcon,
} as const

export function InboxPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const fmt = useFmt()
  const { data, isPending, isError, refetch } = useInbox()
  const [older, setOlder] = useState<Notification[]>([])
  const [hasMore, setHasMore] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const items = [...(data?.items ?? []), ...older.filter((o) => !data?.items.some((i) => i.id === o.id))]

  const setRead = (ids: string[] | 'all', unread: number) => {
    const now = new Date().toISOString()
    const mark = (n: Notification) => (ids === 'all' || ids.includes(n.id) ? { ...n, readAt: n.readAt ?? now } : n)
    qc.setQueryData<InboxData>(keys.inbox, (prev) => (prev ? { items: prev.items.map(mark), unread } : prev))
    setOlder((prev) => prev.map(mark))
  }

  const markRead = async (ids: string[]) => {
    try {
      const res = await endpoints.markRead({ ids })
      setRead(ids, res.unread)
    } catch (err) {
      toast.error(t('inbox.markFailed'), errorMessage(err, t))
    }
  }

  const markAll = async () => {
    try {
      const res = await endpoints.markRead({ all: true })
      setRead('all', res.unread)
    } catch (err) {
      toast.error(t('inbox.markFailed'), errorMessage(err, t))
    }
  }

  const open = (n: Notification) => {
    if (!n.readAt) void markRead([n.id])
    if (!n.eventId) return
    void navigate({ to: '/', search: { event: n.eventId, tab: n.subjectType === 'suggestion' ? 'suggestions' : 'comments' } })
  }

  const loadMore = async () => {
    const last = items[items.length - 1]
    if (!last) return
    setLoadingMore(true)
    try {
      const res = await endpoints.inbox({ before: last.id })
      setOlder((prev) => [...prev, ...res.items])
      if (res.items.length < 50) setHasMore(false)
    } catch (err) {
      toast.error(t('errors.loadFailed'), errorMessage(err, t))
    } finally {
      setLoadingMore(false)
    }
  }

  return (
    <Page>
      <PageHeader
        title={t('inbox.title')}
        description={data ? t('inbox.unreadSummary', { count: data.unread }) : undefined}
        actions={
          data?.unread ? (
            <Button variant="outline" size="sm" onClick={() => void markAll()}>
              <CheckCheckIcon aria-hidden />
              {t('inbox.markAllRead')}
            </Button>
          ) : null
        }
      />
      {isPending ? (
        <div className="grid gap-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-16" />
          ))}
        </div>
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : items.length === 0 ? (
        <EmptyState icon={<InboxIcon />} title={t('inbox.emptyTitle')} body={t('inbox.emptyBody')} />
      ) : (
        <>
          <ul className="grid gap-1.5">
            {items.map((n) => {
              const Icon = ICONS[n.kind] ?? InboxIcon
              const unread = !n.readAt
              return (
                <li
                  key={n.id}
                  className={cn(
                    'group flex items-center gap-2 rounded-xl border border-transparent pe-2 transition-colors hover:bg-accent',
                    unread && 'border-border bg-card',
                  )}
                >
                  <button
                    type="button"
                    onClick={() => open(n)}
                    className="flex min-w-0 flex-1 items-center gap-3 rounded-xl p-3 text-start outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                  >
                    <span className="relative">
                      {n.actor ? (
                        <UserAvatar name={n.actor.name} src={n.actor.avatarUrl} className="size-9" />
                      ) : (
                        <span className="flex size-9 items-center justify-center rounded-full bg-muted">
                          <Icon className="size-4" aria-hidden />
                        </span>
                      )}
                      <span className="absolute -end-1 -bottom-1 flex size-5 items-center justify-center rounded-full bg-card ring-1 ring-border">
                        <Icon className="size-3 text-muted-foreground" aria-hidden />
                      </span>
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className={cn('block truncate text-sm', unread && 'font-semibold')}>
                        {t(`inbox.kinds.${n.kind.replace('.', '_')}`, { name: n.actor?.name ?? t('activity.system'), title: n.eventTitle })}
                      </span>
                      <time dateTime={n.createdAt} title={formatDateTime(Date.parse(n.createdAt), fmt)} className="text-xs text-muted-foreground">
                        {formatRelative(n.createdAt)}
                      </time>
                    </span>
                    {unread ? (
                      <span className="size-2 shrink-0 rounded-full bg-primary">
                        <span className="sr-only">{t('inbox.unread')}</span>
                      </span>
                    ) : null}
                  </button>
                  {unread ? (
                    <Button variant="ghost" size="icon-xs" onClick={() => void markRead([n.id])} aria-label={t('inbox.markRead')}>
                      <CheckIcon />
                    </Button>
                  ) : null}
                </li>
              )
            })}
          </ul>
          {hasMore && items.length >= 50 ? (
            <div className="mt-4 flex justify-center">
              <Button variant="outline" size="sm" disabled={loadingMore} onClick={() => void loadMore()}>
                {t('common.loadMore')}
              </Button>
            </div>
          ) : null}
        </>
      )}
    </Page>
  )
}
