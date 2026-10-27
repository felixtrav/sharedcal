import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, uniqueTitle } from './helpers'

const NOT_FOUND = 'Page not found'

test('admins manage tags and settings, and changes reach everyone', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  const tag = uniqueTitle('Family')
  await alice.page.goto('/admin/tags')
  await alice.page.getByLabel('Tag name').fill(tag)
  await alice.page.getByRole('button', { name: 'Add tag' }).click()
  await expect(alice.page.getByText(`Tag “${tag}” created`).first()).toBeVisible()
  await expect(alice.page.getByRole('list', { name: 'Tags in display order' }).getByLabel(`Name of tag ${tag}`)).toHaveValue(tag)

  // Writers can use the new tag immediately.
  const bob = await signedInContext(browser, 'bob')
  const tags = await api<{ tags: { name: string }[] }>(bob.page, 'GET', '/api/tags')
  expect(tags.tags.map((t) => t.name)).toContain(tag)

  const name = uniqueTitle('Our calendar')
  await alice.page.goto('/admin/settings')
  await alice.page.getByLabel('Calendar name').fill(name)
  await alice.page.getByRole('button', { name: 'Save' }).first().click()
  await expect(alice.page.getByText('Settings saved').first()).toBeVisible()
  await bob.page.reload()
  await expect(bob.page.getByRole('navigation', { name: 'Main navigation' }).first().getByRole('link', { name })).toBeAttached()

  // The audit log records both changes.
  await alice.page.goto('/activity')
  await expect(alice.page.getByText('created a tag').first()).toBeVisible()
  await expect(alice.page.getByText('updated settings').first()).toBeVisible()

  await alice.context.close()
  await bob.context.close()
})

test('non-admins cannot reach admin pages', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  for (const path of ['/admin/people', '/admin/tags', '/admin/settings', '/admin/trash']) {
    await bob.page.goto(path)
    await expect(bob.page.getByRole('heading', { level: 1 })).toHaveText(NOT_FOUND)
  }
  // Their activity page only lists their own changes.
  await bob.page.goto('/activity')
  await expect(bob.page.getByRole('heading', { name: 'My activity' })).toBeVisible()
  await bob.context.close()
})

test('an admin changes an event’s owner from the panel', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Car pool'))
  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto(`/?event=${ev.id}`)
  const panel = alice.page.getByRole('complementary', { name: ev.title })
  await expect(panel.getByText('owned by Bob Writer')).toBeVisible()
  await panel.getByRole('button', { name: `Change the owner of ${ev.title}` }).click()
  await panel.getByLabel('New owner').selectOption({ label: 'Alice Admin' })
  await panel.getByRole('button', { name: 'Save' }).click()
  await expect(alice.page.getByText('Alice Admin now owns this event').first()).toBeVisible()
  await expect(panel.getByText('owned by Alice Admin')).toBeVisible()

  // bob no longer owns it, so he can only suggest.
  await bob.page.goto(`/?event=${ev.id}`)
  const bobPanel = bob.page.getByRole('complementary', { name: ev.title })
  await expect(bobPanel.getByRole('button', { name: 'Suggest changes' })).toBeVisible()
  await expect(bobPanel.getByRole('button', { name: `Change the owner of ${ev.title}` })).toHaveCount(0)

  await alice.context.close()
  await bob.context.close()
})

test('an admin changes a role, then erases a user’s data', async ({ browser }) => {
  // dave signs in, owns an event and leaves a comment.
  const dave = await signedInContext(browser, 'dave')
  const ev = await createEvent(dave.page, uniqueTitle('Dave’s BBQ'))
  await api(dave.page, 'POST', `/api/events/${ev.id}/comments`, { body: 'I will bring the grill' })

  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto('/admin/people')
  const row = alice.page.getByRole('row').filter({ hasText: 'dave@example.test' })
  await row.getByRole('combobox', { name: /Role for Dave/ }).click()
  await alice.page.getByRole('option', { name: 'Viewer' }).click()
  await expect(row.getByText('Set here')).toBeVisible()
  const me = await api<{ user: { role: string } }>(dave.page, 'GET', '/api/me')
  expect(me.user.role).toBe('viewer')

  await row.getByRole('button', { name: /Erase Dave Writer's data/ }).click()
  const dialog = alice.page.getByRole('dialog', { name: /Erase Dave Writer's data/ })
  await dialog.getByLabel('Give their events to').selectOption({ label: 'Bob Writer (bob@example.test)' })
  const erase = dialog.getByRole('button', { name: 'Erase data' })
  await expect(erase).toBeDisabled()
  await dialog.getByRole('checkbox').check()
  await erase.click()
  await expect(alice.page.getByText("Dave Writer's data was erased").first()).toBeVisible()

  // The row stays as an anonymous placeholder with no actions.
  const erased = alice.page.getByRole('row').filter({ hasText: 'Deleted user' })
  await expect(erased.getByText('Erased')).toBeVisible()
  await expect(erased.getByRole('button', { name: /Erase/ })).toHaveCount(0)

  // dave is signed out; the event lives on under bob; the comment is gone.
  const res = await dave.page.request.get('/api/me')
  expect(res.status()).toBe(401)
  const detail = await api<{ owner: { name: string } }>(alice.page, 'GET', `/api/events/${ev.id}`)
  expect(detail.owner.name).toBe('Bob Writer')
  const comments = await api<{ comments: { body: string }[] }>(alice.page, 'GET', `/api/events/${ev.id}/comments`)
  expect(comments.comments.map((c) => c.body)).not.toContain('I will bring the grill')

  await alice.page.goto('/activity')
  await expect(alice.page.getByText("erased a person's data").first()).toBeVisible()

  await alice.context.close()
  await dave.context.close()
})
