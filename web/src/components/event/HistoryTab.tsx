import { HistoryIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useHistory } from '@/api/queries'
import { ActivityList } from '@/components/ActivityList'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { FormatOptions } from '@/lib/time'

export function HistoryTab({ eventId, fmt }: { eventId: string; fmt: FormatOptions }) {
  const { t } = useTranslation()
  const { data, isPending, isError, refetch } = useHistory(eventId)
  if (isPending) return <Skeleton className="h-32 w-full" />
  if (isError)
    return (
      <div role="alert" className="text-sm">
        {t('history.loadFailed')}{' '}
        <Button variant="link" size="xs" onClick={() => void refetch()}>
          {t('common.retry')}
        </Button>
      </div>
    )
  if (!data.items.length)
    return (
      <div className="flex flex-col items-center gap-2 py-10 text-sm text-muted-foreground">
        <HistoryIcon className="size-6" aria-hidden />
        {t('history.empty')}
      </div>
    )
  return <ActivityList items={data.items} fmt={fmt} />
}
