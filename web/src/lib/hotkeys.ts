import { useEffect, useRef } from 'react'

/** True when the keyboard event comes from a text field or an editable/interactive widget. */
export function isTypingTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  const tag = target.tagName
  if (tag === 'TEXTAREA' || tag === 'SELECT') return true
  if (tag === 'INPUT') {
    const type = (target as HTMLInputElement).type
    return !['checkbox', 'radio', 'button', 'submit', 'reset', 'range', 'color'].includes(type)
  }
  return target.closest('[role="combobox"],[role="listbox"],[role="menu"],[role="dialog"][aria-modal="true"]') !== null
}

const ARROW_OWNERS = '[role="tablist"],[role="radiogroup"],[role="group"],[role="toolbar"],[role="menu"],[role="listbox"],[role="grid"],[role="slider"]'

export type HotkeyMap = Record<string, (e: KeyboardEvent) => void>

/**
 * Global single-key shortcuts. Ignored while typing, with modifiers held, or
 * when a modal dialog is open (except Escape, which dialogs handle themselves).
 */
export function useHotkeys(map: HotkeyMap, enabled = true) {
  const ref = useRef(map)
  useEffect(() => {
    ref.current = map
  })
  useEffect(() => {
    if (!enabled) return
    const onKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return
      if (isTypingTarget(e.target)) return
      if (document.querySelector('[role="dialog"][aria-modal="true"],[role="alertdialog"]')) return
      const key = e.key.length === 1 ? e.key.toLowerCase() : e.key
      // Arrow keys belong to composite widgets (tabs, segmented controls, menus) when focus is inside one.
      if (key.startsWith('Arrow') && e.target instanceof HTMLElement && e.target.closest(ARROW_OWNERS)) return
      const fn = ref.current[key]
      if (fn) {
        e.preventDefault()
        fn(e)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [enabled])
}
