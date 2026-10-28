import { useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { UploadAbortedError, type UploadHandle } from '@/api/client'
import { endpoints } from '@/api/endpoints'
import { useMeData } from '@/api/queries'
import type { Attachment } from '@/api/types'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatFileSize } from '@/lib/eventText'
import { applyStreamMessage } from '@/realtime/applyMessage'

export type UploadItem = { key: number; name: string; size: number; progress: number; error?: string }

type UploadTarget = { kind: 'event'; eventId: string } | { kind: 'comment'; commentId: string; eventId: string }

let seq = 1

/** Per-file XHR uploads with progress and cancel. Finished files are put into the query cache. */
export function useUploads(target: UploadTarget | null, existingCount: number) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const me = useMeData()
  const [items, setItems] = useState<UploadItem[]>([])
  const handles = useRef(new Map<number, UploadHandle<Attachment>>())

  useEffect(() => {
    const map = handles.current
    return () => {
      for (const h of map.values()) h.abort()
    }
  }, [])

  const limits = me.instance.limits
  const maxCount = target?.kind === 'comment' ? limits.maxPerComment : limits.maxPerEvent

  const add = useCallback(
    (files: FileList | File[]) => {
      if (!target) return
      const list = Array.from(files)
      const room = Math.max(0, maxCount - existingCount - items.length)
      if (list.length > room) {
        toast.error(t('upload.tooMany'), t('upload.tooManyBody', { count: maxCount }))
      }
      for (const file of list.slice(0, room)) {
        if (limits.maxSize && file.size > limits.maxSize) {
          toast.error(t('upload.tooLarge', { name: file.name }), t('upload.tooLargeBody', { size: formatFileSize(limits.maxSize) }))
          continue
        }
        if (file.size === 0) {
          toast.error(t('upload.empty', { name: file.name }))
          continue
        }
        const key = seq++
        const onProgress = (p: number) => setItems((prev) => prev.map((it) => (it.key === key ? { ...it, progress: p } : it)))
        const handle =
          target.kind === 'event'
            ? endpoints.uploadEventAttachment(target.eventId, file, onProgress)
            : endpoints.uploadCommentAttachment(target.commentId, file, onProgress)
        handles.current.set(key, handle)
        setItems((prev) => [...prev, { key, name: file.name, size: file.size, progress: 0 }])
        handle.promise
          .then((att) => {
            applyStreamMessage(qc, 'attachment.added', att)
            setItems((prev) => prev.filter((it) => it.key !== key))
          })
          .catch((err: unknown) => {
            if (err instanceof UploadAbortedError) {
              setItems((prev) => prev.filter((it) => it.key !== key))
              return
            }
            setItems((prev) => prev.map((it) => (it.key === key ? { ...it, error: errorMessage(err, t) } : it)))
          })
          .finally(() => handles.current.delete(key))
      }
    },
    [target, maxCount, existingCount, items.length, limits.maxSize, qc, t],
  )

  const cancel = (key: number) => {
    const h = handles.current.get(key)
    if (h) h.abort()
    else setItems((prev) => prev.filter((it) => it.key !== key))
  }

  return { items, add, cancel, uploading: items.some((i) => !i.error) }
}
