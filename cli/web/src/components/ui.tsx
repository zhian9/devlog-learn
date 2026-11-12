import { useState, type ReactNode } from 'react'
import { AlertTriangle, Check, Copy, Loader2, X } from 'lucide-react'

export type Tone =
  | 'neutral'
  | 'green'
  | 'amber'
  | 'red'
  | 'blue'
  | 'violet'
  | 'outline'

export function Badge({
  children,
  tone = 'neutral',
  title,
}: {
  children: ReactNode
  tone?: Tone
  title?: string
}) {
  return (
    <span className={`badge badge-${tone}`} title={title}>
      {children}
    </span>
  )
}

export function PageHeader({
  eyebrow,
  title,
  subtitle,
  actions,
}: {
  eyebrow?: string
  title: string
  subtitle?: ReactNode
  actions?: ReactNode
}) {
  return (
    <header className="page-header">
      <div>
        {eyebrow && <div className="eyebrow">{eyebrow}</div>}
        <h1>{title}</h1>
        {subtitle && <p className="page-subtitle">{subtitle}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </header>
  )
}

export function StatCard({
  label,
  value,
  detail,
  icon,
  tone = 'neutral',
}: {
  label: string
  value: ReactNode
  detail?: ReactNode
  icon?: ReactNode
  tone?: Tone
}) {
  return (
    <div className={`stat-card stat-${tone}`}>
      <div className="stat-top">
        <span>{label}</span>
        {icon && <span className="stat-icon">{icon}</span>}
      </div>
      <strong>{value}</strong>
      {detail && <div className="stat-detail">{detail}</div>}
    </div>
  )
}

export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon?: ReactNode
  title: string
  description?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="empty-state">
      {icon && <div className="empty-icon">{icon}</div>}
      <h3>{title}</h3>
      {description && <p>{description}</p>}
      {action && <div className="empty-action">{action}</div>}
    </div>
  )
}

export function ErrorBox({ error }: { error: unknown }) {
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div className="error-box" role="alert">
      <AlertTriangle size={18} />
      <span>{message}</span>
    </div>
  )
}

export function Loading({ label = '加载中…' }: { label?: string }) {
  return (
    <div className="loading">
      <Loader2 size={18} className="spin" />
      <span>{label}</span>
    </div>
  )
}

export function Banner({
  tone = 'amber',
  children,
  onDismiss,
}: {
  tone?: 'amber' | 'blue' | 'green' | 'red'
  children: ReactNode
  onDismiss?: () => void
}) {
  return (
    <div className={`banner banner-${tone}`}>
      <div>{children}</div>
      {onDismiss && (
        <button className="icon-button" onClick={onDismiss} aria-label="关闭">
          <X size={15} />
        </button>
      )}
    </div>
  )
}

export function CopyButton({ text, label }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      className="copy-button"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1400)
        } catch {
          setCopied(false)
        }
      }}
      title="复制"
    >
      {copied ? <Check size={13} /> : <Copy size={13} />}
      {label && <span>{copied ? '已复制' : label}</span>}
    </button>
  )
}

export function reviewTone(status: string): Tone {
  switch (status) {
    case 'approved':
      return 'green'
    case 'rejected':
      return 'red'
    case 'pending':
      return 'amber'
    default:
      return 'neutral'
  }
}

export function reviewLabel(status: string): string {
  switch (status) {
    case 'approved':
      return '已通过'
    case 'rejected':
      return '已驳回'
    case 'pending':
      return '待审核'
    default:
      return status || '未审核'
  }
}

export function statusLabel(status: string): string {
  switch (status) {
    case 'resolved':
      return '已解决'
    case 'workaround':
      return '有绕过方案'
    case 'unresolved':
      return '未解决'
    case 'unknown':
      return '状态未知'
    default:
      return status || '未知'
  }
}

export function statusTone(status: string): Tone {
  switch (status) {
    case 'resolved':
      return 'green'
    case 'workaround':
      return 'blue'
    case 'unresolved':
      return 'amber'
    default:
      return 'neutral'
  }
}

export function kindLabel(kind: string): string {
  const labels: Record<string, string> = {
    bug_fix: '问题修复',
    learning: '学习',
    architecture: '架构',
    development: '开发',
    refactoring: '重构',
    configuration: '配置',
    decision: '决策',
    unknown: '未分类',
  }
  return labels[kind] ?? kind
}

export function difficultyLabel(value: string): string {
  const labels: Record<string, string> = {
    low: '简单',
    medium: '中等',
    high: '困难',
    unknown: '难度未知',
  }
  return labels[value] ?? value
}

export function learningValueLabel(value?: string): string {
  const labels: Record<string, string> = {
    low: '学习价值低',
    medium: '学习价值中',
    high: '学习价值高',
  }
  return value ? labels[value] ?? value : ''
}

export function evidenceLabel(checked: number, verified: number): string {
  if (!checked) return '无证据'
  return `证据 ${verified}/${checked}`
}
