import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { CalendarView } from '@/api/types'
import { TooltipProvider } from '@/components/ui/tooltip'
import { JumpToDate } from './JumpToDate'

function open(view: CalendarView, date = '2026-10-29') {
  const onPick = vi.fn()
  render(
    <TooltipProvider>
      <JumpToDate view={view} date={date} timeZone="UTC" weekStart={1} onPick={onPick} />
    </TooltipProvider>,
  )
  // Without i18n loaded, labels are their keys.
  fireEvent.click(screen.getByRole('button', { name: view === 'month' ? 'calendar.pickMonth' : 'calendar.pickDate' }))
  return onPick
}

describe('JumpToDate', () => {
  it('offers only months and years in the month view', () => {
    const onPick = open('month')
    expect(screen.queryByRole('gridcell')).toBeNull()
    const months = screen.getByRole('group', { name: '2026' })
    expect(within(months).getAllByRole('button')).toHaveLength(12)
    fireEvent.click(screen.getByRole('button', { name: 'calendar.nextYear' }))
    fireEvent.click(within(screen.getByRole('group', { name: '2027' })).getAllByRole('button')[2]!)
    expect(onPick).toHaveBeenCalledWith('2027-03-01')
  })

  it.each(['week', 'day', 'agenda'] as const)('offers specific days in the %s view', (view) => {
    const onPick = open(view)
    const cells = screen.getAllByRole('gridcell')
    expect(cells).toHaveLength(42)
    expect(screen.getByRole('gridcell', { selected: true })).toHaveTextContent('29')
    fireEvent.click(screen.getByRole('button', { name: 'calendar.nextMonth' }))
    // November 2026 starts on a Sunday; with Monday first the grid begins on Oct 26.
    expect(screen.getAllByRole('gridcell')[0]).toHaveTextContent('26')
    fireEvent.click(screen.getAllByRole('gridcell').find((c) => c.textContent === '15')!)
    expect(onPick).toHaveBeenCalledWith('2026-11-15')
  })
})
