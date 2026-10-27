import type * as React from 'react'
import { cn } from '@/lib/utils'
import { Label } from './label'

/** Label + control + hint/error, wired with ids for accessibility. */
export function Field({
  id,
  label,
  error,
  hint,
  children,
  className,
  labelHidden,
}: {
  id: string
  label: string
  error?: string
  hint?: React.ReactNode
  children: React.ReactNode
  className?: string
  labelHidden?: boolean
}) {
  return (
    <div className={cn('grid gap-1.5', className)}>
      <Label htmlFor={id} className={cn(labelHidden && 'sr-only')}>
        {label}
      </Label>
      {children}
      {hint && !error ? (
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={`${id}-error`} role="alert" className="text-xs font-medium text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function describedBy(id: string, error?: string, hint?: unknown) {
  if (error) return `${id}-error`
  if (hint) return `${id}-hint`
  return undefined
}
