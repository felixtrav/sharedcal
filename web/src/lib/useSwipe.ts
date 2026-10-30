import { useEffect, useRef, type RefObject } from 'react'

/** Horizontal travel, in px, that counts as a swipe. */
const TOUCH_DISTANCE = 60
const WHEEL_DISTANCE = 60
/** A pause in wheel events this long ends a trackpad swipe. */
const WHEEL_QUIET_MS = 100

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
    // is a stream of events. Fire once per stream, and treat a direction change, a rise in speed or a pause
    // as the start of the next swipe so quick repeated swipes all register.
    let acc = 0
    let spent = false
    let lastAt = 0
    let lastAbs = 0
    let lastSign = 0
    const onWheel = (e: WheelEvent) => {
      if (e.ctrlKey) return
      const abs = Math.abs(e.deltaX)
      if (abs < Math.abs(e.deltaY)) {
        acc = 0
        return
      }
      if (abs === 0) return
      const now = Date.now()
      const sign = Math.sign(e.deltaX)
      if (now - lastAt > WHEEL_QUIET_MS || sign !== lastSign || abs > lastAbs * 1.3 + 1) {
        acc = 0
        spent = false
      }
      lastAt = now
      lastAbs = abs
      lastSign = sign
      if (spent) return
      acc += e.deltaX
      if (Math.abs(acc) < WHEEL_DISTANCE) return
      spent = true
      fire(acc)
    }

    const cancel = () => {
      start = null
    }
    el.addEventListener('touchstart', onTouchStart, { passive: true })
    el.addEventListener('touchend', onTouchEnd, { passive: true })
    el.addEventListener('touchcancel', cancel, { passive: true })
    el.addEventListener('wheel', onWheel, { passive: true })
    return () => {
      el.removeEventListener('touchstart', onTouchStart)
      el.removeEventListener('touchend', onTouchEnd)
      el.removeEventListener('touchcancel', cancel)
      el.removeEventListener('wheel', onWheel)
    }
  }, [ref, rtl])
}
