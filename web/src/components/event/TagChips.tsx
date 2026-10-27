import { useTags } from '@/api/queries'
import { cn } from '@/lib/utils'

/** Tag names with their color swatch. Color is never the only signal: the name is always shown. */
export function TagChips({ ids, className }: { ids: string[]; className?: string }) {
  const { data } = useTags()
  const tags = ids.map((id) => data?.tags.find((t) => t.id === id)).filter((t) => t !== undefined)
  if (!tags.length) return null
  return (
    <ul className={cn('flex flex-wrap gap-1.5', className)}>
      {tags.map((tag) => (
        <li key={tag.id} className="inline-flex items-center gap-1.5 rounded-full border border-border px-2 py-0.5 text-xs font-medium">
          <span className="size-2.5 rounded-full" style={{ background: `var(--${tag.colorSlot})` }} aria-hidden />
          {tag.name}
        </li>
      ))}
    </ul>
  )
}
