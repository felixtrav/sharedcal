import { useCallback, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import type { EditScope } from '@/api/types'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { cn } from '@/lib/utils'

type Pending = { kind: 'edit' | 'delete'; disableThis: boolean; resolve: (s: EditScope | null) => void }

export type ScopeAsk = (kind: 'edit' | 'delete', opts?: { disableThis?: boolean }) => Promise<EditScope | null>

/**
 * Promise-based "This event / This and following / All events" prompt for
 * recurring events.
 */
export function useScopePrompt(): { ask: ScopeAsk; element: ReactNode } {
  const [pending, setPending] = useState<Pending | null>(null)
  const [choice, setChoice] = useState<EditScope>('this')
  const resolved = useRef(false)

  const ask = useCallback<ScopeAsk>((kind, opts) => {
    return new Promise<EditScope | null>((resolve) => {
      resolved.current = false
      const disableThis = !!opts?.disableThis
      setChoice(disableThis ? 'following' : 'this')
      setPending({ kind, disableThis, resolve })
    })
  }, [])

  const finish = (value: EditScope | null) => {
    if (pending && !resolved.current) {
      resolved.current = true
      pending.resolve(value)
    }
    setPending(null)
  }

  const element = <ScopeDialogView pending={pending} choice={choice} setChoice={setChoice} finish={finish} />
  return { ask, element }
}

function ScopeDialogView({
  pending,
  choice,
  setChoice,
  finish,
}: {
  pending: Pending | null
  choice: EditScope
  setChoice: (s: EditScope) => void
  finish: (v: EditScope | null) => void
}) {
  const { t } = useTranslation()
  const options: { value: EditScope; label: string }[] = [
    { value: 'this', label: t('scope.this') },
    { value: 'following', label: t('scope.following') },
    { value: 'all', label: t('scope.all') },
  ]
  const isDelete = pending?.kind === 'delete'
  return (
    <Dialog open={!!pending} onOpenChange={(open) => !open && finish(null)}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{isDelete ? t('scope.deleteTitle') : t('scope.editTitle')}</DialogTitle>
          <DialogDescription>{t('scope.description')}</DialogDescription>
        </DialogHeader>
        <fieldset className="grid gap-2">
          <legend className="sr-only">{t('scope.legend')}</legend>
          {options.map((o) => (
            <label
              key={o.value}
              className={cn(
                'flex cursor-pointer items-center gap-3 rounded-lg border border-border px-3 py-2.5 text-sm transition-colors has-[:focus-visible]:ring-[3px] has-[:focus-visible]:ring-ring/50',
                choice === o.value && 'border-primary bg-primary/5',
                o.value === 'this' && pending?.disableThis && 'cursor-not-allowed opacity-60',
              )}
            >
              <input
                type="radio"
                name="scope"
                value={o.value}
                checked={choice === o.value}
                disabled={o.value === 'this' && pending?.disableThis}
                onChange={() => setChoice(o.value)}
                className="size-4 accent-(--primary)"
              />
              <span>
                {o.label}
                {o.value === 'this' && pending?.disableThis ? (
                  <span className="block text-xs text-muted-foreground">{t('scope.thisUnavailable')}</span>
                ) : null}
              </span>
            </label>
          ))}
        </fieldset>
        <DialogFooter>
          <Button variant="outline" onClick={() => finish(null)}>
            {t('common.cancel')}
          </Button>
          <Button variant={isDelete ? 'destructive' : 'default'} onClick={() => finish(choice)}>
            {isDelete ? t('common.delete') : t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
