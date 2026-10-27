import { expect, test } from '@playwright/test'
import { eventChip, gotoWeek, login, midWeekDate, slotPoint, uniqueTitle } from './helpers'

test('a writer creates an event by dragging across empty slots', async ({ page }) => {
  await login(page, 'bob')
  const date = midWeekDate()
  await gotoWeek(page, date)

  const from = await slotPoint(page, date, '09:00:00')
  const to = await slotPoint(page, date, '10:30:00')
  await page.mouse.move(from.x, from.y)
  await page.mouse.down()
  await page.mouse.move(to.x, to.y, { steps: 8 })
  await page.mouse.up()

  const popover = page.getByRole('dialog', { name: 'New event' })
  await expect(popover).toBeVisible()
  const title = uniqueTitle('Standup')
  await popover.getByPlaceholder('Add a title').fill(title)
  const created = page.waitForResponse((r) => r.url().endsWith('/api/events') && r.request().method() === 'POST')
  await popover.getByRole('button', { name: 'Create' }).click()
  expect((await created).status()).toBe(201)

  await expect(eventChip(page, title)).toBeVisible()
  // Survives a reload (it was saved, not just drawn).
  await page.reload()
  await expect(eventChip(page, title)).toBeVisible()
})

test('"More options" opens the full form', async ({ page }) => {
  await login(page, 'bob')
  const date = midWeekDate()
  await gotoWeek(page, date)
  const p = await slotPoint(page, date, '15:00:00')
  await page.mouse.click(p.x, p.y)
  const popover = page.getByRole('dialog', { name: 'New event' })
  await expect(popover).toBeVisible()
  await popover.getByPlaceholder('Add a title').fill('Planning')
  await popover.getByRole('button', { name: 'More options' }).click()
  const panel = page.getByRole('complementary', { name: 'New event' })
  await expect(panel.getByLabel('Title')).toHaveValue('Planning')
  await expect(panel.getByLabel('Repeats')).toBeVisible()
})
