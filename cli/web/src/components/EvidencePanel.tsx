import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { FileSearch, X } from 'lucide-react'
import { api, formatDateTime } from '../api'
import type { EventView } from '../types'
import { ErrorBox, Loading } from './ui'

const kindLabels: Record<string, string> = {
  user_text: '用户',
  assistant_text: '助手',
  assistant_thinking: '思考',
  tool_use: '工具调用',
  tool_result: '工具结果',
  meta: '元信息',
  unknown: '未知事件',
}

function execText(event: EventView): string | null {
  const exec = event.exec
  if (!exec) return null
  const lines: string[] = []
  if (exec.exit_code !== undefined && exec.exit_code !== null) {
    lines.push(`exit code: ${exec.exit_code}`)
  } else if (exec.known) {
    lines.push('exit code: 未知')
  }
  if (exec.timed_out) lines.push('超时')
  if (exec.canceled) lines.push('已取消')
  if (exec.stderr) lines.push(`stderr:\n${exec.stderr}`)
  if (exec.stdout) lines.push(`stdout:\n${exec.stdout}`)
  return lines.join('\n')
}

export function EvidencePanel({
  docId,
  seqs,
  onClose,
}: {
  docId: string
  seqs: number[]
  onClose: () => void
}) {
  const ordered = [...new Set(seqs)].sort((a, b) => a - b)
  const events = useQuery({
    queryKey: ['events', docId, ordered],
    queryFn: () => api.events(docId, ordered),
    enabled: ordered.length > 0,
  })

  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', listener)
    return () => window.removeEventListener('keydown', listener)
  }, [onClose])

  return (
    <div className="drawer-layer" role="dialog" aria-modal="true">
      <button className="drawer-backdrop" onClick={onClose} aria-label="关闭证据面板" />
      <aside className="drawer">
        <header className="drawer-head">
          <div>
            <div className="eyebrow">证据来源</div>
            <h2>
              <FileSearch size={18} /> 原始会话事件
            </h2>
          </div>
          <button className="icon-button" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </header>

        {events.isLoading && <Loading label="读取原始事件…" />}
        {events.error && <ErrorBox error={events.error} />}
        {events.data && (
          <div className="drawer-body">
            <div className="drawer-meta">
              <span>{events.data.project}</span>
              <span>·</span>
              <span>{events.data.source}</span>
              <span>·</span>
              <span>共 {events.data.event_count} 个事件</span>
            </div>
            {events.data.events.length === 0 && (
              <div className="empty-inline">没有找到对应事件。证据可能来自已移除的 Segment。</div>
            )}
            {events.data.events.map((event) => (
              <article className="event-card" key={event.seq}>
                <header>
                  <span className="seq">#{event.seq}</span>
                  <span className={`event-kind kind-${event.kind}`}>
                    {kindLabels[event.kind] ?? event.kind}
                  </span>
                  {event.role && <span className="muted">{event.role}</span>}
                  {event.time && <span className="event-time">{formatDateTime(event.time)}</span>}
                  {event.is_error && <span className="badge badge-red">错误</span>}
                  {event.truncated && <span className="badge badge-amber">已截断</span>}
                </header>

                {event.tool_name && (
                  <div className="tool-name">
                    工具 <code>{event.tool_name}</code>
                  </div>
                )}

                {event.text && <pre className="event-text">{event.text}</pre>}

                {(event.tool_input_text || event.tool_input !== undefined) && (
                  <details className="event-details" open>
                    <summary>工具输入</summary>
                    <pre className="event-text">
                      {event.tool_input_text ||
                        JSON.stringify(event.tool_input, null, 2)}
                    </pre>
                  </details>
                )}

                {event.tool_result && (
                  <details className="event-details">
                    <summary>工具输出</summary>
                    <pre className="event-text">{event.tool_result}</pre>
                  </details>
                )}

                {execText(event) && (
                  <div className={`exec-box${event.is_error ? ' failed' : ''}`}>
                    <div className="exec-title">执行结果</div>
                    <pre>{execText(event)}</pre>
                  </div>
                )}
              </article>
            ))}
          </div>
        )}
      </aside>
    </div>
  )
}
