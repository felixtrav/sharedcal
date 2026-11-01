import { useQueryClient } from '@tanstack/react-query'
import { SendIcon, Undo2Icon } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useCatalogRequests, useEventTypes, useMeData, useTags } from '@/api/queries'
import { TAG_SLOTS, TYPE_ICONS, type CatalogKind, type TagSlot, type TypeIcon } from '@/api/types'
import { SlotPicker } from '@/components/admin/SlotPicker'
import { CatalogSwatch, RequestSummary } from '@/components/catalog/CatalogParts'
import { EmptyState, LoadError, Page, PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input, NativeSelect, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatRelative } from '@/lib/time'
import { TypeGlyph } from '@/lib/typeIcons'

const MAX_OTHER = 500

/** Everyone can browse the tags and event types; people who aren't admins can also ask for new ones. */
export function CatalogPage() {
  const { t } = useTranslation()
  const me = useMeData()
  const canRequest = !me.permissions.admin
  return (
    <Page wide>
      <PageHeader title={t('catalog.title')} description={t(canRequest ? 'catalog.description' : 'catalog.descriptionAdmin')} />
      <div className="grid gap-6 md:grid-cols-2">
        <TagList />
        <TypeList />
      </div>
      {canRequest ? (
        <>
          <RequestForm />
          <MyRequests />
        </>
      ) : null}
    </Page>
  )
}

function TagList() {
  const { t } = useTranslation()
  const { data, isPending, isError, refetch } = useTags()
  const tags = data?.tags ?? []
  return (
    <section aria-labelledby="catalog-tags">
      <h2 id="catalog-tags" className="mb-2 text-sm font-semibold">
        {t('catalog.tags')}
      </h2>
      {isPending ? (
        <Skeleton className="h-32" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : tags.length === 0 ? (
        <EmptyState title={t('catalog.noTags')} />
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border bg-card">
          {tags.map((tag) => (
            <li key={tag.id} className="flex items-center gap-3 px-3 py-2.5">
              <CatalogSwatch kind="tag" slot={tag.colorSlot} />
              <span className="truncate text-sm font-medium">{tag.name}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

function TypeList() {
  const { t } = useTranslation()
  const { data, isPending, isError, refetch } = useEventTypes()
  const types = data?.eventTypes ?? []
  return (
    <section aria-labelledby="catalog-types">
      <h2 id="catalog-types" className="mb-2 text-sm font-semibold">
        {t('catalog.eventTypes')}
      </h2>
      {isPending ? (
        <Skeleton className="h-32" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : types.length === 0 ? (
        <EmptyState title={t('catalog.noTypes')} />
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border bg-card">
          {types.map((type) => (
            <li key={type.id} className="flex items-center gap-3 px-3 py-2.5">
              <CatalogSwatch kind="event_type" icon={type.icon} slot={type.colorSlot} />
              <span className="truncate text-sm font-medium">{type.name}</span>
              {type.adminOnly ? <span className="ms-auto text-xs text-muted-foreground">{t('catalog.adminOnly')}</span> : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** Name, color, icon (event types) and a note: the only things a request can carry. */
function RequestForm() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const uid = useId()
  const [kind, setKind] = useState<CatalogKind>('tag')
  const [name, setName] = useState('')
  const [slot, setSlot] = useState<TagSlot>(TAG_SLOTS[0])
  const [icon, setIcon] = useState<TypeIcon>('calendar')
  const [other, setOther] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    const n = name.trim()
    if (!n || busy) return
    setBusy(true)
    try {
      await endpoints.createCatalogRequest({
        kind,
        name: n,
        colorSlot: slot,
        ...(kind === 'event_type' ? { icon } : {}),
        ...(other.trim() ? { other: other.trim() } : {}),
      })
      toast.success(t('catalog.sent', { name: n }))
      setName('')
      setOther('')
      void qc.invalidateQueries({ queryKey: keys.catalogRequestsAll })
    } catch (err) {
      toast.error(t('catalog.sendFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      className="mt-8 grid gap-4 rounded-xl border border-border bg-card p-4"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <div>
        <h2 className="text-sm font-semibold">{t('catalog.suggest')}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t('catalog.suggestHint')}</p>
      </div>
      <Segmented
        value={kind}
        onChange={setKind}
        label={t('catalog.kind')}
        options={[
          { value: 'tag', label: t('catalog.kinds.tag') },
          { value: 'event_type', label: t('catalog.kinds.event_type') },
        ]}
      />
      <div className="grid gap-4 sm:grid-cols-2">
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-name`}>{t('catalog.name')}</Label>
          <Input id={`${uid}-name`} value={name} maxLength={64} required onChange={(e) => setName(e.target.value)} placeholder={t('catalog.namePlaceholder')} />
        </div>
        {kind === 'event_type' ? (
          <div className="grid gap-1.5">
            <Label htmlFor={`${uid}-icon`}>{t('catalog.icon')}</Label>
            <div className="flex items-center gap-2">
              <span
                className="flex size-9 shrink-0 items-center justify-center rounded-lg"
                style={{ background: `var(--${slot})`, color: `var(--${slot}-foreground)` }}
                aria-hidden
              >
                <TypeGlyph name={icon} className="size-4" />
              </span>
              <NativeSelect id={`${uid}-icon`} value={icon} onChange={(e) => setIcon(e.target.value as TypeIcon)}>
                {TYPE_ICONS.map((i) => (
                  <option key={i} value={i}>
                    {t(`eventTypes.icons.${i}`)}
                  </option>
                ))}
              </NativeSelect>
            </div>
          </div>
        ) : null}
      </div>
      <div className="grid gap-1.5">
        <span id={`${uid}-color`} className="text-sm leading-none font-medium">
          {t('catalog.color')}
        </span>
        <SlotPicker value={slot} onChange={setSlot} label={t('catalog.color')} name={`${uid}-slot`} />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor={`${uid}-other`}>
          {t('catalog.other')} <span className="font-normal text-muted-foreground">{t('catalog.optional')}</span>
        </Label>
        <Textarea
          id={`${uid}-other`}
          value={other}
          maxLength={MAX_OTHER}
          rows={3}
          onChange={(e) => setOther(e.target.value)}
          placeholder={t('catalog.otherPlaceholder')}
        />
        <p className="text-xs text-muted-foreground tabular-nums">{t('catalog.otherCount', { count: other.length, max: MAX_OTHER })}</p>
      </div>
      <div>
        <Button type="submit" size="sm" disabled={!name.trim() || busy}>
          <SendIcon aria-hidden />
          {t('catalog.send')}
        </Button>
      </div>
    </form>
  )
}

function MyRequests() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isPending, isError, refetch } = useCatalogRequests('all')
  const requests = data?.requests ?? []

  const withdraw = async (id: string, name: string) => {
    try {
      await endpoints.withdrawCatalogRequest(id)
      toast({ title: t('catalog.withdrawn', { name }) })
      void qc.invalidateQueries({ queryKey: keys.catalogRequestsAll })
    } catch (err) {
      toast.error(t('catalog.withdrawFailed'), errorMessage(err, t))
    }
  }

  return (
    <section className="mt-8" aria-labelledby="catalog-mine">
      <h2 id="catalog-mine" className="mb-2 text-sm font-semibold">
        {t('catalog.mine')}
      </h2>
      {isPending ? (
        <Skeleton className="h-24" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : requests.length === 0 ? (
        <EmptyState title={t('catalog.noneYet')} body={t('catalog.noneYetBody')} />
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border bg-card">
          {requests.map((r) => (
            <li key={r.id} className="flex flex-wrap items-start gap-3 px-3 py-3">
              <RequestSummary request={r} byline={formatRelative(r.createdAt)} />
              {r.permissions.withdraw ? (
                <Button variant="ghost" size="xs" onClick={() => void withdraw(r.id, r.name)} aria-label={t('catalog.withdraw', { name: r.name })}>
                  <Undo2Icon aria-hidden />
                  {t('catalog.withdrawShort')}
                </Button>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
