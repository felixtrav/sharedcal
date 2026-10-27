import { useQuery, keepPreviousData } from '@tanstack/react-query'
import { setCsrfToken } from './client'
import { endpoints, type RangeParams } from './endpoints'
import { keys } from './keys'
import type { Me } from './types'

export async function fetchMe(): Promise<Me> {
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

export function useDirectory() {
  return useQuery({ queryKey: keys.directory, queryFn: endpoints.directory, staleTime: 5 * 60_000 })
}

export function useInbox() {
  return useQuery({ queryKey: keys.inbox, queryFn: () => endpoints.inbox(), staleTime: 60_000 })
}

export function useThemes(all = false) {
  return useQuery({ queryKey: keys.themes(all), queryFn: () => endpoints.themes(all), staleTime: 10 * 60_000 })
}
