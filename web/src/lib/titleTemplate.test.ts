import { describe, expect, it } from 'vitest'
import { applySections, renderTitle } from './titleTemplate'

const type = (titleTemplate: string) => ({ name: 'Birthday', titleTemplate, fields: [{ key: 'guests', label: 'Guests', kind: 'members' as const }] })

describe('renderTitle', () => {
  it('fills placeholders from field values', () => {
    expect(renderTitle(type("{{person}}'s Birthday"), { person: 'Sam' })).toBe("Sam's Birthday")
    expect(renderTitle(type('{{ a }} and {{b}}'), { a: 'X', b: 2 })).toBe('X and 2')
  })

  it('joins lists, drops missing values and collapses spaces', () => {
    expect(renderTitle(type('{{tags}} {{gone}} party'), { tags: ['A', 'B'] })).toBe('A, B party')
  })

  it('falls back to the type name when nothing is left', () => {
    expect(renderTitle(type("{{person}}'s"), {})).toBe("'s")
    expect(renderTitle(type('{{person}}'), {})).toBe('Birthday')
  })

  it('shows people by their current name and skips unknown ids', () => {
    const names: Record<string, string> = { u1: 'Alice', u2: 'Bob' }
    expect(renderTitle(type('Party for {{guests}}'), { guests: ['u1', 'gone', 'u2'] }, (id) => names[id])).toBe('Party for Alice, Bob')
  })

  it('shows optional parts only when their field has a value', () => {
    const release = {
      name: 'Release',
      titleTemplate: '{{name}} Releases{{#at}} @{{at}}{{/at}}',
      fields: [
        { key: 'name', label: 'Name', kind: 'text' as const },
        { key: 'at', label: 'At', kind: 'time' as const },
      ],
    }
    expect(renderTitle(release, { name: 'Game X' })).toBe('Game X Releases')
    expect(renderTitle(release, { name: 'Game X', at: '  ' })).toBe('Game X Releases')
    expect(renderTitle(release, { name: 'Game X', at: '19:30' })).toBe('Game X Releases @19:30')
  })

  it('handles nested parts and leaves unbalanced tags alone', () => {
    const t = { name: 'T', titleTemplate: '{{a}}{{#b}} [{{b}}{{#c}} {{c}}{{/c}}]{{/b}}', fields: [] }
    expect(renderTitle(t, { a: 'A', b: 'B' })).toBe('A [B]')
    expect(renderTitle(t, { a: 'A', b: 'B', c: 'C' })).toBe('A [B C]')
    expect(renderTitle(t, { a: 'A' })).toBe('A')
    expect(applySections('x{{#a}}y', () => true)).toBe('x{{#a}}y')
  })

  it('rewrites time values for the viewer when given a time formatter', () => {
    const release = {
      name: 'Release',
      titleTemplate: '{{name}}{{#at}} @{{at}}{{/at}}',
      fields: [
        { key: 'name', label: 'Name', kind: 'text' as const },
        { key: 'at', label: 'At', kind: 'time' as const },
      ],
    }
    expect(renderTitle(release, { name: 'X', at: '19:30' }, undefined, (t) => `${t}!`)).toBe('X @19:30!')
    expect(renderTitle(release, { name: 'X' }, undefined, () => 'never')).toBe('X')
  })
})
