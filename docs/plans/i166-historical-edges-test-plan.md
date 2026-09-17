# I166 cancelled-node historical dependencies

Owner request: retain original topology when a task is cancelled; show × on the
cancelled node and mark its original dependencies inactive.

Display-only recovery reads the selected generation and its parent chain. It maps
snapshot task IDs onto visible nodes, restores missing incident edges for historical
nodes, deduplicates, and never mutates snapshots or execution dependencies. Current
views use the same recovery when history is expanded. Future generations and hidden
endpoints must not leak in. Without an ancestor snapshot, no edge is invented.

## Checks

1. Original review → integration → release topology survives cancellation; edges
   are inactive, cancelled cards show ×, and snapshots/dependencies remain unchanged.
2. Earlier revisions do not display future edges or later cancellation styling.
3. Collapsing history removes historical endpoints and their edges; current edges
   and missing dependencies between effective tasks are not rewritten.
4. Duplicate, partial and cyclic ancestry is safe; edge kinds are retained.
5. Real ELK adapter and legacy fallback both preserve historical edge metadata.
6. Browser fixture: historical/current-with-history views, light/dark themes;
   real SVG paths are grey dashed with visible inactive labels, cards remain connected.
7. Full frontend tests, make lint, make build.

Browser fixture uses in-memory sample API responses and no center connection.
It verifies presentation, not production deployment or scheduler behavior.
