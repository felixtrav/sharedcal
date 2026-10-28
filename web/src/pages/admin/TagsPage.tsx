import { useQueryClient } from '@tanstack/react-query'
import { ArrowDownIcon, ArrowUpIcon, GripVerticalIcon, PlusIcon, TagsIcon, Trash2Icon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useTags } from '@/api/queries'
import { TAG_SLOTS, type Tag, type TagSlot } from '@/api/types'
import { EmptyState, Page, PageHeader } from '@/components/layout/PageHeader'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'
import { cn } from '@/lib/utils'

type TagsData = { tags: Tag[] }

/** Radio group of the 8 theme tag slots, shown as swatches. */
function SlotPicker({ value, onChange, label, name }: { value: TagSlot; onChange: (s: TagSlot) => void; label: string; name: string }) {
  const { t } = useTranslation()
  return (
    <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-1.5">
      {TAG_SLOTS.map((slot, i) => (
        <label key={slot} className="relative cursor-pointer">
          <input type="radio" name={name} value={slot} checked={value === slot} onChange={() => onChange(slot)} className="peer sr-only" />
          <span
            className={cn(
              'flex size-7 items-center justify-center rounded-full text-[0.625rem] font-semibold ring-offset-2 ring-offset-card peer-focus-visible:ring-[3px] peer-focus-visible:ring-ring/60',
              value === slot && 'ring-2 ring-foreground',
            )}
            style={{ background: `var(--${slot})`, color: `var(--${slot}-foreground)` }}
            aria-hidden
          >
            {i + 1}
          </span>
          <span className="sr-only">{t('tags.slot', { n: i + 1 })}</span>
        </label>
      ))}
    </div>
  )
}

export function TagsPage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const { data, isPending, isError, refetch } = useTags()
  const tags = data?.tags ?? []
  const [name, setName] = useState('')
  const [slot, setSlot] = useState<TagSlot>('tag-1')
  const [dragId, setDragId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState<Tag | null>(null)
  const [announce, setAnnounce] = useState('')

  const setTags = (list: Tag[]) => qc.setQueryData<TagsData>(keys.tags, { tags: list })

  const create = async () => {
    const n = name.trim()
    if (!n) return
    try {
      const tag = await endpoints.createTag({ name: n, colorSlot: slot })
      setTags([...tags, tag])
      setName('')
      setSlot(TAG_SLOTS[(tags.length + 1) % 8] ?? 'tag-1')
      toast.success(t('tags.created', { name: tag.name }))
    } catch (err) {
      toast.error(t('tags.saveFailed'), errorMessage(err, t))
    }
  }

  const patch = async (tag: Tag, body: { name?: string; colorSlot?: TagSlot }) => {
    const prev = tags
    setTags(tags.map((x) => (x.id === tag.id ? { ...x, ...body } : x)))
    try {
      const saved = await endpoints.updateTag(tag.id, body)
      qc.setQueryData<TagsData>(keys.tags, (d) => (d ? { tags: d.tags.map((x) => (x.id === tag.id ? saved : x)) } : d))
    } catch (err) {
      setTags(prev)
      toast.error(t('tags.saveFailed'), errorMessage(err, t))
    }
  }

  const reorder = async (ids: string[]) => {
    const prev = tags
    const byId = new Map(tags.map((x) => [x.id, x]))
    setTags(ids.map((id, i) => ({ ...(byId.get(id) as Tag), sortOrder: i })))
    try {
      const res = await endpoints.reorderTags(ids)
      setTags(res.tags)
    } catch (err) {
      setTags(prev)
      toast.error(t('tags.saveFailed'), errorMessage(err, t))
    }
  }

  const move = (index: number, delta: number) => {
    const target = index + delta
    if (target < 0 || target >= tags.length) return
    const ids = tags.map((x) => x.id)
    const [id] = ids.splice(index, 1)
    if (!id) return
    ids.splice(target, 0, id)
    void reorder(ids)
    setAnnounce(t('tags.moved', { name: tags[index]?.name, position: target + 1, total: tags.length }))
  }

  const remove = async (tag: Tag) => {
    try {
      await endpoints.deleteTag(tag.id)
      setTags(tags.filter((x) => x.id !== tag.id))
      void qc.invalidateQueries({ queryKey: keys.eventsAll })
      toast({ title: t('tags.deleted', { name: tag.name }) })
    } catch (err) {
      toast.error(t('tags.deleteFailed'), errorMessage(err, t))
    }
  }

  return (
    <Page>
      <PageHeader title={t('tags.title')} description={t('tags.description')} />
      <form
        className="mb-6 grid gap-3 rounded-xl border border-border bg-card p-4"
        onSubmit={(e) => {
          e.preventDefault()
          void create()
        }}
      >
        <h2 className="text-sm font-semibold">{t('tags.new')}</h2>
        <div className="flex flex-wrap items-center gap-3">
          <label htmlFor="new-tag" className="sr-only">
            {t('tags.name')}
          </label>
          <Input id="new-tag" value={name} maxLength={50} onChange={(e) => setName(e.target.value)} placeholder={t('tags.namePlaceholder')} className="w-56" />
          <SlotPicker value={slot} onChange={setSlot} label={t('tags.color')} name="new-tag-slot" />
          <Button type="submit" size="sm" disabled={!name.trim()}>
            <PlusIcon aria-hidden />
            {t('tags.add')}
          </Button>
        </div>
      </form>

      <p className="sr-only" role="status" aria-live="polite">
        {announce}
      </p>

      {isPending ? (
        <Skeleton className="h-40" />
      ) : isError ? (
        <div role="alert" className="text-sm">
          {t('errors.loadFailed')}{' '}
          <Button variant="link" size="xs" onClick={() => void refetch()}>
            {t('common.retry')}
          </Button>
        </div>
      ) : tags.length === 0 ? (
        <EmptyState icon={<TagsIcon />} title={t('tags.empty')} body={t('tags.emptyBody')} />
      ) : (
        <ol className="divide-y divide-border rounded-xl border border-border bg-card" aria-label={t('tags.list')}>
          {tags.map((tag, i) => (
            <li
              key={tag.id}
              draggable
              onDragStart={(e) => {
                setDragId(tag.id)
                e.dataTransfer.effectAllowed = 'move'
              }}
              onDragOver={(e) => {
                if (dragId) e.preventDefault()
              }}
              onDrop={(e) => {
                e.preventDefault()
                if (!dragId || dragId === tag.id) return
                const ids = tags.map((x) => x.id).filter((id) => id !== dragId)
                ids.splice(i, 0, dragId)
                setDragId(null)
                void reorder(ids)
              }}
              onDragEnd={() => setDragId(null)}
              className={cn('flex flex-wrap items-center gap-3 px-3 py-3', dragId === tag.id && 'opacity-50')}
            >
              <GripVerticalIcon className="size-4 cursor-grab text-muted-foreground" aria-hidden />
              <TagName tag={tag} onSave={(n) => void patch(tag, { name: n })} />
              <SlotPicker
                value={tag.colorSlot}
                onChange={(s) => void patch(tag, { colorSlot: s })}
                label={t('tags.colorFor', { name: tag.name })}
                name={`slot-${tag.id}`}
              />
              <div className="ms-auto flex items-center gap-1">
                <Button variant="ghost" size="icon-xs" disabled={i === 0} onClick={() => move(i, -1)} aria-label={t('tags.moveUp', { name: tag.name })}>
                  <ArrowUpIcon />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  disabled={i === tags.length - 1}
                  onClick={() => move(i, 1)}
                  aria-label={t('tags.moveDown', { name: tag.name })}
                >
                  <ArrowDownIcon />
                </Button>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="hover:text-destructive"
                  onClick={() => setDeleting(tag)}
                  aria-label={t('tags.delete', { name: tag.name })}
                >
                  <Trash2Icon />
                </Button>
              </div>
            </li>
          ))}
        </ol>
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={t('tags.deleteTitle', { name: deleting?.name })}
        description={t('tags.deleteBody')}
        confirmLabel={t('common.delete')}
        destructive
        onConfirm={() => deleting && void remove(deleting)}
      />
    </Page>
  )
}

function TagName({ tag, onSave }: { tag: Tag; onSave: (name: string) => void }) {
  const { t } = useTranslation()
  const [value, setValue] = useState(tag.name)
  const commit = () => {
    const v = value.trim()
    if (!v) setValue(tag.name)
    else if (v !== tag.name) onSave(v)
  }
  return (
    <Input
      value={value}
      maxLength={50}
      aria-label={t('tags.renameLabel', { name: tag.name })}
      onChange={(e) => setValue(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === 'Enter') {
          e.preventDefault()
          commit()
        }
        if (e.key === 'Escape') setValue(tag.name)
      }}
      className="h-8 w-44"
    />
  )
}
