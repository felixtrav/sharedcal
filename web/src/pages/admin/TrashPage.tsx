import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArchiveRestoreIcon, FileIcon, PaperclipIcon, Trash2Icon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { storeEvent } from '@/api/eventCache'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { TrashedAttachment } from '@/api/types'
import { EmptyState, Page, PageHeader } from '@/components/layout/PageHeader'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { eventWhen, formatFileSize } from '@/lib/eventText'
import { formatRelative } from '@/lib/time'
import { useFmt } from '@/lib/useFmt'
import { applyStreamMessage } from '@/realtime/applyMessage'

export function TrashPage() {
  const { t } = useTranslation()
  return (
    <Page>
      <PageHeader title={t('trash.title')} description={t('trash.description')} />
      <Tabs defaultValue="events">
        <TabsList>
          <TabsTrigger value="events">{t('trash.tabs.events')}</TabsTrigger>
          <TabsTrigger value="attachments">{t('trash.tabs.attachments')}</TabsTrigger>
        </TabsList>
        <TabsContent value="events" className="mt-4">
          <EventTrash />
        </TabsContent>
        <TabsContent value="attachments" className="mt-4">
          <AttachmentTrash />
        </TabsContent>
      </Tabs>
    </Page>
  )
}

function Loading({ isPending, isError, refetch, children }: { isPending: boolean; isError: boolean; refetch: () => void; children: () => ReactNode }) {
  const { t } = useTranslation()
  if (isPending) return <Skeleton className="h-40" />
  if (isError)
    return (
      <div role="alert" className="text-sm">
        {t('errors.loadFailed')}{' '}
        <Button variant="link" size="xs" onClick={refetch}>
          {t('common.retry')}
        </Button>
      </div>
    )
  return <>{children()}</>
}

function EventTrash() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const fmt = useFmt()
  const { data, isPending, isError, refetch } = useQuery({
    queryKey: keys.trash,
    queryFn: endpoints.trash,
  })

  const restore = async (id: string, title: string) => {
    try {
      const ev = await endpoints.restoreEvent(id)
      storeEvent(qc, ev)
      // Restoring an event can make its removed attachments restorable again.
      void qc.invalidateQueries({ queryKey: keys.trash })
      toast.success(t('trash.restored', { title }))
    } catch (err) {
      toast.error(t('trash.restoreFailed'), errorMessage(err, t))
    }
  }

  return (
    <Loading isPending={isPending} isError={isError} refetch={() => void refetch()}>
      {() =>
        !data || data.events.length === 0 ? (
          <EmptyState icon={<Trash2Icon />} title={t('trash.empty')} />
        ) : (
          <ul className="divide-y divide-border rounded-xl border border-border bg-card">
            {data.events.map((ev) => (
              <li key={ev.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{ev.title}</p>
                  <p className="text-sm text-muted-foreground tabular-nums">{eventWhen(ev, fmt, t)}</p>
                </div>
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <UserAvatar name={ev.owner.name} src={ev.owner.avatarUrl} className="size-5" />
                  {ev.owner.name}
                </div>
                <p className="text-xs text-muted-foreground">
                  <time dateTime={ev.deletedAt}>
                    {t('trash.deletedAgo', {
                      when: formatRelative(ev.deletedAt),
                    })}
                  </time>
                </p>
                <Button size="sm" variant="outline" onClick={() => void restore(ev.id, ev.title)} aria-label={t('trash.restoreNamed', { name: ev.title })}>
                  <ArchiveRestoreIcon aria-hidden />
                  {t('trash.restore')}
                </Button>
              </li>
            ))}
          </ul>
        )
      }
    </Loading>
  )
}

function AttachmentTrash() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isPending, isError, refetch } = useQuery({
    queryKey: keys.attachmentTrash,
    queryFn: endpoints.attachmentTrash,
  })

  const restore = async (a: TrashedAttachment) => {
    try {
      const restored = await endpoints.restoreAttachment(a.id)
      applyStreamMessage(qc, 'attachment.added', restored)
      void qc.invalidateQueries({ queryKey: keys.attachmentTrash })
      toast.success(
        t('trash.attachmentRestored', {
          name: a.filename,
          title: a.eventTitle,
        }),
      )
    } catch (err) {
      void qc.invalidateQueries({ queryKey: keys.attachmentTrash })
      toast.error(t('trash.attachmentRestoreFailed'), errorMessage(err, t))
    }
  }

  return (
    <Loading isPending={isPending} isError={isError} refetch={() => void refetch()}>
      {() =>
        !data || data.attachments.length === 0 ? (
          <EmptyState icon={<PaperclipIcon />} title={t('trash.attachmentsEmpty')} />
        ) : (
          <ul className="divide-y divide-border rounded-xl border border-border bg-card">
            {data.attachments.map((a) => (
              <li key={a.id} className="flex flex-wrap items-center gap-3 px-4 py-3">
                <FileIcon aria-hidden className="size-5 shrink-0 text-muted-foreground" />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{a.filename}</p>
                  <p className="truncate text-sm text-muted-foreground">
                    {t(a.commentId ? 'trash.onComment' : 'trash.onEvent', {
                      title: a.eventTitle,
                    })}{' '}
                    · <span className="tabular-nums">{formatFileSize(a.size)}</span>
                  </p>
                </div>
                <div className="flex items-center gap-2 text-sm text-muted-foreground">
                  <UserAvatar name={a.uploader.name} src={a.uploader.avatarUrl} className="size-5" />
                  {a.uploader.name}
                </div>
                <p className="text-xs text-muted-foreground">
                  <time dateTime={a.deletedAt}>
                    {t('trash.removedAgo', {
                      when: formatRelative(a.deletedAt),
                    })}
                  </time>
                </p>
                <Button size="sm" variant="outline" onClick={() => void restore(a)} aria-label={t('trash.restoreNamed', { name: a.filename })}>
                  <ArchiveRestoreIcon aria-hidden />
                  {t('trash.restore')}
                </Button>
              </li>
            ))}
          </ul>
        )
      }
    </Loading>
  )
}
