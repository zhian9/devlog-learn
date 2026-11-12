# 学习案例 01：支付成功但订单一直“待支付”，还能反复点去支付

> 项目：GoForge（Go + go-zero + Kafka + MySQL + Vue3/TS）
> 会话：`codex / 01a0b473-9479-7d72-bc28-30756d445a7e`（2026-09-18）
> 原始事件：约 line 1384–1466
> 学习价值：**高**。它展示了一个非常典型的“动作成功了，但状态没变”的排查路径。
> 说明：文中的数据库密码、服务器地址等已脱敏。

---

## 0. 先自己想

1. 支付单表里已经有 5 条 `status=1` 的成功记录，订单表却还是“待支付”。你会先怀疑哪一层：支付服务、Kafka、消费者、订单状态机，还是前端？
2. Kafka 消费者组的 `lag=0`，能不能证明消息已经被业务代码处理了？
3. 如果根因是“消息被跳过”，你应该只改代码，还是也要修数据？为什么？

---

## 1. 症状（可观察事实）

- 用户支付后，订单页仍然显示“去支付”，点击后可以不断循环支付。
- 数据库里订单 28 有 **5 条支付成功记录**（`payment.status=1`）。
- 但 `orders` 表里订单 28 的状态仍是 **1（待支付）**，`payment_time` 为 `NULL`。
- 前一天支付的订单 25/26 状态正常（3/4），说明链路以前是通的。

**关键矛盾：支付单成功 ≠ 订单已支付。**

---

## 2. 定位过程（每一步都在缩小范围）

| 步骤 | 做了什么 | 命令 | 观察到什么 | 结论 |
|---|---|---|---|---|
| 1 | 查 `payment` 与 `orders` 两张表 | C1 | 5 条支付成功，订单状态仍为 1，`payment_time=NULL` | 问题在“支付成功之后”的状态同步 |
| 2 | 看 `order-consumer` 日志 | C2 | 只有启动行，没有任何消费/处理日志 | 消费者可能没处理消息 |
| 3 | 查 Kafka topic `payment-service.success` 的消息数 | C3 | 有 15 条消息 | 支付服务**确实发出去了** |
| 4 | 查消费者组 offset | C3 | `current=15, log-end=15, lag=0` | 消息“看起来已被消费” |
| 5 | 再对比消费者日志 | C2 | lag=0，但 handler 一条日志都没有 | **矛盾点**：offset 前进了，业务代码却没执行 |
| 6 | 读 `server/internal/pkg/mq/kafka.go` | C4 | `Offsets.Initial = sarama.OffsetNewest` | 找到可疑配置 |
| 7 | 对照时间线 + 关联查询 | C5 | 支付发生在 12:38–12:53；Kafka/消费者刚经历 `down` + `up` | 重启后 offset 状态异常，`Newest` 跳过了积压消息 |

### 被排除的假设

- **“支付服务没发消息”**：topic 里有 15 条，排除。
- **“消费者没启动”**：日志显示 12:34 起持续运行，排除。
- **“前端按钮逻辑写错了”**：按钮只是结果；数据库订单状态确实是“待支付”，排除。
- **“消费者 handler 有 bug”**：handler 根本没执行，连日志都没有，排除。
- **“`pay_amount=0` 导致的问题”**：这是另一个历史问题，和本次状态不同步无关，先隔离。

---

## 2.5 本次实际用到的有效命令（可复用）

> 只列“改变了判断”的命令，不列纯探索或写错的命令。
> `C1–C5` 是只读排查；`C6` 会改数据；`C7` 会构建/重启服务。
> 占位符：`<db-user>`、`<db-password>`、`<order-consumer>`、`<kafka>`、`<group>` 换成你的环境。

### C1. 对比支付单与订单状态

```bash
# 最近 5 条支付单
docker exec <mysql> mysql -u<db-user> -p<db-password> go_forge -e \
  "SELECT id, payment_no, order_id, status, paid_at FROM payment ORDER BY id DESC LIMIT 5;"

# 最近 5 条订单
docker exec <mysql> mysql -u<db-user> -p<db-password> go_forge -e \
  "SELECT id, order_no, status, payment_time FROM orders ORDER BY id DESC LIMIT 5;"
```

**看什么**：

- `payment.status=1` 表示支付成功。
- `orders.status` 是否还是 `1`（待支付）。
- `payment_time` 是否为 `NULL`。
- 两边 `order_id` 是否能对上。

### C2. 看消费者到底有没有处理消息

```bash
# 只保留支付相关日志
docker logs --tail 40 <order-consumer> 2>&1 \
  | grep -iE "支付|MarkPaid|payment|积分|消费|error|失败" | tail -20

# 去掉统计噪音，看完整启动/处理日志
docker logs <order-consumer> 2>&1 \
  | grep -vE "stat/usage|CPU:|MEMORY:|@timestamp.*stat" | head -60
```

**看什么**：

- 有没有 “等待消息” 之外的 handler 日志。
- 有没有 `MarkPaid`、消费失败、反序列化错误。
- 如果只有启动行，说明业务代码没执行。

### C3. 查 Kafka：消息发了吗？消费到哪了？

```bash
# topic 是否存在
docker exec <kafka> kafka-topics --bootstrap-server localhost:9092 --list \
  | grep -iE "payment|order"

# topic 末尾 offset（可以理解为消息总数）
docker exec <kafka> kafka-run-class kafka.tools.GetOffsetShell \
  --broker-list localhost:9092 --topic payment-service.success --time -1

# 消费者组当前 offset / lag
docker exec <kafka> kafka-consumer-groups --bootstrap-server localhost:9092 \
  --describe --group <group>
```

**看什么**：

- `LOG-END-OFFSET > 0`：消息确实发出去了。
- `CURRENT-OFFSET` 与 `LOG-END-OFFSET` 是否相等（`LAG=0`）。
- `LAG=0` **不等于** handler 执行过；它只表示 offset 追平了。

### C4. 找消费者的 offset 配置

```bash
ls server/internal/pkg/mq/
grep -rn "Offset\|offset\|Initial\|Earliest\|Latest" \
  server/internal/pkg/mq/*.go | grep -v "topics.go"
```

**看什么**：

- `Offsets.Initial` 是 `OffsetNewest` 还是 `OffsetOldest`。
- 有没有其他重置 offset 的逻辑。

### C5. 把支付单和订单关联起来，确认影响范围

```bash
docker exec <mysql> mysql -u<db-user> -p<db-password> go_forge -e \
  "SELECT p.id, p.order_id, p.payment_no, p.status AS pay_status,
          o.status AS order_status, o.order_no
   FROM payment p JOIN orders o ON p.order_id = o.id
   ORDER BY p.id DESC LIMIT 15;"
```

**看什么**：

- 哪些订单是“支付成功但订单未支付”。
- 是单笔异常，还是同一时间段的一批订单。
- 对比故障前后的数据，判断是不是重启时间点造成的。

### C6. 立即补偿数据（会改数据）

```sql
UPDATE orders
SET status = 2, payment_method = 1, payment_time = NOW(), updated_at = NOW()
WHERE id = 28 AND status = 1;
```

**注意**：

- 这是写操作，执行前先备份/确认订单号。
- `AND status = 1` 是幂等保护，避免覆盖已处理订单。
- 补偿只是止血，根因还要改代码。

### C7. 改代码后构建并重启（会部署）

```bash
go build ./internal/pkg/mq/... ./cmd/order-service-consumer/...

docker build -t <image> -f deploy/docker/server.Dockerfile .
cd deploy/compose
docker compose up -d --force-recreate <order-consumer>
```

**看什么**：

- 构建是否通过。
- 容器是否 `Started`，日志是否正常。
- 重启后再制造一次支付，确认订单状态会变。

### 命令坑：不要被无关报错带偏

本次排查里，`docker logs ... | grep ...` 和 SQL 都是有效的；但数据库查询一度被 PowerShell 的反引号转义干扰。遇到 `Unknown command '\k'` 这类错误时，先判断是 **shell 转义问题**，不是 SQL 或业务问题。可以改用 `SELECT *`、把 SQL 放进文件，或换一个 shell。

---

## 3. 根因

> Kafka 消费者配置了 `Offsets.Initial = OffsetNewest`。Kafka 重启后消费者组没有有效的已提交 offset，消费者按“最新”位置启动，把积压的 15 条支付成功消息全部跳过。于是 offset 到了 15（`lag=0`），但 `MarkPaid` 一次都没执行，订单永远停在“待支付”。

一句话：**消息不是丢了，是被“跳过”了；`lag=0` 只说明 offset 追平了，不代表 handler 跑过。**

---

## 4. 修复

### 4.1 立即补偿数据

```sql
UPDATE orders
SET status = 2,                -- 待发货
    payment_method = 1,
    payment_time = NOW(),
    updated_at = NOW()
WHERE id = 28 AND status = 1;  -- 条件更新，避免覆盖已处理订单
```

### 4.2 根治代码

```go
// server/internal/pkg/mq/kafka.go
config.Consumer.Offsets.Initial = sarama.OffsetOldest
```

### 4.3 为什么要“补数据 + 改代码”两步

- 只改代码：历史 15 条消息已经被跳过，订单 28 不会自动恢复。
- 只补数据：下次重启还会再发生。
- 条件更新 `WHERE status=1` 让补偿和消息重放都具备幂等性。

---

## 5. 验证

- 补偿后查询：订单 28 变为 `status=2`，`payment_time` 已写入。
- `go build ./internal/pkg/mq/... ./cmd/order-service-consumer/...` 通过。
- 重新构建镜像并重启 consumer，容器 `Started`，无报错。
- 前端按钮只在 `status===1` 时显示；订单变为 2 后不再出现“去支付”。
- 代码中的 `MarkPaid` 使用条件更新，重复消费历史消息也不会把已发货订单改回去。

---

## 6. 可迁移的方法

### 方法 1：沿着“写 → 消息 → 消费 → 读”逐层验证

```text
写入数据库 → 发布事件 → 消费者收到 → handler 执行 → 状态落库 → 前端读取
```

每一层都要有一个独立证据；不要因为某一层“看起来成功”就跳过下一层。

### 方法 2：区分三种“没处理”

| 现象 | 证据 | 含义 |
|---|---|---|
| 消息没发出 | topic end offset 不增长 | 生产端问题 |
| 消息没被消费 | consumer group lag > 0 | 消费端没跟上 |
| 消息被跳过/消费了但没执行 | lag=0 但 handler 无日志 | offset 重置、过滤、空 handler、异常吞掉 |

### 方法 3：不要相信单一指标

`lag=0` 是“offset 追平”，不是“业务成功”。必须配合 handler 日志、数据库状态、关键字段（如 `payment_time`）一起看。

### 方法 4：最终一致性的系统要设计补偿

- 同步兜底：支付回调成功后直接调用订单服务。
- Outbox 模式：支付单状态与事件同事务落库，再投递。
- 幂等消费：条件更新/去重表，保证重放安全。

---

## 7. 知识点卡片

### 卡片 A：Kafka `Offsets.Initial`

- `OffsetNewest`：没有已提交 offset 时，从最新位置开始，**会跳过积压消息**。
- `OffsetOldest`：没有已提交 offset 时，从最早位置开始，**会重放历史消息**。
- 它只在“没有有效已提交 offset”时生效；有 offset 时按已提交位置继续。
- 重放安全的前提：消费逻辑必须幂等。

### 卡片 B：Consumer Group Lag

```text
lag = log-end-offset - current-offset
```

- `lag=0` 只表示消费者 offset 追上了日志末尾。
- 如果 offset 被重置到末尾，lag 也会立刻变成 0，但消息没有被处理。

### 卡片 C：幂等条件更新

```sql
UPDATE orders SET status = 2
WHERE id = ? AND status = 1;
```

- 只有当前状态符合预期才更新。
- 重复执行不会产生副作用。
- 这是消息系统“至少一次”语义下的基本防御。

### 卡片 D：Outbox 模式（延伸）

业务数据和“待发送事件”写在同一个数据库事务里，再由独立投递器发送到 Kafka。可以避免“数据库成功、消息丢失”的不一致。

---

## 8. 这次 AI 知道、你可能没注意的点

| 点 | 类型 | 说明 |
|---|---|---|
| 先查数据库对比两张表 | 通用调试方法 | 先确认事实，不先猜代码 |
| 用 topic 消息数排除生产端 | 通用调试方法 | 把“没发”与“没消费”分开 |
| 用 lag + handler 日志发现矛盾 | 框架/中间件知识 | offset 语义 |
| 条件更新保证补偿安全 | 数据库/业务知识 | 幂等 |
| 最终一致性需要兜底 | 架构知识 | Outbox / 同步补偿 |

---

## 9. 下次遇到类似问题，先做这 6 件事

1. 找到“状态应该变化”的那张表，确认它到底有没有变。
2. 找到“应该触发变化”的事件/接口，确认它有没有发出。
3. 查消费者/队列的 offset、lag、错误日志，三者一起看。
4. 如果 lag=0 但业务没执行，优先怀疑 offset 重置/跳过。
5. 读中间件配置（`Initial`、`auto.offset.reset`、ack 模式）。
6. 先补数据止血，再改根因，最后补幂等和兜底。

---

## 10. 复习题

<details>
<summary>Q1：lag=0 但订单没更新，有哪些可能？</summary>

1. offset 被重置到最新位置，消息被跳过。
2. 消费者过滤掉了这类消息。
3. handler 执行了但异常被吞掉，没有日志。
4. 消费的是另一个 topic/分区，业务 topic 没消费。
5. 业务代码更新了错误的订单/字段。
6. 消息是旧格式，反序列化后字段为空。
</details>

<details>
<summary>Q2：为什么 `OffsetNewest` 在重启后特别危险？</summary>

重启后如果消费者组没有有效的已提交 offset，`Newest` 会把启动点定位到当前末尾，之前的积压消息全部跳过；而 offset 追平后 `lag=0`，监控看起来“正常”，故障很隐蔽。
</details>

<details>
<summary>Q3：为什么补偿 SQL 要带 `AND status = 1`？</summary>

保证幂等：只有仍处于“待支付”的订单才更新；重复执行或消息重放不会覆盖已经发货/取消的订单。
</details>

---

## 11. 证据来源

- 会话：`01a0b473-9479-7d72-bc28-30756d445a7e`（Codex，GoForge）
- 原始事件：line 1384–1466
- 关键文件：`server/internal/pkg/mq/kafka.go`（`Offsets.Initial`）
- 关键查询：`payment` / `orders` 关联、Kafka topic offset、consumer group lag
