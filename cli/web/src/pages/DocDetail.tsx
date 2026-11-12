import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft,
  BookOpen,
  Braces,
  Check,
  CircleHelp,
  Copy,
  FileText,
  Link2,
  RotateCcw,
  ShieldCheck,
  Sparkles,
  X,
} from 'lucide-react'
import { Link, useParams } from 'react-router-dom'
import { api, formatDate, formatDateTime } from '../api'
import { EvidencePanel } from '../components/EvidencePanel'
import { LearningView } from '../components/LearningView'
import { MarkdownView } from '../components/MarkdownView'
import { QuestionRow } from '../components/QuestionRow'
import {
  Badge,
  Banner,
  CopyButton,
  ErrorBox,
  Loading,
  difficultyLabel,
  evidenceLabel,
  kindLabel,
  learningValueLabel,
  reviewLabel,
  reviewTone,
  statusLabel,
  statusTone,
} from '../components/ui'

type Tab = 'learn' | 'markdown' | 'json'

export function DocDetail() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()
  const [tab, setTab] = useState<Tab>('learn')
  const [markdownProfile, setMarkdownProfile] = useState<'agent' | 'learning'>('agent')
  const [panelSeqs, setPanelSeqs] = useState<number[] | null>(null)
  const [noteDraft, setNoteDraft] = useState('')
  const [notice, setNotice] = useState('')

  const detail = useQuery({
    queryKey: ['doc', id],
    queryFn: () => api.doc(id as string),
    enabled: Boolean(id),
  })
  const markdown = useQuery({
    queryKey: ['markdown', id, markdownProfile],
    queryFn: () => api.markdown(id as string, markdownProfile),
    enabled: Boolean(id) && tab === 'markdown',
  })

  const review = useMutation({
    mutationFn: ({ status, note }: { status: string; note?: string }) =>
      api.review(id as string, status, note),
    onSuccess: () => {
      setNotice('审核状态已更新')
      queryClient.invalidateQueries({ queryKey: ['doc', id] })
      queryClient.invalidateQueries({ queryKey: ['status'] })
      queryClient.invalidateQueries({ queryKey: ['docs'] })
      queryClient.invalidateQueries({ queryKey: ['questions'] })
    },
  })

  useEffect(() => {
    if (detail.data) setNoteDraft(detail.data.doc.review_note ?? '')
  }, [detail.data?.doc.id, detail.data?.doc.review_note])

  if (detail.isLoading) return <Loading label="读取文档…" />
  if (detail.error) return <ErrorBox error={detail.error} />
  if (!detail.data) return null

  const { doc } = detail.data
  const questions = detail.data.questions ?? []
  const related = detail.data.related ?? []

  const changeReview = (status: string) => {
    review.mutate({ status, note: noteDraft })
  }

  return (
    <div className="page detail-page">
      <div className="detail-top">
        <Link className="back-link" to="/library">
          <ArrowLeft size={15} /> 返回文档库
        </Link>
        <div className="detail-meta-line">
          <span>{doc.project}</span>
          <span>·</span>
          <span>{doc.source}</span>
          <span>·</span>
          <span>{formatDateTime(doc.occurred_at)}</span>
          <span>·</span>
          <span>持续 {doc.duration_minutes} 分钟</span>
        </div>
      </div>

      <header className="detail-header">
        <div className="detail-title-block">
          <div className="badge-row">
            <Badge tone={reviewTone(doc.review_status)}>{reviewLabel(doc.review_status)}</Badge>
            <Badge tone={statusTone(doc.status)}>{statusLabel(doc.status)}</Badge>
            <Badge tone="outline">{kindLabel(doc.knowledge_type)}</Badge>
            <Badge tone="outline">{difficultyLabel(doc.difficulty)}</Badge>
            {doc.learning_value && (
              <Badge tone="violet">{learningValueLabel(doc.learning_value)}</Badge>
            )}
            {doc.lifecycle === 'stale' && <Badge tone="red">已过期</Badge>}
          </div>
          <h1>{doc.title}</h1>
          {doc.summary && <p className="detail-summary">{doc.summary}</p>}
          <div className="detail-stats">
            <span>
              <ShieldCheck size={14} /> {evidenceLabel(doc.validation.evidence_checked, doc.validation.evidence_verified)}
            </span>
            <span>
              <Sparkles size={14} /> 置信度 {Math.round(doc.confidence * 100)}%
            </span>
            <span>
              <CircleHelp size={14} /> {doc.open_questions?.length ?? 0} 个未解决问题
            </span>
          </div>
        </div>
      </header>

      {notice && (
        <Banner tone="green" onDismiss={() => setNotice('')}>
          {notice}
        </Banner>
      )}
      {review.error && <ErrorBox error={review.error} />}
      {detail.data.warnings && detail.data.warnings.length > 0 && (
        <Banner tone="amber">
          {detail.data.warnings.slice(0, 3).join('；')}
          {detail.data.warnings.length > 3 ? '…' : ''}
        </Banner>
      )}

      <div className="detail-grid">
        <div className="detail-main">
          <div className="tabs" role="tablist">
            <button
              role="tab"
              aria-selected={tab === 'learn'}
              className={tab === 'learn' ? 'active' : ''}
              onClick={() => setTab('learn')}
            >
              <BookOpen size={15} /> 学习视图
            </button>
            <button
              role="tab"
              aria-selected={tab === 'markdown'}
              className={tab === 'markdown' ? 'active' : ''}
              onClick={() => setTab('markdown')}
            >
              <FileText size={15} /> Markdown
            </button>
            <button
              role="tab"
              aria-selected={tab === 'json'}
              className={tab === 'json' ? 'active' : ''}
              onClick={() => setTab('json')}
            >
              <Braces size={15} /> JSON
            </button>
          </div>

          {tab === 'learn' && (
            <LearningView doc={doc} onEvidence={(seqs) => setPanelSeqs(seqs)} />
          )}

          {tab === 'markdown' && (
            <div className="markdown-panel">
              <div className="markdown-toolbar">
                <div className="segmented">
                  <button
                    className={markdownProfile === 'agent' ? 'active' : ''}
                    onClick={() => setMarkdownProfile('agent')}
                  >
                    Agent 版
                  </button>
                  <button
                    className={markdownProfile === 'learning' ? 'active' : ''}
                    onClick={() => setMarkdownProfile('learning')}
                  >
                    学习版
                  </button>
                </div>
                {markdown.data && <CopyButton text={markdown.data} label="复制 Markdown" />}
              </div>
              {markdown.isLoading && <Loading label="生成 Markdown…" />}
              {markdown.error && <ErrorBox error={markdown.error} />}
              {markdown.data && <MarkdownView content={markdown.data} />}
            </div>
          )}

          {tab === 'json' && (
            <div className="json-panel">
              <div className="markdown-toolbar">
                <span className="muted">Doc JSON 是稳定契约，供 Agent/RAG 使用。</span>
                <CopyButton text={JSON.stringify(doc, null, 2)} label="复制 JSON" />
              </div>
              <pre className="json-view">{JSON.stringify(doc, null, 2)}</pre>
            </div>
          )}
        </div>

        <aside className="detail-rail">
          <section className="rail-card">
            <h3>
              <ShieldCheck size={16} /> 人工审核
            </h3>
            <div className="review-buttons">
              <button
                className={`button small${doc.review_status === 'approved' ? ' success' : ''}`}
                onClick={() => changeReview('approved')}
                disabled={review.isPending}
              >
                <Check size={14} /> 通过
              </button>
              <button
                className={`button small${doc.review_status === 'rejected' ? ' danger' : ''}`}
                onClick={() => changeReview('rejected')}
                disabled={review.isPending}
              >
                <X size={14} /> 驳回
              </button>
              <button
                className={`button small${doc.review_status === 'pending' ? ' selected' : ''}`}
                onClick={() => changeReview('pending')}
                disabled={review.isPending}
              >
                <RotateCcw size={14} /> 待审
              </button>
            </div>
            <label className="field-label" htmlFor="review-note">
              备注
            </label>
            <textarea
              id="review-note"
              className="review-note"
              value={noteDraft}
              onChange={(event) => setNoteDraft(event.target.value)}
              placeholder="记录你验证过的内容或保留意见…"
              rows={3}
            />
            <button
              className="button ghost small full"
              onClick={() => changeReview(doc.review_status || 'pending')}
              disabled={review.isPending}
            >
              保存备注
            </button>
          </section>

          <section className="rail-card">
            <h3>
              <Link2 size={16} /> 元信息
            </h3>
            <dl className="meta-list">
              <div>
                <dt>来源</dt>
                <dd>{doc.source}</dd>
              </div>
              <div>
                <dt>项目</dt>
                <dd>{doc.project}</dd>
              </div>
              <div>
                <dt>时间</dt>
                <dd>{formatDate(doc.occurred_at)}</dd>
              </div>
              <div>
                <dt>模型</dt>
                <dd>{doc.provenance.model || '—'}</dd>
              </div>
              <div>
                <dt>证据</dt>
                <dd>
                  {doc.validation.evidence_verified}/{doc.validation.evidence_checked} 已核验
                </dd>
              </div>
              <div>
                <dt>Segment</dt>
                <dd className="mono with-copy">
                  {doc.provenance.segment_id}
                  <CopyButton text={doc.provenance.segment_id} />
                </dd>
              </div>
              <div>
                <dt>会话</dt>
                <dd className="mono with-copy">
                  {doc.provenance.session_id}
                  <CopyButton text={doc.provenance.session_id} />
                </dd>
              </div>
            </dl>
          </section>

          <section className="rail-card">
            <h3>
              <CircleHelp size={16} /> 问题与闭环
            </h3>
            {questions.length === 0 ? (
              <div className="empty-inline">
                当前文档没有记录待解决问题。
                {doc.open_questions && doc.open_questions.length > 0 && (
                  <span>（文档里还有 {doc.open_questions.length} 条原始问题，重新提炼后会出现。）</span>
                )}
              </div>
            ) : (
              <div className="question-list compact">
                {questions.map((entry) => (
                  <QuestionRow key={entry.id} entry={entry} />
                ))}
              </div>
            )}
          </section>

          <section className="rail-card">
            <h3>
              <Link2 size={16} /> 相关文档
            </h3>
            {related.length === 0 ? (
              <div className="empty-inline">暂无相关文档。</div>
            ) : (
              <div className="related-list">
                {related.map((item) => (
                  <Link to={`/docs/${item.id}`} key={item.id} className="related-item">
                    <span>{item.title}</span>
                    <span className="related-reasons">
                      {item.reasons.slice(0, 3).map((reason) => (
                        <span className="tag" key={reason}>
                          {reason}
                        </span>
                      ))}
                    </span>
                  </Link>
                ))}
              </div>
            )}
          </section>

          {doc.review_note && (
            <section className="rail-card">
              <h3>
                <Copy size={16} /> 最近人工备注
              </h3>
              <p className="review-note-display">{doc.review_note}</p>
            </section>
          )}
        </aside>
      </div>

      {panelSeqs && (
        <EvidencePanel docId={doc.id} seqs={panelSeqs} onClose={() => setPanelSeqs(null)} />
      )}
    </div>
  )
}
