import type { ReactNode } from 'react'

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

export function EmptyState({ icon, title, body }: { icon?: ReactNode; title: string; body?: ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-border px-6 py-14 text-center">
      {icon ? <div className="text-muted-foreground [&_svg]:size-8">{icon}</div> : null}
      <p className="font-medium">{title}</p>
      {body ? <p className="max-w-sm text-sm text-muted-foreground">{body}</p> : null}
    </div>
  )
}
