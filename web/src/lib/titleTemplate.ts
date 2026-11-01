import type { EventFieldValues, EventType } from '@/api/types'

/** `{{key}}`, or `{{key:first}}` on a people field to show first names only. */
const PLACEHOLDER = /\{\{\s*([a-z][a-z0-9_]*)(?::(first))?\s*\}\}/g

/** Kinds whose values read naturally inside a title. Mirrors `titleFieldKind` in internal/domain/titletemplate.go. */
export const TITLE_FIELD_KINDS = ['text', 'number', 'time', 'select', 'members'] as const

/** A person's current name (or first name only) for their id, or undefined when unknown. */
export type NameOf = (id: string, firstOnly?: boolean) => string | undefined

/** The first word of a display name; names that are really email addresses stay whole. */
export function firstNameOf(name: string): string {
  if (name.includes('@')) return name
  return name.trim().split(/\s+/)[0] ?? name
}

function valueText(v: EventFieldValues[string] | undefined, kind: string | undefined, nameOf?: NameOf, firstOnly = false): string {
  if (v === undefined || typeof v === 'boolean') return ''
  if (!Array.isArray(v)) return String(v).trim()
  // People are stored by id and shown by their current name; unknown ids are left out.
  return (kind === 'members' ? v.map((id) => nameOf?.(id, firstOnly)).filter(Boolean) : v).join(', ')
}

const SECTION_TAG = /\{\{([#/])([a-z][a-z0-9_]*)\}\}/g

/**
 * Keeps the parts of a template whose field has a value and drops the rest, tags included.
 * Mirrors `applySections` in internal/domain/titletemplate.go; unbalanced tags return the template unchanged.
 */
export function applySections(template: string, has: (key: string) => boolean): string {
  let out = ''
  const open: { key: string; show: boolean }[] = []
  let hidden = 0
  let last = 0
  for (const m of template.matchAll(SECTION_TAG)) {
    const at = m.index ?? 0
    if (hidden === 0) out += template.slice(last, at)
    last = at + m[0].length
    const key = m[2] ?? ''
    if (m[1] === '#') {
      const show = has(key)
      open.push({ key, show })
      if (!show) hidden++
      continue
    }
    const top = open.pop()
    if (!top || top.key !== key) return template
    if (!top.show) hidden--
  }
  if (open.length) return template
  return hidden === 0 ? out + template.slice(last) : out
}

/**
 * The title a type's template produces for these field values: optional parts appear only when their field has a
 * value, missing values are empty, spaces collapse, and an empty result falls back to the type's name. Mirrors
 * `renderTitle` in internal/domain/titletemplate.go.
 */
export function renderTitle(
  type: Pick<EventType, 'name' | 'titleTemplate' | 'fields'>,
  values: EventFieldValues,
  nameOf?: NameOf,
  timeText?: (hhmm: string) => string,
): string {
  const text = (key: string, firstOnly = false) => {
    const kind = type.fields.find((f) => f.key === key)?.kind
    const s = valueText(values[key], kind, nameOf, firstOnly)
    return s && kind === 'time' && timeText ? timeText(s) : s
  }
  const out = applySections(type.titleTemplate, (key) => text(key) !== '')
    .replace(PLACEHOLDER, (_, key: string, first?: string) => text(key, !!first))
    .split(/\s+/)
    .filter(Boolean)
    .join(' ')
  return (out || type.name).slice(0, 200)
}
