/**
 * localStorage for UI conveniences only (theme, mode, rail pin). Never store
 * secrets here. Every access is wrapped because storage can be unavailable.
 */
export function readLocal<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key)
    return raw === null ? fallback : (JSON.parse(raw) as T)
  } catch {
    return fallback
  }
}

export function writeLocal(key: string, value: unknown) {
  try {
    window.localStorage.setItem(key, JSON.stringify(value))
  } catch {
    /* storage unavailable or full: the app works without it */
  }
}

export const STORAGE_KEYS = {
  /** Read by public/theme-init.js before React loads; keep the shape in sync. */
  theme: 'sharedcal.theme.v1',
  railPinned: 'sharedcal.rail.pinned',
} as const
