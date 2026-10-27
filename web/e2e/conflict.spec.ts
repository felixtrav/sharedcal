import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, uniqueTitle, type ApiEvent } from './helpers'

test('saving over a newer version shows both and lets the user pick per field', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Retro'))
  await bob.page.goto(`/?event=${ev.id}`)
  const panel = bob.page.getByRole('complementary', { name: ev.title })
  await panel.getByRole('button', { name: 'Edit' }).click()

  // Someone else (an admin) saves first.
  const alice = await signedInContext(browser, 'alice')
  const theirTitle = `${ev.title} — moved`
  await api<ApiEvent>(alice.page, 'PATCH', `/api/events/${ev.id}`, { title: theirTitle, location: 'Room 1' }, { 'If-Match': String(ev.version) })

  const myTitle = `${ev.title} — mine`
  await panel.getByLabel('Title').fill(myTitle)
  await panel.getByLabel('Location').fill('Room 2')
  await panel.getByRole('button', { name: 'Save' }).click()

  const dialog = bob.page.getByRole('dialog', { name: 'This event changed while you were editing' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText(theirTitle)).toBeVisible()
  await expect(dialog.getByText(myTitle)).toBeVisible()
  // Take their title, keep my location.
  await dialog.getByRole('radio', { name: `Use latest version: ${theirTitle}` }).check()
  await dialog.getByRole('button', { name: 'Save my choices' }).click()
  await expect(bob.page.getByText('Event saved').first()).toBeVisible()

  const after = await api<{ title: string; location: string; version: number }>(bob.page, 'GET', `/api/events/${ev.id}`)
  expect(after).toMatchObject({ title: theirTitle, location: 'Room 2', version: ev.version + 2 })

  await alice.context.close()
  await bob.context.close()
})
