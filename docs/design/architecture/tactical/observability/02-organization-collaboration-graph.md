# Organization Collaboration Graph — Frozen Product/Data/Layout Contract

> T2108 · issue-f1bad8db · contract `collaboration-graph.org.v1`
>
> Baseline: `origin/main` fetched and pinned at
> `45e23623e36aa57e44f8b1b42240545b7cc6d6a1` before this audit.

## 1. Product outcome

The Collaboration landing page is an organization-wide, browsable graph. A user
opens the page and immediately sees the collaboration system without entering a
project or task query. Search and the old task-scoped view are focus controls;
they are never prerequisites for loading the graph.

The first response MUST contain a useful overview, including disconnected
components and low-activity agents. Empty `query`, `project_id`, `task_id`, and
`agent_ref` are valid. Organization authorization comes only from the authenticated
`/api/orgs/{slug}` route; a client-supplied organization id is not accepted.

This read model remains owned by Observability. ProjectManager and Conversation
remain the business truth sources. The graph does not write back, rank people,
or use an LLM to decide edge polarity, importance, or inclusion.

## 2. Frozen topology

Node kinds:

| Kind | Stable id | Required display fields | Meaning |
|---|---|---|---|
| `agent` | `agent:<id>` | `label`, `status` | Organization member, including zero-degree members. |
| `plan` | `plan:<id>` | `label`, `status`, `project_id` | Plan containing visible tasks. |
| `task` | `task:<id>` | `label`, `status`, `project_id`, `plan_id?` | Task, including floating tasks without a plan. |

Edge kinds:

| Kind | Direction | Semantics |
|---|---|---|
| `agent_agent` | agent → agent | Aggregated contact/effect relationship; deterministic sum of shared task/plan evidence. |
| `agent_task` | agent → task | Assignment, completion, block/unblock, review, or other frozen effect relation. |
| `agent_plan` | agent → plan | Aggregation of the agent's task effects in that plan; evidence points to constituent effects. |
| `plan_task` | plan → task | Structural membership from PM truth mirrored into the read model; polarity is `neutral`. |

Every non-structural edge has `event_count`, `first_seen_at`, `last_seen_at`,
`polarity_counts`, `magnitude_sum`, and at least one `evidence_ref`. An aggregate
edge never invents an effect: its counts must equal a deterministic group of
committed effect/evidence ids. `agent_agent` is directed when evidence has a
source/target direction; reciprocal activity is represented by two edges.

## 3. Organization query contract

`GET /api/orgs/{slug}/insights/collaboration-graph`

All query parameters are optional:

| Parameter | Default | Contract |
|---|---|---|
| `view` | `overview` | `overview` or `detail`. |
| `lod` | `auto` | `overview`, `community`, `detail`, or `auto`. |
| `cursor` | empty | Opaque snapshot cursor; reject malformed/mismatched cursors. |
| `limit` | `250` | Page size, `50..500`; caps nodes, not evidence rows. |
| `since`, `until` | server default window | Half-open RFC3339 interval. |
| `focus_kind`, `focus_id` | empty | Optional graph focus; never required to bootstrap. |
| `project_id`, `relation_type`, `polarity` | empty | Filters applied inside the organization boundary. |

The response DTO is frozen in
`docs/design/contracts/collaboration-organization-graph-v1.json`. The server
returns a stable snapshot id and opaque continuation cursor. Every page repeats
the same `snapshot_id`, `as_of`, and `totals`; clients discard accumulated pages
if these change. Ordering is `(community_id, kind, activity desc, id)` and is
stable within a snapshot. `next_cursor=null` is terminal. A continuation request
must not duplicate node ids or edge ids already emitted for that cursor chain.

`truncated=true` means the visible payload is incomplete and the UI must show a
"Load more" affordance. It must not silently present a partial graph as complete.

## 4. Level of detail and progressive loading

`auto` selects:

* up to 250 nodes: `detail`, all node/edge kinds;
* 251–2,000 nodes: `community`, plan nodes plus agent community nodes and
  aggregated cross-community edges;
* above 2,000 nodes: `overview`, community nodes and aggregate edges only.

The response always reports `effective_lod`, `totals`, and `visible_counts`.
Community/overview nodes carry `member_count` and `expand_cursor`. Expanding one
community is a continuation of the same snapshot, not a new filtered query.
The server may reduce detail under a payload budget, but may never report a more
detailed LOD than it returned.

## 5. Layout contract

The browser owns coordinates; the API owns stable topology and optional grouping
hints. Layout is deterministic for `(snapshot_id, effective_lod, viewport class)`:

* desktop ≥1280 px: force-directed communities, plans as group anchors, tasks
  orbit their plan, agents remain outside plan hulls;
* tablet 768–1279 px: same topology with collapsed labels and a bottom evidence
  drawer;
* mobile <768 px: community/plan overview first, tap-to-expand one component;
  no attempt to render every detail node simultaneously.

Pinned/focused nodes retain position while more pages load. Reduced-motion mode
uses one deterministic settle with no continuous animation. Keyboard traversal
follows stable DTO ordering, not canvas coordinates. A graph list alternative
exposes the same nodes, edges, counts, and evidence actions.

## 6. Legend, interaction, and evidence

The legend must independently encode node kind, edge kind, and polarity. Color is
never the sole signal: line style and accessible text accompany it. Counts and
edge thickness represent event volume, not agent quality.

Interactions:

1. Hover/focus previews label, relation, count, and time range.
2. Click pins a node/edge and opens the Evidence drawer.
3. Search locates and focuses an existing graph node; it does not replace the
   organization graph with a task-only page.
4. `Focus on this task/plan/agent` dims non-neighbors and may request detail LOD.
5. `Clear focus` restores the prior viewport and loaded organization graph.

Evidence is fetched lazily from
`GET /api/orgs/{slug}/insights/collaboration-graph/edges/{edge_id}/evidence`.
The response includes aggregate membership, committed event/effect references,
rule version, before/after state where available, and pagination. GET must not
synthesize or persist domain records. Missing evidence is rendered as unavailable,
never converted to a guessed explanation.

## 7. Required states and screenshot baselines

T2110 and T2112 capture the following at 1440×900, 1024×768, and 390×844. The
fixture scenario ids are stable screenshot names:

| Scenario id | Required visible result |
|---|---|
| `org-overview-loaded` | Graph renders without query; all four edge kinds and three node kinds appear in legend. |
| `org-overview-progressive` | Partial payload shows totals, visible counts, and Load more. |
| `org-focus-task` | Task focus dims unrelated topology; breadcrumb and Clear focus preserve org context. |
| `org-edge-evidence` | Selected aggregate edge and paged evidence drawer show source facts/rule version. |
| `org-zero-degree-agent` | Inactive member is visible/searchable without fabricated edges. |
| `org-empty` | Honest empty organization state, with no prompt requiring a query. |
| `org-error-retry` | Failed page/evidence fetch preserves already loaded graph and offers retry. |
| `org-reduced-motion` | Settled deterministic layout and equivalent keyboard/list access. |

Baseline metadata is in
`docs/design/fixtures/collaboration-organization-graph-v1.json`; screenshots are
owned by the Web implementation node because this contract node intentionally
does not implement UI.

## 8. Compatibility and ownership boundaries

The existing `collaboration-effects` endpoint remains available for exact
task/effect inspection. The new page may call it only for focus/evidence drilldown.
It must not use repeated task-scoped requests to assemble the organization graph.

T2109 owns server aggregation, cursor implementation, authz, and production tests.
T2110 owns Web rendering, layout implementation, accessibility, and screenshots.
Both consume this document, the DTO, and the validated fixture. Neither may
change relation polarity rules, PM state, or audit facts. Contract conflicts fail
closed and are raised to the plan owner; consumers must not create a second DTO.

## 9. Independent acceptance gates

* An unactioned initial GET (no filters) returns a non-empty fixture graph.
* The graph contains agent/plan/task nodes and all four edge kinds, including a
  floating task and a zero-degree agent.
* Aggregate counts reconcile to evidence membership; dangling ids and fabricated
  evidence fail validation.
* Cursor pages are snapshot-consistent, deterministic, non-overlapping, and
  terminate; malformed or cross-organization cursors fail closed.
* Organization authz is enforced before projection access; `project_id` cannot
  escape `{slug}`.
* Overview, progressive, focus, evidence, empty, error, mobile, reduced-motion,
  keyboard, and list-alternative states satisfy §7.
* No LLM dependency or write to PM/Conversation is introduced.
* Run `node docs/design/fixtures/validate-collaboration-organization-graph.mjs`.
  The frozen fixture must report 48 agents, 12 plans, 132 tasks, 192 nodes, all
  four edge kinds, all screenshot scenarios, and zero failures.
