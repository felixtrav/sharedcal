import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export function PageHeader({ title, description, actions }: { title: string; description?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="flex flex-wrap items-end justify-between gap-4 pb-6">
      <div className="min-w-0">
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </header>
  )
}

/** Standard scrolling page container for non-calendar pages. */
export function Page({ children, wide }: { children: ReactNode; wide?: boolean }) {
  return (
    <div className="h-full overflow-y-auto">
      <div className={`mx-auto w-full px-4 py-8 sm:px-8 ${wide ? 'max-w-6xl' : 'max-w-3xl'}`}>{children}</div>
    </div>
  )
}

/** A titled block of a settings-style page. Pass `wide` when the Page is wide. */
export function Section({ id, title, description, wide, children }: { id: string; title: string; description?: string; wide?: boolean; children: ReactNode }) {
  return (
    <section
      aria-labelledby={id}
      className={cn(
        'grid gap-4 border-b border-border py-6 first:pt-0 last:border-0',
        wide ? 'md:grid-cols-[minmax(0,15rem)_1fr]' : 'sm:grid-cols-[minmax(0,14rem)_1fr]',
      )}
    >
      <div>
        <h2 id={id} className="text-sm font-semibold">
          {title}
        </h2>
        {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
      </div>
      <div className={cn('grid gap-4', wide && 'max-w-xl')}>{children}</div>
    </section>
  )
}

export function EmptyState({ icon, title, body }: { icon?: ReactNode; title: string; body?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border px-6 py-14 text-center">
      {icon ? <div className="text-muted-foreground [&_svg]:size-8">{icon}</div> : null}
      <p className="font-medium">{title}</p>
      {body ? <p className="max-w-sm text-sm text-muted-foreground">{body}</p> : null}
    </div>
  )
}

/** "Couldn't load" notice with a retry link, for a failed page query. */
export function LoadError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div role="alert" className="text-sm">
      {t('errors.loadFailed')}{' '}
      <Button variant="link" size="xs" onClick={onRetry}>
        {t('common.retry')}
      </Button>
    </div>
  )
}
