import { PencilIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { UserAvatar } from '@/components/ui/avatar'
import { Tooltip } from '@/components/ui/tooltip'
import { useStore } from '@/lib/store'
import { presenceStore } from '@/realtime/stores'

/** "Dana is viewing" avatars on the open event panel. */
export function PresenceAvatars({ eventId }: { eventId: string }) {
  const { t } = useTranslation()
  const all = useStore(presenceStore)
  const viewers = all[eventId] ?? []
  if (!viewers.length) return null
  const summary = viewers.map((v) => (v.state === 'editing' ? t('presence.editing', { name: v.name }) : t('presence.viewing', { name: v.name }))).join(', ')
  return (
    <div className="flex items-center gap-2" role="status" aria-live="polite">
      <span className="sr-only">{summary}</span>
      <div className="flex -space-x-1.5 rtl:space-x-reverse" aria-hidden>
        {viewers.slice(0, 4).map((v) => (
          <Tooltip key={v.id} content={v.state === 'editing' ? t('presence.editing', { name: v.name }) : t('presence.viewing', { name: v.name })}>
            <span className="relative inline-flex rounded-full ring-2 ring-card">
              <UserAvatar name={v.name} src={v.avatarUrl} className="size-6" />
              {v.state === 'editing' ? (
                <span className="absolute -end-1 -bottom-1 flex size-3.5 items-center justify-center rounded-full bg-primary text-primary-foreground ring-2 ring-card">
                  <PencilIcon className="size-2" />
                </span>
              ) : null}
            </span>
          </Tooltip>
        ))}
      </div>
      {viewers.length === 1 ? (
        <span className="hidden text-xs text-muted-foreground sm:inline" aria-hidden>
          {viewers[0]?.state === 'editing' ? t('presence.editing', { name: viewers[0].name }) : t('presence.viewing', { name: viewers[0]?.name })}
        </span>
      ) : null}
    </div>
  )
}
