import { useQueryClient } from '@tanstack/react-query'
import { BellIcon, BellRingIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { EventDetail } from '@/api/types'
import { Button } from '@/components/ui/button'
import { toast } from '@/components/ui/toast'
import { Tooltip } from '@/components/ui/tooltip'
import { errorMessage } from '@/lib/errors'

/** Follow or unfollow an event: watchers get grouped inbox items when it changes. */
export function WatchButton({ eventId, watching }: { eventId: string; watching: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [busy, setBusy] = useState(false)

  const setWatching = (value: boolean) => qc.setQueryData<EventDetail>(keys.event(eventId), (prev) => (prev ? { ...prev, watching: value } : prev))

  const toggle = async () => {
    const next = !watching
    setBusy(true)
    setWatching(next)
    try {
      await (next ? endpoints.watchEvent(eventId) : endpoints.unwatchEvent(eventId))
    } catch (err) {
      setWatching(!next)
      toast.error(t('event.watchFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const Icon = watching ? BellRingIcon : BellIcon
  return (
    <Tooltip content={watching ? t('event.stopHint') : t('event.startHint')}>
      <Button variant={watching ? 'secondary' : 'outline'} size="sm" aria-pressed={watching} disabled={busy} onClick={() => void toggle()}>
        <Icon aria-hidden />
        {watching ? t('event.watching') : t('event.watch')}
      </Button>
    </Tooltip>
  )
}
