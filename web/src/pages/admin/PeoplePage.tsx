import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowRightLeftIcon, MonitorSmartphoneIcon, RotateCcwIcon, UserXIcon, UsersIcon } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { isApiError } from '@/api/client'
import { endpoints } from '@/api/endpoints'
import { keys } from '@/api/keys'
import { useMeData } from '@/api/queries'
import type { AdminUser, Role } from '@/api/types'
import { EmptyState, LoadError, Page, PageHeader } from '@/components/layout/PageHeader'
import { ConfirmDialog } from '@/components/ui/alert-dialog'
import { UserAvatar } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { NativeSelect } from '@/components/ui/input'
import { Select } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/components/ui/toast'
import { Tooltip } from '@/components/ui/tooltip'
import { errorMessage } from '@/lib/errors'
import { formatDateTime, formatRelative } from '@/lib/time'
import { useFmt } from '@/lib/useFmt'

type UsersData = { users: AdminUser[] }

export function PeoplePage() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const me = useMeData()
  const { data, isPending, isError, refetch } = useQuery({
    queryKey: keys.users,
    queryFn: endpoints.users,
  })
  const [sessionsFor, setSessionsFor] = useState<AdminUser | null>(null)
  const [transferFrom, setTransferFrom] = useState<AdminUser | null>(null)
  const [eraseUser, setEraseUser] = useState<AdminUser | null>(null)

  const update = async (u: AdminUser, patch: Parameters<typeof endpoints.updateUser>[1]) => {
    const prev = qc.getQueryData<UsersData>(keys.users)
    qc.setQueryData<UsersData>(keys.users, (d) =>
      d
        ? {
            users: d.users.map((x) => (x.id === u.id ? { ...x, ...patch } : x)),
          }
        : d,
    )
    try {
      const saved = await endpoints.updateUser(u.id, patch)
      qc.setQueryData<UsersData>(keys.users, (d) => (d ? { users: d.users.map((x) => (x.id === u.id ? saved : x)) } : d))
      if (u.id === me.user.id) void qc.invalidateQueries({ queryKey: keys.me })
    } catch (err) {
      if (prev) qc.setQueryData(keys.users, prev)
      if (isApiError(err) && err.status === 409) toast.error(t('people.lastAdminTitle'), err.problem.detail ?? t('people.lastAdminBody'))
      else toast.error(t('people.updateFailed'), errorMessage(err, t))
    }
  }

  const roles: { value: Role; label: string }[] = [
    { value: 'viewer', label: t('roles.viewer') },
    { value: 'writer', label: t('roles.writer') },
    { value: 'admin', label: t('roles.admin') },
  ]

  return (
    <Page wide>
      <PageHeader title={t('people.title')} description={t('people.description')} />
      {isPending ? (
        <Skeleton className="h-60" />
      ) : isError ? (
        <LoadError onRetry={() => void refetch()} />
      ) : data.users.length === 0 ? (
        <EmptyState icon={<UsersIcon />} title={t('people.empty')} />
      ) : (
        <div className="overflow-x-auto rounded-xl border border-border bg-card">
          <table className="w-full min-w-[52rem] text-sm">
            <caption className="sr-only">{t('people.title')}</caption>
            <thead className="border-b border-border text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="px-4 py-3 text-start font-medium">
                  {t('people.user')}
                </th>
                <th scope="col" className="px-4 py-3 text-start font-medium">
                  {t('people.role')}
                </th>
                <th scope="col" className="px-4 py-3 text-start font-medium">
                  {t('people.roleSource')}
                </th>
                <th scope="col" className="px-4 py-3 text-start font-medium">
                  {t('people.active')}
                </th>
                <th scope="col" className="px-4 py-3 text-start font-medium">
                  {t('people.lastLogin')}
                </th>
                <th scope="col" className="px-4 py-3 text-end font-medium">
                  <span className="sr-only">{t('people.actions')}</span>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border">
              {data.users.map((u) => (
                <tr key={u.id} className={u.active ? undefined : 'opacity-60'}>
                  <th scope="row" className="px-4 py-3 text-start font-normal">
                    <div className="flex items-center gap-3">
                      <UserAvatar name={u.name} src={u.avatarUrl} />
                      <div className="min-w-0">
                        <p className="truncate font-medium">
                          {u.name} {u.id === me.user.id ? <span className="text-xs text-muted-foreground">({t('people.you')})</span> : null}
                          {u.erased ? (
                            <Badge variant="outline" className="ms-2">
                              {t('people.erased')}
                            </Badge>
                          ) : null}
                        </p>
                        <p className="truncate text-xs text-muted-foreground">{u.email}</p>
                        {u.groups.length ? <p className="truncate text-xs text-muted-foreground">{u.groups.join(', ')}</p> : null}
                      </div>
                    </div>
                  </th>
                  <td className="px-4 py-3">
                    <Select
                      aria-label={t('people.roleFor', { name: u.name })}
                      value={u.role}
                      onValueChange={(v) =>
                        void update(u, {
                          role: v as Role,
                          roleSource: 'manual',
                        })
                      }
                      options={roles}
                      className="w-32"
                      disabled={u.erased}
                    />
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <Badge variant={u.roleSource === 'manual' ? 'secondary' : 'outline'}>
                        {u.roleSource === 'manual' ? t('people.sourceManual') : t('people.sourceIdp')}
                      </Badge>
                      {u.roleSource === 'manual' && !u.erased ? (
                        <Tooltip content={t('people.resetToIdp')}>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => void update(u, { roleSource: 'idp' })}
                            aria-label={t('people.resetToIdpFor', {
                              name: u.name,
                            })}
                          >
                            <RotateCcwIcon />
                          </Button>
                        </Tooltip>
                      ) : null}
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <Switch
                      checked={u.active}
                      disabled={u.erased}
                      onCheckedChange={(v) => void update(u, { active: v })}
                      aria-label={t('people.activeFor', { name: u.name })}
                    />
                  </td>
                  <td className="px-4 py-3 text-muted-foreground">
                    {u.lastLoginAt ? <time dateTime={u.lastLoginAt}>{formatRelative(u.lastLoginAt)}</time> : t('people.never')}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-1">
                      {u.erased ? null : (
                        <>
                          <Button variant="ghost" size="xs" onClick={() => setSessionsFor(u)}>
                            <MonitorSmartphoneIcon aria-hidden />
                            {t('people.sessions', { count: u.sessionCount })}
                          </Button>
                          <Button variant="ghost" size="xs" onClick={() => setTransferFrom(u)}>
                            <ArrowRightLeftIcon aria-hidden />
                            {t('people.transfer')}
                          </Button>
                          {u.id !== me.user.id ? (
                            <Button
                              variant="ghost"
                              size="xs"
                              className="text-destructive"
                              onClick={() => setEraseUser(u)}
                              aria-label={t('people.eraseFor', {
                                name: u.name,
                              })}
                            >
                              <UserXIcon aria-hidden />
                              {t('people.erase')}
                            </Button>
                          ) : null}
                        </>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {sessionsFor ? <SessionsDialog user={sessionsFor} onClose={() => setSessionsFor(null)} /> : null}
      {transferFrom && data ? <TransferDialog from={transferFrom} users={data.users} onClose={() => setTransferFrom(null)} /> : null}
      {eraseUser && data ? <EraseDialog user={eraseUser} users={data.users} onClose={() => setEraseUser(null)} /> : null}
    </Page>
  )
}

function SessionsDialog({ user, onClose }: { user: AdminUser; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const fmt = useFmt()
  const { data, isPending } = useQuery({
    queryKey: keys.sessions(user.id),
    queryFn: () => endpoints.sessions(user.id),
  })
  const [confirmAll, setConfirmAll] = useState(false)

  const revoke = async (sessionId?: string) => {
    try {
      await endpoints.revokeSessions(user.id, sessionId)
      void qc.invalidateQueries({ queryKey: keys.sessions(user.id) })
      void qc.invalidateQueries({ queryKey: keys.users })
      toast({
        title: sessionId ? t('people.sessionRevoked') : t('people.allSessionsRevoked'),
      })
    } catch (err) {
      toast.error(t('people.revokeFailed'), errorMessage(err, t))
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{t('people.sessionsTitle', { name: user.name })}</DialogTitle>
          <DialogDescription>{t('people.sessionsDescription')}</DialogDescription>
        </DialogHeader>
        {isPending ? (
          <Skeleton className="h-24" />
        ) : !data?.sessions.length ? (
          <p className="text-sm text-muted-foreground">{t('people.noSessions')}</p>
        ) : (
          <ul className="grid gap-2">
            {data.sessions.map((s) => (
              <li key={s.id} className="flex items-start gap-3 rounded-lg border border-border p-3 text-sm">
                <div className="min-w-0 flex-1">
                  <p className="truncate font-medium" title={s.userAgent}>
                    {s.userAgent || t('people.unknownDevice')}
                    {s.current ? <Badge className="ms-2">{t('people.currentSession')}</Badge> : null}
                  </p>
                  <p className="text-xs text-muted-foreground tabular-nums">
                    {s.ip} ·{' '}
                    {t('people.lastSeen', {
                      when: formatRelative(s.lastSeenAt),
                    })}{' '}
                    ·{' '}
                    {t('people.expires', {
                      when: formatDateTime(Date.parse(s.expiresAt), fmt),
                    })}
                  </p>
                </div>
                <Button variant="outline" size="xs" onClick={() => void revoke(s.id)}>
                  {t('people.revoke')}
                </Button>
              </li>
            ))}
          </ul>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.close')}
          </Button>
          {data?.sessions.length ? (
            <Button variant="destructive" onClick={() => setConfirmAll(true)}>
              {t('people.revokeAll')}
            </Button>
          ) : null}
        </DialogFooter>
        <ConfirmDialog
          open={confirmAll}
          onOpenChange={setConfirmAll}
          title={t('people.revokeAllTitle', { name: user.name })}
          description={t('people.revokeAllBody')}
          confirmLabel={t('people.revokeAll')}
          destructive
          onConfirm={() => void revoke()}
        />
      </DialogContent>
    </Dialog>
  )
}

/** Who can take over someone's events: other active writers and admins (erased users are inactive). */
function transferTargets(users: AdminUser[], fromId: string) {
  return users.filter((u) => u.id !== fromId && u.active && u.role !== 'viewer')
}

function UserSelect({ id, users, value, onChange }: { id: string; users: AdminUser[]; value: string; onChange: (id: string) => void }) {
  return (
    <NativeSelect id={id} value={value} onChange={(e) => onChange(e.target.value)}>
      {users.map((u) => (
        <option key={u.id} value={u.id}>
          {u.name} ({u.email})
        </option>
      ))}
    </NativeSelect>
  )
}

function TransferDialog({ from, users, onClose }: { from: AdminUser; users: AdminUser[]; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const targets = transferTargets(users, from.id)
  const [to, setTo] = useState(targets[0]?.id ?? '')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    if (!to) return
    setBusy(true)
    try {
      const res = await endpoints.transferOwnership(from.id, to)
      void qc.invalidateQueries({ queryKey: keys.eventsAll })
      void qc.invalidateQueries({ queryKey: keys.eventAll })
      toast.success(t('people.transferred', { count: res.moved }))
      onClose()
    } catch (err) {
      toast.error(t('people.transferFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t('people.transferTitle', { name: from.name })}</DialogTitle>
          <DialogDescription>{t('people.transferDescription')}</DialogDescription>
        </DialogHeader>
        {targets.length ? (
          <div className="grid gap-1.5">
            <label htmlFor="transfer-to" className="text-sm font-medium">
              {t('people.transferTo')}
            </label>
            <UserSelect id="transfer-to" users={targets} value={to} onChange={setTo} />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">{t('people.noTransferTargets')}</p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button disabled={!to || busy} onClick={() => void submit()}>
            {t('people.transfer')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function EraseDialog({ user, users, onClose }: { user: AdminUser; users: AdminUser[]; onClose: () => void }) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const targets = transferTargets(users, user.id)
  const [to, setTo] = useState(targets[0]?.id ?? '')
  const [understood, setUnderstood] = useState(false)
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    try {
      // The server only uses transferTo when the user still owns events.
      const res = await endpoints.eraseUser(user.id, to || undefined)
      void qc.invalidateQueries({ queryKey: keys.users })
      void qc.invalidateQueries({ queryKey: keys.directory })
      void qc.invalidateQueries({ queryKey: keys.eventsAll })
      void qc.invalidateQueries({ queryKey: keys.eventAll })
      toast.success(t('people.erasedToast', { name: user.name }), t('people.erasedSummary', res))
      onClose()
    } catch (err) {
      toast.error(t('people.eraseFailed'), errorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{t('people.eraseTitle', { name: user.name })}</DialogTitle>
          <DialogDescription>{t('people.eraseDescription')}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-1.5">
          <label htmlFor="erase-transfer-to" className="text-sm font-medium">
            {t('people.eraseTransferTo')}
          </label>
          {targets.length ? (
            <UserSelect id="erase-transfer-to" users={targets} value={to} onChange={setTo} />
          ) : (
            <p className="text-sm text-muted-foreground">{t('people.noTransferTargets')}</p>
          )}
          <p className="text-xs text-muted-foreground">{t('people.eraseTransferHint')}</p>
        </div>
        <label className="flex items-start gap-2 text-sm">
          <Checkbox checked={understood} onCheckedChange={(v) => setUnderstood(v === true)} className="mt-0.5" />
          {t('people.eraseConfirm', { name: user.name })}
        </label>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" disabled={!understood || busy} onClick={() => void submit()}>
            {t('people.erase')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
