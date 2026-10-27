import { useQueryClient } from '@tanstack/react-query'
import { MessageSquareDiffIcon, PencilIcon, Trash2Icon } from 'lucide-react'
import { useState } from 'react'
import type { ScopeAsk } from './ScopeDialog'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useMeData } from '@/api/queries'
import type { EditScope, EventDetail } from '@/api/types'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import type { FormatOptions } from '@/lib/time'
import { Attachments } from './Attachments'
import { deleteEventWithUndo } from './deleteEvent'
import { EventForm } from './EventForm'
import { EventView } from './EventView'

export function DetailsTab({
  event,
  occurrence,
  fmt,
  onDirtyChange,
  onDeleted,
  scopePrompt,
}: {
  event: EventDetail
  occurrence: string | null
  fmt: FormatOptions
  onDirtyChange: (dirty: boolean) => void
  onDeleted: () => void
  scopePrompt: ScopeAsk
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const me = useMeData()
  const [editing, setEditing] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [confirmSuggestDelete, setConfirmSuggestDelete] = useState(false)
  const p = event.permissions
  const recurring = !!(event.rrule || occurrence)

  if (editing) {
    return (
      <EventForm
        mode={{ kind: p.edit ? 'edit' : 'suggest', event, occurrence }}
        fmt={fmt}
        scopePrompt={scopePrompt}
        onDirtyChange={onDirtyChange}
        onDone={() => {
          onDirtyChange(false)
          setEditing(false)
        }}
      />
    )
  }

  const doDelete = async () => {
    let scope: EditScope | undefined
    if (recurring) {
      const s = await scopePrompt('delete')
      if (!s) return
      scope = s
    }
    const ok = await deleteEventWithUndo({ qc, ev: event, t, scope, occurrence })
    if (ok) onDeleted()
  }

  const suggestDelete = async () => {
    try {
      await endpoints.suggestDelete(event.id, event.version)
      void qc.invalidateQueries({ queryKey: keys.suggestionsAll(event.id) })
      toast.success(t('suggestions.deleteSent'), t('suggestions.sentBody', { name: event.owner.name }))
    } catch (err) {
      toast.error(t('suggestions.sendFailed'), errorMessage(err, t))
    }
  }

  return (
    <div className="grid gap-6">
      <EventView event={event} fmt={fmt} canChangeOwner={me.permissions.admin && !event.deletedAt} />

      {p.edit || p.suggest || p.delete ? (
        <div className="flex flex-wrap gap-2">
          {p.edit ? (
            <Button size="sm" onClick={() => setEditing(true)}>
              <PencilIcon aria-hidden />
              {t('common.edit')}
            </Button>
          ) : p.suggest ? (
            <Button size="sm" onClick={() => setEditing(true)}>
              <MessageSquareDiffIcon aria-hidden />
              {t('suggestions.suggestChanges')}
            </Button>
          ) : null}
          {p.delete ? (
            <Button
              size="sm"
              variant="outline"
              className="text-destructive hover:text-destructive"
              onClick={() => (recurring ? void doDelete() : setConfirmDelete(true))}
            >
              <Trash2Icon aria-hidden />
              {t('common.delete')}
            </Button>
          ) : p.suggest ? (
            <Button size="sm" variant="outline" onClick={() => setConfirmSuggestDelete(true)}>
              <Trash2Icon aria-hidden />
              {t('suggestions.suggestDeletion')}
            </Button>
          ) : null}
        </div>
      ) : null}

      <Attachments event={event} />

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('event.deleteConfirmTitle', { title: event.title })}
        description={t('event.deleteConfirmBody')}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={() => void doDelete()}
      />
      <ConfirmDialog
        open={confirmSuggestDelete}
        onOpenChange={setConfirmSuggestDelete}
        title={t('suggestions.suggestDeletionTitle')}
        description={t('suggestions.suggestDeletionBody', { name: event.owner.name })}
        confirmLabel={t('suggestions.suggestDeletion')}
        onConfirm={() => void suggestDelete()}
      />
    </div>
  )
}
