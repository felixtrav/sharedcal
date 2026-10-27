import { expect, test } from '@playwright/test'
import { api, createEvent, signedInContext, TINY_PNG, uniqueTitle } from './helpers'

test('the owner attaches files, removes one, and an admin restores it from the trash', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Picnic'))
  await bob.page.goto(`/?event=${ev.id}`)
  const panel = bob.page.getByRole('complementary', { name: ev.title })
  const files = panel.getByRole('region', { name: /Attachments/ })
  await expect(files.getByRole('button', { name: 'Attach' })).toBeVisible()

  await files.locator('input[type="file"]').setInputFiles([
    { name: 'map.png', mimeType: 'image/png', buffer: TINY_PNG },
    { name: 'agenda.txt', mimeType: 'text/plain', buffer: Buffer.from('1. Arrive\n2. Eat\n') },
  ])
  // Images get a thumbnail with a lightbox; other files a download row.
  const thumb = files.getByRole('button', { name: 'Open image map.png' })
  await expect(thumb).toBeVisible()
  await expect(files.getByRole('link', { name: 'Download agenda.txt' })).toBeVisible()

  await thumb.click()
  const lightbox = bob.page.getByRole('dialog')
  await expect(lightbox.getByRole('img')).toBeVisible()
  await bob.page.keyboard.press('Escape')
  await expect(lightbox).toHaveCount(0)

  // Removing is a soft delete…
  await files.getByRole('button', { name: 'Remove agenda.txt' }).click()
  await expect(bob.page.getByText('Removed agenda.txt').first()).toBeVisible()
  await expect(files.getByRole('link', { name: 'Download agenda.txt' })).toHaveCount(0)

  // …that an admin can undo from the trash; the owner sees it come back live.
  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto('/admin/trash')
  await alice.page.getByRole('tab', { name: 'Attachments' }).click()
  const row = alice.page.getByRole('listitem').filter({ hasText: 'agenda.txt' })
  await expect(row).toContainText(ev.title)
  await row.getByRole('button', { name: 'Restore agenda.txt' }).click()
  await expect(alice.page.getByText(`agenda.txt restored to “${ev.title}”`).first()).toBeVisible()
  await expect(row).toHaveCount(0)
  await expect(files.getByRole('link', { name: 'Download agenda.txt' })).toBeVisible()

  const detail = await api<{ attachments: { filename: string }[] }>(bob.page, 'GET', `/api/events/${ev.id}`)
  expect(detail.attachments.map((a) => a.filename).sort()).toEqual(['agenda.txt', 'map.png'])

  await alice.context.close()
  await bob.context.close()
})

test('viewers see attachments but get no upload controls', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const ev = await createEvent(bob.page, uniqueTitle('Flyer'))
  await bob.page.goto(`/?event=${ev.id}`)
  await bob.page
    .getByRole('complementary', { name: ev.title })
    .locator('input[type="file"]')
    .setInputFiles({ name: 'flyer.txt', mimeType: 'text/plain', buffer: Buffer.from('Come along') })
  await expect(bob.page.getByRole('link', { name: 'Download flyer.txt' })).toBeVisible()

  const carol = await signedInContext(browser, 'carol')
  await carol.page.goto(`/?event=${ev.id}`)
  const panel = carol.page.getByRole('complementary', { name: ev.title })
  await expect(panel.getByRole('link', { name: 'Download flyer.txt' })).toBeVisible()
  await expect(panel.getByRole('button', { name: 'Attach' })).toHaveCount(0)
  await expect(panel.getByRole('button', { name: 'Remove flyer.txt' })).toHaveCount(0)

  await carol.context.close()
  await bob.context.close()
})
