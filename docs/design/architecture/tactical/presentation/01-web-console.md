# Web Console

> **DDD 战术层** · 主题: presentation
>
> **状态**: v2 完成（P11 ship'd）— React SPA 嵌入单 binary，loopback bind only

## 定位

| 维度 | 决定 |
|---|---|
| 进程形态 | **嵌入 `agent-center server` 进程**（同二进制，go:embed SPA，独立端口） |
| 网络绑定 | **仅 loopback** — `127.0.0.1:7100` 默认，server 主动 reject 任何非 127.0.0.1 bind（ADR-0037 / NF2）|
| 访问方式 | 用户通过 **SSH 隧道**从笔记本浏览器访问；隧道由用户自理 |
| 认证 | **无**（loopback = 本机用户信任域，单用户 vision B2）|
| TLS | 不需要（loopback 本地）|
| 配置 | `web_console.enabled` (默认 false) + `web_console.listen_addr` (默认 :7100) — see `implementation/04-configuration.md` |

跟 [admin CLI 本机姿态](../agent-harness/02-skill-cli-tooling.md) 一致 — 零认证负担、零公网暴露、零证书管理。

## v2 范围（实装）

| 类型 | Path | 实现内容 |
|---|---|---|
| **聊天** | `/channels` `/channels/:name` | Channel 列表 / 创建 / 详情 (timeline + composer + participants 三栏) |
| **聊天** | `/dms` `/dms/:id` | DM 列表 / Start DM modal (peer + agent quick-pick) / 详情 |
| **派生** | 任意 conversation detail | "Select messages" 模式 → 多选 → Open Issue / Open Task (CV4) |
| **跟踪** | `/issues` `/issues/:id` | Issue list + 状态 filter + detail (carry-over divider + 讨论) |
| **跟踪** | `/tasks` `/tasks/:id` `/tasks/:id/trace` | Task list + detail + 独立 trace timeline (collapsible events) |
| **响应** | `/inputrequests` | 待回 IR 收件箱 + Respond modal + Cancel (确认) + 实时 badge |
| **管理** | `/secrets` | Secret list / Create modal (no-plaintext-echo) / Revoke confirm |
| **可观测** | `/agents` `/agents/:name` | Agent list (state filter) + profile (匹配 fleet 当前 exec) — read-only |
| **可观测** | `/fleet` | Workers / Active executions / Open IRs / Pending issues 4-segment + Warnings banner |

完整 ST → page 映射见 [phase-11-frontend-plan.md](../../../../plans/phase-11-frontend-plan.md)。

### 不在 v2 范围

- 高级时间轴可视化（flamegraph / Gantt） — v3
- Prometheus / Grafana 度量集成 — v3
- 远程访问（公网监听 + 认证） — v3+
- Multi-tab SharedWorker SSE — v3+
- Unread message tracking — v2.1 ([v2.1-backlog](../../../../plans/v2.1-backlog.md))

## 技术形态

- **前端**: React 19 + Vite 5 + TypeScript strict
- **状态层**: 三档分离 — react-query (server) / Zustand (cross-page UI) / `useState` (local)
- **路由**: react-router v7 with lazy chunks per page
- **实时**: 单一 EventSource per app instance + `useSSE` hook + 指数退避重连 + Last-Event-ID query 参数恢复
- **后端**: 嵌在 server 进程；17 个 `/api/*` endpoint + `/api/sse` 单连接 + 其余 path SPA fallback
- **打包**: go:embed `internal/webconsole/spa/dist`；不引入额外 binary，遵循 [conventions § 10 单一二进制 / 多模式](../../../../rules/conventions.md)
- **测试**: vitest + RTL + MSW (Node setupServer)；coverage 80% gate；188 tests / 98.6% lines

详细架构 → [02-spa-architecture.md](./02-spa-architecture.md)

## 安全

- **No-plaintext-echo (ADR-0026 § 5)**: Secret value 全栈不外泄 —
  - backend `secretPublicMap` 不含 value；create response 仅 `{id, name, event_id}`
  - frontend value field type=password + autocomplete=off + spellCheck=false；submit 后立刻 clear state；success banner 只显名字
  - MSW handler 镜像 backend shape；mock-shape audit 测验证无 `value` / `plaintext` key
- **Loopback bind guard**: `api.Server.ListenAndServe` 强制 host ∈ {127.0.0.1, localhost} 否则 refuse
- **SSE Bus**: per-user 单连接（新连接 replaces 旧）；ringbuffer 限上限防内存爆

## 配置

```yaml
# agent-center server config (server.yml)
web_console:
  enabled: true                  # 默认 false
  listen_addr: 127.0.0.1:7100    # 默认；非 127.0.0.1 会被 server 主动拒
```

启动后：浏览器开 `http://127.0.0.1:7100/`（开 SSH 隧道转发即可远程访问）。

## 跟 CLI 的关系

- CLI / Web Console **两个独立的前端**，背后同一份 server / DB / BlobStore
- 三层 mutation 边界一致 — 创建 / 改 / archive 的入口都同一个 domain service
- CLI 是 power-user / scripting / supervisor 自动化首选；Web Console 是查看 / 派生 / 应答 IR 等鼠标交互友好场景

## 自检（按 conventions § 15）

- [x] § 1 单一来源：Web Console 不绕过 center 造任务，所有 mutation 走 application service
- [x] § 2 可观测性优先：每次操作 emit 事件，SSE Bus 同步推 frontend
- [x] § 3 AI Native：Web Console 不是给 agent 用的；agent 走 CLI / supervisor
- [x] § 4 零 LLM SDK：前端无 LLM 调用
- [x] § 5 BlobStore：log / trace artifact 通过 backend endpoint → frontend 链接到 BlobStore URL
- [x] § 10 单一二进制：嵌入 server 进程，go:embed SPA
- [x] § 11 渠道：Web Console 是查询 + 派生面板，事件 / Issue / InputRequest 仍走原通道
- [x] § 13 安全：loopback 绑定 + secret 不外泄

## 见也见

- [02-spa-architecture.md](./02-spa-architecture.md) — React + state + SSE 实现细节
- [phase-11-frontend-plan.md](../../../../plans/phase-11-frontend-plan.md) — 16-ST 实施 plan
- [phase-11-test-report.md](../../../../plans/reports/phase-11-test-report.md) — 收口 test report
- [sse-wiring-audit.md](../../../../plans/sse-wiring-audit.md) — SSE event ↔ dispatch table audit (F13)
- [spa-coverage-audit.md](../../../../plans/spa-coverage-audit.md) — 覆盖率残余分类 (F14)
- [v2.1-backlog.md](../../../../plans/v2.1-backlog.md) — 显式 defer 到 v2.1 的 item
- [ADR-0037](../../decisions/drafts/0037-web-console-loopback-only.md) — loopback bind rationale
