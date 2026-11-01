import { useTranslation } from 'react-i18next'
import { useInbox, useMeData, useOpenCatalogCount, usePendingPeopleCount } from '@/api/queries'
import { cn } from '@/lib/utils'

export function useUnreadCount() {
  return useInbox().data?.unread ?? 0
}

export function InboxBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  const unread = useUnreadCount()
  if (!unread) return null
  return (
    <span
      className={cn(
        'inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-destructive px-1 text-[0.6875rem] leading-none font-semibold text-destructive-foreground tabular-nums',
        className,
      )}
    >
      <span aria-hidden>{unread > 99 ? '99+' : unread}</span>
      <span className="sr-only">{t('nav.unreadCount', { count: unread })}</span>
    </span>
  )
}

function CountBadge({ count, label, className }: { count: number; label: string; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-destructive px-1 text-[0.6875rem] leading-none font-semibold text-destructive-foreground tabular-nums',
        className,
      )}
    >
      <span aria-hidden>{count > 99 ? '99+' : count}</span>
      <span className="sr-only">{label}</span>
    </span>
  )
}

/** How many tag and event type requests wait for an admin. */
export function RequestsBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  const isAdmin = useMeData().permissions.admin
  const waiting = useOpenCatalogCount(isAdmin).data?.open ?? 0
  if (!waiting) return null
  return <CountBadge count={waiting} label={t('nav.requestsWaiting', { count: waiting })} className={className} />
}

/** How many people without an account wait for an admin to approve them. */
export function PeopleBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  const isAdmin = useMeData().permissions.admin
  const waiting = usePendingPeopleCount(isAdmin).data?.pending ?? 0
  if (!waiting) return null
  return <CountBadge count={waiting} label={t('nav.peopleWaiting', { count: waiting })} className={className} />
}
