import { useQueryClient } from '@tanstack/react-query'
import { CheckIcon, LinkIcon, PencilIcon, PlusIcon, Trash2Icon, UsersIcon } from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useMeData, usePeople } from '@/api/queries'
import type { Person, UserRef } from '@/api/types'
import { EmptyState, LoadError, Page, PageHeader } from '@/components/layout/PageHeader'
import { UserAvatar } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input, NativeSelect } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from '@/components/ui/toast'
import { errorMessage } from '@/lib/errors'

const MAX_NAME = 64

/** Registered users, and the people events can name who have no account yet. */
export function PeoplePage() {
  const { t } = useTranslation()
  const me = useMeData()
  const { data, isPending, isError, refetch } = usePeople()
  const canAdd = me.permissions.createEvent
  return (
    <Page wide>
      <PageHeader title={t('directory.title')} description={t('directory.description')} />
      {isPending ? (
        <Skeleton className="h-40" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : (
        <div className="grid gap-8">
          <section aria-labelledby="directory-users">
            <h2 id="directory-users" className="mb-2 text-sm font-semibold">
              {t('directory.users')}
            </h2>
            <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
              {data.users.map((u) => (
                <li key={u.id} className="flex items-center gap-2.5 rounded-xl border border-border bg-card px-3 py-2">
                  <UserAvatar name={u.name} className="size-7" />
                  <span className="truncate text-sm font-medium">{u.name}</span>
                </li>
              ))}
            </ul>
          </section>
          <section aria-labelledby="directory-people">
            <h2 id="directory-people" className="text-sm font-semibold">
              {t('directory.people')}
            </h2>
            <p className="mt-1 mb-3 text-sm text-muted-foreground">{t(me.permissions.admin ? 'directory.peopleHintAdmin' : 'directory.peopleHint')}</p>
            {canAdd ? <AddPerson admin={me.permissions.admin} /> : null}
            {data.people.length === 0 ? (
              <EmptyState icon={<UsersIcon />} title={t('directory.empty')} body={t('directory.emptyBody')} />
            ) : (
              <ul className="divide-y divide-border rounded-xl border border-border bg-card" aria-label={t('directory.people')}>
                {data.people.map((p) => (
                  <PersonRow key={p.id} person={p} users={data.users} />
                ))}
              </ul>
            )}
          </section>
        </div>
      )}
    </Page>
  )
}

function useRefresh() {
  const qc = useQueryClient()
  return () => {
    void qc.invalidateQueries({ queryKey: keys.people })
    void qc.invalidateQueries({ queryKey: keys.eventsAll })
    void qc.invalidateQueries({ queryKey: keys.eventAll })
  }
}

function AddPerson({ admin }: { admin: boolean }) {
  const { t } = useTranslation()
  const refresh = useRefresh()
  const uid = useId()
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    const n = name.trim()
    if (!n || busy) return
    setBusy(true)
    try {
      await endpoints.createPerson(n)
      toast.success(t(admin ? 'directory.added' : 'directory.submitted', { name: n }))
      setName('')
      refresh()
    } catch (err) {
      toast.error(t('directory.addFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }
  return (
    <form
      className="mb-3 flex flex-wrap items-end gap-2"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <div className="grid min-w-48 flex-1 gap-1.5">
        <Label htmlFor={uid}>{t('directory.name')}</Label>
        <Input id={uid} value={name} maxLength={MAX_NAME} onChange={(e) => setName(e.target.value)} />
      </div>
      <Button type="submit" disabled={busy || !name.trim()}>
        <PlusIcon aria-hidden />
        {t(admin ? 'directory.add' : 'directory.submit')}
      </Button>
    </form>
  )
}

function PersonRow({ person: p, users }: { person: Person; users: UserRef[] }) {
  const { t } = useTranslation()
  const refresh = useRefresh()
  const uid = useId()
  const [mode, setMode] = useState<'idle' | 'rename' | 'bind'>('idle')
  const [name, setName] = useState(p.name)
  const [userId, setUserId] = useState('')
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [busy, setBusy] = useState(false)
  const pending = p.status === 'pending'

  const run = async (job: () => Promise<unknown>, failed: string, done: string) => {
    if (busy) return
    setBusy(true)
    try {
      await job()
      toast.success(done)
      setMode('idle')
      refresh()
    } catch (err) {
      toast.error(failed, errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <li className="grid gap-3 px-3 py-3">
      <div className="flex flex-wrap items-center gap-3">
        <UserAvatar name={p.name} className="size-8" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium break-words">{p.name}</span>
            {pending ? <Badge variant="secondary">{t('directory.pending')}</Badge> : null}
          </div>
          {p.createdBy ? <p className="text-xs text-muted-foreground">{t('directory.addedBy', { name: p.createdBy.name })}</p> : null}
        </div>
        {mode === 'idle' ? (
          <div className="flex flex-wrap gap-1.5">
            {p.permissions.manage && pending ? (
              <Button
                size="sm"
                disabled={busy}
                aria-label={t('directory.approveNamed', { name: p.name })}
                onClick={() => void run(() => endpoints.approvePerson(p.id), t('directory.approveFailed'), t('directory.approved', { name: p.name }))}
              >
                <CheckIcon aria-hidden />
                {t('directory.approve')}
              </Button>
            ) : null}
            {p.permissions.edit ? (
              <Button size="sm" variant="outline" aria-label={t('directory.renameNamed', { name: p.name })} onClick={() => setMode('rename')}>
                <PencilIcon aria-hidden />
                {t('directory.rename')}
              </Button>
            ) : null}
            {p.permissions.manage && !pending ? (
              <Button size="sm" variant="outline" aria-label={t('directory.linkNamed', { name: p.name })} onClick={() => setMode('bind')}>
                <LinkIcon aria-hidden />
                {t('directory.link')}
              </Button>
            ) : null}
            {p.permissions.manage ? (
              <Button
                size="sm"
                variant="outline"
                aria-label={t(pending ? 'directory.rejectNamed' : 'directory.deleteNamed', { name: p.name })}
                onClick={() => setConfirmDelete(true)}
              >
                <Trash2Icon aria-hidden />
                {t(pending ? 'directory.reject' : 'common.delete')}
              </Button>
            ) : null}
          </div>
        ) : null}
      </div>
      {mode === 'rename' ? (
        <form
          className="flex flex-wrap items-end gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            const n = name.trim()
            if (n && n !== p.name) void run(() => endpoints.renamePerson(p.id, n), t('directory.renameFailed'), t('directory.renamed'))
            else setMode('idle')
          }}
        >
          <div className="grid min-w-48 flex-1 gap-1.5">
            <Label htmlFor={`${uid}-name`}>{t('directory.name')}</Label>
            <Input id={`${uid}-name`} value={name} maxLength={MAX_NAME} onChange={(e) => setName(e.target.value)} />
          </div>
          <Button type="submit" size="sm" disabled={busy || !name.trim()}>
            {t('common.save')}
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setMode('idle')}>
            {t('common.cancel')}
          </Button>
        </form>
      ) : null}
      {mode === 'bind' ? (
        <div className="grid gap-2 rounded-lg bg-muted/50 p-3">
          <p className="text-sm text-muted-foreground">{t('directory.linkHint', { name: p.name })}</p>
          <div className="flex flex-wrap items-end gap-2">
            <div className="grid min-w-48 flex-1 gap-1.5">
              <Label htmlFor={`${uid}-user`}>{t('directory.user')}</Label>
              <NativeSelect id={`${uid}-user`} value={userId} onChange={(e) => setUserId(e.target.value)}>
                <option value="">{t('directory.chooseUser')}</option>
                {users
                  .filter((u) => !u.system)
                  .map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.name}
                    </option>
                  ))}
              </NativeSelect>
            </div>
            <Button
              size="sm"
              disabled={busy || !userId}
              onClick={() => {
                const who = users.find((u) => u.id === userId)?.name ?? ''
                void run(() => endpoints.bindPerson(p.id, userId), t('directory.linkFailed'), t('directory.linked', { name: p.name, user: who }))
              }}
            >
              {t('directory.link')}
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setMode('idle')}>
              {t('common.cancel')}
            </Button>
          </div>
        </div>
      ) : null}
      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t(pending ? 'directory.rejectTitle' : 'directory.deleteTitle', { name: p.name })}
        description={t('directory.deleteBody')}
        confirmLabel={t(pending ? 'directory.reject' : 'common.delete')}
        destructive
        onConfirm={() => void run(() => endpoints.deletePerson(p.id), t('directory.deleteFailed'), t('directory.deleted', { name: p.name }))}
      />
    </li>
  )
}
