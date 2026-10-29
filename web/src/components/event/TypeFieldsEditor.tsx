import { CheckIcon, ExternalLinkIcon, MinusIcon, PlusIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Controller, type Control, type FieldErrors } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { useDirectory } from '@/api/queries'
import type { EventType, EventTypeField } from '@/api/types'
import { Button } from '@/components/ui/button'
import { UserAvatar } from '@/components/ui/avatar'
import { describedBy } from '@/components/ui/field'
import { Input, NativeSelect, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import { allowsMany, KindIcon, safeUrl } from './fieldKinds'
import type { EventFormValues } from './formModel'

const inputId = (key: string) => `ev-field-${key}`

/** Inputs for the predefined fields of an event type; replaces the free-form description. */
export function TypeFieldsEditor({
  type,
  control,
  errors,
  timeZone,
}: {
  type: EventType
  control: Control<EventFormValues>
  errors: FieldErrors<EventFormValues>
  /** The event's time zone, which time fields are in. */
  timeZone?: string
}) {
  const { t } = useTranslation()
  if (!type.fields.length) return <p className="text-sm text-muted-foreground">{t('eventTypes.noFields', { name: type.name })}</p>
  return (
    <fieldset className="grid gap-4">
      <legend className="mb-1 text-sm font-medium">{t('eventTypes.detailsOf', { name: type.name })}</legend>
      {type.fields.map((def) => (
        <Controller
          key={def.key}
          control={control}
          name={`fields.${def.key}`}
          render={({ field }) => {
            const error = errors.fields?.[def.key]?.message
            const id = inputId(def.key)
            return (
              <FieldInput
                def={def}
                id={id}
                value={field.value}
                onChange={field.onChange}
                onBlur={field.onBlur}
                error={typeof error === 'string' ? error : undefined}
                timeZone={timeZone}
              />
            )
          }}
        />
      ))}
    </fieldset>
  )
}

const CHOICE_CHIPS_MAX = 5

/** A labelled shell for one field: kind glyph, label and error, with the control wired through `id`. */
function FieldShell({ def, id, error, children }: { def: EventTypeField; id: string; error?: string; children: ReactNode }) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id} id={`${id}-label`} className="gap-1.5">
        <KindIcon kind={def.kind} className="size-3.5 text-muted-foreground" />
        {def.required ? `${def.label} *` : def.label}
      </Label>
      {children}
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-xs font-medium text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

function FieldInput({
  def,
  id,
  value,
  onChange,
  onBlur,
  error,
  timeZone,
}: {
  def: EventTypeField
  id: string
  value: string | boolean | string[] | undefined
  onChange: (v: string | boolean | string[]) => void
  onBlur: () => void
  error?: string
  timeZone?: string
}) {
  const { t } = useTranslation()
  const picked = Array.isArray(value) ? value : typeof value === 'string' && value ? [value] : []
  const text = typeof value === 'string' ? value : def.kind === 'select' ? (picked[0] ?? '') : ''
  const common = { id, onBlur, 'aria-invalid': !!error, 'aria-required': def.required || undefined, 'aria-describedby': describedBy(id, error) }

  switch (def.kind) {
    case 'checkbox':
      return (
        <div className="flex items-center justify-between gap-3 rounded-lg border border-border px-3 py-2.5">
          <Label htmlFor={id} className="gap-2">
            <KindIcon kind="checkbox" className="size-4 text-muted-foreground" />
            {def.label}
          </Label>
          <Switch id={id} checked={value === true} onCheckedChange={onChange} />
        </div>
      )
    case 'members':
      return (
        <div className="grid gap-1.5">
          <span id={`${id}-label`} className="flex items-center gap-1.5 text-sm font-medium">
            <KindIcon kind="members" className="size-3.5 text-muted-foreground" />
            {def.required ? `${def.label} *` : def.label}
          </span>
          <MembersPicker labelledBy={`${id}-label`} multiple={allowsMany(def)} value={Array.isArray(value) ? value : []} onChange={onChange} />
          {error ? (
            <p role="alert" className="text-xs font-medium text-destructive">
              {error}
            </p>
          ) : null}
        </div>
      )
    case 'select': {
      const options = def.options ?? []
      const multiple = allowsMany(def)
      if (options.length > CHOICE_CHIPS_MAX && !multiple) {
        return (
          <FieldShell def={def} id={id} error={error}>
            <NativeSelect {...common} value={text} onChange={(e) => onChange(e.target.value)}>
              <option value="">{t('eventTypes.chooseOption')}</option>
              {options.map((o) => (
                <option key={o} value={o}>
                  {o}
                </option>
              ))}
            </NativeSelect>
          </FieldShell>
        )
      }
      return (
        <div className="grid gap-1.5">
          <span id={`${id}-label`} className="flex items-center gap-1.5 text-sm font-medium">
            <KindIcon kind="select" className="size-3.5 text-muted-foreground" />
            {def.required ? `${def.label} *` : def.label}
          </span>
          <div
            role={multiple ? 'group' : 'radiogroup'}
            id={id}
            aria-labelledby={`${id}-label`}
            aria-required={def.required || undefined}
            className="flex flex-wrap gap-1.5"
          >
            {options.map((o) => {
              const on = multiple ? picked.includes(o) : text === o
              return (
                <button
                  key={o}
                  type="button"
                  role={multiple ? undefined : 'radio'}
                  aria-checked={multiple ? undefined : on}
                  aria-pressed={multiple ? on : undefined}
                  // Single choice: choosing the selected option again clears it, unless the field is required.
                  onClick={() => onChange(multiple ? (on ? picked.filter((x) => x !== o) : [...picked, o]) : on && !def.required ? '' : o)}
                  className={cn(
                    'inline-flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm font-medium transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
                    on ? 'border-transparent bg-primary text-primary-foreground' : 'border-border hover:bg-accent',
                  )}
                >
                  {on ? <CheckIcon className="size-3.5" aria-hidden /> : null}
                  {o}
                </button>
              )
            })}
          </div>
          {error ? (
            <p role="alert" className="text-xs font-medium text-destructive">
              {error}
            </p>
          ) : null}
        </div>
      )
    }
    case 'number': {
      const step = (dir: 1 | -1) => onChange(String(Math.round(((Number(text) || 0) + dir) * 1e10) / 1e10))
      return (
        <FieldShell def={def} id={id} error={error}>
          <div className="flex items-center gap-1.5">
            <Button type="button" variant="outline" size="icon" aria-label={t('eventTypes.decrease', { name: def.label })} onClick={() => step(-1)}>
              <MinusIcon aria-hidden />
            </Button>
            <Input
              {...common}
              type="number"
              inputMode="decimal"
              step="any"
              autoComplete="off"
              className="text-center tabular-nums"
              value={text}
              onChange={(e) => onChange(e.target.value)}
            />
            <Button type="button" variant="outline" size="icon" aria-label={t('eventTypes.increase', { name: def.label })} onClick={() => step(1)}>
              <PlusIcon aria-hidden />
            </Button>
          </div>
        </FieldShell>
      )
    }
    case 'date':
      return (
        <FieldShell def={def} id={id} error={error}>
          <div className="flex items-center gap-1.5">
            <Input {...common} type="date" value={text} onChange={(e) => onChange(e.target.value)} />
            <Button type="button" variant="outline" size="sm" onClick={() => onChange(new Date().toISOString().slice(0, 10))}>
              {t('calendar.today')}
            </Button>
          </div>
        </FieldShell>
      )
    case 'time':
      return (
        <FieldShell def={def} id={id} error={error}>
          <div className="flex flex-wrap items-center gap-2">
            <Input {...common} type="time" className="w-40 tabular-nums" value={text} onChange={(e) => onChange(e.target.value)} />
            {timeZone ? <span className="text-xs text-muted-foreground">{t('eventTypes.timeIn', { zone: timeZone.replace(/_/g, ' ') })}</span> : null}
          </div>
        </FieldShell>
      )
    case 'url': {
      const link = safeUrl(text)
      return (
        <FieldShell def={def} id={id} error={error}>
          <div className="flex items-center gap-1.5">
            <Input {...common} type="url" autoComplete="off" placeholder="https://" value={text} onChange={(e) => onChange(e.target.value)} />
            {link ? (
              <Button asChild variant="outline" size="icon" aria-label={t('eventTypes.openLink', { name: def.label })}>
                <a href={link.href} target="_blank" rel="noopener noreferrer nofollow">
                  <ExternalLinkIcon aria-hidden />
                </a>
              </Button>
            ) : null}
          </div>
        </FieldShell>
      )
    }
    case 'longtext':
      return (
        <FieldShell def={def} id={id} error={error}>
          <Textarea {...common} rows={4} maxLength={5000} value={text} onChange={(e) => onChange(e.target.value)} />
          <p className="text-end text-xs text-muted-foreground tabular-nums" aria-hidden>
            {text.length}/5000
          </p>
        </FieldShell>
      )
    default:
      return (
        <FieldShell def={def} id={id} error={error}>
          <Input {...common} type="text" maxLength={200} autoComplete="off" value={text} onChange={(e) => onChange(e.target.value)} />
        </FieldShell>
      )
  }
}

/** Toggle chips for the calendar's people, like the tag picker. */
function MembersPicker({
  value,
  onChange,
  labelledBy,
  multiple,
}: {
  value: string[]
  onChange: (ids: string[]) => void
  labelledBy: string
  multiple: boolean
}) {
  const { t } = useTranslation()
  const { data } = useDirectory()
  const users = data?.users ?? []
  if (!users.length) return <p className="text-sm text-muted-foreground">{t('eventTypes.noPeople')}</p>
  return (
    <div role="group" aria-labelledby={labelledBy} className="flex flex-wrap gap-1.5">
      {users.map((u) => {
        const on = value.includes(u.id)
        return (
          <button
            key={u.id}
            type="button"
            aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== u.id) : multiple ? [...value, u.id] : [u.id])}
            className={cn(
              'inline-flex items-center gap-1.5 rounded-full border py-0.5 ps-0.5 pe-2.5 text-xs font-medium transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
              on ? 'border-transparent bg-primary text-primary-foreground' : 'border-border hover:bg-accent',
            )}
          >
            <UserAvatar name={u.name} className="size-5" />
            {u.name}
          </button>
        )
      })}
    </div>
  )
}
