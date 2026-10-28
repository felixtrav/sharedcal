import type { TFunction } from 'i18next'
import type { PresenceViewer } from '@/api/types'
import { useStore } from '@/lib/store'
import { presenceStore } from './stores'

const NONE: PresenceViewer[] = []

/** Who is viewing or editing an event right now. */
export function useEventViewers(eventId: string): PresenceViewer[] {
  return useStore(presenceStore)[eventId] ?? NONE
}

/** "Dana is viewing" / "Dana is editing". */
export function presenceLabel(t: TFunction, v: PresenceViewer): string {
  return v.state === 'editing' ? t('presence.editing', { name: v.name }) : t('presence.viewing', { name: v.name })
}
