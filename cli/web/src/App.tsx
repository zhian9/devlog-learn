import { Route, Routes } from 'react-router-dom'
import { Layout } from './components/Layout'
import { Dashboard } from './pages/Dashboard'
import { DocDetail } from './pages/DocDetail'
import { Extract } from './pages/Extract'
import { Library } from './pages/Library'
import { Questions } from './pages/Questions'
import { Settings } from './pages/Settings'
import { EmptyState, PageHeader } from './components/ui'
import { Link } from 'react-router-dom'

function NotFound() {
  return (
    <div className="page">
      <PageHeader title="页面不存在" />
      <EmptyState
        title="找不到这个页面"
        description="可能是链接过期，或者文档被移除了。"
        action={
          <Link className="button primary" to="/">
            回到概览
          </Link>
        }
      />
    </div>
  )
}

export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="library" element={<Library />} />
        <Route path="docs/:id" element={<DocDetail />} />
        <Route path="extract" element={<Extract />} />
        <Route path="questions" element={<Questions />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
