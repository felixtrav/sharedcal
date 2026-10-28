import { PaperclipIcon, XIcon } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useDirectory } from '@/api/queries'
import type { UserRef } from '@/api/types'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/input'
import { formatFileSize } from '@/lib/eventText'
import { cn } from '@/lib/utils'

const MENTION_RE = /(^|\s)@([\p{L}\p{N}_.'-]{0,40})$/u

/** IDs of users whose "@Name" still appears in the text. */
function extractMentions(body: string, candidates: UserRef[]) {
  const ids = new Set<string>()
  for (const u of candidates) if (body.includes(`@${u.name}`)) ids.add(u.id)
  return [...ids]
}

/** Markdown comment box with @mention autocomplete from the user directory. */
export function CommentComposer({
  initialBody = '',
  submitLabel,
  placeholder,
  onSubmit,
  onCancel,
  allowFiles,
  focusOnMount,
  label,
}: {
  initialBody?: string
  submitLabel: string
  placeholder?: string
  onSubmit: (body: string, mentions: string[], files: File[]) => Promise<boolean>
  onCancel?: () => void
  allowFiles?: boolean
  focusOnMount?: boolean
  label: string
}) {
  const { t } = useTranslation()
  const id = useId()
  const { data: dir } = useDirectory()
  const users = useMemo(() => dir?.users ?? [], [dir])
  const [body, setBody] = useState(initialBody)
  const [query, setQuery] = useState<string | null>(null)
  const [active, setActive] = useState(0)
  const [files, setFiles] = useState<File[]>([])
  const [busy, setBusy] = useState(false)
  const ref = useRef<HTMLTextAreaElement>(null)
  const fileInput = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (focusOnMount) ref.current?.focus()
  }, [focusOnMount])

  const matches = useMemo(() => {
    if (query === null) return []
    const q = query.toLowerCase()
    return users.filter((u) => u.name.toLowerCase().includes(q)).slice(0, 6)
  }, [query, users])
  const open = query !== null && matches.length > 0

  const detect = (text: string, caret: number) => {
    const m = MENTION_RE.exec(text.slice(0, caret))
    setQuery(m ? (m[2] ?? '') : null)
    setActive(0)
  }

  const insert = (u: UserRef) => {
    const el = ref.current
    if (!el) return
    const caret = el.selectionStart
    const before = body.slice(0, caret).replace(/@[\p{L}\p{N}_.'-]{0,40}$/u, `@${u.name} `)
    const next = before + body.slice(caret)
    setBody(next)
    setQuery(null)
    requestAnimationFrame(() => {
      el.focus()
      el.setSelectionRange(before.length, before.length)
    })
  }

  const submit = async () => {
    const text = body.trim()
    if (!text || busy) return
    if (text.length > 10_000) return
    setBusy(true)
    const ok = await onSubmit(text, extractMentions(text, users), files)
    setBusy(false)
    if (ok) {
      setBody('')
      setFiles([])
    }
  }

  const listId = `${id}-mentions`
  return (
    <form
      className="grid gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <label htmlFor={`${id}-body`} className="sr-only">
        {label}
      </label>
      <div className="relative">
        <Textarea
          id={`${id}-body`}
          ref={ref}
          value={body}
          rows={2}
          maxLength={10_000}
          placeholder={placeholder ?? t('comments.placeholder')}
          role="combobox"
          aria-autocomplete="list"
          aria-expanded={open}
          aria-controls={open ? listId : undefined}
          aria-activedescendant={open ? `${listId}-${active}` : undefined}
          aria-describedby={`${id}-hint`}
          className="max-h-60 resize-none"
          onChange={(e) => {
            setBody(e.target.value)
            detect(e.target.value, e.target.selectionStart)
          }}
          onClick={(e) => detect(body, e.currentTarget.selectionStart)}
          onKeyDown={(e) => {
            if (open) {
              if (e.key === 'ArrowDown') {
                e.preventDefault()
                setActive((a) => (a + 1) % matches.length)
                return
              }
              if (e.key === 'ArrowUp') {
                e.preventDefault()
                setActive((a) => (a - 1 + matches.length) % matches.length)
                return
              }
              if (e.key === 'Enter' || e.key === 'Tab') {
                e.preventDefault()
                const u = matches[active]
                if (u) insert(u)
                return
              }
              if (e.key === 'Escape') {
                e.preventDefault()
                e.stopPropagation()
                setQuery(null)
                return
              }
            }
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
              e.preventDefault()
              void submit()
            }
            if (e.key === 'Escape' && onCancel) {
              e.stopPropagation()
              onCancel()
            }
          }}
        />
        {open ? (
          <ul
            id={listId}
            role="listbox"
            aria-label={t('comments.mentionSuggestions')}
            className="absolute bottom-full z-20 mb-1 w-64 overflow-hidden rounded-lg border border-border bg-popover p-1 shadow-lg"
          >
            {matches.map((u, i) => (
              <li
                key={u.id}
                id={`${listId}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => {
                  e.preventDefault()
                  insert(u)
                }}
                className={cn('flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm', i === active && 'bg-accent text-accent-foreground')}
              >
                <UserAvatar name={u.name} src={u.avatarUrl} className="size-5" />
                {u.name}
              </li>
            ))}
          </ul>
        ) : null}
      </div>
      {files.length ? (
        <ul className="flex flex-wrap gap-1.5" aria-label={t('comments.stagedFiles')}>
          {files.map((f, i) => (
            <li key={`${f.name}-${i}`} className="flex items-center gap-1.5 rounded-md border border-border px-2 py-1 text-xs">
              <span className="max-w-40 truncate">{f.name}</span>
              <span className="text-muted-foreground tabular-nums">{formatFileSize(f.size)}</span>
              <button
                type="button"
                aria-label={t('upload.removeStaged', { name: f.name })}
                onClick={() => setFiles((prev) => prev.filter((_, j) => j !== i))}
                className="rounded text-muted-foreground hover:text-foreground"
              >
                <XIcon className="size-3" />
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <div className="flex items-center justify-between gap-2">
        <p id={`${id}-hint`} className="text-xs text-muted-foreground">
          {t('comments.hint')}
        </p>
        <div className="flex items-center gap-1.5">
          {allowFiles ? (
            <>
              <Button type="button" variant="ghost" size="icon-xs" onClick={() => fileInput.current?.click()} aria-label={t('attachments.attach')}>
                <PaperclipIcon />
              </Button>
              <input
                ref={fileInput}
                type="file"
                multiple
                hidden
                aria-hidden
                tabIndex={-1}
                onChange={(e) => {
                  const picked = e.target.files
                  if (picked?.length) setFiles((prev) => [...prev, ...Array.from(picked)])
                  e.target.value = ''
                }}
              />
            </>
          ) : null}
          {onCancel ? (
            <Button type="button" variant="ghost" size="xs" onClick={onCancel}>
              {t('common.cancel')}
            </Button>
          ) : null}
          <Button type="submit" size="xs" disabled={!body.trim() || busy}>
            {submitLabel}
          </Button>
        </div>
      </div>
    </form>
  )
}
