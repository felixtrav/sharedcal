import { expect, test } from '@playwright/test'
import { signedInContext, uniqueTitle } from './helpers'

test('a writer suggests a tag and an event type, and an admin reviews them', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const tagName = uniqueTitle('Book club')
  const typeName = uniqueTitle('Concert')

  // Non-admins can browse what exists and suggest what's missing.
  await bob.page.goto('/catalog')
  await expect(bob.page.getByRole('heading', { name: 'Tags & event types' })).toBeVisible()
  await bob.page.getByLabel('Name', { exact: true }).fill(tagName)
  await bob.page.getByLabel('Other').fill('For the monthly meetup')
  await bob.page.getByRole('button', { name: 'Send suggestion' }).click()
  await expect(bob.page.getByText(`Suggested “${tagName}”`).first()).toBeVisible()

  // An event type only offers a name, color, icon and note: nothing else.
  await bob.page.getByRole('radio', { name: 'Event type' }).click()
  await bob.page.getByLabel('Name', { exact: true }).fill(typeName)
  await bob.page.getByLabel('Icon').selectOption({ label: 'Music' })
  await expect(bob.page.getByLabel(/repeat|field|admin/i)).toHaveCount(0)
  await bob.page.getByRole('button', { name: 'Send suggestion' }).click()
  const mine = bob.page.getByRole('region', { name: 'Your suggestions' })
  await expect(mine.getByText(typeName)).toBeVisible()
  await expect(mine.getByText(tagName)).toBeVisible()
  await expect(mine.getByText('For the monthly meetup')).toBeVisible()

  // Admins see them in the queue; approving the tag creates it for everyone.
  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto('/admin/requests')
  const tagRow = alice.page.getByRole('listitem').filter({ hasText: tagName })
  await expect(tagRow).toContainText('Suggested by')
  await tagRow.getByRole('button', { name: `Approve ${tagName}` }).click()
  await expect(alice.page.getByText(`“${tagName}” created`).first()).toBeVisible()
  await expect(alice.page.getByRole('listitem').filter({ hasText: tagName })).toHaveCount(0)

  // Rejecting the type tells the requester why.
  const typeRow = alice.page.getByRole('listitem').filter({ hasText: typeName })
  await typeRow.getByRole('button', { name: `Reject ${typeName}` }).click()
  await typeRow.getByLabel('Note to the requester').fill('We already have enough types')
  await typeRow.getByRole('button', { name: 'Reject request' }).click()
  await expect(alice.page.getByText(`“${typeName}” rejected`).first()).toBeVisible()

  await bob.page.reload()
  const after = bob.page.getByRole('region', { name: 'Your suggestions' })
  await expect(after.getByRole('listitem').filter({ hasText: tagName })).toContainText('Approved')
  await expect(after.getByRole('listitem').filter({ hasText: typeName })).toContainText('We already have enough types')
  await expect(bob.page.getByRole('region', { name: 'Tags' }).getByText(tagName)).toBeVisible()

  // Admins create tags directly and have no suggestion form of their own.
  await alice.page.goto('/catalog')
  await expect(alice.page.getByRole('button', { name: 'Send suggestion' })).toHaveCount(0)
})

test('only admins reach the review page', async ({ browser }) => {
  const carol = await signedInContext(browser, 'carol')
  await carol.page.goto('/admin/requests')
  await expect(carol.page.getByRole('heading', { name: 'Requests' })).toHaveCount(0)
  await carol.page.goto('/catalog')
  await expect(carol.page.getByRole('button', { name: 'Send suggestion' })).toBeVisible()
})
