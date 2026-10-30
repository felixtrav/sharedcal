import { useEffect, useMemo } from 'react'
import { useMe, useThemes } from '@/api/queries'
import { useMediaQuery } from '@/lib/useMediaQuery'
import { BUILTIN_THEMES, compileTheme } from './compile'
import { applyTheme, DARK_SCHEME, persistTheme } from './runtime'

/** Applies the user's theme, mode and the admin accent to <html>. Renders nothing. */
export function ThemeController() {
  const { data: me } = useMe()
  const { data: themesData } = useThemes()
  // Re-applies the theme when the OS switches between light and dark.
  const systemDark = useMediaQuery(DARK_SCHEME)

  const themeId = me?.preferences.themeId || 'default'
  const mode = me?.preferences.colorMode ?? 'system'
  const accent = me?.instance.accentColor || undefined

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

  return null
}
