import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import en from './en.json'

function walk(dir: string, out: string[] = []) {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p, out)
    else if (/\.(ts|tsx)$/.test(name) && !/\.test\./.test(name)) out.push(p)
  }
  return out
}

function has(key: string) {
  const lookup = (k: string) =>
    k.split('.').reduce<unknown>((node, part) => (node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined), en)
  return typeof lookup(key) === 'string' || typeof lookup(`${key}_other`) === 'string'
}

describe('i18n', () => {
  const files = walk(join(__dirname, '..'))
  const used = new Set<string>()
  for (const f of files) {
    const src = readFileSync(f, 'utf8')
    for (const m of src.matchAll(/\bt\(\s*'([a-zA-Z0-9_.-]+)'/g)) used.add(m[1] as string)
    for (const m of src.matchAll(/labelKey: '([a-zA-Z.]+)'/g)) used.add(m[1] as string)
    for (const m of src.matchAll(/'(validation\.[a-zA-Z]+)'/g)) used.add(m[1] as string)
  }

  it('finds the keys used in source', () => {
    expect(used.size).toBeGreaterThan(300)
  })

  it.each([...used].sort())('has a translation for %s', (key) => {
    expect(has(key)).toBe(true)
  })

  it('covers dynamic key families', () => {
    const families: Record<string, string[]> = {
      'calendar.views': ['month', 'week', 'day', 'agenda'],
      roles: ['viewer', 'writer', 'admin'],
      'event.status': ['confirmed', 'tentative'],
      'suggestions.status': ['open', 'accepted', 'partially_accepted', 'rejected', 'withdrawn', 'closed'],
      'recurrence.every': ['daily', 'weekly', 'monthly', 'yearly'],
      'recurrence.units': ['daily', 'weekly', 'monthly', 'yearly'],
      'recurrence.nth': ['1', '2', '3', '4', '-1'],
      'inbox.kinds': ['suggestion_created', 'suggestion_resolved', 'comment_reply', 'comment_mention'],
      'activity.subjects': ['event', 'comment', 'suggestion', 'attachment', 'tag', 'user', 'settings', 'theme'],
      'event.fields': ['title', 'description', 'location', 'start', 'end', 'timeZone', 'allDay', 'rrule', 'status', 'tags'],
    }
    for (const [prefix, names] of Object.entries(families)) for (const n of names) expect(has(`${prefix}.${n}`), `${prefix}.${n}`).toBe(true)
  })
})
