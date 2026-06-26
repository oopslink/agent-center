# v2.3-5b SPA BC-native Cutover Audit

> Closes slock task #32. Frontend-only ST that switches the SPA's
> Issues + Tasks surfaces from `useConversations({kind:'issue'|'task'})`
> (Conversation BC reach) to the BC-native `GET /api/issues` +
> `GET /api/tasks` endpoints landed by #31 (v2.3-5a). Backend
> untouched.
>
> Net effect at the layer: Issue/Task lists, detail headers, and the
> per-project ProjectDetail panels now read from the BC that OWNS the
> projection (Discussion BC, TaskRuntime BC). Conversation BC reads
> remain in place for message-thread rendering inside the detail
> pages — that surface is genuinely Conversation BC's responsibility
> (message ownership; ADR-0036 + Conversation v2 § 5.1).

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。`discussion.Issue` AR exposes `ProjectID()`, `Title()`, `Status()`, `OpenedAt()`, `OpenedByIdentityID()`, `ConversationID()`; `taskruntime/task.Task` AR exposes `ProjectID()`, `Title()`, `Status()`, `Priority()`, `CreatedAt()`, `ConversationID()`, `CurrentExecutionID()`. The SPA was previously asking the WRONG BC (Conversation) about per-project Issue/Task scope because Conversation AR has no `project_id` — see Conversation v2 § 1.2 (Conversation does not link to Project). This ST routes the question back to the owning BC. No AR change. No Repository change. No migration. |
| 保留 BC invariants？ | 是。`AppService is the only entry to domain state` 不破：reads continue to flow through `IssueRepo.FindByProject` + `TaskRepo.FindByProject` on the server side (already in place since #31). The SPA only switches WHICH HTTP endpoint it calls; no client-side BC contamination. Conversation BC stays responsible for message thread reads in detail pages, which is correct (Conversation v2 § 5.1 — Conversation AR owns messages). |
| 没省 transport？ | 是。SPA calls go through the existing fetch client (`api/client.ts`) → `/api/issues` + `/api/tasks` over HTTP — same transport CLI uses (admin unix socket vs TCP is the deployment topology, not a layer skip; per feedback `appservice-only-entry`). No mock-as-default shortcut, no in-process bypass. |
| Mock-as-default 消除？ | 是。MSW handlers for `/api/issues` + `/api/tasks` echo the v2.3-5a backend's projection shape verbatim (validated against running binary in § 3). Existing hook tests use `renderHook` + MSW round-trip (mirrors the pattern from `api/hooks.test.tsx`). |
| 起点 = "previously didn't support per-project filtering"？ | 否（per § 0.6）。事实是 **the SPA was reading Issue/Task list state from the Conversation BC projection, which doesn't carry `project_id` — so the project filter chip was cosmetic and the cross-project "(cross-project view)" hint on `/projects/:id` was a layer-violation tell**. Backend endpoints + per-project filter have existed since #31. This ST is the SPA-side cutover that makes the chips functional. The audit describes the layer state, not past intent. |

## § 1. 范围 — what changed at the layer

### F1 — API types + hooks

- `web/src/api/types.ts`: added `Issue` + `Task` + `IssueStatus` + `TaskStatus` + `TaskPriority` types. Field names match the backend `issuePublicMap` / `taskPublicMap` projections in `internal/webconsole/api/handlers.go` byte-for-byte (id, project_id, conversation_id, title, status, opened_at/created_at, opener, priority, current_execution_id, depends_on_task_ids, closed_at, closed_reason). No fabricated fields — Issue does NOT have `kind` or `priority` per the AR's getter surface.
- `web/src/api/issues.ts` (new): `useIssues({projectId?, status?})` + `useIssue(id)`. Backend rejects requests without `project_id` (400) — hook short-circuits via `enabled: !!projectId`. Status filter forwarded as `?status=`.
- `web/src/api/tasks.ts` (new): `useTasksList({projectId?, status?})` + `useTask(id)`. Same shape. Named `useTasksList` to avoid colliding with `useTaskTrace` (`api/fleet.ts`) which already owned the `tasks` plural in spirit.
- `web/src/api/queryKeys.ts`: added `qk.issues({projectId?, status?})`, `qk.issue(id)`, `qk.tasksList({projectId?, status?})`, `qk.task(id)`. Distinct from `qk.taskTrace(taskId)` — both surfaces live on TaskRuntime BC but answer different questions (list/show vs execution trace).
- `web/src/mocks/handlers.ts`: added 4 GET handlers; 400 on missing `project_id` (mirrors backend); canned fixtures match the backend projection shape.

### F2 — Issues page cutover

- `web/src/pages/Issues.tsx`: replaced `useConversations({kind:'issue'})` with `useIssues({projectId, status})`.
- Project chip filter is now REAL (was cosmetic since #30). URL `?project=<id>` drives the chip; "All" state shows a `pick-a-project` empty-state because the backend requires `project_id`.
- Status filter row updated to the Discussion BC 6-value enum (`open`, `under_discussion`, `concluded`, `closed_with_tasks`, `closed_no_action`, `withdrawn`) instead of the Conversation BC 3-value enum (`active`/`closed`/`archived`).
- Row renders `title` (was `name`), status chip, `opener` mono, `opened_at` relative.

### F3 — Tasks page cutover

- `web/src/pages/Tasks.tsx`: replaced `useConversations({kind:'task'})` with `useTasksList({projectId, status})`.
- Project chip filter real, same pick-a-project empty state.
- Status row uses TaskRuntime BC 4-value enum (`open`, `suspended`, `done`, `abandoned`).
- Row renders `title`, status chip, priority chip, `created_at` relative, and an inline `view trace →` link ONLY when `current_execution_id` is present (per Task projection — emitted only on active execution).

### F4 — IssueDetail / TaskDetail Option B route shape

- `web/src/pages/IssueDetail.tsx`: route `:id` is now the **issue_id**, not the conversation_id. Header is fed by `useIssue(id)` (Discussion BC). The bound `conversation_id` is read from the Issue projection and passed to `useConversation(convId)`, `useMessages(convId)`, `useConversationRefs(convId)` for message + carry-over rendering (Conversation BC). Header surfaces title / status chip / opened_at / opener / project link.
- `web/src/pages/TaskDetail.tsx`: symmetric — `:id` is **task_id**, header from `useTask(id)` (TaskRuntime BC); message thread + composer scoped to Task projection's `conversation_id`. Header surfaces title / status / priority / created_at / project link / optional `exec · <id>`.

**Option B chosen** (route param semantics change from conversation_id → issue/task_id). Rationale: the list rows already link via `/issues/${issue.id}` and `/tasks/${task.id}` after the F2/F3 cutover so consistency is automatic; Option A would have required either a server-side lookup endpoint (extra round trip + new transport) or stuffing `conversation_id → issue_id` plumbing into the SPA — both inferior. Option B keeps the SPA's URL semantics aligned with the BC that owns the resource.

### F5 — ProjectDetail panels upgrade

- `web/src/pages/ProjectDetail.tsx`: IssuesPanel + TasksPanel now read `useIssues({projectId})` / `useTasksList({projectId})`. The "(cross-project view)" hint text is gone — the panels now answer the obvious question accurately.

### F6 — DeriveModal / derive mutation cache invalidation

- `web/src/api/derive.ts`: `useDeriveIssue` + `useDeriveTask` now invalidate `qk.issues()` / `qk.tasksList()` in addition to `qk.conversations()`. The Conversation BC invalidation stays because the derive flow creates a bound `kind=issue|task` conversation (CV4 carry-over).
- `web/src/sse/useSSE.ts`: extended the SSE → cache dispatcher to invalidate the new BC-native caches on `issue.opened|withdrawn|concluded|tasks_spawned|discussion_started` and on `task.created|abandoned|suspended|done`. Backend event-type literals verified via grep against `internal/discussion/service/issue_lifecycle_service.go`, `internal/discussion/service/issue_conclude.go`, `internal/taskruntime/service/task_service.go`, `internal/taskruntime/service/execution_service.go`, `internal/taskruntime/kill/coordinator.go`.

### F7 — Tests

- **New (round-trip hooks)**: `web/src/api/issues.test.ts` (6 cases), `web/src/api/tasks.test.ts` (5 cases). Cover happy path + idle-when-projectId-missing + status query-param forwarding + backend 400 surfacing.
- **Updated page tests**: `web/src/pages/Issues.test.tsx`, `web/src/pages/Tasks.test.tsx` now assert (a) pick-project nudge in the all-state, (b) project chip narrows the list (cross-project leak rejected), (c) status filter is forwarded server-side, (d) error surfacing.
- **Updated**: `web/src/pages/ProjectDetail.test.tsx` asserts panels forward `project_id=proj-a` to the backend and the "(cross-project view)" hint is GONE.
- **Updated**: `web/src/pages/IssueDetail.test.tsx`, `web/src/pages/TaskDetail.test.tsx` mock `/api/issues/:id` + `/api/tasks/:id` (the new route semantic) and assert header surfaces project link, priority chip, exec id.
- **Updated**: `web/src/pages/DetailLoadingStates.test.tsx` for the new outer loading state (Issue/Task projection fetch, not Conversation fetch).
- **Updated**: `web/src/api/queryKeys.test.ts` covers `qk.issues`/`qk.issue`/`qk.tasksList`/`qk.task`.
- **Updated**: `web/src/sse/useSSE.test.tsx` asserts the new task.* + issue.* lifecycle events invalidate `qk.tasksList()` / `qk.issues()`.

Total: 269 tests pass (was 250+; +19 new cases across the suite).

### F8 — Verification

| Gate | Outcome |
|---|---|
| `cd web && pnpm test` | 269/269 pass (53 files) |
| `cd web && pnpm build` | green, 642ms |
| `go test ./...` | all green (cached + uncached re-runs) |
| `make lint` | green (vet + no-vendor-refs + no-mock-default + doc-impl-drift) |
| `make smoke` | `smoke pass: 7 seconds` (deployed-binary pipeline) |

### F9 — Deploy + screenshots

- Built fresh binary, booted server on `127.0.0.1:7401` with a clean `/tmp/ac-p1/db.sqlite`.
- Seeded `proj-alpha` (3 issues + 2 tasks) and `proj-beta` (2 issues + 1 task) via `bin/agent-center --config /tmp/ac-p1/conf.yaml project add … issue open … task create …`.
- Captured at 1440×900:
  - `/tmp/ac-p1/v23-5b-project-detail-1440.png` — `/projects/proj-alpha` showing the per-project Issues + Tasks panels with real counts (no more `(cross-project view)` hint).
  - `/tmp/ac-p1/v23-5b-issues-1440.png` — `/issues?project=proj-alpha` showing only proj-alpha's 3 issues; `Project Alpha` chip selected (filter is functional, not cosmetic).
  - `/tmp/ac-p1/v23-5b-tasks-1440.png` — `/tasks?project=proj-alpha` showing only proj-alpha's 2 tasks with priority chips and the new Task status enum.
- Playwright spec deleted after capture (`tests/e2e/v2/tests/v23-5b-screenshots.spec.ts` removed).

## § 2. Deviation

None. § 0.6 observed — the audit describes what changed at the layer and does not retroactively claim past intent. The previous cosmetic project chip + cross-project hint text were genuine layer violations made visible by the v2.3-4 / v2.3-5a backend additions; this ST closes them.

## § 3. Cross-check — backend projection shapes vs SPA types

Verified against live server with two seeded projects:

```
$ curl -s "http://127.0.0.1:7401/api/issues?project_id=proj-alpha" | jq '.[0]'
{
  "conversation_id": "",
  "id": "01KSFHJM2W86SVZD4MGH68XGFP",
  "opened_at": "2026-05-25T12:25:55.036102Z",
  "opener": "user:hayang",
  "project_id": "proj-alpha",
  "status": "open",
  "title": "Login flow returns 401"
}

$ curl -s "http://127.0.0.1:7401/api/tasks?project_id=proj-alpha" | jq '.[0]'
{
  "conversation_id": "",
  "created_at": "2026-05-25T12:26:09.542688Z",
  "id": "01KSFHK28642239PG1CPMAQMXZ",
  "priority": "medium",
  "project_id": "proj-alpha",
  "status": "open",
  "title": "Update API docs for v2.3-5b"
}
```

Both shapes match the `Issue` / `Task` types added in `web/src/api/types.ts` exactly (modulo optional fields the SPA already handles).
