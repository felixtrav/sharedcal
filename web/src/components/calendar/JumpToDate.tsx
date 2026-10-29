import { CalendarDaysIcon, ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { CalendarView } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Tooltip } from '@/components/ui/tooltip'
import { addDays, dateInZone } from '@/lib/time'
import { cn } from '@/lib/utils'

const pad = (n: number) => String(n).padStart(2, '0')
const names = (opts: Intl.DateTimeFormatOptions) => new Intl.DateTimeFormat(undefined, { timeZone: 'UTC', ...opts })

/** The 6×7 grid of dates for a month, starting on `weekStart` (0 = Sunday). */
function monthGrid(year: number, month: number, weekStart: number) {
  const first = new Date(Date.UTC(year, month - 1, 1))
  const lead = (first.getUTCDay() - weekStart + 7) % 7
  const start = addDays(`${year}-${pad(month)}-01`, -lead)
  return Array.from({ length: 42 }, (_, i) => addDays(start, i))
}

/**
 * Jumps the calendar to a date. The month view moves by whole months, so it offers months and years only;
 * every other view offers specific days.
 */
export function JumpToDate({
  view,
  date: current,
  timeZone,
  weekStart,
  onPick,
}: {
  view: CalendarView
  /** The date being viewed, when the calendar has reported one. */
  date?: string
  timeZone: string
  weekStart: number
  onPick: (date: string) => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [today, setToday] = useState('1970-01-01')
  const date = current ?? today
  const [shown, setShown] = useState(date)
  const byMonth = view === 'month'

  const [year, month] = shown.split('-').map(Number) as [number, number]
  const [curYear, curMonth] = date.split('-').map(Number) as [number, number]
  const move = (months: number) => {
    const t0 = new Date(Date.UTC(year, month - 1 + months, 1))
    setShown(`${t0.getUTCFullYear()}-${pad(t0.getUTCMonth() + 1)}-01`)
  }
  const pick = (d: string) => {
    setOpen(false)
    onPick(d)
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        // Start from the date being viewed each time the picker opens.
        if (o) {
          const now = dateInZone(Date.now(), timeZone)
          setToday(now)
          setShown(current ?? now)
        }
        setOpen(o)
      }}
    >
      <Tooltip content={t('calendar.pickDate')}>
        <PopoverTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={byMonth ? t('calendar.pickMonth') : t('calendar.pickDate')}>
            <CalendarDaysIcon aria-hidden />
          </Button>
        </PopoverTrigger>
      </Tooltip>
      <PopoverContent align="start" className="w-72 p-3" aria-label={byMonth ? t('calendar.pickMonth') : t('calendar.pickDate')}>
        <div className="mb-2 flex items-center justify-between">
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => move(byMonth ? -12 : -1)}
            aria-label={byMonth ? t('calendar.previousYear') : t('calendar.previousMonth')}
          >
            <ChevronLeftIcon className="rtl:rotate-180" aria-hidden />
          </Button>
          <p className="text-sm font-semibold tabular-nums" aria-live="polite">
            {byMonth ? year : names({ month: 'long', year: 'numeric' }).format(Date.UTC(year, month - 1, 1))}
          </p>
          <Button variant="ghost" size="icon-sm" onClick={() => move(byMonth ? 12 : 1)} aria-label={byMonth ? t('calendar.nextYear') : t('calendar.nextMonth')}>
            <ChevronRightIcon className="rtl:rotate-180" aria-hidden />
          </Button>
        </div>

        {byMonth ? (
          <div className="grid grid-cols-3 gap-1.5" role="group" aria-label={String(year)}>
            {Array.from({ length: 12 }, (_, i) => {
              const selected = year === curYear && i + 1 === curMonth
              return (
                <Button key={i} variant={selected ? 'default' : 'ghost'} size="sm" aria-pressed={selected} onClick={() => pick(`${year}-${pad(i + 1)}-01`)}>
                  {names({ month: 'short' }).format(Date.UTC(year, i, 1))}
                </Button>
              )
            })}
          </div>
        ) : (
          <div role="grid" aria-label={names({ month: 'long', year: 'numeric' }).format(Date.UTC(year, month - 1, 1))}>
            <div role="row" className="mb-1 grid grid-cols-7 text-center text-xs text-muted-foreground">
              {Array.from({ length: 7 }, (_, i) => (
                <span key={i} role="columnheader" aria-label={names({ weekday: 'long' }).format(Date.UTC(2023, 0, 1 + ((weekStart + i) % 7)))}>
                  {names({ weekday: 'narrow' }).format(Date.UTC(2023, 0, 1 + ((weekStart + i) % 7)))}
                </span>
              ))}
            </div>
            <div className="grid grid-cols-7 gap-y-0.5">
              {monthGrid(year, month, weekStart).map((d) => {
                const inMonth = Number(d.slice(5, 7)) === month
                const selected = d === date
                return (
                  <button
                    key={d}
                    type="button"
                    role="gridcell"
                    aria-selected={selected}
                    aria-current={d === today ? 'date' : undefined}
                    aria-label={names({ dateStyle: 'full' }).format(Date.parse(`${d}T00:00:00Z`))}
                    onClick={() => pick(d)}
                    className={cn(
                      'mx-auto flex size-8 items-center justify-center rounded-full text-sm tabular-nums outline-none transition-colors hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50',
                      !inMonth && 'text-muted-foreground/60',
                      d === today && !selected && 'font-semibold text-primary',
                      selected && 'bg-primary text-primary-foreground hover:bg-primary',
                    )}
                  >
                    {Number(d.slice(8))}
                  </button>
                )
              })}
            </div>
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}
