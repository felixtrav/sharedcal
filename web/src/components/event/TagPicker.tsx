import { CheckIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useTags } from '@/api/queries'
import { cn } from '@/lib/utils'

/** Multi-select of admin-defined tags as toggle chips. Order of selection is kept (the first sets the color). */
export function TagPicker({ value, onChange, labelledBy }: { value: string[]; onChange: (ids: string[]) => void; labelledBy: string }) {
  const { t } = useTranslation()
  const { data } = useTags()
  const tags = data?.tags ?? []
  if (!tags.length) return <p className="text-sm text-muted-foreground">{t('event.noTagsDefined')}</p>
  return (
    <div role="group" aria-labelledby={labelledBy} className="flex flex-wrap gap-1.5">
      {tags.map((tag) => {
        const on = value.includes(tag.id)
        const idx = value.indexOf(tag.id)
        return (
          <button
            key={tag.id}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== tag.id) : [...value, tag.id])}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-medium transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
              on ? 'border-transparent' : 'border-border hover:bg-accent',
            )}
            style={on ? { background: `var(--${tag.colorSlot})`, color: `var(--${tag.colorSlot}-foreground)` } : undefined}
          >
            {on ? (
              <CheckIcon className="size-3" aria-hidden />
            ) : (
              <span className="size-2.5 rounded-full" style={{ background: `var(--${tag.colorSlot})` }} aria-hidden />
            )}
            {tag.name}
            {on && idx === 0 && value.length > 1 ? <span className="sr-only">{t('event.primaryTag')}</span> : null}
          </button>
        )
      })}
    </div>
  )
}
