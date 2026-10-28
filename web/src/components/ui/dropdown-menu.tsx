import { DropdownMenu as M } from 'radix-ui'
import type * as React from 'react'
import { cn } from '@/lib/utils'

export const DropdownMenu = M.Root
export const DropdownMenuTrigger = M.Trigger

export function DropdownMenuContent({ className, sideOffset = 6, ...props }: React.ComponentProps<typeof M.Content>) {
  return (
    <M.Portal>
      <M.Content
        sideOffset={sideOffset}
        className={cn(
          'z-50 max-h-(--radix-dropdown-menu-content-available-height) min-w-40 overflow-y-auto rounded-lg border border-border bg-popover p-1 text-popover-foreground shadow-lg data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95',
          className,
        )}
        {...props}
      />
    </M.Portal>
  )
}

const itemClass =
  "relative flex cursor-default items-center gap-2 rounded-md px-2 py-1.5 text-sm outline-none select-none focus:bg-accent focus:text-accent-foreground data-disabled:pointer-events-none data-disabled:opacity-50 [&_svg]:size-4 [&_svg]:shrink-0 [&_svg:not([class*='text-'])]:text-muted-foreground"

export function DropdownMenuItem({ className, destructive, ...props }: React.ComponentProps<typeof M.Item> & { destructive?: boolean }) {
  return <M.Item className={cn(itemClass, destructive && 'text-destructive focus:text-destructive', className)} {...props} />
}

export function DropdownMenuCheckboxItem({ className, children, ...props }: React.ComponentProps<typeof M.CheckboxItem>) {
  return (
    <M.CheckboxItem className={cn(itemClass, 'ps-8', className)} {...props}>
      <span className="pointer-events-none absolute start-2 flex size-4 items-center justify-center">
        <M.ItemIndicator>
          <svg viewBox="0 0 16 16" className="size-3.5" aria-hidden="true">
            <path d="M3 8.5l3 3 7-7" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        </M.ItemIndicator>
      </span>
      {children}
    </M.CheckboxItem>
  )
}

export function DropdownMenuLabel({ className, ...props }: React.ComponentProps<typeof M.Label>) {
  return <M.Label className={cn('px-2 py-1.5 text-xs font-medium text-muted-foreground', className)} {...props} />
}

export function DropdownMenuSeparator({ className, ...props }: React.ComponentProps<typeof M.Separator>) {
  return <M.Separator className={cn('-mx-1 my-1 h-px bg-border', className)} {...props} />
}
