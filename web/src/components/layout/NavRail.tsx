import { Link } from '@tanstack/react-router'
import { LogOutIcon, PinIcon, PinOffIcon } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMeData } from '@/api/queries'
import { UserAvatar } from '@/components/ui/avatar'
import { Tooltip } from '@/components/ui/tooltip'
import { readLocal, STORAGE_KEYS, writeLocal } from '@/lib/storage'
import { cn } from '@/lib/utils'
import { InboxBadge, useUnreadCount } from './InboxBadge'
import { ADMIN_NAV, MAIN_NAV, MY_ACTIVITY_NAV, type NavItem } from './navItems'
import { signOut } from './signOut'

export function useRailPinned() {
  const [pinned, setPinned] = useState(() => readLocal<boolean>(STORAGE_KEYS.railPinned, false))
  const toggle = () =>
    setPinned((p) => {
      writeLocal(STORAGE_KEYS.railPinned, !p)
      return !p
    })
  return [pinned, toggle] as const
}

function RailLink({ item, expanded }: { item: NavItem; expanded: boolean }) {
  const { t } = useTranslation()
  const label = t(item.labelKey)
  const unread = useUnreadCount()
  const Icon = item.icon
  return (
    <li>
      <Tooltip content={label} side="right" disabled={expanded}>
        <Link
          to={item.to}
          activeOptions={{ exact: item.to === '/', includeSearch: false }}
          aria-label={expanded ? undefined : label}
          className={cn(
            'group relative flex h-10 items-center gap-3 rounded-lg px-2.5 text-sm font-medium text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50',
            'data-[status=active]:bg-accent data-[status=active]:text-foreground',
          )}
        >
          <Icon className="size-5 shrink-0" aria-hidden />
          <span className={cn('truncate transition-opacity duration-150', expanded ? 'opacity-100' : 'pointer-events-none w-0 opacity-0')}>{label}</span>
          {item.badge === 'inbox' && unread > 0 ? <InboxBadge className={cn('absolute', expanded ? 'end-2' : 'end-0.5 top-0.5')} /> : null}
        </Link>
      </Tooltip>
    </li>
  )
}

/** 56 px icon rail that expands to 240 px on hover, focus or pin. */
export function NavRail({ pinned, onTogglePin }: { pinned: boolean; onTogglePin: () => void }) {
  const { t } = useTranslation()
  const me = useMeData()
  const [hover, setHover] = useState(false)
  // Hover intent: expand only when the pointer rests on the rail, collapse at once on leave.
  const hoverTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(hoverTimer.current), [])
  const [focusWithin, setFocusWithin] = useState(false)
  const expanded = pinned || hover || focusWithin
  const name = me.instance.calendarName || t('app.name')

  return (
    <nav
      aria-label={t('nav.main')}
      onMouseEnter={() => {
        clearTimeout(hoverTimer.current)
        hoverTimer.current = setTimeout(() => setHover(true), 200)
      }}
      onMouseLeave={() => {
        clearTimeout(hoverTimer.current)
        setHover(false)
      }}
      onFocus={() => setFocusWithin(true)}
      onBlur={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setFocusWithin(false)
      }}
      className={cn(
        'fixed inset-y-0 start-0 z-40 hidden flex-col border-e border-border bg-card py-3 transition-[width,box-shadow] duration-150 md:flex',
        expanded ? 'w-(--rail-expanded)' : 'w-(--rail-collapsed)',
        expanded && !pinned && 'shadow-xl',
      )}
    >
      <div className="flex h-10 items-center gap-3 px-3">
        <Link
          to="/"
          className="flex min-w-0 items-center gap-3 rounded-lg outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          aria-label={expanded ? undefined : name}
        >
          {me.instance.hasLogo && me.instance.logoUrl ? (
            <img src={me.instance.logoUrl} alt="" className="size-8 shrink-0 rounded-lg object-contain" />
          ) : (
            <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground" aria-hidden>
              <svg viewBox="0 0 24 24" className="size-4.5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                <rect x="3.5" y="5" width="17" height="15" rx="3" />
                <path d="M3.5 10h17M8 3v4M16 3v4" />
              </svg>
            </span>
          )}
          <span className={cn('truncate text-sm font-semibold transition-opacity', expanded ? 'opacity-100' : 'w-0 opacity-0')}>{name}</span>
        </Link>
      </div>

      <div className="mt-4 flex flex-1 flex-col overflow-y-auto overflow-x-hidden px-2">
        <ul className="flex flex-col gap-1">
          {MAIN_NAV.map((item) => (
            <RailLink key={item.to} item={item} expanded={expanded} />
          ))}
          {!me.permissions.admin ? <RailLink item={MY_ACTIVITY_NAV} expanded={expanded} /> : null}
        </ul>
        {me.permissions.admin ? (
          <>
            <div className="mx-2 my-3 h-px bg-border" role="separator" />
            <p className={cn('mb-1 px-2.5 text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase', !expanded && 'sr-only')}>
              {t('nav.admin')}
            </p>
            <ul className="flex flex-col gap-1" aria-label={t('nav.admin')}>
              {ADMIN_NAV.map((item) => (
                <RailLink key={item.to} item={item} expanded={expanded} />
              ))}
            </ul>
          </>
        ) : null}
      </div>

      <div className="flex flex-col gap-1 px-2 pt-2">
        <Tooltip content={pinned ? t('nav.unpin') : t('nav.pin')} side="right" disabled={expanded}>
          <button
            type="button"
            onClick={onTogglePin}
            aria-pressed={pinned}
            aria-label={pinned ? t('nav.unpin') : t('nav.pin')}
            className="flex h-10 items-center gap-3 rounded-lg px-2.5 text-sm text-muted-foreground outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            {pinned ? <PinOffIcon className="size-5 shrink-0" aria-hidden /> : <PinIcon className="size-5 shrink-0" aria-hidden />}
            <span className={cn('truncate', !expanded && 'w-0 opacity-0')} aria-hidden>
              {pinned ? t('nav.unpin') : t('nav.pin')}
            </span>
          </button>
        </Tooltip>
        <div className="flex h-12 items-center gap-3 rounded-lg px-1.5">
          <UserAvatar name={me.user.name} src={me.user.avatarUrl} className="size-8" />
          <div className={cn('min-w-0 flex-1', !expanded && 'sr-only')}>
            <p className="truncate text-sm font-medium">{me.user.name}</p>
            <p className="truncate text-xs text-muted-foreground">{t(`roles.${me.user.role}`)}</p>
          </div>
          <Tooltip content={t('nav.signOut')} side="right" disabled={expanded}>
            <button
              type="button"
              onClick={() => void signOut()}
              aria-label={t('nav.signOut')}
              className={cn(
                'flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50',
                !expanded && 'sr-only-focusable',
              )}
            >
              <LogOutIcon className="size-4" aria-hidden />
            </button>
          </Tooltip>
        </div>
      </div>
    </nav>
  )
}
