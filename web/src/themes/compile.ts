import type { Theme, ThemeTokens } from '@/api/types'
import { bestContrast, contrastRatio, mix, parseHex } from './contrast'
import defaultTheme from './default.json'
import nordTheme from './nord.json'

export const REQUIRED_KEYS = ['background', 'foreground', 'primary', 'muted', 'border'] as const

export const ALLOWED_KEYS = [
  ...REQUIRED_KEYS,
  'primary-foreground',
  'muted-foreground',
  'card',
  'card-foreground',
  'popover',
  'popover-foreground',
  'secondary',
  'secondary-foreground',
  'accent',
  'accent-foreground',
  'destructive',
  'destructive-foreground',
  'ring',
  'input',
  'radius',
  'font',
  'tag-1',
  'tag-2',
  'tag-3',
  'tag-4',
  'tag-5',
  'tag-6',
  'tag-7',
  'tag-8',
] as const

export const FONTS = ['Inter', 'Geist', 'system-ui'] as const

const FONT_STACKS: Record<(typeof FONTS)[number], string> = {
  Inter: "'Inter Variable', Inter, ui-sans-serif, system-ui, sans-serif",
  Geist: "Geist, 'Geist Variable', 'Inter Variable', ui-sans-serif, system-ui, sans-serif",
  'system-ui': 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
}

export const MIN_CONTRAST = 4.5

const RADIUS_RE = /^(0|\d+(\.\d+)?(rem|px|em))$/

/** Built-in themes shipped with the app (the server serves the same files). */
export const BUILTIN_THEMES: Theme[] = [defaultTheme, nordTheme].map((t) => ({ ...t, builtin: true }))

export type Mode = 'light' | 'dark'

/** Validate an uploaded theme with the same rules the server applies. Returns error messages. */
export function validateTheme(input: unknown): string[] {
  const errors: string[] = []
  if (!input || typeof input !== 'object') return ['Theme must be a JSON object.']
  const t = input as Record<string, unknown>
  for (const k of ['id', 'name', 'author'] as const) {
    if (typeof t[k] !== 'string' || !(t[k] as string).trim()) errors.push(`"${k}" must be a non-empty string.`)
  }
  if (typeof t.id === 'string' && !/^[a-z0-9][a-z0-9-]{0,63}$/.test(t.id)) {
    errors.push('"id" may only contain lowercase letters, digits and dashes.')
  }
  const modes = t.modes as Record<string, unknown> | undefined
  if (!modes || typeof modes !== 'object') return [...errors, '"modes" must contain "light" and "dark".']
  for (const mode of ['light', 'dark'] as const) {
    const tokens = modes[mode] as Record<string, unknown> | undefined
    if (!tokens || typeof tokens !== 'object') {
      errors.push(`"modes.${mode}" is missing.`)
      continue
    }
    for (const req of REQUIRED_KEYS) if (!(req in tokens)) errors.push(`${mode}: "${req}" is required.`)
    for (const [k, v] of Object.entries(tokens)) {
      if (!(ALLOWED_KEYS as readonly string[]).includes(k)) {
        errors.push(`${mode}: "${k}" is not an allowed token.`)
        continue
      }
      if (typeof v !== 'string') {
        errors.push(`${mode}: "${k}" must be a string.`)
        continue
      }
      if (k === 'radius') {
        if (!RADIUS_RE.test(v)) errors.push(`${mode}: "radius" must be a length like 0.5rem or 6px.`)
      } else if (k === 'font') {
        if (!(FONTS as readonly string[]).includes(v)) errors.push(`${mode}: "font" must be one of ${FONTS.join(', ')}.`)
      } else if (!parseHex(v)) {
        errors.push(`${mode}: "${k}" must be a #rgb or #rrggbb color.`)
      }
    }
    if (errors.length === 0) {
      const fg = tokens.foreground as string
      const bg = tokens.background as string
      if (contrastRatio(fg, bg) < MIN_CONTRAST) errors.push(`${mode}: foreground/background contrast is below 4.5:1.`)
    }
  }
  return errors
}

const WHITE = '#ffffff'
const INK = '#111114'

/** First preferred color that reaches AA on `bg`, else whichever of white/ink is stronger. */
export function readableOn(bg: string, preferred: string[] = []) {
  for (const p of preferred) if (contrastRatio(bg, p) >= MIN_CONTRAST) return p
  return bestContrast(bg, [WHITE, INK])
}

function deriveMutedForeground(fg: string, bg: string, muted: string) {
  for (let w = 0.4; w >= 0; w -= 0.05) {
    const c = mix(fg, bg, w)
    if (contrastRatio(c, bg) >= MIN_CONTRAST && contrastRatio(c, muted) >= MIN_CONTRAST) return c
  }
  return fg
}

const FALLBACK_TAGS: Record<Mode, ThemeTokens> = {
  light: defaultTheme.modes.light,
  dark: defaultTheme.modes.dark,
}

export type CompiledVars = Record<string, string>

/**
 * Compile one mode's tokens into CSS custom properties. Optional tokens are
 * derived from the required ones; `--tag-N-foreground` is computed for
 * readable text on tag-colored events. `accent` (admin branding) overrides
 * the primary color.
 */
export function compileMode(tokens: ThemeTokens, mode: Mode, accent?: string): CompiledVars {
  const tk = (k: string) => tokens[k]
  const background = tk('background') ?? (mode === 'dark' ? '#0b0b0f' : WHITE)
  const foreground = tk('foreground') ?? (mode === 'dark' ? '#fafafa' : '#18181b')
  const muted = tk('muted') ?? mix(background, foreground, 0.05)
  const border = tk('border') ?? mix(background, foreground, 0.12)
  const accentOverride = accent && parseHex(accent) ? accent : undefined
  const primary = accentOverride ?? tk('primary') ?? foreground
  const card = tk('card') ?? background
  const popover = tk('popover') ?? card
  const secondary = tk('secondary') ?? muted
  const accentBg = tk('accent') ?? muted
  const destructive = tk('destructive') ?? (mode === 'dark' ? '#f87171' : '#dc2626')

  const vars: CompiledVars = {
    '--background': background,
    '--foreground': foreground,
    '--card': card,
    '--card-foreground': tk('card-foreground') ?? foreground,
    '--popover': popover,
    '--popover-foreground': tk('popover-foreground') ?? foreground,
    '--primary': primary,
    '--primary-foreground':
      (!accentOverride && tk('primary-foreground')) || readableOn(primary, [background, foreground]),
    '--secondary': secondary,
    '--secondary-foreground': tk('secondary-foreground') ?? foreground,
    '--muted': muted,
    '--muted-foreground': tk('muted-foreground') ?? deriveMutedForeground(foreground, background, muted),
    '--accent': accentBg,
    '--accent-foreground': tk('accent-foreground') ?? foreground,
    '--destructive': destructive,
    '--destructive-foreground': tk('destructive-foreground') ?? readableOn(destructive, [WHITE, background, foreground]),
    '--border': border,
    '--input': tk('input') ?? border,
    '--ring': accentOverride ?? tk('ring') ?? primary,
    '--radius': tk('radius') ?? '0.625rem',
    '--font-sans': FONT_STACKS[(tk('font') as (typeof FONTS)[number]) ?? 'Inter'] ?? FONT_STACKS.Inter,
  }
  vars['--chart-1'] = vars['--primary'] as string
  vars['--chart-3'] = vars['--muted-foreground'] as string
  for (let i = 1; i <= 8; i++) {
    const key = `tag-${i}`
    const color = tk(key) ?? FALLBACK_TAGS[mode][key] ?? primary
    vars[`--${key}`] = color
    vars[`--${key}-foreground`] = readableOn(color, [WHITE, INK])
  }
  return vars
}

export type CompiledTheme = { id: string; light: CompiledVars; dark: CompiledVars }

export function compileTheme(theme: Pick<Theme, 'id' | 'modes'>, accent?: string): CompiledTheme {
  return {
    id: theme.id,
    light: compileMode(theme.modes.light, 'light', accent),
    dark: compileMode(theme.modes.dark, 'dark', accent),
  }
}

/** Pairs that must reach AA, as [label, foreground var, background var]. */
export const CONTRAST_PAIRS: [string, string, string][] = [
  ['foreground/background', '--foreground', '--background'],
  ['card', '--card-foreground', '--card'],
  ['popover', '--popover-foreground', '--popover'],
  ['primary', '--primary-foreground', '--primary'],
  ['secondary', '--secondary-foreground', '--secondary'],
  ['muted-foreground/background', '--muted-foreground', '--background'],
  ['muted-foreground/muted', '--muted-foreground', '--muted'],
  ['accent', '--accent-foreground', '--accent'],
  ['destructive', '--destructive-foreground', '--destructive'],
  ...Array.from({ length: 8 }, (_, i): [string, string, string] => [
    `tag-${i + 1}`,
    `--tag-${i + 1}-foreground`,
    `--tag-${i + 1}`,
  ]),
]

export function contrastReport(vars: CompiledVars) {
  return CONTRAST_PAIRS.map(([label, fg, bg]) => ({
    label,
    ratio: contrastRatio(vars[fg] as string, vars[bg] as string),
  }))
}
