import { expect, test } from '@playwright/test'
import { login, waitForCalendar } from './helpers'

test('calendar keyboard shortcuts', async ({ page }) => {
  await login(page, 'bob')
  await page.goto('/?view=month')
  await waitForCalendar(page)
  await page.locator('body').click({ position: { x: 5, y: 5 } })

  await page.keyboard.press('w')
  await expect(page).toHaveURL(/view=week/)
  await page.keyboard.press('d')
  await expect(page).toHaveURL(/view=day/)
  await page.keyboard.press('a')
  await expect(page).toHaveURL(/view=agenda/)
  await page.keyboard.press('m')
  await expect(page).toHaveURL(/view=month/)

  const before = new URL(page.url()).searchParams.get('date')
  await page.keyboard.press('ArrowRight')
  await expect.poll(() => new URL(page.url()).searchParams.get('date')).not.toBe(before)
  await page.keyboard.press('ArrowLeft')
  await page.keyboard.press('t')

  await page.keyboard.press('/')
  await expect(page.getByRole('searchbox', { name: 'Search events' })).toBeFocused()
  // Shortcuts are ignored while typing.
  await page.keyboard.type('wdm')
  await expect(page).toHaveURL(/view=month/)
  await page.keyboard.press('Escape')

  await page.keyboard.press('n')
  const panel = page.getByRole('complementary', { name: 'New event' })
  await expect(panel).toBeVisible()
  await page.locator('body').click({ position: { x: 5, y: 5 } })
  await page.keyboard.press('Escape')
  await expect(panel).toHaveCount(0)
})
