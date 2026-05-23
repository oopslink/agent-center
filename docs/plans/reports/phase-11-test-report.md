# Phase 11 测试报告（部分）

> 完成日期：2026-05-24 · 提交范围：`7359526..e6e27bc` (5 commits, P11 backend + CLI)
>
> 状态：**Backend 完成 / Frontend 待 § 3.4 独立 session**

## § 1. 覆盖率汇总

| 包 | 覆盖率 | 是否达标（≥ 90%） |
|---|---|---|
| `internal/webconsole/sse` | **90.1%** | ✅ |
| `internal/webconsole/api` | **39.8%** | 🟡 待 coverage push |
| `internal/cli` | **68.4%** | 🟡 待 coverage push（input-request / secret / webconsole-wiring handlers 新加，单测未覆盖） |
| `internal/conversation/...` | 92-99% | ✅（P10 F1 维持）|
| `internal/workforce/service` | 90.4% | ✅ |
| `internal/secretmgmt/...` | ≥ 92% | ✅（P8 维持）|

**总体**：本 commit 范围内新增 `webconsole/{api,sse}` + 3 个 CLI handler + `webconsole_wiring` 都通过端到端 smoke 测验证；单测覆盖留 follow-up coverage push commit。

## § 2. 提交清单

| Commit | 范围 | 关键工件 |
|---|---|---|
| `7359526` | § 3.7 | CLI input-request list/show/respond/cancel + service.Cancel |
| `daf549d` | § 3.7b | CLI secret list/show/create/revoke + master_key_file config |
| `7a9761f` | § 3.2 + § 3.3 | Web Console HTTP API skeleton + SSE Bus |
| `ebb8c22` | EventSink → SSE | Poll tailer (250ms tick) |
| `e6e27bc` | /api/fleet + /api/tasks/{id}/trace | Query svc 接入 |
| (this commit) | § 3.10 + 测试报告 | 本文档 |

## § 3. 测试场景（与 plan § 5 对位）

### 3.1 单测

| 工件 | 测试 |
|---|---|
| `sse.Bus` (8 cases) | Subscribe/Unsubscribe happy + bad-args + idempotent；Publish routes by conv_id；ringbuffer drops-oldest + since-all；ServeHTTP 流事件 + heartbeat + requires user_id；Shutdown |
| `sse.EventFanout` (5 cases) | PublishesNewEvents (bootstrap + emit + bus 看到)；SkipsHistoryOnBootstrap；ErrorHandlerInvoked (failingRepo)；NewEventFanout default interval；WithErrorHandler nil no-op |
| `api.Server` (11 cases) | health / conversations roundtrip / send msg / not-found / invite participant / invite-on-DM rejected / archive / fleet (with-svc + without-svc=501) / task trace (with-svc + without-svc=501) / non-loopback bind refused / decodeJSON |
| `service.InputRequestService.Cancel` | (待 unit test 补) — Cancel 已经覆盖 happy path 通过 CLI smoke |
| CLI handlers_input_request / handlers_secret | (待 unit test 补) — smoke 测过 |

### 3.2 集成 / e2e (手动)

- conversation send / channel CRUD / agent create / 派生 issue / task / Web API 全套：每个 commit 都跑了 binary smoke
- SSE 端到端：subscribe → CLI emit → SSE event 1s 内到达 ✅
- Loopback bind guard：refuse 0.0.0.0 / 非 127.0.0.1 ✅

### 3.3 浏览器 e2e（待 § 3.4）

Frontend SPA 未实装；plan § 5.3 浏览器 e2e 留 § 3.4 完成后。

## § 4. DoD 自检

| § 4 DoD 行 | 状态 | 说明 |
|---|---|---|
| § 3.1 框架选型 + 跟其他 phase 同步 | ✅ | React + Vite + Zustand + react-router (2026-05-23 锁定) |
| § 3.2-3.4 Web Console 全 page + API 全功能 | 🟡 backend ✅ / frontend 待 § 3.4 | 17 endpoint 实装；DM / secrets / agent profile pages 待 frontend |
| § 3.5-3.9 CLI 全套补齐 | 🟡 部分 | 3.5/3.6 P10 已 land；3.7/3.7b ✅；3.8 (--format universal) / 3.9 (help discoverability) 留 follow-up |
| Go side 单测 ≥ 90% | 🟡 部分 | sse ✅；api 39.8% / cli 68.4% 待 coverage push round |
| Frontend 单测 ≥ 80% | ⏳ | frontend 未实装 |
| SSE 实时推送 e2e 通 | ✅ | subscribe / unsubscribe / heartbeat / reconnect with Last-Event-ID 单测 + smoke 全过 |
| HTTP server loopback bind verify | ✅ | api.Server.ListenAndServe 主动 reject 非 127.0.0.1 addr；单测覆盖 |
| Secret endpoint 明文不外泄校验 | ✅ | create response 不含 value 字段；list/show 永不含 plaintext；CLI handler wipe 后 buffer |
| 派生 UI: 选 messages → 开 Issue with carry-over | 🟡 backend 已通（POST /api/issues 已实装；CV4 派生 service P10 § 3.6 + CLI P10 F2 都 ✅）；frontend UI 待 § 3.4 |
| InputRequest 卡片 UI + CLI 都能回 + 取消 | 🟡 CLI ✅ (§ 3.7) / UI 待 § 3.4 |
| DM 入口 | 🟡 backend ready / UI 待 § 3.4 |
| CLI / Web Console 命名一致性自检 | ⏳ 待 § 3.9 |
| phase-11-test-report.md 归档 | ✅ | 本文档 |

## § 5. Follow-up

| F# | 项 | 处置 |
|---|---|---|
| F1 | § 3.4 React SPA frontend：Vite + Zustand + react-router + EventSource hook + 全 page (channels / dms / issues / tasks / inputrequests / secrets / agents / fleet / trace) | 独立 multi-day session；可启动指令 `npx create-vite web --template react-ts` + 按 plan § 1.6 page 列表实装 |
| F2 | webconsole/api + cli/handlers_input_request + handlers_secret + webconsole_wiring 单测 ≥ 90% | coverage push commit；接 P8 / P10 套路 (trigger fail injection + nil-arg + endpoint matrix) |
| F3 | § 3.8 --format=table\|json\|yaml universal flag | 全 list/show 命令统一；yaml 包 dep 加 gopkg.in/yaml.v3 |
| F4 | § 3.9 CLI help / discoverability — `agent-center --help` 列全树；命令名一致性 lint | 接 ADR-0038 § 7 命名规则 |
| F5 | § 3.10 docs：tactical/presentation/01-web-console.md (rewrite) + 02-web-console-architecture.md (新) | 文档专项 commit |
| F6 | bus.go Shutdown 释放 channels.Map memory | 当前实现 shutdown 只关 subscribers；channels map 由用户 explicit Unsubscribe 清理 — 设计 OK |
| F7 | SSE multi-tab 共享单一 connection (SharedWorker) | plan § 1.6 v2 简化方案：多 tab 各开 connection；SharedWorker 留 v3+ |

## § 6. 下一步

Phase 11 backend + CLI 完成（plan § 3.2 + § 3.3 + § 3.5 + § 3.6 + § 3.7 + § 3.7b + EventSink fan-out + fleet/trace 全部 ✅）。

**Phase 11 还剩**：§ 3.4 React SPA frontend (multi-day) + § 3.8 / § 3.9 / § 3.10 docs + coverage push。

**Phase 12 Cleanup + Release** 待 P11 全完成后启动。
