# Phase 11: 用户入口面（Web Console + CLI 等价）

> 入口层（Presentation） · 依赖 Phase 8 + 9 + 10 · 解锁 Phase 12
> 纪律：按里程碑顺序 / 模块完备不半成品 / TDD + 单测 ≥ 90% + 集成 + e2e + 测试报告

覆盖 ADR：[ADR-0037 Web Console 升 v2 用户主入口](../design/decisions/drafts/0037-web-console-as-main-user-ui.md) · [ADR-0038 CLI UX 增强](../design/decisions/drafts/0038-cli-ux-enhancement.md)

## § 0. 目标

把 Phase 8-10 的业务模型给用户用：

- **Web Console SPA 全量 UI**：聊天 (channel timeline + 发消息 + SSE 实时) / 派生 UI (多选 message → 开 Issue/Task) / InputRequest 卡片回复 / Channel CRUD + Participants 管理 / 现有 fleet view + trace 维持
- **CLI 全套补齐**：conversation send / tail -f / input-request respond/list/show/cancel / channel CRUD 别名 / --format flag / stdin 输入约定
- **两者兄弟前端**：都过 application service，无依赖

vendor 撤回（per ADR-0031）→ Web Console + CLI 是用户接触系统的**唯一**入口。

## § 1. DDD 工件清单

### 1.1 - 1.3 AR / Entity / VO

无新增 —— 入口层不引入业务 AR。Application Service 层用 Phase 8-10 的领域工件。

### 1.4 Repositories

无新增；用 Phase 8-10 的 Repository。

### 1.5 Application Services（HTTP / CLI 共用）

| Service | 来源 ADR | 说明 |
|---|---|---|
| **ConversationAppService** | 0037+0038 | List / Show / Send / Tail / Archive / 等；Web + CLI 共用 |
| **ChannelAppService** | 0032+0034+0037 | CRUD + invite/leave/kick |
| **DerivationAppService** | 0036+0037 | issue/task open from messages |
| **InputRequestAppService** | 0037+0038 | list / show / respond / cancel |
| **AgentInquiryAppService** | 0037 (read-only) | agent list / show（仅查；admin 类操作不入 Web；走 CLI）|

### 1.6 Presentation / Frontend

**Web Console（SPA）**：

| 模块 | 备注 |
|---|---|
| Frontend framework | **React**（2026-05-23 用户拍板）|
| Build pipeline | Vite + Makefile rule: frontend build → go:embed → go build |
| State management | Zustand（轻量；不引入 Redux 全家桶）|
| Routing | react-router v6 |
| SSE client | EventSource API；自定义 hook 按 conversation_id 订阅 |
| UI components | shadcn/ui or Tailwind 直手；具体实施时定 |

**Web Console pages（v2 必备）**：

- `/channels`：channel 列表 + 「新建」按钮
- `/channels/:name`：channel timeline + 发消息 + participants 侧边 + invite/leave 按钮 + SSE 实时
- `/channels/:name/derive`：多选 message → 开 Issue/Task form
- `/issues/:id`、`/tasks/:id`：同样 timeline + 发消息 + participants（carry-over 分段显示）
- `/inputrequests`：列待回 InputRequest + 回卡片
- `/fleet`：v1 现有；维持
- `/tasks/:id/trace`：v1 现有；维持

### 1.7 Domain Events（基本无新增）

Web Console 不 emit 业务事件；只订阅。

### 1.8 Context Map 关系

Web Console + CLI 都通过 application service → 调用 Phase 8-10 的 domain services。**不引入新 BC**。

## § 2. 上游依赖

来自 Phase 8-10 的几乎所有工件：

- AgentInstance / Channel / Conversation / Message / Issue / Task / InputRequest / etc
- 全部 CLI 命令前面 phase 已设计；本 phase 实装 + 补 conversation send / tail / input-request respond

## § 3. 工作项分解

### 3.1 ~~SPA 框架选型 spike~~ → React 已锁定（2026-05-23 用户拍板）

- **工件**：(无 spike)
- **决定**：React + Vite + Zustand + react-router v6 + EventSource (per § 1.6)
- 实施开始时 `npx create-vite web --template react-ts` 初始化

### 3.2 Web Console backend HTTP API

- **工件**：`internal/webconsole/api/*.go`
- **依赖**：Phase 8-10 application services
- **endpoints**（per ADR-0037 § 5）：
  - GET /api/conversations?kind=&participant=
  - GET /api/conversations/:id
  - GET /api/conversations/:id/messages?after=&before=
  - POST /api/conversations
  - POST /api/conversations/:id/messages
  - POST /api/conversations/:id/participants
  - DELETE /api/conversations/:id/participants/:identity_id
  - POST /api/conversations/:id/archive
  - GET /api/conversations/:id/sse
  - POST /api/issues
  - POST /api/tasks
  - POST /api/input_requests/:id/respond
  - GET /api/input_requests?pending
  - GET /api/fleet
  - GET /api/tasks/:id/trace
  - GET /api/agents
  - GET /api/agents/:name
- **DoD**：所有 endpoint 单测 + 集成测

### 3.3 SSE 推送实现

- **工件**：`internal/webconsole/sse/`
- **步骤**：
  1. EventSink listener for `conversation.message_added` / `input_request_pending` / `agent_status_changed` / `task_changed` 等
  2. Per-conversation subscriber pool
  3. SSE message format ({event_type, data})
  4. Heartbeat ping 防 idle close
- **DoD**：浏览器 EventSource 接收事件；集成测覆盖

### 3.4 SPA frontend 实装

- **工件**：`web/` 目录（frontend source）+ Makefile rule
- **依赖**：3.1 + 3.2 + 3.3
- **pages** (per § 1.6)
- **build**: npm/pnpm install → build → output `web/dist/` → go:embed
- **DoD**：所有 page 可用；端到端浏览器测（playwright / cypress）

### 3.5 CLI conversation send + alias

- **工件**：`internal/cli/conversation/`
- **依赖**：Phase 10
- 通用 conversation send + alias channel post / issue comment / task comment
- stdin / `--file` 支持
- **DoD**：CLI 全 e2e

### 3.6 CLI conversation tail -f

- polling 实现（admin endpoint）
- --follow / --since / -n flags
- **DoD**：跟 SSE 等价用户体验；e2e

### 3.7 CLI input-request

- list / show / respond / cancel commands
- --format flag
- **DoD**：CLI e2e；替代飞书卡片功能完整

### 3.8 CLI --format flag 通用支持

- 所有 list / show / participants / refs / inspect 类命令加 --format=table|json|yaml
- 默认 table
- **DoD**：每命令 3 格式测试通过

### 3.9 CLI help / discoverability

- 命令树扁平化整理
- `--help` 全树 + per-group help
- **DoD**：用户 `agent-center --help` 可发现所有 v2 命令

### 3.10 documentation update

- `docs/design/architecture/tactical/presentation/01-web-console.md` rewrite v2 形态
- `docs/design/architecture/tactical/agent-harness/02-skill-cli-tooling.md` 含 v2 新 CLI

## § 4. Definition of Done

- [ ] § 3.1 框架选型敲定 + 跟其他 phase 同步
- [ ] § 3.2-3.4 Web Console 全 page + API 全功能
- [ ] § 3.5-3.9 CLI 全套补齐
- [ ] 单测 ≥ 90% (Go side; frontend 测试单独跑 Jest/Vitest)
- [ ] SSE 实时推送 e2e 通
- [ ] 派生 UI: 选 messages → 开 Issue with carry-over 全链路 e2e
- [ ] InputRequest 卡片 UI + CLI 都能回 +取消
- [ ] CLI / Web Console 命名一致性自检 (per ADR-0038 § 7)
- [ ] phase-11-test-report.md 归档

## § 5. 测试计划

### 5.1 单测

| 工件 | 场景 |
|---|---|
| HTTP handlers | 所有 endpoint 路径正常 + 异常 + 权限 |
| SSE 推送 | event dispatch；多 subscriber broadcast |
| CLI commands | 各命令 stdin / file / flag 解析；--format 三格式 |

### 5.2 集成测

| 场景 |
|---|
| HTTP → Application Service → Domain Service → DB 全链路 |
| SSE 端到端推 |
| CLI command → application service → DB |

### 5.3 e2e (browser + cli)

| 场景 |
|---|
| 用户 Web Console 创建 channel + invite agent + 发消息 + agent reply 实时收到 (SSE) |
| 用户在 channel 多选 messages → 派 Issue with carry-over → child page 显示分段 |
| 用户在 Web Console 回 InputRequest 卡片 |
| 用户 CLI `conversation tail -f` 实时跟随；另一终端 `conversation send` 注入消息 |
| 用户 CLI `input-request respond` 回复 task 卡 |

## § 6. 风险

| 风险 | 缓解 |
|---|---|
| Frontend 框架选型若改变需 rebuild | spike 阶段决定后冻结；本 phase 不允许中途换 |
| SSE 连接管理 (idle / disconnect / reconnect) | 客户端 reconnect with last-event-id；服务端 keep-alive ping |
| Web Console 跟 CLI 命令名 drift | ADR-0038 § 7 命名一致性自检；CI 加 lint |
| Frontend build pipeline 复杂 (node/npm 依赖) | Dockerfile / Makefile 自包；CI 内 build |
| 长连接 / SSE 性能 (多 channel 订阅) | per-conn limit；监控 connection 数 |

## § 7. 下游解锁

本 phase 完成后 **Phase 12 Cleanup + Release** 可启动：v2 全部 user-facing feature ready → e2e suite 跑全链路 + release prep。
