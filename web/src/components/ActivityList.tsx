import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import type { ActivityItem } from '@/api/types'
import { UserAvatar } from '@/components/ui/avatar'
import { fieldLabel, FieldValue } from '@/components/event/FieldValue'
import { formatDateTime, formatRelative, type FormatOptions } from '@/lib/time'

const IGNORED = new Set(['version', 'updatedAt', 'createdAt', 'id', 'counts', 'permissions', 'startUtc', 'endUtc', 'owner'])

function changedKeys(before: Record<string, unknown> | null, after: Record<string, unknown> | null) {
  if (!before || !after) return []
  const keys = new Set([...Object.keys(before), ...Object.keys(after)])
  return [...keys].filter((k) => !IGNORED.has(k) && JSON.stringify(before[k]) !== JSON.stringify(after[k]))
}

function actionLabel(action: string, t: (k: string, o?: Record<string, unknown>) => string) {
  return t(`activity.actions.${action.replace(/\./g, '_')}`, { defaultValue: action })
}

/** Audit log rows (event History tab and the Activity page). */
export function ActivityList({ items, fmt, showEventLinks }: { items: ActivityItem[]; fmt: FormatOptions; showEventLinks?: boolean }) {
  const { t } = useTranslation()
  return (
    <ol className="relative grid gap-4 border-s border-border ps-5">
      {items.map((it) => {
        const changes = changedKeys(it.before, it.after)
        const title = (it.after?.title ?? it.before?.title) as string | undefined
        return (
          <li key={it.id} className="relative">
            <span
              className="absolute -start-[1.6875rem] top-1 flex size-3 items-center justify-center rounded-full border-2 border-card bg-muted-foreground/60"
              aria-hidden
            />
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
              {it.actor ? <UserAvatar name={it.actor.name} src={it.actor.avatarUrl} className="size-5" /> : null}
              <span className="font-medium">{it.actor?.name ?? t('activity.system')}</span>
              <span className="text-muted-foreground">{actionLabel(it.action, t)}</span>
              {showEventLinks && it.eventId ? (
                <Link to="/" search={{ event: it.eventId }} className="truncate font-medium text-primary underline-offset-2 hover:underline">
                  {title ?? t('activity.viewEvent')}
                </Link>
              ) : null}
              <time dateTime={it.createdAt} title={formatDateTime(Date.parse(it.createdAt), fmt)} className="text-xs text-muted-foreground">
                {formatRelative(it.createdAt)}
              </time>
            </div>
            {changes.length ? (
              <details className="mt-1.5 text-sm">
                <summary className="cursor-pointer text-xs text-muted-foreground hover:text-foreground">
                  {t('activity.changedFields', { fields: changes.map((k) => fieldLabel(k, t)).join(', ') })}
                </summary>
                <dl className="mt-2 grid gap-2 rounded-lg bg-muted p-3">
                  {changes.map((k) => (
                    <div key={k} className="grid gap-0.5">
                      <dt className="text-xs font-medium">{fieldLabel(k, t)}</dt>
                      <dd className="grid grid-cols-[auto_1fr] gap-x-2 text-xs">
                        <span className="text-muted-foreground">{t('activity.before')}</span>
                        <span className="min-w-0 line-through decoration-muted-foreground/50">
                          <FieldValue field={k} value={it.before?.[k]} fmt={fmt} />
                        </span>
                        <span className="text-muted-foreground">{t('activity.after')}</span>
                        <span className="min-w-0">
                          <FieldValue field={k} value={it.after?.[k]} fmt={fmt} />
                        </span>
                      </dd>
                    </div>
                  ))}
                </dl>
              </details>
            ) : null}
          </li>
        )
      })}
    </ol>
  )
}
