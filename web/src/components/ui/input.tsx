import type * as React from 'react'
import { cn } from '@/lib/utils'

export const inputClass =
  'flex h-9 w-full min-w-0 rounded-md border border-input bg-background px-3 py-1 text-sm shadow-xs transition-[color,box-shadow] outline-none placeholder:text-muted-foreground selection:bg-primary selection:text-primary-foreground file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-destructive/20'

export function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return <input type={type} data-slot="input" className={cn(inputClass, className)} {...props} />
}

export function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(inputClass, 'field-sizing-content min-h-20 h-auto py-2 leading-relaxed', className)}
      {...props}
    />
  )
}

export function NativeSelect({ className, ...props }: React.ComponentProps<'select'>) {
  return <select className={cn(inputClass, 'pe-8 appearance-auto', className)} {...props} />
}
