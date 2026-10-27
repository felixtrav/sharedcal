import { useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckIcon, CopyIcon, RefreshCwIcon, RssIcon, ShieldAlertIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { Page, PageHeader } from '@/components/layout/PageHeader'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatDateTime } from '@/lib/time'
import { useFmt } from '@/lib/useFmt'

export function SubscribePage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const fmt = useFmt()
  const { data, isPending } = useQuery({ queryKey: keys.feedToken, queryFn: endpoints.feedToken })
  const [url, setUrl] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)
  const [confirm, setConfirm] = useState<'rotate' | 'revoke' | null>(null)
  const [busy, setBusy] = useState(false)

  const create = async () => {
    setBusy(true)
    try {
      const res = await endpoints.createFeedToken()
      setUrl(res.url)
      setCopied(false)
      void qc.invalidateQueries({ queryKey: keys.feedToken })
    } catch (err) {
      toast.error(t('subscribe.createFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const revoke = async () => {
    setBusy(true)
    try {
      await endpoints.revokeFeedToken()
      setUrl(null)
      void qc.invalidateQueries({ queryKey: keys.feedToken })
      toast({ title: t('subscribe.revoked') })
    } catch (err) {
      toast.error(t('subscribe.revokeFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  const copy = async () => {
    if (!url) return
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      toast.error(t('subscribe.copyFailed'))
    }
  }

  return (
    <Page>
      <PageHeader title={t('subscribe.title')} description={t('subscribe.description')} />
      <div className="grid gap-6">
        <section className="rounded-xl border border-border bg-card p-5" aria-labelledby="feed-status">
          <div className="flex items-start gap-4">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary" aria-hidden>
              <RssIcon className="size-5" />
            </span>
            <div className="min-w-0 flex-1">
              <h2 id="feed-status" className="font-semibold">
                {t('subscribe.feed')}
              </h2>
              {isPending ? (
                <Skeleton className="mt-2 h-4 w-48" />
              ) : data?.exists ? (
                <p className="mt-1 text-sm text-muted-foreground">
                  {t('subscribe.activeSince', { date: data.createdAt ? formatDateTime(Date.parse(data.createdAt), fmt) : '—' })}
                  {' · '}
                  {data.lastUsedAt ? t('subscribe.lastUsed', { date: formatDateTime(Date.parse(data.lastUsedAt), fmt) }) : t('subscribe.neverUsed')}
                </p>
              ) : (
                <p className="mt-1 text-sm text-muted-foreground">{t('subscribe.none')}</p>
              )}
            </div>
          </div>

          {url ? (
            <div className="mt-5 grid gap-2 rounded-lg border border-primary/40 bg-primary/5 p-4">
              <label htmlFor="feed-url" className="text-sm font-medium">
                {t('subscribe.yourUrl')}
              </label>
              <div className="flex gap-2">
                <Input id="feed-url" readOnly value={url} onFocus={(e) => e.currentTarget.select()} className="font-mono text-xs" />
                <Button variant="outline" onClick={() => void copy()} aria-live="polite">
                  {copied ? <CheckIcon aria-hidden /> : <CopyIcon aria-hidden />}
                  {copied ? t('subscribe.copied') : t('subscribe.copy')}
                </Button>
              </div>
              <p className="flex items-start gap-2 text-xs text-muted-foreground">
                <ShieldAlertIcon className="mt-0.5 size-3.5 shrink-0" aria-hidden />
                {t('subscribe.shownOnce')}
              </p>
            </div>
          ) : null}

          <div className="mt-5 flex flex-wrap gap-2">
            {data?.exists ? (
              <>
                <Button variant="outline" disabled={busy} onClick={() => setConfirm('rotate')}>
                  <RefreshCwIcon aria-hidden />
                  {t('subscribe.rotate')}
                </Button>
                <Button variant="ghost" className="text-destructive hover:text-destructive" disabled={busy} onClick={() => setConfirm('revoke')}>
                  {t('subscribe.revoke')}
                </Button>
              </>
            ) : (
              <Button disabled={busy || isPending} onClick={() => void create()}>
                {t('subscribe.create')}
              </Button>
            )}
          </div>
        </section>

        <section className="text-sm text-muted-foreground" aria-labelledby="feed-help">
          <h2 id="feed-help" className="mb-2 font-semibold text-foreground">
            {t('subscribe.howTitle')}
          </h2>
          <ul className="grid list-disc gap-1 ps-5">
            <li>{t('subscribe.howApple')}</li>
            <li>{t('subscribe.howGoogle')}</li>
            <li>{t('subscribe.howThunderbird')}</li>
          </ul>
          <p className="mt-3">{t('subscribe.readOnlyNote')}</p>
        </section>
      </div>

      <ConfirmDialog
        open={confirm === 'rotate'}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t('subscribe.rotateTitle')}
        description={t('subscribe.rotateBody')}
        confirmLabel={t('subscribe.rotate')}
        onConfirm={() => void create()}
      />
      <ConfirmDialog
        open={confirm === 'revoke'}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t('subscribe.revokeTitle')}
        description={t('subscribe.revokeBody')}
        confirmLabel={t('subscribe.revoke')}
        destructive
        onConfirm={() => void revoke()}
      />
    </Page>
  )
}
