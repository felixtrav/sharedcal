import type { QueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { removeEventFromCache, restoreSnapshot, snapshotEvent, storeEvent } from '@/api/eventCache'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { CalEvent, EditScope } from '@/api/types'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { markOwnWrite } from '@/realtime/stores'

const UNDO_MS = 10_000

/** Delete an event (or occurrences) and offer "Undo" for 10 seconds. Resolves true on success. */
export async function deleteEventWithUndo(opts: {
  qc: QueryClient
  ev: CalEvent
  t: TFunction
  scope?: EditScope
  occurrence?: string | null
}): Promise<boolean> {
  const { qc, ev, t, scope, occurrence } = opts
  const snap = snapshotEvent(qc, ev.id)
  const wholeSeries = !scope || scope === 'all'
  markOwnWrite(ev.id)
  removeEventFromCache(qc, ev.id, wholeSeries ? undefined : scope === 'this' ? occurrence : undefined)
  try {
    await endpoints.deleteEvent(ev.id, ev.version, scope ? { scope, occurrence } : undefined)
  } catch (err) {
    restoreSnapshot(qc, snap)
    toast.error(t('event.deleteFailed'), errorMessage(err, t))
    return false
  }

  if (!wholeSeries) {
    void qc.invalidateQueries({ queryKey: keys.eventsAll })
    void qc.invalidateQueries({ queryKey: keys.event(ev.id) })
    toast({ title: t('event.occurrenceDeleted') })
    return true
  }

  qc.removeQueries({ queryKey: keys.event(ev.id) })
  toast({
    title: t('event.deletedToast', { title: ev.title }),
    duration: UNDO_MS,
    action: {
      label: t('common.undo'),
      altText: t('event.undoDelete'),
      onClick: () => {
        markOwnWrite(ev.id)
        endpoints
          .restoreEvent(ev.id)
          .then((restored) => {
            storeEvent(qc, restored)
            void qc.invalidateQueries({ queryKey: keys.eventsAll })
            toast.success(t('event.restored'))
          })
          .catch((err: unknown) => toast.error(t('event.restoreFailed'), errorMessage(err, t)))
      },
    },
  })
  return true
}
