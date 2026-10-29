import { useQueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { ArrowDownIcon, ArrowUpIcon, ChevronDownIcon, ChevronRightIcon, PlusIcon, ShapesIcon, Trash2Icon } from 'lucide-react'
import { useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useEventTypes } from '@/api/queries'
import {
  EVENT_TYPE_STYLES,
  FIELD_KINDS,
  TAG_SLOTS,
  TYPE_ICONS,
  type EventType,
  type EventTypeField,
  type EventTypeStyle,
  type EventTypeWrite,
  type FieldKind,
  type TagSlot,
  type TypeIcon,
} from '@/api/types'
import { allowsMany } from '@/components/event/fieldKinds'
import { SlotPicker } from '@/components/admin/SlotPicker'
import { EmptyState, LoadError, Page, PageHeader } from '@/components/layout/PageHeader'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input, NativeSelect, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { renderTitle, TITLE_FIELD_KINDS } from '@/lib/titleTemplate'
import { errorMessage } from '@/lib/errors'
import { TypeGlyph } from '@/lib/typeIcons'

type TypesData = { eventTypes: EventType[] }

/** Stands for "never repeats" in the repetition select; it is stored as `neverRepeats`, not as a rule. */
const NEVER = 'never'
const REPEATS = ['', NEVER, 'FREQ=DAILY', 'FREQ=WEEKLY', 'FREQ=MONTHLY', 'FREQ=YEARLY'] as const

type Preset = Pick<
  EventType,
  'name' | 'icon' | 'colorSlot' | 'style' | 'yearCounter' | 'defaultAllDay' | 'defaultDurationMinutes' | 'defaultRrule' | 'fields'
> &
  Partial<Pick<EventType, 'titleTemplate' | 'singleDay' | 'adminOnly' | 'ownerless'>>

/** Translations write placeholders as [key] because i18next reads {{key}} itself. */
const braced = (s: string) => s.replace(/\[([#/]?[a-z][a-z0-9_]*)\]/g, '{{$1}}')

/** Starting points an admin can add with one click and then adjust. */
function presets(t: TFunction): Record<string, Preset> {
  const f = (key: string, kind: FieldKind, required = false): EventTypeField => ({
    key,
    kind,
    ...(required ? { required } : {}),
    label: t(`eventTypes.presets.fields.${key}`),
  })
  return {
    birthday: {
      name: t('eventTypes.presets.birthday'),
      icon: 'cake',
      colorSlot: 'tag-3',
      style: 'banner',
      yearCounter: true,
      defaultAllDay: true,
      singleDay: true,
      defaultDurationMinutes: 60,
      defaultRrule: 'FREQ=YEARLY',
      titleTemplate: braced(t('eventTypes.presets.titles.birthday')),
      fields: [f('person', 'text', true), f('gift_ideas', 'longtext')],
    },
    anniversary: {
      name: t('eventTypes.presets.anniversary'),
      icon: 'heart',
      colorSlot: 'tag-1',
      style: 'banner',
      yearCounter: true,
      defaultAllDay: true,
      singleDay: true,
      defaultDurationMinutes: 60,
      defaultRrule: 'FREQ=YEARLY',
      titleTemplate: braced(t('eventTypes.presets.titles.anniversary')),
      fields: [f('names', 'text', true)],
    },
    holiday: {
      name: t('eventTypes.presets.holiday'),
      icon: 'party-popper',
      colorSlot: 'tag-5',
      style: 'banner',
      yearCounter: false,
      defaultAllDay: true,
      defaultDurationMinutes: 60,
      defaultRrule: '',
      // Public holidays belong to the calendar, and are usually imported by an admin.
      adminOnly: true,
      ownerless: true,
      fields: [f('region', 'text'), f('day_off', 'checkbox')],
    },
    deadline: {
      name: t('eventTypes.presets.deadline'),
      icon: 'flag',
      colorSlot: 'tag-6',
      style: 'outline',
      yearCounter: false,
      defaultAllDay: false,
      defaultDurationMinutes: 0,
      defaultRrule: '',
      fields: [f('project', 'text'), f('link', 'url')],
    },
    reminder: {
      name: t('eventTypes.presets.reminder'),
      icon: 'bell',
      colorSlot: 'tag-4',
      style: 'badge',
      yearCounter: false,
      defaultAllDay: false,
      defaultDurationMinutes: 0,
      defaultRrule: '',
      fields: [],
    },
  }
}

/** Key for a new field, derived from its label and unique within the type. */
function makeKey(label: string, taken: Set<string>) {
  const base =
    label
      .toLowerCase()
      .normalize('NFKD')
      .replace(/[^a-z0-9]+/g, '_')
      .replace(/^_+|_+$/g, '')
      .replace(/^[^a-z]+/, '')
      .slice(0, 28) || 'field'
  let key = base
  for (let n = 2; taken.has(key); n++) key = `${base}_${n}`
  taken.add(key)
  return key
}

export function EventTypesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isPending, isError, refetch } = useEventTypes()
  const types = data?.eventTypes ?? []
  const [name, setName] = useState('')
  const [slot, setSlot] = useState<TagSlot>('tag-1')
  const [newAdminOnly, setNewAdminOnly] = useState(false)
  const [newOwnerless, setNewOwnerless] = useState(false)
  const [deleting, setDeleting] = useState<EventType | null>(null)
  const [announce, setAnnounce] = useState('')
  // Editors start collapsed so a long list doesn't mean a long scroll; a new type opens ready to configure.
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set())
  const toggle = (id: string) =>
    setOpen((prev) => {
      const next = new Set(prev)
      if (!next.delete(id)) next.add(id)
      return next
    })

  const setTypes = (list: EventType[]) => qc.setQueryData<TypesData>(keys.eventTypes, { eventTypes: list })

  const add = async (body: Pick<EventType, 'name' | 'colorSlot'> & EventTypeWrite) => {
    try {
      const created = await endpoints.createEventType(body)
      setTypes([...types, created])
      setOpen((prev) => new Set(prev).add(created.id))
      toast.success(t('eventTypes.created', { name: created.name }))
      return true
    } catch (err) {
      toast.error(t('eventTypes.saveFailed'), errorMessage(err, t))
      return false
    }
  }

  const create = async () => {
    const n = name.trim()
    if (!n) return
    if (await add({ name: n, colorSlot: slot, adminOnly: newAdminOnly, ownerless: newOwnerless })) {
      setName('')
      setNewAdminOnly(false)
      setNewOwnerless(false)
      setSlot(TAG_SLOTS[(types.length + 1) % 8] ?? 'tag-1')
    }
  }

  const save = async (type: EventType, body: EventTypeWrite) => {
    try {
      const saved = await endpoints.updateEventType(type.id, body)
      qc.setQueryData<TypesData>(keys.eventTypes, (d) => (d ? { eventTypes: d.eventTypes.map((x) => (x.id === type.id ? saved : x)) } : d))
      void qc.invalidateQueries({ queryKey: keys.eventAll })
      toast.success(t('eventTypes.saved', { name: saved.name }))
      return true
    } catch (err) {
      toast.error(t('eventTypes.saveFailed'), errorMessage(err, t))
      return false
    }
  }

  const reorder = async (ids: string[]) => {
    const prev = types
    const byId = new Map(types.map((x) => [x.id, x]))
    setTypes(ids.map((id, i) => ({ ...(byId.get(id) as EventType), sortOrder: i })))
    try {
      setTypes((await endpoints.reorderEventTypes(ids)).eventTypes)
    } catch (err) {
      setTypes(prev)
      toast.error(t('eventTypes.saveFailed'), errorMessage(err, t))
    }
  }

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= types.length) return
    const ids = types.map((x) => x.id)
    const [id] = ids.splice(index, 1)
    if (!id) return
    ids.splice(target, 0, id)
    void reorder(ids)
    setAnnounce(t('eventTypes.moved', { name: types[index]?.name, position: target + 1, total: types.length }))
  }

  const remove = async (type: EventType) => {
    try {
      await endpoints.deleteEventType(type.id)
      setTypes(types.filter((x) => x.id !== type.id))
      toast({ title: t('eventTypes.deleted', { name: type.name }) })
    } catch (err) {
      toast.error(t('eventTypes.deleteFailed'), errorMessage(err, t))
    }
  }

  const presetList = Object.entries(presets(t))
  return (
    <Page>
      <PageHeader title={t('eventTypes.title')} description={t('eventTypes.description')} />
      <form
        className="mb-6 grid gap-3 rounded-xl border border-border bg-card p-4"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <h2 className="text-sm font-semibold">{t('eventTypes.new')}</h2>
        <div className="flex flex-wrap items-center gap-3">
          <label htmlFor="new-type" className="sr-only">
            {t('eventTypes.name')}
          </label>
          <Input
            id="new-type"
            value={name}
            maxLength={64}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('eventTypes.namePlaceholder')}
            className="w-56"
          />
          <SlotPicker value={slot} onChange={setSlot} label={t('tags.color')} name="new-type-slot" />
          <Button type="submit" size="sm" disabled={!name.trim()}>
            <PlusIcon aria-hidden />
            {t('eventTypes.add')}
          </Button>
        </div>
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
          <div className="flex items-center gap-2">
            <Switch id="new-type-admin-only" checked={newAdminOnly} onCheckedChange={setNewAdminOnly} />
            <Label htmlFor="new-type-admin-only">{t('eventTypes.adminOnly')}</Label>
          </div>
          <div className="flex items-center gap-2">
            <Switch id="new-type-ownerless" checked={newOwnerless} onCheckedChange={setNewOwnerless} aria-describedby="new-type-ownerless-hint" />
            <Label htmlFor="new-type-ownerless">{t('eventTypes.ownerless')}</Label>
          </div>
          <p id="new-type-ownerless-hint" className="basis-full text-xs text-muted-foreground">
            {t('eventTypes.ownerlessHint')}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
          <span className="text-xs text-muted-foreground">{t('eventTypes.startFrom')}</span>
          {presetList.map(([id, p]) => (
            <Button
              key={id}
              type="button"
              variant="outline"
              size="xs"
              disabled={types.some((x) => x.name.toLowerCase() === p.name.toLowerCase())}
              onClick={() => void add(p)}
            >
              {p.name}
            </Button>
          ))}
        </div>
      </form>

      <p className="sr-only" role="status" aria-live="polite">
        {announce}
      </p>

      {isPending ? (
        <Skeleton className="h-40" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : types.length === 0 ? (
        <EmptyState icon={<ShapesIcon />} title={t('eventTypes.empty')} body={t('eventTypes.emptyBody')} />
      ) : (
        <>
          {types.length > 1 ? (
            <div className="mb-2 flex justify-end">
              <Button type="button" variant="ghost" size="xs" onClick={() => setOpen(open.size === types.length ? new Set() : new Set(types.map((x) => x.id)))}>
                {open.size === types.length ? t('eventTypes.collapseAll') : t('eventTypes.expandAll')}
              </Button>
            </div>
          ) : null}
          <ol className="grid gap-4" aria-label={t('eventTypes.list')}>
            {types.map((type, i) => {
              const actions = (
                <div className="ms-auto flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    disabled={i === 0}
                    onClick={() => move(i, -1)}
                    aria-label={t('eventTypes.moveUp', { name: type.name })}
                  >
                    <ArrowUpIcon />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    disabled={i === types.length - 1}
                    onClick={() => move(i, 1)}
                    aria-label={t('eventTypes.moveDown', { name: type.name })}
                  >
                    <ArrowDownIcon />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    className="hover:text-destructive"
                    onClick={() => setDeleting(type)}
                    aria-label={t('eventTypes.delete', { name: type.name })}
                  >
                    <Trash2Icon />
                  </Button>
                </div>
              )
              return (
                <li key={type.id} className="rounded-xl border border-border bg-card">
                  {open.has(type.id) ? (
                    <TypeEditor
                      // Remount when the server copy changes (live update from another admin) so the draft starts fresh.
                      key={`${type.id}:${JSON.stringify({ ...type, sortOrder: 0 })}`}
                      type={type}
                      onSave={(body) => save(type, body)}
                      onCollapse={() => toggle(type.id)}
                      actions={actions}
                    />
                  ) : (
                    <TypeSummary type={type} onExpand={() => toggle(type.id)} actions={actions} />
                  )}
                </li>
              )
            })}
          </ol>
        </>
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t('eventTypes.deleteTitle', { name: deleting?.name })}
        description={t('eventTypes.deleteBody')}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={() => deleting && void remove(deleting)}
      />
    </Page>
  )
}

/** A collapsed type: its look and a one-line summary, with the same actions as the open editor. */
function TypeSummary({ type, onExpand, actions }: { type: EventType; onExpand: () => void; actions: React.ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center gap-3 p-3">
      <button
        type="button"
        aria-expanded={false}
        aria-label={t('eventTypes.expand', { name: type.name })}
        onClick={onExpand}
        className="flex min-w-0 flex-1 items-center gap-3 rounded-md text-start outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground rtl:rotate-180" aria-hidden />
        <span
          className="flex size-8 shrink-0 items-center justify-center rounded-lg"
          style={{ background: `var(--${type.colorSlot})`, color: `var(--${type.colorSlot}-foreground)` }}
          aria-hidden
        >
          <TypeGlyph name={type.icon} className="size-4" />
        </span>
        <span className="truncate font-medium">{type.name}</span>
        <span className="truncate text-xs text-muted-foreground">
          {t('eventTypes.fieldCount', { count: type.fields.length })}
          {type.titleTemplate ? ` · ${type.titleTemplate}` : ''}
        </span>
      </button>
      {actions}
    </div>
  )
}

function TypeEditor({
  type,
  onSave,
  onCollapse,
  actions,
}: {
  type: EventType
  onSave: (body: EventTypeWrite) => Promise<boolean>
  onCollapse: () => void
  actions: React.ReactNode
}) {
  const { t } = useTranslation()
  const uid = useId()
  const [name, setName] = useState(type.name)
  const [icon, setIcon] = useState<TypeIcon>(type.icon)
  const [slot, setSlot] = useState<TagSlot>(type.colorSlot)
  const [allDay, setAllDay] = useState(type.defaultAllDay)
  const [singleDay, setSingleDay] = useState(type.singleDay)
  const [style, setStyle] = useState<EventTypeStyle>(type.style)
  const [counter, setCounter] = useState(type.yearCounter)
  const [adminOnly, setAdminOnly] = useState(type.adminOnly)
  const [ownerless, setOwnerless] = useState(type.ownerless)
  const [minutes, setMinutes] = useState(String(type.defaultDurationMinutes))
  const [rrule, setRrule] = useState<string>(type.neverRepeats ? NEVER : type.defaultRrule)
  const [fields, setFields] = useState<EventTypeField[]>(type.fields)
  const [titleTemplate, setTitleTemplate] = useState(type.titleTemplate)
  const [instructions, setInstructions] = useState(type.instructions)
  const [busy, setBusy] = useState(false)

  const dirty =
    JSON.stringify({ name, icon, slot, style, counter, adminOnly, ownerless, allDay, singleDay, minutes, rrule, fields, titleTemplate, instructions }) !==
    JSON.stringify({
      name: type.name,
      icon: type.icon,
      slot: type.colorSlot,
      style: type.style,
      counter: type.yearCounter,
      adminOnly: type.adminOnly,
      ownerless: type.ownerless,
      allDay: type.defaultAllDay,
      singleDay: type.singleDay,
      minutes: String(type.defaultDurationMinutes),
      rrule: type.neverRepeats ? NEVER : type.defaultRrule,
      fields: type.fields,
      titleTemplate: type.titleTemplate,
      instructions: type.instructions,
    })
  // New rows get their key from the label; existing keys never change, so stored values keep their meaning.
  const keyed = useMemo(() => {
    const taken = new Set(fields.map((f) => f.key).filter(Boolean))
    return fields.map((f) => ({ ...f, key: f.key || makeKey(f.label, taken) }))
  }, [fields])
  // Generated titles are built from field values, so something must always be filled in.
  const titleNeedsRequired = !!titleTemplate.trim() && !fields.some((f) => f.required && f.kind !== 'checkbox')
  const titleFields = keyed.filter((f) => (TITLE_FIELD_KINDS as readonly string[]).includes(f.kind) && f.label.trim())
  const titleRef = useRef<HTMLInputElement>(null)
  /** Adds `{{key}}` at the caret in the title template. */
  const insertField = (key: string) => {
    const el = titleRef.current
    const at = el?.selectionStart ?? titleTemplate.length
    const to = el?.selectionEnd ?? at
    const token = `{{${key}}}`
    setTitleTemplate(titleTemplate.slice(0, at) + token + titleTemplate.slice(to))
    requestAnimationFrame(() => {
      el?.focus()
      el?.setSelectionRange(at + token.length, at + token.length)
    })
  }
  /** Wraps the selection (or an empty spot) in `{{#key}}…{{/key}}` so it shows only when that field is filled in. */
  const wrapOptional = (key: string) => {
    const el = titleRef.current
    const at = el?.selectionStart ?? titleTemplate.length
    const to = el?.selectionEnd ?? at
    const open = `{{#${key}}}`
    const close = `{{/${key}}}`
    setTitleTemplate(titleTemplate.slice(0, at) + open + titleTemplate.slice(at, to) + close + titleTemplate.slice(to))
    requestAnimationFrame(() => {
      el?.focus()
      const caret = at + open.length + (to - at)
      el?.setSelectionRange(to === at ? at + open.length : caret, to === at ? at + open.length : caret)
    })
  }
  // Sample values for the preview: the field's own label, so "{{person}}'s Birthday" reads "Person's Birthday".
  const previewValues = Object.fromEntries(keyed.map((f) => [f.key, f.label.trim()]))
  const requiredPreviewValues = Object.fromEntries(keyed.filter((f) => f.required).map((f) => [f.key, f.label.trim()]))
  const minutesValid = /^\d+$/.test(minutes) && Number(minutes) <= 1440
  const patchField = (i: number, patch: Partial<EventTypeField>) => setFields((list) => list.map((f, j) => (j === i ? { ...f, ...patch } : f)))
  const moveField = (i: number, delta: number) =>
    setFields((list) => {
      const next = [...list]
      const [f] = next.splice(i, 1)
      if (f) next.splice(i + delta, 0, f)
      return next
    })

  const submit = async () => {
    const out = keyed.map((f) => ({
      key: f.key,
      label: f.label.trim(),
      kind: f.kind,
      ...(f.required && f.kind !== 'checkbox' ? { required: true } : {}),
      ...(f.kind === 'select' ? { options: (f.options ?? []).map((o) => o.trim()).filter(Boolean), ...(f.multiple ? { multiple: true } : {}) } : {}),
      // Unset means several people, so a single-person field must say so.
      ...(f.kind === 'members' ? { multiple: allowsMany(f) } : {}),
    }))
    setBusy(true)
    await onSave({
      name: name.trim(),
      icon,
      colorSlot: slot,
      style,
      yearCounter: counter,
      adminOnly,
      ownerless,
      defaultAllDay: allDay,
      singleDay: allDay && singleDay,
      defaultDurationMinutes: Number(minutes),
      defaultRrule: rrule === NEVER ? '' : rrule,
      neverRepeats: rrule === NEVER,
      titleTemplate: titleTemplate.trim(),
      instructions: instructions.trim(),
      fields: out,
    })
    setBusy(false)
  }

  return (
    <form
      className="grid gap-4 p-4"
      aria-label={type.name}
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <div className="flex flex-wrap items-center gap-3">
        <Button type="button" variant="ghost" size="icon-xs" aria-expanded aria-label={t('eventTypes.collapse', { name: type.name })} onClick={onCollapse}>
          <ChevronDownIcon aria-hidden />
        </Button>
        <span
          className="flex size-8 shrink-0 items-center justify-center rounded-lg"
          style={{ background: `var(--${slot})`, color: `var(--${slot}-foreground)` }}
          aria-hidden
        >
          <TypeGlyph name={icon} className="size-4" />
        </span>
        <Input
          value={name}
          maxLength={64}
          aria-label={t('eventTypes.renameLabel', { name: type.name })}
          onChange={(e) => setName(e.target.value)}
          className="h-8 w-52"
        />
        <SlotPicker value={slot} onChange={setSlot} label={t('eventTypes.colorFor', { name: type.name })} name={`${uid}-slot`} />
        {actions}
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-icon`}>{t('eventTypes.icon')}</Label>
          <NativeSelect id={`${uid}-icon`} value={icon} onChange={(e) => setIcon(e.target.value as TypeIcon)}>
            {TYPE_ICONS.map((i) => (
              <option key={i} value={i}>
                {t(`eventTypes.icons.${i}`)}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-repeat`}>{t('eventTypes.defaultRepeat')}</Label>
          <NativeSelect
            id={`${uid}-repeat`}
            value={REPEATS.includes(rrule as (typeof REPEATS)[number]) ? rrule : ''}
            onChange={(e) => setRrule(e.target.value)}
          >
            {REPEATS.map((r) => (
              <option key={r} value={r}>
                {t(`eventTypes.repeats.${r || 'none'}`)}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className="flex items-center justify-between gap-3 self-end pb-1.5">
          <Label htmlFor={`${uid}-allday`}>{t('eventTypes.defaultAllDay')}</Label>
          <Switch id={`${uid}-allday`} checked={allDay} onCheckedChange={setAllDay} />
        </div>
        {allDay ? (
          <div className="grid gap-1.5">
            <Label htmlFor={`${uid}-days`}>{t('eventTypes.dates')}</Label>
            <NativeSelect id={`${uid}-days`} value={singleDay ? 'single' : 'range'} onChange={(e) => setSingleDay(e.target.value === 'single')}>
              <option value="range">{t('eventTypes.datesRange')}</option>
              <option value="single">{t('eventTypes.datesSingle')}</option>
            </NativeSelect>
          </div>
        ) : null}
        <p className="text-xs text-muted-foreground sm:col-span-3">{t('eventTypes.timingHint')}</p>
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-style`}>{t('eventTypes.style')}</Label>
          <NativeSelect id={`${uid}-style`} value={style} onChange={(e) => setStyle(e.target.value as EventTypeStyle)}>
            {EVENT_TYPE_STYLES.map((st) => (
              <option key={st} value={st}>
                {t(`eventTypes.styles.${st}`)}
              </option>
            ))}
          </NativeSelect>
          <p className="text-xs text-muted-foreground">{t(`eventTypes.styleHints.${style}`)}</p>
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-minutes`}>{t('eventTypes.defaultDuration')}</Label>
          <Input
            id={`${uid}-minutes`}
            inputMode="numeric"
            value={minutes}
            disabled={allDay}
            aria-invalid={!minutesValid}
            aria-describedby={`${uid}-minutes-hint`}
            onChange={(e) => setMinutes(e.target.value.trim())}
          />
          <p id={`${uid}-minutes-hint`} className="text-xs text-muted-foreground">
            {t('eventTypes.defaultDurationHint')}
          </p>
        </div>
        <div className="flex items-center justify-between gap-3 self-start sm:pt-6">
          <Label htmlFor={`${uid}-counter`}>{t('eventTypes.yearCounter')}</Label>
          <Switch id={`${uid}-counter`} checked={counter} onCheckedChange={setCounter} />
        </div>
        <div className="flex items-center justify-between gap-3 self-start sm:pt-6">
          <Label htmlFor={`${uid}-admin-only`}>{t('eventTypes.adminOnly')}</Label>
          <Switch id={`${uid}-admin-only`} checked={adminOnly} onCheckedChange={setAdminOnly} />
        </div>
        <div className="grid gap-1 self-start sm:pt-6">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor={`${uid}-ownerless`}>{t('eventTypes.ownerless')}</Label>
            <Switch id={`${uid}-ownerless`} checked={ownerless} onCheckedChange={setOwnerless} aria-describedby={`${uid}-ownerless-hint`} />
          </div>
          <p id={`${uid}-ownerless-hint`} className="text-xs text-muted-foreground">
            {t('eventTypes.ownerlessHint')}
          </p>
        </div>
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor={`${uid}-instructions`}>{t('eventTypes.instructions')}</Label>
        <Textarea
          id={`${uid}-instructions`}
          rows={3}
          maxLength={1000}
          value={instructions}
          placeholder={t('eventTypes.instructionsPlaceholder')}
          aria-describedby={`${uid}-instructions-hint`}
          onChange={(e) => setInstructions(e.target.value)}
        />
        <p id={`${uid}-instructions-hint`} className="text-xs text-muted-foreground">
          {t('eventTypes.instructionsHint')}
        </p>
      </div>

      <div className="grid gap-2 rounded-lg border border-border p-3">
        <Label htmlFor={`${uid}-title`} className="text-sm font-semibold">
          {t('eventTypes.titleTemplate')}
        </Label>
        <p id={`${uid}-title-hint`} className="text-xs text-muted-foreground">
          {t('eventTypes.titleTemplateHint')}
        </p>
        <Input
          id={`${uid}-title`}
          ref={titleRef}
          value={titleTemplate}
          maxLength={200}
          placeholder={braced(t('eventTypes.titleTemplatePlaceholder'))}
          aria-describedby={`${uid}-title-hint`}
          onChange={(e) => setTitleTemplate(e.target.value)}
          className="h-8 font-mono text-[0.8125rem]"
        />
        {titleFields.length ? (
          <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={t('eventTypes.insertField')}>
            <span className="text-xs text-muted-foreground">{t('eventTypes.insertField')}</span>
            {titleFields.map((f) => (
              <Button
                key={f.key}
                type="button"
                variant="outline"
                size="xs"
                aria-label={t('eventTypes.insertFieldN', { name: f.label.trim() })}
                onClick={() => insertField(f.key)}
              >
                {f.label.trim()}
              </Button>
            ))}
          </div>
        ) : null}
        {titleFields.length ? (
          <div className="flex flex-wrap items-center gap-1.5" role="group" aria-label={t('eventTypes.optionalPart')}>
            <span className="text-xs text-muted-foreground">{t('eventTypes.optionalPart')}</span>
            {titleFields.map((f) => (
              <Button
                key={f.key}
                type="button"
                variant="outline"
                size="xs"
                aria-label={t('eventTypes.optionalPartN', { name: f.label.trim() })}
                onClick={() => wrapOptional(f.key)}
              >
                {f.label.trim()}
              </Button>
            ))}
          </div>
        ) : null}
        <p className="text-xs text-muted-foreground">{braced(t('eventTypes.titleTemplateOptional'))}</p>
        {titleNeedsRequired ? (
          <p role="alert" className="text-xs font-medium text-destructive">
            {t('eventTypes.titleNeedsRequired')}
          </p>
        ) : null}
        {titleTemplate.trim() ? (
          <p className="text-sm">
            <span className="text-xs text-muted-foreground">{t('eventTypes.titlePreview')}: </span>
            <span className="font-medium">{renderTitle({ name, titleTemplate, fields: keyed }, previewValues)}</span>
          </p>
        ) : null}
        {titleTemplate.includes('{{#') ? (
          <p className="text-sm">
            <span className="text-xs text-muted-foreground">{t('eventTypes.titlePreviewRequired')}: </span>
            <span className="font-medium">{renderTitle({ name, titleTemplate, fields: keyed }, requiredPreviewValues)}</span>
          </p>
        ) : null}
      </div>

      <fieldset className="grid gap-2">
        <legend className="mb-1 text-sm font-medium">{t('eventTypes.fieldsHeading')}</legend>
        <p className="text-xs text-muted-foreground">{t('eventTypes.fieldsHint')}</p>
        {fields.length === 0 ? <p className="text-sm text-muted-foreground">{t('eventTypes.noFieldsYet')}</p> : null}
        {fields.map((f, i) => (
          <div key={f.key || `new-${i}`} className="overflow-hidden rounded-lg border border-border">
            <div className="flex flex-wrap items-center gap-2 border-b border-border bg-muted/50 px-3 py-1.5">
              <h4 className="min-w-0 flex-1 truncate text-sm font-semibold">
                <span className="text-muted-foreground">{t('eventTypes.fieldN', { n: i + 1 })}</span>
                {f.label.trim() ? <span> · {f.label.trim()}</span> : null}
              </h4>
              {f.kind !== 'checkbox' ? (
                <label className="flex items-center gap-1.5 text-sm">
                  <input
                    type="checkbox"
                    checked={!!f.required}
                    onChange={(e) => patchField(i, { required: e.target.checked })}
                    className="accent-(--primary)"
                  />
                  {t('eventTypes.required')}
                </label>
              ) : null}
              <div className="flex items-center gap-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  disabled={i === 0}
                  onClick={() => moveField(i, -1)}
                  aria-label={t('eventTypes.fieldUp', { n: i + 1 })}
                >
                  <ArrowUpIcon />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  disabled={i === fields.length - 1}
                  onClick={() => moveField(i, 1)}
                  aria-label={t('eventTypes.fieldDown', { n: i + 1 })}
                >
                  <ArrowDownIcon />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="hover:text-destructive"
                  onClick={() => setFields((list) => list.filter((_, j) => j !== i))}
                  aria-label={t('eventTypes.fieldRemove', { n: i + 1 })}
                >
                  <Trash2Icon />
                </Button>
              </div>
            </div>
            <div className="grid gap-3 p-3 sm:grid-cols-[1fr_12rem]">
              <div className="grid gap-1">
                <span aria-hidden className="text-xs font-medium text-muted-foreground">
                  {t('eventTypes.fieldLabel')}
                </span>
                <Input
                  value={f.label}
                  maxLength={64}
                  placeholder={t('eventTypes.fieldLabel')}
                  aria-label={t('eventTypes.fieldLabelN', { n: i + 1 })}
                  onChange={(e) => patchField(i, { label: e.target.value })}
                  className="h-8"
                />
              </div>
              <div className="grid gap-1">
                <span aria-hidden className="text-xs font-medium text-muted-foreground">
                  {t('eventTypes.fieldKind')}
                </span>
                <NativeSelect
                  value={f.kind}
                  aria-label={t('eventTypes.fieldKindN', { n: i + 1 })}
                  onChange={(e) =>
                    patchField(i, {
                      kind: e.target.value as FieldKind,
                      required: e.target.value === 'checkbox' ? false : f.required,
                      options: e.target.value === 'select' ? (f.options?.length ? f.options : ['']) : undefined,
                      multiple: e.target.value === 'select' || e.target.value === 'members' ? f.multiple : undefined,
                    })
                  }
                  className="h-8"
                >
                  {FIELD_KINDS.map((k) => (
                    <option key={k} value={k}>
                      {t(`eventTypes.kinds.${k}`)}
                    </option>
                  ))}
                </NativeSelect>
              </div>
              {f.kind === 'select' ? (
                <div className="grid gap-2 sm:col-span-2">
                  <div className="grid gap-1">
                    <span aria-hidden className="text-xs font-medium text-muted-foreground">
                      {t('eventTypes.choices')}
                    </span>
                    <Input
                      value={(f.options ?? []).join(', ')}
                      placeholder={t('eventTypes.optionsPlaceholder')}
                      aria-label={t('eventTypes.optionsN', { n: i + 1 })}
                      onChange={(e) => patchField(i, { options: e.target.value.split(',').map((o) => o.trimStart()) })}
                      className="h-8"
                    />
                  </div>
                  <label className="flex items-center gap-1.5 text-sm">
                    <input
                      type="checkbox"
                      checked={allowsMany(f)}
                      onChange={(e) => patchField(i, { multiple: e.target.checked })}
                      className="accent-(--primary)"
                    />
                    {t('eventTypes.multiple')}
                  </label>
                </div>
              ) : null}
              {f.kind === 'members' ? (
                <div className="sm:col-span-2">
                  <label className="flex items-center gap-1.5 text-sm">
                    <input
                      type="checkbox"
                      checked={allowsMany(f)}
                      onChange={(e) => patchField(i, { multiple: e.target.checked })}
                      className="accent-(--primary)"
                    />
                    {t('eventTypes.multiplePeople')}
                  </label>
                </div>
              ) : null}
            </div>
          </div>
        ))}
        <div>
          <Button
            type="button"
            variant="outline"
            size="xs"
            disabled={fields.length >= 20}
            onClick={() => setFields((list) => [...list, { key: '', label: '', kind: 'text' }])}
          >
            <PlusIcon aria-hidden />
            {t('eventTypes.addField')}
          </Button>
        </div>
      </fieldset>

      <div className="flex justify-end">
        <Button type="submit" size="sm" disabled={!dirty || busy || !minutesValid || !name.trim() || fields.some((f) => !f.label.trim()) || titleNeedsRequired}>
          {t('common.save')}
        </Button>
      </div>
    </form>
  )
}
