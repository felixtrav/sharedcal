import { useEffect, useRef } from 'react'
import { endpoints } from '@/api/endpoints'

const HEARTBEAT_MS = 20_000

/** Announce that this user is viewing (or editing) an event while its panel is open. */
export function usePresence(eventId: string | null, editing: boolean) {
  const state = editing ? 'editing' : 'viewing'
  const stateRef = useRef<'viewing' | 'editing'>(state)

  useEffect(() => {
    if (!eventId) return
    const send = () => endpoints.presence(eventId, stateRef.current).catch(() => undefined)
    void send()
    const timer = setInterval(send, HEARTBEAT_MS)
    const onLeave = () => {
      void endpoints.presence(eventId, 'left').catch(() => undefined)
    }
    window.addEventListener('pagehide', onLeave)
    return () => {
      clearInterval(timer)
      window.removeEventListener('pagehide', onLeave)
      onLeave()
    }
  }, [eventId])

  // State changes are sent immediately rather than waiting for the next beat.
  useEffect(() => {
    if (stateRef.current === state) return
    stateRef.current = state
    if (eventId) void endpoints.presence(eventId, state).catch(() => undefined)
  }, [state, eventId])
}
