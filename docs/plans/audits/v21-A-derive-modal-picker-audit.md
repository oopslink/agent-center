# v2.1-A — DeriveModal project picker audit

> Run 2026-05-24 · v2.1 micro-pass (≤2h per x9527 final cadence rule
> applied in self-enforced mode after handoff). Inline audit (no
> separate plan-detail); 1 audit log + impl commit(s).

## § 0. Issue + scope

Filed in `phase-12-test-report.md § 4` v2.1+ carryovers table and
referenced from S11 audit § 8.6: the SPA DeriveModal does not
collect `project_id`, but the backend `POST /api/issues` +
`POST /api/tasks` deriveIssue/deriveTask handlers require it
(via the service layer). The S11 carry-over-ui e2e test had to
stop at "modal opens; title editable" because submit would 400.

Goal: thread `project_id` end-to-end through the carry-over derive
UI so the full select → modal → submit → navigate-to-new-page flow
works.

### Scope (in)

1. Backend: new `GET /api/projects` endpoint returning the list of
   `{id, name, kind}`. ProjectRepo wire into HandlerDeps.
2. Frontend: new `useProjects()` query hook + `qk.projects()` cache
   key. DeriveModal renders a project picker (select dropdown);
   disables submit until a project is selected. Mutation payload
   carries `project_id`.
3. Tests: backend handler test for the new endpoint; frontend
   unit test (`DeriveModal.test.tsx`) for the picker; S11 e2e
   `carry-over-ui.spec.ts` upgraded to full submit-to-navigation.
4. Audit: this file + execution log in § 4.

### Scope (out — explicit narrowing)

- **No project CREATE in the picker** — if there are zero projects
  the modal shows "No projects — create one via `agent-center
  project add` first." Adding inline project-create UI is a
  separate v2.1+ feature ask, not bundled here.
- **No agent picker for deriveTask** — current scope is Issue +
  Task with no agent assignment; agent-binding happens at dispatch
  time (P11 already), not derive time.
- **No SSE invalidation of project list** — picker fetches at modal
  open; if a project is added in another tab the user must reload.
  React Query 5-second stale time will catch ordinary refreshes.

## § 1. Backend plan

### 1.1 New endpoint

```
GET /api/projects → 200 [{ id, name, kind, created_at }, ...]
```

`kind` is optional in the Project AR (may be empty string); marshal
as JSON null when empty.

### 1.2 HandlerDeps

Add `ProjectRepo workforce.ProjectRepository`. Wire in
`cli/webconsole_wiring.go` (both `buildWebConsoleHandler` and
`runWebConsole` paths).

### 1.3 Tests

Add to `internal/webconsole/api/coverage_test.go` (or a new
project handlers test file): empty list (200, `[]`), single
project after sqlite INSERT (200, 1 row), repo-error returns 500.

## § 2. Frontend plan

### 2.1 API client + query hook

`web/src/api/projects.ts` (new):

```ts
export interface Project { id: string; name: string; kind?: string; }
export function useProjects() {
  return useQuery({ queryKey: qk.projects(), queryFn: () =>
    api.get<Project[]>('/projects') });
}
```

`qk.projects()` added to `queryKeys.ts`.

### 2.2 DeriveModal changes

- New state `projectId` (default empty).
- New `<select data-testid="derive-project-select">` rendered
  ABOVE the title input.
- Options: `<option value="" disabled>Select project…</option>`
  plus one `<option value={p.id}>{p.name}</option>` per project.
- Empty-state: if `useProjects().data.length === 0`, render a
  `data-testid="derive-no-projects"` message instead of the
  select.
- Submit button disabled until BOTH `title.trim()` AND `projectId`
  are set.
- Mutation payload adds `project_id`.

### 2.3 Tests

- `DeriveModal.test.tsx`: extend existing happy-path with MSW
  project list mock; assert submit disabled until both fields
  filled; assert mutation payload contains project_id; new test
  for empty-state (no projects).

## § 3. e2e upgrade

`tests/e2e/v2/tests/carry-over-ui.spec.ts`:

1. Seed project via direct sqlite (existing S9 codified rule).
2. (Existing) seed channel + 3 messages.
3. (Existing) goto channel; select-mode-toggle; tick 2 messages;
   click derive-open-issue.
4. **New**: wait for `[data-testid="derive-project-select"]`;
   `selectOption({ value: projectID })`.
5. (Existing) fill title.
6. **New**: click `[data-testid="derive-modal-submit"]`; wait for
   `[data-testid="derive-success"]`; click
   `[data-testid="derive-success-link"]`.
7. **New**: assert URL navigated to `/issues/<id>`; assert
   CarryOverDivider visible with source message IDs as data
   attributes.

Note: this requires the IssueDetail page to render CarryOverDivider
with `data-testid="carry-over-divider"` + `data-source-msg-ids` —
verify whether it already does; if not, small add.

## § 4. Execution log

### 4.1 Impl

**Backend** (`internal/webconsole/api/`):
- `handlers.go` — HandlerDeps gained `ProjectRepo
  workforce.ProjectRepository`; new `listProjectsHandler` —
  returns `[{id, name, kind?, created_at}]` with 501 if repo not
  wired + 500 on repo error.
- `server.go` — new `GET /api/projects` route, grouped with v2.1-A
  comment.
- `coverage_test.go` — 4 new tests:
  `TestAPI_ListProjects_Empty` / `_SingleProject` /
  `_RepoNotWired` / `_RepoError` + `fakeProjectRepo` helper.
- `internal/cli/webconsole_wiring.go` — wires `a.ProjectRepo`
  into both handler instances.

**Frontend** (`web/src/`):
- `api/projects.ts` (new) — `Project` type + `useProjects()`
  query hook with 5s stale time.
- `api/queryKeys.ts` — `qk.projects()` added.
- `api/derive.ts` — `project_id` field type changed from optional
  to required (forces type-system call sites to comply).
- `components/DeriveModal.tsx` — new state `projectId` + `<select
  data-testid="derive-project-select">` rendered above title (with
  loading + empty states `derive-projects-loading` /
  `derive-no-projects`); submit disabled until BOTH `title.trim()`
  AND `projectId` are set; payload threads `project_id`.
- `components/DeriveModal.test.tsx` — 7 tests covering MSW
  project list + happy paths (Issue + Task) + cancel + server
  error + submit-disable matrix + no-projects empty state.
- `api/hooks.test.tsx` — `useDeriveIssue` / `useDeriveTask` tests
  add `project_id: 'p-demo'` to match new required type.
- `pages/ChannelDetail.derive.test.tsx` — integration test
  refreshed to select project before submitting (MSW handler for
  GET /api/projects added).

**E2E** (`tests/e2e/v2/`):
- `tests/carry-over-ui.spec.ts` — UPGRADED: now seeds project via
  direct sqlite, then runs full select → modal → pick project →
  fill title → submit → success → click View Issue link → assert
  navigation to `/issues/<id>` + CarryOverDivider renders with
  `data-message-id` attributes matching the 2 picked source
  messages.

### 4.2 First-run bug caught: stale SPA bundle

First Playwright run failed on `expect(projectSelect).toBeVisible()`
with element-not-found. Root cause: `make build-backend` was used
to rebuild the binary, but it doesn't trigger vite — the embedded
SPA was still the pre-change bundle without the picker. `make
build` (which runs `build-frontend` THEN `build-backend`) is
required when SPA-side changes need to land in the binary.

**Lesson codified**: for any test/audit cycle that mixes Go +
SPA changes, use `make build`, NOT `make build-backend`. The
Makefile already documents this at the top of the file; adding
this audit § entry surfaces the gotcha for future v2.1+ work.

### 4.3 Verification (3× anti-flake)

```
=== Run 1: 12 passed (6.6s) ===
=== Run 2: 12 passed (6.6s) ===
=== Run 3: 12 passed (6.6s) ===
```

Per-test variance < 30ms; 0 retries.

Go suite: `go test ./...` green (cached + freshly-run packages).
`go vet ./...` clean. `make lint-vendor` clean.
Frontend suite: `pnpm exec vitest run` 39 files / 189 tests pass.

### 4.4 What v2.1-A ships

- New API endpoint `GET /api/projects` (read-only; 4 tests cover
  success + boundary cases)
- React project picker in DeriveModal with loading / empty / picked
  states + submit-gating
- Full carry-over derive UI happy-path e2e (the test was previously
  stopping at "modal opens"; now runs end-to-end through navigate
  + CarryOverDivider assertion)
- Type-system enforcement that `project_id` is required on derive
  mutation inputs (any future caller that forgets it fails to
  compile)

### 4.5 v2.1-B + v2.1-C hand-off

- v2.1-B (SPA coverage): no dependency on this work. Independent.
- v2.1-C (Unread tracking): no dependency on this work. Per the
  cadence rule it gets per-ST P12 cadence (≥3 STs) when claimed.
- Both queued in task board (#3 + #4 in #agent-center channel).
