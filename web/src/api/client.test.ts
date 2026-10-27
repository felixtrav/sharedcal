import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api, buildUrl, fieldErrors, setCsrfToken, setUnauthorizedHandler } from './client'

function jsonResponse(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'content-type': status >= 400 ? 'application/problem+json' : 'application/json', ...headers },
  })
}

describe('api client', () => {
  const fetchMock = vi.fn<typeof fetch>()
  const onUnauthorized = vi.fn()

  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock)
    setUnauthorizedHandler(onUnauthorized)
    setCsrfToken('tok-123')
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
    onUnauthorized.mockReset()
  })

  it('builds query strings with repeated params and skips empties', () => {
    expect(buildUrl('/api/events', { from: 'a', to: 'b', tag: ['x', 'y'], q: '', owner: undefined })).toBe('/api/events?from=a&to=b&tag=x&tag=y')
  })

  it('does not send the CSRF token on GET', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { ok: true }))
    await api.get('/api/me')
    const init = fetchMock.mock.calls[0]?.[1]
    expect((init?.headers as Record<string, string>)['X-CSRF-Token']).toBeUndefined()
    expect(init?.credentials).toBe('same-origin')
  })

  it('adds X-CSRF-Token and JSON body on non-GET requests', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(201, { id: '1' }))
    const res = await api.post<{ id: string }>('/api/events', { title: 'x' })
    expect(res.id).toBe('1')
    const init = fetchMock.mock.calls[0]?.[1]
    const headers = init?.headers as Record<string, string>
    expect(headers['X-CSRF-Token']).toBe('tok-123')
    expect(headers['Content-Type']).toBe('application/json')
    expect(init?.body).toBe('{"title":"x"}')
  })

  it('passes If-Match and returns undefined for 204', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }))
    const res = await api.del('/api/events/1', { headers: { 'If-Match': '4' } })
    expect(res).toBeUndefined()
    expect((fetchMock.mock.calls[0]?.[1]?.headers as Record<string, string>)['If-Match']).toBe('4')
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe('DELETE')
  })

  it('parses problem+json into a typed ApiError', async () => {
    fetchMock.mockResolvedValueOnce(
      jsonResponse(422, {
        type: 'about:blank',
        title: 'Unprocessable Entity',
        status: 422,
        detail: 'validation failed',
        errors: [{ message: 'title is required', location: 'body.title', value: '' }],
      }),
    )
    const err = await api.post('/api/events', {}).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    const e = err as ApiError
    expect(e.status).toBe(422)
    expect(e.message).toBe('validation failed')
    expect(fieldErrors(e)).toEqual({ title: 'title is required' })
  })

  it('exposes the current event on 409 and suggest on 403', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(409, { status: 409, title: 'Conflict', current: { id: 'ev', version: 7 } }))
    const conflict = (await api.patch('/api/events/ev', {}).catch((e: unknown) => e)) as ApiError
    expect(conflict.isConflict).toBe(true)
    expect(conflict.problem.current?.version).toBe(7)

    fetchMock.mockResolvedValueOnce(jsonResponse(403, { status: 403, title: 'Forbidden', suggest: true }))
    const forbidden = (await api.patch('/api/events/ev', {}).catch((e: unknown) => e)) as ApiError
    expect(forbidden.problem.suggest).toBe(true)
  })

  it('reads Retry-After on 429', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(429, { status: 429, title: 'Too Many Requests' }, { 'retry-after': '12' }))
    const e = (await api.post('/api/events', {}).catch((x: unknown) => x)) as ApiError
    expect(e.retryAfter).toBe(12)
  })

  it('redirects to sign-in on 401 unless told not to', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(401, { status: 401, title: 'Unauthorized' }))
    await api.get('/api/me').catch(() => undefined)
    expect(onUnauthorized).toHaveBeenCalledTimes(1)

    fetchMock.mockResolvedValueOnce(jsonResponse(401, { status: 401, title: 'Unauthorized' }))
    await api.get('/api/me', { noAuthRedirect: true }).catch(() => undefined)
    expect(onUnauthorized).toHaveBeenCalledTimes(1)
  })

  it('handles non-JSON error bodies', async () => {
    fetchMock.mockResolvedValueOnce(new Response('bad gateway', { status: 502, statusText: 'Bad Gateway', headers: { 'content-type': 'text/plain' } }))
    const e = (await api.get('/api/me').catch((x: unknown) => x)) as ApiError
    expect(e.status).toBe(502)
  })
})
