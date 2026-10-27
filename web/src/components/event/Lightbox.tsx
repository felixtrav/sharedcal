import { ChevronLeftIcon, ChevronRightIcon, DownloadIcon, XIcon } from 'lucide-react'
import { Dialog as D } from 'radix-ui'
import { useTranslation } from 'react-i18next'
import type { Attachment } from '@/api/types'

/** Accessible image viewer: focus-trapped dialog, ←/→ to navigate, Esc to close. */
export default function Lightbox({
  images,
  index,
  onIndex,
  onClose,
}: {
  images: Attachment[]
  index: number
  onIndex: (i: number) => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const img = images[index]
  if (!img) return null
  const prev = () => onIndex((index - 1 + images.length) % images.length)
  const next = () => onIndex((index + 1) % images.length)
  const btn =
    'rounded-full bg-black/50 p-2 text-white hover:bg-black/70 focus-visible:ring-[3px] focus-visible:ring-white/70 focus-visible:outline-none'

  return (
    <D.Root open onOpenChange={(o) => !o && onClose()}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-[70] bg-black/85 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <D.Content
          className="fixed inset-0 z-[70] flex flex-col items-center justify-center p-4 outline-none"
          onKeyDown={(e) => {
            if (e.key === 'ArrowLeft') {
              e.preventDefault()
              if (document.dir === 'rtl') next()
              else prev()
            } else if (e.key === 'ArrowRight') {
              e.preventDefault()
              if (document.dir === 'rtl') prev()
              else next()
            }
          }}
        >
          <D.Title className="sr-only">{img.filename}</D.Title>
          <D.Description className="sr-only">{t('lightbox.position', { index: index + 1, total: images.length })}</D.Description>
          <div className="absolute end-4 top-4 flex gap-2">
            <a href={img.url} download={img.filename} className={btn} aria-label={t('attachments.download', { name: img.filename })}>
              <DownloadIcon className="size-5" />
            </a>
            <D.Close className={btn} aria-label={t('common.close')}>
              <XIcon className="size-5" />
            </D.Close>
          </div>
          <img src={img.url} alt={img.filename} className="max-h-[85dvh] max-w-full rounded-lg object-contain shadow-2xl" />
          <p className="mt-3 text-sm text-white/80 tabular-nums" aria-hidden>
            {img.filename} · {index + 1} / {images.length}
          </p>
          {images.length > 1 ? (
            <>
              <button type="button" onClick={prev} className={`${btn} absolute start-4 top-1/2 -translate-y-1/2`} aria-label={t('lightbox.previous')}>
                <ChevronLeftIcon className="size-6 rtl:rotate-180" />
              </button>
              <button type="button" onClick={next} className={`${btn} absolute end-4 top-1/2 -translate-y-1/2`} aria-label={t('lightbox.next')}>
                <ChevronRightIcon className="size-6 rtl:rotate-180" />
              </button>
            </>
          ) : null}
        </D.Content>
      </D.Portal>
    </D.Root>
  )
}
