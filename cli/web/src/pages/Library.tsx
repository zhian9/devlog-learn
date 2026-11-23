import { useDeferredValue } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ArrowRight, BookOpen, Search, SlidersHorizontal, X } from 'lucide-react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
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
  const kind = params.get('kind') ?? ''
  const status = params.get('status') ?? ''
  const learning = params.get('learning') ?? ''
  const tags = params.getAll('tag')
  const deferredQuery = useDeferredValue(q)

  const statusQuery = useQuery({ queryKey: ['status'], queryFn: api.status })
  const docs = useQuery({
    queryKey: ['docs', deferredQuery, project, review, lifecycle, sort, kind, status, learning, tags.join('|')],
    queryFn: () =>
      api.docs({
        q: deferredQuery,
        project,
        review: review === 'any' ? undefined : review,
        lifecycle,
        sort,
        kind,
        status,
        learning,
        tag: tags,
      }),
  })
  const docList = docs.data?.docs ?? []

  const isDefault = (key: string, value: string) =>
    !value ||
    (key === 'review' && value === 'any') ||
    (key === 'lifecycle' && value === 'active') ||
    (key === 'sort' && value === 'recent')

  const update = (key: string, value: string) => {
    const next = new URLSearchParams(params)
    if (isDefault(key, value)) next.delete(key)
    else next.set(key, value)
    setParams(next, { replace: true })
  }

  // Build a library URL that preserves the other active filters, so clicking a
  // badge drills down instead of resetting the page.
  const filterUrl = (overrides: Record<string, string>) => {
    const next = new URLSearchParams(params)
    for (const [key, value] of Object.entries(overrides)) {
      if (isDefault(key, value)) next.delete(key)
      else next.set(key, value)
    }
    const text = next.toString()
    return text ? `/library?${text}` : '/library'
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
    q ||
      project ||
      review !== 'any' ||
      lifecycle !== 'active' ||
      sort !== 'recent' ||
      kind ||
      status ||
      learning ||
      tags.length > 0,
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
            {statusQuery.data?.projects.map((item) => (
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
            value={kind}
            onChange={(event) => update('kind', event.target.value)}
            aria-label="按知识类型筛选"
          >
            <option value="">全部知识类型</option>
            <option value="bug_fix">问题修复</option>
            <option value="learning">学习</option>
            <option value="architecture">架构</option>
            <option value="development">开发</option>
            <option value="refactoring">重构</option>
            <option value="configuration">配置</option>
            <option value="decision">决策</option>
            <option value="unknown">未分类</option>
          </select>
          <select
            value={status}
            onChange={(event) => update('status', event.target.value)}
            aria-label="按解决状态筛选"
          >
            <option value="">全部状态</option>
            <option value="resolved">已解决</option>
            <option value="workaround">有绕过方案</option>
            <option value="unresolved">未解决</option>
            <option value="unknown">状态未知</option>
          </select>
          <select
            value={learning}
            onChange={(event) => update('learning', event.target.value)}
            aria-label="按学习价值筛选"
          >
            <option value="">全部学习价值</option>
            <option value="high">学习价值高</option>
            <option value="medium">学习价值中</option>
            <option value="low">学习价值低</option>
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

      {(kind || status || learning || tags.length > 0) && (
        <div className="active-tags">
          <span className="muted">筛选：</span>
          {kind && (
            <button className="tag active" onClick={() => update('kind', '')}>
              类型：{kindLabel(kind)} <X size={11} />
            </button>
          )}
          {status && (
            <button className="tag active" onClick={() => update('status', '')}>
              状态：{statusLabel(status)} <X size={11} />
            </button>
          )}
          {learning && (
            <button className="tag active" onClick={() => update('learning', '')}>
              学习价值：{learningValueLabel(learning)} <X size={11} />
            </button>
          )}
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

          {docList.length === 0 ? (
            <EmptyState
              icon={<BookOpen size={28} />}
              title="没有匹配的文档"
              description={
                hasFilters
                  ? '试试放宽筛选条件，或清除搜索关键词。'
                  : '先从本地会话里提炼第一份学习案例。'
              }
              action={
                hasFilters ? (
                  <button className="button ghost" onClick={clear}>
                    清除筛选
                  </button>
                ) : (
                  <Link className="button primary" to="/extract">
                    去提炼
                  </Link>
                )
              }
            />
          ) : (
            <div className="doc-grid">
              {docList.map((doc) => (
                <article
                  className="doc-card"
                  key={doc.id}
                  onClick={() => navigate(`/docs/${doc.id}`)}
                >
                  <div className="doc-card-head">
                    <Link
                      className="doc-card-title"
                      to={`/docs/${doc.id}`}
                      onClick={(event) => event.stopPropagation()}
                    >
                      {doc.title}
                    </Link>
                    <ArrowRight size={17} className="doc-card-arrow" />
                  </div>
                  <p className="doc-card-summary">{doc.summary || '暂无摘要'}</p>
                  <div className="badge-row" onClick={(event) => event.stopPropagation()}>
                    <Badge
                      tone={reviewTone(doc.review_status)}
                      to={filterUrl({ review: doc.review_status })}
                      title="按审核状态筛选"
                    >
                      {reviewLabel(doc.review_status)}
                    </Badge>
                    <Badge
                      tone={statusTone(doc.status)}
                      to={filterUrl({ status: doc.status })}
                      title="按解决状态筛选"
                    >
                      {statusLabel(doc.status)}
                    </Badge>
                    <Badge
                      tone="outline"
                      to={filterUrl({ kind: doc.knowledge_type })}
                      title="按知识类型筛选"
                    >
                      {kindLabel(doc.knowledge_type)}
                    </Badge>
                    {doc.learning_value && (
                      <Badge
                        tone="violet"
                        to={filterUrl({ learning: doc.learning_value })}
                        title="按学习价值筛选"
                      >
                        {learningValueLabel(doc.learning_value)}
                      </Badge>
                    )}
                    {doc.lifecycle === 'stale' && (
                      <Badge tone="red" to={filterUrl({ lifecycle: 'stale' })}>
                        已过期
                      </Badge>
                    )}
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
                  <div className="doc-card-foot" onClick={(event) => event.stopPropagation()}>
                    <Link
                      className="inline-link"
                      to={filterUrl({ project: doc.project })}
                      title="按项目筛选"
                    >
                      {doc.project}
                    </Link>
                    <span>{formatDate(doc.occurred_at)}</span>
                    <span>{evidenceLabel(doc.evidence_checked, doc.evidence_verified)}</span>
                    <span>置信度 {Math.round(doc.confidence * 100)}%</span>
                  </div>
                </article>
              ))}
            </div>
          )}
        </>
      )}
    </div>
  )
}
