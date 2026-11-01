import type * as React from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Label } from './label'

/** Says whether a field must be filled in: a red asterisk when required, quiet "Optional" text when not. */
export function RequirementMark({ requirement }: { requirement?: 'required' | 'optional' }) {
  const { t } = useTranslation()
  if (requirement === 'required') {
    return (
      <span aria-hidden className="text-sm leading-none font-semibold text-destructive" title={t('common.required')}>
        *
      </span>
    )
  }
  if (requirement === 'optional') return <span className="text-xs leading-none font-normal text-muted-foreground">{t('common.optional')}</span>
  return null
}

/** Label + control + hint/error, wired with ids for accessibility. */
export function Field({
  id,
  label,
  error,
  hint,
  children,
  className,
  labelHidden,
  requirement,
}: {
  id: string
  label: string
  error?: string
  hint?: React.ReactNode
  children: React.ReactNode
  className?: string
  labelHidden?: boolean
  requirement?: 'required' | 'optional'
}) {
  return (
    <div className={cn('grid gap-1.5', className)}>
      <div className={cn('flex items-baseline gap-1.5', labelHidden && 'sr-only')}>
        <Label htmlFor={id}>{label}</Label>
        <RequirementMark requirement={requirement} />
      </div>
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
