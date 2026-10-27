import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { CalEvent, EventWrite } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { FormatOptions } from '@/lib/time'
import { cn } from '@/lib/utils'
import { fieldLabel, FieldValue } from './FieldValue'

export type Conflict = { mine: Partial<EventWrite>; current: CalEvent }

/**
 * 409 on save: show my changes next to the version someone else saved, and
 * let me pick, field by field, which value to re-apply.
 */
export function ConflictDialog({
  conflict,
  fmt,
  onApply,
  onDiscard,
  pending,
}: {
  conflict: Conflict | null
  fmt: FormatOptions
  onApply: (fields: Partial<EventWrite>) => void
  onDiscard: () => void
  pending: boolean
}) {
  const { t } = useTranslation()
  const fields = conflict ? (Object.keys(conflict.mine) as (keyof EventWrite)[]) : []
  const [keepMine, setKeepMine] = useState<Record<string, boolean>>({})
  const choose = (f: string) => keepMine[f] ?? true

  return (
    <Dialog open={!!conflict} onOpenChange={(o) => !o && onDiscard()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t('conflict.title')}</DialogTitle>
          <DialogDescription>{t('conflict.description')}</DialogDescription>
        </DialogHeader>
        {conflict ? (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <caption className="sr-only">{t('conflict.caption')}</caption>
              <thead>
                <tr className="text-start text-xs text-muted-foreground">
                  <th scope="col" className="py-2 pe-3 text-start font-medium">
                    {t('conflict.field')}
                  </th>
                  <th scope="col" className="px-3 py-2 text-start font-medium">
                    {t('conflict.theirs')}
                  </th>
                  <th scope="col" className="px-3 py-2 text-start font-medium">
                    {t('conflict.mine')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {fields.map((f) => {
                  const theirs = (conflict.current as unknown as Record<string, unknown>)[f]
                  const mineVal = conflict.mine[f]
                  const same = JSON.stringify(theirs) === JSON.stringify(mineVal)
                  return (
                    <tr key={f} className="border-t border-border align-top">
                      <th scope="row" className="py-2.5 pe-3 text-start font-medium">
                        {fieldLabel(f, t)}
                      </th>
                      <td className="px-3 py-2.5">
                        <label className={cn('flex cursor-pointer gap-2 rounded-md p-1.5', !choose(f) && 'bg-primary/10 ring-1 ring-primary')}>
                          <input
                            type="radio"
                            name={`conflict-${f}`}
                            checked={!choose(f)}
                            onChange={() => setKeepMine((m) => ({ ...m, [f]: false }))}
                            className="mt-1 accent-(--primary)"
                          />
                          <span className="min-w-0">
                            <span className="sr-only">{t('conflict.useTheirs')}: </span>
                            <FieldValue field={f} value={theirs} fmt={fmt} startDate={conflict.current.start} />
                          </span>
                        </label>
                      </td>
                      <td className="px-3 py-2.5">
                        <label className={cn('flex cursor-pointer gap-2 rounded-md p-1.5', choose(f) && 'bg-primary/10 ring-1 ring-primary')}>
                          <input
                            type="radio"
                            name={`conflict-${f}`}
                            checked={choose(f)}
                            onChange={() => setKeepMine((m) => ({ ...m, [f]: true }))}
                            className="mt-1 accent-(--primary)"
                          />
                          <span className="min-w-0">
                            <span className="sr-only">{t('conflict.keepMine')}: </span>
                            <FieldValue field={f} value={mineVal} fmt={fmt} startDate={conflict.current.start} />
                            {same ? <span className="ms-1 text-xs text-muted-foreground">({t('conflict.same')})</span> : null}
                          </span>
                        </label>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        ) : null}
        <DialogFooter>
          <Button variant="outline" onClick={onDiscard}>
            {t('conflict.discard')}
          </Button>
          <Button
            disabled={pending}
            onClick={() => {
              if (!conflict) return
              const out: Partial<EventWrite> = {}
              for (const f of fields) if (choose(f)) (out as Record<string, unknown>)[f] = conflict.mine[f]
              onApply(out)
            }}
          >
            {t('conflict.apply')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
