import { useQueryClient } from '@tanstack/react-query'
import { CheckIcon } from 'lucide-react'
import { useRef, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useMeData, useThemes } from '@/api/queries'
import type { CalendarView, ColorMode, Me, Preferences, Theme } from '@/api/types'
import { TimeZoneSelect } from '@/components/event/TimeZoneSelect'
import { Page, PageHeader } from '@/components/layout/PageHeader'
import { NativeSelect } from '@/components/ui/input'
import { Segmented } from '@/components/ui/segmented'
import { toast } from '@/components/ui/toast'
import { BUILTIN_THEMES, compileMode } from '@/themes/compile'
import { errorMessage } from '@/lib/errors'
import { browserTimeZone } from '@/lib/time'
import { cn } from '@/lib/utils'
import { weekdayName } from '@/components/event/recurrenceText'
import { WEEKDAYS } from '@/lib/recurrence'

function Section({ title, description, children, id }: { title: string; description?: string; children: ReactNode; id: string }) {
  return (
    <section aria-labelledby={id} className="grid gap-4 border-b border-border py-6 first:pt-0 last:border-0 sm:grid-cols-[minmax(0,14rem)_1fr]">
      <div>
        <h2 id={id} className="text-sm font-semibold">
          {title}
        </h2>
        {description ? <p className="mt-1 text-sm text-muted-foreground">{description}</p> : null}
      </div>
      <div className="grid gap-4">{children}</div>
    </section>
  )
}

export function PreferencesPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const me = useMeData()
  const prefs = me.preferences
  const { data: themesData } = useThemes()
  const themes = themesData?.themes ?? BUILTIN_THEMES

  // Saves go out one at a time, each sending the latest cached preferences, so
  // quick successive changes can't overwrite one another.
  const queue = useRef<Promise<void>>(Promise.resolve())
  const pending = useRef(0)
  const save = (patch: Partial<Preferences>) => {
    qc.setQueryData<Me>(keys.me, (m) => (m ? { ...m, preferences: { ...m.preferences, ...patch } } : m))
    pending.current++
    queue.current = queue.current.then(async () => {
      try {
        const current = qc.getQueryData<Me>(keys.me)?.preferences
        if (!current) return
        const saved = await endpoints.savePreferences(current)
        if (pending.current === 1) qc.setQueryData<Me>(keys.me, (m) => (m ? { ...m, preferences: saved } : m))
        if ('timeZone' in patch || 'weekStart' in patch) void qc.invalidateQueries({ queryKey: keys.eventsAll })
      } catch (err) {
        void qc.invalidateQueries({ queryKey: keys.me })
        toast.error(t('preferences.saveFailed'), errorMessage(err, t))
      } finally {
        pending.current--
      }
    })
    return queue.current
  }

  return (
    <Page>
      <PageHeader title={t('preferences.title')} description={t('preferences.description')} />

      <Section id="pref-appearance" title={t('preferences.appearance')} description={t('preferences.appearanceHint')}>
        <div className="grid gap-2">
          <span className="text-sm font-medium" id="pref-mode-label">
            {t('preferences.mode')}
          </span>
          <Segmented<ColorMode>
            label={t('preferences.mode')}
            value={prefs.colorMode}
            onChange={(v) => void save({ colorMode: v })}
            options={[
              { value: 'light', label: t('preferences.modes.light') },
              { value: 'dark', label: t('preferences.modes.dark') },
              { value: 'system', label: t('preferences.modes.system') },
            ]}
          />
        </div>
        <div className="grid gap-2">
          <span className="text-sm font-medium" id="pref-theme-label">
            {t('preferences.theme')}
          </span>
          <div role="radiogroup" aria-labelledby="pref-theme-label" className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {themes.map((th) => (
              <ThemeCard
                key={th.id}
                theme={th}
                selected={prefs.themeId === th.id || (!prefs.themeId && th.id === 'default')}
                onSelect={() => void save({ themeId: th.id })}
              />
            ))}
          </div>
        </div>
        <div className="grid gap-2">
          <span className="text-sm font-medium">{t('preferences.density')}</span>
          <Segmented<Preferences['density']>
            label={t('preferences.density')}
            value={prefs.density}
            onChange={(v) => void save({ density: v })}
            options={[
              { value: 'comfortable', label: t('preferences.densities.comfortable') },
              { value: 'compact', label: t('preferences.densities.compact') },
            ]}
          />
        </div>
      </Section>

      <Section id="pref-time" title={t('preferences.dateTime')}>
        <div className="grid gap-1.5">
          <label htmlFor="pref-tz" className="text-sm font-medium">
            {t('preferences.timeZone')}
          </label>
          <TimeZoneSelect
            id="pref-tz"
            value={prefs.timeZone}
            onChange={(tz) => void save({ timeZone: tz })}
            includeBrowserDefault
            browserDefaultLabel={t('preferences.browserTimeZone', { zone: browserTimeZone() })}
          />
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="pref-week" className="text-sm font-medium">
            {t('preferences.weekStart')}
          </label>
          <NativeSelect id="pref-week" value={prefs.weekStart} onChange={(e) => void save({ weekStart: Number(e.target.value) as Preferences['weekStart'] })}>
            {[0, 1, 2, 3, 4, 5, 6].map((d) => (
              <option key={d} value={d}>
                {weekdayName(WEEKDAYS[(d + 6) % 7] ?? 'MO')}
              </option>
            ))}
          </NativeSelect>
        </div>
        <div className="grid gap-2">
          <span className="text-sm font-medium">{t('preferences.clock')}</span>
          <Segmented<'12' | '24'>
            label={t('preferences.clock')}
            value={prefs.clock24h ? '24' : '12'}
            onChange={(v) => void save({ clock24h: v === '24' })}
            options={[
              { value: '12', label: t('preferences.clock12') },
              { value: '24', label: t('preferences.clock24') },
            ]}
          />
        </div>
        <div className="grid gap-1.5">
          <label htmlFor="pref-view" className="text-sm font-medium">
            {t('preferences.defaultView')}
          </label>
          <NativeSelect id="pref-view" value={prefs.defaultView} onChange={(e) => void save({ defaultView: e.target.value as CalendarView })}>
            {(['month', 'week', 'day', 'agenda'] as const).map((v) => (
              <option key={v} value={v}>
                {t(`calendar.views.${v}`)}
              </option>
            ))}
          </NativeSelect>
        </div>
      </Section>
    </Page>
  )
}

function ThemeCard({ theme, selected, onSelect }: { theme: Theme; selected: boolean; onSelect: () => void }) {
  const { t } = useTranslation()
  const light = compileMode(theme.modes.light, 'light')
  const dark = compileMode(theme.modes.dark, 'dark')
  const swatch = (vars: Record<string, string>) => (
    <div className="flex flex-1 flex-col gap-1.5 p-2.5" style={{ background: vars['--background'], color: vars['--foreground'] }}>
      <div className="h-2 w-12 rounded-full" style={{ background: vars['--foreground'] }} />
      <div className="flex gap-1">
        {[1, 2, 3, 4].map((i) => (
          <span key={i} className="h-3 flex-1 rounded-sm" style={{ background: vars[`--tag-${i}`] }} />
        ))}
      </div>
      <div className="h-3 w-8 rounded-sm" style={{ background: vars['--primary'] }} />
    </div>
  )
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        'overflow-hidden rounded-xl border text-start outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50',
        selected ? 'border-primary ring-1 ring-primary' : 'border-border hover:border-muted-foreground/50',
      )}
    >
      <div className="flex h-20 border-b border-border" aria-hidden>
        {swatch(light)}
        {swatch(dark)}
      </div>
      <div className="flex items-center gap-2 px-3 py-2">
        <div className="min-w-0 flex-1">
          <p className="truncate text-sm font-medium">{theme.name}</p>
          <p className="truncate text-xs text-muted-foreground">{t('preferences.themeBy', { author: theme.author })}</p>
        </div>
        {selected ? <CheckIcon className="size-4 text-primary" aria-hidden /> : null}
      </div>
    </button>
  )
}
