import { useSyncExternalStore } from 'react'

export function useMediaQuery(query: string) {
  return useSyncExternalStore(
    (cb) => {
      const mq = window.matchMedia?.(query)
      mq?.addEventListener('change', cb)
      return () => mq?.removeEventListener('change', cb)
    },
    () => window.matchMedia?.(query).matches ?? false,
    () => false,
  )
}

export const useIsMobile = () => useMediaQuery('(max-width: 767px)')
