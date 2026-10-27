import { useQueryClient } from '@tanstack/react-query'
import { MessageSquareIcon, PaperclipIcon, PencilIcon, ReplyIcon, Trash2Icon } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { useComments, useMeData } from '@/api/queries'
import type { CalEvent, Comment } from '@/api/types'
import { LazyMarkdown } from '@/components/LazyMarkdown'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { UserAvatar } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { formatDateTime, formatRelative, type FormatOptions } from '@/lib/time'
import { applyStreamMessage } from '@/realtime/applyMessage'
import { AttachmentItems } from './AttachmentItems'
import { CommentComposer } from './CommentComposer'
import { UploadList } from './UploadList'
import { useUploads } from './useUploads'

export function CommentsTab({ event, fmt }: { event: CalEvent; fmt: FormatOptions }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const me = useMeData()
  const { data, isPending, isError, refetch } = useComments(event.id)
  const [pendingFiles, setPendingFiles] = useState<Record<string, File[]>>({})
  const bottom = useRef<HTMLDivElement>(null)
  // Whether files may go on a comment not written yet; the server still checks each comment.
  const canAttachToNew = me.permissions.attachToComments

  const threads = useMemo(() => {
    const list = data?.comments ?? []
    const tops = list.filter((c) => !c.parentId)
    return tops.map((c) => ({ comment: c, replies: list.filter((r) => r.parentId === c.id) }))
  }, [data])

  const count = data?.comments.length ?? 0
  const prevCount = useRef(count)
  useEffect(() => {
    if (count > prevCount.current) bottom.current?.scrollIntoView({ block: 'end', behavior: 'smooth' })
    prevCount.current = count
  }, [count])

  const post = async (body: string, mentions: string[], files: File[], parentId?: string) => {
    try {
      const c = await endpoints.createComment(event.id, { body, mentions, ...(parentId ? { parentId } : {}) })
      applyStreamMessage(qc, 'comment.created', c)
      if (files.length) {
        if (c.permissions.attach) setPendingFiles((p) => ({ ...p, [c.id]: files }))
        else toast.error(t('comments.attachNotAllowed'))
      }
      return true
    } catch (err) {
      toast.error(t('comments.postFailed'), errorMessage(err, t))
      return false
    }
  }

  return (
    <div className="flex min-h-full flex-col">
      <div className="flex-1 px-5 pt-4 pb-4">
        {isPending ? (
          <div className="grid gap-4" aria-hidden>
            {[0, 1].map((i) => (
              <div key={i} className="flex gap-3">
                <Skeleton className="size-8 rounded-full" />
                <div className="grid flex-1 gap-2">
                  <Skeleton className="h-4 w-32" />
                  <Skeleton className="h-12 w-full" />
                </div>
              </div>
            ))}
          </div>
        ) : isError ? (
          <div role="alert" className="text-sm">
            {t('comments.loadFailed')}{' '}
            <Button variant="link" size="xs" onClick={() => void refetch()}>
              {t('common.retry')}
            </Button>
          </div>
        ) : threads.length === 0 ? (
          <div className="flex flex-col items-center gap-2 py-10 text-center text-sm text-muted-foreground">
            <MessageSquareIcon className="size-6" aria-hidden />
            {t('comments.empty')}
          </div>
        ) : (
          <ol className="grid gap-5" aria-label={t('comments.list')}>
            {threads.map(({ comment, replies }) => (
              <li key={comment.id}>
                <CommentItem
                  comment={comment}
                  fmt={fmt}
                  canReply={event.permissions.comment}
                  onReply={(body, mentions, files) => post(body, mentions, files, comment.id)}
                  initialFiles={pendingFiles[comment.id]}
                  canAttachToNew={canAttachToNew}
                />
                {replies.length ? (
                  <ol className="ms-11 mt-3 grid gap-3 border-s border-border ps-4" aria-label={t('comments.replies')}>
                    {replies.map((r) => (
                      <li key={r.id}>
                        <CommentItem comment={r} fmt={fmt} initialFiles={pendingFiles[r.id]} canAttachToNew={canAttachToNew} />
                      </li>
                    ))}
                  </ol>
                ) : null}
              </li>
            ))}
          </ol>
        )}
        <div ref={bottom} />
      </div>
      {event.permissions.comment ? (
        <div className="sticky bottom-0 border-t border-border bg-card px-5 py-3">
          <CommentComposer
            label={t('comments.new')}
            submitLabel={t('comments.post')}
            allowFiles={canAttachToNew}
            onSubmit={(body, mentions, files) => post(body, mentions, files)}
          />
        </div>
      ) : null}
    </div>
  )
}

function CommentItem({
  comment,
  fmt,
  canReply,
  onReply,
  initialFiles,
  canAttachToNew,
}: {
  comment: Comment
  fmt: FormatOptions
  canReply?: boolean
  onReply?: (body: string, mentions: string[], files: File[]) => Promise<boolean>
  initialFiles?: File[]
  canAttachToNew: boolean
}) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [editing, setEditing] = useState(false)
  const [replying, setReplying] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const uploads = useUploads(comment.permissions.attach ? { kind: 'comment', commentId: comment.id, eventId: comment.eventId } : null, comment.attachments.length)
  const fileInput = useRef<HTMLInputElement>(null)
  const started = useRef(false)

  const { add } = uploads
  useEffect(() => {
    if (initialFiles?.length && !started.current) {
      started.current = true
      add(initialFiles)
    }
  }, [initialFiles, add])

  if (comment.deleted) {
    return <p className="rounded-lg bg-muted px-3 py-2 text-sm text-muted-foreground italic">{t('comments.deletedPlaceholder')}</p>
  }

  const save = async (body: string, mentions: string[]) => {
    try {
      const c = await endpoints.updateComment(comment.id, { body, mentions })
      applyStreamMessage(qc, 'comment.updated', c)
      setEditing(false)
      return true
    } catch (err) {
      toast.error(t('comments.saveFailed'), errorMessage(err, t))
      return false
    }
  }

  const remove = async () => {
    try {
      await endpoints.deleteComment(comment.id)
      applyStreamMessage(qc, 'comment.deleted', { id: comment.id, eventId: comment.eventId })
    } catch (err) {
      toast.error(t('comments.deleteFailed'), errorMessage(err, t))
    }
  }

  return (
    <article className="flex gap-3" aria-label={t('comments.by', { name: comment.author.name })}>
      <UserAvatar name={comment.author.name} src={comment.author.avatarUrl} className="mt-0.5 size-8" />
      <div className="min-w-0 flex-1">
        <header className="flex flex-wrap items-baseline gap-x-2">
          <span className="text-sm font-semibold">{comment.author.name}</span>
          <time dateTime={comment.createdAt} title={formatDateTime(Date.parse(comment.createdAt), fmt)} className="text-xs text-muted-foreground">
            {formatRelative(comment.createdAt)}
          </time>
          {comment.editedAt ? (
            <span className="text-xs text-muted-foreground" title={formatDateTime(Date.parse(comment.editedAt), fmt)}>
              {t('comments.edited')}
            </span>
          ) : null}
        </header>
        {editing ? (
          <div className="mt-2">
            <CommentComposer
              label={t('comments.edit')}
              initialBody={comment.body}
              submitLabel={t('common.save')}
              focusOnMount
              onSubmit={(body, mentions) => save(body, mentions)}
              onCancel={() => setEditing(false)}
            />
          </div>
        ) : (
          <LazyMarkdown source={comment.body} className="mt-1 text-sm" />
        )}
        {comment.attachments.length ? (
          <div className="mt-2">
            <AttachmentItems attachments={comment.attachments} compact />
          </div>
        ) : null}
        <div className="mt-2">
          <UploadList items={uploads.items} onCancel={uploads.cancel} />
        </div>
        <div className="mt-1 flex flex-wrap gap-1">
          {canReply && onReply ? (
            <Button variant="ghost" size="xs" onClick={() => setReplying((r) => !r)} aria-expanded={replying}>
              <ReplyIcon aria-hidden />
              {t('comments.reply')}
            </Button>
          ) : null}
          {comment.permissions.edit && !editing ? (
            <Button variant="ghost" size="xs" onClick={() => setEditing(true)}>
              <PencilIcon aria-hidden />
              {t('common.edit')}
            </Button>
          ) : null}
          {comment.permissions.attach ? (
            <>
              <Button variant="ghost" size="xs" onClick={() => fileInput.current?.click()}>
                <PaperclipIcon aria-hidden />
                {t('attachments.attach')}
              </Button>
              <input
                ref={fileInput}
                type="file"
                multiple
                hidden
                aria-hidden
                tabIndex={-1}
                onChange={(e) => {
                  if (e.target.files?.length) uploads.add(e.target.files)
                  e.target.value = ''
                }}
              />
            </>
          ) : null}
          {comment.permissions.delete ? (
            <Button variant="ghost" size="xs" className="text-muted-foreground hover:text-destructive" onClick={() => setConfirmDelete(true)}>
              <Trash2Icon aria-hidden />
              {t('common.delete')}
            </Button>
          ) : null}
        </div>
        {replying && onReply ? (
          <div className="mt-2">
            <CommentComposer
              label={t('comments.replyTo', { name: comment.author.name })}
              submitLabel={t('comments.reply')}
              focusOnMount
              allowFiles={canAttachToNew}
              onSubmit={async (body, mentions, files) => {
                const ok = await onReply(body, mentions, files)
                if (ok) setReplying(false)
                return ok
              }}
              onCancel={() => setReplying(false)}
            />
          </div>
        ) : null}
      </div>
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t('comments.deleteTitle')}
        description={t('comments.deleteBody')}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={() => void remove()}
      />
    </article>
  )
}
