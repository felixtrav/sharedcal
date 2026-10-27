import { expect, test } from '@playwright/test'
import { api, createEvent, eventChip, gotoWeek, midWeekDate, signedInContext, uniqueTitle } from './helpers'

test('changes appear live in another browser', async ({ browser }) => {
  const date = midWeekDate()
  const alice = await signedInContext(browser, 'alice')
  const bob = await signedInContext(browser, 'bob')
  await gotoWeek(alice.page, date)
  await gotoWeek(bob.page, date)

  // Created by Bob → appears for Alice without a reload.
  const title = uniqueTitle('Live')
  const ev = await createEvent(bob.page, title, date, 11, 1)
  await expect(eventChip(alice.page, title)).toBeVisible({ timeout: 5_000 })

  // Renamed by Bob → updates for Alice.
  await api(bob.page, 'PATCH', `/api/events/${ev.id}`, { title: `${title} renamed` }, { 'If-Match': String(ev.version) })
  await expect(eventChip(alice.page, `${title} renamed`)).toBeVisible({ timeout: 5_000 })

  // Deleted by Bob → disappears for Alice.
  await api(bob.page, 'DELETE', `/api/events/${ev.id}`)
  await expect(eventChip(alice.page, `${title} renamed`)).toHaveCount(0, { timeout: 5_000 })

  await alice.context.close()
  await bob.context.close()
})
