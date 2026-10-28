import { PencilIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { UserAvatar } from '@/components/ui/avatar'
import { presenceLabel, useEventViewers } from '@/realtime/presence'

/** Compact presence on a calendar event chip: up to two avatars plus a count. */
export function ChipPresence({ eventId }: { eventId: string }) {
  const { t } = useTranslation()
  const viewers = useEventViewers(eventId)
  if (!viewers.length) return null
  const editing = viewers.some((v) => v.state === 'editing')
  const summary = viewers.map((v) => presenceLabel(t, v)).join(', ')
  return (
    <>
      <span className="sr-only">{summary}</span>
      <span className="ms-auto flex shrink-0 items-center -space-x-1 rtl:space-x-reverse" aria-hidden title={summary} data-testid="chip-presence">
        {viewers.slice(0, 2).map((v) => (
          <UserAvatar key={v.id} name={v.name} src={v.avatarUrl} className="size-3.5 text-[0.5rem] ring-1 ring-current/40" />
        ))}
        {viewers.length > 2 ? <span className="ps-1.5 text-[0.625rem] tabular-nums">+{viewers.length - 2}</span> : null}
        {editing ? <PencilIcon className="ms-1 size-2.5" /> : null}
      </span>
    </>
  )
}
