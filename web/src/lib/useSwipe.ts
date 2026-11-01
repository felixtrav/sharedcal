import { useEffect, useRef, type RefObject } from 'react'

/** Horizontal travel, in px, that counts as a swipe. */
const TOUCH_DISTANCE = 60
const WHEEL_DISTANCE = 100
/** A pause in wheel events this long ends a trackpad swipe. */
const WHEEL_QUIET_MS = 100
/** The shortest time between two trackpad page changes. */
const WHEEL_MIN_GAP_MS = 350

/** Elements that handle their own horizontal drags. */
const IGNORED = '.fc-event, .fc-event-mirror, .fc-highlight, input, textarea, select, [role="slider"]'

/**
 * Calls `onSwipe(1)` for a swipe toward the left (the next page) and `onSwipe(-1)` toward the right,
 * for touch screens and for two-finger trackpad scrolling. `rtl` flips the directions.
 */
export function useSwipe(ref: RefObject<HTMLElement | null>, onSwipe: (dir: 1 | -1) => void, rtl = false) {
  const cb = useRef(onSwipe)
  useEffect(() => {
    cb.current = onSwipe
  })

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const fire = (dx: number) => cb.current((dx > 0 !== rtl ? 1 : -1) as 1 | -1)

    let start: { x: number; y: number } | null = null
    const onTouchStart = (e: TouchEvent) => {
      const t = e.touches[0]
      start = e.touches.length === 1 && t && !(e.target as Element).closest?.(IGNORED) ? { x: t.clientX, y: t.clientY } : null
    }
    const onTouchEnd = (e: TouchEvent) => {
      const t = e.changedTouches[0]
      const s = start
      start = null
      if (!s || !t) return
      const dx = s.x - t.clientX
      const dy = s.y - t.clientY
      // Mostly horizontal, and not the end of a long-press drag selecting a time range.
      if (Math.abs(dx) < TOUCH_DISTANCE || Math.abs(dx) < Math.abs(dy) * 1.5 || el.querySelector('.fc-event-mirror, .fc-highlight')) return
      fire(dx)
    }

    // Trackpads keep sending decaying "momentum" wheel events after the fingers lift, so one physical swipe
    // is a stream of events. Fire once per stream. A new swipe is told apart by a pause, a change of
    // direction, or speed picking up again after the stream had died down, and never within
    // WHEEL_MIN_GAP_MS of the last one, so noise in the stream can't skip several periods.
    let acc = 0
    let spent = false
    let lastAt = 0
    let firedAt = 0
    let lowest = Infinity
    let lastSign = 0
    const onWheel = (e: WheelEvent) => {
      if (e.ctrlKey) return
      const abs = Math.abs(e.deltaX)
      if (abs < Math.abs(e.deltaY)) {
        if (!spent) acc = 0
        return
      }
      if (abs === 0) return
      // Stop the browser reading the swipe as its history gesture (back/forward).
      if (e.cancelable) e.preventDefault()
      const now = Date.now()
      const sign = Math.sign(e.deltaX)
      if (spent) {
        lowest = Math.min(lowest, abs)
        const fresh = now - lastAt > WHEEL_QUIET_MS || sign !== lastSign || abs > lowest * 2 + 8
        if (fresh && now - firedAt > WHEEL_MIN_GAP_MS) {
          acc = 0
          spent = false
        }
      } else if (now - lastAt > WHEEL_QUIET_MS || sign !== lastSign) {
        acc = 0
      }
      lastAt = now
      lastSign = sign
      if (spent) return
      acc += e.deltaX
      if (Math.abs(acc) < WHEEL_DISTANCE) return
      spent = true
      firedAt = now
      lowest = Infinity
      fire(acc)
    }

    const cancel = () => {
      start = null
    }
    el.addEventListener('touchstart', onTouchStart, { passive: true })
    el.addEventListener('touchend', onTouchEnd, { passive: true })
    el.addEventListener('touchcancel', cancel, { passive: true })
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => {
      el.removeEventListener('touchstart', onTouchStart)
      el.removeEventListener('touchend', onTouchEnd)
      el.removeEventListener('touchcancel', cancel)
      el.removeEventListener('wheel', onWheel)
    }
  }, [ref, rtl])
}
