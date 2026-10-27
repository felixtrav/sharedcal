import { expect, test } from '@playwright/test'
import { createEvent, eventChip, gotoWeek, signedInContext, uniqueTitle } from './helpers'

test('people viewing an event show up on its calendar chip and in the panel', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Planning'))
  await gotoWeek(bob.page)
  const chip = eventChip(bob.page, ev.title)
  await expect(chip).toBeVisible()
  await expect(chip.getByTestId('chip-presence')).toHaveCount(0)

  // alice opens the event; bob sees her on the chip without opening it.
  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto(`/?event=${ev.id}`)
  await expect(alice.page.getByRole('complementary', { name: ev.title })).toBeVisible()
  await expect(chip.getByTestId('chip-presence')).toBeVisible()
  await expect(chip).toContainText('Alice Admin is viewing')

  // Opening it too shows her in the panel, but never bob himself.
  await chip.click()
  const panel = bob.page.getByRole('complementary', { name: ev.title })
  await expect(panel.getByText('Alice Admin is viewing').first()).toBeAttached()
  await expect(panel.getByText('Bob Writer is viewing')).toHaveCount(0)

  // Closing the panel clears the indicator for others.
  await alice.page.getByRole('button', { name: 'Close event details' }).click()
  await expect(chip.getByTestId('chip-presence')).toHaveCount(0)

  await alice.context.close()
  await bob.context.close()
})
