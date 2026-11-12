import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Database,
  FileDown,
  Folder,
  HardDrive,
  RefreshCw,
  ShieldCheck,
  Terminal,
} from 'lucide-react'
import { api, formatBytes, formatDateTime } from '../api'
import {
  Badge,
  Banner,
  ErrorBox,
  Loading,
  PageHeader,
} from '../components/ui'

export function Settings() {
  const queryClient = useQueryClient()
  const [profile, setProfile] = useState('both')
  const [includeStale, setIncludeStale] = useState(false)
  const [prune, setPrune] = useState(false)
  const [digest, setDigest] = useState(true)
  const [digestReview, setDigestReview] = useState('any')
  const [notice, setNotice] = useState('')
  const [warnings, setWarnings] = useState<string[]>([])

  const config = useQuery({ queryKey: ['config'], queryFn: api.config })
  const status = useQuery({ queryKey: ['status'], queryFn: api.status })
  const exportDocs = useMutation({
    mutationFn: () =>
      api.export({
        profile,
        include_stale: includeStale,
        prune,
        digest,
        digest_review: digestReview,
      }),
    onSuccess: (result) => {
      setWarnings(result.warnings ?? [])
      const digestNote = result.digest_path
        ? `；AGENTS.md 已更新（${result.digest_docs} 篇文档）`
        : ''
      setNotice(
        `已生成 ${result.rendered} 个 Markdown，清理 ${result.pruned} 个过期文件；索引包含 ${result.index_docs} 篇文档、${result.index_projects} 个项目${digestNote}。输出目录：${result.out_dir}`,
      )
      queryClient.invalidateQueries({ queryKey: ['status'] })
    },
  })

  if (config.isLoading) return <Loading label="读取设置…" />
  if (config.error) return <ErrorBox error={config.error} />

  return (
    <div className="page">
      <PageHeader
        eyebrow="LOCAL SETUP"
        title="设置与导出"
        subtitle="所有路径和状态都来自当前进程；这个页面不会修改配置文件。"
      />

      {notice && (
        <Banner tone="green" onDismiss={() => setNotice('')}>
          {notice}
        </Banner>
      )}
      {exportDocs.error && <ErrorBox error={exportDocs.error} />}
      {warnings.length > 0 && (
        <Banner tone="amber">
          {warnings.slice(0, 3).join('；')}
          {warnings.length > 3 ? '…' : ''}
        </Banner>
      )}

      <div className="settings-grid">
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>
                <FileDown size={18} /> 生成 Markdown、索引与 Agent 摘要
              </h2>
              <p>把缓存里的文档写入输出目录，并重建全局和项目索引。不会调用模型。</p>
            </div>
          </div>
          <div className="form-row">
            <label>
              生成内容
              <select value={profile} onChange={(event) => setProfile(event.target.value)}>
                <option value="both">Agent 版 + 学习版（推荐）</option>
                <option value="agent">仅 Agent 版</option>
                <option value="learning">仅学习版</option>
              </select>
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={includeStale}
                onChange={(event) => setIncludeStale(event.target.checked)}
              />
              包含已过期文档
            </label>
            <label className="check-row">
              <input
                type="checkbox"
                checked={prune}
                onChange={(event) => setPrune(event.target.checked)}
              />
              清理 manifest 中未再生成的旧文件
            </label>
          </div>
          <div className="form-row">
            <label className="check-row">
              <input
                type="checkbox"
                checked={digest}
                onChange={(event) => setDigest(event.target.checked)}
              />
              同时生成 AGENTS.md（Agent 记忆摘要）
            </label>
            {digest && (
              <label>
                Agent 摘要范围
                <select
                  value={digestReview}
                  onChange={(event) => setDigestReview(event.target.value)}
                >
                  <option value="any">全部有效文档</option>
                  <option value="approved">仅已通过审核</option>
                  <option value="pending">仅待审核</option>
                </select>
              </label>
            )}
          </div>
          <button
            className="button primary"
            onClick={() => {
              setNotice('')
              setWarnings([])
              exportDocs.mutate()
            }}
            disabled={exportDocs.isPending}
          >
            <RefreshCw size={15} className={exportDocs.isPending ? 'spin' : ''} />
            {exportDocs.isPending ? '正在生成…' : '立即生成'}
          </button>
          <p className="hint">
            学习版写入 <code>*.learn.md</code>，不会覆盖 Agent 版 <code>*.md</code>。
            索引和 AGENTS.md 只引用有效文档。若 AGENTS.md 不是本工具生成的，会保留原文件并在结果中提示。
          </p>
        </section>

        <section className="panel">
          <div className="panel-head">
            <div>
              <h2>
                <Folder size={18} /> 本地路径
              </h2>
              <p>由启动命令的 --cache-dir / --cursor / --out 决定。</p>
            </div>
          </div>
          <div className="path-list">
            <div>
              <span>缓存目录</span>
              <code>{config.data?.cache_dir}</code>
            </div>
            <div>
              <span>游标文件</span>
              <code>{config.data?.cursor_path}</code>
            </div>
            <div>
              <span>输出目录</span>
              <code>{config.data?.out_dir}</code>
            </div>
            <div>
              <span>监听地址</span>
              <code>{config.data?.addr}</code>
            </div>
          </div>
          <div className="local-note">
            <ShieldCheck size={15} />
            仅绑定回环地址，并带进程级访问令牌。数据不会离开本机。
          </div>
        </section>
      </div>

      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>
              <HardDrive size={18} /> 缓存与输出状态
            </h2>
            <p>只读统计。</p>
          </div>
          {status.data && (
            <Badge tone={status.data.cache.exists ? 'green' : 'amber'}>
              {status.data.cache.exists ? '缓存可用' : '缓存尚未创建'}
            </Badge>
          )}
        </div>
        {status.isLoading && <Loading />}
        {status.error && <ErrorBox error={status.error} />}
        {status.data && (
          <div className="settings-status">
            <div className="status-block">
              <h3>
                <Database size={15} /> 文档
              </h3>
              <p>
                有效 {status.data.cache.active_docs} · 过期 {status.data.cache.stale_docs} ·
                缓存 {formatBytes(status.data.cache.size_bytes)}
              </p>
              <p>
                待审 {status.data.cache.pending} · 通过 {status.data.cache.approved} · 驳回{' '}
                {status.data.cache.rejected}
              </p>
            </div>
            <div className="status-block">
              <h3>
                <Terminal size={15} /> 增量游标
              </h3>
              <p>
                {status.data.cursor.exists ? `${status.data.cursor.files} 个文件` : '尚未生成'}
                {status.data.cursor.errors > 0 && ` · ${status.data.cursor.errors} 个失败`}
              </p>
              <p>最近更新：{formatDateTime(status.data.cursor.updated_at)}</p>
            </div>
            <div className="status-block">
              <h3>
                <FileDown size={15} /> 输出新鲜度
              </h3>
              <p>
                索引 {status.data.outputs.index_exists ? '已生成' : '未生成'}
                {status.data.outputs.index_stale && '（已过期）'}
              </p>
              <p>
                AGENTS.md {status.data.outputs.digest_exists ? '已生成' : '未生成'}
                {status.data.outputs.digest_stale && '（已过期）'}
              </p>
            </div>
          </div>
        )}
      </section>

      <section className="panel">
        <div className="panel-head">
          <div>
            <h2>
              <Terminal size={18} /> Provider 探测
            </h2>
            <p>只检查可执行文件与会话目录是否存在，不解析日志。</p>
          </div>
        </div>
        {status.data && (
          <div className="provider-grid">
            {status.data.providers.map((provider) => (
              <div className="provider-card" key={provider.name}>
                <div className="health-title">
                  <span className={`dot ${provider.bin_found ? 'ok' : 'bad'}`} />
                  {provider.name}
                </div>
                <div className="health-line">
                  命令 <code>{provider.bin}</code>
                  <Badge tone={provider.bin_found ? 'green' : 'red'}>
                    {provider.bin_found ? '已找到' : '未找到'}
                  </Badge>
                </div>
                <div className="health-line">
                  会话目录
                  <Badge tone={provider.dir_exists ? 'green' : 'amber'}>
                    {provider.dir_exists ? `${provider.session_files} 个文件` : '不存在'}
                  </Badge>
                </div>
                <div className="health-line path">{provider.dir}</div>
              </div>
            ))}
          </div>
        )}
        {status.data?.warnings && status.data.warnings.length > 0 && (
          <Banner tone="amber">{status.data.warnings.join('；')}</Banner>
        )}
      </section>

      <section className="panel help-panel">
        <h2>日常操作</h2>
        <ol className="help-list">
          <li>双击启动命令或快捷方式，浏览器窗口会自动打开（底部任务栏可固定）。</li>
          <li>在“文档库”搜索和阅读；点证据编号可以看到当时的原始事件和命令输出。</li>
          <li>阅读后点“通过/驳回”并写一句备注，之后索引和 AGENTS.md 会反映审核状态。</li>
          <li>需要更新 Agent 可读文件时，回到这里点“立即生成”，或使用概览页的一键同步。</li>
          <li>关闭窗口不会删除任何数据；再次启动会继续使用同一个缓存目录。</li>
        </ol>
      </section>
    </div>
  )
}
