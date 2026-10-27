import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import { compileTheme } from './compile'
import defaultTheme from './default.json'

/** index.css carries pre-JS fallbacks; they must match the default theme. */
describe('index.css fallbacks', () => {
  const css = readFileSync(join(__dirname, '..', 'index.css'), 'utf8')
  const block = (selector: string) => {
    const m = new RegExp(`(?:^|\\n)${selector.replace('.', '\\.')} \\{([^}]*)\\}`).exec(css)
    const out: Record<string, string> = {}
    for (const line of (m?.[1] ?? '').split(';')) {
      const kv = /^\s*(--[a-z0-9-]+):\s*(#[0-9a-f]{3,6})\s*$/i.exec(line)
      if (kv) out[kv[1] as string] = (kv[2] as string).toLowerCase()
    }
    return out
  }
  const compiled = compileTheme({ ...defaultTheme, modes: defaultTheme.modes })

  it.each([
    [':root', 'light'],
    ['.dark', 'dark'],
  ] as const)('%s matches default.json %s', (selector, mode) => {
    const vars = block(selector)
    expect(Object.keys(vars).length).toBeGreaterThan(10)
    for (const [k, v] of Object.entries(vars)) expect([k, compiled[mode][k]?.toLowerCase()]).toEqual([k, v])
  })
})
