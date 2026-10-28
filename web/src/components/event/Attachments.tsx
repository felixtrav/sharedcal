import { PaperclipIcon, UploadIcon } from 'lucide-react'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { EventDetail } from '@/api/types'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { AttachmentItems } from './AttachmentItems'
import { UploadList } from './UploadList'
import { useUploads } from './useUploads'

/** Event attachments section of the Details tab, with drag-and-drop upload. */
export function Attachments({ event }: { event: EventDetail }) {
  const { t } = useTranslation()
  const canAttach = event.permissions.attach
  const uploads = useUploads(canAttach ? { kind: 'event', eventId: event.id } : null, event.attachments.length)
  const input = useRef<HTMLInputElement>(null)
  const [over, setOver] = useState(false)

  if (!canAttach && event.attachments.length === 0) return null

  return (
    <section
      aria-labelledby="att-heading"
      className={cn('grid gap-3 rounded-xl', over && 'ring-2 ring-primary ring-offset-4 ring-offset-card')}
      onDragOver={(e) => {
        if (!canAttach || !e.dataTransfer.types.includes('Files')) return
        e.preventDefault()
        setOver(true)
      }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => {
        if (!canAttach) return
        e.preventDefault()
        setOver(false)
        if (e.dataTransfer.files.length) uploads.add(e.dataTransfer.files)
      }}
    >
      <div className="flex items-center justify-between">
        <h3 id="att-heading" className="flex items-center gap-2 text-sm font-semibold">
          <PaperclipIcon className="size-4 text-muted-foreground" aria-hidden />
          {t('attachments.title')}
          {event.attachments.length ? <span className="text-muted-foreground tabular-nums">({event.attachments.length})</span> : null}
        </h3>
        {canAttach ? (
          <>
            <Button size="xs" variant="outline" onClick={() => input.current?.click()}>
              <UploadIcon aria-hidden />
              {t('attachments.attach')}
            </Button>
            <input
              ref={input}
              type="file"
              multiple
              hidden
              aria-hidden
              tabIndex={-1}
              onChange={(e) => {
                if (e.target.files?.length) uploads.add(e.target.files)
                e.target.value = ''
              }}
            />
          </>
        ) : null}
      </div>
      <AttachmentItems attachments={event.attachments} />
      <UploadList items={uploads.items} onCancel={uploads.cancel} />
      {canAttach && event.attachments.length === 0 && uploads.items.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-5 text-center text-sm text-muted-foreground">{t('attachments.dropHint')}</p>
      ) : null}
    </section>
  )
}
