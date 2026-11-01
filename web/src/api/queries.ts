import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { useCallback } from 'react'
import { setCsrfToken } from './client'
import { endpoints, type RangeParams } from './endpoints'
import { keys } from './keys'
import { firstNameOf, type NameOf } from '@/lib/titleTemplate'
import type { Me } from './types'

async function fetchMe(): Promise<Me> {
  const me = await endpoints.me()
  setCsrfToken(me.csrfToken)
  return me
}

export function useMe() {
  return useQuery({ queryKey: keys.me, queryFn: fetchMe, staleTime: 60_000, retry: 1 })
}

/** The signed-in user. Only call below <RequireMe>, which guarantees it is loaded. */
export function useMeData(): Me {
  const { data } = useMe()
  if (!data) throw new Error('useMeData called before /api/me loaded')
  return data
}

export function useEvents(params: RangeParams | null) {
  return useQuery({
    queryKey: params ? keys.events(params) : ['events', 'none'],
    queryFn: ({ signal }) => endpoints.events(params as RangeParams, signal),
    enabled: !!params,
    placeholderData: keepPreviousData,
    staleTime: 30_000,
  })
}

export function useEventDetail(id: string | undefined) {
  return useQuery({
    queryKey: keys.event(id ?? ''),
    queryFn: () => endpoints.event(id as string),
    enabled: !!id,
    retry: (count, err) => count < 2 && !(err instanceof Error && 'status' in err && (err.status === 404 || err.status === 403)),
  })
}

export function useComments(eventId: string, enabled = true) {
  return useQuery({ queryKey: keys.comments(eventId), queryFn: () => endpoints.comments(eventId), enabled })
}

export function useSuggestions(eventId: string, status: 'open' | 'all', enabled = true) {
  return useQuery({
    queryKey: keys.suggestions(eventId, status),
    queryFn: () => endpoints.suggestions(eventId, status),
    enabled,
  })
}

export function useHistory(eventId: string, enabled = true) {
  return useQuery({ queryKey: keys.history(eventId), queryFn: () => endpoints.history(eventId), enabled })
}

export function useTags() {
  return useQuery({ queryKey: keys.tags, queryFn: endpoints.tags, staleTime: 5 * 60_000 })
}

export function useEventTypes() {
  return useQuery({ queryKey: keys.eventTypes, queryFn: endpoints.eventTypes, staleTime: 5 * 60_000 })
}

export function useCatalogRequests(status: 'open' | 'all') {
  return useQuery({ queryKey: keys.catalogRequests(status), queryFn: () => endpoints.catalogRequests(status), staleTime: 60_000 })
}

/** How many requests wait for review. Admins only. */
export function useOpenCatalogCount(enabled: boolean) {
  return useQuery({ queryKey: keys.catalogCount, queryFn: endpoints.catalogRequestCount, enabled, staleTime: 60_000 })
}

/** Registered users plus people without accounts (a writer's own pending ones included). */
export function usePeople() {
  return useQuery({ queryKey: keys.people, queryFn: endpoints.people, staleTime: 60_000 })
}

/** Looks up the name of a user or person id, as shown in members fields and generated titles. */
export function useMemberName(): NameOf {
  const { data } = usePeople()
  return useCallback(
    (id, firstOnly) => {
      const user = data?.users.find((u) => u.id === id)
      if (user) return firstOnly ? firstNameOf(user.name) : user.name
      const person = data?.people.find((p) => p.id === id)
      return firstOnly ? person?.firstName || person?.name : person?.name
    },
    [data],
  )
}

export function usePendingPeopleCount(enabled: boolean) {
  return useQuery({ queryKey: keys.peopleCount, queryFn: endpoints.pendingPeopleCount, enabled, staleTime: 60_000 })
}

export function useDirectory() {
  return useQuery({ queryKey: keys.directory, queryFn: endpoints.directory, staleTime: 5 * 60_000 })
}

export function useInbox() {
  return useQuery({ queryKey: keys.inbox, queryFn: () => endpoints.inbox(), staleTime: 60_000 })
}

export function useThemes(all = false) {
  return useQuery({ queryKey: keys.themes(all), queryFn: () => endpoints.themes(all), staleTime: 10 * 60_000 })
}
