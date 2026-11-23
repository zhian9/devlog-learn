# devlog-learn

把本机 **Claude Code** 与 **Codex** 的历史会话日志，解析、切分、脱敏，提炼成结构化知识文档。

同一份 Doc 可以生成四种产物：

- **本地 Web UI**：阅读、搜索、证据回看、人工审核、问题闭环、选择会话提炼。
- **Agent Markdown + 索引**：稳定的 RAG 语料。
- **`AGENTS.md` 摘要**：没有 MCP 客户端的 Agent 也能直接读取项目记忆。
- **中文学习模板**（`*.learn.md`）：定位路径、被排除的假设、知识卡、盲区、自测题。

当前版本：`0.13.0`。

---

## 目录

- [适合谁](#适合谁)
- [核心能力](#核心能力)
- [环境要求](#环境要求)
- [快速开始](#快速开始)
- [各种用法（按场景）](#各种用法按场景)
- [本地 Web UI](#本地-web-ui)
- [CLI 工作流](#cli-工作流)
- [命令参考](#命令参考)
- [缓存与增量语义](#缓存与增量语义)
- [配置](#配置)
- [Doc 与 RAG 契约](#doc-与-rag-契约)
- [Agent 接入](#agent-接入)
- [隐私与安全](#隐私与安全)
- [Windows 说明](#windows-说明)
- [性能与资源](#性能与资源)
- [故障排查](#故障排查)
- [项目结构](#项目结构)
- [开发与测试](#开发与测试)
- [已知限制与非目标](#已知限制与非目标)

---

## 适合谁

- 你经常用 Claude Code / Codex 做真实项目，但会话记录很快被淹没。
- 你想把“当时怎么定位问题、哪些假设被排除、哪条命令有效”沉淀下来。
- 你需要给 Agent 一份可检索、可审核、可追溯到原始事件的项目记忆。
- 你希望日志解析、缓存、索引和 Web UI 在本机运行；模型提炼通过你自行配置的本地 CLI 发起。

**明确不支持 Cursor IDE。** 当前只解析 `claude-code` 与 `codex`。

---

## 核心能力

| 能力 | 说明 |
|---|---|
| 本地 Web UI | 单 exe，内嵌前端；Windows 下默认用 Edge/Chrome 应用窗口打开 |
| 会话选择 | 先看会话与 Segment，再决定提炼哪些；不会默认全量调用模型 |
| 增量提炼 | SHA-256 cursor + 版本化 cache，未变化文件不重复解析 |
| 结构化 Doc | 问题、根因、修复、验证、证据、定位路径、有效命令、知识卡、盲区 |
| 证据核验 | 每条结论引用事件 seq，可点击回看原始事件、工具调用、退出码 |
| 人工审核 | `pending` / `approved` / `rejected` + 备注，不污染 Doc 契约 |
| 问题闭环 | 跨文档聚合 open questions，解决状态保存在 sidecar |
| 双 Markdown | Agent 版 `*.md` 与中文学习版 `*.learn.md` 互不覆盖 |
| 项目经历 | 同一目录的会话聚合为项目/实习经历，附静态复习清单；模型润色需显式 `synthesize` |
| 索引与摘要 | 全局/项目 `index.json`、`README.md`、`AGENTS.md`、`EXPERIENCE.md` |
| MCP | 只读 stdio Server：搜索、取文档、关联、项目列表 |
| 隐私 | 本地规则脱敏；Web UI 只绑回环地址；不加载 CDN、不发送遥测 |

---

## 环境要求

- **Go 1.24+**（本机使用 1.25.1）。
- **Node 22+**：只有重新构建前端时需要；仓库已包含构建产物，日常只跑 `go build`。
- **Windows / macOS / Linux**。Windows 支持最完整，包括 `.cmd` / `.bat` shim。
- 至少安装一个本地 CLI：
  - `claude`（Claude Code）
  - `codex`（Codex CLI）

运行时只需要 Go 标准库和 `github.com/spf13/cobra`；不会引入 Node 运行时。

---

## 快速开始

```bash
cd cli

# 1. 构建
go build -o devlog-learn.exe ./cmd
./devlog-learn.exe --version      # devlog-learn 0.13.0

# 2. 打开 Web UI（推荐）
./devlog-learn.exe serve
```

Windows 也可以直接双击 **`start-webui.cmd`**。它会启动 `serve` 并自动打开应用窗口。

进入界面后：

1. 在“文档库”搜索、阅读。
2. 点证据编号 `E1` / `E2` 回看原始事件。
3. 阅读后点“通过/驳回/待审”，写一句备注。
4. 在“待办问题”里标记解决或重新打开。
5. 在“提炼”里选择会话，确认后实时查看进度。
6. 在“设置与导出”里一键生成 Markdown、索引、`AGENTS.md` 和项目经历 `EXPERIENCE.md`。

只使用 CLI 也能完成同样的事情：

```bash
# 本地预览：不调用模型，不写 cache/cursor
./devlog-learn.exe dry-run --provider all --since 7d --limit 2 --show-drops

# 列出可选会话与 Segment
./devlog-learn.exe sessions --provider all --since 7d

# 交互式选择 → extract → render → index
./devlog-learn.exe wizard --provider all --since 7d

# 或者直接选择会话
./devlog-learn.exe run --session claude-code:<session-id>

# 生成 Agent 摘要
./devlog-learn.exe digest --cache-dir .devlog-cache --out .devlog-docs
```

默认路径：

| 参数 | 默认值 |
|---|---|
| `--cache-dir` | `.devlog-cache` |
| `--cursor` | `.devlog-cursor.json` |
| `render/index/digest --out` | `.devlog-docs` |

---

## 各种用法（按场景）

按“你现在想做什么”选择入口：

| 场景 | 推荐入口 | 最小命令 / 页面 |
|---|---|---|
| 只浏览、搜索、审核已有文档 | Web UI | `devlog-learn serve` → 文档库 / 待办问题 |
| 从会话里挑内容提炼 | Web UI | `serve` → 提炼 → 勾选会话或单个 Segment |
| 第一次用，想先看再提炼 | CLI（零成本） | `dry-run` → `sessions` → `wizard` |
| 一条命令跑完提炼 + 渲染 + 索引 | CLI | `run --provider all --since 7d --segments 1` |
| 定时增量更新 | CLI + 计划任务 | `run --provider all --since 1d --segments 0` |
| 给 Claude Code / Codex 按需检索 | MCP | `mcp` + 客户端配置 |
| 给任意 Agent 一份记忆文件 | 文件 | `digest` → `.devlog-docs/AGENTS.md` |
| 生成学习版 / Agent 版 Markdown | CLI | `render --profile learning` / 默认 agent |
| 把同一目录的会话整理成项目/实习经历 | CLI / Web UI | `experience render` / `serve` → 项目经历 |
| 标注经历类型、生成复习清单 | CLI | `experience set` / `experience checklist` |
| 查历史问题、关联文档、健康状态 | CLI | `search` / `related` / `status` / `questions` |
| 做 RAG 或自定义脚本 | Doc JSON | `docs/*.json`、`search --json`、`projects --json` |
| 多个仓库互不干扰 | 多缓存目录 | `--cache-dir` 或项目级 `.devlog-learn.json` |
| 开发前端 / 后端 | 源码 | `npm run dev`、`go test ./...` |

> 下文的命令默认在 `cli/` 目录执行。`extract` / `run` / `wizard` 的 `--provider` 默认是 `claude-code`；要同时处理 Codex，请显式加 `--provider all`。

### 1. 只阅读和人工闭环（不调用模型）

```bash
./devlog-learn.exe serve
# 或：./devlog-learn.exe serve --no-open --addr 127.0.0.1:0
```

- **文档库**：中文搜索；项目、审核状态、知识类型、解决状态、学习价值、生命周期、标签筛选。徽章和标签都可以直接点击下钻。
- **文档详情**：学习视图 / Agent Markdown / JSON；点 `E1`、`E2` 回看原始事件、工具输入输出和退出码。
- **人工审核**：通过 / 驳回 / 待审 + 备注；只改 `docs/<id>.json`，不删文件。
- **待办问题**：跨文档聚合，标记解决或重新打开；状态保存在 `<cache-dir>/questions.json`。
- **设置与导出**：生成 Markdown、索引、`AGENTS.md` 和项目经历 `EXPERIENCE.md`；路径可直接复制。
- 整个浏览过程不调用模型；只有审核、问题状态和导出会写缓存/输出。

CLI 等价操作：

```bash
./devlog-learn.exe review --list --status pending
./devlog-learn.exe review <doc-id> --status approved --note "verified locally"
./devlog-learn.exe questions
./devlog-learn.exe questions resolve <question-id> --note "fixed in v0.12"
./devlog-learn.exe questions reopen <question-id>
```

### 2. 在 Web UI 里提炼（日常推荐）

1. 打开“提炼”，选择来源（Claude Code / Codex / 全部）和时间范围（1d / 7d / 30d / 全部）。
2. 等待扫描完成（只解析日志，不调用模型；首次可能需要数秒到数十秒）。
3. 勾选整个会话，或展开“片段”只勾选某个 Segment；支持半选和“全选当前列表”。
4. 点“开始提炼”，确认框会明确提示“只有未命中缓存的 Segment 才会调用本地模型”。
5. 通过 SSE 实时看进度，可取消；失败后可按失败 Segment 重试。

注意：

- 同一个 `serve` 进程同时只允许一个提炼 job；job 历史只在内存，重启后消失。
- 关闭窗口不会删除缓存，也不会停止正在运行的 `serve` 进程（需要 Ctrl+C 或关闭控制台）。
- 缓存命中不会重复调用模型；`--force`/强制重提才会绕过缓存。

### 3. 第一次用：先看有什么（零成本）

```bash
# 发现/解析诊断，不调用模型、不写缓存
./devlog-learn.exe ingest --provider all --since 7d --limit 2 --show-events

# 列出可选会话与 Segment，拿到 selector / segment-id
./devlog-learn.exe sessions --provider all --since 7d
./devlog-learn.exe sessions --provider all --since 7d --json

# 解析 → 切分 → 脱敏 → Prompt 预算预览，不写 cursor/cache
./devlog-learn.exe dry-run --provider all --since 7d --limit 2 --show-drops
```

### 4. 命令行小步提炼

```bash
# 默认只提炼 1 个 Segment，避免一次产生大量模型调用
./devlog-learn.exe extract --provider all --since 7d --segments 1

# 明确选择整个会话
./devlog-learn.exe extract --provider all --session claude-code:<session-id>

# 明确选择单个 Segment
./devlog-learn.exe extract --provider all --segment <segment-id>

# 交互式：按编号多选（1,3 / 1-3 / all），确认后执行完整流程
./devlog-learn.exe wizard --provider all --since 7d
```

- `--segments 0` 表示全部；默认 1。显式 `--session` / `--segment` 时忽略 `--segments`。
- `extract` 未指定 `--out` 时把本次 Doc JSON 数组写到 stdout；要落盘用 `--out <dir>`。规范结果始终在 `<cache-dir>/docs/`。
- `--full` 忽略 cursor 但仍复用缓存；`--force` 忽略缓存并强制调用模型（会产生费用）。
- `--keep-raw` 保存失败的模型响应（保存前已脱敏）。
- 每个真实模型调用会在 stderr 打印 `usage:`，结束打印 `tokens:` 汇总。
- `--lang en` 可生成英文 Doc；默认中文。

### 5. 一条命令跑完（提炼 + 渲染 + 索引）

```bash
./devlog-learn.exe run --provider all --since 7d --segments 1 --out .devlog-docs
```

- 等价于 `extract → render(agent) → index`。
- 即使本次没有新 Segment，也会重新渲染 active Doc 并重建索引。
- `--prune` 会清理 `.devlog-manifest.json` 中登记、且内容 hash 未变化的旧输出；不会删除手改文件或未登记文件。
- `run` 只渲染 Agent 版；学习版需要额外执行 `render --profile learning`。

### 6. 生成 Markdown、索引、学习案例和 AGENTS.md

```bash
./devlog-learn.exe render --profile agent --out .devlog-docs
./devlog-learn.exe render --profile learning --out .devlog-docs
./devlog-learn.exe index --out .devlog-docs
./devlog-learn.exe digest --out .devlog-docs --review approved --limit 20
```

- **Agent 版**：`<out>/<project>/<date>-<slug>.md`，带 YAML front matter。
- **学习版**：`*.learn.md`，包含定位路径、被排除的假设、知识点卡片、自测题等；不会覆盖 Agent 版。
- **索引**：`index.json`、`README.md`、每个项目的索引；先 `render` 再 `index`，链接才指向真实文件。
- **AGENTS.md**：给 Agent 的紧凑记忆摘要；默认不覆盖非本工具生成的文件，确认后加 `--force`。
- `--include-stale` 才会渲染 stale 文档；`--prune` 只清理本 profile 的旧文件。

### 7. 给 Agent 用：MCP（按需检索）

```bash
# Claude Code（建议用绝对路径，-s user 表示全局可用）
claude mcp add -s user devlog-learn -- "D:\software\traceback\cli\devlog-learn.exe" mcp --cache-dir "D:\software\traceback\cli\.devlog-cache"
```

```toml
# Codex: ~/.codex/config.toml
[mcp_servers.devlog-learn]
command = "D:/software/traceback/cli/devlog-learn.exe"
args = ["mcp", "--cache-dir", "D:/software/traceback/cli/.devlog-cache"]
```

工具：`search_docs`、`get_doc`、`related_docs`、`list_projects`。

- 只读：不提炼、不写缓存、不调用模型；只服务 active Doc。
- `--review approved` 只看人工通过的内容；`--project <name>` 限定项目。
- `--project auto` 取的是 MCP 进程 cwd 的目录名，跨项目使用容易指错，建议显式传项目名或留空。
- 在 Claude Code 里用 `/mcp` 查看连接状态；配置改动后需要重连。
- 详细参数和调试方法见 [`mcp`](#mcp)。

### 8. 给 Agent 用：AGENTS.md（零基础设施）

```bash
./devlog-learn.exe digest --out .devlog-docs --review approved
```

把 `.devlog-docs/AGENTS.md` 放进仓库根目录，或直接让 Agent 读取该文件。适合不支持 MCP 的客户端；用 `--project` / `--limit` 控制摘要大小。

### 9. 检索、关联与健康检查（只读）

```bash
./devlog-learn.exe search "timer localStorage" --project test-1
./devlog-learn.exe search "render fail" --review approved --tag go --json
./devlog-learn.exe projects --json
./devlog-learn.exe status --json
./devlog-learn.exe related <doc-id> --json
./devlog-learn.exe questions --status all --json
```

- `search` 是词法检索（ASCII 词 + 中文 bigram），不依赖 embedding；空 query 返回最近文档。
- `--tag` 可重复，要求全部命中。
- `--json` 适合脚本/CI 集成。
- `status` 只读检查缓存、cursor、索引/AGENTS.md 新鲜度和 Provider 探测，不会创建缓存。

### 10. 定时增量更新

建议先写一个包装脚本，避免计划任务里的引号和 PATH 问题：

```bat
:: devlog-daily.cmd
@echo off
cd /d D:\software\traceback\cli
devlog-learn.exe run --provider all --since 1d --segments 0 >> .devlog-daily.log 2>&1
```

```bat
:: 每天 09:00 执行
schtasks /Create /SC DAILY /TN devlog-learn /TR "D:\software\traceback\cli\devlog-daily.cmd" /ST 09:00
```

Linux / macOS：

```cron
0 9 * * * cd /path/to/project && /path/to/devlog-learn run --provider all --since 1d --segments 0 >> /tmp/devlog-learn.log 2>&1
```

注意：

- 计划任务的环境变量/PATH 可能不含 `claude` / `codex`；用 `--claude-bin` / `--codex-bin` 指定绝对路径。
- 费用控制：先用 `--segments 1` 或 `dry-run` 验证；缓存命中不会调用模型。
- `run` 会持有 `.devlog.lock`；与 Web UI 的审核/导出并发时会返回 409 或报错，这是有意的保护。
- 增量由文件 SHA-256 cursor 决定；`--since` 控制读取范围，二者不冲突。

### 11. 多个仓库 / 多份缓存隔离

```bash
./devlog-learn.exe run \
  --cache-dir D:/devlog-cache/project-a \
  --out D:/devlog-docs/project-a \
  --provider all --since 7d
```

或在项目根目录放 `.devlog-learn.json`：

```json
{
  "cache_dir": ".devlog-cache",
  "out": ".devlog-docs",
  "provider": "all",
  "since": "7d"
}
```

- 每个仓库独立缓存、cursor 和输出，互不污染。
- 也可以共享一份缓存，用 MCP / `digest` 的 `--project` 过滤。
- 配置优先级：**显式 flag > 环境变量 > 项目配置 > 用户配置 > 内置默认值**。详见 [配置](#配置)。

### 12. 调试与开发

```bash
# 看解析/丢弃/事件预览
./devlog-learn.exe dry-run --provider all --since 7d --show-drops --show-events
./devlog-learn.exe ingest --provider all --since 7d --json --show-events

# 更详细的日志
./devlog-learn.exe -v extract --provider all --since 7d --segments 1

# 保留失败响应（已脱敏）
./devlog-learn.exe extract --provider all --segments 1 --keep-raw

# 测试与前端开发
go vet ./...
go test ./... -count=1
cd web && npm run dev     # 另开终端：./devlog-learn.exe serve --no-open
```

### 13. 成本与隐私速记

- 只有 `extract`、`run`、`wizard`、`experience synthesize` 和 Web UI 的“提炼”会调用模型；其余命令（包括 `experience show/render/list/checklist`）都是只读或确定性输出。
- 默认一次只提炼 1 个 Segment；`--force` 会绕过缓存，谨慎使用。
- 会话日志在本机解析；Web UI 只绑定回环地址并要求进程级 token；没有遥测。
- 脱敏是尽力而为；不要把 `.devlog-cache/`、`.devlog-docs/`、`.devlog-cursor.json` 提交到公开仓库。
- 详细边界见 [隐私与安全](#隐私与安全) 和 [`docs/PRIVACY.md`](../docs/PRIVACY.md)。

### 14. 项目经历 / 实习经验与复习清单

把同一项目目录下已经提炼成 Doc 的会话聚合为一份项目级经历：

```bash
# 只读：先看有哪些项目、当前类型和建议类型
./devlog-learn.exe experience list

# 人工确认类型与展示信息（写入 <cache-dir>/experiences.json）
./devlog-learn.exe experience set --project my-app --kind internship \
  --org ACME --role "Backend Intern" --start 2025-07 --end 2025-09

# 查看 Markdown 或只打印复习清单
./devlog-learn.exe experience show --project my-app
./devlog-learn.exe experience checklist --project my-app

# 写出 <out>/<project>/EXPERIENCE.md 与 <out>/EXPERIENCE.md
./devlog-learn.exe experience render --out .devlog-docs
```

- 类型只做**建议**（项目名/目录包含 `intern`、`internship`、`placement` 或 `实习`），最终以 `set` 为准；也可以直接用 Web UI 的“项目经历”页阅读。
- 经历只聚合已经提炼的 Doc；没有文档时先运行 `extract` / `run`。
- Markdown 里的文档链接指向 agent profile 的 `*.md`；如果还没生成，先运行 `render` 或 `run`。
- 复习清单是静态 `- [ ]` 列表，不记录完成状态，也不安排间隔重复。
- 默认不调用模型。只有显式执行 `experience synthesize --project my-app` 才会把已脱敏的 Doc 摘要交给本地 `claude` / `codex` CLI，结果缓存并在 Markdown 中标注为“模型补充（未验证）”。

---

## 本地 Web UI

### 启动

```bash
./devlog-learn.exe serve
```

默认：

- 监听 `127.0.0.1:8787`
- Windows 下自动用 Edge/Chrome 的 `--app=` 应用窗口打开；其他平台回退默认浏览器
- 打印带进程级 token 的访问 URL
- 首次访问后 token 写入 HttpOnly + SameSite=Strict cookie
- 前端资源全部内嵌在二进制里，不需要 Node，也不加载外部 CDN

常用参数：

```bash
./devlog-learn.exe serve --no-open              # 不自动打开窗口
./devlog-learn.exe serve --addr 127.0.0.1:0     # 随机端口
./devlog-learn.exe serve --addr 127.0.0.1:9000
./devlog-learn.exe serve --cache-dir D:/devlog-cache --out D:/devlog-docs
./devlog-learn.exe serve --claude-dir D:/claude/projects --codex-dir D:/codex/sessions
```

### 页面

| 页面 | 作用 |
|---|---|
| **概览** | 文档/待审核/未解决问题/缓存大小、Provider 探测、输出新鲜度、最近文档 |
| **文档库** | 中文搜索；项目、审核状态、知识类型、解决状态、学习价值、生命周期、标签筛选；徽章与标签均可点击 |
| **文档详情** | 学习视图、Agent Markdown、原始 JSON；证据抽屉；审核；相关问题；相关文档 |
| **项目经历** | 按项目聚合的「项目经历/实习经验」、关键工作、难点、结果、STAR 故事、静态复习清单、文档索引；只读，不调用模型 |
| **提炼** | 选择来源/时间 → 按会话或单个 Segment 勾选 → 确认 → 实时进度 → 取消 / 重试失败片段 |
| **待办问题** | 跨文档聚合未解决问题，解决状态保存在 `<cache-dir>/questions.json` |
| **设置与导出** | 本地路径、Provider 状态、输出新鲜度、生成 Markdown/索引/AGENTS.md/EXPERIENCE.md |

### 学习视图

文档详情默认展示结构化学习视图：

- 问题与症状
- 定位路径（观察 → 假设 → 动作 → 命令 → 结果 → 结论）
- 被排除的假设
- 根因
- 修复步骤
- 验证
- 有效命令
- 可迁移经验
- 常见坑
- 知识卡（概念 + 最小示例）
- 盲区
- 证据来源
- 不确定项

所有 Markdown 都由 Go 侧 `render.Markdown` / `render.LearnMarkdown` 生成，前端只负责展示，避免两套模板漂移。

### 提炼流程与成本保护

1. 前端调用 `/api/sessions` 扫描本地日志（不调用模型），列出会话与 Segment。
2. 用户可以勾选整个会话，也可以展开后只勾选单个 Segment；界面显示已选 Segment 数和已提炼数量。
3. 点击“开始提炼”后弹出确认框，明确提示“未命中缓存的 Segment 才会调用本地模型”。
4. 服务端启动 job，通过 SSE 推送 `scan`、`selected`、每个 `segment` 以及最终 `done/failed/canceled`。
5. 支持取消；失败后可按失败 Segment 重试。
6. SSE 断开时自动降级为 2 秒轮询，不会冻结进度。

服务端保护：

- 必须显式选择至少一个 session/segment；空选择返回 `400`。
- 同一进程同一时间只允许一个提炼 job。
- 默认并发 `2`，单次超时 `180s`；API 限制并发 `1–8`、超时 `30–900s`。
- 显式选择会禁用 cursor 跳过以匹配候选，但缓存仍然复用。
- CLI 与 Web UI 共用 `<cache-dir>/.devlog.lock` 跨进程写锁；缓存被另一进程占用时返回 `409`，不会并发写 review、questions、cursor 或 docs。

### 安全边界

- 只允许绑定回环地址；`--addr` 非 loopback 会直接拒绝。
- API 需要 token；写操作校验 `Origin`。
- 返回 CSP、`X-Frame-Options: DENY`、`nosniff`、`Referrer-Policy: no-referrer`。
- 前端不引用外部字体、脚本或图片。
- token 只用于本机进程；不要转发启动时打印的完整 URL。

### 进程与任务生命周期

- 关闭窗口只停止界面，不删除 cache/docs。
- 提炼任务运行在 `serve` 进程内；`Ctrl+C` 会停止接收新请求、取消当前模型调用、等待正在退出的 goroutine，并保留已完成的 Doc。
- job 历史只在内存里；重启后从 `/api/jobs/current` 取不到旧任务，但 docs 不受影响。

---

## CLI 工作流

### 1. 先看有什么

```bash
./devlog-learn.exe ingest --provider all --since 7d --limit 2
./devlog-learn.exe sessions --provider all --since 7d
./devlog-learn.exe dry-run --provider all --since 7d --limit 2 --show-drops
```

这三个命令都不会调用模型。

### 2. 小步提炼

```bash
# 默认只提炼 1 个 Segment，避免一次产生大量模型调用
./devlog-learn.exe extract --provider all --since 7d --segments 1

# 明确选择整个会话
./devlog-learn.exe extract --session claude-code:<session-id>

# 明确选择单个 Segment
./devlog-learn.exe extract --segment <segment-id>
```

### 3. 生成产物

```bash
./devlog-learn.exe render --cache-dir .devlog-cache --out .devlog-docs
./devlog-learn.exe render --profile learning --cache-dir .devlog-cache --out .devlog-docs
./devlog-learn.exe index --cache-dir .devlog-cache --out .devlog-docs
./devlog-learn.exe digest --cache-dir .devlog-cache --out .devlog-docs
```

`run` 已经包含 `extract → render → index`：

```bash
./devlog-learn.exe run --provider all --since 7d --segments 1
```

### 4. 人工闭环

```bash
./devlog-learn.exe review --list --status pending
./devlog-learn.exe review <doc-id> --status approved --note "verified locally"
./devlog-learn.exe questions
./devlog-learn.exe questions resolve <question-id> --note "fixed in v0.12"
./devlog-learn.exe related <doc-id> --json
```

### 5. 给 Agent 使用

```bash
./devlog-learn.exe digest --out .devlog-docs
./devlog-learn.exe mcp --cache-dir .devlog-cache
```

---

## 命令参考

### `serve`

启动本地 Web UI。

```bash
./devlog-learn.exe serve
```

| 参数 | 默认值 | 说明 |
|---|---|---|
| `--addr` | `127.0.0.1:8787` | 只允许 loopback；`127.0.0.1:0` 使用随机端口 |
| `--cache-dir` | `.devlog-cache` | Doc/缓存目录 |
| `--cursor` | `.devlog-cursor.json` | 增量游标 |
| `--out` | `.devlog-docs` | Markdown/索引/AGENTS.md 输出目录 |
| `--claude-dir` | `~/.claude/projects` | Claude Code 会话目录 |
| `--codex-dir` | `~/.codex/sessions` | Codex 会话目录 |
| `--claude-bin` | `claude` | Claude Code 可执行文件 |
| `--codex-bin` | `codex` | Codex 可执行文件 |
| `--no-open` | `false` | 不自动打开浏览器窗口 |

### `ingest`

只做发现/解析诊断，不调用模型、不写缓存。支持 `--json`、`--show-events`。

```bash
./devlog-learn.exe ingest --provider all --since 7d --limit 2 --show-events
```

### `dry-run`

本地执行“解析 → 切分 → 脱敏 → Prompt 预算预览”，不读写 cursor/cache。

```bash
./devlog-learn.exe dry-run --show-drops --show-events
```

### `sessions`

不调用模型，列出可选择的会话与 Segment：

```bash
./devlog-learn.exe sessions --provider all --since 7d
./devlog-learn.exe sessions --provider all --since 7d --json
```

输出包含：

- `selector`：`<source>:<session-id>`，传给 `--session`
- 每个 Segment 的 `segment-id`，传给 `--segment`
- `doc=yes/no`：该 Segment 是否已有规范 Doc（stale 也算已有）

`sessions` 只检查 `docs/<segment-id>.json` 是否存在，不是 cache-key 精确预测；实际是否调用模型仍由运行时缓存决定。

### `extract`

调用本机 `claude` / `codex` CLI 提炼 Doc。

| 模式 | 扫描 | Cursor 跳过 | Cache 读取 |
|---|---|---|---|
| 默认 | 只处理变更文件 | 使用 | 命中可用 |
| `--full` | 全量扫描 | 忽略 | 命中可用 |
| `--force` | 全量扫描（隐含 `--full`） | 忽略 | 忽略，强制调模型 |

关键语义：

- `--segments N`：最多选 N 个 Segment；`0` 表示全部；默认 `1`。
- `--session <selector>`：选择整个会话，可重复。
- `--segment <id>`：选择单个 Segment，可重复。
- 显式选择时忽略 `--segments`，并禁用 cursor 跳过以保证能匹配候选；缓存仍复用。
- 显式选择只更新被选中文件的 cursor；未选中文件保持原状态。
- 选中文件仍有未选 Segment 时保持 `status=error`，下次可继续处理。
- `--since`：`7d`、`2w`、`36h`、RFC3339 或 `all`；默认 `7d`。
- 未指定 `--out` 时，本次 Doc JSON 数组写到 stdout；指定后写 `<out>/<doc-id>.json`。
- 增量跳过的文件不会重复输出旧 Doc；规范结果始终在 `<cache-dir>/docs/`。
- 每个真实模型调用在 stderr 打印 `usage:`；结束打印 `tokens:` 汇总。
- `--keep-raw` 才保存失败模型响应；保存前已脱敏。
- `--provider all` 时按 Segment 来源路由；一个 Provider 失败不会阻止另一个成功结果。
- `--claude-bin` / `--codex-bin` 可覆盖可执行文件。

### `run`

```bash
./devlog-learn.exe run --provider claude-code --since 7d --segments 1
./devlog-learn.exe run --session claude-code:<id>
./devlog-learn.exe run --segment <segment-id>
```

- 默认 `--out .devlog-docs`，完成 `extract → render → index`。
- 默认只提炼 1 个新 Segment；`--segments 0` 表示全部。
- 即使本次没有新 Segment，也会重新渲染 active Doc 并重建索引。
- 与 `extract` 一样，stderr 输出 `usage:` / `tokens:`。

### `wizard`

```bash
./devlog-learn.exe wizard --provider all --since 7d
```

- 编号多选：`1,3`、`1-3`、`all`。
- 显示预计模型调用次数（按已有 Doc 估算）。
- 确认后执行 `run`。
- `--yes` 跳过确认；非交互环境请用 `sessions` + `extract --session`。

### `render`

```bash
./devlog-learn.exe render --cache-dir .devlog-cache --out .devlog-docs
./devlog-learn.exe render --profile learning --include-stale
./devlog-learn.exe render --prune
```

- 默认 `--profile agent`：`<out>/<project>/<slug>.md`，含 YAML front matter。`slug` 通常已带日期前缀，例如 `2026-11-12-fix-cache-eviction`。
- `--profile learning`：写入 `*.learn.md`，完整中文学习模板。
- 学习模板顺序：症状 → 先自己想 → 定位路径（C1..Cn 命令）→ 被排除的假设 → 根因 → 修复 → 验证 → 可迁移方法 → 知识点卡片 → 盲区 → 下次检查清单 → 案例内自测题 → 证据来源。
- 自测题只使用 `<details><summary>答案</summary>` 折叠答案；不记录掌握度，不做间隔复习。
- `*.learn.md` 与 Agent 版 `.md` 互不覆盖；旧 Doc 没有学习字段时不会输出空节。
- 默认只渲染 active Doc；`--include-stale` 才渲染 stale。
- 默认不删除旧 Markdown；`--prune` 只删除同一 profile 在 `.devlog-manifest.json` 中登记、且内容 hash 未变化的旧文件。
- 未登记文件、用户修改过的生成文件以及其他 profile 的输出都不会删除；同名路径由统一规划消歧。
- Doc 含 `diagnosis[]` / `commands[]` 时，Agent 版额外输出 **Diagnosis Path** 和 **Effective Commands**。

### `index`

- 输出 `<out>/index.json`、`<out>/README.md` 以及每个项目的索引。
- 只索引 active Doc；stale 保留在缓存但不进入索引。
- `related` 只读计算并写入索引，不合并、不改写 Doc。
- 先 `render` 再 `index`，链接才会指向真实文件。
- `index --prune` 使用同一 manifest 清理不再生成的项目索引；manifest 不可信或任一索引写入失败时禁用清理。

### `search`

```bash
./devlog-learn.exe search "timer localStorage" --project test-1
./devlog-learn.exe search "render fail" --review approved --tag go
./devlog-learn.exe search "" --json --limit 5
```

- 空 query 返回最近文档。
- `--tag` 可重复，要求全部命中（大小写不敏感）。
- 只检索 active Doc。
- 参与评分的字段包括标题、摘要、tags、`problem`、`root_cause`、`diagnosis`、`commands`、`concepts`、`rejected_hypotheses`、`solution`、`insights`、`lessons`。
- 文本输出为 `score / id / project / review / evidence / title`，下一行是 Markdown 路径。

### `projects`

```bash
./devlog-learn.exe projects
./devlog-learn.exe projects --json
```

按项目汇总 active Doc 与 review 计数。

### `experience`

```bash
./devlog-learn.exe experience                 # 等价于 experience list
./devlog-learn.exe experience list --json
./devlog-learn.exe experience show --project my-app
./devlog-learn.exe experience set --project my-app --kind internship --org ACME --role "Backend Intern"
./devlog-learn.exe experience unset --project my-app
./devlog-learn.exe experience checklist --project my-app
./devlog-learn.exe experience render --out .devlog-docs --prune
./devlog-learn.exe experience synthesize --project my-app   # 唯一会调用模型的 experience 子命令
```

- `list` / `show` / `checklist` 只读，不会创建 cache 或调用模型。
- `render` 是确定性输出，只写 `EXPERIENCE.md`（profile=`experience`），不会覆盖 agent/learning/index 的文件。
- 人工类型与组织/角色/时间保存在 `<cache-dir>/experiences.json`；`set`/`unset`/`render`/`synthesize` 会获取缓存写锁。
- `synthesize` 只发送 Doc 摘要（title/summary/root cause/lessons），不发送原始会话或证据全文；输出缓存在 `<cache-dir>/experience-synthesis/`，渲染时标注“未验证”。

### `status`

```bash
./devlog-learn.exe status
./devlog-learn.exe status --json
```

只读报告：缓存大小、active/stale、review、未解决问题、cursor、索引/AGENTS.md/EXPERIENCE.md 新鲜度、Provider 二进制与目录。不会创建 cache，不调用模型。

### `questions`

```bash
./devlog-learn.exe questions
./devlog-learn.exe questions --project cli --status all
./devlog-learn.exe questions resolve <question-id> --note "fixed in v0.12"
./devlog-learn.exe questions reopen <question-id>
```

- 状态保存在 `<cache-dir>/questions.json`，不修改 Doc JSON。
- 问题 ID 由 `Doc ID + 规范化问题文本` 计算；重新提炼后同一问题仍保持状态。
- `digest` 会自动省略已解决的问题。
- stale Doc 的问题不会出现在列表中。

### `review`

```bash
./devlog-learn.exe review <doc-id> --status approved --note "verified locally"
./devlog-learn.exe review --list --status pending
```

- `pending` / `approved` / `rejected`。
- 只改写 `docs/<id>.json`，不删除文件。
- 重新提炼时：`SegmentHash` 相同保留人工状态；内容变化则重置为 `pending` + 空备注。

### `related`

```bash
./devlog-learn.exe related <doc-id> --json
```

按共同 tag、stack、knowledge_type、project 打分，最多 5 条；stale 排除。

### `digest`

```bash
./devlog-learn.exe digest --cache-dir .devlog-cache --out .devlog-docs
./devlog-learn.exe digest --review approved --limit 10
./devlog-learn.exe digest --project cli
```

- 只包含 active Doc。
- 默认 `--review any`，表里标注 review；需要人工确认过的内容时用 `--review approved`。
- 包含标题、项目、tags、confidence、证据 `verified/checked`、review、Markdown 路径与 Open Questions。
- 默认拒绝覆盖不是本工具生成的 `AGENTS.md`；需要覆盖时加 `--force`。
- CLI 与 Web UI 共用 `internal/digest`，输出格式一致。

### `mcp`

```bash
./devlog-learn.exe mcp --cache-dir .devlog-cache
```

工具：

| 工具 | 作用 |
|---|---|
| `search_docs` | 按关键词、错误签名、project、tags、review 检索 |
| `get_doc` | 按 id 取完整 Markdown 或 JSON |
| `related_docs` | 复用 related 评分列出关联文档 |
| `list_projects` | 列出项目与 review 计数 |

- 只读：不提炼、不写 cache、不调用模型。
- 只服务 active Doc。
- `--project ""` 表示全部项目；`--project auto` 使用 server 进程 cwd 的 basename。
- stdout 只输出 JSON-RPC；日志在 stderr。
- 检索是词法评分（ASCII 词 + 中文 bigram），不依赖 embedding。

Claude Code：

```bash
claude mcp add devlog-learn -- devlog-learn mcp --cache-dir .devlog-cache
```

Codex（`~/.codex/config.toml`）：

```toml
[mcp_servers.devlog-learn]
command = "devlog-learn"
args = ["mcp", "--cache-dir", ".devlog-cache"]
```

Agent 使用约定：把 Doc 当历史记忆而非当前事实；优先 `approved` 与高证据比例的文档；行动前用当前代码/命令输出复核。

---

## 缓存与增量语义

```text
.devlog-cache/
  .devlog.lock                 # 跨进程写锁（运行时文件）
  entries/<cache-key>.json     # 提炼缓存（版本化 + 校验和）
  docs/<doc-id>.json           # 当前规范 Doc（含 review/lifecycle）
  segments/<segment-id>.json   # 脱敏后的中间 Segment
  raw/<segment-id>-<ts>.txt    # 仅 --keep-raw
  questions.json               # open questions 解决状态 sidecar

.devlog-cursor.json            # 文件级 SHA-256 指纹与 SegmentID 关联
.devlog-docs/
  .devlog-manifest.json        # 本工具生成文件的归属与内容 hash
  ...                          # render/index/digest 输出
```

- **SHA-256 是权威**：同大小改写、恢复 mtime 的改写、截断都会重新解析。
- Parser/Segmenter 版本变化会让所有文件视为 changed。
- `status=error` 的文件下次自动重试；只有全部预期 Segment 成功才写 `ok`。
- Cache key 包含 `SegmentID` 与 `SegmentHash`，不同会话的同内容 Segment 不会碰撞。
- Cache 命中只在该 Doc 文件缺失时写入，不会覆盖人工 review。
- `--force` 成功后仍刷新 cache entry 与 docs。
- stale Doc 永不物理删除；内容恢复后会自动回到 active。
- 文件被删除时，cursor 中记录的 Segment 对应 Doc 标记 `source_missing`，仍不删除。
- `docs/` 是规范结果；stdout/`--out` 只包含本次实际处理的 Doc。

### 重新提炼与人工状态

- `SegmentHash` 不变：保留旧 `review_status` / `review_note`。
- `SegmentHash` 变化：重置为 `pending` + 空备注，避免旧结论套到新内容。
- 缓存命中路径不覆盖已有 `docs/`，因此人工状态自然保留。

---

## 配置

优先级：**显式 flag > 环境变量 > 项目配置 > 用户配置 > 内置默认值**。

配置文件是 JSON，未知字段会报错，避免拼写错误被静默忽略：

- 用户级：`<UserConfigDir>/devlog-learn/config.json`
  - Windows 通常是 `%AppData%\devlog-learn\config.json`
  - Linux 通常是 `~/.config/devlog-learn/config.json`
- 项目级：当前目录的 `.devlog-learn.json`

```json
{
  "cache_dir": ".devlog-cache",
  "out": ".devlog-docs",
  "provider": "all",
  "since": "7d",
  "model": "your-model",
  "concurrency": 2,
  "claude_bin": "claude",
  "codex_bin": "codex",
  "claude_dir": "C:/Users/me/.claude/projects",
  "codex_dir": "C:/Users/me/.codex/sessions",
  "redact_config": "C:/path/to/redact.json"
}
```

环境变量：

`DEVLOG_LEARN_CACHE_DIR`、`DEVLOG_LEARN_CURSOR`、`DEVLOG_LEARN_OUT`、`DEVLOG_LEARN_PROVIDER`、`DEVLOG_LEARN_SINCE`、`DEVLOG_LEARN_MODEL`、`DEVLOG_LEARN_CLAUDE_BIN`、`DEVLOG_LEARN_CODEX_BIN`、`DEVLOG_LEARN_CLAUDE_DIR`、`DEVLOG_LEARN_CODEX_DIR`、`DEVLOG_LEARN_REDACT_CONFIG`、`DEVLOG_LEARN_CONCURRENCY`。

注意：`out` 不会应用到 `extract`。在 `extract` 中，`--out` 表示“把 Doc JSON 写到目录而不是 stdout”；在 `render` / `index` / `digest` / `serve` 中，它表示 Markdown/索引输出目录。

---

## Doc 与 RAG 契约

### 稳定字段

- `schema_version`：只做加法；新增字段安全，删除/重命名会破坏下游。
- `id`：由 Segment 身份决定；追加消息只改变 `SegmentHash`，不改变 `id`。
- 每条 Doc 自包含：
  - `knowledge_type`、`tags`、`stack`
  - `evidence[]`：`event_seq` / `kind` / `quote` / `verified`
  - `provenance`：provider / model / cli_version / segment_id / segment_hash / prompt_version / prompt_hash / 版本号 / extracted_at

### 学习案例字段（可选、加法）

- `diagnosis[]`：定位路径
  - observation / hypothesis / action / command / command_kind / expected_signal / result / conclusion
- `commands[]`：有效命令
  - purpose / command / kind / expected_signal / risk
- `concepts[]`：知识点卡片
  - name / stack / why_relevant / syntax / example / docs_url / evidence
  - `example` 必须是可运行的最小示例，不接受只写概念名。
- `rejected_hypotheses[]`：被排除的假设
  - hypothesis / why_rejected / evidence
- `blind_spots[]`：盲区
  - point / kind / why_it_matters / evidence
  - `kind` 只允许 `general_method`、`framework_knowledge`、`project_context`
- `learning_value`：`low` / `medium` / `high`

新字段全部可选；旧 Doc 反序列化、渲染和检索都不受影响。证据引用与 `diagnosis` / `commands` 走同一套 `ValidateEvidence` 过滤：非法 seq 会被剔除，缺证据只降级文档，不会让文档失败。

`schema_version` 当前为 `1`（`version.SchemaVersion`）。这些字段由 `internal/extract/contract_test.go` 守护；修改 Schema 时必须同步更新测试与本文档。

### 证据规则（摘要）

- evidence 的 seq 必须存在于 Segment，且必须在 Prompt 实际包含的 `IncludedSeqs` 中。
- quote 必须能在事件文本中匹配；不匹配保留但 `verified=false`。
- `Edit` / `Write` 等多行代码引用会先解码 `ToolInput` 的 JSON 字符串再逐字段匹配，不跨字段拼接。
- `root_cause.known=yes` 需要至少一条 verified 证据。
- verification passed 需要 verified 事件且 `Exec.Known && exit 0`。
- 强结论缺证据会被降级并写 uncertainty；置信度上限降低。
- Claude 日志通常没有退出码，`is_error=false` 不算成功；这是有意的防编造策略。

---

## Agent 接入

两条路，按环境选择：

### 1. AGENTS.md（零基础设施）

```bash
./devlog-learn.exe digest --out .devlog-docs
```

任何能读文件的 Agent 都可以直接使用 `.devlog-docs/AGENTS.md`。Web UI 的“设置与导出”也能同时生成它和项目经历 `EXPERIENCE.md`。

### 2. MCP（按需检索）

适合 Claude Code / Codex 等支持 MCP 的客户端。配置示例见 [mcp](#mcp)。

只读工具：`search_docs`、`get_doc`、`related_docs`、`list_projects`。

### 推荐使用习惯

- 优先 `approved` 文档。
- 优先证据比例高的文档。
- 把 Doc 当“历史记忆”，不是当前事实。
- 执行前用当前代码、测试或命令输出复核。
- 定期用 `questions` 清理未解决问题，并把结论写回代码/文档。

---

## 隐私与安全

- 内置脱敏覆盖：PEM、JWT、Bearer、品牌 token、认证头、凭证赋值、URL 凭证、邮箱、手机号、IPv4、长 Hex、Windows/Unix home 路径。
- `--redact-config` 可禁用规则、添加自定义规则、调整 Hex 阈值、私网 IP/邮箱/手机/IP 开关。
- ToolInput/Raw 为 JSON 时按结构递归脱敏；缓存中的 Segment 也会脱敏 `File`/`Cwd`。
- 模型回显内容会做防御性二次脱敏；程序生成的 provenance 哈希会被恢复，不会当作长 Hex 替换。
- Web UI 只绑 loopback；API 需要进程级 token；写操作校验同源；CSP 禁止外部资源。
- 本工具没有遥测或自建云服务；提炼只通过本机 `claude` / `codex` CLI 发起。该 CLI 是否向模型 Provider 发送 prompt，取决于其配置和账户策略。
- 真实模型调用会产生费用；所有自动测试使用 fake Client / fake `.cmd`。

---

## Windows 说明

- `.cmd` / `.bat` shim 通过 PowerShell 执行；参数以 JSON 环境变量传入并 splat，中文、空格、`&` 等路径不会进入命令行字符串。
- 无扩展名的可执行文件按 `.exe` → `.cmd` → `.bat` 解析。
- 输出路径内部使用前向斜杠，写盘时转换为平台分隔符。
- 项目名/文件名会清洗 Windows 保留名（`CON`、`NUL`、`COM1` 等）。
- Windows 下 Web UI 默认使用 Edge/Chrome 的 `--app=` 应用窗口；未找到时回退默认浏览器。
- 从开始菜单/图形界面启动时，`claude` / `codex` 可能不在 PATH；可在配置或 `serve` 参数中显式指定路径，并在“设置”里看探测结果。

---

## 性能与资源

- 解析器按行流式读取，单行上限 64 MiB；坏行不中断文件。
- Prompt 默认上限 120000 字节；超预算会逐级压缩字段，仍超则整条省略事件，并通过 `IncludedSeqs` 阻止模型引用被省略的 seq。
- `--concurrency` 默认 2；结果输出顺序恒等于发现顺序 + Segment 顺序。
- `sessions` / Web UI 的会话扫描会解析日志但不调用模型；首次扫描可能需要数秒到数十秒。
- 可运行基准：

```bash
go test ./internal/ingest ./internal/segment ./internal/render ./internal/index \
  -run '^$' -bench . -benchtime=1x
```

---

## 故障排查

| 现象 | 处理 |
|---|---|
| `unsupported provider "cursor"` | 不支持 Cursor；用 `claude-code`、`codex` 或 `all`。 |
| `provider ... directory ...` | 显式传入的 `--claude-dir` / `--codex-dir` 不存在；检查路径。 |
| Web UI 打不开 / 端口占用 | 换端口：`serve --addr 127.0.0.1:0`，或关闭旧 `devlog-learn.exe` 进程。 |
| Web UI 返回 401 | token 失效或 URL 不完整；重启 `serve`，使用新打印的完整 URL。 |
| Web UI 点提炼提示 provider 找不到 | 图形界面 PATH 可能不含 `claude`/`codex`；用 `--claude-bin` / `--codex-bin` 显式指定。 |
| 会话列表为空 | 切换 `--since`（如 `all`）、检查 Provider 会话目录；CLI 用 `sessions --json` 对照。 |
| 模型输出无法解析 | 会自动 repair 一次；仍失败可加 `--keep-raw` 查看脱敏后的原始响应。 |
| 文档没有生成 | 检查 `--segments`、`--since`、drop 原因（`dry-run --show-drops`）与 cursor 状态。 |
| 索引链接失效 | 先运行 `render`，再运行 `index`。 |
| `AGENTS.md` 没有被覆盖 | 它可能是手工维护的；换输出目录，或确认后加 `--force`。 |
| 审核状态丢失 | 只有 `SegmentHash` 不变才保留；内容变化会按设计重置。 |
| 提炼失败想继续 | Web UI 点“重试失败片段”；CLI 用失败 Segment ID 重新 `extract --segment`。 |

---

## 项目结构

```text
cli/
  cmd/                  CLI 子命令（含 serve）
  internal/
    ingest/             Claude Code / Codex 解析
    segment/            切分、SegmentID、Hash
    redact/             脱敏规则与二次脱敏
    pipeline/           Prepare、增量提炼、进度回调、Selection
    cache/              缓存、docs、segments
    cursor/             SHA-256 文件指纹
    extract/            Doc Schema、Prompt、LLM Client、证据校验
    render/             Agent/学习版/经历 Markdown 与路径规划
    index/              索引与 related
    export/             render + index + experience 的共享导出层
    search/             词法检索与项目统计
    experience/         项目经历聚合、类型 sidecar、静态复习清单、可选合成
    questions/          人工问题解决状态 sidecar
    digest/             AGENTS.md 摘要（CLI 与 Web UI 共用）
    manifest/           输出文件归属、hash 与安全 prune
    webapi/             Web UI HTTP API、SSE job、token/同源校验、内嵌 dist
  web/                  React + Vite + TypeScript 前端源码
  start-webui.cmd       Windows 双击启动入口
```

数据流：

```text
Claude Code / Codex 日志
        │
        ▼
ingest → segment → redact → extract（本地 CLI）
                              │
                              ▼
                        cache/docs
                       /     |      \
                 render    index    digest
                    │        │        │
                    ▼        ▼        ▼
              *.md/*.learn.md  index  AGENTS.md
                       \       |      /
                        Web UI / MCP
```

---

## 开发与测试

仓库级开发约定见 [`../CONTRIBUTING.md`](../CONTRIBUTING.md)，架构与信任边界见
[`../docs/ARCHITECTURE.md`](../docs/ARCHITECTURE.md)，漏洞报告方式见
[`../SECURITY.md`](../SECURITY.md)。

```bash
cd cli

gofmt -l .
go vet ./...
go test ./... -count=1
go test ./... -race -count=1

go build -o devlog-learn.exe ./cmd
./devlog-learn.exe --version
```

### 重新构建 Web UI

前端源码在 `cli/web/`，构建产物写入 `internal/webapi/dist/` 并嵌入二进制：

```bash
cd cli/web
npm install
npm run build          # tsc --noEmit && vite build
cd ..
go build -o devlog-learn.exe ./cmd
```

已有构建产物时，只跑 `go build` 即可。开发模式：

```bash
# 终端 1
./devlog-learn.exe serve --no-open

# 终端 2
cd web
npm run dev
# 浏览器访问 http://localhost:5173/?token=<serve 打印的 token>
```

Vite 只代理 `/api`；token 由前端从 URL 取出后通过 `X-Devlog-Token` 发送。

### 测试约定

- 自动测试从不调用真实模型。
- 模型边界使用 fake Client；Windows `.cmd` 使用 fake shim。
- `internal/webapi` 覆盖 token/Host/Origin、文档、review、questions、export、sessions、fake-client 提炼 job、进度/取消/忙锁。
- 真实模型冒烟需要用户明确同意费用。

> Windows Defender/文件扫描可能让单个包的首轮测试耗时约 30 秒，只要最终 `ok` 即正常。

---

## 已知限制与非目标

- **不支持 Cursor IDE**；没有 Cursor parser 或测试数据。
- 检索是词法匹配（ASCII 词 + 中文 bigram），不做语义/向量检索。
- 本地 Web UI 是单用户、loopback-only；没有登录、远程访问或多用户隔离。
- 没有原生安装包/托盘/自动更新；当前用 Edge/Chrome 应用窗口，后续可考虑 Wails/Tauri。
- 同一 `serve` 进程只允许一个提炼 job；job 历史只在内存里。
- 显式 `--session` / `--segment` 会为匹配候选重新解析所有被发现文件；缓存仍复用，但大日志首次选择可能较慢。
- Codex 的 `Provenance.Model` 在未显式指定时记为 `default`。
- Claude 日志通常没有退出码，verification 常为 `unknown`（有意的防编造策略）。
- 证据 quote 是严格子串匹配；模型改写/翻译会导致 `verified=false`。
- `index` 不生成 Markdown；`render` 默认不删除旧 Markdown，只有 `--prune` 才清理 manifest 中未再生成且未手改的文件。
- 两个不同项目名清洗后若落到同一目录，会合并到同一项目索引。
- `mcp` / `digest` / `search` / `projects` / `status` 是只读视图：不会触发提炼，也不会替你审核文档。
- `questions` 的状态在 `<cache-dir>/questions.json`；删除整个缓存目录会同时丢失 Doc 和问题状态。
- 不做掌握度/间隔复习调度；学习模板的自测题只折叠答案，`experience` 生成的是静态复习清单，不记录完成状态、不安排间隔重复。
- `experience` 只聚合已经提炼成 Doc 的会话，不直接读取未提炼的原始会话；也不会自动推断组织/职位（只给类型建议，最终由人工 `set`）。
- `status` 只统计本地文件，不解析会话日志；需要未提炼 Segment 明细时用 `sessions`。
