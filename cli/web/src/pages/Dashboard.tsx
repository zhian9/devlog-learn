import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowRight,
  BookOpen,
  CircleHelp,
  Database,
  FileText,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  TriangleAlert,
  Zap,
} from 'lucide-react'
import { Link } from 'react-router-dom'
import { api, formatBytes, formatDateTime } from '../api'
import {
  Badge,
  Banner,
  EmptyState,
  ErrorBox,
  Loading,
  PageHeader,
  StatCard,
  evidenceLabel,
  kindLabel,
  reviewLabel,
  reviewTone,
} from '../components/ui'

export function Dashboard() {
  const queryClient = useQueryClient()
  const [notice, setNotice] = useState('')
  const [exportWarnings, setExportWarnings] = useState<string[]>([])
  const status = useQuery({ queryKey: ['status'], queryFn: api.status })
  const exportAll = useMutation({
    mutationFn: () =>
      api.export({
        profile: 'both',
        include_stale: false,
        digest: true,
        digest_review: 'any',
      }),
    onSuccess: (result) => {
      setExportWarnings(result.warnings ?? [])
      const digest = result.digest_path
        ? `；AGENTS.md 包含 ${result.digest_docs} 篇文档`
        : ''
      setNotice(
        `已生成 ${result.rendered} 个 Markdown，索引包含 ${result.index_docs} 篇文档、${result.index_projects} 个项目${digest}。`,
      )
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
  })

  if (status.isLoading) return <Loading label="读取项目记忆…" />
  if (status.error) return <ErrorBox error={status.error} />
  if (!status.data) return null

  const data = status.data
  const cache = data.cache

  return (
    <div className="page">
      <PageHeader
        eyebrow="PROJECT MEMORY"
        title="项目记忆概览"
        subtitle="从 Claude Code 与 Codex 会话里沉淀下来的问题、定位路径和可迁移经验。"
        actions={
          <>
            <button
              className="button ghost"
              onClick={() => {
                setNotice('')
                setExportWarnings([])
                exportAll.mutate()
              }}
              disabled={exportAll.isPending}
            >
              <RefreshCw size={15} className={exportAll.isPending ? 'spin' : ''} />
              同步 Markdown、索引与 AGENTS.md
            </button>
            <Link className="button ghost" to="/library">
              文档库
            </Link>
            <Link className="button primary" to="/extract">
              <Zap size={15} /> 提炼新案例
            </Link>
          </>
        }
      />

      {notice && (
        <Banner tone="green" onDismiss={() => setNotice('')}>
          {notice}
        </Banner>
      )}
      {exportAll.error && <ErrorBox error={exportAll.error} />}
      {exportWarnings.length > 0 && (
        <Banner tone="amber">
          {exportWarnings.slice(0, 3).join('；')}
          {exportWarnings.length > 3 ? '…' : ''}
        </Banner>
      )}
      {data.warnings && data.warnings.length > 0 && (
        <Banner tone="amber">
          {data.warnings.length} 条提示：{data.warnings.slice(0, 3).join('；')}
          {data.warnings.length > 3 ? '…' : ''}
        </Banner>
      )}

      <section className="stat-grid">
        <StatCard
          label="有效文档"
          value={cache.active_docs}
          detail={`${cache.stale_docs} 篇已过期，不进入索引`}
          icon={<FileText size={18} />}
          tone="blue"
        />
        <StatCard
          label="待审核"
          value={cache.pending}
          detail={`已通过 ${cache.approved} · 已驳回 ${cache.rejected}`}
          icon={<ShieldCheck size={18} />}
          tone={cache.pending > 0 ? 'amber' : 'green'}
        />
        <StatCard
          label="未解决问题"
          value={data.questions.open}
          detail={`已解决 ${data.questions.resolved}`}
          icon={<CircleHelp size={18} />}
          tone={data.questions.open > 0 ? 'violet' : 'green'}
        />
        <StatCard
          label="本地缓存"
          value={formatBytes(cache.size_bytes)}
          detail={cache.dir}
          icon={<Database size={18} />}
        />
      </section>

      <div className="dashboard-grid">
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>最近沉淀</h2>
              <p>按发生时间排序，点击进入学习视图。</p>
            </div>
            <Link className="text-link" to="/library">
              全部 <ArrowRight size={14} />
            </Link>
          </div>
          {data.recent.length === 0 ? (
            <EmptyState
              icon={<BookOpen size={26} />}
              title="还没有文档"
              description="先运行 devlog-learn run 或 extract，再回到这里查看。"
            />
          ) : (
            <div className="recent-list">
              {data.recent.map((doc) => (
                <Link className="recent-item" to={`/docs/${doc.id}`} key={doc.id}>
                  <div className="recent-main">
                    <div className="recent-title">
                      {doc.title}
                      <Badge tone={reviewTone(doc.review_status)}>
                        {reviewLabel(doc.review_status)}
                      </Badge>
                      {doc.lifecycle === 'stale' && <Badge tone="outline">已过期</Badge>}
                    </div>
                    <p>{doc.summary || '暂无摘要'}</p>
                    <div className="meta-row">
                      <span>{doc.project}</span>
                      <span>·</span>
                      <span>{kindLabel(doc.knowledge_type)}</span>
                      <span>·</span>
                      <span>{formatDateTime(doc.occurred_at)}</span>
                      <span>·</span>
                      <span>
                        {evidenceLabel(doc.evidence_checked, doc.evidence_verified)}
                      </span>
                    </div>
                  </div>
                  <ArrowRight size={16} className="recent-arrow" />
                </Link>
              ))}
            </div>
          )}
        </section>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>项目分布</h2>
              <p>{data.projects.length} 个项目有有效文档。</p>
            </div>
          </div>
          {data.projects.length === 0 ? (
            <EmptyState icon={<Sparkles size={26} />} title="暂无项目" />
          ) : (
            <div className="project-list">
              {data.projects.map((project) => (
                <Link
                  className="project-item"
                  to={`/library?project=${encodeURIComponent(project.name)}`}
                  key={project.name}
                >
                  <div className="project-avatar">{project.name.slice(0, 1).toUpperCase()}</div>
                  <div className="project-main">
                    <strong>{project.name}</strong>
                    <span>
                      {project.docs} 篇 · 待审 {project.pending} · 已通过 {project.approved}
                    </span>
                  </div>
                  <ArrowRight size={15} />
                </Link>
              ))}
            </div>
          )}
        </section>
      </div>

      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>本地状态</h2>
            <p>只读检查；不解析会话日志，也不调用模型。</p>
          </div>
        </div>
        <div className="health-grid">
          {data.providers.map((provider) => (
            <div className="health-item" key={provider.name}>
              <div className="health-title">
                <span className={`dot ${provider.bin_found ? 'ok' : 'bad'}`} />
                {provider.name}
              </div>
              <div className="health-line">
                命令 <code>{provider.bin}</code> {provider.bin_found ? '已找到' : '未找到'}
              </div>
              <div className="health-line">
                会话目录 {provider.dir_exists ? `${provider.session_files} 个文件` : '不存在'}
              </div>
            </div>
          ))}
          <div className="health-item">
            <div className="health-title">
              <span className={`dot ${data.cursor.errors === 0 ? 'ok' : 'bad'}`} />
              增量游标
            </div>
            <div className="health-line">
              {data.cursor.exists ? `${data.cursor.files} 个文件` : '尚未生成'}
            </div>
            <div className="health-line">
              {data.cursor.errors > 0 ? `${data.cursor.errors} 个文件上次失败` : '没有失败记录'}
            </div>
          </div>
          <div className="health-item">
            <div className="health-title">
              <span className={`dot ${data.outputs.index_stale ? 'warn' : 'ok'}`} />
              输出产物
            </div>
            <div className="health-line">
              索引 {data.outputs.index_exists ? '已生成' : '未生成'}
              {data.outputs.index_stale && (
                <Badge tone="amber">
                  <TriangleAlert size={12} /> 已过期
                </Badge>
              )}
            </div>
            <div className="health-line">
              AGENTS.md {data.outputs.digest_exists ? '已生成' : '未生成'}
            </div>
          </div>
        </div>
        {data.knowledge_types && Object.keys(data.knowledge_types).length > 0 && (
          <div className="knowledge-strip">
            {Object.entries(data.knowledge_types).map(([kind, count]) => (
              <span key={kind} className="knowledge-pill">
                {kindLabel(kind)} <strong>{count}</strong>
              </span>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}
