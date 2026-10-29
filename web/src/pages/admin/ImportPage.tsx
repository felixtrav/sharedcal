import { useQueryClient } from '@tanstack/react-query'
import { FileUpIcon } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useEventTypes } from '@/api/queries'
import type { IcsImportResult } from '@/api/types'
import { EmptyState, Page, PageHeader } from '@/components/layout/PageHeader'
import { Button } from '@/components/ui/button'
import { Input, NativeSelect } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'

export function ImportPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const uid = useId()
  const { data: typesData } = useEventTypes()
  const types = typesData?.eventTypes ?? []
  const [ics, setIcs] = useState('')
  const [typeId, setTypeId] = useState('')
  const [preview, setPreview] = useState<IcsImportResult | null>(null)
  const [busy, setBusy] = useState(false)

  const run = async (nextIcs: string, nextType: string, dryRun: boolean) => {
    setBusy(true)
    try {
      const res = await endpoints.importIcs({ ics: nextIcs, eventTypeId: nextType || undefined, dryRun })
      if (dryRun) {
        setPreview(res)
      } else {
        toast.success(t('import.done', { count: res.created }), t('import.doneBody', { duplicates: res.duplicates, invalid: res.invalid }))
        void qc.invalidateQueries({ queryKey: keys.eventsAll })
        setIcs('')
        setPreview(null)
      }
    } catch (err) {
      setPreview(null)
      toast.error(t(dryRun ? 'import.readFailed' : 'import.failed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const onFile = async (file: File | undefined) => {
    setPreview(null)
    if (!file) return setIcs('')
    const text = await file.text()
    setIcs(text)
    await run(text, typeId, true)
  }

  const onType = (id: string) => {
    setTypeId(id)
    if (ics) void run(ics, id, true)
  }

  return (
    <Page>
      <PageHeader title={t('import.title')} description={t('import.description')} />
      <div className="mb-6 grid gap-4 rounded-xl border border-border bg-card p-4">
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-file`}>{t('import.file')}</Label>
          <Input id={`${uid}-file`} type="file" accept=".ics,text/calendar" disabled={busy} onChange={(e) => void onFile(e.target.files?.[0])} />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor={`${uid}-type`}>{t('import.type')}</Label>
          <NativeSelect id={`${uid}-type`} value={typeId} disabled={busy} onChange={(e) => onType(e.target.value)} aria-describedby={`${uid}-type-hint`}>
            <option value="">{t('import.generic')}</option>
            {types.map((et) => (
              <option key={et.id} value={et.id}>
                {et.name}
              </option>
            ))}
          </NativeSelect>
          <p id={`${uid}-type-hint`} className="text-xs text-muted-foreground">
            {t('import.typeHint')}
          </p>
        </div>
      </div>

      {!preview ? (
        <EmptyState icon={<FileUpIcon />} title={busy ? t('import.previewing') : t('import.choose')} body="" />
      ) : (
        <section aria-label={t('import.preview')} className="grid gap-3">
          <p role="status" className="text-sm">
            {t('import.summary', { created: preview.created, duplicates: preview.duplicates, invalid: preview.invalid, ignored: preview.ignored })}
          </p>
          {preview.ignored ? <p className="text-xs text-muted-foreground">{t('import.ignoredHint')}</p> : null}
          <div className="overflow-x-auto rounded-xl border border-border bg-card">
            <table className="w-full text-start text-sm" aria-label={t('import.list')}>
              <thead className="text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="p-2 text-start font-medium">
                    {t('import.columns.title')}
                  </th>
                  <th scope="col" className="p-2 text-start font-medium">
                    {t('import.columns.when')}
                  </th>
                  <th scope="col" className="p-2 text-start font-medium">
                    {t('import.columns.result')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {preview.entries.map((en, i) => (
                  <tr key={`${en.uid}:${i}`} className="border-t border-border align-top">
                    <td className="p-2">{en.title || t('event.untitled')}</td>
                    <td className="p-2 whitespace-nowrap">
                      {en.start}
                      {en.rrule ? ` · ${en.rrule}` : ''}
                    </td>
                    <td className="p-2">
                      <span className={en.outcome === 'invalid' ? 'text-destructive' : en.outcome === 'duplicate' ? 'text-muted-foreground' : ''}>
                        {t(`import.outcome.${en.outcome}`)}
                      </span>
                      {en.error ? <span className="block text-xs text-muted-foreground">{en.error}</span> : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div>
            <Button disabled={busy || preview.created === 0} onClick={() => void run(ics, typeId, false)}>
              {busy ? t('import.importing') : t('import.import', { count: preview.created })}
            </Button>
          </div>
        </section>
      )}
    </Page>
  )
}
