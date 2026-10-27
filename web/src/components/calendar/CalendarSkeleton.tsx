import { useTranslation } from 'react-i18next'
import type { CalendarView } from '@/api/types'
import { Skeleton } from '@/components/ui/skeleton'

/** Overlay shown while the first range loads. */
export function CalendarSkeleton({ view }: { view: CalendarView }) {
  const { t } = useTranslation()
  return (
    <div className="absolute inset-0 z-10 bg-background/80 p-3" role="status" aria-live="polite">
      <span className="sr-only">{t('calendar.loading')}</span>
      {view === 'month' ? (
        <div className="grid h-full grid-cols-7 grid-rows-[auto_repeat(5,1fr)] gap-1.5" aria-hidden>
          {Array.from({ length: 7 }, (_, i) => (
            <Skeleton key={`h${i}`} className="mx-auto h-4 w-10" />
          ))}
          {Array.from({ length: 35 }, (_, i) => (
            <div key={i} className="flex flex-col gap-1.5 rounded-md border border-border/60 p-2">
              <Skeleton className="h-3 w-5" />
              {i % 3 === 0 ? <Skeleton className="h-4 w-full" /> : null}
              {i % 5 === 1 ? <Skeleton className="h-4 w-3/4" /> : null}
            </div>
          ))}
        </div>
      ) : view === 'agenda' ? (
        <div className="flex flex-col gap-3 p-2" aria-hidden>
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="flex items-center gap-4">
              <Skeleton className="size-9 rounded-full" />
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-4 flex-1" />
            </div>
          ))}
        </div>
      ) : (
        <div className="grid h-full gap-1.5" style={{ gridTemplateColumns: `3rem repeat(${view === 'day' ? 1 : 7}, 1fr)` }} aria-hidden>
          <div className="flex flex-col gap-10 pt-10">
            {Array.from({ length: 8 }, (_, i) => (
              <Skeleton key={i} className="h-3 w-8" />
            ))}
          </div>
          {Array.from({ length: view === 'day' ? 1 : 7 }, (_, i) => (
            <div key={i} className="relative rounded-md border border-border/60">
              <Skeleton className="absolute inset-x-1 h-14" style={{ top: `${15 + ((i * 23) % 50)}%` }} />
              {i % 2 === 0 ? <Skeleton className="absolute inset-x-1 h-10" style={{ top: `${60 + ((i * 7) % 20)}%` }} /> : null}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
