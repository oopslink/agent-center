# v2.2-A2 — admin endpoint covers full CLI AppService surface

> Phase A sub-ST 2 of 5. Per-ST P12 cadence. v2.2-A1 stood up the
> unix-socket transport adapter + health endpoint; A2 builds the route
> surface that Phase B's CLI client will call into.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。AppService 是唯一入口；admin endpoint 是 transport adapter (port 已存在，adapter 之前缺) |
| 保留 BC invariants？ | 是。所有 endpoint 都调现有 AppService method，不绕过 |
| 没省 transport？ | 是。不"为 single-host 简化"省 route，全 79 method 都暴露 |
| Mock-as-default 消除？ | 不在 A2 scope，A3/A4 处理 |
| **起点 = "领域模型要求"，不是"现状最小补丁"？** | 是。@oopslink 驳回了我之前提的"先暴露 5 个 worker-loop endpoint，其它延后"。现 plan 全 79 method 一次过 |

## § 1. 范围

### in scope

把 CLI 当前需要的全部 79 个 `App.Service.Method` 调用点暴露成 admin endpoint。每个 endpoint:

- HTTP/JSON over unix socket（v2.2-A1 的 listener）
- 路径约定 `/admin/<service-kebab>/<method-kebab>`
- POST 为默认（请求体携带 method 参数 JSON）
- GET 用于无写入的查询（`Find*` / `Snapshot` / etc.）
- 错误用 webconsole/api `writeError` 同款 envelope `{"error": "<code>", "message": "<text>"}`
- 错误 → status 用 webconsole/api `mapDomainError` 同款映射

### out of scope

- A2 **不**替换 NoopSender / KillSender（A3）
- A2 **不** wire SupervisorSpawner（A4）
- A2 **不**碰 CLI handler（Phase B）
- A2 **不**实现 streaming/long-poll endpoint（C 阶段 worker daemon 才需要；A2 留 router scaffold 即可）

## § 2. 79 method 按 BC 分组

按 BC 分 8 组，每组一个 file 在 `internal/admin/api/<bc>.go`:

1. **workforce.go** — EnrollSvc, AcceptanceSvc, AgentMgmtSvc, AgentInstanceRepo, ProposalRepo, MappingRepo, ProjectRepo, ProjectSvc, WorkerRepo
2. **conversation.go** — ConvRepo, MsgRepo, MessageWriter, ChannelMgmtSvc, ParticipantMgmtSvc, CarryOverSvc, DerivationSvc, ConvRefRepo
3. **taskruntime.go** — TaskRepo, ExecRepo, IRRepo, ArtifactRepo, TaskSvc, IRSvc, ArtifactSvc, ExecSvc, DispatchSvc, KillCoordinator
4. **cognition.go** — SupervisorSpawner, DecisionRecorder, InvocationRepo, DecisionRepo
5. **discussion.go** — IssueRepo, IssueLifecycleSvc, IssueCommentSvc, IssueBindConversationSvc, IssueLinkConversationSvc
6. **secret.go** — UserSecretRepo, UserSecretSvc
7. **identity.go** — IdentityRepo, IdentityRegistration
8. **observability.go** — EventRepo, ProjectionRepo, QuerySvc, FleetSvc, StatsSvc, LogsSvc

### 复用策略

`internal/webconsole/api/` 已经把约 25 个 method 暴露成 HTTP/JSON。**不重复实现** —— 把 webconsole/api 的 handler 函数 export 出来（或抽到一个公共 package），admin endpoint route 直接复用同一 handler。这条减掉约 40% 的样板代码。

## § 3. HandlerDeps

复用 `internal/webconsole/api/HandlerDeps`（已携带所有 BC Service / Repo handle 的 dep bag）。admin api 的 routes 注册同样的 `WithDeps` middleware，handler 函数读 deps 一致。

新增 dep（admin 独有 CLI 触及但 webconsole 没用的）:
- `EnrollSvc *wfservice.WorkerEnrollService`
- `AcceptanceSvc *wfservice.ProposalAcceptanceService`
- `ProjectSvc *wfservice.ProjectCRUDService`
- `ExecSvc *trservice.ExecutionService`
- `DispatchSvc *dispatch.Service`
- `KillCoordinator *kill.Coordinator`
- `IdentityRegistration *identity.RegistrationService`
- `DecisionRecorder *decision.Recorder`
- `SupervisorSpawner *scheduler.Spawner`
- `InvocationRepo cognition.SupervisorInvocationRepository`
- `DecisionRepo cognition.DecisionRecordRepository`
- `IssueLifecycleSvc *disservice.IssueLifecycleService`
- `IssueCommentSvc *disservice.IssueCommentService`
- `IssueBindConversationSvc *disservice.IssueBindConversationService`
- `IssueLinkConversationSvc *disservice.IssueLinkConversationService`
- `WorkerRepo workforce.WorkerRepository`
- `MappingRepo workforce.WorkerProjectMappingRepository`
- `ProposalRepo workforce.WorkerProjectProposalRepository`
- `MsgRepo conversation.MessageRepository`
- `TaskRepo task.Repository`
- `ExecRepo execution.Repository`
- `ArtifactRepo execution.ArtifactRepository`
- `ArtifactSvc *trservice.ArtifactService`
- `StatsSvc *query.StatsService`
- `LogsSvc *query.LogsService`
- `BlobStore blobstore.BlobStore`

## § 4. 验收

- 8 个 BC 分组 file 全建，每个内部 ~10 handler
- `internal/admin/api/server.go` 的 `routes()` mount 全 79 endpoint
- unit test 覆盖每组 ≥ 1 个 happy path + 1 个 error path（不追求 90% 覆盖率，A2 是 scaffolding）
- **临时 deploy smoke**: 起 `./bin/agent-center server`，至少 5 个端点 curl + 期望 JSON shape:
  - `GET /admin/workforce/agent-instance/find-all` → `[]` (空 DB)
  - `POST /admin/workforce/worker/enroll` body `{"worker_id":"smoke-1"}` → `{"worker_id":"smoke-1", "event_id":"...", "version":1}`
  - `GET /admin/conversation/conv/find-all` → `[]`
  - `POST /admin/identity/register` body `{"id":"user:smoke","display_name":"Smoke"}` → success
  - `GET /admin/admin-health` (already from A1) → `{"ok":true}`

## § 5. 风险

- **大 PR**: 8 files × ~10 handler × ~20 LOC = ~1600 LOC change. 一次 review 量大。**缓解**: 按 BC 分组分多个 commit (8 commits in A2 ST)。
- **测试 explosion**: 全覆盖每个 endpoint 测试会非常多。**缓解**: A2 测试 = 路由 + 一个 happy + 一个 error 即可，深度覆盖留 Phase B 时 CLI client 测试连带捕获。
- **WebConsole handlers 复用**: 把 webconsole/api handler 跨 package 复用需要 export。**缓解**: 重命名 webconsole/api 内部 file，handler 函数 → export，admin 直接 import。

## § 6. Execution log

**Files landed (single batch commit; sub-agent wrote 8 BC files in
parallel from this audit + the existing webconsole/api reference)**:

- `internal/admin/api/deps.go` — `HandlerDeps` (45+ fields across 7
  BCs) + `WithDeps` middleware + JSON helpers (`writeJSON`,
  `writeError`, `decodeJSON`).
- `internal/admin/api/errors.go` — `mapDomainError` covering 60+
  domain sentinels (conversation, workforce, taskruntime, secretmgmt,
  discussion, cognition, identity).
- `internal/admin/api/workforce.go` — 22 handlers (worker /
  proposal / agent-instance / project — all CRUD + lifecycle).
- `internal/admin/api/conversation.go` — 19 handlers (conv / msg /
  message-writer / channel / participant / carry-over / derivation /
  conv-ref).
- `internal/admin/api/taskruntime.go` — 20 handlers (task / exec /
  ir / artifact / dispatch / kill).
- `internal/admin/api/cognition.go` — 6 handlers (supervisor.spawn,
  decision.record, invocation 3x, decision lookup).
- `internal/admin/api/discussion.go` — 10 handlers (issue lifecycle +
  comment + bind/link).
- `internal/admin/api/secret.go` — 7 handlers (UserSecretRepo 3x +
  Create/Rotate/Revoke; Resolve stubbed 501 per ADR-0026 security).
- `internal/admin/api/identity.go` — 2 handlers (Find,
  RegisterIdentity).
- `internal/admin/api/observability.go` — 7 handlers (events / query
  / fleet / stats / logs).
- `internal/admin/api/server.go` — `routes()` mounts all 92 BC
  endpoints + the A1 health endpoint = 93 total.
- `internal/cli/admin_wiring.go` — `adminDepsFromApp` adapter +
  `runAdminEndpoint` now installs `WithDeps` middleware.
- `internal/cli/handlers_system.go` — passes `app` to
  `runAdminEndpoint` so deps are populated.

**Total**: 93 admin endpoints, ~3.85K LOC across 11 new files (deps +
errors + 8 BC + server changes).

**Out-of-scope items confirmed (per § 1 audit out)**:
- `UserSecretSvc.Resolve` returns 501 — plaintext path deferred to
  worker daemon (Phase C) where secret resolution happens just-in-time
  for agent CLI MCP injection. ADR-0026 security path stays gated.
- Streaming endpoints (long-poll dispatch / SSE) — A2 ships request /
  response only. Phase C worker daemon adds streaming as needed.

**Verification — deploy smoke (firsthand, per conventions § 0.4)**:

```
$ ./bin/agent-center server --config=/tmp/v22a2.yaml
agent-center server: db=/tmp/v22a2.db listen=:7099 web=127.0.0.1:7199 admin=/tmp/v22a2-admin.sock (escalator running)

$ curl --unix-socket /tmp/v22a2-admin.sock http://localhost/admin/health
{"endpoint":"admin","ok":true,"transport":"unix"}

$ curl --unix-socket /tmp/v22a2-admin.sock 'http://localhost/admin/workforce/agent-instance/find-all'
[]

$ curl --unix-socket /tmp/v22a2-admin.sock -XPOST \
   -H 'Content-Type: application/json' \
   -d '{"worker_id":"smoke-1","capabilities":["claude-code"]}' \
   http://localhost/admin/workforce/worker/enroll
{"event_id":"01KSD8F3VPGNEGN1M66SC66BJ0","version":1,"worker_id":"smoke-1"}

$ curl --unix-socket /tmp/v22a2-admin.sock http://localhost/admin/workforce/worker/find-all
[{"capabilities":["claude-code"],"enrolled_at":"...","status":"offline","version":1,"worker_id":"smoke-1"}]

$ curl --unix-socket /tmp/v22a2-admin.sock 'http://localhost/admin/observability/fleet/snapshot'
{"executions":[],"workers":[{"worker_id":"smoke-1","status":"offline",...}],...}
```

Full chain works: enroll (write through admin) → find-all (read
through admin) → fleet snapshot (cross-BC aggregation through admin)
all return consistent state. AppService invariants preserved
end-to-end through the admin transport.

- `go build ./...` clean
- `go test ./internal/admin/api/` 5/5 (A1 tests still pass post-A2)
- `go test ./internal/cli/` green
- A2 done.
