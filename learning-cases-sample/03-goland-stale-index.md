# 学习案例 03：移动 Go 缓存后，GoLand 里 `time.Second` 全部报红

> 环境：Windows + Go 1.25.1 + GoLand 2024.3.4
> 会话：`codex / 01a166b6-0e1f-7f60-9511-8fe035678730`（2026-10-23）
> 原始事件：约 line 344–475
> 学习价值：**高**。它教你区分“编译器真的坏了”和“IDE 在骗你”，以及环境迁移后的正确收尾。

---

## 0. 先自己想

1. `time.Second` 是标准库常量。它报“未解析”，更可能是 Go 安装坏了、项目代码坏了，还是 IDE 索引坏了？
2. 怎么用命令行证明“Go 工具链其实是好的”？
3. 移动 `GOMODCACHE` / `GOCACHE` 之后，IDE 需要做什么才能一致？

---

## 1. 症状

- 用户把 C 盘的 Go 模块缓存/构建缓存迁到 D 盘，并删除了 C 盘旧数据。
- 之后在 GoLand 里写 Go 代码，`time.Second`、`context` 等标准库全部显示“未解析的引用”。
- 用户第一反应：“你干了什么？是不是把环境弄坏了？”

---

## 2. 定位过程

| 步骤 | 做了什么 | 命令 | 观察到什么 | 结论 |
|---|---|---|---|---|
| 1 | `go version` / `go env` | C1 | Go 1.25.1；`GOROOT=D:\golang\go`；`GOPATH=D:\goProject`；`GOMODCACHE=D:\goProject\pkg\mod`；`GOCACHE=D:\goProject\.cache\go-build` | 路径都已指向 D 盘 |
| 2 | `go doc time.Second` | C2 | 能正常输出 `Second Duration = 1000 * Millisecond` | Go 工具链能解析标准库 |
| 3 | 检查 `D:\golang\go\src\time\time.go` | C3 | 文件存在 | 标准库源码没丢 |
| 4 | 把 `GOCACHE`/`GOTMPDIR` 指到可写目录，`go build main.go` | C4 | **编译成功**，生成 exe | 代码和工具链都没问题 |
| 5 | 读 GoLand `go.sdk.xml` | C5 | 登记了 `...toolchain@v0.0.1-go1.25.9.windows-amd64` | 这个目录**不存在** |
| 6 | 对比实际 toolchain 目录 | C6 | 存在 1.25.0 / 1.25.1 / 1.26.9 / 1.26.10，没有 1.25.9 | 无效 SDK 条目 |
| 7 | 看 GoLand 索引/日志时间 | C7 | 索引日志停在更早的时间，和当前会话不一致 | 旧索引没有重建 |
| 8 | 顺带检查项目文件 | C8 | `test_2.go` 是空文件 | 另一个独立编译错误 |

### 被排除的假设

- **“Go 安装坏了”**：`go doc` 和 `go build` 都正常，排除。
- **“标准库源码被删了”**：`D:\golang\go\src\time\time.go` 存在，排除。
- **“项目代码有语法错误导致标准库也报红”**：单独编译 `main.go` 成功，排除。
- **“模块缓存不完整”**：标准库不依赖 `GOMODCACHE`；且 1.25.1 toolchain 完整，排除为主因。

---

## 2.5 本次实际用到的有效命令（可复用）

> `C1–C8` 都是只读；修复动作主要是 GoLand 的 UI 操作，见 4.1–4.5。
> 路径按你的实际安装替换。

### C1. 先看 Go 环境变量

```bash
go version
go env -json GOROOT GOTOOLDIR GOPATH GOMODCACHE GOCACHE GOENV GOPROXY GOTMPDIR
```

**看什么**：

- `GOROOT` 是否指向真实安装目录（这次是 `D:\golang\go`）。
- `GOMODCACHE` / `GOCACHE` 是否都在迁移后的盘。
- `GOTMPDIR` 是否还指向 C 盘临时目录（可能导致权限问题）。

### C2. 用命令行证明标准库能解析

```bash
go doc time.Second
go list -json time
```

**看什么**：

- `go doc` 能输出 `Second Duration = 1000 * Millisecond`，说明标准库可用。
- `go list` 报错才是工具链/环境问题；这次报错是临时目录权限，不是 `time` 包不存在。

### C3. 检查标准库源码是否存在

```bash
# PowerShell
Test-Path D:\golang\go\bin\go.exe
Test-Path D:\golang\go\src\time\time.go
```

**看什么**：

- 两个都应为 `True`。
- 如果 `src\time\time.go` 不存在，才是 Go 安装不完整。

### C4. 隔离编译，排除 IDE

```powershell
$test = "D:\temp\go-env-check"
New-Item -ItemType Directory -Force -Path $test | Out-Null

$env:GOCACHE  = Join-Path $test "cache"
$env:GOTMPDIR = Join-Path $test "tmp"
$env:TMP      = $env:GOTMPDIR
$env:TEMP     = $env:GOTMPDIR
New-Item -ItemType Directory -Force -Path $env:GOCACHE, $env:GOTMPDIR | Out-Null

go build -o (Join-Path $test "check.exe") .\main.go
```

**看什么**：

- 编译成功 = 代码和 Go 工具链都没问题，问题在 IDE。
- 这一步是整次排查的 ground truth。

### C5. 读 GoLand 登记的 SDK

```powershell
Get-Content "$env:APPDATA\JetBrains\GoLand2024.3\options\go.sdk.xml" -Raw
```

**看什么**：

- 每个 `file://...` 路径是否真实存在。
- 这次发现 `toolchain@v0.0.1-go1.25.9...` 不存在。
- `D:/golang/go` 是有效的。

### C6. 对比模块缓存里的 toolchain

```powershell
Get-ChildItem "D:\goProject\pkg\mod\golang.org" -Directory -Filter "toolchain@*" |
  ForEach-Object {
    [pscustomobject]@{
      Path    = $_.FullName
      GoExe   = Test-Path (Join-Path $_.FullName "bin\go.exe")
      TimeSrc = Test-Path (Join-Path $_.FullName "src\time\time.go")
    }
  } | Format-Table -AutoSize
```

**看什么**：

- 有哪些 toolchain 版本。
- 哪个目录真实可用。
- 不要盲目相信 IDE 里保存的路径。

### C7. 看 IDE 是否还在运行、索引是否过期

```powershell
Get-Process | Where-Object { $_.ProcessName -match 'goland|jetbrains|fsnotifier' } |
  Select-Object Id, ProcessName, Path, StartTime

Get-ChildItem "$env:LOCALAPPDATA\JetBrains\GoLand2024.3\log" -File |
  Sort-Object LastWriteTime -Descending |
  Select-Object -First 5 Name, Length, LastWriteTime
```

**看什么**：

- GoLand 是否正在运行（运行中直接改配置可能被回写）。
- 索引/日志时间是否明显落后于当前时间。

### C8. 找空文件导致的独立错误

```powershell
Get-ChildItem "D:\goProject\src\interview" -Recurse -File -Include *.go |
  Where-Object { (Get-Item $_.FullName).Length -eq 0 } |
  Select-Object FullName
```

**看什么**：

- 空 `.go` 文件会导致 `expected 'package', found 'EOF'`。
- 这是独立问题，不要和 SDK 报红混在一起。

### 命令坑：PowerShell 里的反引号

这次会话中，MySQL 查询里的反引号在 PowerShell 里被解释，连续报：

```text
ERROR at line 1: Unknown command '\k'.
```

解决方式是换一种写法（`SELECT *`、把 SQL 放进文件、或使用参数化工具）。**遇到 shell 转义错误，先怀疑命令本身，不要怀疑业务结论。**

### 修复动作（不是命令，是 UI）

1. `File > Invalidate Caches`
2. 勾选 `Delete downloaded shared indexes`
3. `Invalidate and Restart`
4. `Settings > Go > GOROOT` → `D:\golang\go`
5. 右键 `go.mod` → `Reload Go Dependencies`

如果仍然失败：完全退出 GoLand（确认没有 `goland64` 进程）后再清理索引/无效 SDK，避免 IDE 退出时把旧配置写回去。

---

## 3. 根因

> Go 工具链本身是好的；问题在 GoLand 的 **SDK 登记 + 索引状态**。迁移缓存时没有先退出 IDE/让旧索引失效，GoLand 仍引用迁移前的 SDK/索引快照，其中还包含一个已经不存在的 toolchain 路径。

一句话：**IDE 的解析结果过期了，不代表编译器坏了。**

---

## 4. 修复

### 4.1 先让 IDE 丢掉旧状态

1. 保存所有文件。
2. `File > Invalidate Caches`。
3. 勾选 `Delete downloaded shared indexes`。
4. `Invalidate and Restart`。

### 4.2 选对 GOROOT

`Settings > Go > GOROOT` 选择：

```text
D:\golang\go
```

不要选模块缓存里的 `toolchain@...` 路径。

### 4.3 重新加载依赖

右键项目 `go.mod` → `Reload Go Dependencies`，等待索引完成。

### 4.4 如果仍然报红

- 完全退出 GoLand（确认后台没有 `goland64` 进程）。
- 删除无效的 SDK 条目，或清理 GoLand 的 `caches` / `index` 目录。
- 重新打开项目，等索引结束。

### 4.5 顺手修掉独立问题

`test_2.go` 是空文件，会导致：

```text
expected 'package', found 'EOF'
```

要么补上 `package main`，要么删除这个空文件。它和 `time.Second` 报红不是同一个问题。

---

## 5. 验证

命令行（不经过 IDE）验证：

```bash
go doc time.Second
go build -o /tmp/check.exe main.go
```

两条都成功，说明工具链和代码没问题。IDE 侧修复后，标准库引用应恢复；如果仍然报红，说明索引还没重建完或 SDK 仍选错。

---

## 6. 可迁移的方法

### 方法 1：把“编译器事实”和“IDE 显示”分开

```text
IDE 报红 ≠ 编译失败
```

先用命令行复现：

```bash
go build ./...
go doc <包>.<符号>
go list -json <包>
```

命令行通过，问题就在 IDE/索引/配置。

### 方法 2：迁移缓存时按顺序操作

1. 关闭 IDE。
2. 迁移 `GOMODCACHE` / `GOCACHE`。
3. 更新 `go env -w`。
4. 用命令行验证 `go build`。
5. 打开 IDE，按需 Invalidate Caches。
6. 重新加载依赖，等索引完成。

### 方法 3：配置项要和文件系统对账

看到 IDE 里配置了某个 SDK/路径，不要假设它存在；直接 `Test-Path` / `ls` 验证。

### 方法 4：一次只改一个变量

先把 `GOCACHE`/`GOTMPDIR` 指到可写目录做隔离编译，避免把“权限/临时目录”问题误判成“Go 安装坏了”。

---

## 7. 知识点卡片

### 卡片 A：GOROOT / GOPATH / GOMODCACHE / GOCACHE

| 变量 | 作用 |
|---|---|
| `GOROOT` | Go 安装目录，标准库源码在 `$GOROOT/src` |
| `GOPATH` | 传统工作区；模块模式下仍影响部分工具 |
| `GOMODCACHE` | 依赖模块缓存，通常在 `$GOPATH/pkg/mod` |
| `GOCACHE` | 编译缓存，可安全删除，重建即可 |
| `GOTMPDIR` | 编译临时目录；C 盘无权限时可改到 D 盘 |

### 卡片 B：Go toolchain 自动下载

`go.mod` 里的 `go 1.25.1` 可能触发 Go 自动下载 toolchain 到：

```text
$GOMODCACHE/golang.org/toolchain@v0.0.1-go1.25.1.windows-amd64
```

这是**模块缓存里的另一个 Go 发行版**，不是你的 `GOROOT`。IDE 如果选中了它，而目录又被移动/删除，就会出现标准库整体报红。

### 卡片 C：IDE 索引

- IDE 的“未解析”来自自己的索引，不一定来自 `go build`。
- 迁移文件、切换 SDK、升级 Go 后，旧索引可能失效。
- `Invalidate Caches / Restart` 是标准修复手段。

### 卡片 D：PowerShell 转义坑

这次排查里，MySQL 查询里的反引号在 PowerShell 中被错误解释，连续两次报 `Unknown command '\k'`。最后改用 `SELECT *` 绕过。

教训：**跨 shell 传 SQL/路径时，优先用文件或参数化方式，不要和引号/反引号搏斗。**

---

## 8. 这次 AI 知道、你可能没注意的点

| 点 | 类型 | 说明 |
|---|---|---|
| 先用 `go doc` / `go build` 验证工具链 | 通用调试方法 | 建立 ground truth |
| 检查 SDK 路径是否真实存在 | 工具配置知识 | 配置会过期 |
| 区分空文件错误和 SDK 错误 | 通用调试方法 | 一次只归因一个问题 |
| 迁移缓存后让 IDE 失效重建 | 工具使用知识 | 顺序很重要 |
| PowerShell 反引号转义 | 环境知识 | 容易浪费很多时间 |

---

## 9. 下次环境迁移后的检查清单

- [ ] `go version`
- [ ] `go env GOROOT GOPATH GOMODCACHE GOCACHE GOTMPDIR`
- [ ] `go doc time.Second`
- [ ] `go build ./...`（不经过 IDE）
- [ ] IDE 的 GOROOT/SDK 指向真实存在的目录
- [ ] Invalidate Caches / 重新索引
- [ ] `go.mod` Reload Dependencies
- [ ] 检查空文件/未保存文件导致的独立错误

---

## 10. 复习题

<details>
<summary>Q1：IDE 里标准库全部报红，但 `go build` 成功，说明什么？</summary>

说明 Go 工具链和代码没问题，问题在 IDE 的 SDK 配置或索引状态。应检查 IDE 的 GOROOT、SDK 路径是否存在，并重建索引。
</details>

<details>
<summary>Q2：`GOMODCACHE` 里的 `toolchain@...` 和 `GOROOT` 有什么区别？</summary>

`GOROOT` 是你安装的 Go；`GOMODCACHE/golang.org/toolchain@...` 是 Go 为了满足 `go.mod` 版本要求自动下载的另一份工具链。两者都可能提供标准库，但 IDE 应指向真实存在且预期的那个。
</details>

<details>
<summary>Q3：为什么移动缓存前要先关闭 IDE？</summary>

IDE 在运行时会持有 SDK/索引快照，退出时还可能回写配置。先关闭可以避免旧状态覆盖新配置，也避免索引指向已移动的路径。
</details>

---

## 11. 证据来源

- 会话：`01a166b6-0e1f-7f60-9511-8fe035678730`（Codex，jia）
- 原始事件：line 344–475
- 关键命令：`go version`、`go env -json`、`go doc time.Second`、`go build main.go`
- 关键配置：GoLand `go.sdk.xml`、项目 `GOROOT`
