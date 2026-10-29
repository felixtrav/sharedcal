import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { EyeIcon, LockIcon, PencilLineIcon, SparklesIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { fieldErrors, isApiError } from '@/api/client'
import { storeEvent } from '@/api/eventCache'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useDirectory, useEventTypes, useMe } from '@/api/queries'
import type { CalEvent, EditScope, EventType, EventWrite } from '@/api/types'
import { LazyMarkdown } from '@/components/LazyMarkdown'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { describedBy, Field } from '@/components/ui/field'
import { Input, NativeSelect, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { viewerTime } from '@/lib/fieldTime'
import { renderTitle } from '@/lib/titleTemplate'
import { addDays, type FormatOptions } from '@/lib/time'
import { clearOwnWrite, markOwnWrite } from '@/realtime/stores'
import { ConflictDialog, type Conflict } from './ConflictDialog'
import {
  changedFields,
  eventFormSchema,
  fieldsToPayload,
  formFromDraft,
  formFromEvent,
  formToPayload,
  missingRequired,
  needsSeriesScope,
  type Draft,
  type EventFormValues,
} from './formModel'
import { fixedTiming, fixedValuesFor, typeDefaultsFor } from './typeDefaults'
import { RecurrenceField } from './RecurrenceField'
import { describeRRule } from './recurrenceText'
import type { ScopeAsk } from './ScopeDialog'
import { TagPicker } from './TagPicker'
import { TimeZoneSelect } from './TimeZoneSelect'
import { TypeFieldsEditor } from './TypeFieldsEditor'
import { TypeInstructions } from './TypeInstructions'

type FormMode =
  | { kind: 'create'; draft: Draft }
  | { kind: 'edit'; event: CalEvent; occurrence: string | null }
  | { kind: 'suggest'; event: CalEvent; occurrence: string | null }

export function EventForm({
  mode: modeProp,
  fmt,
  onDone,
  onDirtyChange,
  scopePrompt,
}: {
  mode: FormMode
  fmt: FormatOptions
  onDone: (created?: CalEvent) => void
  onDirtyChange?: (dirty: boolean) => void
  scopePrompt?: ScopeAsk
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  // Edit against the version the form was opened on. Live updates re-render the
  // panel with newer data; following them would send the newer If-Match and
  // silently overwrite someone else's save instead of raising a conflict.
  const [mode] = useState(modeProp)
  const initial = useMemo(() => (mode.kind === 'create' ? formFromDraft(mode.draft) : formFromEvent(mode.event)), [mode])
  const [conflict, setConflict] = useState<Conflict | null>(null)
  const [conflictScope, setConflictScope] = useState<EditScope | undefined>(undefined)
  const [preview, setPreview] = useState(false)
  const [busy, setBusy] = useState(false)

  const form = useForm<EventFormValues>({ resolver: zodResolver(eventFormSchema), defaultValues: initial, mode: 'onTouched' })
  const { register, control, handleSubmit, setValue, setError, formState } = form
  const { errors, isDirty } = formState
  const { data: typesData } = useEventTypes()
  const types = useMemo(() => typesData?.eventTypes ?? [], [typesData])
  const { data: me } = useMe()
  const isAdmin = me?.user.role === 'admin'
  const typeById = (id: string): EventType | undefined => types.find((et) => et.id === id)

  useEffect(() => {
    onDirtyChange?.(isDirty)
  }, [isDirty, onDirtyChange])

  const allDay = useWatch({ control, name: 'allDay' })
  const startDate = useWatch({ control, name: 'startDate' })
  const endDate = useWatch({ control, name: 'endDate' })
  const description = useWatch({ control, name: 'description' })
  const typeId = useWatch({ control, name: 'eventTypeId' })
  const timeZone = useWatch({ control, name: 'timeZone' })
  const eventType = typeById(typeId)
  const fieldValues = useWatch({ control, name: 'fields' })
  const { data: directory } = useDirectory()
  // Types with a title template generate the title from their fields, so it is shown but not typed.
  const autoTitle = eventType?.titleTemplate
    ? renderTitle(
        eventType,
        fieldsToPayload(fieldValues ?? {}, eventType),
        (id) => directory?.users.find((u) => u.id === id)?.name,
        (hhmm) => viewerTime(hhmm, { date: startDate, eventTz: timeZone, fmt }),
      )
    : null
  useEffect(() => {
    if (autoTitle !== null) setValue('title', autoTitle)
  }, [autoTitle, setValue])

  // A type brings its own look and, for new events, its defaults. Field values never carry over to another type.
  const chooseType = (id: string) => {
    const next = typeById(id)
    setValue('eventTypeId', id, { shouldDirty: true })
    setValue('fields', {}, { shouldDirty: true })
    if (next && mode.kind === 'create') {
      for (const [k, v] of Object.entries(typeDefaultsFor(form.getValues(), next))) {
        setValue(k as keyof EventFormValues, v as never, { shouldDirty: true })
      }
    }
  }

  // A type can fix all-day, the repeat rule and, with both, a single date. The controls are locked below;
  // this brings the values in line when the type is chosen or an older event doesn't conform yet.
  const rruleValue = useWatch({ control, name: 'rrule' })
  const fixed = fixedTiming(eventType)
  // A time field is a time in the event's zone, so the zone must be visible even for all-day events.
  const hasTimeField = !!eventType?.fields.some((fd) => fd.kind === 'time')
  useEffect(() => {
    if (!eventType) return
    const current = form.getValues()
    for (const [k, v] of Object.entries(fixedValuesFor(current, eventType))) {
      if (current[k as keyof EventFormValues] !== v) setValue(k as keyof EventFormValues, v as never, { shouldDirty: true })
    }
  }, [eventType, startDate, allDay, rruleValue, form, setValue])

  // Keep end ≥ start when the start date moves past it.
  useEffect(() => {
    if (startDate && endDate && endDate < startDate) setValue('endDate', startDate, { shouldDirty: true })
  }, [startDate, endDate, setValue])

  const msg = (m?: string) => (m ? t(m) : undefined)

  const applyServerErrors = (err: unknown) => {
    const fe = fieldErrors(err)
    const map: Record<string, keyof EventFormValues> = { start: 'startDate', end: 'endDate' }
    for (const [k, v] of Object.entries(fe)) {
      if (k.startsWith('fields.')) {
        setError(k as `fields.${string}`, { type: 'server', message: v })
        continue
      }
      const name = (map[k] ?? k) as keyof EventFormValues
      if (name in initial) setError(name, { type: 'server', message: v })
    }
  }

  const saveEdit = async (event: CalEvent, body: Partial<EventWrite>, version: number, scope?: EditScope, occurrence?: string | null) => {
    markOwnWrite(event.id)
    try {
      const updated = await endpoints.updateEvent(event.id, version, body, scope ? { scope, occurrence } : undefined)
      markOwnWrite(updated.id, updated.version)
      storeEvent(qc, updated)
      if (scope) void qc.invalidateQueries({ queryKey: keys.eventsAll })
      toast.success(t('event.saved'))
      setConflict(null)
      onDone()
    } catch (err) {
      if (isApiError(err) && err.status === 409 && err.problem.current) {
        setConflictScope(scope)
        setConflict({ mine: body, current: err.problem.current })
      } else {
        applyServerErrors(err)
        toast.error(t('event.saveFailed'), errorMessage(err, t))
      }
    } finally {
      clearOwnWrite(event.id)
    }
  }

  const onSubmit = async (values: EventFormValues) => {
    const missing = missingRequired(values.fields, typeById(values.eventTypeId))
    if (missing.length) {
      for (const d of missing) setError(`fields.${d.key}`, { type: 'required', message: t('validation.fieldRequired', { name: d.label }) })
      return
    }
    const payload = formToPayload(values, typeById(values.eventTypeId))
    const original = formToPayload(initial, typeById(initial.eventTypeId))
    setBusy(true)
    try {
      if (mode.kind === 'create') {
        const created = await endpoints.createEvent(payload)
        markOwnWrite(created.id, created.version)
        storeEvent(qc, created)
        toast.success(t('calendar.created', { title: created.title }))
        onDone(created)
        return
      }
      const diff = changedFields(original, payload, mode.kind === 'suggest')
      if (Object.keys(diff).length === 0) {
        onDone()
        return
      }
      const ev = mode.event
      if (mode.kind === 'suggest') {
        await endpoints.suggestEdit(ev.id, ev.version, diff)
        void qc.invalidateQueries({ queryKey: keys.suggestionsAll(ev.id) })
        toast.success(t('suggestions.sent'), t('suggestions.sentBody', { name: ev.owner.name }))
        onDone()
        return
      }
      let scope: EditScope | undefined
      if ((ev.rrule || mode.occurrence) && scopePrompt) {
        const s = await scopePrompt('edit', { disableThis: needsSeriesScope(diff) })
        if (!s) return
        scope = s
      }
      await saveEdit(ev, diff, ev.version, scope, mode.occurrence)
    } catch (err) {
      applyServerErrors(err)
      toast.error(mode.kind === 'suggest' ? t('suggestions.sendFailed') : t('event.saveFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const submitLabel = mode.kind === 'create' ? t('common.create') : mode.kind === 'suggest' ? t('suggestions.suggestChanges') : t('common.save')

  return (
    <form onSubmit={handleSubmit(onSubmit)} noValidate className="grid gap-5 pt-1" aria-label={submitLabel}>
      {mode.kind === 'suggest' ? (
        <p className="rounded-lg border border-border bg-muted px-3 py-2 text-sm text-muted-foreground">
          {t('suggestions.formHint', { name: mode.event.owner.name })}
        </p>
      ) : null}

      <Field
        id="ev-title"
        label={t('event.fields.title')}
        error={autoTitle === null ? msg(errors.title?.message) : undefined}
        hint={autoTitle !== null ? t('eventTypes.titleAuto', { name: eventType?.name }) : undefined}
      >
        {autoTitle !== null ? (
          <div className="relative">
            <SparklesIcon className="pointer-events-none absolute start-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
            <Input id="ev-title" readOnly value={autoTitle} aria-describedby="ev-title-hint" className="bg-muted/50 ps-9 font-medium" />
          </div>
        ) : (
          <Input
            id="ev-title"
            autoComplete="off"
            maxLength={200}
            placeholder={t('event.titlePlaceholder')}
            aria-invalid={!!errors.title}
            aria-describedby={describedBy('ev-title', errors.title?.message)}
            {...register('title')}
          />
        )}
      </Field>

      {types.length || typeId ? (
        <Field id="ev-type" label={t('event.fields.eventTypeId')}>
          <NativeSelect id="ev-type" value={typeId} onChange={(e) => chooseType(e.target.value)}>
            <option value="">{t('eventTypes.generic')}</option>
            {types
              .filter((et) => !et.adminOnly || isAdmin || et.id === typeId)
              .map((et) => (
                <option key={et.id} value={et.id}>
                  {et.name}
                </option>
              ))}
          </NativeSelect>
        </Field>
      ) : null}
      {eventType && mode.kind === 'create' ? <TypeInstructions type={eventType} /> : null}

      {fixed.allDay ? (
        <div className="flex items-center justify-between gap-3">
          <span className="text-sm font-medium">{t('event.fields.allDay')}</span>
          <span className="inline-flex items-center gap-1.5 text-sm text-muted-foreground">
            <LockIcon className="size-3.5" aria-hidden />
            {t('eventTypes.lockedAllDay')}
          </span>
        </div>
      ) : (
        <div className="flex items-center justify-between gap-3">
          <Label htmlFor="ev-allday">{t('event.fields.allDay')}</Label>
          <Controller
            control={control}
            name="allDay"
            render={({ field }) => (
              <Switch
                id="ev-allday"
                checked={field.value}
                onCheckedChange={(v) => {
                  field.onChange(v)
                  if (!v && endDate === startDate) setValue('endTime', addHour(form.getValues('startTime')), { shouldDirty: true })
                }}
              />
            )}
          />
        </div>
      )}

      <fieldset className="grid gap-3">
        <legend className="sr-only">{t('event.fields.when')}</legend>
        <div className="grid grid-cols-[1fr_auto] gap-2">
          <Field id="ev-start-date" label={fixed.singleDay ? t('eventTypes.date') : t('event.fields.start')} error={msg(errors.startDate?.message)}>
            <Input id="ev-start-date" type="date" aria-invalid={!!errors.startDate} {...register('startDate')} />
          </Field>
          {!allDay ? (
            <Field id="ev-start-time" label={t('event.fields.startTime')} labelHidden error={msg(errors.startTime?.message)} className="self-end">
              <Input id="ev-start-time" type="time" step={300} className="w-32 tabular-nums" aria-invalid={!!errors.startTime} {...register('startTime')} />
            </Field>
          ) : null}
        </div>
        {fixed.singleDay ? null : (
          <div className="grid grid-cols-[1fr_auto] gap-2">
            <Field id="ev-end-date" label={allDay ? t('event.fields.endDateInclusive') : t('event.fields.end')} error={msg(errors.endDate?.message)}>
              <Input id="ev-end-date" type="date" min={startDate} aria-invalid={!!errors.endDate} {...register('endDate')} />
            </Field>
            {!allDay ? (
              <Field id="ev-end-time" label={t('event.fields.endTime')} labelHidden error={msg(errors.endTime?.message)} className="self-end">
                <Input id="ev-end-time" type="time" step={300} className="w-32 tabular-nums" aria-invalid={!!errors.endTime} {...register('endTime')} />
              </Field>
            ) : null}
          </div>
        )}
        {!allDay || hasTimeField ? (
          <Field id="ev-tz" label={t('event.fields.timeZone')} error={msg(errors.timeZone?.message)}>
            <Controller
              control={control}
              name="timeZone"
              render={({ field }) => <TimeZoneSelect id="ev-tz" value={field.value} onChange={field.onChange} aria-invalid={!!errors.timeZone} />}
            />
          </Field>
        ) : null}
      </fieldset>

      {fixed.noRepeat ? (
        <div className="grid gap-1.5">
          <span className="text-sm font-medium">{t('event.fields.recurrence')}</span>
          <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
            <LockIcon className="size-3.5 shrink-0" aria-hidden />
            {t('eventTypes.lockedNoRepeat')}
          </p>
        </div>
      ) : fixed.rrule ? (
        <div className="grid gap-1.5">
          <span className="text-sm font-medium">{t('event.fields.recurrence')}</span>
          <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
            <LockIcon className="size-3.5 shrink-0" aria-hidden />
            {describeRRule(fixed.rrule, startDate || addDays(new Date().toISOString().slice(0, 10), 0), t)}
          </p>
        </div>
      ) : (
        <Field id="ev-rrule" label={t('event.fields.recurrence')}>
          <Controller
            control={control}
            name="rrule"
            render={({ field }) => (
              <RecurrenceField
                key={typeId}
                id="ev-rrule"
                value={field.value}
                onChange={field.onChange}
                startDate={startDate || addDays(new Date().toISOString().slice(0, 10), 0)}
                allDay={allDay}
              />
            )}
          />
        </Field>
      )}

      <Field id="ev-location" label={t('event.fields.location')} error={msg(errors.location?.message)} hint={t('event.locationHint')}>
        <Input
          id="ev-location"
          maxLength={1000}
          autoComplete="off"
          aria-describedby={describedBy('ev-location', errors.location?.message, true)}
          {...register('location')}
        />
      </Field>

      {eventType ? (
        <TypeFieldsEditor type={eventType} control={control} errors={errors} timeZone={timeZone} />
      ) : (
        <div className="grid gap-1.5">
          <div className="flex items-center justify-between">
            <Label htmlFor="ev-description">{t('event.fields.description')}</Label>
            <Button type="button" variant="ghost" size="xs" onClick={() => setPreview((p) => !p)} aria-pressed={preview}>
              {preview ? <PencilLineIcon aria-hidden /> : <EyeIcon aria-hidden />}
              {preview ? t('common.write') : t('common.preview')}
            </Button>
          </div>
          {preview ? (
            <div className="min-h-24 rounded-md border border-border p-3 text-sm">
              <LazyMarkdown source={description || t('common.nothingToPreview')} />
            </div>
          ) : (
            <Textarea
              id="ev-description"
              rows={5}
              maxLength={20000}
              placeholder={t('event.descriptionPlaceholder')}
              aria-invalid={!!errors.description}
              aria-describedby="ev-description-hint"
              {...register('description')}
            />
          )}
          <p id="ev-description-hint" className="text-xs text-muted-foreground">
            {errors.description ? <span className="text-destructive">{msg(errors.description.message)}</span> : t('event.markdownHint')}
          </p>
        </div>
      )}

      <div className="grid gap-1.5">
        <span id="ev-tags-label" className="text-sm font-medium">
          {t('event.fields.tags')}
        </span>
        <Controller
          control={control}
          name="tags"
          render={({ field }) => <TagPicker value={field.value} onChange={field.onChange} labelledBy="ev-tags-label" />}
        />
      </div>

      <div className="grid gap-1.5">
        <span className="text-sm font-medium" id="ev-status-label">
          {t('event.fields.status')}
        </span>
        <Controller
          control={control}
          name="status"
          render={({ field }) => (
            <Segmented
              value={field.value}
              onChange={field.onChange}
              label={t('event.fields.status')}
              options={[
                { value: 'confirmed', label: t('event.status.confirmed') },
                { value: 'tentative', label: t('event.status.tentative') },
              ]}
            />
          )}
        />
      </div>

      {mode.kind !== 'create' ? (
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          <UserAvatar name={mode.event.owner.name} src={mode.event.owner.avatarUrl} className="size-5" />
          {t('event.ownedBy', { name: mode.event.owner.name })}
        </p>
      ) : null}

      <div className="sticky bottom-0 -mx-5 flex justify-end gap-2 border-t border-border bg-card px-5 py-3">
        <Button type="button" variant="outline" onClick={() => onDone()}>
          {t('common.cancel')}
        </Button>
        <Button type="submit" disabled={busy || formState.isSubmitting}>
          {submitLabel}
        </Button>
      </div>

      {mode.kind === 'edit' ? (
        <ConflictDialog
          key={conflict?.current.version ?? 0}
          conflict={conflict}
          fmt={fmt}
          pending={busy}
          onDiscard={() => {
            if (conflict) storeEvent(qc, conflict.current)
            setConflict(null)
            onDone()
          }}
          onApply={(fields) => {
            if (!conflict) return
            if (Object.keys(fields).length === 0) {
              storeEvent(qc, conflict.current)
              setConflict(null)
              onDone()
              return
            }
            setBusy(true)
            void saveEdit(mode.event, fields, conflict.current.version, conflictScope, mode.occurrence).finally(() => setBusy(false))
          }}
        />
      ) : null}
    </form>
  )
}

function addHour(time: string) {
  const [h, m] = time.split(':').map(Number)
  const hh = Math.min(23, (h ?? 9) + 1)
  return `${String(hh).padStart(2, '0')}:${String(m ?? 0).padStart(2, '0')}`
}
