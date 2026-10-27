import { Avatar as AvatarPrimitive } from 'radix-ui'
import { cn } from '@/lib/utils'

export function initials(name: string) {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  const first = parts[0]?.[0] ?? '?'
  const last = parts.length > 1 ? (parts[parts.length - 1]?.[0] ?? '') : ''
  return (first + last).toUpperCase()
}

/** User avatar with initials fallback. Decorative unless `label` is given. */
export function UserAvatar({
  name,
  src,
  className,
  label,
}: {
  name: string
  src?: string | null
  className?: string
  label?: string
}) {
  return (
    <AvatarPrimitive.Root
      className={cn('relative inline-flex size-8 shrink-0 overflow-hidden rounded-full bg-muted select-none', className)}
      role={label ? 'img' : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    >
      {src ? <AvatarPrimitive.Image src={src} alt="" className="aspect-square size-full object-cover" referrerPolicy="no-referrer" /> : null}
      <AvatarPrimitive.Fallback className="flex size-full items-center justify-center text-[0.7em] font-semibold text-muted-foreground">
        {initials(name)}
      </AvatarPrimitive.Fallback>
    </AvatarPrimitive.Root>
  )
}
