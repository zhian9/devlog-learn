import { Component, type ErrorInfo, type ReactNode } from 'react'

// A render error must never blank the whole app: one malformed payload used to
// unmount the entire React tree and made every control unclickable. The layout
// keyed this boundary by route so the sidebar keeps working after a page crash.
export class ErrorBoundary extends Component<
  { children: ReactNode; compact?: boolean },
  { error: Error | null }
> {
  state: { error: Error | null } = { error: null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('devlog-learn UI render error', error, info)
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children
    return (
      <div className={`crash-panel${this.props.compact ? ' compact' : ''}`} role="alert">
        <h2>这个页面渲染失败了</h2>
        <p>其他页面仍然可用。可以先回到概览，或重新加载这个页面。</p>
        <pre>{error.message}</pre>
        <div className="crash-actions">
          <a className="button ghost" href="/">
            回到概览
          </a>
          <button className="button primary" onClick={() => window.location.reload()}>
            重新加载
          </button>
          <button className="button ghost" onClick={() => this.setState({ error: null })}>
            重试本页
          </button>
        </div>
      </div>
    )
  }
}
