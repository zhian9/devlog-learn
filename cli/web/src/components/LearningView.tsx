import type { ReactNode } from 'react'
import {
  AlertTriangle,
  BookOpen,
  CheckCircle2,
  CircleHelp,
  FileText,
  Folder,
  GitBranch,
  Lightbulb,
  Search,
  Target,
  Terminal,
  Wrench,
  XCircle,
} from 'lucide-react'
import type { Doc } from '../types'
import { Badge, CopyButton, evidenceLabel, kindLabel } from './ui'

function seqs(value?: number[] | null): number[] {
  return value && value.length > 0 ? value : []
}

function commandKindLabel(kind?: string): string {
  const labels: Record<string, string> = {
    read: '读取',
    write: '写入',
    build: '构建',
    deploy: '部署',
    debug: '调试',
    unknown: '未知',
  }
  return kind ? labels[kind] ?? kind : ''
}

function verificationTone(status: string): 'green' | 'red' | 'amber' | 'neutral' {
  switch (status) {
    case 'passed':
      return 'green'
    case 'failed':
      return 'red'
    case 'not_run':
      return 'amber'
    default:
      return 'neutral'
  }
}

function verificationLabel(status: string): string {
  const labels: Record<string, string> = {
    passed: '通过',
    failed: '失败',
    not_run: '未运行',
    unknown: '未知',
  }
  return labels[status] ?? status
}

export function LearningView({
  doc,
  onEvidence,
}: {
  doc: Doc
  onEvidence: (seqs: number[]) => void
}) {
  const evidence = new Map((doc.evidence ?? []).map((item) => [item.event_seq, item]))
  const hasContent =
    doc.problem.statement ||
    (doc.problem.symptoms?.length ?? 0) > 0 ||
    (doc.diagnosis?.length ?? 0) > 0 ||
    doc.root_cause.text ||
    (doc.solution?.length ?? 0) > 0 ||
    (doc.insights?.length ?? 0) > 0 ||
    (doc.concepts?.length ?? 0) > 0

  const EvidenceChips = ({ values }: { values?: number[] | null }) => {
    const list = seqs(values)
    if (list.length === 0) return null
    return (
      <span className="evidence-chips">
        {list.map((seq) => {
          const item = evidence.get(seq)
          return (
            <button
              key={seq}
              className={`evidence-chip${item?.verified ? ' verified' : ''}`}
              onClick={() => onEvidence([seq])}
              title={item?.quote || '查看原始事件'}
            >
              E{seq}
            </button>
          )
        })}
      </span>
    )
  }

  const Section = ({
    title,
    icon,
    children,
    hint,
  }: {
    title: string
    icon: ReactNode
    children: ReactNode
    hint?: string
  }) => (
    <section className="learn-section">
      <div className="learn-section-head">
        <h2>
          {icon}
          {title}
        </h2>
        {hint && <span>{hint}</span>}
      </div>
      {children}
    </section>
  )

  const Command = ({ text, kind }: { text: string; kind?: string }) => (
    <div className="command-block">
      <div className="command-head">
        {kind && <span className="command-kind">{commandKindLabel(kind)}</span>}
        <CopyButton text={text} label="复制命令" />
      </div>
      <pre>
        <code>{text}</code>
      </pre>
    </div>
  )

  if (!hasContent) {
    return (
      <div className="empty-inline">
        这份文档只有摘要或旧格式内容。切换到 Markdown 视图可以看到完整原文。
      </div>
    )
  }

  return (
    <div className="learn-view">
      {(doc.problem.statement ||
        (doc.problem.symptoms?.length ?? 0) > 0 ||
        (doc.problem.error_signatures?.length ?? 0) > 0) && (
        <Section title="问题与症状" icon={<Search size={17} />}>
          {doc.problem.statement && <p className="lead">{doc.problem.statement}</p>}
          {(doc.problem.symptoms?.length ?? 0) > 0 && (
            <ul className="bullet-list">
              {doc.problem.symptoms?.map((item, index) => (
                <li key={index}>{item}</li>
              ))}
            </ul>
          )}
          {(doc.problem.error_signatures?.length ?? 0) > 0 && (
            <div className="signature-list">
              {doc.problem.error_signatures?.map((signature, index) => (
                <pre key={index}>
                  <code>{signature}</code>
                </pre>
              ))}
            </div>
          )}
          <EvidenceChips values={doc.problem.evidence} />
        </Section>
      )}

      {(doc.diagnosis?.length ?? 0) > 0 && (
        <Section
          title="定位路径"
          icon={<GitBranch size={17} />}
          hint="当时是怎么一步步缩小范围的"
        >
          <div className="timeline">
            {doc.diagnosis?.map((step, index) => (
              <article className="timeline-item" key={`${step.order}-${index}`}>
                <div className="timeline-marker">{step.order || index + 1}</div>
                <div className="timeline-body">
                  {step.observation && (
                    <p>
                      <span className="field-label">观察</span>
                      {step.observation}
                    </p>
                  )}
                  {step.hypothesis && (
                    <p>
                      <span className="field-label">假设</span>
                      {step.hypothesis}
                    </p>
                  )}
                  {step.action && (
                    <p>
                      <span className="field-label">动作</span>
                      {step.action}
                    </p>
                  )}
                  {step.command && <Command text={step.command} kind={step.command_kind} />}
                  {step.expected_signal && (
                    <p>
                      <span className="field-label">预期信号</span>
                      {step.expected_signal}
                    </p>
                  )}
                  {step.result && (
                    <p>
                      <span className="field-label">实际结果</span>
                      {step.result}
                    </p>
                  )}
                  {step.conclusion && (
                    <p className="conclusion">
                      <CheckCircle2 size={14} />
                      {step.conclusion}
                    </p>
                  )}
                  <EvidenceChips values={step.evidence} />
                </div>
              </article>
            ))}
          </div>
        </Section>
      )}

      {(doc.rejected_hypotheses?.length ?? 0) > 0 && (
        <Section
          title="被排除的假设"
          icon={<XCircle size={17} />}
          hint="比正确答案更能训练判断力"
        >
          <div className="card-stack">
            {doc.rejected_hypotheses?.map((item, index) => (
              <div className="soft-card rejected" key={index}>
                <strong>{item.hypothesis}</strong>
                <p>
                  <span className="field-label">排除原因</span>
                  {item.why_rejected}
                </p>
                <EvidenceChips values={item.evidence} />
              </div>
            ))}
          </div>
        </Section>
      )}

      {doc.root_cause.text && (
        <Section title="根因" icon={<Target size={17} />} hint={`置信度：${doc.root_cause.confidence}`}>
          <p className="lead">{doc.root_cause.text}</p>
          {!doc.root_cause.known && (
            <Badge tone="amber">根因尚未被证据确认</Badge>
          )}
          <EvidenceChips values={doc.root_cause.evidence} />
        </Section>
      )}

      {(doc.solution?.length ?? 0) > 0 && (
        <Section title="修复步骤" icon={<Wrench size={17} />}>
          <div className="solution-list">
            {doc.solution?.map((step, index) => (
              <div className="solution-step" key={`${step.order}-${index}`}>
                <div className="solution-order">{step.order || index + 1}</div>
                <div>
                  <strong>{step.action}</strong>
                  {step.detail && <p>{step.detail}</p>}
                  {step.command && <Command text={step.command} />}
                  <EvidenceChips values={step.evidence} />
                </div>
              </div>
            ))}
          </div>
        </Section>
      )}

      {(doc.verification?.length ?? 0) > 0 && (
        <Section title="验证" icon={<CheckCircle2 size={17} />}>
          <div className="card-stack">
            {doc.verification?.map((item, index) => (
              <div className="soft-card" key={index}>
                <div className="soft-card-head">
                  <strong>{item.check}</strong>
                  <Badge tone={verificationTone(item.status)}>
                    {verificationLabel(item.status)}
                  </Badge>
                </div>
                <p>
                  <span className="field-label">预期</span>
                  {item.expected || '—'}
                </p>
                <p>
                  <span className="field-label">实际</span>
                  {item.observed || '—'}
                </p>
                <EvidenceChips values={item.evidence} />
              </div>
            ))}
          </div>
        </Section>
      )}

      {(doc.commands?.length ?? 0) > 0 && (
        <Section
          title="有效命令"
          icon={<Terminal size={17} />}
          hint="只保留真正推进判断或验证的命令"
        >
          <div className="command-list">
            {doc.commands?.map((item, index) => (
              <div className="soft-card" key={index}>
                <div className="soft-card-head">
                  <strong>{item.purpose || '命令'}</strong>
                  <span className="badge-row">
                    {item.kind && <Badge tone="outline">{commandKindLabel(item.kind)}</Badge>}
                    {item.risk && <Badge tone="amber">风险：{item.risk}</Badge>}
                  </span>
                </div>
                <Command text={item.command} />
                {item.expected_signal && (
                  <p>
                    <span className="field-label">看什么</span>
                    {item.expected_signal}
                  </p>
                )}
                <EvidenceChips values={item.evidence} />
              </div>
            ))}
          </div>
        </Section>
      )}

      {((doc.insights?.length ?? 0) > 0 || (doc.lessons?.length ?? 0) > 0) && (
        <Section title="可迁移经验" icon={<Lightbulb size={17} />}>
          {(doc.insights?.length ?? 0) > 0 && (
            <div className="card-stack">
              {doc.insights?.map((item, index) => (
                <div className="soft-card insight" key={index}>
                  <p>{item.text}</p>
                  <div className="meta-row">
                    <Badge tone="outline">置信度 {item.confidence}</Badge>
                    <EvidenceChips values={item.evidence} />
                  </div>
                </div>
              ))}
            </div>
          )}
          {(doc.lessons?.length ?? 0) > 0 && (
            <ul className="bullet-list lessons">
              {doc.lessons?.map((lesson, index) => (
                <li key={index}>{lesson}</li>
              ))}
            </ul>
          )}
        </Section>
      )}

      {(doc.pitfalls?.length ?? 0) > 0 && (
        <Section title="常见坑" icon={<AlertTriangle size={17} />}>
          <div className="card-stack">
            {doc.pitfalls?.map((item, index) => (
              <div className="soft-card pitfall" key={index}>
                <strong>{item.trap}</strong>
                {item.why && (
                  <p>
                    <span className="field-label">为什么</span>
                    {item.why}
                  </p>
                )}
                {item.avoid && (
                  <p>
                    <span className="field-label">怎么避免</span>
                    {item.avoid}
                  </p>
                )}
                <EvidenceChips values={item.evidence} />
              </div>
            ))}
          </div>
        </Section>
      )}

      {(doc.concepts?.length ?? 0) > 0 && (
        <Section title="知识卡" icon={<BookOpen size={17} />} hint="概念必须带最小示例">
          <div className="concept-grid">
            {doc.concepts?.map((concept, index) => (
              <article className="concept-card" key={`${concept.name}-${index}`}>
                <div className="concept-head">
                  <strong>{concept.name}</strong>
                  {concept.stack && <Badge tone="blue">{concept.stack}</Badge>}
                </div>
                {concept.why_relevant && <p>{concept.why_relevant}</p>}
                {concept.syntax && (
                  <div className="mini-code">
                    <div className="mini-code-label">语法</div>
                    <pre>
                      <code>{concept.syntax}</code>
                    </pre>
                  </div>
                )}
                {concept.example && (
                  <div className="mini-code">
                    <div className="mini-code-label">最小示例</div>
                    <pre>
                      <code>{concept.example}</code>
                    </pre>
                  </div>
                )}
                <div className="concept-foot">
                  {concept.docs_url && (
                    <a href={concept.docs_url} target="_blank" rel="noreferrer noopener">
                      文档 ↗
                    </a>
                  )}
                  <EvidenceChips values={concept.evidence} />
                </div>
              </article>
            ))}
          </div>
        </Section>
      )}

      {(doc.blind_spots?.length ?? 0) > 0 && (
        <Section title="暴露出的盲区" icon={<CircleHelp size={17} />}>
          <div className="card-stack">
            {doc.blind_spots?.map((item, index) => (
              <div className="soft-card" key={index}>
                <div className="soft-card-head">
                  <strong>{item.point}</strong>
                  {item.kind && <Badge tone="violet">{item.kind}</Badge>}
                </div>
                {item.why_it_matters && <p>{item.why_it_matters}</p>}
                <EvidenceChips values={item.evidence} />
              </div>
            ))}
          </div>
        </Section>
      )}

      {(doc.evidence?.length ?? 0) > 0 && (
        <Section
          title="证据来源"
          icon={<FileText size={17} />}
          hint={evidenceLabel(doc.validation.evidence_checked, doc.validation.evidence_verified)}
        >
          <div className="evidence-table">
            {doc.evidence?.map((item) => (
              <button
                className="evidence-row"
                key={item.event_seq}
                onClick={() => onEvidence([item.event_seq])}
              >
                <span className="evidence-seq">E{item.event_seq}</span>
                <span className="evidence-kind">{item.kind}</span>
                <span className="evidence-quote">{item.quote}</span>
                <Badge tone={item.verified ? 'green' : 'red'}>
                  {item.verified ? '已核验' : '未核验'}
                </Badge>
              </button>
            ))}
          </div>
        </Section>
      )}

      {((doc.uncertain?.length ?? 0) > 0 ||
        (doc.validation.warnings?.length ?? 0) > 0) && (
        <Section title="不确定项" icon={<AlertTriangle size={17} />}>
          <div className="card-stack">
            {doc.uncertain?.map((item, index) => (
              <div className="soft-card uncertain" key={index}>
                <strong>{item.field}</strong>
                <p>{item.reason}</p>
              </div>
            ))}
            {doc.validation.warnings?.map((warning, index) => (
              <div className="soft-card uncertain" key={`w-${index}`}>
                <p>{warning}</p>
              </div>
            ))}
          </div>
        </Section>
      )}

      {((doc.artifacts.files?.length ?? 0) > 0 ||
        (doc.artifacts.urls?.length ?? 0) > 0) && (
        <Section title="相关产出" icon={<Folder size={17} />}>
          <div className="artifact-list">
            {doc.artifacts.files?.map((file) => (
              <span className="artifact" key={file}>
                <FileText size={13} /> {file}
              </span>
            ))}
            {doc.artifacts.urls?.map((url) => (
              <a className="artifact" key={url} href={url} target="_blank" rel="noreferrer noopener">
                {url}
              </a>
            ))}
          </div>
        </Section>
      )}

      <div className="learn-footer">
        <span>分类：{kindLabel(doc.knowledge_type)}</span>
        <span>证据核验：{doc.validation.evidence_verified}/{doc.validation.evidence_checked}</span>
        <span>整体置信度：{Math.round(doc.confidence * 100)}%</span>
      </div>
    </div>
  )
}
