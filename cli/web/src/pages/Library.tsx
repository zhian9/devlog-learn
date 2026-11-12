import { useDeferredValue } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, BookOpen, Search, SlidersHorizontal, X } from 'lucide-react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { api, formatDate } from '../api'
import {
  Badge,
  EmptyState,
  ErrorBox,
  Loading,
  PageHeader,
  evidenceLabel,
  kindLabel,
  learningValueLabel,
  reviewLabel,
  reviewTone,
  statusLabel,
  statusTone,
} from '../components/ui'

export function Library() {
  const [params, setParams] = useSearchParams()
  const navigate = useNavigate()
  const q = params.get('q') ?? ''
  const project = params.get('project') ?? ''
  const review = params.get('review') ?? 'any'
  const lifecycle = params.get('lifecycle') ?? 'active'
  const sort = params.get('sort') ?? 'recent'
  const tags = params.getAll('tag')
  const deferredQuery = useDeferredValue(q)

  const status = useQuery({ queryKey: ['status'], queryFn: api.status })
  const docs = useQuery({
    queryKey: ['docs', deferredQuery, project, review, lifecycle, sort, tags.join('|')],
    queryFn: () =>
      api.docs({
        q: deferredQuery,
        project,
        review: review === 'any' ? undefined : review,
        lifecycle,
        sort,
        tag: tags,
      }),
  })

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (!value || (key === 'review' && value === 'any') || (key === 'lifecycle' && value === 'active')) {
      next.delete(key)
    } else {
      next.set(key, value)
    }
    setParams(next, { replace: true })
  }

  const addTag = (tag: string) => {
    const next = new URLSearchParams(params)
    if (!next.getAll('tag').includes(tag)) next.append('tag', tag)
    setParams(next, { replace: true })
  }
  const removeTag = (tag: string) => {
    const next = new URLSearchParams(params)
    const remaining = next.getAll('tag').filter((item) => item !== tag)
    next.delete('tag')
    for (const item of remaining) next.append('tag', item)
    setParams(next, { replace: true })
  }
  const clear = () => setParams(new URLSearchParams(), { replace: true })
  const hasFilters = Boolean(
    q || project || review !== 'any' || lifecycle !== 'active' || sort !== 'recent' || tags.length > 0,
  )

  return (
    <div className="page">
      <PageHeader
        eyebrow="LIBRARY"
        title="文档库"
        subtitle="按关键词、项目、审核状态和学习价值浏览沉淀下来的案例。"
        actions={
          hasFilters ? (
            <button className="button ghost" onClick={clear}>
              <X size={15} /> 清除筛选
            </button>
          ) : undefined
        }
      />

      <div className="search-bar">
        <Search size={18} className="search-icon" />
        <input
          autoFocus
          value={q}
          onChange={(event) => update('q', event.target.value)}
          placeholder="搜索问题、根因、命令、概念…（支持中文）"
          aria-label="搜索文档"
        />
        {q && (
          <button className="icon-button" onClick={() => update('q', '')} aria-label="清空搜索">
            <X size={16} />
          </button>
        )}
      </div>

      <div className="filter-bar">
        <div className="filter-group">
          <SlidersHorizontal size={15} />
          <select
            value={project}
            onChange={(event) => update('project', event.target.value)}
            aria-label="按项目筛选"
          >
            <option value="">全部项目</option>
            {status.data?.projects.map((item) => (
              <option key={item.name} value={item.name}>
                {item.name} ({item.docs})
              </option>
            ))}
          </select>
          <select
            value={review}
            onChange={(event) => update('review', event.target.value)}
            aria-label="按审核状态筛选"
          >
            <option value="any">全部审核状态</option>
            <option value="pending">待审核</option>
            <option value="approved">已通过</option>
            <option value="rejected">已驳回</option>
          </select>
          <select
            value={sort}
            onChange={(event) => update('sort', event.target.value)}
            aria-label="排序"
          >
            <option value="recent">最近发生</option>
            <option value="confidence">置信度最高</option>
            <option value="title">标题</option>
          </select>
        </div>
        <div className="segmented" role="group" aria-label="生命周期">
          {[
            ['active', '有效'],
            ['all', '全部'],
            ['stale', '已过期'],
          ].map(([value, label]) => (
            <button
              key={value}
              className={lifecycle === value ? 'active' : ''}
              onClick={() => update('lifecycle', value)}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {tags.length > 0 && (
        <div className="active-tags">
          <span className="muted">标签：</span>
          {tags.map((tag) => (
            <button key={tag} className="tag active" onClick={() => removeTag(tag)}>
              #{tag} <X size={11} />
            </button>
          ))}
        </div>
      )}

      {docs.isLoading && <Loading label="检索文档…" />}
      {docs.error && <ErrorBox error={docs.error} />}
      {docs.data && (
        <>
          <div className="result-line">
            <span>
              找到 <strong>{docs.data.total}</strong> 篇文档
              {deferredQuery ? ` · 关键词「${deferredQuery}」` : ''}
            </span>
            {docs.data.warnings && docs.data.warnings.length > 0 && (
              <span className="muted">{docs.data.warnings.length} 条读取提示</span>
            )}
          </div>

          {docs.data.docs.length === 0 ? (
            <EmptyState
              icon={<BookOpen size={28} />}
              title="没有匹配的文档"
              description={
                hasFilters
                  ? '试试放宽筛选条件，或清除搜索关键词。'
                  : '先运行 devlog-learn run 生成第一份学习案例。'
              }
              action={
                hasFilters ? (
                  <button className="button ghost" onClick={clear}>
                    清除筛选
                  </button>
                ) : undefined
              }
            />
          ) : (
            <div className="doc-grid">
              {docs.data.docs.map((doc) => (
                <div
                  className="doc-card"
                  role="link"
                  tabIndex={0}
                  key={doc.id}
                  onClick={() => navigate(`/docs/${doc.id}`)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter' && event.target === event.currentTarget) {
                      navigate(`/docs/${doc.id}`)
                    }
                  }}
                >
                  <div className="doc-card-head">
                    <div className="doc-card-title">{doc.title}</div>
                    <ArrowRight size={17} className="doc-card-arrow" />
                  </div>
                  <p className="doc-card-summary">{doc.summary || '暂无摘要'}</p>
                  <div className="badge-row">
                    <Badge tone={reviewTone(doc.review_status)}>
                      {reviewLabel(doc.review_status)}
                    </Badge>
                    <Badge tone={statusTone(doc.status)}>{statusLabel(doc.status)}</Badge>
                    <Badge tone="outline">{kindLabel(doc.knowledge_type)}</Badge>
                    {doc.learning_value && (
                      <Badge tone="violet">{learningValueLabel(doc.learning_value)}</Badge>
                    )}
                    {doc.lifecycle === 'stale' && <Badge tone="red">已过期</Badge>}
                  </div>
                  {doc.tags && doc.tags.length > 0 && (
                    <div className="tag-row">
                      {doc.tags.slice(0, 6).map((tag) => (
                        <button
                          className={`tag${tags.includes(tag) ? ' active' : ''}`}
                          key={tag}
                          onClick={(event) => {
                            event.preventDefault()
                            event.stopPropagation()
                            addTag(tag)
                          }}
                          title={`按标签 #${tag} 筛选`}
                        >
                          #{tag}
                        </button>
                      ))}
                    </div>
                  )}
                  <div className="doc-card-foot">
                    <span>{doc.project}</span>
                    <span>{formatDate(doc.occurred_at)}</span>
                    <span>{evidenceLabel(doc.evidence_checked, doc.evidence_verified)}</span>
                    <span>置信度 {Math.round(doc.confidence * 100)}%</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
