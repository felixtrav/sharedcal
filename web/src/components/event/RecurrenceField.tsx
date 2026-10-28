import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Input, NativeSelect } from '@/components/ui/input'
import {
  buildRRule,
  defaultRule,
  detectPreset,
  nthOfMonth,
  parseDate,
  parseRRule,
  presetToRRule,
  weekdayOf,
  WEEKDAYS,
  type Freq,
  type Preset,
  type RecurrenceRule,
} from '@/lib/recurrence'
import { addDays, formatPlainDate } from '@/lib/time'
import { cn } from '@/lib/utils'
import { describeRule, weekdayName } from './recurrenceText'

export function RecurrenceField({
  id,
  value,
  onChange,
  startDate,
  allDay,
}: {
  id: string
  value: string
  onChange: (rrule: string) => void
  startDate: string
  allDay: boolean
}) {
  const { t } = useTranslation()
  const start = parseDate(startDate)
  const [mode, setMode] = useState<Preset>(() => detectPreset(value, start))

  // Presets follow the start date (e.g. "Weekly on Thursday" becomes Friday).
  useEffect(() => {
    if (mode === 'none' || mode === 'custom') return
    const next = presetToRRule(mode, parseDate(startDate))
    if (next !== value) onChange(next)
  }, [mode, startDate, value, onChange])

  const nth = nthOfMonth(start)
  const day = weekdayName(weekdayOf(start))
  const presetLabels: Record<Preset, string> = {
    none: t('recurrence.presets.none'),
    daily: t('recurrence.presets.daily'),
    weekdays: t('recurrence.presets.weekdays'),
    weekly: t('recurrence.presets.weekly', { day }),
    monthly: t('recurrence.presets.monthly', { nth: t(`recurrence.nth.${nth}`), day }),
    yearly: t('recurrence.presets.yearly', { date: formatPlainDate(startDate, { month: 'long', day: 'numeric' }) }),
    custom: t('recurrence.presets.custom'),
  }

  const rule: RecurrenceRule = parseRRule(value, start) ?? defaultRule(start)
  const update = (patch: Partial<RecurrenceRule>) => onChange(buildRRule({ ...rule, ...patch }, allDay))

  return (
    <div className="grid gap-3">
      <NativeSelect
        id={id}
        value={mode}
        onChange={(e) => {
          const m = e.target.value as Preset
          setMode(m)
          if (m === 'custom') onChange(buildRRule(parseRRule(value, start) ?? defaultRule(start), allDay))
          else onChange(presetToRRule(m, start))
        }}
      >
        {(Object.keys(presetLabels) as Preset[]).map((p) => (
          <option key={p} value={p}>
            {presetLabels[p]}
          </option>
        ))}
      </NativeSelect>

      {mode === 'custom' ? (
        <fieldset className="grid gap-3 rounded-lg border border-border p-3">
          <legend className="px-1 text-xs font-medium text-muted-foreground">{t('recurrence.custom')}</legend>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <label htmlFor={`${id}-interval`}>{t('recurrence.repeatEvery')}</label>
            <Input
              id={`${id}-interval`}
              type="number"
              min={1}
              max={999}
              value={rule.interval}
              onChange={(e) => update({ interval: Math.max(1, Number(e.target.value) || 1) })}
              className="w-20"
            />
            <label htmlFor={`${id}-freq`} className="sr-only">
              {t('recurrence.frequency')}
            </label>
            <NativeSelect id={`${id}-freq`} value={rule.freq} onChange={(e) => update({ freq: e.target.value as Freq })} className="w-auto">
              {(['DAILY', 'WEEKLY', 'MONTHLY', 'YEARLY'] as const).map((f) => (
                <option key={f} value={f}>
                  {t(`recurrence.units.${f.toLowerCase()}`, { count: rule.interval })}
                </option>
              ))}
            </NativeSelect>
          </div>

          {rule.freq === 'WEEKLY' ? (
            <div role="group" aria-label={t('recurrence.onDaysLabel')} className="flex flex-wrap gap-1">
              {WEEKDAYS.map((d) => {
                const on = rule.byDay.includes(d)
                return (
                  <button
                    key={d}
                    type="button"
                    aria-pressed={on}
                    aria-label={weekdayName(d)}
                    onClick={() => {
                      const next = on ? rule.byDay.filter((x) => x !== d) : [...rule.byDay, d]
                      update({ byDay: next.length ? next : [d] })
                    }}
                    className={cn(
                      'size-8 rounded-full border text-xs font-medium outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
                      on ? 'border-primary bg-primary text-primary-foreground' : 'border-border hover:bg-accent',
                    )}
                  >
                    <span aria-hidden>{weekdayName(d, 'short').slice(0, 2)}</span>
                  </button>
                )
              })}
            </div>
          ) : null}

          {rule.freq === 'MONTHLY' ? (
            <div role="radiogroup" aria-label={t('recurrence.monthlyOn')} className="grid gap-1.5 text-sm">
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name={`${id}-monthly`}
                  checked={rule.monthlyMode === 'day'}
                  onChange={() => update({ monthlyMode: 'day', monthDay: start.day })}
                  className="accent-(--primary)"
                />
                {t('recurrence.onMonthDay', { day: start.day })}
              </label>
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name={`${id}-monthly`}
                  checked={rule.monthlyMode === 'nth'}
                  onChange={() => update({ monthlyMode: 'nth', nth, nthWeekday: weekdayOf(start) })}
                  className="accent-(--primary)"
                />
                {t('recurrence.onNth', { nth: t(`recurrence.nth.${nth}`), day })}
              </label>
            </div>
          ) : null}

          <div role="radiogroup" aria-label={t('recurrence.ends')} className="grid gap-1.5 text-sm">
            <span className="text-xs font-medium text-muted-foreground" aria-hidden>
              {t('recurrence.ends')}
            </span>
            <label className="flex items-center gap-2">
              <input
                type="radio"
                name={`${id}-end`}
                checked={rule.end.type === 'never'}
                onChange={() => update({ end: { type: 'never' } })}
                className="accent-(--primary)"
              />
              {t('recurrence.never')}
            </label>
            <div className="flex items-center gap-2">
              <input
                id={`${id}-end-count`}
                type="radio"
                name={`${id}-end`}
                checked={rule.end.type === 'count'}
                onChange={() => update({ end: { type: 'count', count: 10 } })}
                className="accent-(--primary)"
              />
              <label htmlFor={`${id}-end-count`}>{t('recurrence.after')}</label>
              <Input
                type="number"
                min={1}
                max={1000}
                aria-label={t('recurrence.occurrences')}
                disabled={rule.end.type !== 'count'}
                value={rule.end.type === 'count' ? rule.end.count : 10}
                onChange={(e) => update({ end: { type: 'count', count: Math.max(1, Number(e.target.value) || 1) } })}
                className="h-8 w-20"
              />
              <span>{t('recurrence.occurrencesSuffix')}</span>
            </div>
            <div className="flex items-center gap-2">
              <input
                id={`${id}-end-until`}
                type="radio"
                name={`${id}-end`}
                checked={rule.end.type === 'until'}
                onChange={() => update({ end: { type: 'until', until: addDays(startDate, 30) } })}
                className="accent-(--primary)"
              />
              <label htmlFor={`${id}-end-until`}>{t('recurrence.on')}</label>
              <Input
                type="date"
                aria-label={t('recurrence.endDate')}
                disabled={rule.end.type !== 'until'}
                min={startDate}
                value={rule.end.type === 'until' ? rule.end.until : addDays(startDate, 30)}
                onChange={(e) => e.target.value && update({ end: { type: 'until', until: e.target.value } })}
                className="h-8 w-auto"
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground" aria-live="polite">
            {describeRule(rule, t)}
          </p>
        </fieldset>
      ) : null}
    </div>
  )
}
