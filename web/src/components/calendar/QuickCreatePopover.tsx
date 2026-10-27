import { CalendarClockIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverAnchor, PopoverContent } from '@/components/ui/popover'

export type Slot = {
  /** Date (all-day) or UTC instant. */
  start: string
  end: string
  allDay: boolean
  x: number
  y: number
}

/** Title + time popover shown after clicking or dragging across empty slots. */
export function QuickCreatePopover({
  slot,
  whenText,
  onClose,
  onCreate,
  onMore,
  pending,
}: {
  slot: Slot | null
  whenText: string
  onClose: () => void
  onCreate: (title: string) => void
  onMore: (title: string) => void
  pending: boolean
}) {
  const { t } = useTranslation()
  const [title, setTitle] = useState('')
  const [error, setError] = useState('')
  const open = !!slot

  const submit = () => {
    const v = title.trim()
    if (!v) {
      setError(t('validation.titleRequired'))
      return
    }
    if (v.length > 200) {
      setError(t('validation.titleTooLong'))
      return
    }
    onCreate(v)
  }

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          setTitle('')
          setError('')
          onClose()
        }
      }}
    >
      <PopoverAnchor asChild>
        <span
          aria-hidden
          className="pointer-events-none fixed size-0"
          style={{ left: slot?.x ?? 0, top: slot?.y ?? 0 }}
        />
      </PopoverAnchor>
      <PopoverContent
        className="w-80"
        collisionPadding={16}
        aria-label={t('calendar.quickCreate')}
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          ;(e.currentTarget as HTMLElement | null)?.querySelector('input')?.focus()
        }}
      >
        <form
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            submit()
          }}
        >
          <h2 className="text-sm font-semibold">{t('calendar.quickCreate')}</h2>
          <div className="grid gap-1.5">
            <label htmlFor="qc-title" className="sr-only">
              {t('event.fields.title')}
            </label>
            <Input
              id="qc-title"
              value={title}
              maxLength={200}
              onChange={(e) => {
                setTitle(e.target.value)
                setError('')
              }}
              placeholder={t('event.titlePlaceholder')}
              aria-invalid={!!error}
              aria-describedby={error ? 'qc-error' : undefined}
              autoComplete="off"
            />
            {error ? (
              <p id="qc-error" role="alert" className="text-xs font-medium text-destructive">
                {error}
              </p>
            ) : null}
          </div>
          <p className="flex items-center gap-2 text-sm text-muted-foreground tabular-nums">
            <CalendarClockIcon className="size-4 shrink-0" aria-hidden />
            {whenText}
          </p>
          <div className="flex items-center justify-between gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={() => onMore(title.trim())}>
              {t('calendar.moreOptions')}
            </Button>
            <Button type="submit" size="sm" disabled={pending}>
              {t('common.create')}
            </Button>
          </div>
        </form>
      </PopoverContent>
    </Popover>
  )
}
