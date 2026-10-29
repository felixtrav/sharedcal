import type { TFunction } from 'i18next'
import type { UserRef } from '@/api/types'

/** What to call an event's owner. Events of ownerless types belong to the system, not a person. */
export function ownerName(owner: UserRef, t: TFunction): string {
  return owner.system ? t('event.systemOwner') : owner.name
}

/** "owned by Alice", or just "System" for events nobody owns. */
export function ownerLabel(owner: UserRef, t: TFunction): string {
  return owner.system ? t('event.systemOwner') : t('event.ownedBy', { name: owner.name })
}
