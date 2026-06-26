# v2.3-4 Project Web Surface Audit

> Closes slock task #30. Promotes Project to a first-class top-level
> SPA surface (`/projects` list + `/projects/{id}` detail) alongside a
> small backend projection extension. Projects were not previously
> surfaced in the SPA — the backend had only the v2.1-A picker endpoint
> (`GET /api/projects` returning the 4-field {id, name, kind, created_at}
> shape) for the DeriveModal.

## § 0. 必答铁律答卷 (per conventions § 0.4)

| 必答问题 | 答 |
|---|---|
| 起点 = 领域模型要求？ | 是。`workforce.Project` AR (workforce/02) 早已有 `default_agent_cli` / `description` / `updated_at`；本 ST 仅在 transport projection 暴露完整字段 + 新增按 ID 查询，没有改 AR / 没加 migration |
| 保留 BC invariants？ | 是。`AppService is the only entry to domain state` 不破：handler 走 `ProjectRepo.FindByID` / `FindAll`，CRUD 仍只在 CLI 通过 `workforce/service` 操作 (ADR-0029) |
| 没省 transport？ | 是。新增 `GET /api/projects/{id}` 是新 transport endpoint，不是反推 — 对应前端 `useProject(id)` hook。列表 projection 扩字段同步落 `projectPublicMap` helper |
| Mock-as-default 消除？ | 是。`fakeProjectRepo.FindByID` 在 coverage_test.go 升级为按 ID 查找，不再全部返回 `ErrProjectNotFound`；handler unwired 路径返回 501，empty / error / not-found 各自独立 case |
| 起点 = "Conversation 缺 project_id"？ | 否。事实是 **Conversation AR 当前 projection 无 project_id 字段** — Issues/Tasks 页的 project 过滤行因此渲染为 cosmetic，并在源码 + 测试 + 审计里都注明 "follow-up needs projecting project_id onto conversation read model"。没有伪造数据，没有写 "previously assumed single-project" |

## § 1. 范围

### Backend (B1)
- `internal/webconsole/api/handlers.go`
  - `listProjectsHandler` 改走新 helper `projectPublicMap(p)` — 输出 `id, name, kind, default_agent_cli, description, created_at, updated_at`（空串字段省略）
  - 新增 `showProjectHandler` — `GET /api/projects/{id}`；hit/miss 区分 `ErrProjectNotFound` → 404 / 其它 → 500 / `ProjectRepo == nil` → 501
  - 新增 `projectPublicMap(*workforce.Project) map[string]any` helper
- `internal/webconsole/api/server.go`
  - 注册 `GET /api/projects/{id}` 路由 + 注释更新涵盖 v2.3-4
- `internal/webconsole/api/coverage_test.go`
  - `fakeProjectRepo.FindByID` 升级（按 id 查 slice + 可注入 `findByIDErr` 模拟 transport 故障）
  - 新增 5 个 test：`TestAPI_ListProjects_FullProjection`、`TestAPI_ShowProject_Happy` / `_NotFound` / `_RepoNotWired` / `_RepoError`

### Frontend (B2)
- `web/src/api/types.ts` — 新增 `Project` 接口（7 字段，full projection）
- `web/src/api/projects.ts` — `useProject(id)` 增；保留 `useProjects()`；`Project` 改从 `types.ts` re-export
- `web/src/api/queryKeys.ts` — 新增 `qk.project(id)`；test 同步扩
- `web/src/AppLayout.tsx` — 新增 "Workspace" 区段（紧跟 "Home"，含 `Projects → /projects`）；新增 `FolderIcon` SVG（inline，single stroke-width，current color）
- `web/src/App.tsx` — `Projects` + `ProjectDetail` lazy routes 注册
- `web/src/pages/Projects.tsx` — 新页面：标题 + EmptyState + Skeleton + list 行（name + slug + kind chip + agent CLI badge + description + relative time）
- `web/src/pages/ProjectDetail.tsx` — 新页面：header（name / id / kind / cli badge / description）+ bento grid（Issues panel + Tasks panel + Workers "View in Fleet" link）；PanelCard inline 复用 Home.tsx shape，未提抽组件
- `web/src/pages/Issues.tsx` + `web/src/pages/Tasks.tsx` — 新增 project filter chip row，写回 URL `?project=<id>`。**chip 行 cosmetic only**（见 § 2 deviation），源码内 docstring 显式注明
- `web/src/mocks/handlers.ts` — `/api/projects` + `/api/projects/{id}` MSW handlers 增（full projection 形状）

### Tests (B3)
新增 / 扩：
- `web/src/api/hooks.test.tsx` — `useProjects + useProject round-trip`、`useProject skips when id undefined` (2 新 test)
- `web/src/api/queryKeys.test.ts` — 加 `qk.projects()` + `qk.project(id)` 断言
- `web/src/pages/Projects.test.tsx`（新文件）— 4 test：list 渲染 / EmptyState / Skeleton loading / API error
- `web/src/pages/ProjectDetail.test.tsx`（新文件）— 3 test：header+panels 渲染 / 404 friendly error / loading skeleton
- `web/src/AppLayout.shell.test.tsx` — 加 "Workspace section + Projects link" 断言
- `web/src/App.test.tsx` — route case table 加 `/projects` + `/projects/proj-a`

## § 2. Deviation 与 documented limitation

**Conversation AR 没有 project_id 字段。** Conversation BC 在 P10 重写里就没把 project 链接挂到 Conversation 上 — 派生 issue/task 时 project_id 进入 Issue/Task AR (Discussion BC / TaskRuntime BC)，但派生出的子 conversation 只通过 `parent_conversation_id` 指回，跨 BC 反查 project 需要新的读模型。

影响范围：
- `ProjectDetail` 的 Issues / Tasks 面板渲染 "All recent" 而非按项目过滤；面板 empty body 写 `"No issues yet (cross-project view)"` 而不是 `"No issues for this project"`，避免误导
- `Issues.tsx` + `Tasks.tsx` 的 project chip 行渲染但 **cosmetic**：写 URL 不改 result set，源码顶部 docstring 详述

修复方向（后续 ST）：projection a `project_id` column onto the conversation read view (or join via the Issue/Task AR's project_id at query time), then wire `c.project_id === p.id` filter into both list pages + the detail panels. 没有在本 ST 做这件事是因为它涉及 Conversation 读模型扩展 + carry-over service 写新字段，超出 "project 在 SPA 首类"的范围。

## § 3. Verification

| 检查 | 结果 |
|---|---|
| `cd web && pnpm test` | **51 files / 249 passed**（baseline 49 files / ~239；新增 2 files + ~10 new tests, baseline confirmed via `git stash` round-trip） |
| `cd web && pnpm build` | OK — `Projects-*.js 2.78kB / ProjectDetail-*.js 4.65kB` 出现在新 vite chunk 列表 |
| `go test ./...` | OK — `internal/webconsole/api` 1.4s 全绿（含 5 new project handler tests） |
| `make lint` | OK — `go vet` + `no-vendor-refs` + `no-mock-default` + `doc-impl-drift` 全清 |
| `make smoke` | OK — `v2.2 Phase D — deployed-binary pipeline` 1 passed 3.2s（end-to-end fakeagent task → done 未受影响） |

## § 4. Deploy + screenshot evidence

Fresh-binary deploy：
1. `make build` → `bin/agent-center` v2.2.0 (b4a7aa5 + dirty tree)
2. boot with fresh `/tmp/ac-p1/conf.yaml` + fresh `/tmp/ac-p1/db.sqlite` + new `master.key` (chmod 600)
3. `/api/health` → `{"status":"ok","version":"v2-dev"}`
4. `agent-center project add proj-a --name="Project Alpha" --kind=coding --default-agent-cli=claudecode --description=…` → `added project proj-a`
5. `agent-center project add proj-b --name="Project Beta" --kind=writing --default-agent-cli=opencode --description=…` → `added project proj-b`
6. `curl /api/projects` 返回 2 行，每行包含 `default_agent_cli` / `description` / `updated_at`
7. `curl /api/projects/proj-a` 返回 1 个对象，full projection

Screenshots (chromium-mac via playwright)：
- `/tmp/ac-p1/v23-4-projects-1440.png` — `/projects` @ 1440×900：Workspace section 高亮、2 行渲染、chip + badge 正确
- `/tmp/ac-p1/v23-4-projects-375.png` — `/projects` @ 375×812：mobile 视口、hamburger 显示、内容堆叠
- `/tmp/ac-p1/v23-4-project-detail-1440.png` — `/projects/proj-a` @ 1440×900：header / description / bento grid / Fleet link section 全部就位

## § 5. 文件清单

修改 (15 + 5 new pages/tests = 20 files)：
- backend: `internal/webconsole/api/handlers.go`、`server.go`、`coverage_test.go`
- frontend: `web/src/AppLayout.tsx`、`App.tsx`、`App.test.tsx`、`AppLayout.shell.test.tsx`、`api/types.ts`、`api/projects.ts`、`api/queryKeys.ts`、`api/queryKeys.test.ts`、`api/hooks.test.tsx`、`mocks/handlers.ts`、`pages/Issues.tsx`、`pages/Tasks.tsx`
- new: `web/src/pages/Projects.tsx`、`pages/Projects.test.tsx`、`pages/ProjectDetail.tsx`、`pages/ProjectDetail.test.tsx`
- doc: `docs/plans/v2.3-audits/v23-4-project-web-surface-audit.md` (本文件)

无 schema migration（按 brief 要求），无 admin endpoint 改动，仍尊重 ADR-0029（Project CRUD CLI-only），DeriveModal 未触碰。
