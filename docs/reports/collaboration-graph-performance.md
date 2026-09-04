# Collaboration Graph Performance Remediation

Date: 2026-09-04

## Baseline

The isolated executor has no production database or agent-center access, so the
baseline uses the current production DTO contract with a deterministic
production-scale fixture: 1 project, 80 agents, 80 tasks, 1,000 collaboration
effects/edges.

Previous `InsightCollaboration` rendering behavior for that same response:

- API scope: required `project_id` and `task_id`; fixed `limit=100`; no next-page
  fetch UI.
- SVG graph: one `<line>` and one `<text>` for every edge, plus every node.
- Keyboard edge list: one `<button>` for every edge.
- Timeline: one list item for every effect.
- Estimated first render DOM pressure for 1,000 effects: 1,000 graph lines,
  1,000 graph labels, 160 nodes, 1,000 edge buttons, 1,000 timeline items before
  container/metadata wrappers.

## Remediation

- Removed the task requirement from page loading. Project-level graph is now the
  default; task remains an optional filter.
- Added React Query infinite cursor loading over the existing backend
  `next_cursor` contract with an explicit "Load more" control.
- Raised the first page to 200 effects while staying below the backend
  `MaxQueryLimit=500` guard.
- Added graph LOD:
  - deterministic column layout split by agents/tasks;
  - edge aggregation by source, target, relation, and polarity;
  - vertical viewport clipping with pan/zoom controls;
  - label suppression for large graphs or zoomed-out views;
  - hard caps of 220 visible SVG edges, 80 keyboard edge buttons, and 120
    timeline entries per loaded window.
- Evidence requests now include `project_id`, matching the fail-closed backend
  evidence contract.

## Performance Gate

`web/src/pages/InsightCollaboration.test.tsx` includes a 1,000-effect gate:

- first render must finish in less than 1,000 ms under jsdom;
- rendered SVG edges must be non-empty and no more than 220;
- keyboard edge buttons must be exactly 80;
- timeline must truncate to 120 entries;
- LOD status must disclose rendered/total graph counts.

Observed local run:

- `pnpm exec vitest run src/pages/InsightCollaboration.test.tsx --reporter verbose`
  passed: 7 tests, 1 file, duration 1.72s.
- The production-scale case passed the `<1000 ms` interaction-readiness gate.

