import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, uniqueTitle } from './helpers'

test('a user creates a feed link that serves the calendar as iCalendar', async ({ browser }) => {
  const { context, page } = await signedInContext(browser, 'carol')
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Choir'))

  await page.goto('/subscribe')
  // Start from no feed so the flow is the same on every run.
  const status = await api<{ exists: boolean }>(page, 'GET', '/api/me/feed-token')
  if (status.exists) {
    await api(page, 'DELETE', '/api/me/feed-token')
    await page.reload()
  }
  await page.getByRole('button', { name: 'Create feed link' }).click()
  const field = page.getByLabel('Your feed URL')
  await expect(field).toHaveValue(/\/ics\/.+\.ics$/)
  const url = await field.inputValue()

  // The feed needs no session: a fresh request context reads it.
  const anon = await browser.newContext()
  const feed = await anon.request.get(url)
  expect(feed.status()).toBe(200)
  expect(feed.headers()['content-type']).toContain('text/calendar')
  const body = await feed.text()
  expect(body).toContain('BEGIN:VCALENDAR')
  expect(body).toContain(`SUMMARY:${ev.title}`)

  // Turning it off revokes the link.
  await page.reload()
  await page.getByRole('button', { name: 'Turn off' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Turn off' }).click()
  await expect(page.getByText('Feed turned off').first()).toBeVisible()
  expect((await anon.request.get(url)).status()).toBe(404)

  await anon.close()
  await bob.context.close()
  await context.close()
})

test('preferences save immediately and change how the calendar renders', async ({ browser }) => {
  const { context, page } = await signedInContext(browser, 'carol')
  await page.goto('/preferences')
  await page.getByRole('radio', { name: '24-hour' }).click()
  await page.getByLabel('Week starts on').selectOption({ label: 'Sunday' })
  await page.getByLabel('Default view').selectOption({ label: 'Week' })

  await expect
    .poll(async () => (await api<{ preferences: { clock24h: boolean; weekStart: number; defaultView: string } }>(page, 'GET', '/api/me')).preferences)
    .toMatchObject({ clock24h: true, weekStart: 0, defaultView: 'week' })

  // A fresh visit opens the week view, starting on Sunday, with 24-hour times.
  await page.goto('/')
  await expect(page.getByRole('radio', { name: 'Week', checked: true })).toBeVisible()
  await expect(page.locator('.fc-col-header-cell, [role="columnheader"]').first()).toContainText(/Sun/i)
  await expect(page.getByText('13:00').first()).toBeVisible()

  // Restore defaults for other tests.
  await api(page, 'PUT', '/api/me/preferences', { themeId: 'default', colorMode: 'system', timeZone: '', weekStart: 1, clock24h: false, defaultView: 'month', density: 'comfortable' })
  await context.close()
})
