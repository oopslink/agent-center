# 0061. Collaboration Insight Uses Dimensional Graph Views

| Field | Value |
|---|---|
| Status | Proposed |
| Date | 2026-09-09 |

## Context

The current Collaboration Insight page projects CollaborationEffect facts into
one mixed graph. Owner feedback says the graph is noisy, rigid, and slow, and
explicitly allows splitting dimensions instead of forcing every relationship into
one view.

ADR 0060 already freezes CollaborationEffect as an Observability read model, not
a new business fact. This ADR changes presentation, layout, API projection, and
rendering strategy only.

## Decision

Collaboration Insight will expose three task-focused graph views:

- Collaboration network: `Agent <-> Agent`, for tight ties and communities.
- Task impact: `Agent -> Task/Plan context`, for push/block/review effects.
- Plan path: `Plan -> Stage -> Task` plus task dependencies, for bottlenecks.

The active view is selected by `view=network|impact|lineage`. Time, Project,
Agent, relation/polarity filters, selected context, and Evidence drill-down are
shared across views.

Filters are part of the graph projection contract, not cosmetic controls:
Project, Agent, and Window changes must alter the node/edge set when matching
facts exist, and clearing filters restores the active dimension's global graph.
Switching among the three views must preserve compatible filters, selected
objects, viewport, and return context. When a selected node or edge cannot exist
in the target view, the UI must explain that incompatibility in visible copy
before clearing the object.

The renderer must support per-view layout families: community/force and ego
radial for the network, radial or bipartite lanes for impact, and hierarchical
DAG or stage swimlanes for lineage. Full labels and full edge detail are not
rendered by default; they appear through zoom LOD, hover, selection, or Evidence.
Hit-testing is a renderer responsibility: hover/select must resolve actual
nodes and edges, Focus must act on the user's selected node, expand/collapse
must change the selected node's real neighborhood, and node drag must be
separate from canvas pan. Dragged nodes become pinned until the user unpins or
resets layout.

Evidence opens from selected real edge scopes only. The drawer must display the
selected edge's relation, effect/polarity, direction, occurrence time, scoped
effect ID/project ID, and evidence event IDs. It must not fall back to a global
demo fixture.

For implementation, build a view-model adapter boundary first. Current SVG may
remain as a small-graph fallback, Cytoscape.js Canvas is acceptable for 100/500
element MVP, and Sigma.js + Graphology is the recommended candidate for 2k+
interactive graphs after fixture measurement. The decision is conditional on
measured budgets, not library branding.

## Consequences

- Users choose a question before seeing dense relationships.
- The same CollaborationEffect facts can appear in different view projections
  without changing attribution, polarity, relation type, or Evidence.
- API changes are additive and can be introduced while the current endpoint still
  serves old clients.
- Layout cache and Worker paths become part of the product contract for 500+
  element views.
- The design candidate is frozen with a repeatable benchmark harness comparing
  the current DOM/SVG page baseline and the prototype path at 100, 500, and
  2k+ requested scale.

## Alternatives Considered

### Continue with one graph and add more controls

Rejected. It leaves the primary information architecture problem intact and
keeps making one visual layout carry incompatible graph structures.

### Replace the graph with tables only

Rejected. Tables are useful for accessible detail and Evidence, but they do not
answer network community, dependency path, or bottleneck questions as directly as
graph layouts.

### Move to Sigma.js immediately for all scales

Rejected for MVP sequencing. WebGL is the likely 2k+ answer, but the main risk is
the dimensional view model and Evidence interaction contract. Those should be
stabilized before a full renderer migration.
