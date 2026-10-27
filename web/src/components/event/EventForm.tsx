import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { EyeIcon, PencilLineIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { Controller, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { fieldErrors, isApiError } from '@/api/client'
import { storeEvent } from '@/api/eventCache'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import type { CalEvent, EditScope, EventWrite } from '@/api/types'
import { LazyMarkdown } from '@/components/LazyMarkdown'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { describedBy, Field } from '@/components/ui/field'
import { Input, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { addDays, type FormatOptions } from '@/lib/time'
import { clearOwnWrite, markOwnWrite } from '@/realtime/stores'
import { ConflictDialog, type Conflict } from './ConflictDialog'
import { changedFields, eventFormSchema, needsSeriesScope, formFromDraft, formFromEvent, formToPayload, type Draft, type EventFormValues } from './formModel'
import { RecurrenceField } from './RecurrenceField'
import type { ScopeAsk } from './ScopeDialog'
import { TagPicker } from './TagPicker'
import { TimeZoneSelect } from './TimeZoneSelect'

export type FormMode =
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
  const original = useMemo(() => formToPayload(initial), [initial])
  const [conflict, setConflict] = useState<Conflict | null>(null)
  const [conflictScope, setConflictScope] = useState<EditScope | undefined>(undefined)
  const [preview, setPreview] = useState(false)
  const [busy, setBusy] = useState(false)

  const form = useForm<EventFormValues>({ resolver: zodResolver(eventFormSchema), defaultValues: initial, mode: 'onTouched' })
  const { register, control, handleSubmit, setValue, setError, formState } = form
  const { errors, isDirty } = formState

  useEffect(() => {
    onDirtyChange?.(isDirty)
  }, [isDirty, onDirtyChange])

  const allDay = useWatch({ control, name: 'allDay' })
  const startDate = useWatch({ control, name: 'startDate' })
  const endDate = useWatch({ control, name: 'endDate' })
  const description = useWatch({ control, name: 'description' })

  // Keep end ≥ start when the start date moves past it.
  useEffect(() => {
    if (startDate && endDate && endDate < startDate) setValue('endDate', startDate, { shouldDirty: true })
  }, [startDate, endDate, setValue])

  const msg = (m?: string) => (m ? t(m) : undefined)

  const applyServerErrors = (err: unknown) => {
    const fe = fieldErrors(err)
    const map: Record<string, keyof EventFormValues> = { start: 'startDate', end: 'endDate' }
    for (const [k, v] of Object.entries(fe)) {
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
    const payload = formToPayload(values)
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

      <Field id="ev-title" label={t('event.fields.title')} error={msg(errors.title?.message)}>
        <Input
          id="ev-title"
          autoComplete="off"
          maxLength={200}
          placeholder={t('event.titlePlaceholder')}
          aria-invalid={!!errors.title}
          aria-describedby={describedBy('ev-title', errors.title?.message)}
          {...register('title')}
        />
      </Field>

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

      <fieldset className="grid gap-3">
        <legend className="sr-only">{t('event.fields.when')}</legend>
        <div className="grid grid-cols-[1fr_auto] gap-2">
          <Field id="ev-start-date" label={t('event.fields.start')} error={msg(errors.startDate?.message)}>
            <Input id="ev-start-date" type="date" aria-invalid={!!errors.startDate} {...register('startDate')} />
          </Field>
          {!allDay ? (
            <Field id="ev-start-time" label={t('event.fields.startTime')} labelHidden error={msg(errors.startTime?.message)} className="self-end">
              <Input id="ev-start-time" type="time" step={300} className="w-32 tabular-nums" aria-invalid={!!errors.startTime} {...register('startTime')} />
            </Field>
          ) : null}
        </div>
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
        {!allDay ? (
          <Field id="ev-tz" label={t('event.fields.timeZone')} error={msg(errors.timeZone?.message)}>
            <Controller
              control={control}
              name="timeZone"
              render={({ field }) => <TimeZoneSelect id="ev-tz" value={field.value} onChange={field.onChange} aria-invalid={!!errors.timeZone} />}
            />
          </Field>
        ) : null}
      </fieldset>

      <Field id="ev-rrule" label={t('event.fields.recurrence')}>
        <Controller
          control={control}
          name="rrule"
          render={({ field }) => (
            <RecurrenceField id="ev-rrule" value={field.value} onChange={field.onChange} startDate={startDate || addDays(new Date().toISOString().slice(0, 10), 0)} allDay={allDay} />
          )}
        />
      </Field>

      <Field id="ev-location" label={t('event.fields.location')} error={msg(errors.location?.message)} hint={t('event.locationHint')}>
        <Input id="ev-location" maxLength={1000} autoComplete="off" aria-describedby={describedBy('ev-location', errors.location?.message, true)} {...register('location')} />
      </Field>

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

      <div className="grid gap-1.5">
        <span id="ev-tags-label" className="text-sm font-medium">
          {t('event.fields.tags')}
        </span>
        <Controller control={control} name="tags" render={({ field }) => <TagPicker value={field.value} onChange={field.onChange} labelledBy="ev-tags-label" />} />
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
