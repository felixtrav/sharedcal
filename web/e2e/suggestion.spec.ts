import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, uniqueTitle, type ApiEvent } from './helpers'

test('a writer suggests an edit to someone else’s event and the owner accepts it', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  const ev = await createEvent(alice.page, uniqueTitle('Book club'))

  const bob = await signedInContext(browser, 'bob')
  await bob.page.goto(`/?event=${ev.id}`)
  const panel = bob.page.getByRole('complementary', { name: ev.title })
  await expect(panel.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await panel.getByRole('button', { name: 'Suggest changes' }).click()

  const newTitle = `${ev.title} (moved to library)`
  await panel.getByLabel('Title').fill(newTitle)
  const sent = bob.page.waitForResponse((r) => r.url().endsWith(`/api/events/${ev.id}/suggestions`) && r.request().method() === 'POST')
  await panel.getByRole('button', { name: 'Suggest changes' }).last().click()
  const res = await sent
  expect(res.status()).toBe(201)
  const payload = res.request().postDataJSON() as { kind: string; changes: Record<string, unknown> }
  expect(payload.kind).toBe('edit')
  expect(Object.keys(payload.changes)).toEqual(['title'])

  // The owner reviews and accepts.
  await alice.page.goto(`/?event=${ev.id}&tab=suggestions`)
  const ownerPanel = alice.page.getByRole('complementary')
  await ownerPanel.getByRole('button', { name: 'Review' }).click()
  await expect(ownerPanel.getByText(newTitle)).toBeVisible()
  await ownerPanel.getByRole('button', { name: 'Accept all' }).click()
  await expect(alice.page.getByText('Suggestion accepted').first()).toBeVisible()

  const after = await api<ApiEvent>(alice.page, 'GET', `/api/events/${ev.id}`)
  expect(after.title).toBe(newTitle)

  await alice.context.close()
  await bob.context.close()
})
