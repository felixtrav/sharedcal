import { ToggleGroup } from 'radix-ui'
import { cn } from '@/lib/utils'

type SegmentOption<T extends string> = { value: T; label: string; shortcut?: string }

/** A single-choice segmented control (radio-group semantics via Radix ToggleGroup). */
export function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
  className,
}: {
  value: T
  onChange: (v: T) => void
  options: SegmentOption<T>[]
  label: string
  className?: string
}) {
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => v && onChange(v as T)}
      aria-label={label}
      className={cn('inline-flex h-9 items-center rounded-lg bg-muted p-[3px] text-muted-foreground', className)}
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          aria-keyshortcuts={o.shortcut}
          className="inline-flex h-full items-center rounded-md px-3 text-sm font-medium whitespace-nowrap transition-[color,box-shadow] outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 data-[state=on]:bg-background data-[state=on]:text-foreground data-[state=on]:shadow-sm"
        >
          {o.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  )
}
