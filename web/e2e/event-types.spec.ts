import { expect, test } from '@playwright/test'
import { api, createEvent, eventChip, gotoWeek, listRange, midWeekDate, signedInContext, slotPoint, uniqueTitle, waitForCalendar } from './helpers'

type ApiType = { id: string; name: string }

test('an admin defines an event type and writers use its fields instead of a description', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  const name = uniqueTitle('Birthday')
  await alice.page.goto('/admin/event-types')
  await alice.page.getByLabel('Type name').fill(name)
  await alice.page.getByRole('button', { name: 'Add type' }).click()
  await expect(alice.page.getByText(`Event type “${name}” created`).first()).toBeVisible()

  // Give it fields and a look from the editor.
  const editor = alice.page.getByRole('form', { name })
  await editor.getByLabel('Icon').selectOption({ label: 'Cake' })
  await editor.getByLabel('Repetition').selectOption({ label: 'Always yearly' })
  await editor.getByLabel('Always all-day').click()
  await editor.getByLabel('Length').selectOption({ label: 'Single date' })
  await editor.getByLabel('Instructions').fill('Use the name on their ID.')
  await editor.getByRole('button', { name: 'Add field' }).click()
  await editor.getByLabel('Name of field 1').fill('Person')
  await editor.getByRole('checkbox', { name: 'Required' }).check()
  await editor.getByRole('button', { name: 'Add field' }).click()
  await editor.getByLabel('Name of field 2').fill('Relation')
  await editor.getByLabel('Kind of field 2').selectOption({ label: 'Choice' })
  await editor.getByLabel('Choices for field 2').fill('Family, Friend')
  await editor.getByRole('button', { name: 'Save' }).click()
  await expect(alice.page.getByText(`Event type “${name}” saved`).first()).toBeVisible()

  // A writer picks the type: defaults apply and the description gives way to the fields.
  const bob = await signedInContext(browser, 'bob')
  const date = midWeekDate()
  await gotoWeek(bob.page, date)
  await bob.page.getByRole('button', { name: 'New event' }).click()
  const panel = bob.page.getByRole('complementary', { name: 'New event' })
  const title = uniqueTitle('Sam')
  await panel.getByLabel('Title').fill(title)
  await panel.getByLabel('Type').selectOption({ label: name })
  await expect(panel.getByRole('complementary', { name: `Instructions for ${name}` })).toContainText('Use the name on their ID.')
  // The type fixes all-day and a yearly repeat, so those controls give way to a single date.
  await expect(panel.getByLabel('All day')).toHaveCount(0)
  await expect(panel.getByText('Always', { exact: true })).toBeVisible()
  await expect(panel.getByLabel('Date', { exact: true })).toBeVisible()
  await expect(panel.getByLabel('Last day')).toHaveCount(0)
  await expect(panel.getByLabel('Description')).toHaveCount(0)

  // Required fields are enforced before anything is sent.
  await panel.getByRole('button', { name: 'Create' }).click()
  await expect(panel.getByText('Person is required')).toBeVisible()

  await panel.getByLabel('Person').fill('Sam')
  await panel.getByRole('radio', { name: 'Friend' }).click()
  const created = bob.page.waitForResponse((r) => r.url().endsWith('/api/events') && r.request().method() === 'POST')
  await panel.getByRole('button', { name: 'Create' }).click()
  const res = await created
  expect(res.status()).toBe(201)
  const body = (await res.json()) as { rrule: string; fields: Record<string, unknown>; description: string }
  expect(body.fields).toEqual({ person: 'Sam', relation: 'Friend' })
  expect(body.description).toBe('')
  expect(body.rrule).toContain('FREQ=YEARLY')

  // The chip carries the type's icon-bearing look, and the panel shows the fields.
  await expect(eventChip(bob.page, title)).toBeVisible()
  await eventChip(bob.page, title).click()
  const details = bob.page.getByRole('complementary', { name: title })
  await expect(details.getByText('Person')).toBeVisible()
  await expect(details.getByText('Sam', { exact: true })).toBeVisible()

  // Types in use can't be deleted.
  const types = await api<{ eventTypes: ApiType[] }>(alice.page, 'GET', '/api/event-types')
  const id = types.eventTypes.find((t) => t.name === name)?.id
  expect(id).toBeTruthy()
  const csrf = ((await (await alice.page.request.get('/api/me')).json()) as { csrfToken: string }).csrfToken
  const del = await alice.page.request.delete(`/api/event-types/${id}`, { headers: { 'X-CSRF-Token': csrf, Origin: new URL(alice.page.url()).origin } })
  expect(del.status()).toBe(409)

  await alice.context.close()
  await bob.context.close()
})

test('a members field lets a writer pick people from the calendar', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  const name = uniqueTitle('Party')
  await api(alice.page, 'POST', '/api/event-types', {
    name,
    colorSlot: 'tag-4',
    fields: [{ key: 'guests', label: 'Guests', kind: 'members' }],
  })

  const bob = await signedInContext(browser, 'bob')
  await gotoWeek(bob.page, midWeekDate())
  await bob.page.getByRole('button', { name: 'New event' }).click()
  const panel = bob.page.getByRole('complementary', { name: 'New event' })
  const title = uniqueTitle('Housewarming')
  await panel.getByLabel('Title').fill(title)
  await panel.getByLabel('Type').selectOption({ label: name })
  const guests = panel.getByRole('group', { name: 'Guests' })
  await guests.getByRole('button', { name: /Alice/ }).click()
  await expect(guests.getByRole('button', { name: /Alice/ })).toHaveAttribute('aria-pressed', 'true')
  const created = bob.page.waitForResponse((r) => r.url().endsWith('/api/events') && r.request().method() === 'POST')
  await panel.getByRole('button', { name: 'Create' }).click()
  expect((await created).status()).toBe(201)

  await eventChip(bob.page, title).click()
  const details = bob.page.getByRole('complementary', { name: title })
  await expect(details.getByText('Guests')).toBeVisible()
  await expect(details.getByText(/Alice/)).toBeVisible()

  await alice.context.close()
  await bob.context.close()
})

test('presets, banner style, year counter, agenda order and quick create with defaults', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  await alice.page.goto('/admin/event-types')
  const types = await api<{ eventTypes: { id: string; name: string }[] }>(alice.page, 'GET', '/api/event-types')
  for (const preset of ['Birthday', 'Deadline']) {
    if (!types.eventTypes.some((t) => t.name === preset)) {
      await alice.page.getByRole('button', { name: preset, exact: true }).click()
      await expect(alice.page.getByText(`Event type “${preset}” created`).first()).toBeVisible()
    }
  }
  for (const preset of ['Birthday', 'Deadline']) {
    // Existing types start collapsed; newly added ones are already open.
    const expand = alice.page.getByRole('button', { name: `Expand ${preset}`, exact: true })
    if (await expand.count()) await expand.click()
  }
  const birthday = alice.page.getByRole('form', { name: 'Birthday', exact: true })
  await expect(birthday.getByLabel('Automatic title')).toHaveValue("{{person}}'s Birthday")
  await expect(birthday.getByLabel('Calendar style')).toHaveValue('banner')
  await expect(birthday.getByLabel('Count years since the first occurrence')).toBeChecked()
  const deadline = alice.page.getByRole('form', { name: 'Deadline', exact: true })
  await expect(deadline.getByLabel('Calendar style')).toHaveValue('outline')
  await expect(deadline.getByLabel('New events last (minutes)')).toHaveValue('0')

  const bob = await signedInContext(browser, 'bob')
  const list = await api<{ eventTypes: { id: string; name: string }[] }>(bob.page, 'GET', '/api/event-types')
  const typeId = (name: string) => list.eventTypes.find((t) => t.name === name)?.id

  // A yearly birthday that started in 1990 shows the years on this year's occurrence.
  const date = midWeekDate()
  const person = uniqueTitle('Dana')
  const name = `${person}'s Birthday`
  await api(bob.page, 'POST', '/api/events', {
    eventTypeId: typeId('Birthday'),
    fields: { person },
    allDay: true,
    start: `1990${date.slice(4)}`,
    end: `1990${date.slice(4, 8)}${String(Number(date.slice(8)) + 1).padStart(2, '0')}`,
    timeZone: 'America/New_York',
    rrule: 'FREQ=YEARLY',
  })
  const years = new Date().getFullYear() - 1990
  const timed = uniqueTitle('Standup')
  await createEvent(bob.page, timed, date, 8)

  await gotoWeek(bob.page, date)
  const chip = eventChip(bob.page, name)
  await expect(chip).toHaveClass(/sc-style-banner/)
  await expect(chip).toContainText(`· ${years}`)

  // Banner events come first within the day in the agenda.
  await bob.page.goto(`/?view=agenda&date=${date}`)
  await waitForCalendar(bob.page)
  const bannerBox = await eventChip(bob.page, name).boundingBox()
  const timedBox = await eventChip(bob.page, timed).boundingBox()
  expect(bannerBox && timedBox && bannerBox.y < timedBox.y).toBeTruthy()

  // Quick create with a type applies its defaults: a Deadline is an instant.
  await gotoWeek(bob.page, midWeekDate(1))
  const p = await slotPoint(bob.page, midWeekDate(1), '15:00:00')
  await bob.page.mouse.click(p.x, p.y)
  const popover = bob.page.getByRole('dialog', { name: 'New event' })
  const title = uniqueTitle('Report due')
  await popover.getByPlaceholder('Add a title').fill(title)
  await popover.getByLabel('Type').selectOption({ label: 'Deadline' })
  const created = bob.page.waitForResponse((r) => r.url().endsWith('/api/events') && r.request().method() === 'POST')
  await popover.getByRole('button', { name: 'Create' }).click()
  const body = (await (await created).json()) as { start: string; end: string; eventTypeId: string }
  expect(body.eventTypeId).toBe(typeId('Deadline'))
  expect(body.end).toBe(body.start)

  await alice.context.close()
  await bob.context.close()
})

test('one occurrence of a series can have its own type and fields', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  const name = uniqueTitle('Movie')
  await api(alice.page, 'POST', '/api/event-types', {
    name,
    colorSlot: 'tag-2',
    fields: [{ key: 'film', label: 'Film', kind: 'text', required: true }],
  })

  const bob = await signedInContext(browser, 'bob')
  const title = uniqueTitle('Premieres')
  const first = midWeekDate()
  const ev = await api<{ id: string }>(bob.page, 'POST', '/api/events', {
    title,
    description: 'Weekly roundup',
    start: `${first}T20:00:00`,
    end: `${first}T22:00:00`,
    timeZone: 'America/New_York',
    allDay: false,
    rrule: 'FREQ=WEEKLY;COUNT=3',
  })

  // Open next week's occurrence and make it a particular release.
  await gotoWeek(bob.page, midWeekDate(1))
  await eventChip(bob.page, title).click()
  const panel = bob.page.getByRole('complementary', { name: title })
  await panel.getByRole('button', { name: 'Edit' }).click()
  await panel.getByLabel('Type').selectOption({ label: name })
  await panel.getByLabel('Film').fill('Dune')
  await panel.getByRole('button', { name: 'Save' }).click()
  const scope = bob.page.getByRole('dialog', { name: 'Change recurring event' })
  await scope.getByRole('radio', { name: 'This event' }).check()
  await scope.getByRole('button', { name: 'Save' }).click()
  await expect(bob.page.getByText('Event saved').first()).toBeVisible()

  const occ = await listRange(bob.page, `${midWeekDate(-1)}T00:00:00`, `${midWeekDate(4)}T00:00:00`)
  const mine = occ.filter((o) => o.id === ev.id) as unknown as { eventTypeId: string | null; fields: Record<string, unknown>; description: string }[]
  expect(mine).toHaveLength(3)
  expect(mine.map((o) => o.eventTypeId !== null)).toEqual([false, true, false])
  expect(mine[1]?.fields).toEqual({ film: 'Dune' })
  expect(mine[1]?.description).toBe('')
  expect(mine[0]?.description).toBe('Weekly roundup')

  // The panel shows the occurrence's fields; the other weeks keep the description.
  await expect(panel.getByText('Dune')).toBeVisible()
  await gotoWeek(bob.page, midWeekDate(2))
  await eventChip(bob.page, title).click()
  await expect(bob.page.getByRole('complementary', { name: title }).getByText('Weekly roundup')).toBeVisible()

  await alice.context.close()
  await bob.context.close()
})

test('a title template generates event titles with a live preview and no typing', async ({ browser }) => {
  const alice = await signedInContext(browser, 'alice')
  const name = uniqueTitle('Anniv')
  await alice.page.goto('/admin/event-types')
  await alice.page.getByLabel('Type name').fill(name)
  await alice.page.getByRole('button', { name: 'Add type' }).click()
  await expect(alice.page.getByText(`Event type “${name}” created`).first()).toBeVisible()

  const editor = alice.page.getByRole('form', { name })
  await editor.getByRole('button', { name: 'Add field' }).click()
  await editor.getByLabel('Name of field 1').fill('Couple')
  await editor.getByRole('checkbox', { name: 'Required' }).check()
  await editor.getByRole('button', { name: 'Insert Couple' }).click()
  await editor.getByLabel('Automatic title').pressSequentially(' Anniversary')
  await expect(editor.getByText('Couple Anniversary')).toBeVisible()
  await editor.getByRole('button', { name: 'Save' }).click()
  await expect(alice.page.getByText(`Event type “${name}” saved`).first()).toBeVisible()

  const bob = await signedInContext(browser, 'bob')
  await gotoWeek(bob.page, midWeekDate())
  await bob.page.getByRole('button', { name: 'New event' }).click()
  const panel = bob.page.getByRole('complementary', { name: 'New event' })
  await panel.getByLabel('Type').selectOption({ label: name })
  const title = panel.getByLabel('Title')
  await expect(title).toHaveAttribute('readonly', '')
  await panel.getByLabel('Couple').fill('Ana & Ben')
  await expect(title).toHaveValue('Ana & Ben Anniversary')

  const created = bob.page.waitForResponse((r) => r.url().endsWith('/api/events') && r.request().method() === 'POST')
  await panel.getByRole('button', { name: 'Create' }).click()
  const res = await created
  expect(res.status()).toBe(201)
  expect(((await res.json()) as { title: string }).title).toBe('Ana & Ben Anniversary')

  // Picking a type with required fields in quick create opens the full form with that type chosen.
  await gotoWeek(bob.page, midWeekDate(1))
  const point = await slotPoint(bob.page, midWeekDate(1), '10:00:00')
  await bob.page.mouse.click(point.x, point.y)
  await bob.page.getByRole('dialog', { name: 'New event' }).getByLabel('Type').selectOption({ label: name })
  const full = bob.page.getByRole('complementary', { name: 'New event' })
  await expect(full.getByLabel('Type')).toHaveValue(/.+/)
  await expect(full.getByLabel('Couple')).toBeVisible()

  await alice.context.close()
  await bob.context.close()
})
