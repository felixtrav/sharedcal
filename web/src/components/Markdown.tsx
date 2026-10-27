import { useMemo } from 'react'
import { renderMarkdown } from '@/lib/markdown'
import { cn } from '@/lib/utils'

/**
 * The only component allowed to set inner HTML. Input is Markdown; output is
 * run through the strict allow-list sanitizer in lib/markdown.ts.
 */
export default function Markdown({ source, className }: { source: string; className?: string }) {
  const html = useMemo(() => renderMarkdown(source), [source])
  return <div className={cn('prose-sc', className)} dangerouslySetInnerHTML={{ __html: html }} />
}
