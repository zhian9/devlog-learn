import type {
  ConfigResponse,
  Doc,
  DocDetailResponse,
  DocListResponse,
  EventsResponse,
  ExportResponse,
  ExtractJobRequest,
  JobSnapshot,
  QuestionsResponse,
  SessionList,
  StatusResponse,
} from './types'

const TOKEN_KEY = 'devlog-learn-token'

/** Move a token from the dev URL into session storage, then clean the URL. */
export function captureToken(): void {
  try {
    const url = new URL(window.location.href)
    const token = url.searchParams.get('token')
    if (!token) return
    sessionStorage.setItem(TOKEN_KEY, token)
    url.searchParams.delete('token')
    window.history.replaceState({}, '', url.pathname + url.search + url.hash)
  } catch {
    // Storage can be disabled; cookie auth still works in production.
  }
}

function authHeaders(): Headers {
  const headers = new Headers()
  try {
    const token = sessionStorage.getItem(TOKEN_KEY)
    if (token) headers.set('X-Devlog-Token', token)
  } catch {
    // ignore
  }
  return headers
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = authHeaders()
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const response = await fetch(path, { ...init, headers })
  if (!response.ok) {
    let message = `${response.status} ${response.statusText}`
    try {
      const body = (await response.json()) as { error?: string }
      if (body.error) message = body.error
    } catch {
      // keep the status line
    }
    throw new Error(message)
  }
  return (await response.json()) as T
}

function query(params: Record<string, string | number | boolean | string[] | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '' || value === false) continue
    if (Array.isArray(value)) {
      for (const item of value) search.append(key, item)
    } else {
      search.set(key, String(value))
    }
  }
  const text = search.toString()
  return text ? `?${text}` : ''
}

export const api = {
  status: () => request<StatusResponse>('/api/status'),
  config: () => request<ConfigResponse>('/api/config'),
  docs: (params: {
    q?: string
    project?: string
    review?: string
    lifecycle?: string
    sort?: string
    tag?: string[]
  }) => request<DocListResponse>(`/api/docs${query(params)}`),
  doc: (id: string) => request<DocDetailResponse>(`/api/docs/${encodeURIComponent(id)}`),
  markdown: async (id: string, profile: 'agent' | 'learning') => {
    const headers = authHeaders()
    const response = await fetch(
      `/api/docs/${encodeURIComponent(id)}/markdown?profile=${profile}`,
      { headers },
    )
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
    return response.text()
  },
  events: (id: string, seqs: number[]) =>
    request<EventsResponse>(
      `/api/docs/${encodeURIComponent(id)}/events${query({ seq: seqs.join(',') })}`,
    ),
  review: (id: string, status: string, note?: string) =>
    request<Doc>(`/api/docs/${encodeURIComponent(id)}/review`, {
      method: 'POST',
      body: JSON.stringify({ status, note }),
    }),
  questions: (status: string) =>
    request<QuestionsResponse>(`/api/questions${query({ status })}`),
  resolveQuestion: (id: string, note: string) =>
    request<{ id: string; resolved: boolean }>(
      `/api/questions/${encodeURIComponent(id)}/resolve`,
      { method: 'POST', body: JSON.stringify({ note }) },
    ),
  reopenQuestion: (id: string) =>
    request<{ id: string; resolved: boolean }>(
      `/api/questions/${encodeURIComponent(id)}/reopen`,
      { method: 'POST', body: '{}' },
    ),
  export: (body: {
    profile: string
    include_stale: boolean
    prune?: boolean
    digest?: boolean
    digest_review?: string
    digest_project?: string
    digest_limit?: number
    force_digest?: boolean
  }) =>
    request<ExportResponse>('/api/export', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  sessions: (params: { provider: string; since: string; limit?: number }) =>
    request<SessionList>(`/api/sessions${query(params)}`),
  startExtract: (body: ExtractJobRequest) =>
    request<{ job: JobSnapshot }>('/api/jobs/extract', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  currentJob: () => request<{ job: JobSnapshot | null }>('/api/jobs/current'),
  cancelJob: (id: string) =>
    request<{ job: JobSnapshot }>(`/api/jobs/${encodeURIComponent(id)}/cancel`, {
      method: 'POST',
      body: '{}',
    }),
}

/** EventSource cannot set headers, so the dev token travels in the query. */
export function jobEventsUrl(id: string, after = 0): string {
  const params = new URLSearchParams()
  if (after > 0) params.set('after', String(after))
  try {
    const token = sessionStorage.getItem(TOKEN_KEY)
    if (token) params.set('token', token)
  } catch {
    // cookie auth is enough in production
  }
  const suffix = params.toString()
  return `/api/jobs/${encodeURIComponent(id)}/events${suffix ? `?${suffix}` : ''}`
}

export function formatDate(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).format(date)
}

export function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function formatBytes(value: number): string {
  if (!value) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}
