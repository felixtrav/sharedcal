import { useTranslation } from 'react-i18next'
import { TAG_SLOTS, type TagSlot } from '@/api/types'
import { cn } from '@/lib/utils'

/** Radio group of the 8 theme tag slots, shown as swatches. */
export function SlotPicker({ value, onChange, label, name }: { value: TagSlot; onChange: (s: TagSlot) => void; label: string; name: string }) {
  const { t } = useTranslation()
  return (
    <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-1.5">
      {TAG_SLOTS.map((slot, i) => (
        <label key={slot} className="relative cursor-pointer">
          <input type="radio" name={name} value={slot} checked={value === slot} onChange={() => onChange(slot)} className="peer sr-only" />
          <span
            className={cn(
              'flex size-7 items-center justify-center rounded-full text-[0.625rem] font-semibold ring-offset-2 ring-offset-card peer-focus-visible:ring-[3px] peer-focus-visible:ring-ring/60',
              value === slot && 'ring-2 ring-foreground',
            )}
            style={{ background: `var(--${slot})`, color: `var(--${slot}-foreground)` }}
            aria-hidden
          >
            {i + 1}
          </span>
          <span className="sr-only">{t('tags.slot', { n: i + 1 })}</span>
        </label>
      ))}
    </div>
  )
}
