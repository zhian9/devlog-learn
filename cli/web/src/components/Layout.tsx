import { useQuery } from '@tanstack/react-query'
import {
  BriefcaseBusiness,
  CircleHelp,
  LayoutDashboard,
  Library,
  Settings,
  ShieldCheck,
  Sparkles,
  Zap,
} from 'lucide-react'
import { Link, NavLink, Outlet, useLocation } from 'react-router-dom'
import { api } from '../api'
import { ErrorBoundary } from './ErrorBoundary'

const nav = [
  { to: '/', label: '概览', icon: LayoutDashboard, end: true },
  { to: '/library', label: '文档库', icon: Library },
  { to: '/experiences', label: '项目经历', icon: BriefcaseBusiness },
  { to: '/extract', label: '提炼', icon: Zap },
  { to: '/questions', label: '待办问题', icon: CircleHelp },
  { to: '/settings', label: '设置与导出', icon: Settings },
]

export function Layout() {
  const status = useQuery({ queryKey: ['status'], queryFn: api.status })
  const location = useLocation()
  const openQuestions = status.data?.questions.open ?? 0

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <Link className="brand" to="/">
          <div className="brand-mark">
            <Sparkles size={19} />
          </div>
          <div>
            <div className="brand-name">devlog-learn</div>
            <div className="brand-sub">项目记忆与学习案例</div>
          </div>
        </Link>

        <nav className="nav">
          {nav.map((item) => {
            const Icon = item.icon
            return (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
              >
                <Icon size={17} />
                <span>{item.label}</span>
                {item.to === '/questions' && openQuestions > 0 && (
                  <span className="nav-count">{openQuestions}</span>
                )}
              </NavLink>
            )
          })}
        </nav>

        <div className="sidebar-foot">
          <div className="local-badge">
            <ShieldCheck size={14} />
            仅本机运行
          </div>
          <div className="version">
            v{status.data?.tool_version ?? '—'} · {status.data?.cache.active_docs ?? 0} 篇文档
          </div>
        </div>
      </aside>

      <main className="main">
        <ErrorBoundary key={location.pathname + location.search} compact>
          <Outlet />
        </ErrorBoundary>
      </main>
    </div>
  )
}
