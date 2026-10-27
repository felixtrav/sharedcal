import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, uniqueTitle } from './helpers'

test('a mention lands in the inbox live, opens the event and can be marked read', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Potluck'))
  const me = await api<{ user: { id: string } }>(bob.page, 'GET', '/api/me')
  // Start from an empty inbox so the badge count is exact.
  await api(bob.page, 'POST', '/api/inbox/read', { all: true })
  await bob.page.goto('/inbox')
  const nav = bob.page.getByRole('navigation', { name: 'Main navigation' }).first()
  await expect(nav.getByText(/unread notification/)).toHaveCount(0)

  // carol (a viewer) mentions bob; the badge and list update without a reload.
  const carol = await signedInContext(browser, 'carol')
  await api(carol.page, 'POST', `/api/events/${ev.id}/comments`, { body: 'Can you bring plates, @Bob?', mentions: [me.user.id] })

  await expect(nav.getByText('1 unread notification')).toBeAttached()
  const item = bob.page.getByRole('button', { name: new RegExp(`Carol Viewer mentioned you on “${ev.title}”`) })
  await expect(item).toBeVisible()

  // Opening the item marks it read and lands on the event's comments.
  await item.click()
  await expect(bob.page).toHaveURL(new RegExp(`event=${ev.id}`))
  const panel = bob.page.getByRole('complementary', { name: ev.title })
  await expect(panel.getByRole('tab', { name: /Comments/, selected: true })).toBeVisible()
  await expect(panel.getByText('Can you bring plates')).toBeVisible()
  await expect(nav.getByText(/unread notification/)).toHaveCount(0)

  // A second notification, cleared with "Mark all as read".
  await api(carol.page, 'POST', `/api/events/${ev.id}/comments`, { body: 'Also cups, @Bob', mentions: [me.user.id] })
  await bob.page.goto('/inbox')
  await expect(bob.page.getByText('1 unread').first()).toBeVisible()
  await bob.page.getByRole('button', { name: 'Mark all as read' }).click()
  await expect(nav.getByText(/unread notification/)).toHaveCount(0)
  const inbox = await api<{ unread: number }>(bob.page, 'GET', '/api/inbox')
  expect(inbox.unread).toBe(0)

  await carol.context.close()
  await bob.context.close()
})
