import { lazy, Suspense } from 'react'
import { cn } from '@/lib/utils'

// marked + DOMPurify load on first use, keeping them out of the initial bundle.
const Markdown = lazy(() => import('./Markdown'))

export function LazyMarkdown({ source, className }: { source: string; className?: string }) {
  return (
    <Suspense fallback={<div className={cn('prose-sc whitespace-pre-wrap', className)}>{source}</div>}>
      <Markdown source={source} className={className} />
    </Suspense>
  )
}
