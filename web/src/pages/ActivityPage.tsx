import { useInfiniteQuery } from '@tanstack/react-query'
import { HistoryIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useDirectory, useMeData } from '@/api/queries'
import { ActivityList } from '@/components/ActivityList'
import { EmptyState, Page, PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input, NativeSelect } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { useFmt } from '@/lib/useFmt'

const SUBJECT_TYPES = ['event', 'comment', 'suggestion', 'attachment', 'tag', 'user', 'settings', 'theme'] as const
const PAGE = 50

/** Filterable audit log. Admins see everything; others get their own activity (the API decides). */
export function ActivityPage() {
  const { t } = useTranslation()
  const me = useMeData()
  const fmt = useFmt()
  const { data: dir } = useDirectory()
  const [actor, setActor] = useState('')
  const [subjectType, setSubjectType] = useState('')
  const [action, setAction] = useState('')
  const [actionInput, setActionInput] = useState('')
  const filters = { actor: actor || undefined, subjectType: subjectType || undefined, action: action || undefined }

  const q = useInfiniteQuery({
    queryKey: keys.activity(filters),
    queryFn: ({ pageParam }) => endpoints.activity({ ...filters, before: pageParam, limit: PAGE }),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => (last.items.length < PAGE ? undefined : last.items[last.items.length - 1]?.id),
  })
  const items = q.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <Page>
      <PageHeader title={me.permissions.admin ? t('activity.title') : t('activity.myTitle')} description={me.permissions.admin ? t('activity.description') : t('activity.myDescription')} />
      <form
        className="mb-6 grid gap-3 rounded-xl border border-border bg-card p-4 sm:grid-cols-3"
        onSubmit={(e) => {
          e.preventDefault()
          setAction(actionInput.trim())
        }}
        aria-label={t('activity.filters')}
      >
        {me.permissions.admin ? (
          <div className="grid gap-1.5">
            <label htmlFor="act-actor" className="text-xs font-medium text-muted-foreground">
              {t('activity.actor')}
            </label>
            <NativeSelect id="act-actor" value={actor} onChange={(e) => setActor(e.target.value)}>
              <option value="">{t('activity.anyone')}</option>
              {dir?.users.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.name}
                </option>
              ))}
            </NativeSelect>
          </div>
        ) : null}
        <div className="grid gap-1.5">
          <label htmlFor="act-subject" className="text-xs font-medium text-muted-foreground">
            {t('activity.subjectType')}
          </label>
          <NativeSelect id="act-subject" value={subjectType} onChange={(e) => setSubjectType(e.target.value)}>
            <option value="">{t('activity.anything')}</option>
            {SUBJECT_TYPES.map((s) => (
              <option key={s} value={s}>
                {t(`activity.subjects.${s}`)}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="act-action" className="text-xs font-medium text-muted-foreground">
            {t('activity.action')}
          </label>
          <Input
            id="act-action"
            value={actionInput}
            placeholder="event.updated"
            onChange={(e) => setActionInput(e.target.value)}
            onBlur={() => setAction(actionInput.trim())}
          />
        </div>
      </form>

      {q.isPending ? (
        <div className="grid gap-3">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-10" />
          ))}
        </div>
      ) : q.isError ? (
        <div role="alert" className="text-sm">
          {t('errors.loadFailed')}{' '}
          <Button variant="link" size="xs" onClick={() => void q.refetch()}>
            {t('common.retry')}
          </Button>
        </div>
      ) : items.length === 0 ? (
        <EmptyState icon={<HistoryIcon />} title={t('activity.empty')} />
      ) : (
        <>
          <ActivityList items={items} fmt={fmt} showEventLinks />
          {q.hasNextPage ? (
            <div className="mt-6 flex justify-center">
              <Button variant="outline" size="sm" disabled={q.isFetchingNextPage} onClick={() => void q.fetchNextPage()}>
                {t('common.loadMore')}
              </Button>
            </div>
          ) : null}
        </>
      )}
    </Page>
  )
}
