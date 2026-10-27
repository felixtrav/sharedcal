import { useMemo } from 'react'
import { useMeData } from '@/api/queries'
import { displayTimeZone, type FormatOptions } from './time'

export function useFmt(): FormatOptions {
  const me = useMeData()
  const tz = displayTimeZone(me.preferences.timeZone)
  return useMemo(() => ({ timeZone: tz, clock24h: me.preferences.clock24h }), [tz, me.preferences.clock24h])
}
