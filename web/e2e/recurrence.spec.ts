import { expect, test } from '@playwright/test'
import { api, eventChip, gotoWeek, listRange, midWeekDate, signedInContext, uniqueTitle, type ApiEvent } from './helpers'

test('editing one occurrence of a weekly series only changes that occurrence', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const title = uniqueTitle('Standup')
  const first = midWeekDate()
  const ev = await api<ApiEvent>(bob.page, 'POST', '/api/events', {
    title,
    start: `${first}T09:00:00`,
    end: `${first}T09:30:00`,
    timeZone: 'America/New_York',
    allDay: false,
    rrule: 'FREQ=WEEKLY;COUNT=4',
  })

  // Open next week's occurrence from the calendar.
  await gotoWeek(bob.page, midWeekDate(1))
  await eventChip(bob.page, title).click()
  const panel = bob.page.getByRole('complementary', { name: title })
  await expect(panel.getByText(/This occurrence/)).toBeVisible()
  await panel.getByRole('button', { name: 'Edit' }).click()
  const moved = `${title} (remote)`
  await panel.getByLabel('Title').fill(moved)
  await panel.getByRole('button', { name: 'Save' }).click()

  const scope = bob.page.getByRole('dialog', { name: 'Change recurring event' })
  await scope.getByRole('radio', { name: 'This event' }).check()
  await scope.getByRole('button', { name: 'Save' }).click()
  await expect(bob.page.getByText('Event saved').first()).toBeVisible()

  const occ = await listRange(bob.page, `${midWeekDate(-1)}T00:00:00`, `${midWeekDate(5)}T00:00:00`)
  const mine = occ.filter((o) => o.id === ev.id)
  expect(mine).toHaveLength(4)
  expect(mine.map((o) => o.title)).toEqual([title, moved, title, title])

  await bob.context.close()
})

test('"This and following" splits the series', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const title = uniqueTitle('Yoga')
  const first = midWeekDate()
  await api<ApiEvent>(bob.page, 'POST', '/api/events', {
    title,
    start: `${first}T18:00:00`,
    end: `${first}T19:00:00`,
    timeZone: 'America/New_York',
    allDay: false,
    rrule: 'FREQ=WEEKLY;COUNT=4',
  })

  await gotoWeek(bob.page, midWeekDate(2))
  await eventChip(bob.page, title).click()
  const panel = bob.page.getByRole('complementary', { name: title })
  await panel.getByRole('button', { name: 'Edit' }).click()
  await panel.getByLabel('Location').fill('Studio B')
  await panel.getByRole('button', { name: 'Save' }).click()
  const scope = bob.page.getByRole('dialog', { name: 'Change recurring event' })
  await scope.getByRole('radio', { name: 'This and following events' }).check()
  await scope.getByRole('button', { name: 'Save' }).click()
  await expect(bob.page.getByText('Event saved').first()).toBeVisible()

  const occ = (await listRange(bob.page, `${midWeekDate(-1)}T00:00:00`, `${midWeekDate(5)}T00:00:00`)).filter((o) => o.title === title)
  expect(occ).toHaveLength(4)
  // Two series now: the head keeps the original ID, the tail gets a new one.
  expect(new Set(occ.map((o) => o.id)).size).toBe(2)
  const tail = await api<{ location: string }>(bob.page, 'GET', `/api/events/${occ[3]!.id}`)
  expect(tail.location).toBe('Studio B')
  const head = await api<{ location: string }>(bob.page, 'GET', `/api/events/${occ[0]!.id}`)
  expect(head.location).toBe('')

  await bob.context.close()
})
