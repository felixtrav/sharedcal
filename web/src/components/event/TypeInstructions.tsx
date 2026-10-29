import { InfoIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { EventType } from '@/api/types'
import { Linkified } from '@/lib/linkify'

/** What an admin wants writers to know when they create an event of this type. */
export function TypeInstructions({ type }: { type: Pick<EventType, 'name' | 'instructions'> }) {
  const { t } = useTranslation()
  if (!type.instructions) return null
  return (
    <aside
      aria-label={t('eventTypes.instructionsFor', { name: type.name })}
      className="flex gap-2.5 rounded-lg border border-border bg-muted/50 px-3 py-2.5 text-sm"
    >
      <InfoIcon className="mt-0.5 size-4 shrink-0 text-muted-foreground" aria-hidden />
      <p className="min-w-0 break-words whitespace-pre-wrap">
        <Linkified text={type.instructions} />
      </p>
    </aside>
  )
}
