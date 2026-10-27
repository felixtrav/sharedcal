import { useQueryClient } from '@tanstack/react-query'
import { DownloadIcon, FileIcon, FileSpreadsheetIcon, FileTextIcon, Loader2Icon, Trash2Icon } from 'lucide-react'
import { lazy, Suspense, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import type { Attachment } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatFileSize } from '@/lib/eventText'
import { applyStreamMessage } from '@/realtime/applyMessage'

// The lightbox is code-split; it loads on first open.
const Lightbox = lazy(() => import('./Lightbox'))

function iconFor(a: Attachment) {
  if (/spreadsheet|csv|excel|sheet/.test(a.contentType)) return FileSpreadsheetIcon
  if (/pdf|text|word|document|markdown|presentation/.test(a.contentType)) return FileTextIcon
  return FileIcon
}

/** Thumbnails strip (images, with lightbox) + file rows (icon, name, size, download). */
export function AttachmentItems({ attachments, compact }: { attachments: Attachment[]; compact?: boolean }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [lightbox, setLightbox] = useState<number | null>(null)
  const images = attachments.filter((a) => a.isImage && a.thumbUrl && a.scanStatus !== 'pending')
  const files = attachments.filter((a) => !images.includes(a))

  const remove = async (a: Attachment) => {
    try {
      await endpoints.deleteAttachment(a.id)
      applyStreamMessage(qc, 'attachment.removed', { id: a.id, eventId: a.eventId, commentId: a.commentId })
      toast({ title: t('attachments.removed', { name: a.filename }) })
    } catch (err) {
      toast.error(t('attachments.removeFailed'), errorMessage(err, t))
    }
  }

  return (
    <div className="grid gap-3">
      {images.length ? (
        <ul className="flex gap-2 overflow-x-auto pb-1" aria-label={t('attachments.images')}>
          {images.map((a, i) => (
            <li key={a.id} className="group relative shrink-0">
              <button
                type="button"
                onClick={() => setLightbox(i)}
                className="block overflow-hidden rounded-lg border border-border outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
                aria-label={t('attachments.openImage', { name: a.filename })}
              >
                <img src={a.thumbUrl ?? ''} alt="" loading="lazy" className={compact ? 'size-16 object-cover' : 'size-24 object-cover'} />
              </button>
              {a.permissions.delete ? (
                <button
                  type="button"
                  onClick={() => void remove(a)}
                  aria-label={t('attachments.remove', { name: a.filename })}
                  className="absolute end-1 top-1 rounded-md bg-background/90 p-1 opacity-0 shadow group-hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                >
                  <Trash2Icon className="size-3.5" />
                </button>
              ) : null}
            </li>
          ))}
        </ul>
      ) : null}
      {files.length ? (
        <ul className="grid gap-1.5">
          {files.map((a) => {
            const Icon = iconFor(a)
            return (
              <li key={a.id} className="flex items-center gap-3 rounded-lg border border-border px-3 py-2 text-sm">
                <Icon className="size-5 shrink-0 text-muted-foreground" aria-hidden />
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium">{a.filename}</p>
                  <p className="text-xs text-muted-foreground tabular-nums">{formatFileSize(a.size)}</p>
                </div>
                {a.scanStatus === 'pending' ? (
                  <Badge variant="outline">
                    <Loader2Icon className="animate-spin motion-reduce:animate-none" aria-hidden />
                    {t('attachments.scanning')}
                  </Badge>
                ) : (
                  <a
                    href={a.url}
                    download={a.filename}
                    className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                    aria-label={t('attachments.download', { name: a.filename })}
                  >
                    <DownloadIcon className="size-4" />
                  </a>
                )}
                {a.permissions.delete ? (
                  <button
                    type="button"
                    onClick={() => void remove(a)}
                    aria-label={t('attachments.remove', { name: a.filename })}
                    className="rounded-md p-1.5 text-muted-foreground hover:bg-accent hover:text-destructive focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
                  >
                    <Trash2Icon className="size-4" />
                  </button>
                ) : null}
              </li>
            )
          })}
        </ul>
      ) : null}
      {lightbox !== null ? (
        <Suspense fallback={null}>
          <Lightbox images={images} index={lightbox} onIndex={setLightbox} onClose={() => setLightbox(null)} />
        </Suspense>
      ) : null}
    </div>
  )
}
