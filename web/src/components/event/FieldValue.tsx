import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import type { SuggestionField } from '@/api/types'
import { formatDateTime, formatPlainDate, type FormatOptions } from '@/lib/time'
import { describeRRule } from './recurrenceText'
import { TagChips } from './TagChips'

/** Format a date/time value as the API returns it: date, wall time, or instant. */
function formatTimeValue(v: string, fmt: FormatOptions): string {
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) return formatPlainDate(v)
  if (/(Z|[+-]\d{2}:?\d{2})$/.test(v)) return formatDateTime(Date.parse(v), fmt)
  // Wall-clock time: format its fields without shifting zones.
  const ms = Date.parse(`${v.slice(0, 19)}Z`)
  return Number.isNaN(ms) ? v : formatDateTime(ms, { ...fmt, timeZone: 'UTC' })
}

export function fieldLabel(field: string, t: TFunction) {
  return t(`event.fields.${field}`, { defaultValue: field })
}

/** Render one event field value for diffs (suggestions, conflicts, history). */
export function FieldValue({ field, value, fmt, startDate }: { field: SuggestionField | string; value: unknown; fmt: FormatOptions; startDate?: string }) {
  const { t } = useTranslation()
  if (value === null || value === undefined || value === '') {
    return <span className="text-muted-foreground italic">{t('common.empty')}</span>
  }
  switch (field) {
    case 'start':
    case 'end':
      return <span className="tabular-nums">{formatTimeValue(String(value), fmt)}</span>
    case 'allDay':
      return <span>{value ? t('common.yes') : t('common.no')}</span>
    case 'status':
      return <span>{t(`event.status.${String(value)}`, { defaultValue: String(value) })}</span>
    case 'tags':
      return Array.isArray(value) && value.length ? (
        <TagChips ids={value as string[]} />
      ) : (
        <span className="text-muted-foreground italic">{t('common.none')}</span>
      )
    case 'rrule':
      return <span>{describeRRule(String(value), startDate ?? new Date().toISOString().slice(0, 10), t)}</span>
    case 'timeZone':
      return <span>{String(value).replace(/_/g, ' ')}</span>
    case 'description':
      return <span className="line-clamp-6 break-words whitespace-pre-wrap">{String(value)}</span>
    default:
      return <span className="break-words whitespace-pre-wrap">{typeof value === 'string' ? value : JSON.stringify(value)}</span>
  }
}
