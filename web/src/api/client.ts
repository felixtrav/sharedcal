import type { CalEvent } from './types'

/** RFC 9457 problem details as returned by the server. */
type Problem = {
  type?: string
  title?: string
  status: number
  detail?: string
  errors?: { message: string; location?: string; value?: unknown }[]
  /** 403 when the caller could suggest instead. */
  suggest?: boolean
  /** 409 version conflicts carry the current event. */
  current?: CalEvent
}

export class ApiError extends Error {
  readonly status: number
  readonly problem: Problem
  readonly retryAfter: number | null

  constructor(problem: Problem, retryAfter: number | null = null) {
    super(problem.detail || problem.title || `HTTP ${problem.status}`)
    this.name = 'ApiError'
    this.status = problem.status
    this.problem = problem
    this.retryAfter = retryAfter
  }

  get isConflict() {
    return this.status === 409
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

/** Field errors keyed by their JSON location (`body.title` → `title`). */
export function fieldErrors(e: unknown): Record<string, string> {
  if (!isApiError(e) || !e.problem.errors) return {}
  const out: Record<string, string> = {}
  for (const err of e.problem.errors) {
    const key = (err.location ?? '').replace(/^body\./, '')
    if (key && !out[key]) out[key] = err.message
  }
  return out
}

// ---------------------------------------------------------------------------
// Session state kept in memory only (never in localStorage).

let csrfToken = ''
export function setCsrfToken(token: string) {
  csrfToken = token
}

export function loginUrl(returnTo?: string) {
  const ret = returnTo ?? `${window.location.pathname}${window.location.search}${window.location.hash}`
  return `/auth/login?return=${encodeURIComponent(ret)}`
}

type UnauthorizedHandler = () => void
let onUnauthorized: UnauthorizedHandler = () => {
  window.location.assign(loginUrl())
}
/** Overridable for tests. */
export function setUnauthorizedHandler(fn: UnauthorizedHandler) {
  onUnauthorized = fn
}

// ---------------------------------------------------------------------------

type QueryValue = string | number | boolean | null | undefined | readonly (string | number)[]
type Query = Record<string, QueryValue>

export function buildUrl(path: string, query?: Query) {
  if (!query) return path
  const params = new URLSearchParams()
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === '') continue
    if (Array.isArray(v)) for (const item of v) params.append(k, String(item))
    else params.append(k, String(v))
  }
  const s = params.toString()
  return s ? `${path}?${s}` : path
}

type RequestOptions = {
  query?: Query
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
  /** Don't redirect to sign-in on 401 (used when probing the session). */
  noAuthRedirect?: boolean
}

async function parseProblem(res: Response): Promise<Problem> {
  const type = res.headers.get('content-type') ?? ''
  if (type.includes('json')) {
    try {
      const data = (await res.json()) as Partial<Problem>
      return { ...data, status: data.status ?? res.status }
    } catch {
      /* fall through */
    }
  }
  return { status: res.status, title: res.statusText }
}

async function request<T>(method: string, path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json', ...opts.headers }
  let body: BodyInit | undefined
  if (opts.body instanceof FormData) {
    body = opts.body
  } else if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  if (method !== 'GET' && method !== 'HEAD' && csrfToken) headers['X-CSRF-Token'] = csrfToken

  const res = await fetch(buildUrl(path, opts.query), {
    method,
    headers,
    body,
    credentials: 'same-origin',
    signal: opts.signal,
  })

  if (!res.ok) {
    if (res.status === 401 && !opts.noAuthRedirect) onUnauthorized()
    const retry = res.headers.get('retry-after')
    throw new ApiError(await parseProblem(res), retry ? Number(retry) : null)
  }
  if (res.status === 204 || res.headers.get('content-length') === '0') return undefined as T
  const type = res.headers.get('content-type') ?? ''
  if (!type.includes('json')) return undefined as T
  return (await res.json()) as T
}

export const api = {
  get: <T>(path: string, opts?: RequestOptions) => request<T>('GET', path, opts),
  post: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>('POST', path, { ...opts, body }),
  put: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>('PUT', path, { ...opts, body }),
  patch: <T>(path: string, body?: unknown, opts?: RequestOptions) => request<T>('PATCH', path, { ...opts, body }),
  del: <T = void>(path: string, opts?: RequestOptions) => request<T>('DELETE', path, opts),
}

// ---------------------------------------------------------------------------
// Uploads use XHR for progress events and cancellation.

export type UploadHandle<T> = {
  promise: Promise<T>
  abort: () => void
}

export class UploadAbortedError extends Error {
  constructor() {
    super('Upload cancelled')
    this.name = 'UploadAbortedError'
  }
}

export function upload<T>(path: string, file: File, onProgress?: (fraction: number) => void): UploadHandle<T> {
  const xhr = new XMLHttpRequest()
  const promise = new Promise<T>((resolve, reject) => {
    xhr.open('POST', path)
    xhr.withCredentials = true
    xhr.setRequestHeader('Accept', 'application/json')
    if (csrfToken) xhr.setRequestHeader('X-CSRF-Token', csrfToken)
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(e.loaded / e.total)
    }
    xhr.onload = () => {
      let data: unknown = undefined
      try {
        data = xhr.responseText ? JSON.parse(xhr.responseText) : undefined
      } catch {
        data = undefined
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        onProgress?.(1)
        resolve(data as T)
        return
      }
      if (xhr.status === 401) onUnauthorized()
      const p = (data ?? {}) as Partial<Problem>
      const retry = xhr.getResponseHeader('retry-after')
      reject(new ApiError({ ...p, status: p.status ?? xhr.status, title: p.title ?? xhr.statusText }, retry ? Number(retry) : null))
    }
    xhr.onerror = () => reject(new ApiError({ status: 0, title: 'Network error' }))
    xhr.onabort = () => reject(new UploadAbortedError())
    const form = new FormData()
    form.append('file', file, file.name)
    xhr.send(form)
  })
  return { promise, abort: () => xhr.abort() }
}
