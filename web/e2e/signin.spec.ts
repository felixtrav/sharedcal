import { expect, test } from '@playwright/test'
import { login, waitForCalendar } from './helpers'

test('signing in lands on the calendar', async ({ page }) => {
  await login(page, 'alice')
  await waitForCalendar(page)
  await expect(page).toHaveURL(/view=\w+/)
  await expect(page.getByRole('radiogroup', { name: 'Calendar view' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 1 })).not.toBeEmpty()
  // Admin items are visible for an admin.
  await expect(page.getByRole('link', { name: 'People' })).toBeVisible()
})

test('unauthenticated visitors are sent to sign in', async ({ page }) => {
  const res = await page.request.get('/api/me', { maxRedirects: 0 })
  expect(res.status()).toBe(401)
})

test('viewers do not see writer or admin controls', async ({ page }) => {
  await login(page, 'carol')
  await waitForCalendar(page)
  await expect(page.getByRole('button', { name: 'New event' })).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'People' })).toHaveCount(0)
})
