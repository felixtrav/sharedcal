import type { PresenceViewer } from '@/api/types'
import { createStore } from '@/lib/store'

type ConnectionState = 'connecting' | 'open' | 'reconnecting'

export const connectionStore = createStore<ConnectionState>('connecting')

/** Event IDs that should glow briefly because a remote edit just landed. */
export const glowStore = createStore<ReadonlySet<string>>(new Set())

const GLOW_MS = 600

export function glow(id: string) {
  glowStore.set((prev) => new Set(prev).add(id))
  setTimeout(() => {
    glowStore.set((prev) => {
      const next = new Set(prev)
      next.delete(id)
      return next
    })
  }, GLOW_MS)
}

/** Presence viewers keyed by event ID. */
export const presenceStore = createStore<Readonly<Record<string, PresenceViewer[]>>>({})

/** Events deleted remotely while the client had them loaded. */
export const deletedEventsStore = createStore<ReadonlySet<string>>(new Set())

/**
 * Writes this client made itself, so their echo on the stream doesn't glow.
 * Keys are `${id}` for in-flight writes and `${id}@${version}` for completed ones.
 */
const ownWrites = new Map<string, number>()

export function markOwnWrite(id: string, version?: number) {
  const key = version === undefined ? id : `${id}@${version}`
  ownWrites.set(key, Date.now())
}

export function clearOwnWrite(id: string) {
  ownWrites.delete(id)
}

export function isOwnWrite(id: string, version?: number) {
  const now = Date.now()
  for (const [k, t] of ownWrites) if (now - t > 15_000) ownWrites.delete(k)
  return ownWrites.has(id) || (version !== undefined && ownWrites.has(`${id}@${version}`))
}
