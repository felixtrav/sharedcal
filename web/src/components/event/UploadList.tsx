import { XIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Progress } from '@/components/ui/progress'
import { formatFileSize } from '@/lib/eventText'
import type { UploadItem } from './useUploads'

export function UploadList({ items, onCancel }: { items: UploadItem[]; onCancel: (key: number) => void }) {
  const { t } = useTranslation()
  if (!items.length) return null
  return (
    <ul className="grid gap-2" aria-label={t('upload.inProgress')}>
      {items.map((it) => (
        <li key={it.key} className="grid gap-1.5 rounded-lg border border-border p-2.5 text-sm">
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate font-medium">{it.name}</span>
            <span className="text-xs text-muted-foreground tabular-nums">{formatFileSize(it.size)}</span>
            <button
              type="button"
              onClick={() => onCancel(it.key)}
              aria-label={it.error ? t('upload.dismiss', { name: it.name }) : t('upload.cancel', { name: it.name })}
              className="rounded p-0.5 text-muted-foreground hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <XIcon className="size-4" />
            </button>
          </div>
          {it.error ? (
            <p role="alert" className="text-xs text-destructive">
              {it.error}
            </p>
          ) : (
            <Progress value={it.progress} label={t('upload.progress', { name: it.name })} />
          )}
        </li>
      ))}
    </ul>
  )
}
