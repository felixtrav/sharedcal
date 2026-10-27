import { AlertDialog as A } from 'radix-ui'
import type * as React from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { buttonVariants } from './button'

/** A controlled confirmation dialog. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel,
  onConfirm,
  destructive,
  children,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: React.ReactNode
  confirmLabel: string
  onConfirm: () => void
  destructive?: boolean
  children?: React.ReactNode
}) {
  const { t } = useTranslation()
  return (
    <A.Root open={open} onOpenChange={onOpenChange}>
      <A.Portal>
        <A.Overlay className="fixed inset-0 z-50 bg-black/40 data-[state=open]:animate-in data-[state=open]:fade-in-0" />
        <A.Content className="fixed top-1/2 left-1/2 z-50 grid w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl border border-border bg-popover p-6 text-popover-foreground shadow-xl data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95">
          <div className="flex flex-col gap-2">
            <A.Title className="text-lg font-semibold">{title}</A.Title>
            {description ? <A.Description className="text-sm text-muted-foreground">{description}</A.Description> : <A.Description className="sr-only">{title}</A.Description>}
          </div>
          {children}
          <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <A.Cancel className={buttonVariants({ variant: 'outline' })}>{t('common.cancel')}</A.Cancel>
            <A.Action
              className={cn(buttonVariants({ variant: destructive ? 'destructive' : 'default' }))}
              onClick={onConfirm}
            >
              {confirmLabel}
            </A.Action>
          </div>
        </A.Content>
      </A.Portal>
    </A.Root>
  )
}
