import { expect, test } from '@playwright/test'
import { api, createEvent, eventChip, gotoWeek, login, midWeekDate, slotPoint, uniqueTitle, type ApiEvent } from './helpers'

test('the owner drags an event to a new time', async ({ page }) => {
  await login(page, 'bob')
  const date = midWeekDate()
  const ev = await createEvent(page, uniqueTitle('Move me'), date, 10, 1)
  await gotoWeek(page, date)

  const chip = eventChip(page, ev.title)
  await expect(chip).toBeVisible()
  const box = await chip.boundingBox()
  if (!box) throw new Error('no chip box')
  const target = await slotPoint(page, date, '14:00:00')

  const patched = page.waitForResponse((r) => r.url().includes(`/api/events/${ev.id}`) && r.request().method() === 'PATCH')
  await page.mouse.move(box.x + box.width / 2, box.y + 6)
  await page.mouse.down()
  await page.mouse.move(target.x, target.y, { steps: 12 })
  await page.mouse.up()

  const res = await patched
  expect(res.status()).toBe(200)
  expect(res.request().headers()['if-match']).toBe(String(ev.version))

  const after = await api<ApiEvent & { start: string }>(page, 'GET', `/api/events/${ev.id}`)
  expect(after.start).not.toContain('T10:00')
  expect(after.version).toBeGreaterThan(ev.version)
})
