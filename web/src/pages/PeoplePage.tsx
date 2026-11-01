import { useQueryClient } from '@tanstack/react-query'
import { CheckIcon, LinkIcon, PencilIcon, PlusIcon, SearchIcon, Trash2Icon, UsersIcon, XIcon } from 'lucide-react'
import { useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useMeData, usePeople } from '@/api/queries'
import type { Person, PersonName, UserRef } from '@/api/types'
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

const MAX_NAME = 60

/** Lowercase and accent-free, so "jose" finds "José". */
const fold = (s: string) => s.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase()

/** Every word typed must appear somewhere in the name. */
const matches = (name: string, terms: string[]) => {
  const n = fold(name)
  return terms.every((w) => n.includes(w))
}

/** Registered users, and the people events can name who have no account yet. */
export function PeoplePage() {
  const { t } = useTranslation()
  const me = useMeData()
  const { data, isPending, isError, refetch } = usePeople()
  const canAdd = me.permissions.createEvent
  const [query, setQuery] = useState('')
  const terms = useMemo(() => fold(query).split(/\s+/).filter(Boolean), [query])
  const users = useMemo(() => (data?.users ?? []).filter((u) => matches(u.name, terms)), [data, terms])
  // Waiting people first, so an admin sees what needs approving at the top.
  const people = useMemo(
    () => (data?.people ?? []).filter((p) => matches(p.name, terms)).sort((a, b) => Number(b.status === 'pending') - Number(a.status === 'pending')),
    [data, terms],
  )
  // While searching, a section with no matches is left out; with nothing to search, both stay.
  const searching = terms.length > 0
  const showUsers = users.length > 0 || !searching
  const showPeople = people.length > 0 || !searching
  return (
    <Page wide>
      <PageHeader title={t('directory.title')} description={t('directory.description')} />
      {isPending ? (
        <Skeleton className="h-40" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : (
        <div className="grid gap-6">
          <SearchField value={query} onChange={setQuery} />
          {!showUsers && !showPeople ? <EmptyState icon={<SearchIcon />} title={t('directory.noMatches', { query: query.trim() })} /> : null}
          {showUsers ? (
            <section aria-labelledby="directory-users">
              <h2 id="directory-users" className="mb-2 text-sm font-semibold">
                {t('directory.users')}
              </h2>
              <ul className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3" aria-labelledby="directory-users">
                {users.map((u) => (
                  <li key={u.id} className="flex items-center gap-2.5 rounded-xl border border-border bg-card px-3 py-2">
                    <UserAvatar name={u.name} className="size-7" />
                    <span className="truncate text-sm font-medium">{u.name}</span>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
          {showPeople ? (
            <section aria-labelledby="directory-people">
              <h2 id="directory-people" className="text-sm font-semibold">
                {t('directory.people')}
              </h2>
              <p className="mt-1 mb-3 text-sm text-muted-foreground">{t(me.permissions.admin ? 'directory.peopleHintAdmin' : 'directory.peopleHint')}</p>
              {canAdd ? <AddPerson admin={me.permissions.admin} /> : null}
              {people.length === 0 ? (
                <EmptyState icon={<UsersIcon />} title={t('directory.empty')} body={t('directory.emptyBody')} />
              ) : (
                <ul className="divide-y divide-border rounded-xl border border-border bg-card" aria-labelledby="directory-people">
                  {people.map((p) => (
                    <PersonRow key={p.id} person={p} users={data.users} />
                  ))}
                </ul>
              )}
            </section>
          ) : null}
        </div>
      )}
    </Page>
  )
}

function SearchField({ value, onChange }: { value: string; onChange: (q: string) => void }) {
  const { t } = useTranslation()
  return (
    <div role="search" className="relative">
      <SearchIcon className="pointer-events-none absolute start-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" aria-hidden />
      <Input
        type="search"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={t('directory.search')}
        aria-label={t('directory.search')}
        className="ps-8 pe-8 [&::-webkit-search-cancel-button]:hidden"
      />
      {value ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          className="absolute end-1 top-1/2 -translate-y-1/2"
          aria-label={t('directory.clearSearch')}
          onClick={() => onChange('')}
        >
          <XIcon aria-hidden />
        </Button>
      ) : null}
    </div>
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

/** First and last name, both required. */
function NameFields({ idBase, value, onChange }: { idBase: string; value: PersonName; onChange: (v: PersonName) => void }) {
  const { t } = useTranslation()
  return (
    <>
      <div className="grid min-w-40 flex-1 gap-1.5">
        <Label htmlFor={`${idBase}-first`}>{t('directory.firstName')}</Label>
        <Input id={`${idBase}-first`} value={value.firstName} maxLength={MAX_NAME} onChange={(e) => onChange({ ...value, firstName: e.target.value })} />
      </div>
      <div className="grid min-w-40 flex-1 gap-1.5">
        <Label htmlFor={`${idBase}-last`}>{t('directory.lastName')}</Label>
        <Input id={`${idBase}-last`} value={value.lastName} maxLength={MAX_NAME} onChange={(e) => onChange({ ...value, lastName: e.target.value })} />
      </div>
    </>
  )
}

const trimmed = (v: PersonName): PersonName => ({ firstName: v.firstName.trim(), lastName: v.lastName.trim() })
const complete = (v: PersonName) => !!v.firstName.trim() && !!v.lastName.trim()

function AddPerson({ admin }: { admin: boolean }) {
  const { t } = useTranslation()
  const refresh = useRefresh()
  const uid = useId()
  const [name, setName] = useState<PersonName>({ firstName: '', lastName: '' })
  const [busy, setBusy] = useState(false)
  const submit = async () => {
    const n = trimmed(name)
    if (!complete(n) || busy) return
    setBusy(true)
    try {
      await endpoints.createPerson(n)
      toast.success(t(admin ? 'directory.added' : 'directory.submitted', { name: `${n.firstName} ${n.lastName}` }))
      setName({ firstName: '', lastName: '' })
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
      <NameFields idBase={uid} value={name} onChange={setName} />
      <Button type="submit" disabled={busy || !complete(name)}>
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
  const [name, setName] = useState<PersonName>({ firstName: p.firstName, lastName: p.lastName })
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
            const n = trimmed(name)
            if (complete(n) && (n.firstName !== p.firstName || n.lastName !== p.lastName))
              void run(() => endpoints.renamePerson(p.id, n), t('directory.renameFailed'), t('directory.renamed'))
            else setMode('idle')
          }}
        >
          <NameFields idBase={`${uid}-name`} value={name} onChange={setName} />
          <Button type="submit" size="sm" disabled={busy || !complete(name)}>
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
