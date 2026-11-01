import { useTranslation } from 'react-i18next'
import { useInbox, useMeData, useOpenCatalogCount } from '@/api/queries'
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

/** How many tag and event type requests wait for an admin. */
export function RequestsBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  const isAdmin = useMeData().permissions.admin
  const waiting = useOpenCatalogCount(isAdmin).data?.open ?? 0
  if (!waiting) return null
  return (
    <span
      className={cn(
        'inline-flex h-4.5 min-w-4.5 items-center justify-center rounded-full bg-destructive px-1 text-[0.6875rem] leading-none font-semibold text-destructive-foreground tabular-nums',
        className,
      )}
    >
      <span aria-hidden>{waiting > 99 ? '99+' : waiting}</span>
      <span className="sr-only">{t('nav.requestsWaiting', { count: waiting })}</span>
    </span>
  )
}
