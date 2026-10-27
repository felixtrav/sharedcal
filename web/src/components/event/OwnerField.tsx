import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { isApiError } from '@/api/client'
import { storeEvent } from '@/api/eventCache'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { CalEvent } from '@/api/types'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { NativeSelect } from '@/components/ui/input'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { clearOwnWrite, markOwnWrite } from '@/realtime/stores'

/**
 * The owner line of an event. Admins can hand the event (the whole series,
 * for recurring events) to another active writer or admin.
 */
export function OwnerField({ event, canChange }: { event: CalEvent; canChange: boolean }) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="inline-flex items-center gap-2">
        <UserAvatar name={event.owner.name} src={event.owner.avatarUrl} className="size-5" />
        {t('event.ownedBy', { name: event.owner.name })}
      </span>
      {canChange && !editing ? (
        <Button variant="link" size="xs" className="h-auto p-0" onClick={() => setEditing(true)} aria-label={t('event.changeOwnerFor', { title: event.title })}>
          {t('event.changeOwner')}
        </Button>
      ) : null}
      {editing ? <OwnerPicker event={event} onDone={() => setEditing(false)} /> : null}
    </div>
  )
}

function OwnerPicker({ event, onDone }: { event: CalEvent; onDone: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useQuery({ queryKey: keys.users, queryFn: endpoints.users })
  const candidates = (data?.users ?? []).filter((u) => u.active && !u.erased && u.role !== 'viewer')
  const [to, setTo] = useState(event.owner.id)
  const [busy, setBusy] = useState(false)

  const save = async () => {
    if (to === event.owner.id) return onDone()
    setBusy(true)
    markOwnWrite(event.id)
    try {
      // No scope: ownership always applies to the whole series.
      const updated = await endpoints.updateEvent(event.id, event.version, { ownerId: to })
      markOwnWrite(updated.id, updated.version)
      storeEvent(qc, updated)
      toast.success(t('event.ownerChanged', { name: updated.owner.name }))
      onDone()
    } catch (err) {
      if (isApiError(err) && err.status === 409) {
        void qc.invalidateQueries({ queryKey: keys.event(event.id) })
        toast.error(t('event.ownerChangeConflict'))
      } else {
        toast.error(t('event.ownerChangeFailed'), errorMessage(err, t))
      }
    } finally {
      clearOwnWrite(event.id)
      setBusy(false)
    }
  }

  return (
    <div className="flex w-full flex-wrap items-center gap-2">
      <label htmlFor={`owner-${event.id}`} className="sr-only">
        {t('event.newOwner')}
      </label>
      <NativeSelect id={`owner-${event.id}`} value={to} onChange={(e) => setTo(e.target.value)} className="min-w-0 flex-1" disabled={!data}>
        {candidates.some((u) => u.id === event.owner.id) ? null : <option value={event.owner.id}>{event.owner.name}</option>}
        {candidates.map((u) => (
          <option key={u.id} value={u.id}>
            {u.name}
          </option>
        ))}
      </NativeSelect>
      <Button size="xs" variant="outline" onClick={onDone}>
        {t('common.cancel')}
      </Button>
      <Button size="xs" disabled={busy || !data || to === event.owner.id} onClick={() => void save()}>
        {t('common.save')}
      </Button>
    </div>
  )
}
