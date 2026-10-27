import type { ReactNode } from 'react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useMeData } from '@/api/queries'
import { useEventStream } from '@/realtime/useEventStream'
import { cn } from '@/lib/utils'
import { BottomBar } from './BottomBar'
import { NavRail, useRailPinned } from './NavRail'
import { ReconnectBanner } from './ReconnectBanner'

export function AppShell({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const me = useMeData()
  const [pinned, togglePin] = useRailPinned()
  useEventStream(me.user.id)

  useEffect(() => {
    document.title = me.instance.calendarName || t('app.name')
  }, [me.instance.calendarName, t])

  return (
    <div className="flex h-dvh overflow-hidden bg-background">
      <a
        href="#main"
        className="sr-only-focusable fixed start-2 top-2 z-[60] rounded-md bg-primary px-3 py-2 text-sm font-medium text-primary-foreground"
      >
        {t('nav.skipToContent')}
      </a>
      <NavRail pinned={pinned} onTogglePin={togglePin} />
      <main
        id="main"
        tabIndex={-1}
        className={cn(
          'relative flex min-w-0 flex-1 flex-col outline-none max-md:pb-16 md:transition-[margin] md:duration-150',
          pinned ? 'md:ms-(--rail-expanded)' : 'md:ms-(--rail-collapsed)',
        )}
      >
        {children}
      </main>
      <BottomBar />
      <ReconnectBanner />
    </div>
  )
}
