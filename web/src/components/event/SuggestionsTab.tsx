import { useQueryClient } from '@tanstack/react-query'
import { AlertTriangleIcon, CheckIcon, MessageSquareDiffIcon, Trash2Icon, Undo2Icon, XIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { isApiError } from '@/api/client'
import { removeEventFromCache, storeEvent } from '@/api/eventCache'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useSuggestions } from '@/api/queries'
import { SUGGESTION_FIELDS, type CalEvent, type Suggestion, type SuggestionField } from '@/api/types'
import { UserAvatar } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Textarea } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatDateTime, formatRelative, type FormatOptions } from '@/lib/time'
import { cn } from '@/lib/utils'
import { fieldLabel, FieldValue } from './FieldValue'

export function SuggestionsTab({ event, fmt, onEventDeleted }: { event: CalEvent; fmt: FormatOptions; onEventDeleted: () => void }) {
  const { t } = useTranslation()
  const [showAll, setShowAll] = useState(false)
  const { data, isPending, isError, refetch } = useSuggestions(event.id, showAll ? 'all' : 'open')
  const [reviewing, setReviewing] = useState<string | null>(null)
  const list = data?.suggestions ?? []

  return (
    <div className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-sm text-muted-foreground">{showAll ? t('suggestions.allHeading') : t('suggestions.openHeading')}</p>
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={showAll} onCheckedChange={setShowAll} aria-label={t('suggestions.showResolved')} />
          <span aria-hidden>{t('suggestions.showResolved')}</span>
        </label>
      </div>

      {isPending ? (
        <Skeleton className="h-24 w-full" />
      ) : isError ? (
        <div role="alert" className="text-sm">
          {t('suggestions.loadFailed')}{' '}
          <Button variant="link" size="xs" onClick={() => void refetch()}>
            {t('common.retry')}
          </Button>
        </div>
      ) : list.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-10 text-center text-sm text-muted-foreground">
          <MessageSquareDiffIcon className="size-6" aria-hidden />
          <p>{t('suggestions.empty')}</p>
          {event.permissions.suggest && !event.permissions.edit ? <p>{t('suggestions.emptyHint')}</p> : null}
        </div>
      ) : (
        <ul className="grid gap-3">
          {list.map((s) => (
            <li key={s.id}>
              <SuggestionCard
                suggestion={s}
                event={event}
                fmt={fmt}
                expanded={reviewing === s.id}
                onToggle={() => setReviewing((r) => (r === s.id ? null : s.id))}
                onEventDeleted={onEventDeleted}
              />
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

const STATUS_VARIANT: Record<Suggestion['status'], 'default' | 'secondary' | 'outline' | 'destructive'> = {
  open: 'default',
  accepted: 'secondary',
  partially_accepted: 'secondary',
  rejected: 'outline',
  withdrawn: 'outline',
  closed: 'outline',
}

function SuggestionCard({
  suggestion: s,
  event,
  fmt,
  expanded,
  onToggle,
  onEventDeleted,
}: {
  suggestion: Suggestion
  event: CalEvent
  fmt: FormatOptions
  expanded: boolean
  onToggle: () => void
  onEventDeleted: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const fields = SUGGESTION_FIELDS.filter((f) => s.changes[f])
  const conflicts = fields.filter((f) => s.changes[f]?.conflict)
  const isOpen = s.status === 'open'

  const withdraw = async () => {
    try {
      await endpoints.withdrawSuggestion(s.id)
      void qc.invalidateQueries({ queryKey: keys.suggestionsAll(s.eventId) })
      void qc.invalidateQueries({ queryKey: keys.event(s.eventId) })
      toast({ title: t('suggestions.withdrawn') })
    } catch (err) {
      toast.error(t('suggestions.actionFailed'), errorMessage(err, t))
    }
  }

  return (
    <article className={cn('rounded-xl border border-border bg-background p-4', expanded && 'ring-2 ring-ring/40')} aria-labelledby={`sg-${s.id}`}>
      <header className="flex items-start gap-3">
        <UserAvatar name={s.author.name} src={s.author.avatarUrl} className="size-8" />
        <div className="min-w-0 flex-1">
          <p id={`sg-${s.id}`} className="text-sm">
            <span className="font-semibold">{s.author.name}</span>{' '}
            {s.kind === 'delete' ? t('suggestions.proposesDelete') : t('suggestions.proposesEdit', { count: fields.length })}
          </p>
          <p className="text-xs text-muted-foreground">
            <time dateTime={s.createdAt} title={formatDateTime(Date.parse(s.createdAt), fmt)}>
              {formatRelative(s.createdAt)}
            </time>
          </p>
        </div>
        <Badge variant={STATUS_VARIANT[s.status]}>{t(`suggestions.status.${s.status}`)}</Badge>
      </header>

      {s.kind === 'edit' ? (
        <p className="mt-3 flex flex-wrap gap-1.5">
          {fields.map((f) => (
            <span key={f} className={cn('rounded-md bg-muted px-2 py-0.5 text-xs', s.changes[f]?.conflict && isOpen && 'bg-destructive/10 text-destructive')}>
              {fieldLabel(f, t)}
              {s.changes[f]?.conflict && isOpen ? <span className="sr-only"> ({t('suggestions.conflict')})</span> : null}
            </span>
          ))}
        </p>
      ) : null}

      {conflicts.length && isOpen ? (
        <p className="mt-3 flex items-center gap-2 text-xs text-destructive">
          <AlertTriangleIcon className="size-3.5" aria-hidden />
          {t('suggestions.conflictsNote', { count: conflicts.length })}
        </p>
      ) : null}

      {s.reviewNote ? (
        <p className="mt-3 rounded-md bg-muted px-3 py-2 text-sm">
          <span className="font-medium">{s.reviewer?.name ?? t('suggestions.reviewer')}:</span> {s.reviewNote}
        </p>
      ) : null}

      {isOpen ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {s.permissions.review ? (
            <Button size="xs" onClick={onToggle} aria-expanded={expanded}>
              {expanded ? t('suggestions.hideReview') : t('suggestions.review')}
            </Button>
          ) : (
            <Button size="xs" variant="outline" onClick={onToggle} aria-expanded={expanded}>
              {expanded ? t('suggestions.hideDetails') : t('suggestions.viewDetails')}
            </Button>
          )}
          {s.permissions.withdraw ? (
            <Button size="xs" variant="ghost" onClick={() => void withdraw()}>
              <Undo2Icon aria-hidden />
              {t('suggestions.withdraw')}
            </Button>
          ) : null}
        </div>
      ) : (
        <div className="mt-3">
          <Button size="xs" variant="ghost" onClick={onToggle} aria-expanded={expanded}>
            {expanded ? t('suggestions.hideDetails') : t('suggestions.viewDetails')}
          </Button>
        </div>
      )}

      {expanded ? <SuggestionReview suggestion={s} event={event} fmt={fmt} fields={fields} onEventDeleted={onEventDeleted} /> : null}
    </article>
  )
}

/** Field-by-field diff (current vs proposed) with selection, conflict pickers, accept/reject. */
function SuggestionReview({
  suggestion: s,
  event,
  fmt,
  fields,
  onEventDeleted,
}: {
  suggestion: Suggestion
  event: CalEvent
  fmt: FormatOptions
  fields: SuggestionField[]
  onEventDeleted: () => void
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const canReview = s.permissions.review && s.status === 'open'
  const [selected, setSelected] = useState<Set<SuggestionField>>(() => new Set(fields))
  const [resolutions, setResolutions] = useState<Partial<Record<SuggestionField, 'current' | 'proposed'>>>({})
  const [rejecting, setRejecting] = useState(false)
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  const [missing, setMissing] = useState<SuggestionField[]>([])

  const after = (ok: string) => {
    void qc.invalidateQueries({ queryKey: keys.suggestionsAll(s.eventId) })
    void qc.invalidateQueries({ queryKey: keys.history(s.eventId) })
    toast.success(ok)
  }

  const accept = async (only?: SuggestionField[]) => {
    const chosen = only ?? fields
    const unresolved = chosen.filter((f) => s.changes[f]?.conflict && !resolutions[f])
    if (unresolved.length) {
      setMissing(unresolved)
      return
    }
    setBusy(true)
    try {
      const res = await endpoints.acceptSuggestion(s.id, {
        fields: only && only.length !== fields.length ? only : [],
        resolutions: Object.fromEntries(chosen.filter((f) => resolutions[f]).map((f) => [f, resolutions[f]])),
      })
      if (res.event) storeEvent(qc, res.event)
      else {
        removeEventFromCache(qc, s.eventId)
        onEventDeleted()
      }
      after(s.kind === 'delete' ? t('suggestions.acceptedDelete') : t('suggestions.accepted'))
    } catch (err) {
      if (isApiError(err) && err.status === 409) void qc.invalidateQueries({ queryKey: keys.suggestionsAll(s.eventId) })
      toast.error(t('suggestions.actionFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const reject = async () => {
    setBusy(true)
    try {
      await endpoints.rejectSuggestion(s.id, note.trim() || undefined)
      after(t('suggestions.rejected'))
    } catch (err) {
      toast.error(t('suggestions.actionFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-4 grid gap-4 border-t border-border pt-4">
      {s.kind === 'delete' ? (
        <p className="flex items-center gap-2 text-sm">
          <Trash2Icon className="size-4 text-destructive" aria-hidden />
          {t('suggestions.deleteExplain', { title: event.title })}
        </p>
      ) : (
        <ul className="grid gap-3">
          {fields.map((f) => {
            const ch = s.changes[f]
            if (!ch) return null
            const isConflict = ch.conflict && s.status === 'open'
            return (
              <li
                key={f}
                className={cn(
                  'rounded-lg border border-border p-3',
                  isConflict && 'border-destructive/50',
                  missing.includes(f) && 'ring-2 ring-destructive/50',
                )}
              >
                <div className="flex items-center gap-2">
                  {canReview ? (
                    <Checkbox
                      id={`sel-${s.id}-${f}`}
                      checked={selected.has(f)}
                      onCheckedChange={(v) =>
                        setSelected((prev) => {
                          const next = new Set(prev)
                          if (v) next.add(f)
                          else next.delete(f)
                          return next
                        })
                      }
                    />
                  ) : null}
                  <label htmlFor={`sel-${s.id}-${f}`} className="text-sm font-semibold">
                    {fieldLabel(f, t)}
                  </label>
                  {isConflict ? (
                    <Badge variant="destructive" className="ms-auto">
                      <AlertTriangleIcon aria-hidden />
                      {t('suggestions.conflict')}
                    </Badge>
                  ) : null}
                </div>
                <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-sm">
                  <dt className="text-muted-foreground">{t('suggestions.current')}</dt>
                  <dd className="min-w-0">
                    <FieldValue field={f} value={ch.current} fmt={fmt} startDate={event.start} />
                  </dd>
                  {isConflict ? (
                    <>
                      <dt className="text-muted-foreground">{t('suggestions.base')}</dt>
                      <dd className="min-w-0 text-muted-foreground">
                        <FieldValue field={f} value={ch.base} fmt={fmt} startDate={event.start} />
                      </dd>
                    </>
                  ) : null}
                  <dt className="font-medium text-primary">{t('suggestions.proposed')}</dt>
                  <dd className="min-w-0 font-medium">
                    <FieldValue field={f} value={ch.proposed} fmt={fmt} startDate={event.start} />
                  </dd>
                </dl>
                {isConflict && canReview ? (
                  <fieldset className="mt-3 flex flex-wrap gap-3 text-sm">
                    <legend className="mb-1 w-full text-xs text-muted-foreground">{t('suggestions.whichWins')}</legend>
                    {(['current', 'proposed'] as const).map((choice) => (
                      <label key={choice} className="flex items-center gap-2">
                        <input
                          type="radio"
                          name={`res-${s.id}-${f}`}
                          checked={resolutions[f] === choice}
                          onChange={() => {
                            setResolutions((r) => ({ ...r, [f]: choice }))
                            setMissing((m) => m.filter((x) => x !== f))
                          }}
                          className="accent-(--primary)"
                        />
                        {choice === 'current' ? t('suggestions.keepCurrent') : t('suggestions.useProposed')}
                      </label>
                    ))}
                  </fieldset>
                ) : null}
              </li>
            )
          })}
        </ul>
      )}

      {missing.length ? (
        <p role="alert" className="text-sm text-destructive">
          {t('suggestions.resolveConflicts')}
        </p>
      ) : null}

      {canReview ? (
        rejecting ? (
          <div className="grid gap-2">
            <label htmlFor={`note-${s.id}`} className="text-sm font-medium">
              {t('suggestions.rejectReason')}
            </label>
            <Textarea id={`note-${s.id}`} value={note} onChange={(e) => setNote(e.target.value)} rows={2} maxLength={1000} />
            <div className="flex gap-2">
              <Button size="sm" variant="destructive" disabled={busy} onClick={() => void reject()}>
                {t('suggestions.reject')}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setRejecting(false)}>
                {t('common.cancel')}
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-wrap gap-2">
            <Button size="sm" disabled={busy} onClick={() => void accept()}>
              <CheckIcon aria-hidden />
              {s.kind === 'delete' ? t('suggestions.acceptDelete') : t('suggestions.acceptAll')}
            </Button>
            {s.kind === 'edit' && fields.length > 1 ? (
              <Button
                size="sm"
                variant="outline"
                disabled={busy || selected.size === 0 || selected.size === fields.length}
                onClick={() => void accept(fields.filter((f) => selected.has(f)))}
              >
                {t('suggestions.acceptSelected', { count: selected.size })}
              </Button>
            ) : null}
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => setRejecting(true)}>
              <XIcon aria-hidden />
              {t('suggestions.reject')}
            </Button>
          </div>
        )
      ) : null}
    </div>
  )
}
