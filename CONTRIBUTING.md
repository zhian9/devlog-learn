# Contributing

感谢你考虑为 devlog-learn 贡献代码、测试或文档。

## 开始之前

- 使用 GitHub Issues 报告可复现的问题或讨论较大的功能变更。
- 使用 [SECURITY.md](SECURITY.md) 描述的私密渠道报告安全漏洞。
- 不要在 Issue、PR、测试夹具或提交中粘贴真实会话日志、访问令牌、客户数据或本机用户目录。

## 开发环境

需要：

- Go 1.24+
- Node.js 22+，仅在前端源码变化或重新构建内嵌资源时需要
- 可选的本地 `claude` / `codex` CLI；自动测试不需要它们

```bash
git clone <repository-url>
cd <repository-directory>/cli
go mod download
go test ./...
```

构建前端：

```bash
cd web
npm ci
npm run build
cd ..
go build -o devlog-learn ./cmd
```

`npm run build` 会更新 `cli/internal/webapi/dist/`，该目录是 Go 二进制的内嵌资源。

## 开发约束

- 自动测试不得调用真实模型。使用 fake Client 或 fake CLI 脚本。
- `Doc`、索引和游标格式是下游契约。字段优先做加法；不要随意的删除、重命名或改变语义。
- Segment ID、证据 `event_seq`、review 保留规则和缓存失效规则都有回归测试保护。
- 任何写 cache、输出目录或 cursor 的新路径都必须接入 `.devlog.lock` 写锁。
- 任何删除输出文件的新行为都必须经过 manifest ownership 和内容 hash 校验。
- 会话脱敏是纵深防御，不是数据分类保证。测试数据必须使用合成内容。
- 用户可见变化需要同步更新 `README.md`、`cli/README.md`、`CHANGELOG.md` 和相关契约测试。

## 验证

在 `cli/` 下运行：

```bash
gofmt -w .
go vet ./...
go test ./... -race -shuffle=on -count=1
go build -o devlog-learn ./cmd
```

前端变化还要运行：

```bash
cd web
npm run build
```

CI 会重复这些检查，并执行依赖完整性和漏洞扫描。

## 提交与 Pull Request

- 每个 PR 尽量只处理一个问题。
- 提交信息说明用户可见结果，例如 `fix: serialize CLI and Web cache writes`。
- PR 描述请包含动机、行为变化、验证命令、兼容性影响和隐私影响。
- 新行为应有测试；纯文档修复可以只引用检查过的命令。
- 不要在 PR 中提交 `.codex/`、`.devlog-cache/`、`.devlog-docs/`、`.devlog-cursor.json`、构建二进制或 `node_modules/`。

提交 PR 即表示你有权提供该贡献，并同意按仓库的 [MIT License](LICENSE) 发布。
