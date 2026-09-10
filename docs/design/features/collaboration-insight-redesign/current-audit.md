# Current Collaboration Insight Audit

## Scope

Audited `web/src/pages/InsightCollaboration.tsx`,
`web/src/api/insights.ts`, `web/src/pages/InsightCollaboration.test.tsx`, and
the server/client route contract on `origin/main@512d1181264a706155266fa658566146b2ae58c6`.

## What Works Today

- The page uses the real `GET /api/orgs/:slug/insights/collaboration-effects`
  projection and keeps Evidence down-drill via
  `/collaboration-effects/:effect_id/evidence`.
- The client preserves `effect_scopes` so cross-project agent-agent edges can
  load Evidence with each contributing `project_id`.
- Cursor pages are accumulated and semantic edges are merged by source, target,
  relation, and polarity.
- The current SVG graph has basic pan, zoom, node focus, drag positioning,
  cluster fallback, selected-neighborhood dimming, and keyboard activation.

## Product Problems

1. The page still asks one visual surface to carry agent-agent collaboration,
   agent-task causality, plan/stage/task containment, dependency release, review
   outcomes, and Evidence entry. Users see an all-purpose graph before choosing a
   question.
2. Summary cards are polarity counts, not task-oriented answers. They do not
   tell the user who is central, which tasks are blocked, or which stage is the
   bottleneck.
3. The layout is hard-coded into entity lanes (`agent`, `project`, `plan`,
   `stage`, `task`). This is readable for containment, but poor for dense
   agent-agent networks and weak for dependency DAGs.
4. Edge labels are drawn for active/default edges in the SVG layer. On larger
   graphs, labels compete with edges and become the dominant noise.
5. Search is implemented through filters but not as graph-local locate/focus.
   Users must already know exact Project/Plan/Task/Agent values.
6. Evidence is preserved but visually tied to edge-list buttons below the graph;
   the graph itself does not provide enough progressive disclosure before the
   drawer opens.

## Rendering Bottlenecks

| Area | Current behavior | Risk |
|---|---|---|
| DOM/SVG element count | One React element group per edge and per node, plus text labels | 500+ elements remains workable, but 2k nodes plus 2k edges produce thousands of retained DOM nodes and React diff work. |
| Layout | Calculated synchronously in React render by lane index | Fast, but only because it is simplistic; better layouts would block the main thread if added inline. |
| Labels | SVG text generated in normal render flow | Labels cost layout/paint and create clutter before they create insight. |
| Aggregation | Cursor accumulation and edge merge happen on main thread in `useMemo` | Acceptable at 100/500; needs Worker or server pre-aggregation at 2k+. |
| Culling | SVG renders all current edges/nodes even when zoomed or panned away | Pan/zoom cost scales with graph size rather than visible graph size. |
| Interaction | Wheel handler is attached to the SVG and window | Useful for current control, but global wheel handling can steal scroll outside the graph. |

## Reusable Pieces

- `CollaborationEffect`, `CollaborationEdge`, `CollaborationNode`,
  `CollaborationEffectScope`, Evidence bundle query, and fail-closed project
  scoping should be retained.
- Existing filters (`project_id`, `plan_id`, `task_id`, `agent_ref`,
  `relation_type`, `polarity`, `since`, `until`) are still valid as shared
  global context.
- Existing tests around Evidence de-dupe, cross-project scopes, cursor
  accumulation, empty/403/500 states, and clear-filter restore remain contract
  tests for any implementation.

## Replace or Extend

- Replace the single `accumulateGraph -> readableGraph -> SVG lanes` rendering
  path with a view-model pipeline:
  `projection response -> dimensional adapter -> layout cache -> renderer`.
- Extend API/view parameters with `view`, `layout_hint`, `focus_id`, `depth`,
  and `aggregate_by`; do not change or reclassify facts.
- Move non-trivial layout and graph aggregation out of React render: server cache
  for stable/global layouts, Web Worker for local filtered/focus layouts.

## Baseline Evidence Added

`evidence/raw/current-page-baseline.json` records the current source-level
baseline used for the redesign comparison on this replay branch:

- renderer: React-owned SVG with one rendered group per returned edge and node;
- data path: existing `/collaboration-effects` pages accumulated on the main
  thread;
- interaction retained: current Evidence drawer and selected effect scopes;
- missing product boundary: no `view` parameter and no dimensional first screen.
