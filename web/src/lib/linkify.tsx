import type { ReactNode } from 'react'

const URL_RE = /\b(https?:\/\/[^\s<>"']+|www\.[^\s<>"']+)/gi

export type Piece = { type: 'text' | 'link'; value: string; href?: string }

/** Split free text into text and http(s) link pieces. Trailing punctuation stays text. */
export function splitLinks(text: string): Piece[] {
  const out: Piece[] = []
  let last = 0
  for (const m of text.matchAll(URL_RE)) {
    let url = m[0]
    const trail = /[).,;:!?]+$/.exec(url)
    if (trail) url = url.slice(0, -trail[0].length)
    const start = m.index ?? 0
    if (start > last) out.push({ type: 'text', value: text.slice(last, start) })
    out.push({ type: 'link', value: url, href: url.startsWith('www.') ? `https://${url}` : url })
    last = start + url.length
  }
  if (last < text.length) out.push({ type: 'text', value: text.slice(last) })
  return out
}

/** Render text with URLs as safe external links (React elements, never HTML). */
export function Linkified({ text }: { text: string }): ReactNode {
  return splitLinks(text).map((p, i) =>
    p.type === 'link' ? (
      <a
        key={i}
        href={p.href}
        target="_blank"
        rel="noopener noreferrer nofollow"
        className="text-primary underline underline-offset-2 break-all"
      >
        {p.value}
      </a>
    ) : (
      <span key={i}>{p.value}</span>
    ),
  )
}
