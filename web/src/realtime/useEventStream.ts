import { useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { isApiError, loginUrl } from '@/api/client'
import { endpoints } from '@/api/endpoints'
import type { PresenceViewer } from '@/api/types'
import { applyStreamMessage, STREAM_TYPES } from './applyMessage'
import { connectionStore, deletedEventsStore, glow, isOwnWrite, presenceStore } from './stores'

const STREAM_URL = '/api/stream'

/**
 * Holds the app's single EventSource. The browser reconnects on its own
 * (sending Last-Event-ID); only when the connection is closed for good do we
 * check the session and open a fresh one.
 */
export function useEventStream(meId: string | undefined) {
  const qc = useQueryClient()

  useEffect(() => {
    if (!meId || typeof EventSource === 'undefined') return
    let es: EventSource | null = null
    let disposed = false
    let retryTimer: ReturnType<typeof setTimeout> | undefined
    let attempt = 0
    let everOpened = false

    const ctx = {
      meId,
      isOwnWrite,
      onRemoteChange: glow,
      onEventDeleted: (id: string) => deletedEventsStore.set((prev) => new Set(prev).add(id)),
      onPresence: (eventId: string, viewers: PresenceViewer[]) =>
        presenceStore.set((prev) => ({ ...prev, [eventId]: viewers })),
    }

    const handle = (type: string) => (e: MessageEvent<string>) => {
      let payload: unknown = {}
      if (e.data) {
        try {
          const msg = JSON.parse(e.data) as { payload?: unknown }
          payload = msg && typeof msg === 'object' && 'payload' in msg ? msg.payload : msg
        } catch {
          return
        }
      }
      applyStreamMessage(qc, type, payload, ctx)
    }

    const checkSessionThenReconnect = async () => {
      try {
        await endpoints.probeMe()
      } catch (err) {
        if (isApiError(err) && err.status === 401) {
          window.location.assign(loginUrl())
          return
        }
      }
      if (disposed) return
      const delay = Math.min(30_000, 1000 * 2 ** attempt) + Math.random() * 500
      attempt++
      retryTimer = setTimeout(connect, delay)
    }

    function connect() {
      if (disposed) return
      es = new EventSource(STREAM_URL, { withCredentials: true })
      for (const type of STREAM_TYPES) es.addEventListener(type, handle(type) as EventListener)
      es.onopen = () => {
        // A fresh EventSource can't replay what we missed, so refetch after a hard reconnect.
        if (everOpened && attempt > 0) void qc.invalidateQueries()
        everOpened = true
        attempt = 0
        connectionStore.set('open')
      }
      es.onerror = () => {
        if (disposed || !es) return
        connectionStore.set(everOpened ? 'reconnecting' : 'connecting')
        if (es.readyState === EventSource.CLOSED) {
          es.close()
          es = null
          connectionStore.set('reconnecting')
          void checkSessionThenReconnect()
        } else {
          // The browser is retrying; if the session ended, go sign in.
          endpoints.probeMe().catch((err: unknown) => {
            if (isApiError(err) && err.status === 401) window.location.assign(loginUrl())
          })
        }
      }
    }

    connect()
    return () => {
      disposed = true
      clearTimeout(retryTimer)
      es?.close()
    }
  }, [meId, qc])
}
