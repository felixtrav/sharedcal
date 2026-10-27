import { fireEvent, render } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { useHotkeys } from './hotkeys'

function Harness({ map }: { map: Record<string, () => void> }) {
  useHotkeys(map)
  return (
    <div>
      <input aria-label="text" />
      <div role="group" aria-label="seg">
        <button type="button">A</button>
      </div>
      <button type="button">plain</button>
    </div>
  )
}

describe('useHotkeys', () => {
  it('fires single-key shortcuts and ignores typing, modifiers and composite widgets', () => {
    const w = vi.fn()
    const left = vi.fn()
    const { getByLabelText, getByText } = render(<Harness map={{ w, ArrowLeft: left }} />)
    fireEvent.keyDown(getByText('plain'), { key: 'w' })
    expect(w).toHaveBeenCalledTimes(1)
    fireEvent.keyDown(getByText('plain'), { key: 'W' })
    expect(w).toHaveBeenCalledTimes(2)
    fireEvent.keyDown(getByLabelText('text'), { key: 'w' })
    fireEvent.keyDown(getByText('plain'), { key: 'w', ctrlKey: true })
    expect(w).toHaveBeenCalledTimes(2)
    fireEvent.keyDown(getByText('A'), { key: 'ArrowLeft' })
    expect(left).not.toHaveBeenCalled()
    fireEvent.keyDown(getByText('plain'), { key: 'ArrowLeft' })
    expect(left).toHaveBeenCalledTimes(1)
  })
})
