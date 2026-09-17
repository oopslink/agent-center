# I166 historical dependency regression report

Scope: cancelled task cards keep their recorded incoming/outgoing topology in
historical revisions and the expanded current-history view. UI-only recovery from
immutable ancestor snapshots; no scheduler, generation snapshot, or production
state mutation. Cancelled nodes show ×; recovered/retired edges are grey dashed and
labelled Inactive (已失效), and do not expose dependency deletion controls.

Corresponding plan: `docs/plans/i166-historical-edges-test-plan.md`.

| Check | Result |
| --- | --- |
| 1: cancelled chain, immutability | PASS — unit and browser fixture |
| 2: no future leakage / earlier revision | PASS — ancestor-only test and browser revision switching |
| 3: hidden endpoints / effective edges | PASS — unit test and current view browser inspection |
| 4: deduplication / partial / cyclic / edge kind | PASS — unit tests |
| 5: ELK / legacy metadata | PASS — real ELK layout tests for both adapters |
| 6: light/dark actual rendering | PASS — history.png and current-dark.png; actual SVG path computed grey stroke and 3px/4px dash pattern, inactive labels visible |
| 7: test/lint/build | PASS — 202 files / 1,951 frontend tests; make lint and make build exit 0 |

The first full test run exposed an asynchronous test assumption: the DAG container
exists before ELK produces nodes. Connection tests now wait for the target node
before interaction; no assertions or interaction checks were removed.

Browser evidence uses a frontend sample fixture, not production center data. Copy
fixture.tsx to web/i166-history.tsx, serve an HTML root with that module using Vite
(and optimizeDeps esbuild target es2022, as in the existing readability fixture).
The sample contains original review → integration → release dependencies, then a
revision cancelling integration/release and omitting those execution edges.

Historical START/END creation and the pure-cancellation generation summary are
outside this focused fix. Missing ancestor data cannot recover an unknown edge.
