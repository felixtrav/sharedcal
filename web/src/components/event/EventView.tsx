import { CircleHelpIcon, ClockIcon, MapPinIcon, RepeatIcon, TagIcon, UserIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { CalEvent } from '@/api/types'
import { LazyMarkdown } from '@/components/LazyMarkdown'
import { Badge } from '@/components/ui/badge'
import { eventWhen } from '@/lib/eventText'
import { Linkified } from '@/lib/linkify'
import { formatPlainDate, type FormatOptions } from '@/lib/time'
import { OwnerField } from './OwnerField'
import { describeRRule } from './recurrenceText'
import { TagChips } from './TagChips'

function Row({ icon, label, children }: { icon: ReactNode; label: string; children: ReactNode }) {
  return (
    <div className="flex gap-3">
      <span className="mt-0.5 text-muted-foreground [&_svg]:size-4" aria-hidden>
        {icon}
      </span>
      <div className="min-w-0 flex-1">
        <span className="sr-only">{label}: </span>
        {children}
      </div>
    </div>
  )
}

export function EventView({ event, fmt, canChangeOwner = false }: { event: CalEvent; fmt: FormatOptions; canChangeOwner?: boolean }) {
  const { t } = useTranslation()
  const otherZone = !event.allDay && event.timeZone !== fmt.timeZone
  return (
    <div className="grid gap-4 text-sm">
      <Row icon={<ClockIcon />} label={t('event.fields.when')}>
        <p className="font-medium tabular-nums">{eventWhen(event, fmt, t)}</p>
        {otherZone ? (
          <p className="text-muted-foreground tabular-nums">
            {t('event.inZone', { when: eventWhen(event, { ...fmt, timeZone: event.timeZone }, t), zone: event.timeZone.replace(/_/g, ' ') })}
          </p>
        ) : null}
      </Row>
      {event.rrule ? (
        <Row icon={<RepeatIcon />} label={t('event.fields.recurrence')}>
          <p>{describeRRule(event.rrule, event.start.slice(0, 10), t)}</p>
          {event.occurrence ? (
            <p className="text-muted-foreground">
              {event.overridden ? t('event.occurrenceModified') : t('event.occurrenceOf', { date: formatPlainDate(event.start.slice(0, 10)) })}
            </p>
          ) : null}
        </Row>
      ) : null}
      {event.status === 'tentative' ? (
        <Row icon={<CircleHelpIcon />} label={t('event.fields.status')}>
          <Badge variant="outline">{t('event.status.tentative')}</Badge>
        </Row>
      ) : null}
      {event.location ? (
        <Row icon={<MapPinIcon />} label={t('event.fields.location')}>
          <p className="break-words whitespace-pre-wrap">
            <Linkified text={event.location} />
          </p>
        </Row>
      ) : null}
      {event.tags.length ? (
        <Row icon={<TagIcon />} label={t('event.fields.tags')}>
          <TagChips ids={event.tags} />
        </Row>
      ) : null}
      <Row icon={<UserIcon />} label={t('event.fields.owner')}>
        <OwnerField event={event} canChange={canChangeOwner} />
      </Row>
      {event.description ? (
        <div className="rounded-lg border border-border bg-background p-3">
          <h3 className="sr-only">{t('event.fields.description')}</h3>
          <LazyMarkdown source={event.description} className="text-sm" />
        </div>
      ) : null}
    </div>
  )
}
