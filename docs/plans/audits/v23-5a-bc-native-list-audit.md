# v2.3-5a BC-native Issue/Task List Endpoints Audit

> Closes slock task #31. Backend-only ST that adds four read endpoints
> on the Web Console HTTP API:
> - `GET /api/issues` + `GET /api/issues/{id}` — Discussion BC
>   surfaces the Issue projection
> - `GET /api/tasks` + `GET /api/tasks/{id}` — TaskRuntime BC surfaces
>   the Task projection
>
> The SPA's Issues / Tasks pages currently read these projections via
> `GET /api/conversations?kind=issue|task` — a cross-BC reach where the
> Conversation BC projection cannot carry the `project_id` field that
> lives on `discussion.Issue` / `task.Task`. The SPA `#5b` cutover
> swaps the SPA reads onto the new endpoints; this ST only adds the
> backend surface so #5b can land cleanly without a half-step.
>
> No SPA changes here. No schema migration here. No mutation endpoints
> here.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。`discussion.Issue` AR (discussion/00) 与 `taskruntime/task.Task` AR (01-task) 早已带 `project_id` + repos 已暴露 `FindByProject(projectID, filter)`；本 ST 仅在 transport 上加 BC-native projection — 没有改 AR / 没有改 Repository / 没有加 migration |
| 保留 BC invariants？ | 是。`AppService is the only entry to domain state` 不破：handler 走 `IssueRepo.FindByID` / `FindByProject` + `TaskRepo.FindByID` / `FindByProject`。CRUD 仍只在 CLI 通过 `disservice.IssueLifecycleService` / `trservice.TaskService` 等操作 (ADR-0029 + Discussion BC § 5.1 + TaskRuntime § 5.1)。Discussion BC 与 TaskRuntime BC 重新成为各自 projection 的唯一源 — Conversation BC 不再被借去回答 "this issue belongs to which project?" |
| 没省 transport？ | 是。四个 GET endpoints 都是新 transport，不是 CLI 直读 — 对应 SPA #5b 将要用的 `useIssues({project_id, status?})` / `useIssue(id)` / `useTasks(...)` / `useTask(id)` hooks。Wiring 在 `webconsole_wiring.go` 双路径（embed + run）同步落 |
| Mock-as-default 消除？ | 是。所有新 test 直接走 `setupAPI` 起的 in-memory SQLite + 真实 `disqlite.NewIssueRepo` / `trsqlite.NewTaskRepo`，无 fake repo 出场。`not_wired` 路径 (501) 用 `deps.IssueRepo = nil` / `deps.TaskRepo = nil` 显式触发，非默认行为 |
| 起点 = "previously didn't support cross-BC reads"？ | 否（per § 0.6）。事实是 **SPA 当前从 Conversation BC 读 `kind=issue|task` projection**，而 Issue/Task 的 `project_id` 字段不在 Conversation AR 上；本 ST 直接补出 BC-native list endpoint，让 SPA #5b 可以切到正确的来源。审计描述只写 layer state，不写 intent |

## § 1. 范围

### B1+B2 — `GET /api/issues` + `GET /api/issues/{id}`

- `internal/webconsole/api/handlers.go`
  - 新增 `listIssuesHandler` — `project_id` REQUIRED (missing → 400 `missing_project_id`)，optional `status` → `discussion.IssueFilter{Status: &ss}`，repo 调用 `IssueRepo.FindByProject(ctx, projectID, filter)`
  - 新增 `showIssueHandler` — `IssueRepo.FindByID(id)`；`discussion.ErrIssueNotFound` → 404；其它 err → 500；`IssueRepo == nil` → 501
  - 新增 `issuePublicMap(*discussion.Issue) map[string]any` projection helper

### B3+B4 — `GET /api/tasks` + `GET /api/tasks/{id}`

- `internal/webconsole/api/handlers.go`
  - 新增 `listTasksHandler` — 同上结构，repo 调用 `TaskRepo.FindByProject(ctx, projectID, task.Filter{Status: &ss})`
  - 新增 `showTaskHandler` — `TaskRepo.FindByID(id)`；`task.ErrTaskNotFound` → 404；其它 err → 500；`TaskRepo == nil` → 501
  - 新增 `taskPublicMap(*task.Task) map[string]any` projection helper

### Projection 形状

- **Issue projection** (per Issue AR's getters — no kind/priority field exists on the AR):

  ```json
  {
    "id": "I-1",
    "project_id": "p-1",
    "conversation_id": "C-...",
    "title": "...",
    "status": "open",
    "opened_at": "2026-05-24T10:00:00Z",
    "opener": "user:hayang",
    "closed_at": "2026-05-25T...",       // present only if Issue.ConcludedAt() != nil
    "closed_reason": "duplicate"          // present only if Issue.WithdrawReason() != ""
  }
  ```

  Brief 提到 `kind` / `priority`，但 `discussion.Issue` AR 在 v2 没有这两个 getter — `Origin` 是 entry-point 分类（cli / web_console / supervisor / agent_open_issue / derived_from_conversation），不是 issue category。Per brief 明文「Don't include any field the Issue AR doesn't expose」，本 ST 选择 **不伪造** 这两个字段；§ 2 deviation 详述。

- **Task projection** (per Task AR's getters):

  ```json
  {
    "id": "T-1",
    "project_id": "p-1",
    "conversation_id": "C-...",
    "title": "...",
    "status": "open",
    "priority": "high",
    "created_at": "2026-05-24T10:00:00Z",
    "current_execution_id": "E-99",        // present only if Task.HasActiveExecution()
    "depends_on_task_ids": ["T-x", "T-y"]  // present only if len(deps) > 0
  }
  ```

### B5 — wiring

- `internal/webconsole/api/handlers.go` — `HandlerDeps` 加 `IssueRepo discussion.IssueRepository` + `TaskRepo task.Repository`（含 doc-comment 解释 BC 归属）
- `internal/webconsole/api/server.go` — 注册 4 个 GET routes（紧邻 `POST /api/issues` / `POST /api/tasks` 的 CV4 derivation routes，注释说明 v2.3-5a 与 #5b 的关系）
- `internal/cli/webconsole_wiring.go` — `buildWebConsoleHandler` + `runWebConsole` 两条路径同步填 `IssueRepo: a.IssueRepo` + `TaskRepo: a.TaskRepo`（App 上早已存在）
- `internal/webconsole/api/server_test.go` — `setupAPI` 注入 `disqlite.NewIssueRepo(db)` + 复用 `taskRepo` 让 `IssueRepo` / `TaskRepo` 默认 wired，覆盖 happy + filter 路径

### B6 — Tests

新增 2 个文件 + 22 个 test：

- `internal/webconsole/api/issues_list_test.go` (11 tests)
  - `TestAPI_ListIssues_Happy` — 2 rows + projection 形状（含 kind/priority **必须不出现** 断言）
  - `TestAPI_ListIssues_StatusFilter` — `status=open` 过滤掉 `under_discussion`
  - `TestAPI_ListIssues_MissingProjectID_400` — 400 + `error: missing_project_id`
  - `TestAPI_ListIssues_EmptyResult` — empty array (`[]`)，不能是 `null`
  - `TestAPI_ListIssues_RepoNotWired` — `IssueRepo = nil` → 501
  - `TestAPI_ListIssues_DBError` — closed DB → 500
  - `TestAPI_ShowIssue_Happy` — 单 ID 查 + 字段断言
  - `TestAPI_ShowIssue_NotFound` — 404
  - `TestAPI_ShowIssue_RepoNotWired` — 501
  - `TestAPI_ShowIssue_DBError` — 500
  - `TestIssuePublicMap_WithdrawnHasClosedFields` — projection helper 单测：withdrawn Issue 暴露 `closed_at` + `closed_reason`

- `internal/webconsole/api/tasks_list_test.go` (11 tests)
  - 对偶 issues 的 11 个 case：`ListTasks_Happy` / `StatusFilter` / `MissingProjectID_400` / `EmptyResult` / `RepoNotWired` / `DBError` / `ShowTask_Happy` / `NotFound` / `RepoNotWired` / `DBError`
  - `TestAPI_ShowTask_CoexistsWithTrace` — **共存防线**：同时打 `/api/tasks/T-trace` 与 `/api/tasks/T-trace/trace`，断言前者命中 detail handler（`id == "T-trace"`），后者命中 trace handler（`resource == "events"`），防止未来 route 注册顺序回归
  - `TestTaskPublicMap_ActiveExecutionAndDeps` — projection helper 单测：active execution + deps 同时存在时两个 addenda 都出现

webconsole/api package test count: baseline ~74（pre-ST `go test -count=1 ./internal/webconsole/api -run . -v` 列表）→ 96 个 test 全绿。

## § 2. Deviation 与 documented limitation

### Issue projection 不含 `kind` / `priority`

Brief 列了 `kind` 和 `priority`，但 Issue AR 当前在 v2 里只有：
- `Origin()` ← entry-point 分类（cli / web_console / supervisor / agent_open_issue / derived_from_conversation），不是 issue category，把它命名为 `kind` 会误导 SPA
- 没有 priority getter — Issue 在 Discussion BC 模型里没有 priority；priority 是 Task 概念

Per brief 明文「Don't include any field the Issue AR doesn't expose. Check getters on Issue AR + add fields verbatim」，本 ST 选 **省略** 这两个字段。SPA #5b 在切换时如果发现 UI 真的需要 issue kind，要走 Discussion BC AR 扩字段 + migration 的正路，不在本 ST 范围。Task projection 的 `priority` 字段保留（Task AR 有 `Priority()` getter）。

### `closed_reason` 仅 withdrawn 路径暴露

Issue terminal 有两条路径：
- **withdrawn** ← `WithdrawReason()` (closed enum-ish: e.g. `duplicate` / `wontfix`)
- **closed_no_action** / **closed_with_tasks** ← `ConclusionSummary()` （prose summary，非 enum）

把 `conclusion_summary` 塞进同一个 `closed_reason` 字段会让 SPA terminal-banner 拿到两种语义不同的 payload。本 ST 只在 withdrawn 路径填 `closed_reason`，conclude 路径让 SPA 落 detail 页直接取 `conversation_id` 拉对应议事 conversation 的 `[conclude]` system event。

### Route 注册顺序与 `/api/tasks/{id}/trace` 共存

`GET /api/tasks/{id}` 与 `GET /api/tasks/{id}/trace` 共存于同一 mux。net/http 1.22+ 的 pattern matcher 按 specificity 排序，longer literal segment 胜出，所以 `/{id}/trace` 不会被 `/{id}` 吃掉。`TestAPI_ShowTask_CoexistsWithTrace` 显式锁住这条契约 — 任何把两条 route 顺序反了或合并的回归都会 fail。

## § 3. Verification

| 检查 | 结果 |
|---|---|
| `go test ./...` | OK — full backend suite 绿；`internal/webconsole/api` 1.7s 内含 22 new tests |
| `make lint` | OK — `go vet ./...` + `no-vendor-refs` + `no-mock-default` + `doc-impl-drift` 全清 |
| `make smoke` | OK — `v2.2 Phase D — deployed-binary pipeline` 1 passed 2.9s（deployed-binary 启动 → server + worker daemon + fakeagent 跑 task → done 全链路未受影响；新 endpoints 在该 binary 内已注册可达） |

Smoke pipeline 启用了 webconsole，binary 启动并完成 task → done 等于证明：
1. 新 route 注册不破坏现有 mux（健康检查、其它 endpoints、SSE 全部绿）
2. `webconsole_wiring.go` 双路径（embed + run）的 `IssueRepo` / `TaskRepo` 字段填充不破 App 启动
3. `HandlerDeps` 扩字段不破现有 handler 行为（旧 endpoints 全绿）

## § 4. 文件清单

修改 (4)：
- `internal/webconsole/api/handlers.go` — `HandlerDeps` 扩 `IssueRepo` / `TaskRepo` 字段 + 加 `discussion` / `task` import + 加 4 个 handler + 加 2 个 projection helper
- `internal/webconsole/api/server.go` — 注册 4 个 GET routes
- `internal/webconsole/api/server_test.go` — `setupAPI` 注入 `IssueRepo` + `TaskRepo`
- `internal/cli/webconsole_wiring.go` — `buildWebConsoleHandler` + `runWebConsole` 两路径都 wiring

新增 (2)：
- `internal/webconsole/api/issues_list_test.go` (11 tests)
- `internal/webconsole/api/tasks_list_test.go` (11 tests)
- `docs/plans/v2.3-audits/v23-5a-bc-native-list-audit.md`（本文件）

无 schema migration，无 admin endpoint 改动，无 SPA 改动（SPA #5b 单独处理），无 mutation endpoint。
