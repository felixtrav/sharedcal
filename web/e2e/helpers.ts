import { expect, type Browser, type BrowserContext, type Page } from '@playwright/test'

/**
 * Test users provided by the mock OIDC provider (`go run ./cmd/mockoidc`).
 * Adjust here if the provider's fixtures change.
 */
const USERS = {
  alice: { hint: 'alice', name: /alice/i, role: 'admin' },
  bob: { hint: 'bob', name: /bob/i, role: 'writer' },
  carol: { hint: 'carol', name: /carol/i, role: 'viewer' },
  dave: { hint: 'dave', name: /dave/i, role: 'writer' },
} as const
type TestUser = keyof typeof USERS

const OIDC_ISSUER = process.env.E2E_OIDC_ISSUER ?? 'http://localhost:9999'

/**
 * Sign in through the real OIDC flow: the app redirects to the mock
 * provider, whose picker page lists its fixed users.
 */
export async function login(page: Page, user: TestUser, returnTo = '/') {
  await page.goto(`/auth/login?return=${encodeURIComponent(returnTo)}`)
  await page.getByRole('link', { name: USERS[user].name }).click()
  const providerOrigin = new URL(OIDC_ISSUER).origin
  await page.waitForURL((u) => u.origin !== providerOrigin && !u.pathname.startsWith('/auth/'), { timeout: 20_000 })
  await expect(page.getByRole('navigation', { name: 'Main navigation' }).first()).toBeVisible()
}

type StorageState = Awaited<ReturnType<BrowserContext['storageState']>>

// One sign-in per user per worker: the server rate-limits /auth/* per IP, and
// the whole suite comes from 127.0.0.1.
const sessions = new Map<TestUser, StorageState>()

/**
 * An isolated browser context signed in as `user`. It reuses the user's
 * session cookie from an earlier sign-in when that session is still valid.
 */
export async function signedInContext(browser: Browser, user: TestUser): Promise<{ context: BrowserContext; page: Page }> {
  const saved = sessions.get(user)
  if (saved) {
    const context = await browser.newContext({ storageState: saved })
    const page = await context.newPage()
    if ((await page.request.get('/api/me')).ok()) {
      await page.goto('/')
      await expect(page.getByRole('navigation', { name: 'Main navigation' }).first()).toBeVisible()
      return { context, page }
    }
    // Revoked or expired (e.g. the user was deactivated); sign in again.
    sessions.delete(user)
    await context.close()
  }
  const context = await browser.newContext()
  const page = await context.newPage()
  await login(page, user)
  sessions.set(user, await context.storageState())
  return { context, page }
}

/** Wait for the calendar grid and its first data load. */
export async function waitForCalendar(page: Page) {
  // Keep the pointer off the navigation rail so it stays collapsed.
  const vp = page.viewportSize()
  if (vp) await page.mouse.move(vp.width / 2, vp.height / 2)
  await expect(page.getByRole('button', { name: 'Today' })).toBeVisible()
  await expect(page.locator('.sc-calendar[aria-busy="true"]')).toHaveCount(0)
}

/** Call the API as the signed-in user of `page` (shares its cookies), adding the CSRF token. */
export async function api<T = unknown>(page: Page, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<T> {
  const me = await page.request.get('/api/me')
  expect(me.ok()).toBeTruthy()
  const { csrfToken } = (await me.json()) as { csrfToken: string }
  const res = await page.request.fetch(path, {
    method,
    // A browser sends Origin on same-origin writes; the API request context does not.
    headers: { 'X-CSRF-Token': csrfToken, 'Content-Type': 'application/json', Origin: new URL(page.url()).origin, ...headers },
    data: body === undefined ? undefined : JSON.stringify(body),
  })
  expect(res.ok(), `${method} ${path} → ${res.status()} ${await res.text()}`).toBeTruthy()
  return (res.status() === 204 ? undefined : await res.json()) as T
}

export type ApiEvent = { id: string; title: string; version: number; owner: { id: string; name: string } }

const pad = (n: number) => String(n).padStart(2, '0')

/** YYYY-MM-DD of the Wednesday in the current week (keeps tests away from week edges). */
export function midWeekDate(offsetWeeks = 0) {
  const d = new Date()
  const day = d.getDay()
  d.setDate(d.getDate() + (3 - day) + offsetWeeks * 7)
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** Create an event via the API for test setup. Times are wall times in America/New_York. */
export async function createEvent(page: Page, title: string, date = midWeekDate(), startHour = 10, hours = 1): Promise<ApiEvent> {
  return api<ApiEvent>(page, 'POST', '/api/events', {
    title,
    start: `${date}T${pad(startHour)}:00:00`,
    end: `${date}T${pad(startHour + hours)}:00:00`,
    timeZone: 'America/New_York',
    allDay: false,
  })
}

/** A valid 8×8 PNG for upload tests (the server re-encodes it). */
export const TINY_PNG = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAGElEQVR4nGJhqGCQY8CCWMAkFjg4JQADAFSvAj003n0VAAAAAElFTkSuQmCC', 'base64')

type ApiRangeEvent = { id: string; title: string; occurrence?: string | null; version: number }

/** Occurrences in [from, to) as the API expands them. */
export async function listRange(page: Page, from: string, to: string): Promise<ApiRangeEvent[]> {
  const q = new URLSearchParams({ from: new Date(from).toISOString(), to: new Date(to).toISOString() })
  const res = await api<{ events: ApiRangeEvent[] }>(page, 'GET', `/api/events?${q}`)
  return res.events
}

export function uniqueTitle(prefix: string) {
  return `${prefix} ${Math.random().toString(36).slice(2, 7)}`
}

/** Locate an event chip on the calendar by its (accessible) title. */
export function eventChip(page: Page, title: string) {
  return page.locator('[data-event-id]').filter({ hasText: title }).first()
}

/** Open week view at a given date via the URL. */
export async function gotoWeek(page: Page, date = midWeekDate()) {
  await page.goto(`/?view=week&date=${date}`)
  await waitForCalendar(page)
}

/** Bounding box center of the time-grid cell for `date` at `time` (HH:MM:SS). */
export async function slotPoint(page: Page, date: string, time: string) {
  const col = page.locator(`[data-date="${date}"]`).last()
  const row = page.locator(`[data-time="${time}"]`).first()
  await row.scrollIntoViewIfNeeded()
  const c = await col.boundingBox()
  const r = await row.boundingBox()
  if (!c || !r) throw new Error(`slot ${date} ${time} not visible`)
  return { x: c.x + c.width / 2, y: r.y + r.height / 2 }
}
