import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { isApiError } from '@/api/client'
import { useMe } from '@/api/queries'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

/** Gate the app on GET /api/me. 401 is redirected to sign-in by the API client. */
export function RequireMe({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const { data, error, isPending, refetch } = useMe()

  if (data) return <>{children}</>

  if (isPending || (isApiError(error) && error.status === 401)) {
    return (
      <div className="flex h-dvh" aria-busy="true">
        <span className="sr-only" role="status">
          {isPending ? t('common.loading') : t('auth.redirecting')}
        </span>
        <div className="hidden w-14 flex-col items-center gap-4 border-e border-border py-4 md:flex">
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i} className="size-8 rounded-lg" />
          ))}
        </div>
        <div className="flex flex-1 flex-col gap-4 p-6">
          <Skeleton className="h-9 w-72" />
          <Skeleton className="flex-1" />
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-dvh items-center justify-center p-6">
      <div className="max-w-sm text-center" role="alert">
        <h1 className="text-lg font-semibold">{t('errors.loadAppTitle')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{error instanceof Error ? error.message : t('errors.generic')}</p>
        <Button className="mt-4" onClick={() => void refetch()}>
          {t('common.retry')}
        </Button>
      </div>
    </div>
  )
}
