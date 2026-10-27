import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ImageUpIcon, Trash2Icon, UploadIcon } from 'lucide-react'
import { useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { fieldErrors } from '@/api/client'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useMeData, useThemes } from '@/api/queries'
import type { Role, Settings, Theme } from '@/api/types'
import { TimeZoneSelect } from '@/components/event/TimeZoneSelect'
import { Page, PageHeader } from '@/components/layout/PageHeader'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field } from '@/components/ui/field'
import { Input, NativeSelect } from '@/components/ui/input'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatFileSize } from '@/lib/eventText'
import { contrastRatio, parseHex } from '@/themes/contrast'
import { BUILTIN_THEMES, validateTheme } from '@/themes/compile'

const MB = 1024 * 1024
const GB = 1024 * MB

function Section({ id, title, description, children }: { id: string; title: string; description?: string; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="grid gap-4 border-b border-border py-6 first:pt-0 last:border-0 md:grid-cols-[minmax(0,15rem)_1fr]">
      <div>
        <h2 id={id} className="text-sm font-semibold">
          {title}
        </h2>
        {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
      </div>
      <div className="grid max-w-xl gap-4">{children}</div>
    </section>
  )
}

export function SettingsPage() {
  const { t } = useTranslation()
  const { data, isPending, isError, refetch, dataUpdatedAt } = useQuery({ queryKey: keys.settings, queryFn: endpoints.settings })
  return (
    <Page wide>
      <PageHeader title={t('settings.title')} description={t('settings.description')} />
      {isPending ? (
        <Skeleton className="h-96" />
      ) : isError ? (
        <div role="alert" className="text-sm">
          {t('errors.loadFailed')}{' '}
          <Button variant="link" size="xs" onClick={() => void refetch()}>
            {t('common.retry')}
          </Button>
        </div>
      ) : (
        <SettingsForm key={dataUpdatedAt} initial={data} />
      )}
    </Page>
  )
}

function SettingsForm({ initial }: { initial: Settings }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const me = useMeData()
  const [s, setS] = useState<Settings>(initial)
  const [errors, setErrors] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const dirty = JSON.stringify(s) !== JSON.stringify(initial)
  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setS((prev) => ({ ...prev, [k]: v }))

  const accentOk = !s.accentColor || !!parseHex(s.accentColor)
  const accentContrast = s.accentColor && parseHex(s.accentColor) ? contrastRatio(s.accentColor, '#ffffff') : null

  const save = async () => {
    if (!accentOk) {
      setErrors({ accentColor: t('settings.accentInvalid') })
      return
    }
    setSaving(true)
    setErrors({})
    try {
      const saved = await endpoints.saveSettings(s)
      qc.setQueryData(keys.settings, saved)
      void qc.invalidateQueries({ queryKey: keys.me })
      void qc.invalidateQueries({ queryKey: keys.themesAll })
      toast.success(t('settings.saved'))
    } catch (err) {
      setErrors(fieldErrors(err))
      toast.error(t('settings.saveFailed'), errorMessage(err, t))
    } finally {
      setSaving(false)
    }
  }

  const usage = s.attachmentQuota ? s.storageUsed / s.attachmentQuota : 0

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        void save()
      }}
      noValidate
    >
      <Section id="set-general" title={t('settings.general')}>
        <Field id="set-name" label={t('settings.calendarName')} error={errors.calendarName}>
          <Input id="set-name" value={s.calendarName} maxLength={80} onChange={(e) => set('calendarName', e.target.value)} />
        </Field>
        <Field id="set-tz" label={t('settings.defaultTimeZone')} error={errors.defaultTimeZone}>
          <TimeZoneSelect id="set-tz" value={s.defaultTimeZone} onChange={(v) => set('defaultTimeZone', v)} />
        </Field>
        <Field id="set-role" label={t('settings.defaultRole')} hint={t('settings.defaultRoleHint')} error={errors.defaultRole}>
          <NativeSelect id="set-role" value={s.defaultRole} onChange={(e) => set('defaultRole', e.target.value as Role)}>
            {(['viewer', 'writer', 'admin'] as const).map((r) => (
              <option key={r} value={r}>
                {t(`roles.${r}`)}
              </option>
            ))}
          </NativeSelect>
        </Field>
      </Section>

      <Section id="set-brand" title={t('settings.branding')} description={t('settings.brandingHint')}>
        <Field id="set-accent" label={t('settings.accentColor')} error={errors.accentColor} hint={t('settings.accentHint')}>
          <div className="flex items-center gap-2">
            <input
              type="color"
              aria-label={t('settings.accentPicker')}
              value={parseHex(s.accentColor) ? s.accentColor : '#4f46e5'}
              onChange={(e) => set('accentColor', e.target.value)}
              className="h-9 w-12 cursor-pointer rounded-md border border-input bg-background p-1"
            />
            <Input
              id="set-accent"
              value={s.accentColor}
              placeholder={t('settings.accentDefault')}
              onChange={(e) => set('accentColor', e.target.value.trim())}
              aria-invalid={!accentOk}
              className="w-36 font-mono"
            />
            {s.accentColor ? (
              <Button type="button" variant="ghost" size="sm" onClick={() => set('accentColor', '')}>
                {t('settings.useThemeAccent')}
              </Button>
            ) : null}
          </div>
          {accentContrast !== null && accentContrast < 3 ? <p className="text-xs text-muted-foreground">{t('settings.accentLowContrast')}</p> : null}
        </Field>
        <LogoField hasLogo={me.instance.hasLogo} logoUrl={me.instance.logoUrl} />
      </Section>

      <Section id="set-themes" title={t('settings.themes')} description={t('settings.themesHint')}>
        <ThemesField enabled={s.enabledThemes} onChange={(ids) => set('enabledThemes', ids)} />
      </Section>

      <Section id="set-att" title={t('settings.attachments')}>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field id="set-maxsize" label={t('settings.maxSize')} error={errors.attachmentMaxSize}>
            <Input
              id="set-maxsize"
              type="number"
              min={1}
              value={Math.round(s.attachmentMaxSize / MB)}
              onChange={(e) => set('attachmentMaxSize', Math.max(1, Number(e.target.value)) * MB)}
            />
          </Field>
          <Field id="set-quota" label={t('settings.quota')} error={errors.attachmentQuota}>
            <Input
              id="set-quota"
              type="number"
              min={0}
              step={0.5}
              value={Number((s.attachmentQuota / GB).toFixed(2))}
              onChange={(e) => set('attachmentQuota', Math.max(0, Number(e.target.value)) * GB)}
            />
          </Field>
          <Field id="set-perevent" label={t('settings.maxPerEvent')} error={errors.attachmentMaxPerEvent}>
            <Input id="set-perevent" type="number" min={0} value={s.attachmentMaxPerEvent} onChange={(e) => set('attachmentMaxPerEvent', Math.max(0, Number(e.target.value)))} />
          </Field>
          <Field id="set-percomment" label={t('settings.maxPerComment')} error={errors.attachmentMaxPerComment}>
            <Input id="set-percomment" type="number" min={0} value={s.attachmentMaxPerComment} onChange={(e) => set('attachmentMaxPerComment', Math.max(0, Number(e.target.value)))} />
          </Field>
        </div>
        <div className="grid gap-2">
          <p className="text-sm font-medium">{t('settings.storageUsed')}</p>
          <Progress value={usage} label={t('settings.storageUsed')} className="h-2" />
          <p className="text-xs text-muted-foreground tabular-nums">
            {t('settings.storageOf', { used: formatFileSize(s.storageUsed), quota: formatFileSize(s.attachmentQuota), pct: Math.round(usage * 100) })}
          </p>
        </div>
      </Section>

      <div className="sticky bottom-0 -mx-4 flex justify-end gap-2 border-t border-border bg-background/95 px-4 py-3 backdrop-blur sm:-mx-8 sm:px-8">
        <Button type="button" variant="outline" disabled={!dirty || saving} onClick={() => setS(initial)}>
          {t('common.reset')}
        </Button>
        <Button type="submit" disabled={!dirty || saving}>
          {t('common.save')}
        </Button>
      </div>
    </form>
  )
}

function LogoField({ hasLogo, logoUrl }: { hasLogo: boolean; logoUrl: string }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const input = useRef<HTMLInputElement>(null)
  const [progress, setProgress] = useState<number | null>(null)
  const [confirm, setConfirm] = useState(false)

  const upload = async (file: File) => {
    if (!/^image\/(png|jpeg|webp)$/.test(file.type)) {
      toast.error(t('settings.logoType'))
      return
    }
    setProgress(0)
    try {
      await endpoints.uploadLogo(file, setProgress).promise
      void qc.invalidateQueries({ queryKey: keys.me })
      toast.success(t('settings.logoUpdated'))
    } catch (err) {
      toast.error(t('settings.logoFailed'), errorMessage(err, t))
    } finally {
      setProgress(null)
    }
  }

  const remove = async () => {
    try {
      await endpoints.deleteLogo()
      void qc.invalidateQueries({ queryKey: keys.me })
      toast({ title: t('settings.logoRemoved') })
    } catch (err) {
      toast.error(t('settings.logoFailed'), errorMessage(err, t))
    }
  }

  return (
    <div className="grid gap-2">
      <span className="text-sm font-medium">{t('settings.logo')}</span>
      <div className="flex items-center gap-3">
        <div className="flex size-14 items-center justify-center overflow-hidden rounded-lg border border-border bg-muted">
          {hasLogo && logoUrl ? <img src={logoUrl} alt={t('settings.currentLogo')} className="size-full object-contain" /> : <ImageUpIcon className="size-5 text-muted-foreground" aria-hidden />}
        </div>
        <Button type="button" variant="outline" size="sm" disabled={progress !== null} onClick={() => input.current?.click()}>
          <UploadIcon aria-hidden />
          {hasLogo ? t('settings.replaceLogo') : t('settings.uploadLogo')}
        </Button>
        {hasLogo ? (
          <Button type="button" variant="ghost" size="sm" onClick={() => setConfirm(true)}>
            <Trash2Icon aria-hidden />
            {t('settings.removeLogo')}
          </Button>
        ) : null}
        <input
          ref={input}
          type="file"
          accept="image/png,image/jpeg,image/webp"
          hidden
          aria-hidden
          tabIndex={-1}
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) void upload(f)
            e.target.value = ''
          }}
        />
      </div>
      {progress !== null ? <Progress value={progress} label={t('settings.uploadingLogo')} /> : null}
      <ConfirmDialog open={confirm} onOpenChange={setConfirm} title={t('settings.removeLogo')} confirmLabel={t('settings.removeLogo')} destructive onConfirm={() => void remove()} />
    </div>
  )
}

function ThemesField({ enabled, onChange }: { enabled: string[]; onChange: (ids: string[]) => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data } = useThemes(true)
  const input = useRef<HTMLInputElement>(null)
  const [errors, setErrors] = useState<string[]>([])
  const [removing, setRemoving] = useState<Theme | null>(null)
  const themes = data?.themes ?? BUILTIN_THEMES

  const upload = async (file: File) => {
    setErrors([])
    let json: unknown
    try {
      json = JSON.parse(await file.text())
    } catch {
      setErrors([t('settings.themeNotJson')])
      return
    }
    const problems = validateTheme(json)
    if (problems.length) {
      setErrors(problems)
      return
    }
    try {
      const theme = await endpoints.uploadTheme(json as Omit<Theme, 'builtin'>)
      void qc.invalidateQueries({ queryKey: keys.themesAll })
      if (!enabled.includes(theme.id)) onChange([...enabled, theme.id])
      toast.success(t('settings.themeUploaded', { name: theme.name }))
    } catch (err) {
      const fe = fieldErrors(err)
      setErrors(Object.values(fe).length ? Object.values(fe) : [errorMessage(err, t)])
    }
  }

  const remove = async (theme: Theme) => {
    try {
      await endpoints.deleteTheme(theme.id)
      onChange(enabled.filter((id) => id !== theme.id))
      void qc.invalidateQueries({ queryKey: keys.themesAll })
      toast({ title: t('settings.themeRemoved', { name: theme.name }) })
    } catch (err) {
      toast.error(t('settings.themeRemoveFailed'), errorMessage(err, t))
    }
  }

  return (
    <div className="grid gap-3">
      <ul className="grid gap-2">
        {themes.map((th) => {
          const isDefault = th.id === 'default'
          return (
            <li key={th.id} className="flex items-center gap-3 rounded-lg border border-border px-3 py-2.5">
              <Checkbox
                id={`theme-${th.id}`}
                checked={isDefault || enabled.includes(th.id)}
                disabled={isDefault}
                onCheckedChange={(v) => onChange(v ? [...enabled, th.id] : enabled.filter((x) => x !== th.id))}
              />
              <label htmlFor={`theme-${th.id}`} className="min-w-0 flex-1 text-sm">
                <span className="font-medium">{th.name}</span> <span className="text-muted-foreground">· {th.author}</span>
              </label>
              <span className="flex gap-0.5" aria-hidden>
                {[1, 2, 3, 4, 5].map((i) => (
                  <span key={i} className="size-3 rounded-full" style={{ background: th.modes.light[`tag-${i}`] ?? th.modes.light.primary }} />
                ))}
              </span>
              {th.builtin ? (
                <Badge variant="outline">{t('settings.builtin')}</Badge>
              ) : (
                <Button type="button" variant="ghost" size="icon-xs" onClick={() => setRemoving(th)} aria-label={t('settings.removeTheme', { name: th.name })}>
                  <Trash2Icon />
                </Button>
              )}
            </li>
          )
        })}
      </ul>
      <div>
        <Button type="button" variant="outline" size="sm" onClick={() => input.current?.click()}>
          <UploadIcon aria-hidden />
          {t('settings.uploadTheme')}
        </Button>
        <input
          ref={input}
          type="file"
          accept="application/json,.json"
          hidden
          aria-hidden
          tabIndex={-1}
          onChange={(e) => {
            const f = e.target.files?.[0]
            if (f) void upload(f)
            e.target.value = ''
          }}
        />
      </div>
      {errors.length ? (
        <div role="alert" className="rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm">
          <p className="font-medium">{t('settings.themeInvalid')}</p>
          <ul className="mt-1 list-disc ps-5 text-xs">
            {errors.map((e) => (
              <li key={e}>{e}</li>
            ))}
          </ul>
        </div>
      ) : null}
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={t('settings.removeTheme', { name: removing?.name })}
        description={t('settings.removeThemeBody')}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={() => removing && void remove(removing)}
      />
    </div>
  )
}
