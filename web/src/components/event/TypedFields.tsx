import { CheckIcon, ExternalLinkIcon, XIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useDirectory, useMeData } from '@/api/queries'
import type { EventFieldValues, EventTypeField } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { UserAvatar } from '@/components/ui/avatar'
import { Linkified } from '@/lib/linkify'
import { formatPlainDate } from '@/lib/time'
import { cn } from '@/lib/utils'
import { KindIcon, safeUrl } from './fieldKinds'

/** `HH:MM` as a time of day in the viewer's clock style. */
function formatClock(hhmm: string, clock24h: boolean) {
  const m = /^(\d{2}):(\d{2})$/.exec(hhmm)
  if (!m) return hhmm
  return new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit', hourCycle: clock24h ? 'h23' : 'h12', timeZone: 'UTC' }).format(
    Date.UTC(1970, 0, 1, Number(m[1]), Number(m[2])),
  )
}

type Value = string | number | boolean | string[]

/** One field value, presented the way its kind is meant to be read. */
function FieldText({ def, value, timeOf }: { def: EventTypeField; value: Value; timeOf?: (hhmm: string) => string }) {
  const { t } = useTranslation()
  const { data: dir } = useDirectory()
  const clock24h = useMeData().preferences.clock24h
  if (Array.isArray(value) && def.kind === 'select') {
    return (
      <ul className="flex flex-wrap gap-1.5">
        {value.map((o) => (
          <li key={o}>
            <Badge variant="outline">{o}</Badge>
          </li>
        ))}
      </ul>
    )
  }
  if (Array.isArray(value)) {
    return (
      <ul className="flex flex-wrap gap-1.5">
        {value.map((id) => {
          const name = dir?.users.find((u) => u.id === id)?.name ?? t('eventTypes.unknownPerson')
          return (
            <li key={id} className="inline-flex items-center gap-1.5 rounded-full border border-border bg-background py-0.5 ps-0.5 pe-2.5 text-xs font-medium">
              <UserAvatar name={name} className="size-5" />
              {name}
            </li>
          )
        })}
      </ul>
    )
  }
  switch (def.kind) {
    case 'checkbox':
      return (
        <span
          className={cn(
            'inline-flex items-center gap-1 rounded-full px-2.5 py-0.5 text-xs font-medium',
            value ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground',
          )}
        >
          {value ? <CheckIcon className="size-3" aria-hidden /> : <XIcon className="size-3" aria-hidden />}
          {value ? t('common.yes') : t('common.no')}
        </span>
      )
    case 'number':
      return (
        <span className="text-base font-semibold tabular-nums">
          {typeof value === 'number' ? new Intl.NumberFormat(undefined, { maximumFractionDigits: 10 }).format(value) : String(value)}
        </span>
      )
    case 'date':
      return <span className="font-medium">{/^\d{4}-\d{2}-\d{2}$/.test(String(value)) ? formatPlainDate(String(value)) : String(value)}</span>
    case 'time':
      return <span className="font-medium tabular-nums">{timeOf ? timeOf(String(value)) : formatClock(String(value), clock24h)}</span>
    case 'select':
      return <Badge variant="outline">{String(value)}</Badge>
    case 'url': {
      const url = safeUrl(String(value))
      if (!url) return <span className="break-all">{String(value)}</span>
      return (
        <a
          href={url.href}
          target="_blank"
          rel="noopener noreferrer nofollow"
          className="inline-flex max-w-full items-center gap-1.5 rounded-md border border-border bg-background px-2 py-1 text-primary outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50"
        >
          <span className="truncate">{url.host.replace(/^www\./, '') + (url.pathname === '/' ? '' : url.pathname)}</span>
          <ExternalLinkIcon className="size-3.5 shrink-0" aria-hidden />
        </a>
      )
    }
    case 'longtext':
      return (
        <p className="border-s-2 border-border ps-3 break-words whitespace-pre-wrap">
          <Linkified text={String(value)} />
        </p>
      )
    default:
      return (
        <span className="break-words whitespace-pre-wrap">
          <Linkified text={String(value)} />
        </span>
      )
  }
}

/**
 * Values of a typed event, one labelled row per field in the type's order, each shown in a form suited to its kind.
 * Values whose field was removed from the type are not shown.
 */
export function TypedFields({
  defs,
  values,
  timeOf,
}: {
  defs: EventTypeField[]
  values: EventFieldValues
  /** Shows a time field for the viewer (zone and clock); without it the stored time is shown as is. */
  timeOf?: (hhmm: string) => string
}) {
  const shown = defs.filter((d) => values[d.key] !== undefined)
  if (!shown.length) return null
  return (
    <dl className="grid gap-3">
      {shown.map((d) => (
        <div key={d.key} className="flex gap-3">
          <span className="mt-0.5 text-muted-foreground" aria-hidden>
            <KindIcon kind={d.kind} />
          </span>
          <div className="grid min-w-0 flex-1 gap-0.5">
            <dt className="text-xs text-muted-foreground">{d.label}</dt>
            <dd className="min-w-0">
              <FieldText def={d} value={values[d.key] as Value} timeOf={timeOf} />
            </dd>
          </div>
        </div>
      ))}
    </dl>
  )
}
