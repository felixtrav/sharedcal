import { Link, useNavigate } from '@tanstack/react-router'
import { LogOutIcon, MenuIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useMeData } from '@/api/queries'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'
import { InboxBadge } from './InboxBadge'
import { ADMIN_NAV, MAIN_NAV, MY_ACTIVITY_NAV } from './navItems'
import { signOut } from './signOut'

const itemClass =
  'relative flex flex-1 flex-col items-center justify-center gap-0.5 rounded-lg py-1.5 text-[0.6875rem] font-medium text-muted-foreground outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 data-[status=active]:text-foreground'

/** Under 768 px the rail becomes this bottom bar. */
export function BottomBar() {
  const { t } = useTranslation()
  const me = useMeData()
  const navigate = useNavigate()
  const more = me.permissions.admin ? ADMIN_NAV : [MY_ACTIVITY_NAV]

  return (
    <nav
      aria-label={t('nav.main')}
      className="fixed inset-x-0 bottom-0 z-40 flex h-16 items-stretch gap-1 border-t border-border bg-card/95 px-2 pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
    >
      {MAIN_NAV.map((item) => {
        const Icon = item.icon
        return (
          <Link key={item.to} to={item.to} activeOptions={{ exact: item.to === '/', includeSearch: false }} className={itemClass}>
            <span className="relative">
              <Icon className="size-5" aria-hidden />
              {item.badge === 'inbox' ? <InboxBadge className="absolute -end-2.5 -top-1.5" /> : null}
            </span>
            <span>{t(item.labelKey)}</span>
          </Link>
        )
      })}
      <DropdownMenu>
        <DropdownMenuTrigger className={cn(itemClass)} aria-label={t('nav.more')}>
          <MenuIcon className="size-5" aria-hidden />
          <span aria-hidden>{t('nav.more')}</span>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" side="top" className="w-56">
          <DropdownMenuLabel>{me.user.name}</DropdownMenuLabel>
          <DropdownMenuSeparator />
          {more.map((item) => {
            const Icon = item.icon
            return (
              <DropdownMenuItem key={item.to} onSelect={() => void navigate({ to: item.to })}>
                <Icon aria-hidden />
                {t(item.labelKey)}
              </DropdownMenuItem>
            )
          })}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => void signOut()}>
            <LogOutIcon aria-hidden />
            {t('nav.signOut')}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </nav>
  )
}
