import { expect, test } from '@playwright/test'
import { createEvent, signedInContext, uniqueTitle } from './helpers'

test('a viewer can comment on an event', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Picnic'))

  const carol = await signedInContext(browser, 'carol')
  await carol.page.goto(`/?event=${ev.id}&tab=comments`)
  const panel = carol.page.getByRole('complementary', { name: ev.title })
  await expect(panel).toBeVisible()

  // Viewers can't edit…
  await panel.getByRole('tab', { name: 'Details' }).click()
  await expect(panel.getByRole('button', { name: 'Edit' })).toHaveCount(0)
  await expect(panel.getByRole('button', { name: 'Suggest changes' })).toHaveCount(0)

  // …but they can comment.
  await panel.getByRole('tab', { name: /Comments/ }).click()
  const body = `I'll bring **lemonade** ${Date.now()}`
  await panel.getByRole('combobox', { name: 'New comment' }).fill(body)
  await panel.getByRole('button', { name: 'Comment', exact: true }).click()
  await expect(panel.getByRole('article', { name: /Comment by/ }).filter({ hasText: 'lemonade' })).toBeVisible()
  await expect(panel.locator('strong', { hasText: 'lemonade' })).toBeVisible()

  // The owner sees it live.
  await bob.page.goto(`/?event=${ev.id}&tab=comments`)
  await expect(bob.page.getByText('lemonade').first()).toBeVisible()

  await bob.context.close()
  await carol.context.close()
})
