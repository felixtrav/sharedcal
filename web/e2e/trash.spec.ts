import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, uniqueTitle } from './helpers'

test('the owner deletes an event and undoes it from the toast', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Dentist'))
  await bob.page.goto(`/?event=${ev.id}`)
  const panel = bob.page.getByRole('complementary', { name: ev.title })
  await panel.getByRole('button', { name: 'Delete' }).click()
  await bob.page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
  await expect(bob.page.getByText(`“${ev.title}” deleted`).first()).toBeVisible()
  await expect(panel).toHaveCount(0)

  await bob.page.getByRole('button', { name: 'Undo' }).click()
  await expect(bob.page.getByText('restored', { exact: false }).first()).toBeVisible()
  const back = await api<{ title: string; deletedAt?: string }>(bob.page, 'GET', `/api/events/${ev.id}`)
  expect(back.title).toBe(ev.title)
  expect(back.deletedAt).toBeFalsy()

  await bob.context.close()
})

test('an admin restores a deleted event from the trash', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Haircut'))
  await api(bob.page, 'DELETE', `/api/events/${ev.id}`, undefined, { 'If-Match': String(ev.version) })

  // Writers have no trash page.
  await bob.page.goto('/')
  await expect(bob.page.getByRole('link', { name: 'Trash' })).toHaveCount(0)

  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto('/admin/trash')
  const row = alice.page.getByRole('tabpanel', { name: 'Events' }).getByRole('listitem').filter({ hasText: ev.title })
  await expect(row).toContainText('Bob Writer')
  await row.getByRole('button', { name: `Restore ${ev.title}` }).click()
  await expect(alice.page.getByText(`“${ev.title}” restored`).first()).toBeVisible()
  await expect(row).toHaveCount(0)

  const back = await api<{ title: string }>(bob.page, 'GET', `/api/events/${ev.id}`)
  expect(back.title).toBe(ev.title)

  await alice.context.close()
  await bob.context.close()
})
