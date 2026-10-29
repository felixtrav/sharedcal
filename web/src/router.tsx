import { createRootRoute, createRoute, createRouter, lazyRouteComponent, Outlet } from '@tanstack/react-router'
import type { ComponentType } from 'react'
import { useMeData } from '@/api/queries'
import { AppShell } from '@/components/layout/AppShell'
import { RequireMe } from '@/components/layout/RequireMe'
import { NotFound } from '@/components/layout/NotFound'
import { RouteError } from '@/components/layout/RouteError'
import { CalendarPage } from '@/components/calendar/CalendarPage'
import { validateCalendarSearch } from '@/lib/search'

/** Admin pages show Not Found to everyone else (the API refuses them anyway). */
function adminOnly(Page: ComponentType) {
  return function AdminOnly() {
    return useMeData().permissions.admin ? <Page /> : <NotFound />
  }
}

const rootRoute = createRootRoute({
  component: () => (
    <RequireMe>
      <AppShell>
        <Outlet />
      </AppShell>
    </RequireMe>
  ),
  notFoundComponent: NotFound,
})

const calendarRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  validateSearch: validateCalendarSearch,
  component: CalendarPage,
})

const inboxRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/inbox',
  component: lazyRouteComponent(() => import('@/pages/InboxPage'), 'InboxPage'),
})

const preferencesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/preferences',
  component: lazyRouteComponent(() => import('@/pages/PreferencesPage'), 'PreferencesPage'),
})

const subscribeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/subscribe',
  component: lazyRouteComponent(() => import('@/pages/SubscribePage'), 'SubscribePage'),
})

const activityRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/activity',
  component: lazyRouteComponent(() => import('@/pages/ActivityPage'), 'ActivityPage'),
})

const peopleRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/people',
  component: adminOnly(lazyRouteComponent(() => import('@/pages/admin/PeoplePage'), 'PeoplePage')),
})

const tagsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/tags',
  component: adminOnly(lazyRouteComponent(() => import('@/pages/admin/TagsPage'), 'TagsPage')),
})

const eventTypesRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/event-types',
  component: adminOnly(lazyRouteComponent(() => import('@/pages/admin/EventTypesPage'), 'EventTypesPage')),
})

const importRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/import',
  component: adminOnly(lazyRouteComponent(() => import('@/pages/admin/ImportPage'), 'ImportPage')),
})

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/settings',
  component: adminOnly(lazyRouteComponent(() => import('@/pages/admin/SettingsPage'), 'SettingsPage')),
})

const trashRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/admin/trash',
  component: adminOnly(lazyRouteComponent(() => import('@/pages/admin/TrashPage'), 'TrashPage')),
})

const routeTree = rootRoute.addChildren([
  calendarRoute,
  inboxRoute,
  preferencesRoute,
  subscribeRoute,
  activityRoute,
  peopleRoute,
  tagsRoute,
  eventTypesRoute,
  importRoute,
  settingsRoute,
  trashRoute,
])

export const router = createRouter({ routeTree, defaultPreload: 'intent', scrollRestoration: true, defaultErrorComponent: RouteError })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}
