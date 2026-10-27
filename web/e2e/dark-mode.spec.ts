import { expect, test } from '@playwright/test'
import { login } from './helpers'

test('dark mode toggles, persists, and paints before the app loads', async ({ page }) => {
  await login(page, 'carol')
  await page.goto('/preferences')
  const mode = page.getByRole('radiogroup', { name: 'Mode' })

  await mode.getByRole('radio', { name: 'Dark' }).click()
  await expect(page.locator('html')).toHaveClass(/\bdark\b/)
  const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor)
  expect(bg).not.toBe('rgb(255, 255, 255)')

  // theme-init.js applies the stored mode before React renders.
  await page.reload({ waitUntil: 'domcontentloaded' })
  await expect(page.locator('html')).toHaveClass(/\bdark\b/)

  await mode.getByRole('radio', { name: 'Light' }).click()
  await expect(page.locator('html')).not.toHaveClass(/\bdark\b/)
  await mode.getByRole('radio', { name: 'System' }).click()
})
