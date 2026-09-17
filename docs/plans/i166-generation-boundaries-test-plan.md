# I166 generation changes and historical flow boundaries

Owner requested all three screenshot findings together: explain a generation with
no new task cards, retain cancelled tasks' topology, and show Start/End in history.

Design: task-origin R labels remain unchanged. The selected revision shows its
node decisions separately, in that revision's colour, both in the summary and on
affected task cards. Readable focus includes those affected tasks. Historical
snapshots gain presentation-only Start/End controls; their effective forward roots
and leaves exclude historical and lineage edges. Cancelled cards stay on their
recorded historical chain (018e9f56). Controls are not tasks or completion signals.

Checks:
1. R3 cancellation-only summary names T2215/T2216; cards retain R1 and show R3
   cancellation badges. Earlier revisions have no future decision badges.
2. Restored grey inactive dependencies remain visible; collapsed current history
   and immutable input/readiness semantics regressions remain green.
3. Historical Start/End connect effective roots/leaves; cancelled cards do not
   create terminal dependencies. Empty and all-cancelled snapshots are safe.
4. Staged graph/layout and legacy fallback regressions pass.
5. Browser sample: light/dark and English/Chinese, readable focus and mobile summary.
6. Full frontend tests, make lint and make build pass.

The browser sample is isolated frontend data, never production state or an end-to-end
scheduler/deployment test. No plan generations or task states are changed.
