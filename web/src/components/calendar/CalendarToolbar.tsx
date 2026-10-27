import { ChevronLeftIcon, ChevronRightIcon, FilterIcon, PlusIcon, SearchIcon, XIcon } from 'lucide-react'
import { forwardRef, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useDirectory, useTags } from '@/api/queries'
import type { CalendarView } from '@/api/types'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Segmented } from '@/components/ui/segmented'
import { Tooltip } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

export type Filters = { tags: string[]; owners: string[] }

type Props = {
  title: string
  view: CalendarView
  onView: (v: CalendarView) => void
  onToday: () => void
  onPrev: () => void
  onNext: () => void
  query: string
  onQuery: (q: string) => void
  filters: Filters
  onFilters: (f: Filters) => void
  canCreate: boolean
  onNew: () => void
}

export const CalendarToolbar = forwardRef<HTMLInputElement, Props>(function CalendarToolbar(p, searchRef) {
  const { t } = useTranslation()
  const views: { value: CalendarView; label: string; shortcut: string }[] = [
    { value: 'month', label: t('calendar.views.month'), shortcut: 'M' },
    { value: 'week', label: t('calendar.views.week'), shortcut: 'W' },
    { value: 'day', label: t('calendar.views.day'), shortcut: 'D' },
    { value: 'agenda', label: t('calendar.views.agenda'), shortcut: 'A' },
  ]

  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-2 px-4 pt-4 pb-3 sm:px-6">
      <div className="flex min-w-0 items-center gap-2">
        <Tooltip content={`${t('calendar.today')} (T)`}>
          <Button variant="outline" size="sm" onClick={p.onToday} aria-keyshortcuts="T">
            {t('calendar.today')}
          </Button>
        </Tooltip>
        <div className="flex items-center">
          <Tooltip content={`${t('calendar.previous')} (←)`}>
            <Button variant="ghost" size="icon-sm" onClick={p.onPrev} aria-label={t('calendar.previous')} aria-keyshortcuts="ArrowLeft">
              <ChevronLeftIcon className="rtl:rotate-180" />
            </Button>
          </Tooltip>
          <Tooltip content={`${t('calendar.next')} (→)`}>
            <Button variant="ghost" size="icon-sm" onClick={p.onNext} aria-label={t('calendar.next')} aria-keyshortcuts="ArrowRight">
              <ChevronRightIcon className="rtl:rotate-180" />
            </Button>
          </Tooltip>
        </div>
        <h1 className="truncate text-lg font-semibold tracking-tight sm:text-2xl" aria-live="polite">
          {p.title}
        </h1>
      </div>

      <div className="ms-auto flex flex-wrap items-center gap-2">
        <Segmented value={p.view} onChange={p.onView} options={views} label={t('calendar.viewSwitcher')} />
        <SearchBox ref={searchRef} value={p.query} onChange={p.onQuery} />
        <FilterMenu filters={p.filters} onChange={p.onFilters} />
        {p.canCreate ? (
          <Tooltip content={`${t('calendar.newEvent')} (N)`}>
            <Button size="sm" onClick={p.onNew} aria-keyshortcuts="N">
              <PlusIcon aria-hidden />
              <span className="max-sm:sr-only">{t('calendar.newEvent')}</span>
            </Button>
          </Tooltip>
        ) : null}
      </div>
    </div>
  )
})

const SearchBox = forwardRef<HTMLInputElement, { value: string; onChange: (q: string) => void }>(function SearchBox(
  { value, onChange },
  ref,
) {
  const { t } = useTranslation()
  const [text, setText] = useState(value)
  const [prevValue, setPrevValue] = useState(value)
  // Reset local text when the URL's query changes from elsewhere (derived state, no effect).
  if (value !== prevValue) {
    setPrevValue(value)
    setText(value)
  }
  useEffect(() => {
    if (text === value) return
    const timer = setTimeout(() => onChange(text.trim()), 300)
    return () => clearTimeout(timer)
  }, [text, value, onChange])

  return (
    <div role="search" className="relative">
      <SearchIcon className="pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
      <Input
        ref={ref}
        type="search"
        value={text}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Escape') {
            setText('')
            onChange('')
            e.currentTarget.blur()
          }
        }}
        placeholder={t('calendar.searchPlaceholder')}
        aria-label={t('calendar.search')}
        aria-keyshortcuts="/"
        className="h-8 w-40 ps-8 pe-7 transition-[width] focus:w-56 sm:w-48"
      />
      {text ? (
        <button
          type="button"
          onClick={() => {
            setText('')
            onChange('')
          }}
          className="absolute end-1.5 top-1/2 -translate-y-1/2 rounded p-0.5 text-muted-foreground hover:text-foreground"
          aria-label={t('calendar.clearSearch')}
        >
          <XIcon className="size-3.5" />
        </button>
      ) : null}
    </div>
  )
})

function FilterMenu({ filters, onChange }: { filters: Filters; onChange: (f: Filters) => void }) {
  const { t } = useTranslation()
  const { data: tags } = useTags()
  const { data: dir } = useDirectory()
  const count = filters.tags.length + filters.owners.length
  const toggle = (list: string[], id: string) => (list.includes(id) ? list.filter((x) => x !== id) : [...list, id])

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className={cn(count > 0 && 'border-primary text-primary')}>
          <FilterIcon aria-hidden />
          <span className="max-sm:sr-only">{t('calendar.filters')}</span>
          {count > 0 ? (
            <span className="rounded-full bg-primary px-1.5 text-[0.6875rem] text-primary-foreground tabular-nums">
              <span aria-hidden>{count}</span>
              <span className="sr-only">{t('calendar.activeFilters', { count })}</span>
            </span>
          ) : null}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        <DropdownMenuLabel>{t('calendar.filterTags')}</DropdownMenuLabel>
        {tags?.tags.length ? (
          tags.tags.map((tag) => (
            <DropdownMenuCheckboxItem
              key={tag.id}
              checked={filters.tags.includes(tag.id)}
              onSelect={(e) => e.preventDefault()}
              onCheckedChange={() => onChange({ ...filters, tags: toggle(filters.tags, tag.id) })}
            >
              <span className="size-3 shrink-0 rounded-full" style={{ background: `var(--${tag.colorSlot})` }} aria-hidden />
              {tag.name}
            </DropdownMenuCheckboxItem>
          ))
        ) : (
          <p className="px-2 py-1.5 text-sm text-muted-foreground">{t('calendar.noTags')}</p>
        )}
        <DropdownMenuSeparator />
        <DropdownMenuLabel>{t('calendar.filterOwners')}</DropdownMenuLabel>
        {dir?.users.map((u) => (
          <DropdownMenuCheckboxItem
            key={u.id}
            checked={filters.owners.includes(u.id)}
            onSelect={(e) => e.preventDefault()}
            onCheckedChange={() => onChange({ ...filters, owners: toggle(filters.owners, u.id) })}
          >
            {u.name}
          </DropdownMenuCheckboxItem>
        ))}
        {count > 0 ? (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => onChange({ tags: [], owners: [] })}>{t('calendar.clearFilters')}</DropdownMenuItem>
          </>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
