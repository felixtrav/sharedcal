import type { ColorMode } from '@/api/types'
import { STORAGE_KEYS, writeLocal } from '@/lib/storage'
import type { CompiledTheme, CompiledVars, Mode } from './compile'

type StoredTheme = CompiledTheme & { mode: ColorMode }

export const DARK_SCHEME = '(prefers-color-scheme: dark)'

function systemPrefersDark() {
  return typeof window !== 'undefined' && window.matchMedia?.(DARK_SCHEME).matches === true
}

function resolveMode(mode: ColorMode): Mode {
  if (mode === 'system') return systemPrefersDark() ? 'dark' : 'light'
  return mode
}

function setVars(root: HTMLElement, vars: CompiledVars) {
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v)
}

/** Apply a compiled theme to <html>: CSS variables, `.dark`, data-theme, color-scheme. */
export function applyTheme(theme: CompiledTheme, mode: ColorMode, root: HTMLElement = document.documentElement) {
  const resolved = resolveMode(mode)
  setVars(root, theme[resolved])
  root.classList.toggle('dark', resolved === 'dark')
  root.dataset.theme = theme.id
  root.dataset.mode = mode
  root.style.colorScheme = resolved
}

/** Mirror to localStorage so public/theme-init.js paints the next load correctly. */
export function persistTheme(theme: CompiledTheme, mode: ColorMode) {
  const stored: StoredTheme = { ...theme, mode }
  writeLocal(STORAGE_KEYS.theme, stored)
}
