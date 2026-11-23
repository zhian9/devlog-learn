import { useEffect, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { BriefcaseBusiness, FolderGit2 } from 'lucide-react'
import { Link, useSearchParams } from 'react-router-dom'
import { api, formatDate } from '../api'
import { Badge, Banner, EmptyState, ErrorBox, Loading, PageHeader } from '../components/ui'
import type {
  Experience,
  ExperienceHighlight,
  ExperienceReviewItem,
  ExperienceSynthesis,
} from '../types'

function kindTone(kind: string): 'blue' | 'violet' | 'green' | 'amber' | 'neutral' {
  switch (kind) {
    case 'internship':
      return 'violet'
    case 'work':
      return 'green'
    case 'course':
      return 'amber'
    case 'project':
      return 'blue'
    default:
      return 'neutral'
  }
}

function sourceLabel(source: string): string {
  switch (source) {
    case 'manual':
      return '人工确认'
    case 'suggestion':
      return '自动建议'
    default:
      return '默认类型'
  }
}

function DocLink({ id }: { id?: string }) {
  if (!id) return null
  return (
    <Link className="experience-doc-link" to={`/docs/${encodeURIComponent(id)}`}>
      {id}
    </Link>
  )
}

function HighlightList({ items }: { items: ExperienceHighlight[] }) {
  if (items.length === 0) return null
  return (
    <ul className="experience-bullets">
      {items.map((item, index) => (
        <li key={`${item.text}-${index}`}>
          <span>{item.text}</span>
          {item.doc_ids.length > 0 && (
            <span className="experience-refs">
              {item.doc_ids.map((id) => (
                <DocLink id={id} key={id} />
              ))}
            </span>
          )}
        </li>
      ))}
    </ul>
  )
}

function Section({
  title,
  children,
}: {
  title: string
  children: ReactNode
}) {
  return (
    <section className="experience-section">
      <h3>{title}</h3>
      {children}
    </section>
  )
}

function Checklist({ items }: { items: ExperienceReviewItem[] }) {
  if (items.length === 0) return null
  return (
    <ul className="experience-checklist">
      {items.map((item) => (
        <li key={item.id}>
          <input type="checkbox" disabled aria-label={item.text} />
          <span>
            {item.text}
            {item.doc_id && (
              <>
                {' '}
                <DocLink id={item.doc_id} />
              </>
            )}
          </span>
        </li>
      ))}
    </ul>
  )
}

function SynthesisPanel({ synthesis }: { synthesis: ExperienceSynthesis }) {
  return (
    <Banner tone="amber">
      <strong>模型补充（未验证）</strong>
      <p>
        以下内容由 {synthesis.provider}
        {synthesis.model ? ` · ${synthesis.model}` : ''} 根据文档摘要生成，未逐条核验；上面的确定性内容优先。
      </p>
      {synthesis.summary && <p>{synthesis.summary}</p>}
      {synthesis.background && <p>{synthesis.background}</p>}
      {synthesis.contributions.length > 0 && (
        <>
          <strong>关键贡献</strong>
          <HighlightList items={synthesis.contributions} />
        </>
      )}
      {synthesis.challenges.length > 0 && (
        <>
          <strong>难点</strong>
          <HighlightList items={synthesis.challenges} />
        </>
      )}
      {synthesis.outcomes.length > 0 && (
        <>
          <strong>结果</strong>
          <HighlightList items={synthesis.outcomes} />
        </>
      )}
      {synthesis.stories.length > 0 && (
        <>
          <strong>故事</strong>
          <HighlightList items={synthesis.stories} />
        </>
      )}
    </Banner>
  )
}

function ExperienceDetail({ experience }: { experience: Experience }) {
  return (
    <div className="panel experience-detail">
      <div className="panel-head">
        <div>
          <h2>{experience.project}</h2>
          <p>
            {experience.docs} 篇文档 · {experience.sessions} 个会话
            {experience.period ? ` · ${experience.period}` : ''}
          </p>
        </div>
        <div className="badge-row">
          <Badge tone={kindTone(experience.kind)}>{experience.kind_label}</Badge>
          <Badge tone="outline">{sourceLabel(experience.kind_source)}</Badge>
          {experience.synthesized && <Badge tone="amber">含模型补充</Badge>}
        </div>
      </div>

      {experience.headline && <blockquote className="experience-headline">{experience.headline}</blockquote>}
      {experience.overview && <p className="experience-overview">{experience.overview}</p>}

      {(experience.organization || experience.role || experience.paths.length > 0) && (
        <div className="meta-list">
          {experience.organization && (
            <div>
              <span>组织</span>
              <strong>{experience.organization}</strong>
            </div>
          )}
          {experience.role && (
            <div>
              <span>角色</span>
              <strong>{experience.role}</strong>
            </div>
          )}
          {experience.paths.length > 0 && (
            <div>
              <span>目录</span>
              <strong>{experience.paths.join('；')}</strong>
            </div>
          )}
          {experience.stack.length > 0 && (
            <div>
              <span>技术栈</span>
              <strong>{experience.stack.join(' · ')}</strong>
            </div>
          )}
        </div>
      )}

      {(experience.warnings?.length ?? 0) > 0 && (
        <Banner tone="amber">
          {experience.warnings!.map((warning) => (
            <div key={warning}>{warning}</div>
          ))}
        </Banner>
      )}

      {experience.synthesized && <SynthesisPanel synthesis={experience.synthesized} />}

      {experience.files.length > 0 && (
        <Section title="涉及文件">
          <ul className="experience-bullets">
            {experience.files.map((file) => (
              <li key={file}>
                <code>{file}</code>
              </li>
            ))}
          </ul>
        </Section>
      )}
      {experience.highlights.length > 0 && (
        <Section title="关键工作">
          <HighlightList items={experience.highlights} />
        </Section>
      )}
      {experience.challenges.length > 0 && (
        <Section title="难点与解决">
          <HighlightList items={experience.challenges} />
        </Section>
      )}
      {experience.outcomes.length > 0 && (
        <Section title="结果与数据">
          <ul className="experience-bullets">
            {experience.outcomes.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </Section>
      )}
      {experience.stories.length > 0 && (
        <Section title="可讲的故事（STAR）">
          <HighlightList items={experience.stories} />
        </Section>
      )}
      {experience.lessons.length > 0 && (
        <Section title="复盘与收获">
          <ul className="experience-bullets">
            {experience.lessons.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </Section>
      )}
      {experience.blind_spots.length > 0 && (
        <Section title="盲区">
          <ul className="experience-bullets">
            {experience.blind_spots.map((item) => (
              <li key={item.id}>
                {item.text}
                {item.doc_id && (
                  <>
                    {' '}
                    <DocLink id={item.doc_id} />
                  </>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
      {experience.review_checklist.length > 0 && (
        <Section title="复习清单">
          <p className="experience-hint">静态清单，不记录完成状态。先自己回答，再回到对应文档核对。</p>
          <Checklist items={experience.review_checklist} />
        </Section>
      )}
      {experience.doc_refs.length > 0 && (
        <Section title="文档索引">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>日期</th>
                  <th>标题</th>
                  <th>类型</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {experience.doc_refs.map((doc) => (
                  <tr key={doc.id}>
                    <td>{formatDate(doc.occurred_at)}</td>
                    <td>
                      <Link className="experience-doc-link" to={`/docs/${encodeURIComponent(doc.id)}`}>
                        {doc.title || doc.id}
                      </Link>
                    </td>
                    <td>{doc.knowledge_type || '—'}</td>
                    <td>{doc.status || '—'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}
    </div>
  )
}

export function Experiences() {
  const [params, setParams] = useSearchParams()
  const selected = params.get('project') ?? ''
  const list = useQuery({ queryKey: ['experiences'], queryFn: api.experiences })
  const detail = useQuery({
    queryKey: ['experience', selected],
    queryFn: () => api.experience(selected),
    enabled: Boolean(selected),
  })

  useEffect(() => {
    if (selected || !list.data || list.data.experiences.length === 0) return
    const next = new URLSearchParams(params)
    next.set('project', list.data.experiences[0].project)
    setParams(next, { replace: true })
  }, [selected, list.data, params, setParams])

  if (list.isLoading) return <Loading label="加载项目经历…" />
  if (list.error) return <ErrorBox error={list.error} />
  if (!list.data) return null

  return (
    <div className="page">
      <PageHeader
        eyebrow="项目聚合"
        title="项目经历与复习清单"
        subtitle="把同一项目目录下已提炼的会话整理成项目经历或实习经验。类型可以人工确认；复习清单是静态的。"
      />

      {list.data.experiences.length === 0 ? (
        <EmptyState
          icon={<FolderGit2 size={26} />}
          title="还没有可聚合的经历"
          description="先运行提取或 run，让项目下的会话变成文档，再回到这里。"
          action={
            <Link className="button primary" to="/extract">
              去提炼会话
            </Link>
          }
        />
      ) : (
        <div className="experience-layout">
          <aside className="panel experience-list">
            <div className="panel-head">
              <div>
                <h2>项目</h2>
                <p>{list.data.count} 个经历</p>
              </div>
            </div>
            {list.data.experiences.map((item) => (
              <Link
                key={item.project}
                className={`experience-list-item${item.project === selected ? ' active' : ''}`}
                to={`/experiences?project=${encodeURIComponent(item.project)}`}
              >
                <strong>{item.project}</strong>
                <span>
                  {item.kind_label} · {item.docs} 篇 · {item.sessions} 个会话
                </span>
                <span>{item.period || '时间未记录'}</span>
                {item.suggested_kind && item.kind_source !== 'manual' && (
                  <span>建议类型：{item.suggested_kind === 'internship' ? '实习经验' : '项目经历'}</span>
                )}
              </Link>
            ))}
          </aside>

          <main className="experience-main">
            {!selected ? (
              <EmptyState icon={<BriefcaseBusiness size={26} />} title="选择一个项目" description="左侧列表列出了可聚合的经历。" />
            ) : detail.isLoading ? (
              <Loading label="加载经历详情…" />
            ) : detail.error ? (
              <ErrorBox error={detail.error} />
            ) : detail.data ? (
              <ExperienceDetail experience={detail.data} />
            ) : null}
          </main>
        </div>
      )}
    </div>
  )
}
