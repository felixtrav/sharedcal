import { fireEvent, render } from '@testing-library/react'
import { useRef } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useSwipe } from './useSwipe'

function Harness({ onSwipe, rtl }: { onSwipe: (d: 1 | -1) => void; rtl?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  useSwipe(ref, onSwipe, rtl)
  return (
    <div ref={ref} data-testid="area">
      <span className="fc-event">chip</span>
    </div>
  )
}

const touch = (el: Element, x0: number, y0: number, x1: number, y1: number) => {
  fireEvent.touchStart(el, { touches: [{ clientX: x0, clientY: y0 }] })
  fireEvent.touchEnd(el, { changedTouches: [{ clientX: x1, clientY: y1 }] })
}

describe('useSwipe', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('maps touch swipes to next and previous, ignoring short, vertical and chip swipes', () => {
    const fn = vi.fn()
    const { getByTestId, getByText } = render(<Harness onSwipe={fn} />)
    const area = getByTestId('area')
    touch(area, 200, 100, 100, 100)
    expect(fn).toHaveBeenLastCalledWith(1)
    touch(area, 100, 100, 200, 100)
    expect(fn).toHaveBeenLastCalledWith(-1)
    touch(area, 200, 100, 170, 100)
    touch(area, 200, 100, 100, 200)
    touch(getByText('chip'), 200, 100, 100, 100)
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('flips directions for right-to-left layouts', () => {
    const fn = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={fn} rtl />)
    touch(getByTestId('area'), 200, 100, 100, 100)
    expect(fn).toHaveBeenLastCalledWith(-1)
  })

  it('fires once per trackpad swipe, however long the momentum lasts', () => {
    const fn = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={fn} />)
    const area = getByTestId('area')
    for (let i = 0; i < 10; i++) fireEvent.wheel(area, { deltaX: 40, deltaY: 2 })
    expect(fn).toHaveBeenCalledTimes(1)
    expect(fn).toHaveBeenLastCalledWith(1)
    vi.advanceTimersByTime(300)
    for (let i = 0; i < 10; i++) fireEvent.wheel(area, { deltaX: -40, deltaY: 2 })
    expect(fn).toHaveBeenCalledTimes(2)
    expect(fn).toHaveBeenLastCalledWith(-1)
  })

  it('leaves vertical scrolling alone', () => {
    const fn = vi.fn()
    const { getByTestId } = render(<Harness onSwipe={fn} />)
    for (let i = 0; i < 10; i++) fireEvent.wheel(getByTestId('area'), { deltaX: 30, deltaY: 60 })
    expect(fn).not.toHaveBeenCalled()
  })
})
