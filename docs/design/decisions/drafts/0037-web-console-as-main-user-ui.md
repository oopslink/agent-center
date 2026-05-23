# 0037. Web Console 升为 v2 用户主入口（v2 W1）

| Field | Value |
|---|---|
| Status | Draft |
| Date | 2026-05-23 |
| Related | v2 议题 W1；触发自 [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md)；为 v2 用户接入面（CLI + Web Console）核心组件之一；W2 (CLI UX 增强) 并行 |

## Context

[ADR-0031](0031-v2-drop-bridge-vendor-integration.md) 撤回 Bridge / 飞书后，v2 用户接入只剩 **Web Console + CLI**。现有 [F21 Web Console](../../requirements/01-functional.md) 是**只读 / 查看**设计（task / issue / fleet view 概览），**没有用户主入口能力**：

- ❌ 发起任务 / 创建 channel
- ❌ Channel 聊天 UI
- ❌ 回 InputRequest 卡片
- ❌ Issue 议事讨论
- ❌ CV4 派生入口（选 messages → 开 Issue / 派 Task）
- ❌ Agent / Worker / Secret 管理 UI

→ W1 = 把 Web Console 升级为**全量用户 UI**，覆盖 v2 用户日常核心操作。

### v1 vision 留下的约束

[domain vision § B2](../../architecture/strategic/00-domain-vision.md)：v1 Web Console 仅本机绑定（loopback），SSH 隧道访问。v2 继承：

- 不开放外网；不引入认证（单用户）
- 部署在 center 进程同机（embedded in binary）
- SSH 隧道用户自理

## Decision

### 1. 技术栈：Go API backend + SPA frontend

| 层 | 技术 |
|---|---|
| Backend API | Go HTTP server（同 center binary）；输出 JSON；走 application service |
| Frontend | SPA（React / Vue / Svelte / Solid 等；**具体框架选型留开发计划阶段定**） |
| Realtime | Server-Sent Events (SSE) 推送 message / event 给浏览器 |
| Static assets | SPA build output 通过 `go:embed` 打进 center binary；单 binary 部署 |
| Build pipeline | Frontend 走 npm/pnpm；Makefile 编译时 build frontend → embed → go build |

### 2. 跟 CLI 的关系

**Web Console 跟 CLI 是兄弟前端**（不是 wrapper 关系）：

- 都通过同一套 **application service layer**（go 代码内部 service interface）
- 两者各自有 API surface（CLI = Cobra commands；Web Console = HTTP endpoints）
- 业务逻辑零重复

> Q: 为什么不让 Web Console wrap CLI？
> A: CLI 跟 Web 是不同 surface，输入 / 输出 / error handling 模式各异；wrap 会让 Web Console 内部有 subprocess 调用 + parsing 等过度复杂。Go service layer 直接调用更清晰。

### 3. v2 必备功能 scope

| 功能 | v2 必备 | 备注 |
|---|---|---|
| 看 channel / task / issue 列表 | ✅ | 现有；维持 |
| 看 conversation timeline（任何 kind）| ✅ | message 列表 + sender / posted_at / 内容 |
| **发消息**（任何 conversation kind 都能发）| ✅ | 文本输入 + 发送；支持 markdown |
| **创建 channel**（CV1）| ✅ | name + description form |
| **Invite / Leave / Kick** channel participants（CV2b）| ✅ | UI 列 + add/remove |
| **开 Issue / 派 Task with carry-over**（CV4）| ✅ | 多选父 conv messages → 派生 form |
| **回 InputRequest 卡片** | ✅ | 撤回飞书后唯一入口；UI 显示卡片 + 答复按钮 |
| **看 carry-over 分段**（CV3）| ✅ | UI 渲染子 conv 时分「📥 From parent / 💬 Discussion」段 |
| Fleet view / agent runtime 可视化 | ✅ | 现有；维持 |
| Task trace 概览 | ✅ | 现有；维持 |
| Agent 管理（create / list / config set / archive）| ⏭ | v3+ roadmap；v2 走 CLI |
| Worker 管理 | ⏭ | 同 |
| Secret 管理 | ⏭ | 同 |

### 4. Realtime：SSE

| 场景 | SSE event 类型 |
|---|---|
| 新 message 写入某 conversation | `message_added` (含 conv_id + message body) |
| Conversation status 变化 | `conversation_changed` |
| New InputRequest 等待 | `input_request_pending` |
| Task status 变化 | `task_changed` |
| Agent online / offline | `agent_status_changed` |
| Heartbeat / ping | `ping`（防 connection idle close） |

→ Web Console 一进入 channel page 即订阅该 conversation 的 SSE channel；切换 conversation 时 close + 重新订阅。

### 5. API endpoints（v2 必备子集）

| Endpoint | 说明 |
|---|---|
| `GET  /api/conversations?kind=&participant=` | 列 conversations |
| `GET  /api/conversations/:id` | conversation 详情（含 participants） |
| `GET  /api/conversations/:id/messages?after=&before=` | message 时间线 (paginated) |
| `POST /api/conversations` | 创建 channel (kind=channel) |
| `POST /api/conversations/:id/messages` | 发消息 |
| `POST /api/conversations/:id/participants` | invite |
| `DELETE /api/conversations/:id/participants/:identity_id` | leave / kick |
| `POST /api/conversations/:id/archive` | archive |
| `GET  /api/conversations/:id/sse` | SSE 订阅本 conv 事件流 |
| `POST /api/issues` | 开 Issue（含 `from_conversation` + `select_messages` per CV4） |
| `POST /api/tasks` | 派 Task（同 CV4 carry-over）|
| `POST /api/input_requests/:id/respond` | 回 InputRequest |
| `GET  /api/fleet` | fleet view（现有）|
| `GET  /api/tasks/:id/trace` | task trace（现有）|

> 详细 schema / 错误格式 / 等留开发计划 + 实现阶段定。

### 6. 部署形态：embed in binary

- SPA build output `dist/` → `go:embed dist/* assets/` → 编译进 center binary
- Run `agent-center server` 启动时 HTTP server 同时 serve API + static
- 单 binary 部署不变；用户体验跟 v1 一样（SSH 隧道 + 浏览器访问 localhost）

### 7. 实施 priority（v2 阶段 1-2 划分；细节归开发计划）

| Phase | 内容 |
|---|---|
| **W1.1 核心可用**（v2 必发）| Conversation timeline + 发消息 + Channel CRUD + Participants 管理 + InputRequest 回复 |
| **W1.2 派生支持**（CV4 联动）| CV4 派生 UI（多选 message + form 开 Issue/Task）+ carry-over 分段渲染 |
| **W1.3 现有维持** | Fleet view / task trace / 其他 v1 已有 |
| **W1.4 推迟** | Agent / Worker / Secret 管理 → v3+ |

### 8. v3+ Roadmap

| 项 | 推迟原因 |
|---|---|
| 多用户认证 | v2 单用户场景不需要；v3+ Bridge 重接入时一起做（vendor 用户 mapping 等）|
| Mobile UI | v2 桌面浏览器够用 |
| Web Console Plugin / extension | 复杂度；v2 不需要 |
| Realtime collaborative editing | 不在 agent-center 核心场景 |
| Agent / Worker / Secret 管理 UI | v2 CLI 满足；GUI 可推迟 |

## Consequences

**正面**：

- v2 用户日常操作不再依赖飞书；Web Console 是统一入口
- SPA + Go API 解耦 backend / frontend；后续 frontend 框架升级灵活
- SSE 简化实时推送（无 WebSocket 复杂度）
- 维持 single binary 部署 + SSH 隧道访问；运维不变

**负面 / 待跟进**：

- SPA 引入 npm/pnpm + frontend build pipeline；开发环境复杂度 ↑
- 用户必须熟悉 Web Console UI 才能用 v2 完整功能；CLI-only 用户体验受限
- Web Console 是 v2 必发模块；如果延期，v2 整体不可发
- 设计 + 实现工程量大（多个 page + form + realtime + UI components）；占 v2 开发计划相当比重

## Alternatives Considered

### A. Go SSR + HTMX + minimal JS（早期推荐）

- 跟 v1 vision「简单部署 + 单 binary」对齐；不依赖 node 工具链
- ✅ 工程量较小
- ❌ user 选 SPA（Q1=b）；现代 UI 体验 + frontend 演化空间更大
- 否决（per user 拍板）

### B. Web Console wrap CLI（subprocess）

- Web Console 内部 exec CLI 命令获取数据
- ❌ Subprocess overhead；error handling 双重；output parsing 脆弱
- ❌ DDD 上 Web 跟 CLI 应该并列在 application service 之上
- 否决

### C. 引入认证 / 多用户

- v3+ 配套 Bridge 重接入做
- v2 不引入（vision § B2）

### D. 不做 Web Console，CLI-only 应对 vendor 撤回

- 用户日常完全靠 terminal
- ❌ 聊天 UI 在 terminal 上体验差；Channel 多 message 时不直观
- ❌ InputRequest 卡片回复需结构化 UI
- 否决

## References

### v2 ADRs 相关

- [ADR-0031 v2 Drop Bridge](0031-v2-drop-bridge-vendor-integration.md) - 触发本议题
- [ADR-0032 CV1 Channel](0032-conversation-channel-as-first-class.md) - channel CRUD UI 对接
- [ADR-0033 Identity refactor](0033-identity-model-refactor.md) - actor 显示
- [ADR-0034 Participants](0034-conversation-participants-field.md) - invite UI
- [ADR-0035 CV3 Carry-over](0035-cross-conversation-message-carryover.md) - 分段渲染 UI
- [ADR-0036 CV4 派生入口](0036-derive-issue-task-from-messages.md) - 多选 message + form UI

### v1 已有

- [F21 Web Console](../../requirements/01-functional.md) - 当前 read-only Web Console
- [presentation/01-web-console.md](../../architecture/tactical/presentation/01-web-console.md) - v1 设计；v2 阶段 1.1 实施时 rewrite

### Roadmap

- 多用户认证 / Mobile / etc. 推 v3+

### 来源

- 2026-05-22 → 2026-05-23 #agent-center 讨论
