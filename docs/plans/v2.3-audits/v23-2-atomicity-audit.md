# v2.3-2 ADR-0014 § 2 Atomicity Audit

> Closes slock task #25. Restores ADR-0014 § 2 same-tx atomicity for
> dispatch + DecisionRecord and kill + DecisionRecord in Client mode
> (CLI invoked through admin endpoint). Previously v2.2's split-
> endpoint design forced supervisor-driven CLI flows to make two
> sibling HTTP calls — failure between them could land a state
> mutation without its audit record.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。ADR-0014 § 2 立 v1 时代的硬规约：state mutation + observability event + DecisionRecord 必须在同 tx 内完成。本 ST 是 transport layer 把这个不变量在 client mode 下恢复 |
| 保留 BC invariants？ | 是。无 schema 变更；无 service 行为变更；新增的 composite endpoint 仍调既有 `DispatchService.Dispatch` + `KillCoordinator.RequestKill` + `decision.Recorder.Record`，只是在 outer `persistence.RunInTx` 内串联（RunInTx 已支持 nested-reuse；nested 调用复用 outer tx） |
| 没省 transport？ | 是。新 endpoint 名 `dispatch-with-decision` / `request-with-decision`，沿用现有 admin route 规范；CLI 在 supervisor 上下文（`AGENT_CENTER_INVOCATION_ID`）下走 composite，其他场景走 simple endpoint —— transport 层显式区分两种语义 |
| Mock-as-default 消除？ | N/A（本 ST 不涉及 spawner / sender wiring） |
| 起点 = "领域模型要求"？ | 是。第一步是查 ADR-0014 § 2 + cognition/01 § 4.7 的规约，再决定 endpoint shape，而不是「把现有 2 个 endpoint 拼起来」式补丁 |

## § 1. 范围

### 新增 server-side
- `POST /admin/taskruntime/dispatch/dispatch-with-decision`
  - request body: `{"dispatch": <dispatchReq>, "decision": <recordDecisionReq>}`
  - handler 用 `persistence.RunInTx(ctx, d.DB, fn)` 包 `DispatchSvc.Dispatch` + `DecisionRecorder.Record`，inner `RunInTx`/repo writes 复用 outer tx
  - response: `{"execution_id": "...", "decision_id": "..."}`
- `POST /admin/taskruntime/kill/request-with-decision` — 同上结构，对应 `KillCoordinator.RequestKill`

### 新增 DI 接线
- `HandlerDeps.DB *sql.DB` 字段（admin/api/deps.go）
- `adminDepsFromApp` 通过 `a.DB` 填入（internal/cli/admin_wiring.go）
- 不影响 webconsole/api（独立 `HandlerDeps`，且 webconsole 不暴露 dispatch/kill 给 user，无需 composite）

### CLI / Client
- `Client.DispatchWithDecision` + `Client.KillWithDecision` + 对应 Request/Response struct（admin_client_taskruntime.go）
- `dispatchHandler` + `killExecutionHandler` 的 Client 分支按 `AGENT_CENTER_INVOCATION_ID` 是否设置切换 composite / simple endpoint（handlers_task.go）
- 普通 user CLI（不带 invocation env）走 simple endpoint —— 不引入 mandatory DecisionRecord 副作用

## § 2. 设计选择

**为什么 composite endpoint 而不是「让 client 持有 server tx」**：跨 HTTP 边界传递 tx 本质上不可行（无状态 HTTP + 单连接 sqlite）。composite endpoint 是唯一保证 server-side 原子性的方式。

**为什么把 DB 放进 HandlerDeps 而不是 ServerDeps**：handler 需要 per-request 起 tx；ServerDeps 是 server 生命周期级。HandlerDeps 已经持有所有 service 指针（都是 long-lived），多一个 `*sql.DB` 不破坏既有形状。

**为什么 nested RunInTx 复用 outer tx 工作**：`persistence.RunInTx` 通过 ctx 携带 `*sql.Tx`，nested 调用检测到已有 tx 直接复用而不开新的（验证: `TestRunInTx_NestedReusesOuterTx` + `TestRunInTx_NestedRollsBackBothOnInnerError`）。`DispatchService` 和 `KillCoordinator` 的内部 `RunInTx` 调用因此自动 join outer tx，无需改 service 签名。

**为什么 simple endpoint 不删除**：worker daemon 不是 supervisor，daemon 的 enroll/dispatch-pull/notify 不应被强制写 DecisionRecord。保留 simple endpoint 让非 supervisor 调用方继续走最小路径。

## § 3. 验证

### 新增 unit/integration tests（5 个）
- `TestClient_DispatchWithDecision_CommitsBoth` —— happy path，两行都落地
- `TestClient_DispatchWithDecision_RollsBackOnDispatchFailure` —— **核心 atomicity 测试**：第二次 dispatch 同 task 触发 single-active violation → 整 tx 回滚，第二个 DecisionRecord 没落地（如果没回滚，DB 会有 2 个 decision rows，第二个 rationale 是 `second-should-rollback`）
- `TestClient_DispatchWithDecision_Rejects400OnMissingRationale` —— 400 路径不开 tx，无副作用
- `TestClient_KillWithDecision_CommitsBoth` —— happy path
- `TestClient_KillWithDecision_RollsBackOnKillFailure` —— kill 未知 execution → kill 失败 → decision 也不落地

### 全量验证
- ✅ `go test ./...` 全 package green（无 regression）
- ✅ `make lint` 全过（vet / no-vendor-refs / no-mock-default / doc-impl-drift）
- ✅ `make smoke` Phase D v22-deployed-pipeline ~3s 通过 → composite endpoint 没破坏既有 task 链路

## § 4. v2.2 的 mismatch 注释清理

`handlers_task.go` 里两处「v2.2-B mismatch: ... v2.3 follow-up」注释已替换为 v2.3-2 closure note，说明 Client 何时走 composite。

## § 5. 未完成 / Out of scope

- supervisor CLI 子命令（`record-decision`, supervisor 自身的 spawn loop）暂未自动化 atomicity 测试。本 ST 只覆盖 dispatch / kill 两个最高频的 supervisor action；其他 supervisor decision kinds（abandon_task / suspend_task 等）走 kill 路径所以也已覆盖。
- 真正 supervisor invocation full e2e 测试（fork supervisor subprocess + 触发 dispatch）属于 task #26 (real-agent dispatch) 的子集。
- ADR-0014 § 2 在非 Client mode（CLI 直读 sqlite 的 newTestApp 路径）已经通过 `runSupervisorActionTx` 满足；本 ST 不动 server-mode 路径。

## § 6. Diff stats

- 4 files 新增 endpoint 代码（admin/api/deps.go + taskruntime.go + server.go + cli/admin_wiring.go）
- 2 Client methods + request/response struct（admin_client_taskruntime.go）
- 2 CLI handler 改动（handlers_task.go）—— 加 supervisor env switch
- 1 file 新增 5 个 atomicity tests（admin_client_atomicity_test.go）

净 ~190 行 impl + ~200 行 test。
