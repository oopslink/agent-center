# Phase 11 § 3.4 — React SPA Frontend 实施计划

> 起草：2026-05-24 (#6 起手 plan-first) · 状态：**待 x9527 审**
>
> 范围：plan § 3.4 「Web Console SPA 全量 UI」。先落 plan、再 ship 代码。
>
> 上游：plan § 1.4 选型已锁定 `React + Vite + Zustand + react-router v6 + EventSource`；plan § 1.6 page 清单已存在；backend 17 endpoint + SSE Bus 全 ready（commit `9fd19ca`）。

---

## § 0. 范围 + 不做清单

**做**：
- Vite + React + TS scaffolding 在 `web/` 目录
- 12 个 page 实装 + 1 个 404
- 共享组件 (`<ConversationTimeline>` / `<MessageComposer>` / `<ParticipantsPanel>` 等)
- Zustand store + react-query 数据层
- `useSSE` hook 单连接 + subscribe 模型
- Vitest + React Testing Library，覆盖率 ≥ 80%
- Makefile rule: `web/dist/` → `go:embed` → 嵌进 binary
- `docs/design/architecture/tactical/presentation/02-web-console-architecture.md` 同步

**不做**（明示 P11 不范围）：
- SharedWorker multi-tab 共享单一 connection — F7 已 defer 到 v3+
- i18n（plan 隐含 zh/en；本 phase 全 zh，i18n 在 P12 之后）
- 主题切换 / dark mode — 暂只 light
- 移动端响应式调优 — 桌面优先，min-width 1024px
- 浏览器 e2e（Playwright/Cypress）— 测试金字塔顶；写到 F8，跟 P12 e2e suite 一起
- 复用 v1 老 web console 代码 — 全 rewrite per ADR-0037

---

## § 1. Page 清单（13 个；锚 plan § 1.6）

| # | Path | 名称 | 主要内容 | 数据源 |
|---|---|---|---|---|
| 1 | `/` | Home (redirect → `/channels`) | n/a | — |
| 2 | `/channels` | Channel 列表 | 表格：name / status / description / created_at；「新建」按钮 → modal | `GET /api/conversations?kind=channel` |
| 3 | `/channels/:name` | Channel 详情 | timeline + composer + participants 侧边 + invite/leave 按钮 | `GET .../{id}/messages` + SSE |
| 4 | `/channels/:name/derive` | 派生 Issue/Task | 多选 message → 表单 → submit | `POST /api/issues` / `POST /api/tasks` |
| 5 | `/dms` | DM 列表 | 左侧聊天对象列表（agent / supervisor）+ 「新开 DM」 | `GET /api/conversations?kind=dm` |
| 6 | `/dms/:id` | DM 详情 | 类 channel timeline，简化无 participants 面板 | `GET .../{id}/messages` + SSE |
| 7 | `/issues/:id` / `/tasks/:id` | Issue/Task 详情 | timeline + composer + participants (carry-over 分段) | `GET .../{id}/messages` + SSE |
| 8 | `/inputrequests` | InputRequest 收件箱 | 列待回 IR + 「回复」「取消」按钮 | `GET /api/input_requests` |
| 9 | `/secrets` | Secret 管理 | 列表 + 「创建」modal + revoke 按钮；明文仅 create form 内输入 | `GET/POST/DELETE /api/secrets` |
| 10 | `/agents` | AgentInstance 列表 | id / state / name / agent_cli / worker；read-only | `GET /api/agents` |
| 11 | `/agents/:name` | Agent profile | capabilities / mcp_config / skills — read-only | `GET /api/agents/{name}` |
| 12 | `/fleet` | Fleet view | active executions + workers + open IRs + pending issues | `GET /api/fleet` |
| 13 | `/tasks/:id/trace` | Task trace | live trace events for one task | `GET /api/tasks/{id}/trace` + SSE |
| 14 | `*` | 404 | — | — |

> v1 现有的 `/fleet` 跟 `/tasks/:id/trace` **重写**（不继承 v1 代码）— backend 已是 v2 endpoint。
>
> 顶层 layout：左侧 navigation rail（Channels / DMs / Issues / Tasks / InputRequests / Agents / Secrets / Fleet）+ 主区域。

---

## § 2. Page ↔ API 映射

| Page | Endpoint | Method | 用法 |
|---|---|---|---|
| `/channels` | `/api/conversations?kind=channel` | GET | 列表 |
|  | `/api/conversations` | POST (new) — **待 backend 加** | 创建 channel；当前 backend 只 GET，需补 POST |
| `/channels/:name` | `/api/conversations/{id}` | GET | 详情 |
|  | `/api/conversations/{id}/messages` | GET | timeline |
|  | `/api/conversations/{id}/messages` | POST | 发消息 |
|  | `/api/conversations/{id}/participants` | POST | invite |
|  | `/api/conversations/{id}/participants/{identity_id}` | DELETE | remove |
|  | `/api/conversations/{id}/archive` | POST | archive (channel 类启用) |
|  | `/api/sse` + subscribe | GET / POST | 实时 |
| `/channels/:name/derive` | `/api/issues` | POST | 派生 issue (with source_message_ids) |
|  | `/api/tasks` | POST | 派生 task |
| `/dms` `/dms/:id` | `/api/conversations?kind=dm` | GET | DM 同 channel API，filter kind=dm |
| `/issues/:id` `/tasks/:id` | `/api/conversations/{convId}/messages` | GET/POST | issue/task 内部用 conversation 同模型 |
| `/inputrequests` | `/api/input_requests` | GET | 列表 |
|  | `/api/input_requests/{id}/respond` | POST | 回复（含 cancel 的 alt 用 reason=cancel） |
| `/secrets` | `/api/secrets` | GET / POST | list / create |
|  | `/api/secrets/{id}` | DELETE | revoke |
| `/agents` | `/api/agents` | GET | 列表 |
| `/agents/:name` | `/api/agents/{name}` | GET | profile |
| `/fleet` | `/api/fleet` | GET | snapshot |
| `/tasks/:id/trace` | `/api/tasks/{id}/trace` | GET | trace |

**Backend gap 列**（需要本 phase 补 backend）：
- `POST /api/conversations` (channel 创建) — **预计 30 min** in `web/` 起步前补
- `POST /api/conversations/{id}/dm` 或 `POST /api/conversations?kind=dm` (DM 创建) — 同上
- `GET /api/conversations/{id}/derive_candidates` — 不需要；前端按 message_ids selection 即可
- 派生 endpoint 的 `from_message_ids` 参数已 ready（commit `7a9761f` 验证过）

---

## § 3. Route 设计（react-router v6）

```
/                             → <Navigate to="/channels" />
/channels                     → <ChannelList />
/channels/:name               → <ChannelDetail />
/channels/:name/derive        → <ChannelDerive />
/dms                          → <DMList />
/dms/:id                      → <DMDetail />
/issues/:id                   → <IssueDetail />
/tasks/:id                    → <TaskDetail />
/tasks/:id/trace              → <TaskTrace />
/inputrequests                → <InputRequestInbox />
/secrets                      → <SecretList />
/agents                       → <AgentList />
/agents/:name                 → <AgentProfile />
/fleet                        → <Fleet />
*                             → <NotFound />
```

全部在 `<AppLayout>` 内（顶部 header + 左侧 nav rail）。`<AppLayout>` 自身订阅全局 SSE 用于 nav badge（未读 IR count、新 DM 红点）。

**约定**：
- 所有 path 参数解析在 route component 顶部，再传给 hooks；避免组件内多处 `useParams`
- 不用 nested routes for tabs — channel/issue/task 都是独立路径
- protected route 暂无（loopback bind 已是 auth 替代，per ADR-0037）

---

## § 4. State management 边界

清晰三档：

| 数据类 | 工具 | 例子 | 理由 |
|---|---|---|---|
| **Server state**（来自 API） | `@tanstack/react-query` v5 | conversation list / messages / agents / secrets metadata / fleet snapshot | cache + invalidation + retry 是 react-query 强项；reduce store boilerplate |
| **Cross-page UI state** | Zustand single store | `currentUserId` / SSE connection 状态 / nav badge counts / unread per channel | 跨 page 共享但不持久化 |
| **Local component state** | `useState` / `useReducer` | composer 草稿 / modal open / form 输入 / 选中的 message_ids | 不出组件 scope |

**SSE event → state 写入路径**：
- SSE event arrives → store: 决定是 invalidate 还是 patch
- `conversation.message_added` → react-query `queryClient.invalidateQueries(['messages', convId])`（最简）；优化阶段可改为 setQueryData append
- `input_request.created` → invalidate `['inputRequests']` + store 增 badge
- `conversation.archived` → invalidate `['conversation', convId]` + 当前 page 提示

**store 形状（草案）**：
```ts
type AppState = {
  currentUserId: string
  sseStatus: 'idle' | 'connecting' | 'open' | 'reconnecting' | 'closed'
  sseLastEventId: string | null
  navBadges: { inputRequests: number; dms: Record<string, number> }
  // actions
  setSSEStatus: (s) => void
  incInputRequestBadge: () => void
  resetBadges: () => void
}
```

---

## § 5. SSE 接入策略

**单一 EventSource，全 app 一份**。`<AppLayout>` mount 时打开 `/api/sse?user_id=...`；unmount 时 close。`useSSE()` hook 提供：

```ts
function useSSE(): {
  status: SSEStatus
  subscribe: (convId: string) => void   // POST /api/sse/subscribe
  unsubscribe: (convId: string) => void
  events$: Observable<SSEEvent>          // 内部用 ref + listener pool
}
```

实现要点：
- 同一 user 多 tab → 各 tab 各开 connection（SharedWorker 留 v3+）
- 重连：onError → 指数退避（1s / 2s / 4s / max 30s）+ `Last-Event-ID` header (backend Bus ringbuffer 支持 since-all)
- 心跳：SSE 协议自带 keepalive；超时 60s 触发重连
- 路由切换：`<ChannelDetail>` mount → `subscribe(convId)`；unmount → `unsubscribe(convId)`
- 错误：`onError` 写 store.sseStatus，UI 显示「连接断开，重连中…」banner

事件分发：page 组件用 `useSSEEvent(type, handler, deps)` 订阅特定 event type；hook 内部从全局 event stream filter。

---

## § 6. 测试策略

**目标覆盖率 ≥ 80%**（line + branch；plan § 4 DoD 门槛）。工具：

| 层 | 工具 | 范围 |
|---|---|---|
| Unit | Vitest | 纯函数 / Zustand store / 工具方法（formatTime / parseSSE event） |
| Component | RTL + Vitest | 每个 page 至少 1 个 happy 渲染测；form 提交；error / loading 三态 |
| Hook | RTL renderHook | useSSE / useSSEEvent / 自定义 hooks |
| Integration | Vitest + MSW (Mock Service Worker) | page-level：mock API → render → assert UI；SSE 用 EventSource mock |
| E2E (browser) | **F8 deferred** | Playwright；跟 P12 一起 |

**MSW handler 文件**：`web/src/mocks/handlers.ts` 模拟所有 17 endpoint；测试用同一 mock set，不重复造。

**SSE 测试**：`mock EventSource` 自实现一个 fake class（reuses `EventTarget`）；handlers 触发 `dispatchEvent(new MessageEvent(...))`。验证 component 收到 + 更新 state。

**Coverage gate**：`vitest --coverage` 输出 + CI fail < 80%。当前无 frontend CI，本 phase 顺手补一个 npm script `npm run test:ci` 跑 coverage check。

---

## § 7. 子任务拆分（按依赖排序，可串行 ship）

| ST# | 子任务 | 依赖 | 关键工件 | DoD |
|---|---|---|---|---|
| F1 | Scaffold + tooling | — | `web/` (Vite + TS + React) + `package.json` + `tsconfig.json` + `vitest.config.ts` + `.gitignore` + `web/dist/.gitkeep` | `npm run dev` 起 5173；`npm run test` 跑空套件 OK |
| F2 | Backend gap：`POST /api/conversations` (channel + dm) | — | `internal/webconsole/api/handlers.go` + 单测 | api 覆盖率不掉；endpoint 实测建 channel/dm |
| F3 | App shell + routing + layout | F1 | `<AppLayout>` + `<NavRail>` + react-router 全 path stub + 404 | 14 path 跑通；nav 切换；每 stub 渲染 placeholder |
| F4 | API client + react-query + store | F1 | `web/src/api/client.ts` (fetch wrapper) + `useQuery` helpers + Zustand store skeleton + MSW handlers stub | API client 单测；store action 单测 |
| F5 | useSSE hook + connection lifecycle | F1, F4 | `web/src/sse/useSSE.ts` + 自定义 hook tests (fake EventSource) | reconnect 指数退避测；subscribe/unsubscribe 测 |
| F6 | Page: ChannelList + ChannelDetail + composer + participants | F3, F4 | `<ChannelList>` `<ChannelDetail>` `<MessageComposer>` `<ParticipantsPanel>` | 浏览器手测：建 channel / 发消息 / invite；RTL 测三态 |
| F7 | Page: DMList + DMDetail | F6 | `<DMList>` `<DMDetail>` (复用 ConversationTimeline + Composer) | 手测：开 DM / 发消息 |
| F8 | Page: Issue/Task detail | F6 | `<IssueDetail>` `<TaskDetail>`（复用 ConversationTimeline；增 carry-over 分段 UI） | 手测：派生 issue → 跳到 issue page 看 carry-over |
| F9 | Page: ChannelDerive (多选派生) | F6 | `<ChannelDerive>` + message 多选 UI | 手测：多选 → 开 issue/task |
| F10 | Page: InputRequest inbox + 回复 modal | F4 | `<InputRequestInbox>` + `<RespondModal>` | 手测：从 IR list 回复 → IR 状态变 |
| F11 | Page: Secret CRUD + create modal | F4 | `<SecretList>` + `<SecretCreateModal>` + revoke confirm | 手测：建 secret / list / revoke；明文不出现在 list / show response |
| F12 | Page: Agent list + profile + Fleet + TaskTrace | F4 | `<AgentList>` `<AgentProfile>` `<Fleet>` `<TaskTrace>` | 手测 4 个 page |
| F13 | SSE event 接入到各 page (badge / invalidation) | F5, F6-F12 | event handler registration in pages | 手测：另一 tab 发消息 → 本 tab 列表/页面更新 |
| F14 | 覆盖率冲刺到 ≥ 80% | F1-F13 | 补 missing unit / component tests | `npm run test:ci` 输出 ≥ 80% line+branch |
| F15 | Makefile rule + `go:embed` 接入 | F1, F14 | `Makefile` rule `web` + `cmd/agent-center` 嵌 `web/dist/` + serve static | `make build` → binary serve `/` static 文件正常 |
| F16 | `02-web-console-architecture.md` 写 + `01-web-console.md` rewrite | F15 | tactical/presentation docs 2 个 | 两文档归档 + 在 plan 文档引用 |

**串行依赖**（不能省）：F1 → F2 (并行可) → F3 → F4 → F5 → F6 → F7..F12 (内部可并行 if 单人单线则串行) → F13 → F14 → F15 → F16。

---

## § 8. 工作量预估 + commit 里程碑

每个 ST 一个 commit，commit message 前缀 `feat(p11 frontend F#)`。

| ST# | 估时 | commit message |
|---|---|---|
| F1 | 1.5h | `feat(p11 frontend F1): vite + react + ts scaffold` |
| F2 | 1.5h | `feat(p11 § 3.2): POST /api/conversations (channel + dm) endpoint` |
| F3 | 2h | `feat(p11 frontend F3): app shell + react-router + 14 path` |
| F4 | 2.5h | `feat(p11 frontend F4): api client + react-query + zustand store + MSW` |
| F5 | 2h | `feat(p11 frontend F5): useSSE hook + reconnect + subscribe model` |
| F6 | 4h | `feat(p11 frontend F6): channel pages + composer + participants` |
| F7 | 2h | `feat(p11 frontend F7): dm pages` |
| F8 | 2.5h | `feat(p11 frontend F8): issue + task detail pages` |
| F9 | 2h | `feat(p11 frontend F9): channel derive (issue/task carry-over UI)` |
| F10 | 2h | `feat(p11 frontend F10): input-request inbox + respond modal` |
| F11 | 2h | `feat(p11 frontend F11): secret CRUD + create modal (plaintext never echoed)` |
| F12 | 3h | `feat(p11 frontend F12): agents + fleet + task trace pages` |
| F13 | 2h | `feat(p11 frontend F13): SSE event handlers wired into pages` |
| F14 | 3h | `test(p11 frontend F14): coverage push to ≥ 80%` |
| F15 | 1.5h | `feat(p11 frontend F15): Makefile + go:embed + serve static` |
| F16 | 2h | `docs(p11 § 3.10): web-console.md + web-console-architecture.md` |

**合计**：35.5h ≈ **5 个工作日**（按 7h/d）。

**里程碑**：
- M1 (F1-F5)：shell + 数据层 + SSE 完整可 demo，~10h
- M2 (F6-F12)：所有 page 实装，~17.5h
- M3 (F13-F14)：SSE + 测试达标，~5h
- M4 (F15-F16)：embed + docs，~3.5h

---

## § 9. 风险

| 风险 | 缓解 |
|---|---|
| react-query v5 + Zustand 双重 state，新人易混淆 | § 4 边界表写死；code review 卡 |
| SSE EventSource 浏览器实现差异（Safari 心跳行为） | onError 全统一兜底重连；不依赖具体心跳行为 |
| `go:embed` 把 SPA bundle 嵌进 binary 后 size 暴涨 | F15 测算 size 写到 release checklist；初版 gzip + chunk 拆分 |
| 单人工 5 day 估时偏乐观 | M1 (10h) 是关键路径；如 M1 超 15h 触发 re-plan，跟 x9527 同步 |
| MSW 在 Vitest 环境的 polyfill 问题 | 用 `@mswjs/interceptors` + `vitest --environment jsdom`，pinned 版本 |
| 浏览器 e2e (F8 真 e2e Playwright) 未列入 | 显式 defer 到 P12；本 phase 仅 vitest + RTL |

---

## § 10. Open questions（待 x9527 拍板才能动手）

1. **创建 channel 的 backend POST endpoint**：是 § 2 列的 backend gap。是 F2 子任务先补一个 commit，还是当成 P12 cleanup？(我倾向 F2 先补，因为 `/channels` page UI 直接依赖)
2. **DM 创建 endpoint**：同上 — DM 跟 channel 创建分两个 endpoint 还是 `kind=` 参数？
3. **Auth**：当前 backend loopback bind = 唯一 auth。frontend 是否需要在 nav 显示「单用户模式」提示？还是完全透明？
4. **样式**：plan § 1.4 "shadcn/ui or Tailwind 直手；具体实施时定"。我倾向 **Tailwind 直手 + 少量 headlessui 原语**（避免 shadcn 引一堆 dep）；可否拍板？
5. **404 page**：是简单 "Not Found" 还是带 link 回 nav 的卡片？(倾向简单)

---

## § 11. DoD（本 plan 自身的）

- [ ] x9527 审过本 plan + 选定 open questions
- [ ] commit 本文档到 `docs/plans/phase-11-frontend-plan.md`
- [ ] 主道 thread 贴摘要 + open questions
- [ ] 等 x9527 放执行 → 进入 F1
