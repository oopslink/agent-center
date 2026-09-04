# Collaboration Graph LOD Performance Gate

Date: 2026-09-04
Base: `1ba42c24ac613d23bcfa5a0c14ae1ca121032bed`

## Fixture and method

The deterministic production-scale fixture contains 80 agents, 1,000 tasks,
1,000 semantic collaboration edges, and 1,000 effects. Both measurements use
the same Vitest/jsdom environment and measure from render start until the graph
is queryable. DOM counts are captured after that first usable render.

## Before and after

| Metric | Base `origin/main` | LOD implementation |
| --- | ---: | ---: |
| First usable graph render | 587 ms | 211 ms |
| SVG lines | 1,000 | 151 |
| SVG groups | 2,080 | 173 |
| Keyboard edge buttons | 1,000 | 80 |
| Timeline items | 1,000 | 120 |

The isolated baseline test took 6.1 s including matcher traversal and cleanup;
the optimized test took 331 ms. Timing is machine-local and the committed gate
uses the less brittle first-usable-render threshold of 1,000 ms plus exact DOM
ceilings.

## Remediation

- Keeps the organization graph as the default scope; project, plan, task,
  agent, relationship, and time remain optional narrowing filters.
- Keeps the existing cursor accumulation and semantic cross-page edge merge,
  including `effect_scopes` used for fail-closed cross-project evidence fetches.
- Raises each cursor batch from 100 to 200, below the backend limit, and keeps
  the explicit Load more state.
- Defers large layout inputs so urgent controls and filter changes can paint
  first and obsolete layouts can be discarded by React.
- Uses deterministic five-lane layout, a vertical pan/zoom viewport, label LOD,
  and viewport culling.
- Caps visible SVG edges at 220, keyboard edge buttons at 80, and timeline
  entries at 120. Every cap has an explicit rendered/total disclosure.

## Gate

`web/src/pages/InsightCollaboration.test.tsx` checks the 1,000-effect fixture is
usable in under 1,000 ms, SVG edge and group ceilings hold, accessible edges
remain keyboard buttons, timeline entries are bounded, and all truncation
messages disclose totals.
