import { expect, test, type Page } from '@playwright/test'
import { api, midWeekDate, signedInContext, uniqueTitle } from './helpers'

type ApiType = { id: string }
type ApiEvent = { id: string; title: string; fields: Record<string, string[]> }

test('a writer adds a person without an account, an admin approves and later links them to a user', async ({ browser }) => {
  const bob = await signedInContext(browser, 'bob')
  const alice = await signedInContext(browser, 'alice')
  const carol = await signedInContext(browser, 'carol')
  const me = await api<{ user: { id: string; name: string } }>(carol.page, 'GET', '/api/me')
  const carolName = me.user.name
  const first = uniqueTitle('Emma')
  const kid = `${first} Smith`
  const rowOf = (page: Page) => page.getByRole('list', { name: 'Without an account' }).getByRole('listitem').filter({ hasText: kid })

  // Writers submit; the submission waits for approval and is private to its author and the admins.
  await bob.page.goto('/people')
  await expect(bob.page.getByRole('heading', { name: 'Calendar users' })).toBeVisible()
  await bob.page.getByLabel('First name').fill(first)
  // A first name alone isn't enough.
  await expect(bob.page.getByRole('button', { name: 'Submit for approval' })).toBeDisabled()
  await bob.page.getByLabel('Last name').fill('Smith')
  await bob.page.getByRole('button', { name: 'Submit for approval' }).click()
  const bobRow = rowOf(bob.page)
  await expect(bobRow).toContainText('Waiting for approval')
  await expect(bobRow.getByRole('button', { name: `Approve ${kid}` })).toHaveCount(0)

  await carol.page.goto('/people')
  await expect(carol.page.getByRole('heading', { name: 'Without an account' })).toBeVisible()
  await expect(carol.page.getByText(kid)).toHaveCount(0)
  await expect(carol.page.getByLabel('First name')).toHaveCount(0)

  // The author already uses the person in an event.
  const type = await api<ApiType>(alice.page, 'POST', '/api/event-types', {
    name: uniqueTitle('Birthday'),
    colorSlot: 'tag-2',
    titleTemplate: "{{who}}'s birthday",
    fields: [{ key: 'who', label: 'Who', kind: 'members', multiple: false, required: true }],
  })
  const people = await api<{ people: { id: string; name: string }[] }>(bob.page, 'GET', '/api/people')
  const personId = people.people.find((p) => p.name === kid)!.id
  const date = midWeekDate(2)
  const event = await api<ApiEvent>(bob.page, 'POST', '/api/events', {
    eventTypeId: type.id,
    fields: { who: [personId] },
    start: date,
    end: new Date(Date.parse(`${date}T00:00:00Z`) + 86_400_000).toISOString().slice(0, 10),
    timeZone: 'UTC',
    allDay: true,
  })
  expect(event.title).toBe(`${kid}'s birthday`)

  // An admin approves, then links the person to the user they became.
  await alice.page.goto('/')
  await expect(
    alice.page
      .getByRole('navigation', { name: 'Main navigation' })
      .first()
      .getByText(/\d+ (person|people) waiting for approval/),
  ).toBeAttached()
  await alice.page.goto('/people')
  const row = rowOf(alice.page)
  await row.getByRole('button', { name: `Approve ${kid}` }).click()
  await expect(row).not.toContainText('Waiting for approval')
  await row.getByRole('button', { name: `Link ${kid} to a user` }).click()
  await row.getByLabel('User').selectOption({ label: carolName })
  await row.getByRole('button', { name: 'Link to user' }).click()
  await expect(alice.page.getByText(/is now/).first()).toBeVisible()
  await expect(rowOf(alice.page)).toHaveCount(0)

  // Every reference now points at the user.
  const after = await api<ApiEvent>(alice.page, 'GET', `/api/events/${event.id}`)
  expect(after.fields.who).toEqual([me.user.id])
  expect(after.title).not.toContain(kid)
})
