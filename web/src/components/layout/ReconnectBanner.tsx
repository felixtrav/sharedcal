import { WifiOffIcon } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { connectionStore } from '@/realtime/stores'
import { useStore } from '@/lib/store'

/** A quiet banner while the live-update stream is down. */
export function ReconnectBanner() {
  const { t } = useTranslation()
  const state = useStore(connectionStore)
  // Don't flash on brief blips or the initial connect.
  const [delayed, setDelayed] = useState(false)
  const [prev, setPrev] = useState(state)
  if (prev !== state) {
    setPrev(state)
    setDelayed(false)
  }
  useEffect(() => {
    if (state === 'open') return
    const timer = setTimeout(() => setDelayed(true), state === 'connecting' ? 4000 : 1500)
    return () => clearTimeout(timer)
  }, [state])
  const show = state !== 'open' && delayed

  return (
    <div role="status" aria-live="polite" className="pointer-events-none fixed inset-x-0 top-3 z-50 flex justify-center">
      {show ? (
        <div className="pointer-events-auto flex items-center gap-2 rounded-full border border-border bg-popover px-4 py-1.5 text-sm text-muted-foreground shadow-md">
          <WifiOffIcon className="size-4" aria-hidden />
          {t('realtime.reconnecting')}
        </div>
      ) : null}
    </div>
  )
}
