# Changelog

All notable changes land here. Format inspired by
[keepachangelog.com](https://keepachangelog.com/en/1.1.0/); semver
([semver.org](https://semver.org/)) for version numbers.

This project did not maintain a CHANGELOG.md before v2.0.0; commit
history is the authoritative record for v1. For the v2 design /
ADR / phase plan landscape, see
[docs/design/ddd-blueprint.md § 5](docs/design/ddd-blueprint.md#-5-v20-ga-status).

---

## [v2.11.0] — 2026-06-20

Agent 提醒 / 定时（Reminder）— agents and humans can schedule one-time or recurring (cron) reminders that wake a target agent at the due time.

### Added

- **Agent reminders (I4).** A new Cognition-side Reminder capability: create **one-time** or
  **cron** reminders for an agent in your own project; at the due time the reminder content is
  delivered as a **directed message** that wakes the remindee. Full lifecycle — pause / resume /
  cancel, end conditions (never / until / max-count), and **skip-if-overlap** (skip the next
  fire if the previous one hasn't been delivered yet). Managed from the new **Reminders** page
  (list with scope filters 全部 / 我创建的 / 提醒我的, status filters, stats, history) and over
  MCP tools + admin API.
- **Deliver-as-creator toggle.** Reminders carry a `deliver_as_creator` flag — the fired message
  is sent under the **creator's identity** (default) or the system identity.

### Security / guardrails

- **Same-project guard.** An agent may only set reminders for agents **in its own project**
  (owner is exempt); cross-project attempts are rejected (HTTP 403 `cross_project_reminder`).
- **Manage authz (class-guard).** Only the creator or the org owner may pause / resume / cancel a
  reminder; others get 403 `reminder_forbidden`. List visibility: owner sees all, non-owners see
  only their own.

### Fixed

- **Self-reminder no longer mis-flagged cross-project (T229).** A `switch` first-match bug made an
  agent's reminder *to itself* resolve as cross-project (403) — the core I4 use case. Resolution
  now uses independent checks so self-reminders pass the guard.
- **Fired reminders actually deliver (T239).** The `fired` event is appended to the outbox so the
  remindee is delivered / woken.
- **Real delivery outcome + overlap (T240).** Firing records `pending` (in-flight) rather than a
  hardcoded `delivered`; the delivery projector writes back `delivered` on success, so
  `skip_if_overlap` has a real in-flight state to read.

---

## [v2.10.3] — 2026-06-16

All-conversation file attachments, agent issue-management tools, Work Board task re-home, and a full sites doc-site redesign.

### Added

- **All-conversation file attachments (T167).** The agent file-domain now includes the
  conversations of **plans** the agent participates in (channel / DM / task / issue already
  worked) — so agents can upload and receive image & file attachments in **Plan chat** too,
  closing the last gap so all four conversation types support attachments. Membership-gated,
  fail-closed; non-participant / cross-org plan chats stay 403.
- **Agent issue-management tools (T170).** Agents get a full issue toolset over MCP —
  `create` / `update` / `close` / `reopen` / `comment` / `list` / `link-task` — so issue
  lifecycle no longer needs a human, at parity with the Web Console (project-member-gated,
  fail-closed).
- **Sites doc-site redesign (T169).** The `sites/` doc site is rebuilt on the product's
  Web Console design system (light slate/blue): chart-forward, low-text overview pages with
  long-form content on linked doc pages; a gamified interactive **DDD architecture browser**
  (context map → layered app/domain/infra → entity class diagram + sequence diagram); a
  per-version user manual + architecture (v2.7.1 / v2.8 / v2.10.0 / v2.10.2) with a version
  switcher; and a theme-grouped roadmap. The DDD model was re-derived from source to match code.

- **Work Board: drag a task to change its owning plan (T121).** Task cards can now
  be dragged across columns to re-home them — Backlog ↔ Assignment Pool ↔ draft
  plans, in any direction. The **Assignment Pool** is a full drag participant: its
  cards are draggable out (to the Backlog or a draft plan) and it accepts tasks
  dragged in. Both the mouse (HTML5 DnD) and touch long-press paths are covered.

### Changed

- **The built-in Assignment Pool's task-set is now freely editable (T121).**
  `RemoveTaskFromPlan` now exempts the always-running built-in pool from the
  draft-only gate, mirroring the long-standing add-side exemption — so removing a
  task from the pool (the remove-half of a Work Board move) no longer fails with
  `plan_conflict`. Structured `running` / `done` / `archived` plans stay **locked**
  at the service layer (add+remove both rejected, fail-closed) and the Work Board
  now shows that lock explicitly: a padlock on locked plan columns + cards, a
  reason tooltip, and a no-drop banner while dragging over a locked column.
  See [ADR-0049](docs/design/decisions/0049-workboard-task-move-pool-symmetry.md).

## [v2.10.1] — 2026-06-15

Mobile experience for the three-column shell + open claimability + desktop / Plan UI enhancements.

### Added

- **Mobile layout for every module (<768px).** The v2.10.0 three-column desktop
  shell adapts below 768px to a mobile layout: a **bottom Tab bar** (Workspace /
  Conversations / Members / System, col① top modules) drives a full-screen list
  (col②) → full-screen detail (col③) → bottom-sheet context (col④) flow.
  Conversations, the Workspace Tasks / Issues lists (table → **card flow**),
  Members, and System all adapt; the breakpoint flips cleanly at 768 with no
  horizontal overflow, and `≥md` keeps the v2.10.0 three columns.
- **Plan DAG → vertical stepper (mobile).** On mobile the Plan detail keeps its
  Chat / DAG / Task tabs, and the DAG renders as a **vertical stepper** (nodes +
  dependency rail + status colors stacked, paused nodes shown).
- **Work Board mobile.** Portrait scrolls the columns (Backlog / each Plan / New
  Plan) **horizontally**; landscape fans the columns out — rotation switches with
  no layout break.
- **Open claimability (ADR-0047).** The per-project **Assignment Pool is now
  openly claimable**: any project-member agent can see and claim an unassigned
  pool task (claim = atomic `open→running` + stamp assignee via conditional CAS).
  Backlog tasks (no plan) stay unclaimable; structured plan nodes remain
  single-assignee. Authorization is enforced in the **service layer** and is
  opaque to non-members (403 / 404, no existence disclosure). Each agent has a
  **hold cap of N=3** claimed pool tasks (configurable via `Deps.PoolClaimLimit`);
  the 4th claim is rejected with `pool_claim_limit_reached` and frees up as
  in-hand tasks complete.
- **Resizable Thread & Participants side panels (desktop).** The Thread panel
  (col④) and the Participants sidebar share one `ResizablePanel` — a left-edge
  resize grip (`cursor:col-resize`, keyboard-accessible), width clamped and
  persisted in `localStorage`, main content compresses without overflow.
- **Channel sidebar Chat / Threads / Files tabs (desktop).** The Channel sidebar
  is organized as three segment-header tabs — Chat (message stream) / Threads
  (thread list) / Files (file list) — showing one at a time.
- **Archived plans in the global Plan list (desktop).** The Plan list header gains
  an **Active / Archived** segmented filter; archived rows are greyed with an
  "Archived" badge and open to a **read-only** detail (DAG / nodes / history
  viewable; no edit, no start). Unarchive is intentionally out of scope this cycle.
- **col① sidebar rail consolidation (desktop).** The rail now carries the
  connection-status icon (WiFi + breathing dot + tooltip), the search entry, and a
  bottom user panel with a Light / Dark theme toggle and Sign out.
- **`discard_task` agent tool.** Agents can discard their own task (200 →
  `discarded`); a terminal task re-discard returns `422 invalid_transition` (no
  write), and a non-domain task returns `403 not_agents_task` (opaque).

### Changed

- **Plan `P<number>` sequence ids + message linkify.** Plans surface a
  `P<number>` sequence id (list + detail), and messages linkify `plan-<id>` /
  `P123` into clickable plan links — bidirectional (human- and agent-authored) and
  symmetric with the existing `task-<id>` / `T<number>` linkify; refs inside code
  spans / existing links are not converted.
- **Lists show `org_ref` (`T<n>`) instead of `#short-hash`.** Work-item, task,
  issue, board-card, and detail surfaces show the org sequence ref (`T<n>` /
  `I<n>`) everywhere; the `#<id-tail>` short hash is eliminated across the audited
  call sites (only work items with no `org_ref` fall back).
- **Clickable agent names → activity sidebar.** Clicking an agent name in the Task
  / Issue detail panels opens the `SenderDetailSidebar` activity sidebar.
- **Task detail links to its related Plan.** The Task detail panel shows the
  related plan (`P<number>` + name) and clicks through to the plan detail; backlog
  tasks show no empty link.

### Fixed

- **Inbound attachments now reach agents (T103).** A T74 half-fix left wake
  delivery stripping attachments and advancing the cursor, so an agent never saw
  inbound files. The wake payload now carries `file_uri` (+ filename / mime /
  size) inline, the agent can `download_file` it (200 when a participant; 403 /
  404 fail-closed for non-participants), and the unread cursor advances without
  dropping or re-waking.
- **SSE connection no longer flips connecting ↔ reconnecting (T104).** The SSE
  response (`bus.go`) now sets `Cache-Control: … no-transform` (plus
  `X-Accel-Buffering: no`) so a buffering proxy (e.g. Cloudflare) can't chunk the
  stream into watchdog-tripping silence; the connection stays stably open.

## [v2.10.0] — 2026-06-15

Three-column desktop UI/UX refactor + attachments / message-ref / author fixes (38 commits).

### Added

- **Three-column desktop shell.** The Web Console is rebuilt as a four-region
  layout — an **icon rail** of top modules (Workspace / Conversations / Members /
  System) → a per-module **second-level list** (col②) → the **content** pane
  (col③) → an **on-demand context** panel (col④) — on the existing IA (the
  Overview page is removed). Routing is module-nested.
- **Secondary-nav registry (extension contract).** col② is driven by a
  per-module `SECONDARY_NAV_REGISTRY`: a module adds its own `*SecondaryNav` plus
  one registration line, with no edits to the shared app layout (avoids
  concurrent-edit conflicts).
- **Every module rebuilt on the shell.** Conversations (col② channel / DM nav,
  col④ participants + shared files), Workspace Projects + project detail (col②
  project sub-nav, list cards show task / issue / plan / repo counts), Workspace
  Tasks / Issues (col④ read-only metadata panel), **global cross-project Plan
  list + detail** with Chat / DAG / Task-list tabs (Workspace › Plan), the
  **Project Work Board**, Members (col② Humans / Agents, Agent col④ context
  panel), System.
- **Task & issue attachments.** Task and issue detail panes list / upload /
  download attachments, **project-member-gated** — fail-closed (403 / 404) for
  non-members.
- **Inbound message attachments to agents.** Attachments on inbound messages are
  surfaced to agents through `get_my_unread` and the wake brief.

### Changed

- **Message ref linkify.** Messages now linkify both `task-<id>` *and*
  `T<number>` org-refs into clickable task links (sent and received); terminal /
  done task refs resolve correctly (T62).

### Fixed

- **System author.** System / scheduler message author renders as **System**
  instead of "(deleted)" (T75).
- **Members → Agents header overlap.** Fixed the `AVAILABILITY` / `ROLE` table
  header collision (T80).

## [v2.9.1] — 2026-06-14

Message Threads + a task-model / board / archive cleanup wave.

### Added

- **Message Threads.** Derive a thread from any message and reply in a popout
  thread sidebar (Slack-style single level — replies are flat, a reply's parent
  is always the root). The Participants sidebar lists every thread in the
  conversation with reply count and a "new activity" marker for unseen replies.
  **@mentioning a project agent inside a thread wakes it and its reply lands in
  the thread** (not at top level). Works across all conversation types
  (channel / DM / issue / task / plan). Stored as `parent_message_id` with a
  derived root; server enforces single-level depth and rejects a parent in
  another conversation.
- **Built-in assignment pool + claimable (ADR-0047).** Every project has one
  always-running **built-in assignment pool** — a flat list of claimable tasks an
  agent can pull at will (assign ≠ claim). The Work Board is now three segments:
  **Backlog** (unscheduled, not claimable) · **Assignment Pool** (built-in,
  claimable) · **structured Plans** (DAG columns). `claimable` is a derived
  predicate (open ∧ assigned ∧ in a started plan node that's dispatched). A
  backfill migration moves assigned non-terminal backlog tasks into the pool.
- **`list_tasks` MCP tool.** List a project's tasks, filterable by status and
  assignee.
- **Channel archive.** Channels can be archived (irreversible, read-only — writes
  rejected with 409); archived channels leave the default list and show in a
  separate "Archived" group (parity with project archive).
- **Task-recovery tooling.** `unblock_task` / `rerun_failed_node` MCP tools plus
  automatic re-dispatch of stale-released plan nodes (retry-capped) — recovers
  the restart→deadlock failure mode.

### Changed

- **Simpler task state machine (ADR-0046).** Seven states down to five
  (`open` / `running` / `completed` / `discarded` / `reopened`). `blocked` is no
  longer a state — it's a recoverable annotation on a running task (auto-clears on
  resume / unblock / complete / discard); `verified` is removed (`completed` is
  terminal). A round-trip migration preserves prior data.
- **Board visibility.** Backlog / task / issue lists exclude terminal tasks by
  default (re-viewable via `?status=`); tasks & issues of archived projects are
  hidden from the org-level lists; large plans show a searchable task list with
  inline reassignment.
- **Plan detail UX.** Task org-numbers shown; a compact DAG layout with
  in-graph dependency editing (click + keyboard); Chat / DAG / Task-list split
  into three tabs (Chat default).
- **Design tokens.** Remaining raw Tailwind palette classes migrated to semantic
  CSS-var tokens; the `no-raw-colors` lint gate is clean, both-mode AA preserved.

### Fixed

- Restored the SPA ESLint (jsx-a11y) gate.
- Thread follow-ups: thread-list live-refreshes on new messages; thread-list
  preview meets light-mode AA; thread-mentioned agent replies land in-thread.

## [v2.9] — 2026-06-12

Plan Orchestration + explicit org routing (58 PRs).

### Added

- **Plan Orchestration.** Compose work as a DAG of tasks (a *Plan*): create a
  plan, add backlog tasks as nodes, and wire dependencies while it's in draft
  (cycle / self-edge rejected). `start` a plan and the center auto-advances it —
  each node's status is derived (blocked / ready / dispatched / running / done /
  failed), and as upstream tasks complete the system dispatches each ready node
  to its assigned agent (work-delivery, not a chat @mention), running the plan to
  done with no manual stepping. A failed node blocks its subtree and wakes the
  plan's creator. The Work Board (Backlog column + one column per Plan) and Plan
  detail (DAG view with synthetic Start/End nodes, draggable DAG↔chat splitter,
  draft-only edge editing, all-direction task drag) are the operating surface.
- **Programmatic plans for agents (MCP).** A PM-style agent can build and run a
  plan through its own MCP tools (`create_plan`, `add_task_to_plan`,
  `add_plan_dependency`, `start_plan`, …) — the 11 plan tools are registered in
  the agent-facing tool catalog, so an agent can assemble and start a plan and
  let the orchestrator execute it.
- **Plan & project lifecycle.** Plans have draft / running / done plus an
  irreversible **archive** (cascades to its tasks, read-only); a plan with a
  running task can't be archived. Projects can be archived (read-only — every
  child mutation across the web and MCP surfaces is rejected with 409; reads
  still work), and archived projects are excluded from the default list and
  shown in a separate "Archived" group.

### Changed

- **Explicit org routing.** Every org-scoped API moved to `/api/orgs/{slug}/...`
  (path-explicit), eliminating the implicit "current org" that was inferred from
  session / `?org_slug=` query — the same path can no longer return different
  data depending on hidden state. Full no-shim migration; cross-org access is
  denied with 404 (existence-non-disclosure). The frontend addresses pages under
  `/organizations/{slug}/...`.
- **Plan conversations join @mention-wake.** @mentioning a project agent in a
  plan conversation now wakes it — including a project-member agent that isn't yet
  a participant (issue / task conversations already supported @mention-wake since
  v2.7.1; plan conversations and the non-participant project-member breadth are
  the v2.9 addition). Only human-authored messages trigger conversational wake
  (system / agent messages never do).

### Security

- **Authenticated token revoke.** `POST /api/admintoken/revoke` now requires an
  authenticated caller who is a member of the token's organization (was
  unauthenticated): 401 unauthenticated / 403 non-member / 204 on success.

## [v2.8.1] — 2026-06-10

Web Console UX overhaul + agent-dispatch reliability (65 PRs).

### Added

- **Channels & Agents list enrichment.** Channels rows show created-time +
  participant avatars + a recent-message preview; Agents rows show provider
  (CLI / model) + last-activity time and content. Backed by N+1-free list-enrich
  APIs (#255).
- **Detail sidebar from any @mention or avatar.** Clicking an @mentioned agent /
  human in a message, or the peer avatar in a DM header, opens the agent / human
  detail sidebar (kind-routed).
- **Issue batch-update API (#251)** mirroring task batch-update (title / description
  / status / tags), powering the consolidated Issue edit panel.

### Changed

- **Task & Issue detail redesigned, editing consolidated.** Task and Issue detail
  sidebars are read-only; all edits go through a single Edit panel (no per-field
  inline editing). Issue detail aligned to the Task detail style.
- **Chat / DM UX refresh.** Redesigned message rows, DM layout cleanup, code-block
  width, composer, plus a Channels / Agents / Settings visual pass. Contrast (AA)
  verified in both light and dark themes.

### Fixed

- **Single-active race in agent dispatch (#277 / #278).** The activate path was a
  non-atomic check-then-act over a partial (non-unique) index, so concurrent
  assigns to one agent could yield multiple active work items. Fixed with a pull
  model + a UNIQUE partial index (`agent_work_items(agent_id)` WHERE status IN
  active / waiting_input) and queue-not-drop semantics: one agent, one active work
  item.

## [v2.8] — 2026-06-06

Conversation-centric work items + chat references.

### Added

- **`#` / `@` reference picker (#266 / #275).** In-composer autocomplete for
  `#channel` and `@user` / `@agent`, with linkify + click-through in rendered
  messages.
- **Worker detail page.** A dedicated WorkerDetail view for an enrolled worker.
- **Org-scoped Issues / Tasks views.** Work items surfaced at the org level.

### Changed

- **Agents reply in DMs & channels (#185).** An agent that is a DM peer or channel
  participant now receives messages and can reply (beyond work-item dispatch).

## [v2.7.1] — 2026-06-04

### Added

- **Multiple center deployments on one machine — `install center --instance <name>` (#211).**
  A named instance gets its own install prefix (`~/.agent-center.<name>`, the
  `default` instance keeps the legacy `~/.agent-center`) and its own launchd /
  systemd label (`com.agent-center.center.<name>` / `agent-center-<name>.service`;
  `default` keeps the legacy label), so two centers no longer trample each other's
  prefix, ports, or service registration. Ports are explicit (no auto-assign):
  `--web-port` / `--server-port` / `--admin-port` (the legacy `--port` /
  `--tcp-listen` remain as aliases; `--server-port` newly exposes the previously
  hardcoded `:7050`). The config records `server.instance`. New `list-local-centers`
  lists every deployment (instance / prefix / ports / service-vs-foreground /
  online). `uninstall center --instance <name>` removes one instance only (defaults
  to `default` for back-compat). Bare `install center` / `uninstall center` =
  implicit `default` (existing operators unaffected). Fresh-install only — no
  migration. Also fixes a latent gap where `server.bootstrap_public_url` (#200) was
  missing from the config known-keys allowlist.
- **Worker config as the single source of truth (#249 / #251).** `install worker`
  writes all enroll fields (worker_id / name / bootstrap / token /
  server_fingerprint) into `<prefix>/etc/config.yaml` (0600), so `worker run
  --config=…` is the whole launch command and the token no longer appears in `ps`,
  the launchd plist, or the printed command. Legacy flags still work as overrides;
  upgrade migrates older configs automatically.
- **Org-internal sequence numbers `T<n>` / `I<n>` (#245).** A per-(org, type) counter
  (migration 0049) surfaced as `org_ref` in tables, detail views, and breadcrumbs.
- **Humans list + user detail page + signup email (#193 / #213).** The Humans page
  adds email / created-at / last-session columns, a new `/users/{id}` detail page,
  and signup accepts an (optional, unverified) email.
- **MCP `find_org_agent` / `find_org_channel` (#241 / #246)** return ready-to-use
  refs; channel-post errors made precise (404 / 403).

### Changed

- **Icon-ified controls + chrome polish.** AgentDetail Stop / Restart / Reset and
  chat composer controls became icons (#240 / #250); channel URLs use a hash id
  `/channels/:id` (#247); the sidebar collapse control simplified to a chevron
  (#253); README / docs adapted to the v2.7.1 retag (#252).

## [v2.7.0] — 2026-06-04

### Notes / Compatibility

- **Agent-supervisor RPC protocol — backward-compatibility contract
  (deferred-with-trigger).** The v2.7 agent-execution cutover drops the
  cross-version range gate on the persistent agent-supervisor's RPC: a returning
  worker daemon always re-attaches to a live supervisor regardless of its
  advertised protocol version. The protocol is therefore assumed
  **backward-compatible**, which is a CONVENTION, not a runtime guarantee:
  protocol evolution MUST be **additive only** (add optional fields; never
  remove/repurpose a field or change a message's semantics). **Trigger:** if a
  future change is genuinely breaking, it MUST at that time re-introduce a
  mixed-version guard (force-relaunch incompatible old supervisors) or
  force-relaunch all existing agents on deploy, plus its real-claude e2e —
  otherwise an old supervisor mis-parses the new wire and re-attach silently
  breaks. Canonical record: the `ProtocolVersion` note in
  `internal/agentsupervisor/protocol.go` (+ the re-entry comment in
  `supervisormanager.ProbeAgent`); also registered on the acceptance side (§A).
- **Agent terminal-fail → queued WorkItem reassignment (deferred-with-trigger).**
  The v2.7 GATE-7 Mode-B self-heal circuit-breaker (an agent that crash-loops past
  its relaunch cap → terminal `LifecycleFailed`) cascades only its IN-FLIGHT
  WorkItems (active + waiting_input) → failed, atomically (so the user's task never
  silently looks "still running"). A **queued** WorkItem (not yet started, not
  session-bound) is deliberately LEFT `queued`: its work is unstarted/recoverable, so
  failing it would wrongly kill work that could still run, and the owning agent is
  itself visibly `failed` (queryable) — so the residual is non-silent and loses no
  done work. Long-term, a queued WorkItem on a terminally-failed agent should be
  **reassigned to a healthy agent** (work is not session-bound; the reassignment
  mechanism `AgentWorkItem.Supersede` + recreate already exists), but wiring
  agent-death → workforce/dispatch reassignment is a cross-BC change beyond
  Mode-B/B3. **Trigger:** queued WorkItems stuck on dead agents become a practical /
  fleet-scale pain point → wire the agent-death→reassign path + its e2e then.
  Canonical record: the cascade comment in
  `internal/agent/service/appservices.go` (`MarkAgentFailed`); also registered on the
  acceptance side (§A).
- **L2 no-silent-failure turn↔WorkItem correlation (deferred-with-trigger).** When a
  claude turn ends with `is_error=true`, the AgentController fails the agent's
  in-flight WorkItem so a failed turn never sits silently `active`. The correlation
  uses the **last** WorkItem injected into the session (`managedAgent.currentWorkItemID`),
  NOT a precise per-turn id — claude's `result` line carries no WorkItem id, and the
  result event is delivered asynchronously by the session pump (~50ms). If a second
  `work()` injects before the first turn's result is pumped, the result is
  mis-attributed, and the race is two-sided: charging result(A) to B both wrongly
  fails B AND leaves A silently active (A's failure never surfaces). v2.7 injects
  sequentially with low/=1 `max_concurrent`, so the window is effectively unreachable.
  **Trigger:** `max_concurrent > 1` OR an observed mis-attribution → add precise
  correlation (a turn-seq/token claude echoes back). Canonical record: the
  `currentWorkItemID` comment in `internal/workerdaemon/agent_controller.go`; also
  registered on the acceptance side (§A).
- **GATE-7 Mode-B orphan fork-generation sessions (deferred-with-trigger).** The
  Mode-B crash-relaunch fix forks the killed session into a new generation id each
  relaunch (see Fixed below). The prior generations' claude session jsonl files
  (`~/.claude/projects/<path>/<old-id>.jsonl`) are left behind — harmless but
  accumulating across repeated crashes. **Trigger:** if fork-generation orphans
  become a disk/clutter concern at scale → add a housekeeping sweep that prunes
  superseded-generation session files for an agent on reset / terminal-fail.
  Canonical record: the `Generation` comment in
  `internal/supervisormanager/epoch.go`; also registered on the acceptance side (§A).
- **Detached-supervisor claude stderr is not captured (deferred-with-trigger,
  observability).** The agent-supervisor runs `claude` with `Stderr = os.Stderr` and
  is itself spawned detached (`exec.Command` + `Release` + setsid), so claude's
  stderr follows the daemon's stderr fd and is NOT persisted per-agent. This made a
  real-path "Session ID already in use" error invisible in the worker log during the
  GATE-7 Mode-B diagnosis (the mechanism was confirmed via isolated repro instead).
  **Trigger:** when agent-claude failure diagnosis needs the child's own stderr →
  redirect the supervisor's claude stderr to a per-agent home file. Non-blocking;
  registered on the acceptance side (§A).
- **Mode-B WorkItem-id rebind placement (deferred-with-trigger, defensive).** The
  L2×Mode-B fix rebinds the in-flight WorkItem id onto the relaunched managedAgent's
  `currentWorkItemID` AFTER `startSession` returns (in `bootReapRelaunch`), rather
  than inside `startSession` at managedAgent creation. There is a theoretical window
  between those two steps where a resume-phase result could read an empty id, but it
  is UNREACHABLE today: under stream-json `--input-format`, claude does not turn on
  `--resume`/`--fork-session` until it receives stdin input (the resume-nudge,
  injected AFTER the bind), so no result can land before the bind. **Trigger:** a
  future bind-point / nudge-timing refactor, OR observing any resume-phase (pre-nudge)
  result → move the bind INTO `startSession` (set `currentWorkItemID` at managedAgent
  creation = structurally window-free). Canonical record: the bind comment in
  `internal/workerdaemon/boot_reconcile.go` (`bootReapRelaunch`); also registered on
  the acceptance side (§A).

### Fixed

- **Agent-claude could not reach the Anthropic API behind an HTTP proxy (GATE-1
  ship-blocker).** The supervisor's default-deny env allowlist (`BuildClaudeEnv`)
  stripped `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`/`ALL_PROXY` (+ lowercase), so in a
  proxied deployment claude's API request was rejected with a 403 (misleadingly
  surfaced as `authentication_failed`) — agent work was consumed but no turn was
  produced. The proxy routing vars are now allowlisted (routing config, not worker
  secrets; the `AGENT_CENTER_*` secret drop and `CLAUDE_CODE_*` deny are unchanged).
- **A failed claude turn no longer sits silently active (L2 no-silent-failure).** A
  `result` event with `is_error=true` now fails the in-flight WorkItem
  (`active→failed` via the normal feedback edge — distinct from the GATE-7 Mode-B
  agent-death cascade, which handles a crashed/result-less claude), so a failed turn
  surfaces as a failed task instead of a task stuck "running".
- **GATE-7 Mode-B crash recovery failed to relaunch (session-id lock; A-seg
  ship-blocker).** A hard-killed claude (kill -9 / OOM / killpg) does NOT release its
  session-id lock, so the self-heal / boot-reconcile relaunch — which re-used the
  same durable-epoch session-id — was refused by claude (`Session ID … already in
  use`), exited before producing any output, took the supervisor down with it, and
  crash-looped to the terminal circuit-breaker (work lost). It manifested as a timing
  race (idle agents whose lock released in time recovered; work-in-flight agents,
  exactly what Mode-B protects, consistently failed). The relaunch now FORKS the
  killed session into a fresh, never-locked id: a per-agent **generation** counter is
  bumped+persisted (atomically, under the home lock, before spawn) per relaunch
  attempt, and the supervisor spawns claude with `--session-id
  SessionUUIDGen(agent,epoch,gen) --resume <prior-id> --fork-session` — preserving
  the conversation while sidestepping the lock, deterministically (independent of
  lock-hold timing). Generation 0 derives byte-identically to the pre-fix
  `SessionUUID(agent,epoch)`, so initial/normal/clean-restart sessions are unchanged;
  only the crash-recovery paths fork. A reset zeroes the generation (clean slate).
- **A failed re-drive turn after a Mode-B relaunch no longer silently leaves its
  WorkItem active (L2×Mode-B no-silent-failure).** On a crash the `managedAgent`
  (which holds `currentWorkItemID`) is deleted, and the relaunch's resume-nudge does
  not flow through `work()`, so the in-flight WorkItem id was lost across the
  relaunch — if the re-driven turn then ended with `is_error` while the agent stayed
  alive, L2 had no WorkItem to fail (it surfaced only a warning) and B3 (the
  agent-death cascade) did not apply, leaving the original WorkItem silently
  `active`. The in-flight WorkItem id is now carried across the crash: captured
  before the managedAgent is deleted, stored on the crash-surviving `selfHealEntry`
  (self-heal path) or taken from resume-state's active WorkItem (boot-reconcile
  path), and rebound onto the relaunched managedAgent's `currentWorkItemID` — the
  same field L2's `surfaceTurnFailure` reads — so a failed re-drive fails the
  WorkItem (`active→failed`) instead of going silent.
- **Agent `Profile.Model` was ignored by the v2.7 control-loop spawn (all agents
  used claude's default model).** The supervisor's `--model` plumbing existed
  downstream, but no control-loop path fed the agent's configured model into it: the
  reconcile command, resume-state, and the `agent.lifecycle_changed` event all
  lacked it, and the daemon (AdminClient-only, no DB) cannot self-source it. The
  model is now carried end-to-end: the Agent BC emits it in the lifecycle event
  (ADDITIVE field), the Environment projector passes it through into the reconcile
  command (pure event-driven, no Agent-repo read), and resume-state carries it; the
  daemon threads it on all three spawn paths — initial reconcile, **the mid-run
  self-heal relaunch (carried across the crash on `selfHealEntry.model`, since
  self-heal gets no fresh reconcile — otherwise a crash would silently revert the
  agent to claude's default model)**, and boot-reconcile (`centerRecord.Model` from
  resume-state) — into the supervisor's `--model`. **Semantics:** the model is
  snapshotted at the (re)start that emits the lifecycle event, i.e. a model change
  takes effect on the next (re)start/relaunch, not live mid-session. Backward-compat:
  an empty `Profile.Model` passes no `--model` (claude default), so existing
  unconfigured agents are unchanged. (`Profile.EnvVars` remains deferred to slice ②
  — its layer-2 agent-env injection is a worker-secret-leak boundary needing
  dedicated security acceptance, a separate track from this low-risk model name.)
- **Task/Issue conversations were created without an organization_id → humans could
  not reply to them (and could not wake a waiting_input agent).** The participant
  projector created the bound task/issue Conversation without stamping its org, so
  every org-scoped conversation endpoint — including `POST
  /api/conversations/{id}/messages` (the web UI's human-reply route) — hit
  `requireConversationInOrg`'s `conv.org == actor.org` check with an empty conv org
  and returned 404 to **everyone**, including a legitimate same-org participant. This
  broke the core interaction: a human could not reply to an agent waiting on input,
  so the agent never woke (GATE-4 ship-blocker). The project's org now rides the
  `agent.*.created` event payload (sourced from the project at emit) and the projector
  stamps it onto the Conversation at creation; the create branch refuses an empty org
  (defensive, no silent broken conversation). v2.7 is a fresh install (drop+recreate)
  so no backfill of pre-existing org-less conversations is needed.
- **macOS 26 (Darwin 25) `install`/`upgrade` failed to start the service (ship-blocker
  #150).** `serviceActivateCmds` activated the launchd unit with the deprecated
  `launchctl load`, which is removed on macOS 26 → the service never started → the
  admin unix socket was never created → the post-install health probe failed with
  `dial unix admin.sock: no such file or directory` and the upgrade rolled back.
  Activation now uses `launchctl bootout`/`bootstrap` (the modern API the v2.5.17
  uninstall fix already adopted); systemd is unchanged. **This was the real
  install-time startup blocker** behind the "center won't start" reports.
- **Install default `server.listen_addr` changed `:7000` → `:7050` (#161).** Note:
  this field is **vestigial** — it is parsed and validated-required but never bound (a
  leftover from the pre-carve-out gRPC dispatch listener). The center actually listens
  on the Web Console port (`127.0.0.1:7100`) and the admin endpoint (TCP `:7300` + a
  unix socket). Changing the default is a cleanup to avoid confusion with macOS
  AirPlay Receiver's `:7000`; it is **not** a startup-failure fix — the startup
  blocker was #150. (The vestigial field itself is removed in v2.8, #174.)
- **Agents could not run any task — every agent's claude turn failed with `403
  Request not allowed` (#182, FINDING-G).** The agent's claude is launched with
  `--setting-sources ""` (intended to isolate the operator's `~/.claude`
  hooks/settings), but that value loads NO settings sources — and claude's keychain
  `/login` credential is loaded via the **user** source. So the flag suppressed
  authentication and every turn 403'd (orchestration, MCP, and the streaming
  pipeline all worked — only claude's own auth failed). The flag is now
  `--setting-sources user,project`: **user** restores `/login` auth, **project** lets
  the agent carry its own config in `<workspace>/.claude` (created empty per agent).
  **Tradeoff:** loading the user source also loads the operator's user-level
  `~/.claude` settings (hooks/env/plugins) in the agent — user-level isolation is not
  solved here (auth and operator settings share the user source); full isolation is
  deferred to v2.8 via a `setup-token` (`CLAUDE_CODE_OAUTH_TOKEN`) path. `--strict-mcp-config`
  still pins MCP servers to our generated config.
- **Per-agent home path no longer double-nests `workers/<worker-id>` (#179).** The
  runtime home resolved to `<base>/workers/<wid>/agents/<id>` even though `<base>`
  (the worker's `…/var/agent-homes`) was already worker-scoped — a redundant segment
  that also helped push the supervisor socket path past macOS's limit (see #178). It
  is now `<base>/agents/<id>` (agentPaths + the boot-reconcile scan changed in
  lockstep so re-attach still finds surviving supervisors).
- **Per-agent home layout flattened to `workers/<wid>/var/agents/<ULID>/` (#209).**
  The base previously resolved to `…/var/agent-homes`, so the per-agent home was
  `…/var/agent-homes/agents/<id>` — `agent-homes/` was a meaningless wrapper (its
  only child was `agents/`). The base is now the worker state dir (`…/var`)
  directly, dropping the wrapper (same cleanup spirit as #179; also shortens the
  supervisor socket-adjacent paths). Fresh-install only — no migration (v2.6→v2.7
  already requires reinstall); existing pre-tag installs uninstall + `rm -rf` +
  reinstall.
- **Fresh install web console first-screen now routes correctly (#145, ship-blocker).**
  The auth middleware guarded the entire mux, so the SPA's `/`, `/signin`, `/signup`,
  and `/assets/*` all returned `401 JSON` on first load — the SPA catch-all never
  reached. Middleware now guards only `/api/*`; SPA paths are served directly. A new
  public endpoint `GET /api/auth/bootstrap` returns `{"initialized":bool}` so the
  frontend deterministically routes an unauthenticated user to `/signup` (fresh
  system) vs `/signin` (already initialized), instead of probing `/api/orgs 401`.
- **Worker now appears `online` within ~1 RTT instead of up to 30s after start
  (#154).** `RunDaemon` invoked a single `Heartbeat()` immediately before entering the
  ticker loop, so `MarkOnline` + the `workforce.worker.online` event fire on the first
  RTT after enroll — both fresh starts and restarts — rather than waiting for the
  default 30s ticker.
- **Web console identity references now show real users instead of a configured
  default (#155).** The webconsole HandlerDeps `Actor` was a static
  `observability.Actor("user:"+identity.default_user)` built at startup and reused for
  every request, so every channel created, every message sent, and every read-state
  update was stamped with the configured default user — and the SPA's `currentUserId`
  was likewise hard-coded to `user:hayang` in the store. As a result the channel
  participant ownership check (`isOwner`) never matched, the Invite/Remove controls
  silently disappeared, and message history showed the wrong author. `hd(r)` now
  derives `Actor` per-request from the JWT-authenticated identity via
  `filesCallerRef(CurrentIdentity(r))`, and `AppLayout` seeds `currentUserId` from
  `/api/auth/me` (the post-`#146` per-request session identity that already powers the
  attachment download gate). The web `sendMessage` handler also ignores any
  client-supplied `sender_identity_id` and stamps the message from the authenticated
  session.
- **Conversation message senders and participants render display names instead of
  raw `user:<id>` / `agent:<id>` refs (#160).** `/api/members` now enriches each
  member with `display_name` from the Identity repo (a service-level join that the
  list endpoint was already prepared to add). A `useDisplayNameResolver` hook in the
  SPA builds an `identityRef→display_name` map normalised across the bare-id
  (`user-xxx` from `/api/members`) and prefixed (`user:user-xxx` from message events)
  formats, and `MessageList` + `ParticipantsPanel` resolve through it before falling
  back to the raw ref. The `members.list` response is the only backend touch — `Member`
  and `Identity` aggregates are unchanged.
- **Invitations with a malformed identity ref no longer surface as `500` (#158).**
  `POST /api/conversations/{id}/participants` now performs the same identity ref
  format check that channel creation uses, so a body like `not-a-valid-ref` (missing
  the `user:` / `agent:` prefix) returns `400 invalid_identity_id` with a clear
  message instead of bubbling an ADR-0033 validation error string out as `500
  internal`. A legitimate `user:ghost` (well-formed but not yet provisioned) still
  returns `201`, matching pre-existing acceptance behaviour.
- **Attachment download authorization (#142 + #146).** The webconsole now exposes
  conversation message attachments via a real download path (`GET
  /api/conversations/{id}/messages/{mid}/files/{file_id}`) with attach-time auth on
  the send side: the message handler verifies `fileReachableForHuman(caller, uri)`
  for every URI before constructing the `Message` and `FileReference` rows
  atomically in one transaction — so an unreachable URI never produces a half-written
  conversation. Cross-org and non-existent URIs both return a byte-identical `403`
  ("attachment is not reachable"), eliminating the existence oracle on attach.
  `completeUpload` requires the caller to be the upload-session initiator before
  binding a `uploader`-scope reference (`403 + zero FileReference` for
  non-initiators), and `scope=uploader` is no longer accepted from the client
  (server-derives `scope_id` from the caller). The download gate consults
  `HasActiveParticipant` only, so a participant removed from the conversation loses
  download access on the same edge as their participation.
- **Worker control commands now arrive in near-real-time (#108, D5 SSE).** The
  worker daemon opens a long-lived `GET /admin/environment/worker/commands/stream`
  SSE connection that delivers each `WorkerControlEvent` with its monotonic offset
  the moment it's appended to the control log, and acks back via the same offset
  cursor as poll. Poll remains the fallback (clean disconnect, heartbeat-timeout,
  ring eviction → poll backfills); stream and poll feed off the same control log and
  the same `MarkAcked` cursor, so duplicates and gaps are structurally impossible.
  Worker-side stream client + daemon switchover, plus the bus de-dup ring, are new;
  the legacy poll path is unchanged.
- **Worker capabilities are discovered automatically and rendered in the Environment
  page (#147 + #176 + #177).** Each worker now probes the locally-installed agent
  CLIs (`claude-code`, `codex`, `opencode`) every time it comes online — version,
  detected/enabled flags, MCP/skills/session support — and reports the result to the
  center via a new `POST /admin/workforce/worker/capabilities` endpoint that enforces
  token-ownership (a worker can only write its own capabilities, cross-worker writes
  return `403`). A new `PATCH
  /admin/workforce/worker/{id}/capabilities/{name}/enabled` lets an operator toggle a
  CLI off without removing the detection. The `/api/fleet` projection now carries the
  detected capabilities per worker; the Environment worker card renders `claude-code`
  + `codex` + `opencode` with `detected`/`enabled` status and a clear
  `Executable: claude-code only (codex/opencode discovery only — v2.8)` footnote.
  `--capabilities` is removed from `agent-center install`/`worker run` (auto-probe
  replaces the manual flag). The CLI dispatch layer remains pinned to `claude-code`
  in v2.7 (see Added → "Agent CLI is restricted to claude-code in v2.7" below); the
  v2.8 design that lets `codex` / `opencode` actually execute is tracked as #180.
- **`worker daemon` daemon now finds user-installed agent CLIs (#175).** The
  launchd / systemd unit generated by `install worker` inherits a minimal PATH that
  does not include `~/.local/bin`, `~/.cargo/bin`, `~/.opencode/bin`,
  `/opt/homebrew/bin`, etc., so `ProbeAllAdapters` only ever saw whichever CLI
  happened to live on the system PATH. The installer now captures the install-time
  shell PATH and unions it with a well-known list of user-tool bin dirs, deduped and
  order-preserved, into the unit's `EnvironmentVariables.PATH` (launchd) /
  `Environment=PATH=` (systemd). Center installs are unaffected (no spawned CLI).
- **`install worker` now requires `--worker-id` (#171).** The previous fallback to
  `hostname` silently collided when the same machine ran two worker installs without
  an explicit id (each subsequent `install worker` overwrote the previous worker's
  prefix subtree and launchd unit). Missing `--worker-id` now returns
  `install_worker_missing_id` with a message pointing at the Web Console "Add
  Worker" flow, which has always generated a unique id. Web-Console-driven installs
  are unaffected. Multi-worker-per-machine is supported by the existing
  per-worker-id prefix isolation; an `agent-center list-local-workers` discovery
  command is deferred to v2.8 (#170).
- **macOS install activates the launchd service with the modern API (#150).** See
  the entry above; this was the real install-time startup blocker on macOS 26.
- **Agent supervisor socket no longer overflows `sun_path` on macOS (#178,
  ship-blocker).** The agent supervisor's unix-socket path was assembled under the
  agent home (`<base>/agents/<id>/supervisor.sock`), which on a typical user-mode
  install ran ~143 bytes — past macOS's 104-byte `sockaddr_un.sun_path` limit, so
  `bind()` failed silently, the supervisor never came up, the daemon retried
  forever, and orphan `claude` + `supervisor` processes accumulated on every Start.
  The socket now lives at `<TMPDIR>/acsv-<sha256(agent_id)[:12]>.sock` (~71 bytes,
  agent-home-independent, deterministic across daemon restarts). The path is also
  written into `supervisor.instance` so a returning daemon reads it back rather than
  re-deriving — survive-reattach across `--use-control-loop` daemon restarts is
  unaffected. Linux is unaffected (`sun_path` is 108).
- **Agent CLI field is now validated at creation time (#181).** Previously, any
  string was accepted for `cli` and silently mapped to `claude-code` at runtime
  (only the claude-code execution path is wired in v2.7 — `codex` / `opencode`
  adapters are discovery-only stubs that return `ErrNotImplemented`). The agent BC
  now requires `cli ∈ {"claude-code"}`: missing / `codex` / `opencode` / any other
  value returns `400 invalid_cli` from `POST /api/agents` and
  `POST /api/members/agent`. The webconsole Agent-Create modal shows `claude-code`
  as the only selectable option, and the Environment worker card's detected-CLI
  list carries an explicit `Executable: claude-code only (codex/opencode discovery
  only — v2.8)` note so users do not expect a discovered CLI to actually run. The
  full per-CLI runtime-dispatch plumbing is tracked as #180 for v2.8.
- **`AgentDetail` page no longer crashes for agents with no skills / no env
  (#183).** `agentMap` returned the Go nil slice / map for `Skills` and `EnvVars`,
  which serialised to JSON `null` instead of `[]` / `{}`, and the SPA's
  `agent.skills.length` read on detail render dereferenced the null. The webconsole
  now coalesces `Skills` → `[]` and `EnvVars` → `{}` at the projection edge (one fix
  for every agent response), and `AgentDetail` reads through a defensive guard for
  belt-and-braces. Other detail pages (Project / Issue / Task / Channel / DM /
  Members / Environment) were not affected (sweep across 13 pages: zero console
  errors).
- **SSE connection now shows `live` on first frame, not after the 30s heartbeat
  (#172).** `bus.go` set the `text/event-stream` headers but only called
  `flusher.Flush()` inside the `last_event_id` replay branch, so a fresh connection
  received no bytes until the first 30s heartbeat — the browser `EventSource`'s
  `onopen` fired only then, and the top-right status flipped from `connecting` to
  `live` after a 30-second pause on every page load. Headers + a `200 OK` now flush
  immediately on accept.

### Added

- **End-to-end UI integration (#105 / E1 umbrella).** Every v2.7 domain now has a
  full Web Console surface: the Project page (members / issues / tasks / repo
  refs), the Agent page (lifecycle controls / work queue / activity stream — `#136`),
  the Environment page (workers / agents / file transfer sessions — including the
  Fleet operational data merge, see Changed), the unified Conversation surface
  (task / issue owner banner + work-item segmentation + attachment thumbnails per
  metadata), and the Agents-as-organization-Members flow (one-step creation +
  click-through to the lifecycle page).
- **Members → Add Agent creates an execution Agent and an organization Member in a
  single atomic flow (#157).** A new `Agent.identityMemberID` field binds the
  execution Agent to its organization-member principal, and `POST
  /api/members/agent` (with `worker_id` + `model` + `cli`) wraps the
  `AgentProvisionService.Provision` and `Agent.CreateAgent` calls in a single,
  re-entrant `RunInTx` boundary — either both rows commit, or both roll back (the
  TDD includes a "bad worker → 4xx → no orphan member, no orphan agent"
  assertion). Clicking an agent in the Members list now navigates to its
  Agent-detail page via `identityMemberID`. The legacy `member_id`-only Provision
  path remains available for non-agent member kinds.
- **Channel participant invite is now a search + multi-select modal (#167).** The
  free-text "Invite identity" input is replaced with an `Invite` button that opens
  a `MemberInviteModal`: a search box filters `/api/members` client-side, each
  candidate row carries a `Human` / `Agent` badge, and multi-select + confirm
  issues a batch `POST .../participants` per selected member. Active participants
  are excluded from the candidate list; participants previously removed from the
  channel (`left_at` set, still in the org) reappear as candidates and can be
  re-invited. The Participants panel itself is now collapsible (state persisted in
  `localStorage`) so a long member list doesn't crowd the channel view.
- **Worker page shows execution capabilities (#176).** The Environment worker card
  now renders the discovered CLI list with `detected`/`enabled` status. (Reported
  as the visibility half of #147; see Fixed → "Worker capabilities are discovered
  automatically …".)
- **v2.7 release acceptance harness, in-repo (#163).** The acceptance checklist
  (16 functional domains, real-install methodology, executable verification
  recipes + exit criteria) and the multi-worker per-machine deployed-smoke
  evidence (real binary install of two workers, 6-of-6 isolation checks green)
  are committed under `docs/release/acceptance-checklist.md` and
  `docs/release/evidence/v27-multiworker-deployed-smoke.md` respectively.

### Changed

- **Sidebar IA: ORGANIZATION → "Members"; Fleet + Environment merge into a single
  "Environment" page; "Agents (organization)" → "Agents" (one entry, in Members);
  Organization Settings moves into the organization switcher; the "agent-center"
  text label leaves the header (#164 / #165 / #166 in one PR).** The SYSTEM
  sidebar group is now `{Environment, Settings}` (Fleet removed; the legacy `/fleet`
  route 302s to `/environment`). The Environment page absorbs Fleet's worker
  cards, `active_count`, rename / install / remove modals, and the work-items +
  pending-issues sections; the worker `status` semantics there are now the
  workforce-enrolled set (not the control-channel connection), matching the
  Environment-Worker convergence (#140 step-2). The Members group is
  `{Humans, Agents}` (one Agents entry, replacing the old SYSTEM → Agents). The
  organization switcher dropdown now carries a `Settings` button that opens
  `/org/settings` (still directly addressable); the sidebar no longer lists
  Organization Settings.
- **All user-facing strings spell out "Organization" instead of "Org" (#151).**
  Sidebar labels, switcher placeholders, page subtitles, the Agents-organization
  badge, and incidental form copy all use the full word. Code-level comments and
  internal symbols still use the short form where it was already there.
- **All web console copy is English (#166).** The Organization Settings page,
  modals, toasts, and error messages no longer mix Chinese strings into the
  English UI.
- **Worker model is consolidated on `workforce.Worker` everywhere (#140 step-2 /
  step-3).** The Environment page's worker list reads `workforce.Worker` (via
  `/api/workers`) instead of `environment.Worker`; the `last_acked_offset` field
  is no longer surfaced (control-channel state, not a UI concern). The internal
  `environment.Worker` aggregate no longer carries its own copy of
  `organization_id` (the canonical owner is the `workforce.Worker`); the
  connection-establishment handler derives the worker's org from the
  `workforce.Worker` row before binding the control session, and the unused
  `environment_workers.organization_id` column / `ListByOrg` projection are
  removed.
- **All confirmation dialogs are now in-app modals; native
  `window.confirm/alert/prompt` are banned (#169).** A new `ConfirmModal` (built
  on the existing `useModalA11y`) replaces every native browser dialog (Secrets
  revoke, Environment Re-mint, Environment Remove worker). An ESLint
  `no-restricted-globals` + `no-restricted-properties` rule wired into `make lint`
  prevents the regression.

### Removed

- **CLI data-management and read-only business commands (#162).** `agent-center
  agent / secret / channel / conversation / project / message / inspect / query /
  ps / stats / logs / peek-trace` are removed: all of these are now performed
  through the Web Console (the SPA + REST surface). Operational commands —
  `server`, `version`, `worker` (+ `run` / `mcp-host` / `agent-supervisor` /
  `shim`), `install`, `uninstall`, `upgrade`, `migrate`, `bootstrap`, `admin`,
  `help` — are retained.
- **`identity.default_user` config key (#162).** With the CLI data path retired,
  the remaining `DefaultActor()` consumers (system reconciler, admin sink, the
  webconsole HandlerDeps' no-session fallback, the worker daemon's single use)
  now stamp `observability.Actor("system")` directly. The struct field, default,
  YAML key, env / flag overrides, validation, and the `install` config template
  are all gone.
- **The `--capabilities` flag is gone from `install`, `worker run`, and
  `agent-center upgrade` (#147).** Capability reporting is now auto-probed on
  every worker `online`; manual capability strings are no longer accepted (and
  would have been silently overwritten by the next probe).

### Breaking changes

- **Fresh install required.** v2.7 drops and recreates database tables (the
  agent / agent-work-item / pm-task / pm-issue / pm-project / files BC schemas
  are introduced clean; the legacy `taskruntime` / `discussion` / old-
  `tasks`-`issues`-`projects` schema, retired during the carve-out, is not
  re-created). v2.6 data does not migrate; back-up before installing.
- **CLI data-management commands removed.** See Removed; integrate against the
  Web Console / REST API instead.
- **`install worker --worker-id` is now required.** See Fixed (#171).
- **macOS install default `server.listen_addr` is `:7050`.** Was `:7000`; see
  Fixed (#161). This is the install-template default — a custom `--prefix` install
  is unaffected if the operator overrode the value.

## [v2.6.1] — 2026-05-29

### Fixed

- **v2.6 build break.** `AppLayout` used unnamed `useEffect` / `useState` and the
  SPA lint step did not run `tsc -b`, so app-source type errors slipped through.
  `lint-spa-tsc` now uses `tsc -b` to actually check app sources.

## [v2.6.0] — 2026-05-28

### Breaking changes

- **Fresh install required.** v2.6 drops and recreates several database tables
  (`identities`, `organizations`, `members`, `invitations`). Existing v2.5.x
  data (channels, conversations, projects, tasks, issues, secrets, workers) is
  preserved. v2.5 bookmark URLs change to `/organizations/{slug}/...`; browser
  history links will redirect automatically.
- **Signup required on first boot.** A fresh install (or post-wipe restart)
  opens the browser to `/signup`. The old `identity.default_user` config key
  is removed; all identity is now BC9-managed.
- **Supervisor removed.** `supervisor` CLI subcommand and all supervisor
  invocation / decision record concepts are gone. Tasks and issues use the
  simplified dispatch path directly.

### Added

- **Identity BC (BE-1…BE-9):** Full multi-tenant identity layer — `Identity`,
  `Organization`, `Member`, `Invitation` AR + SQLite repos + 15 domain events.
- **Auth:** `POST /api/auth/signup`, `/signin`, `/signout`, `GET /api/auth/me`,
  `PATCH /api/auth/me/passcode`. JWT HS256 session cookie (7-day TTL, master
  key as signing key per ADR-0043).
- **Org management:** `GET/POST /api/orgs`, `DELETE /api/orgs/{id}`.
- **Member management:** `GET/POST /api/members`, role change, disable, re-enable.
- **Frontend auth flow:** `/signup` and `/signin` pages with full inline
  validation. 401 interceptor redirects to `/signin`.
- **Organization-scoped routing:** all existing routes migrated to
  `/organizations/{slug}/...`. `OrgGuard` validates slug; `OrgRedirect` sends
  `/` to the first org.
- **Sidebar update:** Members group (Humans / Agents / Org Settings), Org
  Switcher in top bar, Sign Out footer button.
- **`/me` page:** passcode change + sign out.
- **Org settings page:** org info display + delete org with confirmation.
- **Members Humans page:** list + Add User modal + role change + disable /
  re-enable per-member actions.
- **Members Agents page:** list agent members (read-only).
- **`secretmgmt.MasterKey.Bytes()`:** exposes raw key material for JWT signing.

### Changed

- Migration 0036: drops `supervisor_invocations` + `decision_records` tables.
- `targetSchemaVersion` constant bumped to 36.
- `migrate v1-to-v2` carries fresh installs to schema 36.

### Removed

- `internal/cognition/scheduler/`, `internal/cognition/decision/`, supervisor
  invocation AR, decision record AR, all supervisor CLI commands.
- `internal/cli/supervisor/` package.
- `internal/conversation/identity/` subdomain (replaced by Identity BC).
- `identity.default_user` config key.

---

## [v2.5.17] — 2026-05-27

Uninstall now actually deregisters the launchd service from macOS
Background Items (#72). `agent-center uninstall center|worker` on
macOS Ventura+ used to leave a stale ON toggle in System Settings →
Login Items & Extensions → Allow in Background even after the
daemon stopped and the plist file was removed. Root cause: the
teardown ran `launchctl unload <plist>`, which is the legacy API
and only stops the running daemon — modern macOS tracks
LaunchAgent registration via Service Management (SMAppService), and
`unload` doesn't touch it.

Secondary: `runShellTolerant` was swallowing both stdout and stderr,
so when `launchctl unload` silently no-op'd (or failed for any
reason — wrong domain, missing plist, etc.) the operator had no
feedback and the uninstall looked successful while the entry stayed.

### Changed

- **`serviceTeardownCmds`** (`internal/cli/handlers_uninstall.go`)
  on the launchd branch now emits
  `launchctl bootout gui/<uid> <plist>` instead of
  `launchctl unload <plist>`. `bootout` (since macOS 10.10) kills
  the daemon AND removes the SMAppService registration in one call.
  The GUI domain target uses `os.Getuid()` Go-side via a
  `launchdGUIDomain` indirection so tests can stub it.
- **`runShellTolerant`** now echoes each teardown command + writes
  the subprocess stdout/stderr through to `out`, so the operator
  sees what the service manager actually said. Still tolerant of
  non-zero exits — a service that's already stopped is not a
  failure.
- **`docs/deployment/v2.4-first-mile.md` § 6.1** — manual
  decommission recipe updated to use `launchctl bootout` with the
  v2.5.17 rationale inline.

### Verification

- 2 new specs (`TestServiceTeardownCmds_LaunchdUsesBootout`,
  `TestServiceTeardownCmds_SystemdUnchanged`) pin the wire-level
  teardown commands per service manager so the bootout change is
  guarded against regression. Existing dry-run + preserve + purge
  uninstall specs still pass unchanged.
- `make lint` + `go test ./internal/cli/...` clean.

### Note (operator follow-up)

If you already ran an earlier uninstall and your System Settings →
Login Items & Extensions → Allow in Background still lists
`agent-center` with a stale toggle, you can clear it one-time with
either:

```bash
# Per-plist (if the plist is still on disk):
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.agent-center.center.plist

# Or by label (works even if the plist was removed):
launchctl bootout gui/$(id -u)/com.agent-center.center
```

After v2.5.17, `agent-center uninstall center|worker` does this
automatically.

---

## [v2.5.16] — 2026-05-27

Web Console TaskDetail discussion thread (#69). Tasks created via
the SPA's New Task modal — and any legacy task whose creator left
`with_conversation` off — landed on a TaskDetail page with only
metadata and action buttons. There was no MessageList, no
composer, no way to discuss the task with collaborators or agents.

### Added

- **POST `/api/tasks/{id}/bind-conversation`** wraps
  `TaskService.BindConversation` in `auto` mode — creates a
  Conversation (kind=task) and binds it to the task under one tx.
  Idempotent against re-bind: the AR rejects the second call
  (`task: conversation_id already set`).
- **`useBindTaskConversation(taskId)`** SPA mutation hook.
- **TaskDetail empty-state** (`task-no-conversation` / `task-start-discussion`)
  surfaces when `task.conversation_id` is empty. Clicking
  `[Start discussion]` calls the new endpoint; on success the
  query invalidation re-renders the page with the MessageList +
  composer.

### Changed

- **TaskCreateModal** now passes `with_conversation: true` so
  every newly-created task gets a discussion thread out of the
  box. Operators who don't want a chat thread can still create
  tasks via the CLI without the flag.
- **TaskDetail** message panel / composer / participants now gate
  on `convId` so the layout stays clean when a task has no
  conversation (previously the composer was always mounted even
  with no conversation, which silently dropped sends).

### Verification

- 3 new backend tests (`TestAPI_BindTaskConversation_Happy`,
  `_AlreadyBound_Rejected`, `_NotWired`) cover the new endpoint,
  including the re-bind rejection.
- 1 new frontend spec (`offers Start discussion CTA when the task
  has no conversation`) — toggles a stateful MSW handler to
  simulate the bind flow + asserts the composer appears post-bind.
  312 vitest specs green.
- `make lint` + `go test ./...` clean.

---

## [v2.5.15] — 2026-05-27

Web Console "All projects" filter actually works now (#68 + #70 —
same root cause, bundled per PM ask). Both `/issues` and `/tasks`
showed the "Pick a project" nudge whenever the project chip was on
`All`, with no way to see issues / tasks across the whole workspace.
The list endpoints required `project_id` (returned 400 otherwise),
so the SPA short-circuited the fetch instead of issuing it.

### Added

- **Discussion BC** `IssueRepository.FindAll(ctx, filter)` returns
  every issue with optional `Status` / `Cursor` / `Limit`. SQLite
  impl mirrors the existing `FindByProject` shape minus the project
  predicate.
- **TaskRuntime BC** `task.Repository.FindAll(ctx, filter)` returns
  every task with optional `Status` / `Limit`. SQLite impl mirrors
  the existing `FindByProject` shape minus the project predicate.

### Changed

- **GET `/api/issues`** (`listIssuesHandler`) — `project_id` is now
  OPTIONAL. When omitted the handler delegates to `FindAll(filter)`
  so the SPA can render the cross-project list. `status` continues
  to filter (now across projects when paired with the omitted
  `project_id`).
- **GET `/api/tasks`** (`listTasksHandler`) — symmetric change.
- **`web/src/pages/Issues.tsx`** + **`Tasks.tsx`** — drop the
  `projectFilter === 'all'` empty-state gating. When the chip is on
  `All`, each row gains a project chip column (`issue-row-project`
  / `task-row-project`) so operators can see which project each row
  belongs to. The project chip-row + status tab row stay where they
  were.
- **`useIssues`** / **`useTasksList`** — removed the
  `enabled: !!projectId` gate; both hooks fetch unconditionally now.

### Verification

- Backend: 2 new SQLite repo tests (`TestIssueRepo_FindAll`,
  `TestTaskRepo_FindAll`) cover cross-project + status filter +
  limit; 2 new API tests per endpoint (no `project_id` → 200 with
  cross-project rows; `status` without `project_id` filters
  cross-project). 2 prior tests that pinned the 400 contract were
  rewritten — the 400 path no longer exists by design.
- Frontend: `Tasks.test.tsx` + `Issues.test.tsx` `shows … from
  every project when no project is selected` cover the new path
  (3-row list + project chip column rendering). MSW handlers
  updated to return the cross-project union when `project_id` is
  omitted. The hook-level `useIssues / useTasksList` tests rewritten
  to assert the now-enabled cross-project fetch. 311 vitest specs
  green.
- `make lint` + `go test ./...` + e2e clean.

### Note (intentional UX shift)

The old "Pick a project" nudge was a workaround for the
`project_id`-required contract. v2.5.15 makes the cross-project
list a first-class view; operators who want a single-project view
keep the chip row + the new per-row project chip helps disambiguate
when scanning `All`.

---

## [v2.5.14] — 2026-05-27

Web Console sidebar consistency fix (#67). The Workspace group's
`Projects` link was the only nav entry that didn't follow the
Slack-style expand-to-sub-items pattern introduced in v2.5.9 (#63)
for Channels + DMs. With both sibling groups exposing their items
inline, the operator expected the same affordance for Projects.

### Changed

- **AppLayout sidebar** (`web/src/AppLayout.tsx`) — Projects nav
  item now expands to a sub-list of project names (link target
  `/projects/<id>`) with the same count badge, toggle button, and
  `ac.sidebar.subitems` persistence used by Channels and DMs. The
  Workspace group label gains the collapsible toggle for free
  since v2.5.9 made it data-driven.

### Verification

- 2 new specs in `AppLayout.sidebar.test.tsx`:
  `Projects item exposes a sub-list of project names when expanded`
  asserts the sub-list renders the seeded project names + correct
  hrefs; `Projects sub-list toggle collapses + persists like
  Channels/DMs` asserts the toggle button writes to
  `ac.sidebar.subitems`. 311 vitest specs green (9 in
  AppLayout.sidebar.test.tsx, +2 from v2.5.9's 7).
- `make lint` clean.

---

## [v2.5.13] — 2026-05-27

Web Console SSE connection indicator cycling fix (#71). The topbar
indicator was looping through `connecting → reconnecting → open`
every 30s on a healthy connection — not because SSE was actually
failing, but because the watchdog timer in the frontend hook was
never being reset. Two coupled bugs:

1. **Backend heartbeat was emitted as a `: ping` SSE comment line**
   (W3C-spec correct for keep-alive), but EventSource drops comment
   lines silently — they never fire `onmessage` on the client.
2. **Frontend watchdog was set to 30s**, identical to the backend
   heartbeat interval. Even if the comment had been a real event, a
   symmetric timer pair would still race.

The combined effect: zero `onmessage` traffic for 30s on every
connection → watchdog fires → close + reconnect → repeat.

### Changed

- **SSE Bus `ServeHTTP` heartbeat** (`internal/webconsole/sse/bus.go`)
  now emits a real `data: {"event_type":"sse.heartbeat"}\n\n`
  frame instead of the `: ping` comment. No `id:` line is set, so
  the ringbuffer ID sequence and clients' `lastEventId` anchor are
  unaffected. The frame falls through `dispatchToQueryClient`'s
  default branch (no invalidation), and crucially fires the
  browser `onmessage` event so the client watchdog resets.
- **Frontend SSE watchdog timeout** (`web/src/sse/useSSE.ts`)
  bumped 30s → 45s. With backend heartbeats every 30s the watchdog
  now has a 15s slack for network jitter / GC pauses, eliminating
  the symmetric-timer race while still catching half-open sockets
  (the original Safari/iOS guard) within ~75s of true silence.

### Verification

- Backend: new `TestServeHTTP_HeartbeatIsRealDataMessageWithoutID`
  pins the on-wire shape (data frame containing `sse.heartbeat`,
  no `id:` line). Existing
  `TestServeHTTP_StreamsEventAndHeartbeat` updated to accept the
  new heartbeat format.
- Frontend: new `heartbeat data message resets the watchdog and
  keeps status open` test simulates a 30s-in heartbeat + 30s of
  silence (cumulative 60s past `onopen`) and asserts the
  connection stays in `open`. Existing `heartbeat timeout forces
  reconnect when no event arrives` updated for the new 45s
  threshold. 29 useSSE specs + 7 useSSEConversationSubscribe + 1
  SSEIndicator → 37 SSE-layer tests green.
- `make lint` clean (`go vet`, arch lint, no-mock-default,
  doc-impl-drift, raw-color SPA grep, `tsc --noEmit`).

---

## [v2.5.12] — 2026-05-27

### Changed

- **Makefile `lint` target** now runs `lint-spa-tsc` (cd web &&
  npx tsc --noEmit) as part of the composite lint pipeline (#66,
  follow-up from #65 build break at
  `#agent-center:700dde8d`). `npm test` (vitest) doesn't run the
  TypeScript compiler and `npm run build` is only triggered at
  release time — which let v2.5.9 ship a type break that
  surfaced only during PM smoke. Adding `tsc --noEmit` to the
  local + CI lint loop catches the class of issue (typed missing
  field, backend projection / SPA type drift) before ship.

### Verification

- `make lint` runs all existing linters plus the new TypeScript
  check; clean against current `main`.

---

## [v2.5.11] — 2026-05-27

Web Console Issue Edit + Reopen (#64, follow-up to #61 split).
Closes the last gap in the v2.5.x Issue management surface.
Reopen semantics chosen by @oopslink at
`#agent-center:93118955` — option (c) "Reopen does not touch
spawned tasks": any concluded/withdrawn issue can be flipped
back to open, but any tasks that were spawned at conclude time
remain in their current state. This keeps the operator's mental
model "reopen the discussion" simple and avoids a cross-BC
cascade orchestrator.

### Added

- **Discussion BC** — `Issue.UpdateMetadata(title, description,
  now)` AR method. Title required; rejects terminal status
  (operator must reopen first).
- **Discussion BC state machine** — new edges from each
  concluded/withdrawn terminal back to `open`:
  `closed_no_action → open`, `closed_with_tasks → open`,
  `withdrawn → open`. `Status.IsTerminal()` is unchanged so
  Conclude / Withdraw still reject double-application.
- **Discussion BC** — `Issue.Reopen(reopenedBy, now)` AR method.
  Clears `conclusion_summary` / `concluded_by` / `concluded_at`
  / `withdraw_reason` / `withdraw_message` since they no longer
  reflect current state (event log captures the historical
  conclude/withdraw). Spawned tasks are not touched.
- **IssueRepository** — `UpdateMetadata` + `UpdateReopen` CAS
  methods. `UpdateReopen` is a single statement that flips
  status + clears the conclusion/withdraw columns atomically.
- **IssueLifecycleService.UpdateMetadata** / **Reopen** wrap the
  AR methods with tx + repo write + observability emit
  (`issue.metadata_updated` / `issue.reopened`).
- **PATCH /api/issues/{id}** wraps UpdateMetadata.
- **POST /api/issues/{id}/reopen** wraps Reopen.
- **IssueDetail page**: `[Edit]` action (hidden when terminal);
  `[Reopen]` action (only when terminal). New `IssueEditModal`
  prefills from the current issue and PATCHes on submit.
- **issuePublicMap** now emits `description` (the field was
  always on the AR but was never exposed; the SPA `Issue` type
  gains the field to keep `tsc --noEmit` happy).

### Verification

- Domain: 5 new Issue AR unit tests (UpdateMetadata happy /
  empty-title / terminal-rejected, Reopen happy from both
  closed_no_action + withdrawn, Reopen rejects non-terminal,
  Reopen requires actor).
- Discussion state machine: status_test.go updated — three new
  legal edges, withdrawn → concluded still illegal.
- Backend: 6 new webconsole API tests (Edit happy /
  terminal-rejected / not-wired, Reopen happy /
  non-terminal-rejected / not-wired).
- Frontend: 308 vitest specs green (3 new for
  `IssueEditModal`).

---

## [v2.5.10] — 2026-05-27

Web Console Task Edit metadata (#65, follow-up to #62 split).
Closes the last gap in the v2.5.x Task management surface so the
Edit action shows up alongside Suspend / Resume / Abandon on a
non-terminal task.

### Added

- **TaskRuntime BC** — `Task.UpdateMetadata(title, description,
  priority, now)` AR method. Enforces title required + valid
  priority enum; rejects edits on terminal status (done /
  abandoned). Bumps version on success.
- **TaskService.UpdateMetadata** wraps the AR method with tx +
  repo write + `task.metadata_updated` event emit.
- **PATCH /api/tasks/{id}** wraps the service. Accepts `title`
  (required) + `description` + `priority`. Returns
  `{task_id, event_id}`.
- **TaskDetail page**: `[Edit]` action in the header (hidden
  when terminal). New `TaskEditModal` prefills from the current
  task and PATCHes on submit.

### Verification

- Domain: 4 new task AR unit tests (happy + missing title +
  invalid priority + terminal-rejected).
- Backend: 4 new webconsole API tests (happy + missing title +
  terminal-rejected + not-wired).
- Frontend: 305 vitest specs green (3 new in
  `TaskEditModal.test.tsx`).

---

## [v2.5.9] — 2026-05-27

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
