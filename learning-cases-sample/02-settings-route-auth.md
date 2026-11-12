# 学习案例 02：后台改标题“保存成功”，前台却永远不变

> 项目：Blogo（Go + Gin + React/TS + MySQL）
> 会话：`codex / 01a0bf00-c146-7ce2-a715-f2093e7a4de0`（2026-09-20/21）
> 原始事件：约 line 3387–3474
> 学习价值：**高**。这是“写成功但读不到”的经典分层排查，也演示了本地/线上环境差异。

---

## 0. 先自己想

1. 后台提示“保存成功”，能证明前台一定能读到新值吗？
2. 前台刷新也没变化，你会先查浏览器缓存、前端代码、后端接口，还是数据库？
3. 如果本地正常、线上不正常，你会怎么确认是“没部署”而不是“代码有 bug”？

---

## 1. 症状

- 在后台修改首页标题/副标题，后台显示保存成功。
- 前台首页刷新后仍然是旧文案。
- “关于作者”按钮文案也改了，同样不生效。
- 用户怀疑：“是不是这个功能本身不好改？”

---

## 2. 定位过程

| 步骤 | 做了什么 | 命令 | 观察到什么 | 结论 |
|---|---|---|---|---|
| 1 | 查 `setting` 表 | C1 | `hero_subtitle` 在 14:15:35 已更新为 “React + Gin…” | **保存其实成功了** |
| 2 | 在 Setting BIZ/DAL 搜 Redis/Cache | C2 | 没有服务端缓存 | 排除“后端缓存没失效” |
| 3 | 在前端搜 `site_title` | C3 | 没有任何引用 | 首页读的不是 `site_title` |
| 4 | 确认首页实际读的 key | C3/C4 | `HeroSection.tsx` 使用 `hero_title` / `hero_subtitle` | 用户改的 key 和页面读的 key 需要对齐 |
| 5 | 检查本地 dev server | C5 | 已加载含 `hero_subtitle` 的最新代码 | 本地代码不是旧的 |
| 6 | 匿名请求本地 `/api/v1/settings/all` | C6 | **401 Invalid token** | 前台拿不到配置，回退到硬编码默认值 |
| 7 | 检查线上前台 JS bundle | C7 | 不含 `hero_subtitle`，仍含硬编码 “React + Go…” | 线上还没有部署新前端 |
| 8 | 对比 dev/prod 路由配置 | C4 | prod 的 auth 跳过列表有 `/settings/all`，dev 没有 | **dev 配置缺失是真 bug** |

### 一开始怀疑过、后来被排除的

- **“保存失败了”**：数据库 `updated_at` 和值都变了，排除。
- **“后端有 Redis 缓存”**：BIZ/DAL 没有缓存代码，排除。
- **“本地前端代码没更新”**：dev server 返回的 `HeroSection.tsx` 已包含 `hero_subtitle`，排除。
- **“只是改错了 key（site_title vs hero_title）”**：用户改的是 `hero_subtitle`，DB 里确实变了；key 语义是次要问题，不是本次根因。
- **“浏览器缓存”**：硬刷新也解决不了 401，排除为主因。

---

## 2.5 本次实际用到的有效命令（可复用）

> `C1–C7` 只读；`C8` 会构建/重启；`C9` 是部署动作。
> 占位符：`<db>`、`<db-user>`、`<db-password>`、`<domain>` 换成你的环境。

### C1. 确认“保存成功”到底写没写进去

```bash
mysql -h <db-host> -P <db-port> -u<db-user> -p<db-password> <db> -e \
  "SELECT * FROM setting ORDER BY updated_at DESC;"
```

**看什么**：

- 你改的那个 key 的值和 `updated_at` 是否变了。
- 如果值变了，说明写路径没问题，问题在读路径。
- 这次看到 `hero_subtitle` 在 14:15:35 更新，说明保存其实成功了。

> 原会话里用的是 PowerShell 拼 MySQL 命令，反引号被转义导致 `Unknown command '\k'`。先用 `SELECT *` 或把 SQL 放进文件，能少浪费很多时间。

### C2. 排除服务端缓存

```bash
rg -n "Cache|redis|cachex" \
  blogo-server/internal/mods/blog/biz/setting.biz.go \
  blogo-server/internal/mods/blog/dal/setting.dal.go
```

**看什么**：

- BIZ/DAL 是否用了 Redis/缓存。
- 这次没有命中，说明不是“缓存没失效”。

### C3. 确认前端到底读哪个 key

```bash
# 前台是否使用 site_title / site_description
rg -n "site_title|site_description" blogo-web/src

# 首页 Hero 实际读取的字段
rg -n "hero_title|hero_subtitle|useSettings" blogo-web/src/components/HeroSection.tsx
```

**看什么**：

- 页面读的是 `hero_*` 还是 `site_*`。
- 你改的 key 和页面读的 key 是否一致。
- 这次发现前台根本不引用 `site_title`，首页读的是 `hero_*`。

### C4. 看后端路由是公开还是受保护

```bash
rg -n -C 6 'settings' blogo-server/internal/mods/blog/blog.go
```

**看什么**：

- `/settings/all` 注册在 `public` 组还是受保护的 `setting` 组。
- dev/prod 的 auth 跳过列表是否都包含它。
- 这次发现它只在受保护组，dev 的跳过列表没有它。

### C5. 确认本地 dev server 加载的是新代码

```bash
curl -s http://localhost:5173/src/components/HeroSection.tsx | grep hero_subtitle
```

**看什么**：

- 返回的源码里有没有新字段。
- 有，说明本地代码已更新，问题不在这里。

### C6. 用“页面的身份”请求接口（关键一步）

```bash
# 直接访问后端
curl -s -i http://127.0.0.1:8040/api/v1/settings/all

# 通过前端 dev server 代理访问（模拟首页请求）
curl -s -i http://localhost:5173/api/v1/settings/all
```

**看什么**：

- HTTP 状态码是不是 200。
- 未登录时是否返回 401。
- 返回体里有没有 `hero_subtitle`。
- 这次匿名请求返回 401，`useSettings()` 取不到值，回退硬编码。

### C7. 检查线上 bundle 到底是不是新版本

```bash
# 找到线上页面引用的 JS
curl -s https://<domain>/ | grep -o 'assets/index-[^"]*\.js'

# 在 bundle 里搜新字段/新文案
curl -s https://<domain>/assets/<bundle>.js \
  | grep -E 'hero_subtitle|about_button_label|基于 React \+ Go'
```

**看什么**：

- 有没有 `hero_subtitle` / `about_button_label`。
- 是否还包含旧硬编码文案。
- 这次线上 bundle 不含新字段、仍含旧文案，说明没部署。

### C8. 修复后构建并重启（会写文件/重启服务）

```bash
go build ./...
go run . start -d ../configs/server -c dev
```

**看什么**：

- 编译是否通过。
- 重启后匿名接口是否 200。
- 前端硬刷新后是否显示新值。

### C9. 线上部署（会发布）

- 构建后端二进制 + 前端 `web dist`（以及 admin dist，如有）。
- 上传/发布后清 CDN 或 Cloudflare 缓存。
- 再执行 C7，确认线上 bundle 已包含新字段。

### 命令使用原则

- **先用只读命令定位，再执行写/部署命令**。
- 验证接口时，用和页面相同的身份（匿名/登录）。
- 本地和线上分别验证；不要用本地结果推断线上。
- 命令输出要保留关键行，否则事后无法复盘。

---

## 3. 根因（两个问题叠加）

### 根因 1：dev 环境下 `/settings/all` 需要登录

- 该路由原先注册在**受保护路由组**，靠各环境的“跳过鉴权列表”放行。
- 生产配置放行了，但 **dev 配置没有放行**。
- 前台首页是匿名访问 → 收到 401 → `useSettings()` 拿不到数据 → **回退到硬编码默认文案**。
- 所以本地改任何设置都不会生效。

### 根因 2：线上还没部署

- 线上前台 JS bundle 里既没有 `hero_subtitle`，也仍然包含硬编码 “React + Go…”。
- 线上跑的还是旧前端，自然看不到新功能。

**两个原因同时成立，所以“本地也不行、线上也不行”。**

---

## 4. 修复

### 4.1 把 `/settings/all` 移到公开路由

```go
// RegisterV1PublicRouters
public.GET("/tags/all", b.TagAPI.GetAll)
public.GET("/categories/all", b.CategoryAPI.GetAll)
// 系统配置（前台首页文案等公共配置，匿名可读）
public.GET("/settings/all", b.SettingAPI.GetAll)

// 受保护路由组中删除：
// setting.GET("/all", b.SettingAPI.GetAll)
```

思路：不要让“是否公开”依赖每个环境的跳过列表；路由本身就应该表达公开语义。

### 4.2 重启后端并验证匿名访问

```bash
curl http://127.0.0.1:8040/api/v1/settings/all
```

返回：

```text
hero_subtitle = 基于 React + Gin 构建的技术博客。|...
hero_title    = Blogo
about_button_label = 关于作者
```

### 4.3 线上

- 需要重新构建并部署后端二进制 + 前端 `web dist`（如有 admin dist 也要）。
- 部署后清 CDN/Cloudflare 缓存，再验证线上 bundle 是否包含新 key。

---

## 5. 验证

- 匿名 `GET /settings/all` 返回 200，且包含最新值。
- 通过前台 dev server 代理再请求一次，同样返回 “Gin”。
- 硬刷新 `http://localhost:5173`（前端 TanStack Query 有 10 分钟 `staleTime`），首页副标题更新。
- 线上验证需要先部署；部署前线上 bundle 不含 `hero_subtitle`，属于预期。

---

## 6. 可迁移的方法

### 方法 1：把“保存成功”拆成写路径和读路径

```text
后台写 → DB 写入 → 读接口 → 前端请求 → 前端渲染 → 部署版本
```

- 写成功只证明“写接口返回 2xx”。
- 读不到要逐层验证：DB 有没有值、接口是否 200、返回体有没有值、前端 bundle 有没有读这个字段。

### 方法 2：用“页面的身份”去请求接口

前台是匿名访问，就用匿名请求验证；不要用带管理员 token 的 curl 验证。**同一个接口，不同身份结果可能完全不同。**

### 方法 3：本地和线上分别验证

- 本地：确认代码路径、路由、鉴权配置。
- 线上：确认 bundle 版本和部署状态。
- 不要用“本地好了”推断“线上好了”。

### 方法 4：区分“用户改错 key”和“系统读错 key”

- 先查数据库，确认用户到底改了哪个 key、值是什么。
- 再查前端实际读取的 key。
- 两者不一致时，是产品/命名问题；两者一致但不生效，才是链路问题。

---

## 7. 知识点卡片

### 卡片 A：Gin 路由组与中间件

```go
public := v1.Group("")
public.Use(AuthMiddleware())   // 如果挂在这里，整组都受保护
```

- 路由注册在哪个 group，决定了它经过哪些中间件。
- “受保护组 + 环境跳过列表”是隐性耦合，容易 dev/prod 不一致。
- 公共接口更稳妥的做法是显式注册在 public group。

### 卡片 B：401 与前端 fallback

```ts
const settings = useSettings()
const title = settings?.hero_title ?? '默认标题'
```

- 接口失败时如果静默 fallback，页面看起来“正常”，但永远是默认值。
- 排查时要看 Network/控制台，不要只看页面。

### 卡片 C：TanStack Query 的 `staleTime`

- `staleTime: 600_000` 表示 10 分钟内认为数据新鲜，可能不会重新请求。
- 硬刷新会清空内存缓存；SPA 内部跳转不一定。
- 但本次主因是 401，不是缓存。

### 卡片 D：前端 bundle 检查

```bash
curl -s https://<domain>/ | grep -o 'assets/index-[^"]*\.js'
curl -s https://<domain>/assets/<bundle>.js | grep hero_subtitle
```

- 线上是否包含某段代码，可以直接从 bundle 验证。
- 这比猜“有没有部署”可靠。

---

## 8. 这次 AI 知道、你可能没注意的点

| 点 | 类型 | 说明 |
|---|---|---|
| 先查 DB 确认保存成功 | 通用调试方法 | 不先改代码 |
| 用匿名请求复现前台身份 | 通用调试方法 | 身份不同结果不同 |
| 检查 dev/prod 的 auth skip 差异 | 框架/配置知识 | 环境不一致 |
| 检查线上 bundle 关键字 | 部署知识 | 确认版本 |
| 静默 fallback 会掩盖错误 | 前端设计知识 | 要暴露失败 |

---

## 9. 下次遇到“保存成功但页面不变”，按这个顺序查

1. 数据库：值到底有没有写进去？
2. 读接口：用页面相同的身份请求，返回 200 吗？返回体有值吗？
3. 前端代码：页面读的是哪个字段？和写入的字段一致吗？
4. 前端缓存：是内存缓存、HTTP 缓存还是 CDN？
5. 部署：线上 bundle 里有没有这段代码？
6. 路由/鉴权：dev 和 prod 的公开路由/跳过列表是否一致？

---

## 10. 复习题

<details>
<summary>Q1：后台保存成功，为什么前台可能读不到？</summary>

写入和读取是两条路径：写入可能成功，但读取接口可能 401、返回旧缓存、读错字段，或者线上前端根本没有读取该字段的代码。必须逐层验证。
</details>

<details>
<summary>Q2：为什么不能只用管理员 token 测公共接口？</summary>

前台是匿名访问。管理员 token 会绕过鉴权问题，导致“接口正常”的假象；用匿名身份才能复现真实用户看到的结果。
</details>

<details>
<summary>Q3：怎么快速判断线上是不是旧版本？</summary>

找到线上页面引用的 JS bundle，下载后搜索新增字段名或新文案。搜不到，说明线上没有部署新代码。
</details>

---

## 11. 证据来源

- 会话：`01a0bf00-c146-7ce2-a715-f2093e7a4de0`（Codex，Blogo）
- 原始事件：line 3387–3474
- 关键文件：`blogo-server/internal/mods/blog/blog.go`（路由注册）、`blogo-web/src/components/HeroSection.tsx`（读取设置）
- 关键验证：`setting` 表、匿名 `GET /api/v1/settings/all`、线上 JS bundle
