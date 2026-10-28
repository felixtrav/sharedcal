import { CheckIcon, ChevronDownIcon } from 'lucide-react'
import { Select as S } from 'radix-ui'
import type * as React from 'react'
import { cn } from '@/lib/utils'

type SelectOption = { value: string; label: string; disabled?: boolean }

/** Radix Select with a simple options API. */
export function Select({
  value,
  onValueChange,
  options,
  placeholder,
  id,
  className,
  disabled,
  'aria-label': ariaLabel,
  'aria-invalid': ariaInvalid,
}: {
  value: string
  onValueChange: (v: string) => void
  options: SelectOption[]
  placeholder?: string
  id?: string
  className?: string
  disabled?: boolean
  'aria-label'?: string
  'aria-invalid'?: boolean
}) {
  return (
    <S.Root value={value} onValueChange={onValueChange} disabled={disabled}>
      <S.Trigger
        id={id}
        aria-label={ariaLabel}
        aria-invalid={ariaInvalid}
        className={cn(
          'flex h-9 w-full items-center justify-between gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm whitespace-nowrap shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 data-placeholder:text-muted-foreground [&>span]:truncate',
          className,
        )}
      >
        <S.Value placeholder={placeholder} />
        <S.Icon asChild>
          <ChevronDownIcon className="size-4 opacity-60" />
        </S.Icon>
      </S.Trigger>
      <S.Portal>
        <S.Content
          position="popper"
          sideOffset={4}
          className="relative z-50 max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) overflow-hidden rounded-lg border border-border bg-popover text-popover-foreground shadow-lg data-[state=open]:animate-in data-[state=open]:fade-in-0"
        >
          <S.Viewport className="p-1">
            {options.map((o) => (
              <SelectItem key={o.value} value={o.value} disabled={o.disabled}>
                {o.label}
              </SelectItem>
            ))}
          </S.Viewport>
        </S.Content>
      </S.Portal>
    </S.Root>
  )
}

function SelectItem({ className, children, ...props }: React.ComponentProps<typeof S.Item>) {
  return (
    <S.Item
      className={cn(
        'relative flex w-full cursor-default items-center rounded-md py-1.5 ps-2 pe-8 text-sm outline-none select-none focus:bg-accent focus:text-accent-foreground data-disabled:pointer-events-none data-disabled:opacity-50',
        className,
      )}
      {...props}
    >
      <span className="absolute end-2 flex size-3.5 items-center justify-center">
        <S.ItemIndicator>
          <CheckIcon className="size-4" />
        </S.ItemIndicator>
      </span>
      <S.ItemText>{children}</S.ItemText>
    </S.Item>
  )
}
