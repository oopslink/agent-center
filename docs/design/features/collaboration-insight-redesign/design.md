# Collaboration Insight Dimensional Graph Redesign

## External Practice Baseline

- Neo4j Bloom frames graph exploration through business Perspectives and
  search-to-visualization rather than exposing one raw schema graph to every
  user. Neo4j describes Bloom as a graph exploration app that presents a
  business view of the graph, and Enterprise Studio emphasizes visual
  exploration, point-and-click navigation, graph algorithms, and shared scenes:
  https://neo4j.com/docs/bloom-user-guide/current/ and
  https://neo4j.com/product/enterprise-studio/.
- Cytoscape.js provides mature app-level graph interactions, layouts, gestures,
  selectors, graph algorithms, and performance controls. Its docs call out
  layout choices, built-in gestures, selectors, and label options such as
  `min-zoomed-font-size`: https://js.cytoscape.org/.
- Sigma.js separates graph data from WebGL rendering, while Graphology supplies a
  typed JS graph object with algorithms, layouts, traversals, and events.
  Sigma's renderer docs detail WebGL node/edge programs and picking; event docs
  cover node, edge, and stage interactions:
  https://www.sigmajs.org/docs/advanced/renderers/,
  https://www.sigmajs.org/docs/advanced/events/, and
  https://graphology.github.io/.

Principles taken from the comparison:

- start from the user's graph question, not the complete data model;
- preserve a stable graph model under multiple Perspectives/views;
- make search, expansion, focus, and Evidence first-class;
- precompute or cache expensive layouts;
- keep labels and edges conditional on zoom, focus, and selection.

## Information Architecture

Use three switchable views. They share the same top-level time, Project, Agent,
and search context. Switching views keeps selected context when the selected
entity exists in the new view; otherwise the UI keeps a breadcrumb chip and
offers `Restore global view`.

| View | Primary question | Default data projection | Default layout | Default first screen |
|---|---|---|---|---|
| Collaboration network | Who collaborates or contacts each other closely? | `Agent <-> Agent` edges aggregated from handoff, review, co-work, unblock, dependency release contributors | Community/force with cluster hulls; ego radial when an Agent is selected | Agent communities, top bridge agents, strongest recent ties |
| Task impact | Who is pushing or blocking what? | `Agent -> Task` and optional `Task -> Task` dependency context; edge polarity and Evidence count retained | Radial ego from selected Agent/Task; bipartite lanes for global Project | Active blocked/pushed tasks grouped by project or assignee |
| Plan path | Where is the flow stuck? | `Plan -> Stage -> Task` containment plus `Task -> Task` dependencies and gate/review status | Hierarchical DAG; stage swimlanes alternative | Critical path with blocked/review/waiting nodes highlighted |

Tradeoff: keeping exactly three views avoids the old "everything graph" and maps
to three recurring operator tasks. A fourth "Project map" was considered but
rejected for MVP because Project is better handled as a shared filter and cluster
dimension; adding it would encourage organization-scale hairballs again.

## Shared Interaction Model

- Global filter bar: time window, Project, Agent, relation/polarity advanced
  filters, search. Clear filters restores the current dimension's global view.
- View switcher: tab control with URL param `view=network|impact|lineage`.
- Context chip: selected `agent_ref`, `task_id`, `plan_id`, or edge scope
  persists across view switch when possible.
- Search locate: type-ahead returns nodes and edges; Enter centers the best hit;
  Cmd/Ctrl+Enter opens the Evidence drawer when the hit has effect scopes.
- Focus/restore: focus hides non-neighborhood labels and fades non-neighborhood
  edges; restore clears focus but retains global filters.
- Expand/collapse: selected Agent/Task/Stage can expand one-hop, two-hop, or
  collapse neighborhood. Server-backed expansion must preserve the selected
  `graph_version` or return a changed-version banner.
- Drag pinning: dragged nodes become locally fixed and saved per user/view/filter
  hash. `Reset layout` clears pins.
- Navigation: mini-map for 500+ rendered elements; for 100 nodes a fit/zoom
  control is enough, but the mini-map can remain visible without harming usage.
- Keyboard: Tab enters controls and graph list, arrow keys pan, `+/-` zoom,
  Enter opens node/edge detail, Escape restores graph context, `e` opens
  Evidence for selected effect scopes.

## Visual Semantics

- Color has limited meaning: node kind uses color; edge state uses stroke
  treatment. Do not encode more than one fact in color alone.
- Node shapes: Agent circle, Task square, Plan rounded square, Stage hex/diamond
  or narrow rectangle, cluster pill/hull.
- Edge treatment: positive/progress solid green, blocking dashed red,
  dependency/containment dotted gray, review mixed double-stroke or segmented
  red/green. Directed edges use arrows only where direction answers the view
  question.
- Default graph: render low-opacity non-critical edges, no full edge labels, and
  only labels for selected, searched, high-centrality, blocked, or cluster nodes.
- Hover/select: strengthen adjacent edges, show edge label, magnitude,
  interaction count, last occurrence, and Evidence count in a side detail panel.
- Evidence: drawer remains the single down-drill; it shows contributing effects,
  scoped project IDs, before/after state, and source events. The redesign must
  never rewrite attribution, relation, polarity, or evidence IDs.

## Data/API Impact

Current endpoint can support MVP by client-side dimensional adapters, but 2k+
graphs need server support.

Add optional query params:

| Param | Values | Purpose |
|---|---|---|
| `view` | `network`, `impact`, `lineage` | Server returns view-specific graph projection and summary. |
| `layout_hint` | `community`, `ego`, `radial`, `dag`, `swimlane` | Chooses cached/precomputed coordinates when available. |
| `focus_id` | node id or entity id | Return bounded neighborhood around focus. |
| `depth` | `1`, `2`, `3` | Controls expansion. Default 1 for focus; not allowed unbounded. |
| `aggregate_by` | `project`, `stage`, `agent_cluster`, `none` | Controls cluster nodes and edge aggregation. |
| `include_coordinates` | `true|false` | Allows cached layout reuse across pages and filters. |

Response additions:

```ts
interface CollaborationGraphResponse {
  graph: {
    nodes: CollaborationNode[];
    edges: CollaborationEdge[];
    clusters: CollaborationNode[];
    view: 'network' | 'impact' | 'lineage';
    layout: 'community' | 'ego' | 'radial' | 'dag' | 'swimlane';
    coordinates_version: string;
    bounds: { x: number; y: number; width: number; height: number };
    truncated: boolean;
    lod: 'full' | 'cluster';
  };
  summaries: {
    network?: { agent_count: number; tie_count: number; bridge_agents: string[] };
    impact?: { pushed_tasks: number; blocked_tasks: number; review_tasks: number };
    lineage?: { blocked_stages: number; critical_path_length: number; waiting_edges: number };
  };
}
```

No existing fields are removed. Existing `effect_scopes`, `effect_ids`,
`evidence_event_ids`, `before_state`, `after_state`, and `explanation_key` are
required unchanged.

## Performance Plan

| Scale | Rendering strategy | Layout strategy | Budget |
|---|---|---|---|
| 100 nodes/edges | React + SVG or Canvas both acceptable; show graph and detail together | Client layout allowed under 30 ms; cache after first layout | first interactive <= 800 ms, pan/zoom p95 <= 16 ms, memory <= 120 MB |
| 500 nodes/edges | Canvas or Cytoscape Canvas; SVG only for overlays/detail list | Worker layout, viewport culling, label LOD, edge labels on hover/select | first interactive <= 1.5 s, pan/zoom p95 <= 24 ms, memory <= 180 MB |
| 2k+ nodes/edges | Sigma.js WebGL preferred; Canvas fallback only for clustered/viewport slice | Server precomputed coordinates + Worker refinement; progressive load and aggregation | first interactive <= 2.5 s for clustered/global, pan/zoom p95 <= 40 ms, memory <= 350 MB |

Techniques required before 2k+ release:

- precompute stable org/project layouts keyed by `(org_id, view, filter hash,
  rule_version, graph_version)`;
- Web Worker for force/community, radial packing, edge aggregation, and culling;
- viewport culling for labels and non-selected edges;
- LOD: cluster labels at zoom < .6, important node labels at .6-.9, local node
  labels at > .9, edge labels only on hover/select;
- aggregate parallel edges by semantic key and show count/magnitude/Evidence
  badges;
- interaction downgrade: while panning/zooming/dragging, draw edges simplified,
  hide edge arrows/labels, suspend hull recomputation, then refine after idle;
- progressive loading: first render clusters/top-K critical nodes, then expand
  visible neighborhoods and selected contexts;
- optional edge bundling/aggregation for dense agent communities and dependency
  chains; never bundle selected or Evidence-open edges.

## Renderer Evaluation

| Option | Keep/reuse | Cost | Decision |
|---|---|---|---|
| Current React SVG | Existing tests, Evidence drawer, filters, small graph accessibility | DOM count and React render cost scale poorly; no real layout engine; full labels noisy | Keep only for small fallback and hidden accessible edge list. Do not build the redesign on it. |
| Cytoscape.js Canvas | Mature gestures, selectors, layouts, extensions, accessibility can be paired with list | Bundle/API integration, layout tuning, Canvas text quality at large scale | Good 100/500 implementation choice and lower migration risk. |
| Sigma.js + Graphology WebGL | Best fit for 2k+ pan/zoom, typed graph object, WebGL picking, reducers/custom programs | More custom UI for layouts, labels, minimap, accessible list, Evidence binding | Recommended production renderer for 2k+ path after an adapter spike. |

Recommendation: implement the dimensional view-model and Evidence/control shell
first behind the current endpoint. Use Cytoscape.js for an MVP if the team needs
speed at 100/500. Run a Sigma/Graphology spike against frozen fixtures before
enabling 2k+ global graphs. The ADR freezes this as a staged renderer decision,
not a blind library swap.

## Minimum Implementation Split

1. IA shell: view tabs, shared filters, URL state, persistent selected context,
   clear-filter restore, existing Evidence drawer retained.
2. View adapters: derive `network`, `impact`, `lineage` graphs from existing
   response; add per-view summary cards and graph questions.
3. Interaction layer: search locate, focus/restore, expand/collapse, mini-map,
   keyboard controls, selected-neighborhood dimming, label LOD.
4. Performance layer: Worker layout/aggregation, culling, panning downgrade,
   measurement harness in CI fixtures.
5. API extension: add `view`, `layout_hint`, `focus_id`, `depth`,
   `aggregate_by`, coordinates, and per-view summaries; keep old fields.
6. Renderer hardening: choose Cytoscape or Sigma based on measured fixtures;
   retain accessible edge/node list and Evidence contract tests.

## Acceptance Criteria

- Default first screen renders only Collaboration network, not all dimensions.
- Each view answers exactly one primary question and has its own default layout.
- Project, Agent, and Window filters change the actual node/edge set; clearing
  filters restores the active dimension's global graph.
- Switching view preserves compatible time/Project/Agent filters, selected
  context, viewport, and return context. Incompatible selected context is
  explained visibly before it is cleared.
- Search locate, real node/edge hit-testing, hover/select, selection-driven
  focus, restore, one-hop/two-hop expand/collapse, drag pinning, mini-map
  navigation, and keyboard operations are usable.
- Node drag and canvas pan are separate interactions. Dragging a node pins it;
  reset/unpin clears local pin state without changing server facts.
- Default graph shows bounded labels; hover/select reveals related edge labels
  and fades unrelated edges.
- Evidence drawer opens from graph selection, list selection, and keyboard, and
  loads by the selected edge's existing effect/project scopes. It displays
  relation, effect/polarity, direction, occurrence time, and evidence event IDs.
- 100/500/2k+ fixture measurements record current DOM/SVG baseline and prototype
  first interactive time, idle render, continuous pan, wheel zoom, node drag,
  view switching, filter response, rendered element counts, and heap memory.
- Implementation states which code is reused, which API fields are additive, and
  confirms no attribution facts are rewritten.
