# v2.3-1 Transport Completeness Audit

> Closes slock task #24. Adds the 4 admin endpoints that v2.2
> shipped without (per `v22-closeout-audit.md § 4`), so every CLI
> command + worker daemon path now goes through the unix-socket
> transport (conventions § 0.4: AppService is the only entry).

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。`channel leave` / `conversation tail` / `read-task-context` / worker heartbeat 都是 already-existing AppService methods — 起点是「Service 已存在，transport 没暴露」，纯粹补齐 transport 表面 |
| 保留 BC invariants？ | 是。零 schema 变更；零 service 行为变更（仅 WorkerEnrollService 新增 Heartbeat 方法 — 不触发 event，仅更新 last_seen_at） |
| 没省 transport？ | 是。所有 4 个能力的 transport 路径在 client mode 下都走 admin endpoint；legacy direct-service fallback 仅在 newTestApp（无 Client）路径保留 |
| Mock-as-default 消除？ | N/A（本 ST 不涉及 spawner / sender wiring） |
| 起点 = "领域模型要求"？ | 是。第一时间用 `v22-closeout-audit.md § 4` 表格作为起点，4 个 item 一一对应 |

## § 1. 范围

4 个 admin endpoint + 1 个新 service method + 4 个 client/daemon wire-in：

| Item | Endpoint | Service / Repo | CLI hook |
|---|---|---|---|
| 1 | `POST /admin/conversation/participant/leave` | `ParticipantMgmtSvc.Leave` | `channel leave` |
| 2 | `GET /admin/conversation/msg/find-recent` | `MsgRepo.FindRecent` | `conversation read --tail=N`, `conversation tail`, follow loop |
| 3 | `GET /admin/taskruntime/task/read-context` | `TaskSvc.ReadContext` | `read-task-context` (agent CLI) |
| 4 | `POST /admin/workforce/worker/heartbeat` | **new** `WorkerEnrollService.Heartbeat` | worker-daemon `AdminClient.Heartbeat` |

## § 2. 设计选择

**Endpoint shape**: GET for queries (find-recent, read-context),
POST for state-mutating (leave, heartbeat). Matches existing
admin/api conventions.

**`Heartbeat` service method**: Added to `WorkerEnrollService`
rather than a new BC service. Calls `WorkerRepo.UpdateLastHeartbeatAt`
directly (skip load+save+CAS roundtrip — heartbeats fire every tick,
the cost dominates). Returns repo's `not found` error untouched so
the daemon can detect "center restarted, my row's gone" and re-enroll
on the next tick. **No event emitted** — per-tick events would
flood the audit log.

**`TaskReadContext` Client method**: Returns `json.RawMessage` rather
than a typed DTO. The handler historically just `json.Marshal`'d the
`TaskContext` and wrote it to stdout; passing raw bytes through
keeps the wire shape stable without forcing the cli package to take
a compile dependency on `taskruntime/service` types.

**`Heartbeat` in worker daemon `AdminClient`**: Kept the
`capabilities []string` parameter for source compatibility (callers
that conflated heartbeat with re-enroll), but ignores it server-side
— capabilities only mutate on a fresh `Enroll`. The v2.2 hack of
"call enroll + swallow 409 already_exists" is gone; the daemon now
gets a real 200 on every tick of a known worker.

## § 3. 验证

**Unit tests added (8 new + 1 flipped)**:
- `internal/workforce/service/service_test.go` — `TestHeartbeat_Happy`, `_Idempotent`, `_UnknownWorker`, `_ValidatesArgs`
- `internal/workerdaemon/adminclient_test.go` — `TestAdminClient_Heartbeat_PostsToHeartbeatEndpoint` (replaces `_AliasesEnroll`) + `_EmptyWorkerIDFails`
- `internal/cli/admin_client_conversation_test.go` — `TestClient_ChannelLeave_OverAdminEndpoint`, `TestClient_ConversationRead_TailUsesFindRecent_OverAdminEndpoint`
- `internal/cli/admin_client_taskruntime_test.go` — `TestClient_TaskRuntime_ReadContext_OverClient` (flipped from prior `_NotImplementedOverClient`)

**Full suite verification**:
- ✅ `go test ./...` — all packages green (no regression)
- ✅ `make lint` — vet + no-vendor-refs + no-mock-default + doc-impl-drift all green
- ✅ `make smoke` — Phase D v22-deployed-pipeline Playwright spec still passes (~3s) → 4 new endpoints did not break the end-to-end task pipeline

## § 4. Out-of-scope (filed elsewhere)

Still pending from `v22-closeout-audit.md § 4` (separate v2.3 tasks):
- task #25 (v2.3-2): dispatch+DecisionRecord / kill+DecisionRecord composite endpoints (ADR-0014 § 2 atomicity)
- task #26 (v2.3-3): MCP injection + prompt assembly wire in defaultAgentSpawner + artifact blob upload
- task #27 (v2.3-4): Multi-host TCP transport (ADR-0040)

## § 5. Diff stats

- 4 new admin handler funcs (~80 lines)
- 4 new route registrations
- 1 new service method + types (~30 lines)
- 3 new Client methods (~25 lines)
- 4 CLI/daemon handler updates (~50 line delta — net same)
- ~110 lines of tests
- 2 file-header comment updates (replace "mismatch" notes with closure markers)
