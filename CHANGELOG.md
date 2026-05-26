# Changelog

All notable changes land here. Format inspired by
[keepachangelog.com](https://keepachangelog.com/en/1.1.0/); semver
([semver.org](https://semver.org/)) for version numbers.

This project did not maintain a CHANGELOG.md before v2.0.0; commit
history is the authoritative record for v1. For the v2 design /
ADR / phase plan landscape, see
[docs/design/ddd-blueprint.md § 5](docs/design/ddd-blueprint.md#-5-v20-ga-status).

---

## [Unreleased] — v2.5.9

Sidebar collapsible groups + Channels/DMs sub-lists (#63).
@oopslink (`#agent-center:475113f5` screenshot → #63): the
sidebar's flat section labels mirrored the original v2.3 nav
layout, but with the surface growing (channels, DMs, projects)
the operator wanted a Slack-style collapsible grouping so they
could see all channels they had joined inline + collapse rarely
used groups out of the way.

### Added

- **Per-group collapse**: each top-level nav group (Workspace /
  Conversations / Work / System / Home) now renders its label
  as a button with `aria-expanded`. Click to collapse the
  group's items.
- **Channels + DMs sub-lists**: the Channels and DMs nav items
  now expand into a child list of `# channel-name` / `@ peer`
  links, with a per-item collapse toggle next to the link. Item
  count badge shows the list size.
- **localStorage persistence**: both group state
  (`ac.sidebar.groups`) and sub-item state
  (`ac.sidebar.subitems`) survive page reloads. Default for
  unseen keys is `true` (expanded) so first-time operators see
  everything.

### Backward-compatible

- No backend changes — the new sub-lists hydrate from the
  existing `useConversations({kind:'channel'/'dm'})` reads.
- Existing AppLayout tests still pass — group labels are still
  rendered as text inside the new buttons.

### Verification

- Frontend: 302 vitest specs green (7 new in
  `AppLayout.sidebar.test.tsx` cover group toggle / sub-list
  render / persistence).

---

## [v2.5.8] — 2026-05-27

Web Console Task management — Create + Suspend/Resume/Abandon
(#62, partial). PM created #65 to track Edit (title/description)
as a follow-up since it needs a new Task AR.UpdateMetadata
method.

### Added

- **POST /api/tasks** now branches between the existing CV4
  derive flow and a new create-from-scratch path. The new path
  wraps `TaskSvc.Create` and accepts project_id / title /
  description / parent_task_id / priority / requires_worktree.
- **POST /api/tasks/{id}/suspend** wraps a new
  `TaskService.Suspend` method (open → suspended). Caller is
  responsible for killing active executions first; the AR
  rejects suspend on non-open status.
- **POST /api/tasks/{id}/resume** wraps `TaskService.Resume`
  (suspended → open).
- **POST /api/tasks/{id}/abandon** wraps `TaskService.Abandon`
  (open/suspended → abandoned). Requires reason + message per
  conventions § 16.
- **TaskService** gains `Suspend`, `Resume`, `Abandon` lifecycle
  wrappers (each owns the tx + repo write + observability emit).
  Wires `TaskSvc` into the Web Console `HandlerDeps`.
- **Tasks page**: `+ New Task` button in the header. New
  `TaskCreateModal` (project / title / description / parent task
  id / priority / requires-worktree).
- **TaskDetail page**: `Suspend` / `Resume` / `Abandon` actions
  in the header, gated on current status. New `TaskAbandonModal`
  collects reason + message before the AR-required abandon call.

### Verification

- Backend: full `go test ./...` green; 8 new webconsole API
  tests cover create-from-scratch + suspend/resume/abandon
  (happy paths + status-rejected + not-wired) plus the derive
  path still routes.
- Frontend: 295 vitest specs green (6 new across
  `TaskCreateModal` + `TaskAbandonModal`).
- Out of scope (deferred to #65): Edit (title/description) —
  needs a new Task AR UpdateMetadata method; tracked separately.

---

## [v2.5.7] — 2026-05-27

Web Console Issue management — Create + Conclude (#61, partial). PM
created #64 to track Edit + Reopen as a follow-up since those
require new Discussion BC domain methods (UpdateMetadata + Reopen
with state-machine extension) and a spec discussion on the Reopen
semantics that #61 should not block on.

### Added

- **POST /api/issues** now routes to either the existing CV4 derive
  flow or a new open-from-scratch path, branching on whether the
  payload includes `source_conversation_id` / `source_message_ids`.
  The open-from-scratch path wraps `IssueLifecycleSvc.Open` with
  `OriginWebConsole` (sync-build: the sibling `kind=issue`
  Conversation is created in the same tx, so `issue.conversation_id`
  is bound immediately). Returns `{issue_id, conversation_id,
  event_id}`.
- **POST /api/issues/{id}/conclude** wraps
  `IssueLifecycleSvc.Conclude`. Accepts `kind` ∈
  {`closed_no_action`, `closed_with_tasks`, `withdrawn`} +
  `summary` + optional `tasks[]` (required for closed_with_tasks).
  Returns `{issue_id, task_ids, event_ids}`. Wires
  `IssueLifecycleSvc` into the Web Console `HandlerDeps` (was
  read-only on the Discussion BC up to v2.5.6).
- **Issues page**: `+ Open Issue` button in the header. New
  `IssueCreateModal` (project picker + title + description). After
  open, the new issue invalidates the project's issue list so it
  shows up without a page reload.
- **IssueDetail page**: `[Conclude]` action in the header (hidden
  when the issue is already in a terminal status). New
  `IssueConcludeModal` with three radio options + a dynamic task
  list that appears only for `closed_with_tasks`.

### Verification

- Backend: full `go test ./...` green; 7 new webconsole API tests
  cover open-from-scratch + conclude (no_action / withdrawn /
  invalid-kind / not-wired) plus the derive path still routes.
- Frontend: 289 vitest specs green (9 new across
  `IssueCreateModal` + `IssueConcludeModal`).
- Out of scope (deferred to #64): Edit (title/description) and
  Reopen — both require new Discussion BC AR methods + state
  machine extension; tracked separately so the spec discussion can
  land without blocking #61's mechanical wrap.

---

## [v2.5.6] — 2026-05-27

Channel / DM chat composer pin + auto-scroll (#60). @oopslink
(`#agent-center:475113f5`): the conversation pages let the
composer float in the middle of the page with empty space below,
instead of pinning to the bottom of the channel container the way
Slack / Discord do. The MessageList also stayed at the same
scroll position when new messages arrived, so the user had to
manually scroll to see them.

### Changed

- **AppLayout main pane** now hosts a flex column scroll wrapper
  (`flex h-full flex-col overflow-y-auto`) instead of a plain
  centered div. Lets routed pages stretch to the full visible
  height while still scrolling overflowing content. List pages
  (Channels / Issues / etc.) are unaffected — they fall back to
  the wrapper's own overflow when content exceeds the viewport.
- **MessageList** owns its own scroll container now (wrapped in
  a `relative flex min-h-0 flex-1 flex-col`) and auto-scrolls to
  the bottom when a new message arrives — but only when the user
  is already near the bottom (within 40px). Scrolled-up readers
  are not yanked back.
- **"New messages ↓" pill** appears at the bottom-center when a
  new message lands while the user is scrolled up. Clicking it
  jumps to the latest message.

### Verification

- Frontend: 280 vitest specs green (4 new MessageList tests
  covering pill visibility + scroll-stick heuristic).
- ChannelDetail / DMDetail / IssueDetail / TaskDetail keep their
  `h-full flex flex-col` root — the layout fix is purely upstream
  in AppLayout.

---

## [v2.5.5] — 2026-05-26

Project model simplification (#59) per @oopslink design discussion
in `#agent-center:68d33af4`. v2.5.3 shipped Project CRUD UI on
top of a model that still carried three fields the operator no
longer wanted: a user-typed slug `id`, a single `kind` enum, and a
`default_agent_cli`. v2.5.5 removes all three, slimming Project
to its essential shape (id / name / description / tags + audit
fields).

### Changed (BREAKING)

- **Project.id** is now **server-generated** in `proj-<8hex>`
  format, immutable from create. Operators no longer pick the
  slug; consistency with the v2.5 worker-id pattern.
- **`kind` field replaced by `tags []string`**. Tags are a free
  multi-value list with 6 builtin suggestions (`coding`,
  `research`, `ops`, `docs`, `experimental`, `archived`) plus
  free-text entry — the v2.5.5 frontend Create / Edit modals
  render a chip-style combobox.
- **`default_agent_cli` dropped**. Dispatch routing is the
  supervisor's responsibility per ADR-0011; the field was an
  unused shortcut. No replacement; supervisor sees the available
  AgentInstances on workers mapped to the project and chooses.

### Migration

No backward compatibility (per @oopslink `#agent-center:4a58a286`).
Migration 0032 drops `projects` + `worker_project_mappings` +
`worker_project_proposals` and recreates them with the new
schema. Any v2.5.3 / v2.5.4 install will lose its Project rows on
upgrade — operators must re-create projects after upgrading.

### Verification

- Backend: full `go test ./...` green; new schema round-trips;
  `make smoke` end-to-end.
- Frontend: 276 vitest specs green; Create / Edit modals show
  the simplified 3-field form (name + description + tags); list +
  detail surfaces drop kind / agent_cli columns.

---

## [v2.5.4] — 2026-05-26

@oopslink (#agent-center msg=464872a5): the `make release` tarball
shipped from v2.5.1+ only included the `./install` wrapper. Operators
running `./uninstall center` or `./upgrade center` directly from
the extracted release tarball got a "command not found" because
the wrappers didn't exist — only `bin/agent-center uninstall ...`
worked.

### Fixed

- **`make release`** now ships three POSIX shell wrappers at the
  tarball root: `./install`, `./uninstall`, `./upgrade`. Each
  one-liner forwards `"$@"` to its subcommand on the bundled
  `bin/agent-center` so the full install / upgrade / uninstall
  lifecycle is reachable without remembering the binary path.
  Workaround for tarballs already deployed: invoke
  `bin/agent-center uninstall ...` or `bin/agent-center upgrade ...`
  directly; the binary itself was always complete.

Verified end-to-end: `make release` produces a tarball that lists
all three wrappers at the top level. No code changes outside the
Makefile.

---

## [v2.5.3] — 2026-05-26

Project management UI completion (#58). @oopslink ask:
`agent-center project` CRUD was CLI-only since the v2.3-4 #30 ship
(per ADR-0037 W1.4); the v2.4/v2.5 trajectory reversed that
recommendation. v2.5.3 cuts the create / edit / delete /
worker-mapping CRUD into the Web Console directly so operators
don't context-switch into the CLI for routine project work.

### Added

- **Backend** — six new Web Console endpoints under `/api/projects`:
  - `POST   /api/projects`                            create
  - `PATCH  /api/projects/{id}`                       update (CAS on version)
  - `DELETE /api/projects/{id}[?force=true]`          delete (409 with
    counts when active tasks / open issues / mappings exist; ?force=true
    invalidates mappings and drops the row anyway)
  - `GET    /api/projects/{id}/workers`               list active mappings
  - `POST   /api/projects/{id}/workers`               create mapping
  - `DELETE /api/projects/{id}/workers/{mapping_id}`  invalidate mapping
- **Frontend** —
  - `Projects` page: "+ Add Project" header button + `ProjectCreateModal`
    (id / name / kind / default_agent_cli / description form).
  - `ProjectDetail`: Edit + Delete buttons in the header.
    `ProjectEditModal` lets the operator update name / kind /
    default_agent_cli / description with optimistic-lock CAS.
    `ProjectDeleteModal` walks the two-stage cascade flow — refuse
    first with dependency counts, then surface a force-delete with an
    "I understand" checkbox before allowing the destructive path.
  - `WorkersPanel` on ProjectDetail: combobox of all known workers
    from `/api/fleet` + path input → POST mapping; existing mappings
    list with per-row Unmap.
- **api/client.ts** gains a `patch()` method to feed the new mutations.
- **projectPublicMap** now emits `version` so the SPA can supply CAS
  values without a second fetch.

### Docs / notes

- Both PD pre-flight defaults from #agent-center:23d6fbd6 are honoured:
  cascade-on-delete is refuse-by-default with a force-delete path
  behind a second confirm + "I understand" checkbox, and the Map
  worker UI is a combobox sourced from `/api/fleet`.

---

## [v2.5.2] — 2026-05-26

Explicit `upgrade` subcommand. Reverses the scope-cut from v2.5.1
(@oopslink msg=8e5ea457): operators get a verb that says "I want
to upgrade" out loud instead of relying on `install center`'s
silent fresh-vs-upgrade auto-detect branch. The actual upgrade
path is unchanged — atomic symlink swap + health probe +
auto-rollback from v2.4-D-A5.

### Added

- **`agent-center upgrade center [--prefix=...] [--user-mode] [--dry-run]`**
  Refuses with a clear error if no install exists at the prefix
  ("upgrade_no_install — run `install center` first for fresh
  installs"). Same-version walks the idempotent no-op path; a
  different version walks the real upgrade. Mirrors the
  install-center flag surface.
- **`agent-center upgrade worker --worker-id=<id> [...]`**
  Same shape, scoped to the worker subtree. `--worker-id` is
  required.

### Behaviour difference vs `install center`

| state         | `install center`           | `upgrade center`        |
|---------------|----------------------------|-------------------------|
| Fresh prefix  | walks fresh path            | refuses; exits 2        |
| Same version  | idempotent no-op           | idempotent no-op         |
| Different ver | atomic-swap upgrade        | atomic-swap upgrade     |

Existing `install center` retains its auto-detect behaviour so
v2.4/v2.5 scripts keep working. Operators who want the explicit
verb now have it.

---

## [v2.5.1] — 2026-05-26

Post-v2.5 uninstall command — closes the gap @oopslink flagged in
#agent-center msg=74fb3fa6: there was no way to undo
`install center` / `install worker` without manually unloading
launchctl and rm-rf'ing the prefix. Upgrade was already wired in
v2.4-D-A5; this cycle just adds the inverse.

### Added

- **`agent-center uninstall center [--prefix=...] [--purge] [--yes] [--dry-run]`**
  Stops + unloads the service, removes the unit file +
  `<prefix>/versions/` + `<prefix>/current` + `<prefix>/etc/`.
  Preserves `<prefix>/var/` (sqlite + master.key + tokens) and
  `<prefix>/logs/` by default so a subsequent reinstall at the
  same prefix reuses the existing data — verified end-to-end:
  install → checksum master.key → uninstall (no purge) →
  reinstall → master.key identical.
- **`agent-center uninstall worker --worker-id=<id> [...]`**
  Same flag surface, scoped to `<prefix>/workers/<id>/`. Sibling
  workers + the center install are untouched.
- **`--purge`** opt-in destructive mode: wipes `var/`, `logs/`,
  and the prefix itself. Interactive `yes` prompt by default;
  `--yes` skips it for scripted teardown.
- **`--dry-run`** prints the full plan (every shell command + every
  `rm -rf` target) without mutating state.

### Docs

- `docs/deployment/v2.4-first-mile.md § 4.5 Journey D — uninstall`
  walks all four flag combinations + shows the preserved-vs-purged
  output. Reinstall-on-preserved-var path explicitly verified.

### Why no `upgrade` alias

`install center` (and `install worker`) already auto-detect the
"upgrade" state and walk the atomic-symlink-swap / health-probe /
auto-rollback path from v2.4-D-A5 (see § 4 Journey C). Adding an
`agent-center upgrade` alias would split a single product action
into two operator-visible commands without changing behaviour;
PD-led design discussion in #agent-center:5f6288e6 retired the
alias from this cycle's scope.

---

## [v2.5.0] — 2026-05-26

Add Worker flow redesign — split the logical "add a worker"
(creates a record, status=offline) from the physical
"install the worker" (operator runs `./install worker` on the
worker machine). Per @oopslink design statement in
#agent-center:5f8a6f7e (msg=61fcab27): "添加是逻辑动作 = 创建记录
status=offline；用户在机器上 install 后 worker 上线时 update status".

### Highlights

- **Add Worker Modal collapses from 7 states to 3** (name_prompt /
  minting / mint_error). Clicking Add closes the Modal immediately;
  the new offline row appears in Fleet via SSE.
- **Per-row install command actions** on offline Fleet rows: Show
  install command (re-displays the original token while alive) and
  Re-mint install command (revokes old + issues fresh, refuses when
  the worker is already online).
- **Remove worker** action on every Fleet row: revokes the worker's
  admin tokens (long-term + any active install token) and drops the
  Worker record. SSE retires the row in other tabs automatically.
- **Plaintext-never-at-rest invariant preserved** (ADR-0026): enroll
  token plaintext is AES-GCM-encrypted with the same `master_key`
  UserSecret BC uses, only for the install-command re-display flow,
  and NULL-ed on first use.

### Added

- `WorkerEnrollService.AddWorker` + `RemoveWorker` (workforce
  service) + `Worker.Delete` (repository).
- `AdminTokenService.ShowInstallToken` /
  `RevokeAllForWorker` / `RevokeActiveEnrollForWorker` /
  `HasLongTermTokenForWorker` + `WithMasterKey` config.
- Webconsole endpoints:
  - `GET /api/workers/{id}/install-command` (B2)
  - `POST /api/workers/{id}/install-command/re-mint` (B3)
  - `DELETE /api/workers/{id}` (B4)
- SPA: `InstallCommandModal` component, Fleet row Actions column
  (Show install / Re-mint install / Remove buttons), SSE handler
  for `workforce.worker.added` + `workforce.worker.removed`.

### Schema (migration 0031)

- `admin_tokens.worker_id TEXT NULL` — binds the row to a Worker AR.
- `admin_tokens.plaintext_ciphertext BLOB NULL` +
  `plaintext_nonce BLOB NULL` — AES-GCM-encrypted bearer for the
  show-install-command flow. NULL for long-term tokens and after
  `ConsumeEnrollToken`.
- Partial index `idx_admin_tokens_worker_id` on
  `(worker_id) WHERE is_enroll = 1 AND worker_id IS NOT NULL`.

### Events

- New `workforce.worker.added` — emitted by `AddWorker` so SSE
  paints the Fleet row before the daemon enrolls.
- New `workforce.worker.removed` — emitted by `RemoveWorker` so
  Fleet rows in other tabs retire automatically.

### Docs

- `docs/deployment/v2.4-first-mile.md § 3` rewritten for the v2.5
  decoupled flow (add ≠ install + Show / Re-mint / Remove actions).
- `docs/plans/v2.4-deployment-ui-design.md § 4` marked archived —
  States 1/3/6 of the old Modal state machine are retired; the
  rationale stays as v2.4 design history.

---

## [v2.4.1] — 2026-05-26

Post-v2.4.0 polish from real-binary dogfood on @oopslink's machine.
No new feature surface; all changes target install ergonomics on a
greenfield deploy (no existing v2.4.0 installs in the field).

### Changed

- **Install prefix unified to `~/.agent-center`** across Mac and
  Linux user-mode (#agent-center msg=68b04496). The previous per-OS
  defaults (`~/Library/Application Support/agent-center` on Mac;
  `~/.local/share/agent-center` on Linux user) scattered the install
  across three conventions and were hard to find from a terminal.
  Linux system-mode keeps `/opt/agent-center` since `~/.agent-center`
  resolves to root's home (wrong for a system daemon).
- **Worker subtree relocated to `<base>/workers/<id>/`** so a center
  install at `<base>/{bin,etc,var,logs}/` and each worker at
  `<base>/workers/<id>/{bin,etc,var,logs}/` nest under one tree
  instead of scattering peer `worker-<id>/` dirs at home root.
- **launchd `StandardOutPath` / `StandardErrorPath`** moved from
  `/tmp/<label>.{out,err}.log` to `<prefix>/logs/<label>.{out,err}.log`,
  so daemon logs survive reboot and live alongside the rest of the
  install (no more `/tmp` scavenging when a worker fails to enroll).

### Added

- **`make release`** — host-platform tarball at
  `dist/agent-center-vX.Y.Z-<os>-<arch>.tar.gz`, with bundled
  `./install` POSIX shell wrapper that delegates to
  `bin/agent-center install <args>` (symlink would lose the
  subcommand prefix). Prints sha256 + verify recipe.
- **`make clean-dist`** — removes `./dist` tarballs.
- **`web/pnpm-workspace.yaml`** declares both `allowBuilds:` map
  (pnpm 10.31+) and `onlyBuiltDependencies:` list (older pnpm)
  for `esbuild` + `msw`, eliminating the
  `ERR_PNPM_IGNORED_BUILDS` warning that broke `make build`.

### BREAKING CHANGE

- The unified `~/.agent-center` layout is a hard break — there is
  no auto-migration from the v2.4.0 paths. Justification:
  v2.4.0 only saw single-user dogfood and the operator opted in
  ("不考虑向下兼容，现在还没有实际部署的环境", msg=68b04496).
  Manual move recipe for any straggler v2.4.0 install:
  ```
  systemctl --user stop agent-center            # or: launchctl unload <plist>
  mv ~/Library/Application\ Support/agent-center ~/.agent-center  # mac
  mv ~/.local/share/agent-center ~/.agent-center                  # linux
  # reinstall to refresh service unit + log paths:
  ./install center --prefix=~/.agent-center
  ```

---

## [v2.4.0] — 2026-05-26

> v2.3 work landed on `main` between v2.2 and v2.4 without its own
> tag — its highlights are summarized below under "v2.3 carry-over"
> so the v2.2 → v2.4 diff stays readable.
>
> PD-as-verifier note: this release went through 5 rounds of acceptance
> bounce (@AgentCenterPD on Mac arm64). 10 ship-blockers + 4 polish
> items + 1 architecture-class bug surfaced in the process, all
> resolved before tag. See the "PD-acceptance bounce summary" section
> below for what each round caught.

### Highlights

v2.4 ships the **first-mile deployment** experience that v2.0 GA was
missing. Before v2.4 you assembled the worker invocation by hand
(fingerprint, bearer token, capabilities, etc.); now `./install center`
and a Web Console **Add Worker** Modal cover the path from extracted
tarball to running worker in well under a minute on Mac.

### Added (v2.4-D first-mile)

- **`agent-center install center|worker` subcommand** — single
  idempotent command for fresh install + upgrade, with cross-OS
  service unit generation (launchd on Mac, systemd on Linux).
  Default `--tcp-listen=0.0.0.0:7300` so the Web Console can hand
  out usable worker install commands out of the box.
- **Atomic symlink-swap upgrade with auto-rollback** — new version is
  laid down at `<prefix>/versions/<new>/`, the schema migration runs,
  `<prefix>/current` is flipped via POSIX `rename(2)`, and the
  installer probes the health endpoint. Probe failure → automatic
  symlink rollback + service restart.
- **One-time-use enroll tokens** — new `AdminToken` flavor with
  `is_enroll + expires_at + used_at` columns (migration 0029).
  30-minute default TTL; CAS-based first-use-burns via
  `used_at IS NULL` in the auth middleware. Coexists with v2.3-3a
  long-term tokens.
- **Long-term worker token exchange** — `/admin/workforce/worker/enroll`
  mints a worker-scoped `AdminToken` (`workforce:enroll`,
  `dispatch:pull`, `task:*`, `secret:resolve`, `blob:put`) and
  returns it in the response. Worker daemon persists it at
  `<dataDir>/worker-token` (mode 0600, atomic tmp+rename) and
  swaps its bearer; the one-time enroll token only carries the
  first request. On restart the daemon reads the persisted token
  and skips re-enroll — `launchd`/`systemd` recycles are
  transparent (Day-2 Mac restart no longer drops the worker).
- **Web Console Add Worker UX** — `/fleet` top-bar **+ Add Worker**
  button + `AddWorkerModal` (`name_prompt` → `minting` → `ready` →
  `success` / `token_used` / `token_expired` / `timeout_hint` /
  `mint_error`) showing a copyable install command. Modal asks
  for a friendly worker name first; server generates the
  immutable `worker-<8hex>` id; both flow into `--worker-id` +
  shell-quoted `--worker-name`. Live transition to **Worker
  connected** via SSE `workforce.worker.enrolled`. Newly-enrolled
  Fleet rows briefly pulse green; a global toast in the
  bottom-right acts as fallback when the Modal is closed.
- **Worker id/name split + inline rename** — id is server-generated
  and immutable; name is editable post-enroll. New
  `PATCH /api/workers/{id}/name` endpoint backs the inline-edit
  on the Fleet row (`WorkerNameCell`). Migration 0030 adds the
  `workers.name` column (backfilled to `id` for pre-existing
  rows); `workforce.worker.renamed` SSE event keeps every tab in
  sync.
- **Worker liveness state machine** — `Heartbeat` CAS-transitions
  `offline → online` and emits `workforce.worker.online`;
  `HeartbeatReconciler` scans every 30s and flips workers to
  `offline` after 60s of silence (`workforce.worker.offline`),
  anchored on `max(enrolled_at, last_heartbeat_at)` so freshly
  enrolled workers aren't false-flagged inside their first-tick
  window. Before this, the Fleet view stayed pinned on `offline`
  forever even while heartbeats landed.
- **Multi-worker per machine** — launchd labels + systemd unit
  names scope by worker-id (`com.agent-center.worker.<id>` /
  `agent-center-worker-<id>.service`); default `--prefix` adds a
  `worker-<id>/` suffix so two workers on one host don't trample
  each other's SQLite, token file, or service registration.
- **Home Get-started card** — Home page shows a prominent **Add a
  worker** CTA when no workers are enrolled, so the first-mile gap
  is visible on the landing surface.
- **Sidebar Fleet entry** — `Fleet` route exposed in the System
  nav group so operators can navigate back to the worker list
  from any page after closing the Modal.
- **`install worker` waits for daemon to enroll** — installer tails
  the launchd stderr log for the daemon's success / failure
  marker before claiming `✓ installed + connected`. On failure
  prints the last 12 log lines + a concrete "To retry from
  scratch:" recipe (`launchctl unload …; rm worker-token; ./install
  worker --token=<NEW>`).
- **Friendly install failure messages** — disk full, port in use,
  permission denied (systemd unit / binary write), upgrade health
  probe failure all map to `<friendly> / What to try / Underlying`
  output instead of raw syscall errors. Preflight port-availability
  check runs before service activation.
- **`/api/health` reports the linker-injected version** — was
  hard-coded to `"v2-dev"`; now echoes the same `buildVersion`
  the `install` command prints so the operator sees a coherent
  story.

### Added (v2.3 carry-over — already on `main`)

- **Multi-host TCP+TLS admin endpoint** with SSH-style fingerprint
  pinning, per-token bucket rate limiting, and audit IP capture.
  See [docs/deployment/v2.3-multi-host.md](docs/deployment/v2.3-multi-host.md)
  for the operator walkthrough — still authoritative for the cross-
  host internals.
- **Real agent dispatch chain** — `/admin/secret/user-secret/resolve`,
  `/admin/blob/put`, `defaultAgentSpawner` wires `AssemblePrompt` +
  `MCPInjector`. Previously v2.2 wired the transport but the agent
  spawn was a stub.
- **`AdminToken` AR + middleware + CLI** — `agent-center admintoken
  create/list/revoke` for long-lived per-worker tokens. v2.4-D's
  enroll-token model layers on top.
- **BC-native `/api/issues` + `/api/tasks` list endpoints** + SPA
  surfaces driven by them (project filter is now a real filter, not
  cosmetic).
- **SPA polish** — DeriveModal project picker, unread tracking
  schema + service + frontend, per-conversation SSE subscribe, Web
  Console UX/UI overhaul, Home `bento-grid` dashboard.

### Fixed (pre-existing latent bug surfaced during acceptance)

- **SSE typed events were silently dropped on real browsers since
  v2.0** — `writeSSE` emitted the W3C `event:` field on every line,
  which routes browser EventSource delivery to
  `addEventListener(<type>, …)` rather than the `onmessage` handler
  that `useSSE` was actually listening on. The fake `EventSource`
  used in tests bypassed spec dispatch entirely so 28+ green tests
  masked the gap. Server now emits just `id:` + `data:`; event_type
  stays inside the JSON payload where `dispatchToQueryClient`
  already switches on it. Fixes every SSE-driven invalidation that
  v2.0/v2.1/v2.2/v2.3 silently shipped broken — unread badges,
  agent state changes, input-request inbox push, Fleet refresh,
  conversation read-state. Found and fixed during the v2.4-D-X1
  acceptance bounce; see the bounce summary below.

### Schema

- **migration 0029** — `admin_tokens` gains `is_enroll`,
  `expires_at`, `used_at` columns + partial index for the
  enroll-token sub-table.
- **migration 0030** — `workers` gains `name TEXT NOT NULL DEFAULT
  ''`; pre-existing rows backfilled to `name = id` so the Fleet
  projection always renders a non-empty value.
- `targetSchemaVersion` bumped 28 → 29 → 30.

### PD-acceptance bounce summary

@AgentCenterPD ran 5 rounds of acceptance on a clean Mac. Each round
exercised the first-mile journey end-to-end with real binaries, in
order to validate ship-readiness against the actual user path (not
against test-double green). The mapping of bounce-round → root cause
is preserved in the commit history; condensed list:

- Round 1: `install center` worked but the Modal handed out a
  placeholder enroll token (the mint endpoint wasn't wired), the
  install command was missing `--server-fingerprint` and a host, the
  worker daemon plist prepended non-existent positional args, install
  printed `v-dev` instead of `v2.4.0`, the Modal copy hard-coded a
  fake tarball dir, `launchctl unload` noise leaked, sidebar didn't
  expose Fleet. 4 ship-blockers + 4 polish.
- Round 2: the worker daemon kept reusing its burned enroll token for
  every heartbeat → 401-loop. Server now mints a long-term worker
  token and the daemon persists it (mode 0600). Separately, every
  typed SSE event was silently dropped on real browsers (see "Fixed"
  above). 3 ship-blockers.
- Round 3: worker stayed pinned on `offline` while heartbeats kept
  arriving (`Heartbeat` never transitioned the status field; nothing
  flipped it back to offline on stall). Reconciler + transition path
  added. `install worker` claimed `✓ installed` before the daemon had
  even tried to enroll — installer now waits for the daemon success
  / failure marker. `/api/health` reported a stale version literal.
  @oopslink extended scope to multi-worker per machine; launchd
  labels + install prefix now scope by worker-id. 1 ship-blocker +
  2 polish + 1 scope add.
- Round 4: clean retry verification — all of the above landed.
- Round 5: id/name split landed (server-generated immutable id +
  user-typed editable name; Fleet inline rename) and clean
  retry verification of the full first-mile journey.

### Docs

- New: [docs/deployment/v2.4-first-mile.md](docs/deployment/v2.4-first-mile.md)
  — operator guide for install / enroll / upgrade / rollback / 12
  failure modes.
- The v2.3 multi-host guide is unchanged and remains authoritative
  for fingerprint hygiene, rate-limit tuning, and cross-host
  internals.

### Deferred to v3 (or later v2 minors)

- Tarball distribution (downloads.agent-center.dev etc.) — v2.5+
- New SSE events `worker.enroll_attempt_failed` +
  `admintoken.expired` — these are nice-to-have for richer Modal
  feedback; the client's 5-min timeout state covers the silent-fail
  case in v2.4. See audit
  [v24-D-A4](docs/plans/v2.4-audits/v24-D-A4-sse-events-audit.md).
- Linux acceptance — v2.4 scope is Mac-only per @oopslink's
  acceptance scope. Linux units are written + unit-tested but not
  acceptance-validated; that lands in v2.5.

---

## [v2.2.0] — 2026-05-25

⚠ **MINOR VERSION** with one breaking config-default change
(Web Console default flipped to ON). Full upgrade procedure in
[docs/migration/v2.0-to-v2.2.md](docs/migration/v2.0-to-v2.2.md).

### Highlights

v2.2 closes the v2.0 GA defect that @oopslink surfaced on 2026-05-24
("前端 + 数据面完整，但 worker process 装配从未交付"). v2.0/v2.1
shipped without an actual worker process, without admin transport
between CLI and server, and with `dispatch.NoopSender{}` wired into
production — dispatched tasks went into /dev/null. v2.2 ships the
full transport architecture per `conventions.md § 0.4` ("AppService
is the only entry to domain state").

### Added

- **`cmd/worker-daemon` binary** — separate process that connects to
  the server via admin unix socket, enrolls, polls the dispatch + kill
  queues, spawns the agent CLI subprocess, and reports back via admin
  endpoints. Replaces the placeholder `agent-center worker run` that
  v2.0 GA shipped as "reserved for Phase 2".
- **`cmd/fakeagent` binary** — scripted agent stub for LLM-independent
  testing. Used by the deployed-binary e2e smoke and operator
  manual-verify recipes.
- **Admin endpoint (unix socket)** — `internal/admin/api` package with
  93 routes covering the full CLI AppService surface, per BC. Default
  socket path `/run/agent-center/admin.sock` (configurable via
  `server.admin_socket_path`). Per ADR-0037 still loopback only;
  multi-host TCP reserved for v2.3 (ADR-0040).
- **In-process dispatch queue** (`internal/admin/dispatchq`) — real
  `EnvelopeSender` + `KillSender` backed by per-worker FIFO. Worker
  daemons drain via admin endpoint.
- **Real `SupervisorSpawner` wired in ServerCommand** — supervisor
  invocations actually fork+exec. v2.0 GA had `app.SupervisorSpawner = nil`.
- **Deployed-binary smoke gate** — `make smoke` runs Phase D Playwright
  spec end-to-end against real binaries (no mocks). New
  `tests/e2e/v2/tests/v22-deployed-pipeline.spec.ts` drives a task
  through `submitted → working → completed`.
- **Process gates** (per `conventions § 0.4` Enforce mechanisms):
  - `make lint-mock-default` — `NoopSender{}` / `NoopKillSender{}` in
    production wiring must carry `// FIXME(prod-wiring):` annotation.
  - `make lint-doc-impl-drift` — anchor-based check for documented
    architecture claims vs codebase reality.
  - `TestArch_NoDirectPersistenceOpenInHandlers` — enforces
    `internal/cli/handlers_*.go` whitelist.
- **Layered test report standard** (`docs/rules/testing.md § 2.3`) —
  unit / integration-with-mocks / deployed-binary-smoke must be
  reported separately; deployed-smoke = 0 means the phase MUST NOT
  close.
- **v2.0 → v2.2 upgrade guide** (`docs/migration/v2.0-to-v2.2.md`).
- **Mac single-host deployment guide** (`docs/deployment/v2.2-mac-single-host.md`).

### Breaking changes

1. **Web Console default flipped to ON**. `config.WebConsoleConfig`
   default seeds `Enabled: true, ListenAddr: "127.0.0.1:7100"`. v2.0
   configs that omitted `web_console.enabled` ran headless; v2.2 such
   configs now boot the SPA on loopback. Opt out with explicit
   `web_console: {enabled: false}`. See migration guide § 2.1.

### Refactor

- **CLI through admin transport** — all 36 CLI subcommands now route
  through admin endpoint via `internal/cli/admin_client.go` instead
  of opening sqlite directly. Whitelist: `handlers_migrate*.go` and
  `handlers_system.go` only.
- **`dispatch.NoopSender` + `kill.NoopKillSender` removed from
  production wiring** — replaced with `dispatchq.DispatchSender` and
  `dispatchq.KillSender`. The Noop variants remain in their packages
  as legitimate test doubles (with `// FIXME(prod-wiring):` annotations
  on the constructor fallback paths).
- **`internal/workerdaemon/` package** — previously ~2500 LOC never
  imported in production; v2.2 wires it through `cmd/worker-daemon`.

### Known follow-ups (v2.3 backlog)

Filed in `docs/plans/v2.2-audits/v22-closeout-audit.md § 4`:
participant/leave endpoint, msg/find-recent endpoint, dispatch +
DecisionRecord same-tx, kill + DecisionRecord same-tx,
read-task-context endpoint, worker heartbeat endpoint, MCP injection
wire, artifact blob upload, multi-host TCP transport.

---

## [v2.0.0] — 2026-05-24

⚠ **MAJOR VERSION**. Read the [breaking changes](#breaking-changes)
section below before upgrading. The full operator upgrade procedure
is in [docs/migration/v1-to-v2.md](docs/migration/v1-to-v2.md).

### Breaking changes

1. **`migrate` CLI command refactored into a group**

   | v1 | v2 |
   |---|---|
   | `agent-center migrate` | `agent-center migrate up` |
   | `agent-center migrate --target=N` | `agent-center migrate up --target=N` |
   | _(did not exist)_ | `agent-center migrate v1-to-v2 --dry-run` |
   | _(did not exist)_ | `agent-center migrate v1-to-v2 --apply` |

   Why: v2 introduces a second migration verb (`v1-to-v2`), and the
   router requires a leaf-vs-group split. Existing schema-up behavior
   is preserved verbatim under `migrate up`.
   Action: update any scripts that invoke `migrate ...` to use
   `migrate up ...`.

2. **Bridge BC + vendor IM integration removed**
   (per [ADR-0031](docs/design/decisions/0031-v2-drop-bridge-vendor-integration.md))

   Feishu / Lark / Bridge BC tables / vendor adapters deleted. v2
   exposes Web Console (loopback bind only) + CLI as the only user
   entry points. **If you depend on vendor IM, do not upgrade** until
   v3 re-introduces external IM with a new architecture.

3. **Identity model 4 kinds → 3 kinds**
   (per [ADR-0033](docs/design/decisions/0033-identity-model-refactor.md))

   v1 supported `user / agent / supervisor / bot`. v2 supports
   `user / agent / system`. Migration 0021 DELETEs identities with
   v1-only kinds; the `migrate v1-to-v2` tool runs this automatically.

4. **Conversation v2 unified model**
   (per [ADR-0039](docs/design/decisions/0039-conversation-business-model-v2-unified.md))

   `Conversation.kind` value `group_thread` is renamed to `channel`.
   `kind=task` is 1:1 with Task; `kind=issue` is 1:1 with Issue (the
   v1 separate `IssueComment` table is gone — issue discussion lives
   as Messages in the Issue's bound Conversation). ADR-0017 / 0021 /
   0022 are superseded. Migration 0024 handles the rename
   automatically.

5. **SecretManagement BC introduces master.key + single-node only**
   (per [ADR-0026](docs/design/decisions/0026-user-secret-management-bc.md))

   v2 requires `secret_management.master_key_file` set in config + a
   32-byte AES-256 key on disk (mode 0600). Without it, the secret
   service is disabled (every secret endpoint returns 501).

   **Operational caveat — v2 is single-node by design**: multi-machine
   installs each maintain their OWN master key + UserSecret set;
   cross-machine secret sync is a v3 candidate (KMS adapter). If you
   run multiple agent-center instances, do not rely on master keys
   matching across machines. See
   [docs/operations/master-key.md](docs/operations/master-key.md)
   for generation / backup / rotation procedures.

6. **`notification.*` + `bridge.*` config sections removed**

   v2 rejects unknown YAML keys (per the `04-configuration § 4`
   strict-validate rule) — these sections will cause startup failure
   if left in place. Strip both before upgrading.

### Added

- **Web Console v2** — React SPA bundled into the single binary via
  go:embed; 13 pages cover channel / DM / issue / task / agent /
  secret / input-request / fleet
  ([ADR-0037](docs/design/decisions/0037-web-console-as-main-user-ui.md))
- **SecretManagement BC** — `UserSecret` AR + master-key-encrypted
  at-rest + plaintext-never-echo guarantee
  ([ADR-0026](docs/design/decisions/0026-user-secret-management-bc.md))
- **AgentInstance first-class entity** + lifecycle CLI
  ([ADR-0024](docs/design/decisions/0024-agent-instance-first-class.md)
  / [ADR-0025](docs/design/decisions/0025-agent-create-via-cli-not-protocol.md))
- **Worker enroll** bootstrap-token exchange
  ([ADR-0023](docs/design/decisions/0023-worker-enroll-lightweight.md))
- **AgentAdapter v2 matrix** — claudecode + codex + opencode
  adapters ([ADR-0030](docs/design/decisions/0030-agentadapter-matrix-expansion.md))
- **MCP per-agent injection**
  ([ADR-0027](docs/design/decisions/0027-mcp-per-agent-injection.md))
- **Skill file mount** — `assets/skills/supervisor.md`
  ([ADR-0028](docs/design/decisions/0028-skill-file-mount-lite.md))
- **Conversation v2**: channel first-class (CV1) / Identity refactor
  (CV2a) / Participants JSON (CV2b) / Cross-conv message carry-over
  (CV3) / Issue+Task derive-from-messages (CV4) — ADRs 0032 / 0033 /
  0034 / 0035 / 0036 / 0039
- **CLI UX**: `--format=table|json|text` universal flag + grouped
  help + topic index
  ([ADR-0038](docs/design/decisions/0038-cli-ux-enhancement.md))
- **`agent-center migrate v1-to-v2`** migration tool: `--dry-run` /
  `--apply` / idempotent / bridge-archive JSON
- **Playwright e2e suite** — 12 cases / 7 spec files; opt-in via
  `make e2e`; dual-mode chromium-mac + chromium-linux config
- **v1 vendor lint guard** — `make lint-vendor` + positive-fail
  self-test (`make lint-vendor-selftest`)
- **Operator docs**:
  [v1→v2 migration guide](docs/migration/v1-to-v2.md) +
  [master_key operations](docs/operations/master-key.md)
- **Migration round-trip + v1 column/table/kind absence guard
  tests** in `internal/persistence/migration_round_trip_test.go`

### Changed

- Bounded-context composition: Bridge removed, SecretManagement
  added (net BC count unchanged at 7)
- Issue discussion model: separate `IssueComment` table is gone;
  Issue messages live as `Message` rows in the `kind=issue`
  Conversation per ADR-0039
- Roadmap restructured into three sections: **v2 ✅ 已完成** /
  **v2.1 backlog** / **v3 推迟**
- Decisions/README + all design docs polished for v2; v2 banner
  applied to 16 tactical / implementation docs
- 17 v2 ADRs (0023-0039) promoted from `decisions/drafts/` to
  `decisions/` with `Status: Accepted` + evidence-trail Delivered row

### Removed

- Bridge BC (`internal/bridge/*` deleted in P10 § 3.9)
- Feishu / DingTalk vendor adapters + WebSocket outbound
- v1 ADRs **0009 / 0017 / 0020 / 0021 / 0022** (one-time exception
  to "never delete ADRs" per ADR-0031)
- v1 vendor config sections (`notification.*`, `bridge.*`)
- v1 vendor identity kinds (`supervisor`, `bot`)
- Schema artifacts: `vendor_msg_ref`, `channel_bindings`,
  `feishu_delivery_ledger`, `bridge_subscription_cursors`,
  `conversations.{primary_channel_hint, primary_channel_thread_key,
  title}`, `workers.capabilities`

### Deprecated

None. v2 has no deprecation period — every v1 surface either
survived intact, was breaking-changed, or removed outright.

### v2.1+ backlog

Explicitly deferred (see [docs/plans/v2.1-backlog.md](docs/plans/v2.1-backlog.md)
+ [roadmap.md](docs/design/roadmap.md)):

- Unread message tracking (per-conv read state)
- SPA coverage micro-pass (98.6% → 100% lines)
- DeriveModal project picker (full submit-to-navigation e2e)
- Worker-chain e2e via docker compose (NACK→Issue / dispatch /
  execute) — v3 deployment e2e candidate
- chromium-linux Playwright CI integration
- KMS / vault-backed master key (multi-machine secret sync)

---

[v2.0.0]: https://github.com/oopslink/agent-center/releases/tag/v2.0.0
