import { useQuery } from '@tanstack/react-query'
import { CircleHelp } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import { api } from '../api'
import { QuestionRow } from '../components/QuestionRow'
import {
  EmptyState,
  ErrorBox,
  Loading,
  PageHeader,
  StatCard,
} from '../components/ui'

export function Questions() {
  const [params, setParams] = useSearchParams()
  const status = params.get('status') ?? 'open'
  const questions = useQuery({
    queryKey: ['questions', status],
    queryFn: () => api.questions(status),
  })
  // The API normalizes empty results to [], but guard here as well: a null
  // entries array used to crash the whole React tree.
  const entries = questions.data?.entries ?? []

  const update = (value: string) => {
    const next = new URLSearchParams(params)
    if (value === 'open') next.delete('status')
    else next.set('status', value)
    setParams(next, { replace: true })
  }

  return (
    <div className="page">
      <PageHeader
        eyebrow="OPEN QUESTIONS"
        title="待办问题"
        subtitle="文档里还没被回答的问题。标记解决不会修改 Doc，重新提炼后状态仍然保留。"
      />

      <section className="stat-grid stat-grid-2">
        <StatCard
          label="未解决"
          value={questions.data?.open ?? '—'}
          detail="需要继续调查或验证"
          tone="amber"
          icon={<CircleHelp size={18} />}
          to="/questions"
        />
        <StatCard
          label="已解决"
          value={questions.data?.resolved ?? '—'}
          detail="状态保存在 cache/questions.json"
          tone="green"
          to="/questions?status=resolved"
        />
      </section>

      <div className="filter-bar">
        <div className="segmented" role="group" aria-label="问题状态">
          {[
            ['open', '未解决'],
            ['resolved', '已解决'],
            ['all', '全部'],
          ].map(([value, label]) => (
            <button
              key={value}
              className={status === value ? 'active' : ''}
              onClick={() => update(value)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {questions.isLoading && <Loading label="读取问题…" />}
      {questions.error && <ErrorBox error={questions.error} />}
      {questions.data && (
        <>
          {questions.data.warnings && questions.data.warnings.length > 0 && (
            <div className="muted small-text">{questions.data.warnings.join('；')}</div>
          )}
          {entries.length === 0 ? (
            <EmptyState
              icon={<CircleHelp size={28} />}
              title={status === 'resolved' ? '还没有已解决的问题' : '没有待办问题'}
              description={
                status === 'resolved'
                  ? '在文档详情或本页标记解决后，会出现在这里。'
                  : '所有记录下来的问题都已经处理，或者还没有文档提出开放问题。'
              }
              action={
                status !== 'resolved' ? (
                  <Link className="button ghost" to="/extract">
                    去提炼新案例
                  </Link>
                ) : undefined
              }
            />
          ) : (
            <div className="question-list">
              {entries.map((entry) => (
                <QuestionRow key={entry.id} entry={entry} showDoc />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
