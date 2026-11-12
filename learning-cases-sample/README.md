# 学习案例样例（3 篇）

这三篇不是“知识总结”，而是从真实 AI 调试会话里还原出来的**可学习案例**。目的：让你能复述并独立重做一次定位过程，而不是只记住结论。

- [案例 01：支付成功但订单一直“待支付”](01-payment-consumer-offset.md)
  - 关键词：Kafka offset、`OffsetNewest`、消费积压、最终一致性、幂等补偿
  - 价值：高
- [案例 02：后台改标题“保存成功”，前台却不变](02-settings-route-auth.md)
  - 关键词：Gin 路由组、匿名 401、前端 fallback、dev/prod 配置差异、部署验证
  - 价值：高
- [案例 03：移动 Go 缓存后，GoLand 里 `time.Second` 报红](03-goland-stale-index.md)
  - 关键词：GOROOT/GOMODCACHE/GOCACHE、GoLand SDK、索引失效、命令行 ground truth
  - 价值：高

## 每篇的结构

1. **先自己想**：遮住后文，先回答几个问题
2. **症状**：只写可观察事实，不写猜测
3. **定位过程**：每一步做了什么、看到什么、得出什么结论
4. **被排除的假设**：学习“怎么缩小范围”
5. **根因**
6. **修复**
7. **验证**
8. **可迁移的方法**：换一个项目也能用的思路
9. **知识点卡片**：Go / 中间件 / 前端 / 工具链
10. **这次 AI 知道、你可能没注意的点**
11. **下次检查清单**
12. **复习题**（默认折叠答案）
13. **证据来源**：会话 ID、原始事件行号、关键文件

## 请你重点评估这 4 个问题

1. 读完一篇，你能不能**不看原文**复述“当时是怎么定位的”？
2. “定位过程”里的每一步，是否足够具体到你能**自己重做**？
3. “知识点卡片”是不是你真正缺的语法/框架知识，而不是泛泛而谈？
4. 复习题是否值得做，还是太简单/太像考试？

如果第 1、2 题是否定的，说明提取重点应该放在**诊断路径**，而不是结论。
如果第 3 题是否定的，说明需要针对你的栈（Go / GoFrame / TS / React）做更聚焦的知识卡片。

## 命令如何进入学习案例

“有效命令”不是把所有执行过的命令都抄下来，而是满足以下至少一条：

1. 它的输出**改变或确认了一个假设**。
2. 它是**复现问题**所必需的。
3. 它是**验证修复**所必需的。
4. 它是同类问题中**可直接复用**的排查手段。

不收录：

- 纯探索、没有产生信息量的命令。
- 因拼写/转义错误失败、且没有教学价值的命令（有教学价值的放进 `pitfalls`，例如 PowerShell 反引号坑）。
- 包含真实密钥、token、密码、内部地址的命令（保留结构，替换为占位符）。

每条命令需要标注：

| 字段 | 含义 |
|---|---|
| `purpose` | 这条命令用来回答什么问题 |
| `command` | 实际可执行的命令（已脱敏） |
| `kind` | `read` / `write` / `build` / `deploy` / `debug` |
| `expected_signal` | 看什么输出、什么状态码、什么关键字 |
| `risk` | 是否改数据、重启服务、影响线上 |

在案例正文里，命令用 `C1, C2...` 编号，定位表里直接引用编号，避免“只说做了什么、不给命令”。

## 如果这个格式成立，下一步改什么

**本轮已实现（0.10.0）**：

- `diagnosis[]`：观察 → 假设 → 动作 → 命令 → 结果 → 结论/排除
- `commands[]`：有效命令清单（purpose / command / kind / expected_signal / risk）
- Markdown 新增 `## Diagnosis Path` / `## Effective Commands`
- `search` 会检索命令文本，例如 `search "kafka-consumer-groups offset"`

当前 Doc Schema 已有：

- `problem` / `symptoms` / `error_signatures`
- `root_cause`
- `solution[]`
- `insights` / `pitfalls` / `lessons`
- `verification`
- `evidence`

仍建议新增（只做加法，保持现有字段不变）：

- `rejected_hypotheses[]`：当时排除了什么，为什么
- `concepts[]`：语言/框架知识点 + 最小示例 + 官方文档
- `blind_spots[]`：AI 知道而你可能不知道的点
- `learning_value`：这偏案例值不值得学（low/medium/high）
- `recall_questions[]`：问题 + 折叠答案
- `source_refs[]`：会话 ID、segment ID、事件 seq 范围
- 学习进度 sidecar：已掌握 / 需复习 / 下次复习时间

### Prompt 需要新增的约束

```text
For each diagnosis step, record the exact command that produced the decisive
observation, what output mattered, and whether it is read-only or mutating.
Only include commands that changed/confirmed a hypothesis or are needed to
reproduce/verify the bug. Preserve arguments and paths; redact secrets.
If a command failed because of shell quoting, put it in pitfalls, not in the
diagnosis steps.
```

同时需要一个新的学习导向 Markdown 模板；现有的 Agent 记忆摘要（`AGENTS.md`）可以保留，但不再是主产物。

## 隐私说明

- 三篇样例已手动脱敏：数据库密码、服务器地址等替换为占位符。
- 正式实现应继续复用现有的脱敏层，并在生成学习文档前再次检查敏感信息。
- 原始会话行号仅用于本机追溯，不会进入最终对外文档。
