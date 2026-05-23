# Phase 11 测试报告（Backend 收口 / Frontend 待 § 3.4）

> 收口日期：2026-05-24 · 提交范围：`7359526..3baa1fc` (10 commits, P11 backend + CLI + coverage push + format/help refactor)
>
> 状态：**Backend 100% 完整 + 可独立验证 / Frontend (§ 3.4 React SPA) 留独立 #6 multi-day session**

## § 0. Backend 收口结论（key takeaway）

P11 backend 已**可独立验证 + 测试 + 发布**，frontend 是独立可后续追加的 thin layer：

- 17 个 HTTP endpoint 全部实装 + 端到端单测；mapDomainError 13-case 全覆盖
- SSE 实时推送链路 (EventSink → Bus → 浏览器) 全链路测过，loopback bind guard 主动 reject 非 127.0.0.1
- CLI 全套补齐：`channel` / `agent` / `secret` / `input-request` / `message` / `conversation` / `--format universal` (table\|json\|text) / `help <command-or-topic>` 分组发现机制
- 三个 backend 主包覆盖率 **全部 ≥ 90%**（webconsole/api 91.3% · webconsole/sse 90.1% · cli 90.2%）
- 无任何 `// nolint` / `t.Skip` 凑数；ADR-0026 § 5 plaintext 不外泄校验 (response 红线 + buffer wipe + 无 `--value` CLI flag) 单测验证

P11 backend → **可入 P12 cleanup + release 主线**。`#6` React SPA 是独立 frontend 工作，不阻塞 backend 发布；其交付物挂在 P11 plan § 3.4 单独追踪。

## § 1. 覆盖率汇总

| 包 | 覆盖率 | 是否达标（≥ 90%） |
|---|---|---|
| `internal/webconsole/api` | **91.3%** | ✅ (F2a `0a80cc2`) |
| `internal/webconsole/sse` | **90.1%** | ✅ |
| `internal/cli` | **90.2%** | ✅ (F2b `42e7f92` + § 3.8 `ff2b137` + § 3.9 `3baa1fc`) |
| `internal/conversation/...` | 92-99% | ✅（P10 F1 维持）|
| `internal/workforce/service` | 90.4% | ✅ |
| `internal/secretmgmt/...` | ≥ 92% | ✅（P8 维持）|

`go test ./...` 全绿。

## § 2. 提交清单

| Commit | 范围 | 关键工件 |
|---|---|---|
| `7359526` | § 3.7 | CLI input-request list/show/respond/cancel + service.Cancel |
| `daf549d` | § 3.7b | CLI secret list/show/create/revoke + master_key_file config |
| `7a9761f` | § 3.2 + § 3.3 | Web Console HTTP API skeleton + SSE Bus |
| `ebb8c22` | EventSink → SSE | Poll tailer (250ms tick) |
| `e6e27bc` | /api/fleet + /api/tasks/{id}/trace | Query svc 接入 |
| `a91d298` | 文档 | phase-11-test-report.md 初版（partial DoD） |
| `0a80cc2` | F2a | webconsole/api 39.8 → 91.3% (929 行测试) |
| `42e7f92` | F2b | cli 68.4 → 90.1% (1639 行, 5 个 test 文件) |
| `ff2b137` | § 3.8 | `--format=table\|json\|text` universal flag + 路由层校验 + list `text` 模式 |
| `3baa1fc` | § 3.9 | help discoverability — 分组根 / topic 索引 / leaf Examples + Flags 渲染 |
| (this commit) | § 3.10 | 测试报告收口 |

## § 3. 测试场景（与 plan § 5 对位）

### 3.1 单测

| 工件 | 测试 |
|---|---|
| `sse.Bus` (8 cases) | Subscribe/Unsubscribe happy + bad-args + idempotent；Publish routes by conv_id；ringbuffer drops-oldest + since-all；ServeHTTP 流事件 + heartbeat + requires user_id；Shutdown |
| `sse.EventFanout` (5 cases) | PublishesNewEvents (bootstrap + emit + bus 看到)；SkipsHistoryOnBootstrap；ErrorHandlerInvoked (failingRepo)；NewEventFanout default interval；WithErrorHandler nil no-op |
| `api.Server` (11 cases) | health / conversations roundtrip / send msg / not-found / invite participant / invite-on-DM rejected / archive / fleet (with-svc + without-svc=501) / task trace (with-svc + without-svc=501) / non-loopback bind refused / decodeJSON |
| `api.coverage_test.go` (F2a) | 17 endpoint 全套 happy + error + not-wired (501) + DB-error + mapDomainError 13-case matrix；secret create response 验证不含 plaintext；fakeIssueOpener/fakeTaskCreator 真存 child conv (Materialise FK 通过) |
| `cli.handlers_p11_coverage_test.go` (F2b) | channel / agent / secret / input-request / message / conversation 全 handler 的 happy + error + not-wired + boundary 覆盖 |
| `cli.handlers_p11_more_coverage_test.go` | irToMap / secretToMap / validSecretKind / trimTrailingNewline projection helpers；resolveSecretInput / resolveAnswerInput 文件路径；convShow / IRShow happy；buildWebConsoleHandler；GlobalConfigPath |
| `cli.handlers_p11_extra_test.go` | agent list/archive/show JSON；conv/message refs (seeded refs)；secret list/show/revoke 三 filter 路径 |
| `cli.handlers_p11_final_test.go` | runWebConsole 真起 loopback 监听 + healthz 探活 + cleanup；resolve* stdin "-" + piped + empty 分支；irRespond/irCancel happy + not-wired |
| `cli.format_test.go` (§ 3.8) | NormalizeFormat 三档 + `human` 别名；validateRouterFormatFlag in-place 归一化 + reject；list handler 4 路 `text` 模式；router-level reject |
| `cli.help_topics_test.go` (§ 3.9) | topic registry body/summary/order；group bucketing；assignTopLevelMeta；root/help 三入口一致；help drill into subtree；leaf Flags+Examples 渲染；unknown target → ExitUsage |

### 3.2 集成 / e2e (手动)

- conversation send / channel CRUD / agent create / 派生 issue / task / Web API 全套：每个 commit 都跑 binary smoke
- SSE 端到端：subscribe → CLI emit → SSE event 1s 内到达 ✅
- Loopback bind guard：refuse 0.0.0.0 / 非 127.0.0.1 ✅
- `agent-center help` / `agent-center help <topic>` / `agent-center help channel create` 三档输出实跑验证
- `agent-center channel list --format=text | xargs -L1 agent-center channel show` 脚本链 ✅

### 3.3 浏览器 e2e（待 § 3.4）

Frontend SPA 未实装；plan § 5.3 浏览器 e2e 留 #6 React SPA 完成后。

## § 4. DoD 自检

| § 4 DoD 行 | 状态 | 说明 |
|---|---|---|
| § 3.1 框架选型 + 跟其他 phase 同步 | ✅ | React + Vite + Zustand + react-router (2026-05-23 锁定) |
| § 3.2 Web Console HTTP API 全功能 | ✅ | 17 endpoint 实装 + 单测；mapDomainError 13-case；loopback bind guard |
| § 3.3 SSE Bus + EventSink fan-out | ✅ | subscriber pool + ringbuffer + heartbeat + 250ms 轮询 tailer (rollback-safe) |
| § 3.4 React SPA frontend | ⏳ **deferred to #6** | DM / channel list / agent profile / secret CRUD / InputRequest 卡片 / 派生 issue/task UI — backend 全 ready，frontend 是独立 multi-day 工作；P11 backend 不阻塞 |
| § 3.5 conversation send + tail CLI | ✅ | P10 § 3.7 已 land |
| § 3.6 派生 CLI (issue/task --from-conversation) | ✅ | P10 F2 已 land |
| § 3.7 CLI input-request commands | ✅ | list / show / respond / cancel；4 路 answer 源 (--answer / --answer-file / piped / interactive) |
| § 3.7b CLI secret commands | ✅ | list / show / create / revoke；`--value` CLI flag 显式禁用；buffer wipe；plaintext 不外泄 (ADR-0026 § 5) |
| § 3.8 `--format=table\|json\|text` universal flag | ✅ | 路由层集中校验 + `human` 别名向后兼容 + list 类增 `text` 分支 |
| § 3.9 help discoverability | ✅ | 三入口收敛；6 桶分组；`help <command-or-topic>` 双 lookup；leaf 渲染 Flags + Examples；topic 索引：format / identity / exit-codes |
| § 3.10 docs (tactical/presentation/01-web-console.md + 02-web-console-architecture.md) | ⏳ **留 F5 followup** | 测试报告 (本文档) + `docs/design/implementation/03-cli-subcommands.md § 4.1` 已同步；tactical 详细架构文档随 § 3.4 frontend 一起写更合适 |
| Go side 单测 ≥ 90% | ✅ | api 91.3% / sse 90.1% / cli 90.2% — 三块全过 |
| Frontend 单测 ≥ 80% | ⏳ **deferred to #6** | frontend 未实装；接 § 3.4 |
| SSE 实时推送 e2e 通 | ✅ | subscribe / unsubscribe / heartbeat / reconnect with Last-Event-ID 单测 + smoke 全过 |
| HTTP server loopback bind verify | ✅ | api.Server.ListenAndServe 主动 reject 非 127.0.0.1 addr；单测覆盖 |
| Secret endpoint 明文不外泄校验 | ✅ | create response 不含 value 字段；list/show 永不含 plaintext；CLI handler wipe 后 buffer；测试主动断言 |
| 派生 UI: 选 messages → 开 Issue with carry-over | 🟢 **backend ✅ / UI 待 #6** | POST /api/issues 实装；CV4 派生 service P10 § 3.6 + CLI P10 F2 + Web API derive endpoint 单测覆盖 |
| InputRequest 卡片 UI + CLI 都能回 + 取消 | 🟢 **CLI ✅ / UI 待 #6** | CLI § 3.7 完整；Web API respond/cancel endpoint ready |
| DM 入口 | 🟢 **backend ready / UI 待 #6** | conversation kind=dm + participants 模型已 ready；channel/conversation CLI 可建可发 |
| CLI / Web Console 命名一致性自检 | ✅ | § 3.9 落地；ADR-0038 § 7 命名规则照办；无 vendor 词 / 无 CLI 专属字段名 (JSON 复用 domain projection 函数) |
| phase-11-test-report.md 归档 | ✅ | 本文档收口 |

## § 5. Follow-up

| F# | 项 | 处置 |
|---|---|---|
| F5 | § 3.10 tactical/presentation docs (`01-web-console.md` rewrite + `02-web-console-architecture.md`) | 跟 #6 React SPA 一起写；frontend 架构定稿后一次性出 |
| F6 | bus.go Shutdown 释放 channels.Map memory | 当前实现 shutdown 只关 subscribers；channels map 由用户 explicit Unsubscribe 清理 — 设计 OK，无 leak |
| F7 | SSE multi-tab 共享单一 connection (SharedWorker) | plan § 1.6 v2 简化方案：多 tab 各开 connection；SharedWorker 留 v3+ |

## § 6. 下一步

**Phase 11 backend → 完成、可发布、可独立验证**。

剩余工作：
- **#6 P11 § 3.4 React SPA frontend** (multi-day frontend 独立 session) — 接 backend API + SSE Bus；pages 全套；F5 docs 跟进
- **#7 P12 Cleanup + Release** — 跨 phase e2e suite / v1 残留扫描 / release checklist

Phase 11 backend 不阻塞 P12 启动；frontend 可与 P12 并行推进。
