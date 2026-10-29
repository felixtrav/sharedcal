import { expect, test } from '@playwright/test'
import { login, waitForCalendar } from './helpers'

test('the date picker jumps by month in the month view and by day elsewhere', async ({ page }) => {
  await login(page, 'bob')
  await page.goto('/?view=month&date=2026-10-15')
  await waitForCalendar(page)

  // Month view: months and years only.
  await page.getByRole('button', { name: 'Go to month' }).click()
  await expect(page.getByRole('gridcell')).toHaveCount(0)
  await page.getByRole('button', { name: 'Next year' }).click()
  await page.getByRole('group', { name: '2027' }).getByRole('button').nth(2).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('date')).toMatch(/^2027-03/)

  // Other views: a specific day.
  await page.goto('/?view=week&date=2026-10-15')
  await waitForCalendar(page)
  await page.getByRole('button', { name: 'Go to date' }).click()
  await page.getByRole('button', { name: 'Next month' }).click()
  await page.getByRole('gridcell', { name: /November 20, 2026/ }).click()
  await expect.poll(() => new URL(page.url()).searchParams.get('date')).toBe('2026-11-20')
  await expect(page).toHaveURL(/view=week/)
})
