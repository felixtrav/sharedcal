import { XIcon } from 'lucide-react'
import { Toast as T } from 'radix-ui'
import { useTranslation } from 'react-i18next'
import { createStore, useStore } from '@/lib/store'
import { cn } from '@/lib/utils'

export type ToastInput = {
  title: string
  description?: string
  variant?: 'default' | 'error' | 'success'
  /** ms; default 5000. */
  duration?: number
  action?: { label: string; onClick: () => void; altText?: string }
}

type ToastItem = ToastInput & { id: number; open: boolean }

const toastStore = createStore<ToastItem[]>([])
let nextId = 1

export function toast(input: ToastInput) {
  const id = nextId++
  toastStore.set((list) => [...list.slice(-4), { ...input, id, open: true }])
  return id
}

toast.error = (title: string, description?: string) => toast({ title, description, variant: 'error', duration: 7000 })
toast.success = (title: string, description?: string) => toast({ title, description, variant: 'success' })
toast.dismiss = (id: number) => close(id)

function close(id: number) {
  toastStore.set((list) => list.map((t) => (t.id === id ? { ...t, open: false } : t)))
  setTimeout(() => toastStore.set((list) => list.filter((t) => t.id !== id)), 300)
}

/** Mount once at the app root. Uses Radix Toast (no injected <style> elements, CSP-friendly). */
export function Toaster() {
  const items = useStore(toastStore)
  const { t } = useTranslation()
  return (
    <T.Provider swipeDirection="right" label={t('common.notifications')}>
      {items.map((item) => (
        <T.Root
          key={item.id}
          open={item.open}
          duration={item.duration ?? 5000}
          type={item.variant === 'error' ? 'foreground' : 'background'}
          onOpenChange={(open) => !open && close(item.id)}
          className={cn(
            'group pointer-events-auto relative flex w-full items-start gap-3 rounded-lg border border-border bg-popover p-4 pe-9 text-popover-foreground shadow-lg',
            'data-[state=open]:animate-in data-[state=open]:slide-in-from-bottom-2 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 motion-reduce:animate-none',
            'data-[swipe=move]:translate-x-(--radix-toast-swipe-move-x) data-[swipe=end]:animate-out',
            item.variant === 'error' && 'border-destructive/50',
          )}
        >
          {item.variant === 'error' ? <span className="mt-1.5 size-2 shrink-0 rounded-full bg-destructive" aria-hidden /> : null}
          {item.variant === 'success' ? <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary" aria-hidden /> : null}
          <div className="grid min-w-0 flex-1 gap-1">
            <T.Title className="text-sm font-semibold">{item.title}</T.Title>
            {item.description ? <T.Description className="text-sm text-muted-foreground">{item.description}</T.Description> : null}
          </div>
          {item.action ? (
            <T.Action
              altText={item.action.altText ?? item.action.label}
              onClick={item.action.onClick}
              className="inline-flex h-8 shrink-0 items-center rounded-md border border-border px-3 text-sm font-medium hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              {item.action.label}
            </T.Action>
          ) : null}
          <T.Close
            aria-label={t('common.dismiss')}
            className="absolute end-2 top-2 rounded-md p-1 text-muted-foreground hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:outline-none"
          >
            <XIcon className="size-4" />
          </T.Close>
        </T.Root>
      ))}
      <T.Viewport className="fixed end-0 bottom-0 z-[100] m-0 flex w-full max-w-sm list-none flex-col gap-2 p-4 outline-none max-md:bottom-16" />
    </T.Provider>
  )
}
