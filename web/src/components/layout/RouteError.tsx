import type { ErrorComponentProps } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

/** Shown when a page throws while rendering; keeps the shell usable. */
export function RouteError({ error, reset }: ErrorComponentProps) {
  const { t } = useTranslation()
  return (
    <div className="flex h-full items-center justify-center p-8" role="alert">
      <div className="max-w-md text-center">
        <h1 className="text-lg font-semibold">{t('errors.pageCrashedTitle')}</h1>
        <p className="mt-2 text-sm text-muted-foreground">{error instanceof Error ? error.message : t('errors.generic')}</p>
        <div className="mt-4 flex justify-center gap-2">
          <Button variant="outline" onClick={reset}>
            {t('common.retry')}
          </Button>
          <Button onClick={() => window.location.reload()}>{t('errors.reload')}</Button>
        </div>
      </div>
    </div>
  )
}
