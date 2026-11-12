import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronRight,
  Loader2,
  RefreshCw,
  Search,
  Square,
  Terminal,
  Zap,
} from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, formatDate, jobEventsUrl } from '../api'
import {
  Badge,
  Banner,
  EmptyState,
  ErrorBox,
  Loading,
  PageHeader,
} from '../components/ui'
import type { JobEvent, JobSnapshot, SelectableSession } from '../types'

function statusLabel(status: string): string {
  switch (status) {
    case 'running':
      return '正在提炼'
    case 'done':
      return '提炼完成'
    case 'failed':
      return '提炼失败'
    case 'canceled':
      return '已取消'
    default:
      return status
  }
}

function statusTone(status: string): 'green' | 'amber' | 'red' | 'neutral' | 'blue' {
  switch (status) {
    case 'running':
      return 'blue'
    case 'done':
      return 'green'
    case 'failed':
      return 'red'
    case 'canceled':
      return 'amber'
    default:
      return 'neutral'
  }
}

type PendingStart =
  | { kind: 'sessions'; sessions: string[] }
  | { kind: 'segments'; segments: string[] }

export function Extract() {
  const queryClient = useQueryClient()
  const [params, setParams] = useSearchParams()
  const provider = params.get('provider') ?? 'all'
  const since = params.get('since') ?? '7d'
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [search, setSearch] = useState('')
  const [onlyPending, setOnlyPending] = useState(false)
  const [pending, setPending] = useState<PendingStart | null>(null)
  const [streamBroken, setStreamBroken] = useState(false)
  const [job, setJob] = useState<JobSnapshot | null>(null)
  const previousStatus = useRef<string | undefined>(undefined)

  const sessions = useQuery({
    queryKey: ['sessions', provider, since],
    queryFn: () => api.sessions({ provider, since }),
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  })

  const jobId = job?.id
  const running = job?.status === 'running'
  const currentJob = useQuery({
    queryKey: ['currentJob'],
    queryFn: api.currentJob,
    refetchOnWindowFocus: false,
    refetchInterval: running && streamBroken ? 2000 : job ? false : 10_000,
  })

  useEffect(() => {
    const incoming = currentJob.data?.job
    if (!incoming) return
    setJob((previous) => {
      if (!previous) return incoming
      if (previous.id !== incoming.id) return incoming
      // While the stream is connected it is the authoritative progress source.
      if (previous.status === 'running' && !streamBroken) return previous
      return incoming
    })
  }, [currentJob.data, streamBroken])

  useEffect(() => {
    setStreamBroken(false)
  }, [jobId])

  useEffect(() => {
    if (!jobId || !running) return
    const after = job?.events?.length
      ? Math.max(...job.events.map((event) => event.seq ?? 0)) + 1
      : 0
    const source = new EventSource(jobEventsUrl(jobId, after))
    source.onmessage = (message) => {
      let event: JobEvent
      try {
        event = JSON.parse(message.data) as JobEvent
      } catch {
        return
      }
      setJob((previous) => mergeEvent(previous, jobId, event))
      if (['done', 'failed', 'canceled'].includes(event.phase)) {
        source.close()
      }
    }
    source.onerror = () => {
      // Fall back to polling; a local server restart or sleep/resume should not
      // freeze the progress panel. The currentJob query picks this up.
      source.close()
      setStreamBroken(true)
      queryClient.invalidateQueries({ queryKey: ['currentJob'] })
    }
    return () => source.close()
  }, [jobId, running, queryClient])

  useEffect(() => {
    const wasRunning = previousStatus.current === 'running'
    previousStatus.current = job?.status
    if (wasRunning && job && job.status !== 'running') {
      queryClient.invalidateQueries({ queryKey: ['status'] })
      queryClient.invalidateQueries({ queryKey: ['docs'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      queryClient.invalidateQueries({ queryKey: ['currentJob'] })
    }
  }, [job?.status, queryClient])

  const start = useMutation({
    mutationFn: (request: PendingStart) =>
      api.startExtract(
        request.kind === 'segments'
          ? {
              // Retries must be able to find the original segment regardless
              // of the current time/provider filters.
              provider: 'all',
              since: 'all',
              sessions: [],
              segments: request.segments,
              concurrency: 2,
              timeout_seconds: 180,
            }
          : {
              provider,
              since,
              sessions: request.sessions,
              concurrency: 2,
              timeout_seconds: 180,
            },
      ),
    onSuccess: (result) => {
      setJob(result.job)
      setStreamBroken(false)
      setSelected(new Set())
      setPending(null)
      queryClient.invalidateQueries({ queryKey: ['currentJob'] })
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
    },
  })

  const cancel = useMutation({
    mutationFn: (id: string) => api.cancelJob(id),
    onSuccess: (result) => setJob(result.job),
  })

  useEffect(() => {
    if (!pending) return
    const listener = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setPending(null)
    }
    window.addEventListener('keydown', listener)
    return () => window.removeEventListener('keydown', listener)
  }, [pending])

  const updateParam = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if ((key === 'provider' && value === 'all') || (key === 'since' && value === '7d')) {
      next.delete(key)
    } else {
      next.set(key, value)
    }
    setParams(next, { replace: true })
    setSelected(new Set())
  }

  const allSessions = sessions.data?.sessions ?? []
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase()
    return allSessions
      .filter((session) => {
        if (onlyPending && session.extracted >= session.segment_count) return false
        if (!query) return true
        return (
          session.title.toLowerCase().includes(query) ||
          session.project.toLowerCase().includes(query) ||
          session.selector.toLowerCase().includes(query)
        )
      })
      .sort((a, b) => (a.start < b.start ? 1 : -1))
  }, [allSessions, onlyPending, search])

  const selectedSessions = useMemo(
    () => allSessions.filter((session) => selected.has(session.selector)),
    [allSessions, selected],
  )
  const selectedSegments = selectedSessions.reduce((sum, session) => sum + session.segment_count, 0)
  const selectedExtracted = selectedSessions.reduce((sum, session) => sum + session.extracted, 0)
  const selectedEvents = selectedSessions.reduce((sum, session) => sum + session.event_count, 0)

  const toggle = (selector: string) => {
    setSelected((previous) => {
      const next = new Set(previous)
      if (next.has(selector)) next.delete(selector)
      else next.add(selector)
      return next
    })
  }

  const toggleAll = () => {
    const visible = filtered.map((session) => session.selector)
    const allSelected = visible.length > 0 && visible.every((selector) => selected.has(selector))
    setSelected((previous) => {
      const next = new Set(previous)
      if (allSelected) {
        for (const selector of visible) next.delete(selector)
      } else {
        for (const selector of visible) next.add(selector)
      }
      return next
    })
  }

  const progress = job && job.total > 0 ? Math.min(100, Math.round((job.done / job.total) * 100)) : 0
  const failedSegments = (job?.result?.reports ?? [])
    .filter((report) => report.error && report.segment_id)
    .map((report) => report.segment_id)

  return (
    <div className="page">
      <PageHeader
        eyebrow="EXTRACT"
        title="提炼新案例"
        subtitle="从本地 Claude Code / Codex 会话里生成结构化学习文档。选择会话后才会调用模型。"
        actions={
          <button
            className="button ghost"
            onClick={() => sessions.refetch()}
            disabled={sessions.isFetching || running}
          >
            <RefreshCw size={15} className={sessions.isFetching ? 'spin' : ''} />
            重新扫描
          </button>
        }
      />

      {sessions.error && <ErrorBox error={sessions.error} />}
      {sessions.data?.warnings && sessions.data.warnings.length > 0 && (
        <Banner tone="amber">
          {sessions.data.warnings.slice(0, 3).join('；')}
          {sessions.data.warnings.length > 3 ? '…' : ''}
        </Banner>
      )}

      {job && (
        <section className={`panel job-panel job-${job.status}`}>
          <div className="job-head">
            <div>
              <div className="job-title">
                <Badge tone={statusTone(job.status)}>{statusLabel(job.status)}</Badge>
                <span className="muted">{formatDate(job.started_at)}</span>
                {running && <Loader2 size={15} className="spin" />}
              </div>
              <p className="job-sub">
                {job.total > 0 ? `${job.done} / ${job.total} 个 Segment` : '正在扫描会话日志…'}
                {job.total > 0 && ` · ${progress}%`}
              </p>
            </div>
            <div className="job-actions">
              {running && (
                <button
                  className="button ghost small"
                  onClick={() => cancel.mutate(job.id)}
                  disabled={cancel.isPending}
                >
                  <Square size={13} /> 取消
                </button>
              )}
              {!running && failedSegments.length > 0 && (
                <button
                  className="button ghost small"
                  onClick={() => setPending({ kind: 'segments', segments: failedSegments })}
                >
                  <RefreshCw size={13} /> 重试失败片段（{failedSegments.length}）
                </button>
              )}
              {!running && (
                <Link className="button primary small" to="/library">
                  打开文档库 <ChevronRight size={14} />
                </Link>
              )}
            </div>
          </div>

          <div className="progress-track">
            <div
              className={`progress-fill${job.status === 'failed' ? ' failed' : ''}`}
              style={{ width: `${Math.max(progress, running ? 6 : 0)}%` }}
            />
          </div>

          <div className="job-stats">
            <span>
              新增 <strong>{job.extracted}</strong>
            </span>
            <span>
              缓存命中 <strong>{job.cached}</strong>
            </span>
            <span>
              失败 <strong>{job.failed}</strong>
            </span>
            {job.result && (
              <>
                <span>
                  扫描文件 <strong>{job.result.files}</strong>
                </span>
                <span>
                  耗时 <strong>{formatDuration(job.result.duration_ms)}</strong>
                </span>
              </>
            )}
          </div>

          {job.error && (
            <Banner tone={job.status === 'failed' ? 'red' : 'amber'}>{job.error}</Banner>
          )}
          {cancel.error && <ErrorBox error={cancel.error} />}

          {job.events && job.events.length > 0 && (
            <div className="job-events">
              {job.events.slice(-120).map((event) => (
                <div className={`job-event ${event.failed ? 'failed' : ''}`} key={event.seq}>
                  <span className={`job-event-dot ${event.phase}`} />
                  <div className="job-event-main">
                    <span>{eventText(event)}</span>
                    {event.phase === 'segment' && (
                      <span className="muted">
                        {event.project || event.source || ''}
                      </span>
                    )}
                  </div>
                  <Badge tone={event.failed ? 'red' : event.cached ? 'blue' : 'green'}>
                    {event.phase === 'segment'
                      ? event.failed
                        ? '失败'
                        : event.cached
                          ? '缓存'
                          : '完成'
                      : event.phase}
                  </Badge>
                </div>
              ))}
            </div>
          )}

          {job.result && job.result.failed_segments > 0 && (
            <div className="job-failures">
              <AlertTriangle size={15} />
              <span>
                {job.result.failed_segments} 个 Segment 失败。已成功的结果与缓存不会被删除，可以稍后重试。
              </span>
            </div>
          )}
        </section>
      )}

      <section className="panel session-panel">
        <div className="panel-head">
          <div>
            <h2>
              <Terminal size={18} /> 可选择的会话
            </h2>
            <p>
              {sessions.isFetching
                ? '正在扫描并解析日志，第一次可能需要几十秒…'
                : sessions.data
                  ? `${allSessions.length} 个会话 · ${sessions.data.segment_count} 个 Segment · 丢弃 ${sessions.data.drop_count}`
                  : '尚未扫描'}
            </p>
          </div>
          {sessions.data && (
            <span className="muted small-text">
              生成于 {new Date(sessions.data.generated_at).toLocaleTimeString('zh-CN')}
            </span>
          )}
        </div>

        <div className="extract-controls">
          <select value={provider} onChange={(event) => updateParam('provider', event.target.value)} disabled={running}>
            <option value="all">全部来源</option>
            <option value="claude-code">Claude Code</option>
            <option value="codex">Codex</option>
          </select>
          <select value={since} onChange={(event) => updateParam('since', event.target.value)} disabled={running}>
            <option value="1d">最近 1 天</option>
            <option value="7d">最近 7 天</option>
            <option value="30d">最近 30 天</option>
            <option value="all">全部历史</option>
          </select>
          <div className="compact-search">
            <Search size={15} />
            <input
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder="筛选标题或项目"
              aria-label="筛选会话"
            />
          </div>
          <label className="check-row compact">
            <input
              type="checkbox"
              checked={onlyPending}
              onChange={(event) => setOnlyPending(event.target.checked)}
            />
            只看未提炼
          </label>
          <button className="button ghost small" onClick={toggleAll} disabled={filtered.length === 0 || running}>
            全选当前列表
          </button>
        </div>

        {sessions.isLoading && <Loading label="扫描会话日志…第一次可能需要几十秒" />}
        {sessions.data && filtered.length === 0 && (
          <EmptyState
            icon={<Terminal size={28} />}
            title="没有符合条件的会话"
            description="试试切换来源或时间范围，或者关闭“只看未提炼”。"
          />
        )}
        {filtered.length > 0 && (
          <div className="session-list">
            {filtered.map((session) => (
              <SessionRow
                key={session.selector}
                session={session}
                checked={selected.has(session.selector)}
                disabled={running}
                onToggle={() => toggle(session.selector)}
              />
            ))}
          </div>
        )}
      </section>

      <div className="action-bar">
        <div className="action-summary">
          <span>
            已选 <strong>{selectedSessions.length}</strong> 个会话 ·{' '}
            <strong>{selectedSegments}</strong> 个 Segment
          </span>
          <span className="muted">
            {selectedExtracted > 0
              ? `其中 ${selectedExtracted} 个已提炼，缓存命中时不会重复调用模型`
              : `${selectedEvents} 个事件将被解析`}
          </span>
        </div>
        <button
          className="button primary"
          disabled={selectedSegments === 0 || running}
          onClick={() => setPending({ kind: 'sessions', sessions: [...selected] })}
        >
          <Zap size={15} /> 开始提炼
        </button>
      </div>

      {pending && (
        <div className="modal-layer" role="dialog" aria-modal="true">
          <button className="modal-backdrop" onClick={() => setPending(null)} aria-label="取消" />
          <div className="modal">
            <div className="modal-head">
              <div>
                <div className="eyebrow">CONFIRM</div>
                <h2>{pending.kind === 'segments' ? '重试失败片段' : '确认开始提炼'}</h2>
              </div>
              <button className="icon-button" onClick={() => setPending(null)} aria-label="关闭">
                ×
              </button>
            </div>
            <div className="modal-body">
              <dl className="confirm-list">
                <div>
                  <dt>来源</dt>
                  <dd>
                    {pending.kind === 'segments' ? '全部来源（重试）' : providerLabel(provider)}
                  </dd>
                </div>
                <div>
                  <dt>时间范围</dt>
                  <dd>
                    {pending.kind === 'segments' ? '全部历史（定位原 Segment）' : sinceLabel(since)}
                  </dd>
                </div>
                <div>
                  <dt>会话 / Segment</dt>
                  <dd>
                    {pending.kind === 'segments'
                      ? `— / ${pending.segments.length}`
                      : `${selectedSessions.length} / ${selectedSegments}`}
                  </dd>
                </div>
                <div>
                  <dt>并发 / 超时</dt>
                  <dd>2 / 180 秒</dd>
                </div>
              </dl>
              <div className="confirm-warning">
                <AlertTriangle size={16} />
                <span>
                  每个未命中缓存的 Segment 会调用一次本地模型，可能产生费用。 已提炼过的内容会优先复用缓存。
                </span>
              </div>
              {start.error && <ErrorBox error={start.error} />}
            </div>
            <div className="modal-actions">
              <button className="button ghost" onClick={() => setPending(null)}>
                取消
              </button>
              <button
                className="button primary"
                onClick={() => start.mutate(pending)}
                disabled={start.isPending}
              >
                {start.isPending ? (
                  <Loader2 size={15} className="spin" />
                ) : (
                  <CheckCircle2 size={15} />
                )}
                {start.isPending
                  ? '正在启动…'
                  : pending.kind === 'segments'
                    ? '确认重试'
                    : '确认开始'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

function SessionRow({
  session,
  checked,
  disabled,
  onToggle,
}: {
  session: SelectableSession
  checked: boolean
  disabled: boolean
  onToggle: () => void
}) {
  const complete = session.extracted >= session.segment_count
  return (
    <div className={`session-row${checked ? ' selected' : ''}${complete ? ' complete' : ''}`}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={onToggle}
        aria-label={`选择会话 ${session.title}`}
      />
      <div className="session-main">
        <div className="session-title">
          {session.title || '（无会话标题）'}
          {complete && <Badge tone="outline">已全部提炼</Badge>}
        </div>
        <div className="session-meta">
          <span>{session.source}</span>
          <span>·</span>
          <span>{session.project || '未知项目'}</span>
          <span>·</span>
          <span>{formatDate(session.start)}</span>
          <span>·</span>
          <span>
            {session.segment_count} 个 Segment，已提炼 {session.extracted}
          </span>
          <span>·</span>
          <span>{session.event_count} 个事件</span>
        </div>
        <div className="session-selector">{session.selector}</div>
      </div>
      <details className="session-details">
        <summary>片段</summary>
        <div className="session-segments">
          {session.segments.map((segment) => (
            <div key={segment.id}>
              <span className={`dot ${segment.extracted ? 'ok' : 'warn'}`} />
              <span className="segment-title">{segment.title || segment.id}</span>
              <span className="muted">
                {formatDate(segment.start)} · {segment.event_count} 事件
              </span>
              {segment.extracted && <Badge tone="green">已提炼</Badge>}
            </div>
          ))}
        </div>
      </details>
    </div>
  )
}

function mergeEvent(previous: JobSnapshot | null, jobId: string, event: JobEvent): JobSnapshot | null {
  if (!previous || previous.id !== jobId) return previous
  if (previous.events?.some((item) => item.seq === event.seq)) return previous
  const next: JobSnapshot = { ...previous }
  next.events = [...(previous.events ?? []), event].slice(-300)
  if (event.phase === 'selected') next.total = event.total
  if (event.phase === 'segment') {
    next.total = event.total
    next.done = event.done
    if (event.cached) next.cached += 1
    else if (event.failed) next.failed += 1
    else next.extracted += 1
  }
  if (['done', 'failed', 'canceled'].includes(event.phase)) {
    next.status = event.phase as JobSnapshot['status']
    next.done = event.done
    next.total = event.total
    next.error = event.error
    next.finished_at = event.at
  }
  return next
}

function eventText(event: JobEvent): string {
  switch (event.phase) {
    case 'scan':
      return '扫描并解析本地会话日志…'
    case 'selected':
      return `已选中 ${event.total} 个 Segment，准备开始`
    case 'segment':
      return event.title || event.segment_id || 'Segment'
    case 'done':
      return '全部处理完成'
    case 'failed':
      return '提炼失败'
    case 'canceled':
      return '操作已取消'
    default:
      return event.phase
  }
}

function providerLabel(provider: string): string {
  switch (provider) {
    case 'claude-code':
      return 'Claude Code'
    case 'codex':
      return 'Codex'
    default:
      return '全部来源'
  }
}

function sinceLabel(since: string): string {
  switch (since) {
    case '1d':
      return '最近 1 天'
    case '7d':
      return '最近 7 天'
    case '30d':
      return '最近 30 天'
    default:
      return '全部历史'
  }
}

function formatDuration(ms: number): string {
  if (!ms) return '—'
  if (ms < 1000) return `${ms} ms`
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds} 秒`
  return `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
}
