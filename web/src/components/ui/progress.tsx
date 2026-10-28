import { Progress as ProgressPrimitive } from 'radix-ui'
import { cn } from '@/lib/utils'

export function Progress({ value, className, label }: { value: number; className?: string; label: string }) {
  const pct = Math.round(Math.max(0, Math.min(1, value)) * 100)
  return (
    <ProgressPrimitive.Root value={pct} aria-label={label} className={cn('relative h-1.5 w-full overflow-hidden rounded-full bg-muted', className)}>
      <ProgressPrimitive.Indicator className="h-full bg-primary transition-[width] duration-150" style={{ width: `${pct}%` }} />
    </ProgressPrimitive.Root>
  )
}
