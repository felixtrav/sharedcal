import { AlignLeftIcon, CalendarIcon, ClockIcon, HashIcon, LinkIcon, ListIcon, SquareCheckIcon, TypeIcon, UsersIcon, type LucideIcon } from 'lucide-react'
import type { EventTypeField, FieldKind } from '@/api/types'

/** One glyph per field kind, shared by the editor and the read-only view. */
const KIND_ICONS: Record<FieldKind, LucideIcon> = {
  text: TypeIcon,
  longtext: AlignLeftIcon,
  number: HashIcon,
  date: CalendarIcon,
  time: ClockIcon,
  select: ListIcon,
  url: LinkIcon,
  checkbox: SquareCheckIcon,
  members: UsersIcon,
}

export function KindIcon({ kind, className }: { kind: FieldKind; className?: string }) {
  const Icon = KIND_ICONS[kind]
  return <Icon className={className ?? 'size-4'} aria-hidden />
}

/** Whether a field takes several values; person fields predate the flag and default to several. */
export function allowsMany(def: Pick<EventTypeField, 'kind' | 'multiple'>): boolean {
  return def.multiple ?? def.kind === 'members'
}

/** The http(s) URL a value points at, or null when it is not a safe link target. */
export function safeUrl(value: string): URL | null {
  try {
    const u = new URL(value.trim())
    return u.protocol === 'http:' || u.protocol === 'https:' ? u : null
  } catch {
    return null
  }
}
