import { useEffect, useMemo, useSyncExternalStore } from 'react'
import { useMe, useThemes } from '@/api/queries'
import { BUILTIN_THEMES, compileTheme } from './compile'
import { applyTheme, persistTheme } from './runtime'

function subscribeScheme(cb: () => void) {
  const mq = window.matchMedia?.('(prefers-color-scheme: dark)')
  mq?.addEventListener('change', cb)
  return () => mq?.removeEventListener('change', cb)
}
const getScheme = () => window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false

/** Applies the user's theme, mode and the admin accent to <html>. Renders nothing. */
export function ThemeController() {
  const { data: me } = useMe()
  const { data: themesData } = useThemes()
  const systemDark = useSyncExternalStore(subscribeScheme, getScheme, () => false)

  const themeId = me?.preferences.themeId || 'default'
  const mode = me?.preferences.colorMode ?? 'system'
  const accent = me?.instance.accentColor || undefined
  const density = me?.preferences.density ?? 'comfortable'

  const compiled = useMemo(() => {
    const list = themesData?.themes ?? BUILTIN_THEMES
    const theme = list.find((t) => t.id === themeId) ?? BUILTIN_THEMES.find((t) => t.id === themeId) ?? BUILTIN_THEMES[0]
    return theme ? compileTheme(theme, accent) : null
  }, [themesData, themeId, accent])

  useEffect(() => {
    if (!compiled || !me) return
    applyTheme(compiled, mode)
    persistTheme(compiled, mode)
  }, [compiled, mode, systemDark, me])

  useEffect(() => {
    document.documentElement.dataset.density = density
  }, [density])

  return null
}
