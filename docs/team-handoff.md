# Team Handoff — agent-center

> **Single entry point for a successor team.** Read this page plus the
> authoritative docs it links, and you can run the **agent-center** project and
> its delivery team without any prior chat context. Written **2026-06-12** by the
> Product Manager agent (PD) at the close of the **v2.9** cycle.
>
> This is a *durable, meta* document — it is about the **project, the team, and
> how we work**, not any single release. It lives on `main` and should be kept
> current as the team and process evolve. For release content see
> [`CHANGELOG.md`](../CHANGELOG.md) and [`README.md`](../README.md); for the
> design system of record see [`docs/index.md`](./index.md).

---

## 0. TL;DR

- **agent-center** is the **product**: a DDD-architected, multi-agent
  orchestration & project-management platform (Go backend + a pnpm/React SPA Web
  Console + MCP tool surface + CLI). It runs *inside* a multi-agent runtime
  (Slock), but **agent-center ≠ the runtime** — never conflate them.
- **Current release: v2.9 (shipped 2026-06-12, tag `v2.9` → `b29376b`).** Headline
  feature: **Plan Orchestration** (DAG-driven multi-agent workflow). See §6.
- **Branch reality:** the `v2.9` tag (`b29376b`) is the immutable release point.
  **v2.9 was promoted to `main`** on 2026-06-12 (per stakeholder) via a `--no-ff`
  sync merge, so `main` now carries the v2.9 release — see §8.
- **The team is 6 agents** with fixed lanes (§3). **How we ship** is a
  verify-not-trust, multi-face-acceptance, single-actor-merge discipline (§4).
- The hardest-won, most reusable knowledge is in §5 (**institutional lessons**) —
  read it to avoid relearning the same lessons the expensive way.

---

## 1. What agent-center is (one page)

A platform where **humans and AI agents collaborate on software-project work** —
organizations, projects, members (human + agent), issues, tasks, plans, and
conversations — with agents that can be **dispatched, woken, and orchestrated**.

- **Backend:** Go, DDD, ~6 conceptual bounded contexts (the design site counts
  "6 BCs / 34 ADRs"). The Go module is organized as fine-grained packages under
  [`internal/`](../internal): the load-bearing ones are
  **`projectmanager`** (orgs / projects / issues / tasks / **plans + DAG**),
  **`conversation`** (messages, participants, conversation lifecycle),
  **`identity`** (users, auth, passcodes, orgs membership),
  **`environment`** (agent wake / dispatch projection),
  **`mcphost`** (the MCP tool surface agents call),
  **`webconsole`** (HTTP API for the SPA),
  plus supporting packages (`agent`, `agentsupervisor`, `workforce`,
  `mention`, `outbox`, `persistence`, `secretmgmt`, `mcphost`, …).
- **Frontend:** a single SPA under [`web/`](../web) (pnpm workspace, React + TS +
  Tailwind, Vitest, ESLint). Org-scoped routes under `/organizations/{slug}/...`.
- **Tool/automation surface:** **MCP** tools (agents author & drive work
  programmatically) + a **CLI** (`cmd/agent-center`); `cmd/fakeagent` is a test
  harness agent.
- **Design system of record:** the VitePress DDD docs ([`docs/index.md`](./index.md))
  — strategic layer (domain vision / subdomains / ubiquitous language), tactical
  layer (per-BC aggregates / domain services / invariants), 34 ADRs, and the
  implementation layer.

**Ubiquitous language (the terms that matter):** see §9.

---

## 2. Repo map — where everything lives

| You want… | Look here |
|---|---|
| What the product is / how to run it | [`README.md`](../README.md) (+ [`README.zh-CN.md`](../README.zh-CN.md)) |
| What changed per release | [`CHANGELOG.md`](../CHANGELOG.md) |
| Architecture / DDD design (authoritative) | [`docs/index.md`](./index.md) → `docs/design/**` (strategic / tactical / ADRs / implementation) |
| **The rules we are bound by** | [`docs/rules/`](./rules) — see §4 |
| Per-release plans / backlogs / cycle handoffs | [`docs/plans/`](./plans) (e.g. the v2.9 cycle: `plans/v2.9-handoff.md`, `plans/v2.9-backlog.md`) |
| Release & ops runbooks | [`docs/release/`](./release), [`docs/deployment/`](./deployment), [`docs/operations/`](./operations) |
| Public marketing / docs site (static) | [`sites/`](../sites) |
| Backend code | [`internal/`](../internal), [`cmd/`](../cmd) |
| Frontend code | [`web/src/`](../web/src) |
| Build / test / lint entry | [`Makefile`](../Makefile) — see §7 |

> **Two different "sites":** [`sites/`](../sites) is the hand-rolled static
> marketing/showcase site (homepage + roadmap). [`docs/`](./) is the VitePress DDD
> design site. They are different surfaces — don't confuse them, and keep both in
> sync on release per the release-docs-sync rule (§4).

---

## 3. The team & roles

Six agents, fixed lanes. **Each member owns a lane end-to-end; cross-lane work is
coordinated, not freelanced.** Humans @mention the relevant lane; the PD
coordinates.

| Agent | Lane | Owns |
|---|---|---|
| **PD** (`AgentCenterPD`) | Product / process / **§-1 review gate** | Requirements & rulings (PD/DDD), the §-1 review on **every** PR (verify-not-trust), acceptance design & reporting, release docs, tagging, keeping the stakeholder honestly informed. **Does not merge feature code or self-accept.** |
| **Dev** (`AgentCenterDev`) | Backend | Domain model, services, HTTP API, migrations, MCP tools (server side). |
| **Dev2** (`AgentCenterDev2`) | Frontend | The SPA — pages, routing, components, both-mode (light/dark) AA, reachability. |
| **Tester** (`AgentCenterTester`) | Data/API acceptance | Deterministic go-test + real-instance prod-zero-footprint checks; **inverse-mutation class-guards** (a test per defect *class*, not per defect). |
| **Tester2** (`AgentCenterTester2`) | E2E / UI-UX acceptance | **run-real** acceptance (drive the real app + real LLM, computed-truth screenshots), UX/AA, §4.2 reachability. |
| **IntegrationDev** (`AgentCenterIntegrationDev`) | Integration | The **single actor** who merges PRs to the release branch, handles rebases/supersets, keeps the board clean. |

**Collaboration model in one line:** Dev/Dev2 build on a feature branch →
**PD §-1 reviews every PR** (read-only diff + runs tests in an isolated worktree)
→ **Tester (data/API) + Tester2 (run-real)** accept independently →
**IntegrationDev merges** (single actor) → PD designs/runs acceptance, writes the
report, syncs release docs, tags.

---

## 4. How we ship (the process) — and the rules that bind it

The full rules are in [`docs/rules/`](./rules) and are **binding**, not advisory:

- [`conventions.md`](./rules/conventions.md) — cross-cutting code/design rules.
  Notably **§20 "API has no implicit state"** (org context is explicit in the
  path — `/api/orgs/{slug}/...` — never session-implicit) and **§21 "single
  canonical page/route"** (delete the old one on replace; no orphan routes).
- [`acceptance-methodology.md`](./rules/acceptance-methodology.md) — **§4.1**
  (inline evidence per acceptance point), **§4.2** (verify via a *real
  reachable* user path — nav / shortcut / breadcrumb / post-create redirect — not
  just a direct URL), and **§4.3** (the release acceptance report MUST carry
  **user-perspective end-to-end key-step screenshots** produced by a
  **committed, reproducible capture script** — a text-only acceptance report is
  not a pass).
- [`testing.md`](./rules/testing.md) — test discipline (independence, ownership by
  scope, real-artifact acceptance).
- [`ux-standards.md`](./rules/ux-standards.md) — both-mode AA, **no alpha-tint**
  (`bg-{token}/{opacity}` + same-token text fails contrast — use solid
  `X-100/X-800` chips), the visual standards.
- [`v2.7-delivery-process.md`](./rules/v2.7-delivery-process.md) — the delivery
  process: branch model, §-1 review against the **promoted release ref**, and
  **§23 release-docs-sync** (a release must update README + CHANGELOG + sites
  together).
- [`ddd-design-diagram.md`](./rules/ddd-design-diagram.md),
  [`documentation.md`](./rules/documentation.md) — design-diagram & doc rules.

**The operating disciplines that make the above work:**

1. **Branch model.** All work for a release lands on the release branch (e.g.
   `v2.9`); PRs target it; the released commit is **tagged** (annotated). `main`
   is the long-term default; promoting a release branch to `main` is an explicit
   step (see §8).
2. **PD §-1 on every PR — verify, don't trust.** PD reviews the **read-only diff**
   from a clean checkout *and* **runs the tests in an isolated worktree** before
   approving. No PR merges on a code-read alone (see §5.1).
3. **Multi-face acceptance.** A feature is accepted by **two independent faces**:
   Tester (deterministic data/API + class-guards) **and** Tester2 (**run-real** on
   the live app/LLM). PD designs the acceptance from a **product-feature angle**,
   independent of dev progress (§5.6). **Nobody self-accepts.**
4. **Single-actor merge.** Only **IntegrationDev** writes to the shared release
   branch. Anyone touching shared state announces it in the **main channel** (not
   a thread) first, and routing for a given branch goes through one owner (§5.2).
5. **Isolated worktrees.** PD/reviewers build & test in throwaway worktrees
   (e.g. `ac-wt-pd`); **the canonical checkout is never mutated** for review.
6. **mock = contract, day-0.** Frontend builds against the agreed API contract
   immediately, in parallel with the backend — no waiting.
7. **Release-docs-sync.** On release: README (EN + zh-CN) + CHANGELOG + sites
   showcase/roadmap are updated **together** and reviewed multi-face, then the
   release commit is tagged.

---

## 5. Institutional lessons (hard-won — read before you "optimize" the process)

These are the lessons this team paid for. They are externalized here so they
survive the handoff.

### 5.1 run-real-truth > code-read — and it's a *merge gate*, not a fast-follow
A change can look correct in the diff and still be wrong against the running
system. **Three times** in recent cycles a feature passed code review but **failed
run-real**, and the real cause was upstream of where the diff looked correct
(e.g. a plan-conversation @mention not waking the agent — the visible fix was in
the wake projector, but the *actual* gap was an event-emit gate one layer up).
**Rule:** hold the hard run-real gate; do not downgrade run-real to a fast-follow.
If run-real is red, it isn't done.

### 5.2 Shared-state writes = single actor + announce-in-main
Every coordination slip we had traced to two actors touching shared state (a
merge out of queue; a merge inside someone's verify window; a concurrent rebase /
force-push). **Rule:** writes to a shared branch go through **one actor**
(IntegrationDev), and any shared-state action is **announced in the main channel
before** it happens (a thread is not visible enough). This is now institutional,
not ad-hoc.

### 5.3 Superset-pattern for stacked PRs
When a guard/test PR branches off a fix PR, you get a **superset** (the second
contains the first). Recognized **3×** (`#306⊇#305`, `#311⊇#308`, `#314⊇#313`).
**Rule:** detect the base, **merge the superset, close the subset.** Don't merge
both. When possible, branch follow-up work off clean trunk to avoid the stack
entirely.

### 5.4 Trace the field to its source at *every* render site (contract seams)
A status/flag that is correct in one DTO can be **missing in a parallel DTO** used
by a different render site. (A task "archived" badge was correct on the board card
— `pmTaskMap` — but the DAG/task-list read a different map — `pmPlanNodeMap` —
that lacked the field.) **Rule:** when verifying a derived field, trace it to its
**data source for each render site**, not just the one component you happened to
open.

### 5.5 Both-mode AA, no alpha-tint (recurred 4×)
Alpha-tint chips (`bg-{token}/10` + same-token text) silently fail WCAG contrast,
especially in light mode. It recurred **four times**. **Rule:** solid
`X-100/X-800` chips, AA-verified (≥4.5) in **both** light and dark; an ESLint
guardrail is the durable fix.

### 5.6 Design acceptance from the product-feature angle, not dev progress
Acceptance plans must be written from **what the product should do for a user**,
independent of which PRs landed or how it was built. A plan that tracks
PRs/merges tests the *implementation*, not the *capability*.

### 5.7 Existence-non-disclosure: cross-tenant = 404, not 403
For org/tenant isolation, a request for something in another org returns **404**
(as if it doesn't exist), **not 403** (which would confirm it exists). This is a
deliberate security posture (org routing was cut over to explicit
`/api/orgs/{slug}/...` with no compatibility shim, cross-org → 404).

---

## 6. What v2.9 delivered (current release)

Authoritative detail: [`CHANGELOG.md`](../CHANGELOG.md) → `## [v2.9]`. In brief:

- **Plan Orchestration** — a Plan groups pre-assigned tasks into an **acyclic
  dependency DAG** that **auto-advances**: when a node's upstream completes, the
  system **@mentions the node's assignee in the Plan's conversation** (@human =
  notify, @agent = wake to act). Delivered in three phases: **P1** model + manual
  advance + DAG view, **P2** event-driven auto-orchestrator, **P3** PM-agent **MCP
  authoring** (`create_plan` / `add_task` / `add_dependency` / `start_plan`).
- **Explicit org routing** — full cutover to `/api/orgs/{slug}/...` (no
  session-implicit org, no `?org_slug=` shim); cross-org → 404 (§5.7).
- **Plan-conversation @mention-wake** — @mentioning a project agent in a *plan*
  conversation now wakes it (issue/task conversations already did since v2.7.1;
  plan conversations + non-participant project-member breadth are the v2.9 add).
- **Archive semantics** — archived projects are read-only and enforced
  server-side (mutations → 409); archived projects are excluded from the default
  project list (`?status=archived` to see them); a Plan with a running task
  cannot be archived.
- **Security** — authenticated admin-token revoke (401/403/204).

**Acceptance:** 8/8 capabilities PASS, evidenced with computed-truth screenshots;
the report PDF was delivered to the stakeholder and signed off. The cycle's
start-of-development handoff is [`docs/plans/v2.9-handoff.md`](./plans/v2.9-handoff.md)
(now historical — it describes the *plan* at kickoff, not the shipped state).

---

## 7. Build / run / test

All via the [`Makefile`](../Makefile) (run from repo root):

```bash
make build          # build-frontend + build-backend + build-fakeagent
make build-backend  # Go backend only
make build-frontend # the web/ SPA (pnpm)
make test           # backend test suite
make cover          # coverage (cover-html for the HTML report)
make lint           # vet + vendor + mock-default + doc-impl-drift +
                    # no-raw-colors-spa + spa-tsc + spa-eslint
make smoke          # smoke test
make e2e            # build + end-to-end (e2e-install first)
make release        # release build
```

- **Review/build discipline:** reviewers build & test in an **isolated worktree**
  (e.g. `git worktree add`), never in the canonical checkout.
- **Known env caveat:** on macOS some `agentsupervisor` tests can fail due to a
  socket-path-length limit — that is an environment issue, not a code regression;
  confirm against a clean baseline before attributing a failure to a change.

### 7.1 Production deployment topology (this host)

The live dogfood/production deployment runs on the dev host under
`~/.agent-center` (default prefix), via the **atomic-symlink** model
(`versions/<branch>-<hash>/` + a `current` symlink). Runbook:
[`docs/deployment/v2.4-first-mile.md`](./deployment/v2.4-first-mile.md).

- **Two processes, both hand-run (NOT launchd):** the **center**
  (`agent-center server --config=~/.agent-center/etc/config.yaml`, web `7100` /
  server `7050` / admin TLS `7300`) and **one worker** `worker-edb09a0c`
  (`agent-center worker run --config=.../workers/worker-edb09a0c/etc/config.yaml`),
  which spawns the agent-supervisors that run the dogfood agents. The host also
  has separate **`test-*`** center/worker instances (acceptance sandboxes,
  launchd-managed) — leave those alone.
- **Upgrade:** the blessed path is `agent-center upgrade center|worker`
  (atomic version lay-down + DB migrate-on-start + symlink flip + health probe +
  auto-rollback) — **but** that command restarts via **launchctl**, which does
  **not** apply to the hand-run instance (it has no launchd plist). For the
  hand-run prod instance, upgrade manually: build the new binary, lay it down at
  `versions/<new>/`, stop the old process, flip `current`, restart via `nohup`.
  Keep the previous `versions/<old>/` for instant rollback (flip back + restart).
- **Self-impact is nil for chat ops:** the team/agents communicate over **Slock**
  (a separate cloud daemon → `api.slock.ai`), independent of this local
  agent-center — restarting the deployment does not interrupt Slock coordination.

---

## 8. Open items / decisions for the successor

1. **`v2.9 → main` — DONE (2026-06-12).** v2.9 was promoted to `main` via a
   `--no-ff` sync merge (per stakeholder), matching the prior release-promotion
   pattern. The `v2.9` tag (`b29376b`) remains the immutable release point; `main`
   now carries the v2.9 release. *(Pattern for future releases: ship + tag on the
   release branch, then promote to `main` via a `--no-ff` sync merge.)*
2. **Deep versioned docs (offered, non-blocking).** A deeper `dev/v2.9` +
   `manual/v2.9` versioned doc set was proposed; the homepage showcase + roadmap
   already reflect the ship. Pick up if desired.
3. **v2.9 backlog remainder.** [`docs/plans/v2.9-backlog.md`](./plans/v2.9-backlog.md)
   groups A–F; Plan Orchestration (the centerpiece) shipped, other groups remain
   queued (shipped-but-incomplete gaps, correctness/AA sweeps, quality
   guardrails, verification follow-ups, planned features, minor tech debt).
4. **Passcode policy change (was queued as task #290).** Stakeholder set a
   stronger policy (min 6 chars, letters + digits + symbols, replacing the
   6-numeric-digit rule). Confirm whether this shipped in v2.9 or is still
   pending — touch points: `identity` passcode validation + temp-passcode
   generation + signup/change-passcode UI hint + docs.
5. **Durable guardrails.** The alpha-tint ESLint guardrail and other class-guards
   (§5) should keep expanding — a guard per *defect class*, not per defect.

---

## 9. Glossary (ubiquitous language)

| Term | Meaning |
|---|---|
| **Organization (org)** | Top-level tenant. Routing is explicit: `/api/orgs/{slug}/...`. Cross-org access → 404 (§5.7). |
| **Project** | A unit of work in an org; has members (human + agent), issues, tasks, plans. Can be **archived** (read-only, server-enforced). |
| **Issue / Task** | Work items. A Task has an assignee and a status; status → done is the orchestration trigger. |
| **Plan** | An acyclic **DAG** over selected tasks, with a 1:1 **conversation**. Auto-advances by @mentioning the next ready node's assignee. |
| **Node** | A task's position in a Plan's DAG; its state is derived from the task's status + upstream completion. |
| **Dispatch** | Pulling an assignee into work by **@mentioning them in the Plan/issue/task conversation** (idempotent). |
| **Wake** | Bringing an **agent** online to act on a mention. @human mention = notify; @agent mention = wake. |
| **Conversation** | The message thread attached to a project surface (issue / task / **plan**). Plan conversations gained @mention-wake in v2.9. |
| **BC (bounded context)** | A DDD context boundary. The design site maps the conceptual BCs; `internal/` holds the package-level implementation. |
| **§-1 review** | The PD's mandatory pre-merge review of every PR (verify-not-trust). |
| **run-real** | Acceptance that drives the *real* running app + real LLM and captures computed-truth evidence; a hard merge gate (§5.1). |
| **class-guard** | An inverse-mutation test that protects against a whole *class* of defect, not a single instance. |

---

*Maintained by the PD lane. When the team or process changes, update this page —
it is the front door for the next person.*
