# devlog-learn

把本机 **Claude Code** 与 **Codex** 的历史会话日志，解析、切分、脱敏，提炼成结构化知识文档。

同一份 Doc 可以变成：

- **本地 Web UI**：阅读、搜索、证据回看、人工审核、问题闭环、选择会话提炼。
- **Agent 记忆**：稳定的 Markdown、全局/项目索引、`AGENTS.md`、只读 MCP Server。
- **中文学习案例**：定位路径、被排除的假设、有效命令、知识卡、盲区、自测题。
- **可追溯证据**：每条结论引用原始事件 seq，可回看当时的命令、输出和退出码。

当前版本：`0.13.0`。支持 `claude-code` 与 `codex`，**不支持 Cursor IDE**。

## 快速开始

```bash
cd cli
go build -o devlog-learn.exe ./cmd
./devlog-learn.exe serve
```

Windows 下也可以直接双击 [`cli/start-webui.cmd`](cli/start-webui.cmd)：启动服务并自动打开应用窗口。

默认路径：

- 缓存：`.devlog-cache`
- 输出：`.devlog-docs`
- Web UI：`127.0.0.1:8787`

第一次使用建议按这个顺序：

1. 在“文档库”浏览已有文档。
2. 点证据编号回看原始事件。
3. 阅读后做审核、关闭待办问题。
4. 在“提炼”里选择会话，确认后提炼。
5. 在“设置与导出”里生成 Markdown、索引和 `AGENTS.md`。

## 文档

- **完整用户文档**：[`cli/README.md`](cli/README.md)
  - 环境要求、构建、Web UI、全部 CLI 命令、配置、缓存/增量、RAG 契约、Agent 接入、故障排查。
- **贡献指南**：[`CONTRIBUTING.md`](CONTRIBUTING.md)
  - 开发环境、质量门禁、兼容性与隐私约束、PR 流程。
- **安全策略**：[`SECURITY.md`](SECURITY.md)
  - 私密漏洞报告、支持版本、范围与非范围。
- **架构说明**：[`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)
  - 数据流、包职责、Doc/输出契约、并发与信任边界。
- **隐私说明**：[`docs/PRIVACY.md`](docs/PRIVACY.md)
  - 本地数据、模型 Provider 边界、脱敏限制和安全贡献清单。
- **GitHub 发布步骤**：[`docs/GITHUB_SETUP.md`](docs/GITHUB_SETUP.md)
  - 首次提交、邮箱保护、创建仓库、分支保护和发布配置。
- **变更记录**：[`CHANGELOG.md`](CHANGELOG.md)
- **学习案例样例**：[`learning-cases-sample/`](learning-cases-sample/)
  - 人工整理的 Markdown 样例，用于验证学习模板格式。

## 仓库结构

```text
cli/cmd/                  CLI 子命令（含 serve）
cli/internal/             解析、切分、脱敏、提炼、缓存、渲染、索引、检索、MCP、Web API
cli/web/                  React + Vite + TypeScript 前端源码
cli/start-webui.cmd       Windows 双击启动入口
docs/                     架构、隐私、发布与 GitHub 设置文档
.github/                  CI、CodeQL、Release、Dependabot、Issue/PR 模板
learning-cases-sample/    人工学习案例样例
```

## 开发

```bash
cd cli
gofmt -l .
go vet ./...
go test ./... -count=1
go test ./... -race -count=1
```

重新构建前端：

```bash
cd cli/web
npm install
npm run build
cd ..
go build -o devlog-learn.exe ./cmd
```

自动测试从不调用真实模型；模型边界使用 fake Client。

## 隐私

- 会话日志只在本机解析。
- 内置规则脱敏；Web UI 只绑定回环地址，API 需要进程级 token。
- 本项目没有遥测、CDN 或自建云服务。
- 提炼只通过你本机的 `claude` / `codex` CLI 发起。该 CLI 是否把 prompt 发送给模型 Provider，取决于它的配置和账户策略；详见 [`docs/PRIVACY.md`](docs/PRIVACY.md)。
- `.devlog-cache/`、`.devlog-docs/`、`.devlog-cursor.json`、`.codex/` 和构建二进制默认不进入 Git。

## 参与贡献

自动测试不得调用真实模型。开始提交前请阅读 [`CONTRIBUTING.md`](CONTRIBUTING.md)，安全问题请按 [`SECURITY.md`](SECURITY.md) 私密报告。

## License

[MIT](LICENSE)
