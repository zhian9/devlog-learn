import { useQuery } from '@tanstack/react-query'
import {
  CircleHelp,
  LayoutDashboard,
  Library,
  Settings,
  ShieldCheck,
  Sparkles,
  Zap,
} from 'lucide-react'
import { NavLink, Outlet } from 'react-router-dom'
import { api } from '../api'

const nav = [
  { to: '/', label: '概览', icon: LayoutDashboard, end: true },
  { to: '/library', label: '文档库', icon: Library },
  { to: '/extract', label: '提炼', icon: Zap },
  { to: '/questions', label: '待办问题', icon: CircleHelp },
  { to: '/settings', label: '设置与导出', icon: Settings },
]

export function Layout() {
  const status = useQuery({ queryKey: ['status'], queryFn: api.status })
  const openQuestions = status.data?.questions.open ?? 0

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <div className="brand-mark">
            <Sparkles size={19} />
          </div>
          <div>
            <div className="brand-name">devlog-learn</div>
            <div className="brand-sub">项目记忆与学习案例</div>
          </div>
        </div>

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
        <Outlet />
      </main>
    </div>
  )
}
