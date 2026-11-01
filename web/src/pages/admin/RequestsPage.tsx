import { useQueryClient } from '@tanstack/react-query'
import { CheckIcon, InboxIcon, PencilIcon, XIcon } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useCatalogRequests } from '@/api/queries'
import { TYPE_ICONS, type CatalogRequest, type TagSlot, type TypeIcon } from '@/api/types'
import { SlotPicker } from '@/components/admin/SlotPicker'
import { RequestSummary } from '@/components/catalog/CatalogParts'
import { EmptyState, LoadError, Page, PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input, NativeSelect, Textarea } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Segmented } from '@/components/ui/segmented'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatRelative } from '@/lib/time'

/** Admins review what people asked for: approve (optionally adjusting it first) or reject with a note. */
export function RequestsPage() {
  const { t } = useTranslation()
  const [view, setView] = useState<'open' | 'all'>('open')
  const { data, isPending, isError, refetch } = useCatalogRequests(view)
  const requests = data?.requests ?? []

  return (
    <Page>
      <PageHeader
        title={t('requests.title')}
        description={t('requests.description')}
        actions={
          <Segmented
            value={view}
            onChange={setView}
            label={t('requests.view')}
            options={[
              { value: 'open', label: t('requests.open') },
              { value: 'all', label: t('requests.all') },
            ]}
          />
        }
      />
      {isPending ? (
        <Skeleton className="h-40" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : requests.length === 0 ? (
        <EmptyState icon={<InboxIcon />} title={t(view === 'open' ? 'requests.emptyOpen' : 'requests.emptyAll')} body={t('requests.emptyBody')} />
      ) : (
        <ul className="divide-y divide-border rounded-xl border border-border bg-card" aria-label={t('requests.list')}>
          {requests.map((r) => (
            <RequestRow key={r.id} request={r} />
          ))}
        </ul>
      )}
    </Page>
  )
}

function RequestRow({ request: r }: { request: CatalogRequest }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const uid = useId()
  const [mode, setMode] = useState<'idle' | 'edit' | 'reject'>('idle')
  const [name, setName] = useState(r.name)
  const [slot, setSlot] = useState<TagSlot>(r.colorSlot)
  const [icon, setIcon] = useState<TypeIcon>(r.icon || 'calendar')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = (catalog: boolean) => {
    void qc.invalidateQueries({ queryKey: keys.catalogRequestsAll })
    if (catalog) void qc.invalidateQueries({ queryKey: r.kind === 'tag' ? keys.tags : keys.eventTypes })
  }

  const run = async (job: () => Promise<unknown>, failed: string, done: string, catalog: boolean) => {
    if (busy) return
    setBusy(true)
    try {
      await job()
      toast.success(done)
      refresh(catalog)
    } catch (err) {
      toast.error(failed, errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const approve = () => {
    const n = name.trim()
    if (!n) return
    void run(
      () =>
        endpoints.approveCatalogRequest(r.id, {
          ...(n !== r.name ? { name: n } : {}),
          ...(slot !== r.colorSlot ? { colorSlot: slot } : {}),
          ...(r.kind === 'event_type' && icon !== r.icon ? { icon } : {}),
        }),
      t('requests.approveFailed'),
      t('requests.approved', { name: n }),
      true,
    )
  }

  const reject = () =>
    void run(() => endpoints.rejectCatalogRequest(r.id, note.trim()), t('requests.rejectFailed'), t('requests.rejected', { name: r.name }), false)

  const byline = t('requests.byline', { name: r.author.name, when: formatRelative(r.createdAt) })

  return (
    <li className="grid gap-3 px-3 py-3">
      <div className="flex flex-wrap items-start gap-3">
        <RequestSummary request={r} byline={byline} />
        {r.permissions.review && mode === 'idle' ? (
          <div className="flex items-center gap-1">
            <Button size="xs" disabled={busy} onClick={approve} aria-label={t('requests.approve', { name: r.name })}>
              <CheckIcon aria-hidden />
              {t('requests.approveShort')}
            </Button>
            <Button variant="outline" size="xs" onClick={() => setMode('edit')} aria-label={t('requests.edit', { name: r.name })}>
              <PencilIcon aria-hidden />
              {t('requests.editShort')}
            </Button>
            <Button
              variant="ghost"
              size="xs"
              className="hover:text-destructive"
              onClick={() => setMode('reject')}
              aria-label={t('requests.reject', { name: r.name })}
            >
              <XIcon aria-hidden />
              {t('requests.rejectShort')}
            </Button>
          </div>
        ) : null}
      </div>

      {mode === 'edit' ? (
        <form
          className="grid gap-3 rounded-lg bg-muted/50 p-3"
          onSubmit={(e) => {
            e.preventDefault()
            approve()
          }}
        >
          <p className="text-xs text-muted-foreground">{t('requests.editHint')}</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div className="grid gap-1.5">
              <Label htmlFor={`${uid}-name`}>{t('catalog.name')}</Label>
              <Input id={`${uid}-name`} value={name} maxLength={64} onChange={(e) => setName(e.target.value)} />
            </div>
            {r.kind === 'event_type' ? (
              <div className="grid gap-1.5">
                <Label htmlFor={`${uid}-icon`}>{t('catalog.icon')}</Label>
                <NativeSelect id={`${uid}-icon`} value={icon} onChange={(e) => setIcon(e.target.value as TypeIcon)}>
                  {TYPE_ICONS.map((i) => (
                    <option key={i} value={i}>
                      {t(`eventTypes.icons.${i}`)}
                    </option>
                  ))}
                </NativeSelect>
              </div>
            ) : null}
          </div>
          <SlotPicker value={slot} onChange={setSlot} label={t('catalog.color')} name={`${uid}-slot`} />
          <div className="flex gap-2">
            <Button type="submit" size="sm" disabled={busy || !name.trim()}>
              {t('requests.approveWithChanges')}
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setMode('idle')}>
              {t('common.cancel')}
            </Button>
          </div>
        </form>
      ) : null}

      {mode === 'reject' ? (
        <form
          className="grid gap-3 rounded-lg bg-muted/50 p-3"
          onSubmit={(e) => {
            e.preventDefault()
            reject()
          }}
        >
          <div className="grid gap-1.5">
            <Label htmlFor={`${uid}-note`}>{t('requests.noteLabel')}</Label>
            <Textarea
              id={`${uid}-note`}
              value={note}
              rows={2}
              maxLength={2000}
              onChange={(e) => setNote(e.target.value)}
              placeholder={t('requests.notePlaceholder')}
            />
          </div>
          <div className="flex gap-2">
            <Button type="submit" variant="destructive" size="sm" disabled={busy}>
              {t('requests.confirmReject')}
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setMode('idle')}>
              {t('common.cancel')}
            </Button>
          </div>
        </form>
      ) : null}
    </li>
  )
}
