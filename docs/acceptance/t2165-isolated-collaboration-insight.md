# T2165 isolated Collaboration Insight acceptance

## Provenance and isolation

- Baseline: `origin/main@b942af983755639d74b24c5f41bfb47a523b749c`.
- Runtime candidate used for the red/green upgrade: `cd209a0bd42d86f3456e66c979f29033a3b4ed74`.
- Final delivery SHA is recorded in the task delivery because this report and
  the fresh-main lint repair are committed after the runtime probe.
- Instance purpose: T2165 production-isomorphic acceptance only.
- Instance: `t2165`, installed by the production `install test-instance` path.
- Web origin: `http://127.0.0.1:57780`; server/admin ports: `57781`/`57782`.
- Install/data root: `~/.agent-center-test/t2165`; isolated SQLite and DuckDB,
  runtime worker identity, launchd labels, seeded organization, session cookie,
  and test entities. No production instance, dev server, mock server, or manual
  projection writes were used.
- Seeded organization/project: `org-522d84ab` / `project-27d004ce`.
- The runtime candidate was rebuilt with `make build`, upgraded through the production
  `upgrade center` path, and the same instance was restarted in place. Its
  health endpoint reported `fix/t2165-collaboration-insight-cd209a0b`.

## Before/after loading evidence

The same URL and instance were probed before and after the upgrade.

| Probe | Baseline | Candidate |
|---|---|---|
| missing `/assets/InsightCollaboration-stale.js` | `200 text/html`, SPA index body, `Cache-Control: no-cache` | `404 text/plain`, no SPA index |
| real hashed Collaboration chunk | `200 text/javascript`, no explicit immutable policy | `200 text/javascript`, `Cache-Control: public, max-age=31536000, immutable` |
| direct `/signin` refresh | `200 text/html`, `Cache-Control: no-cache` | `200 text/html`, `Cache-Control: no-cache` |

The baseline response reproduced the stale-manifest failure mechanism: a
module request silently received HTML. The candidate makes asset misses fail
honestly while retaining the SPA fallback for direct client routes.

## Authentication and organization graph

The seeded owner signed in through the real `/signin` UI. Successful auth
performed a full-page reload and landed at
`/organizations/org-522d84ab/projects`; a later direct navigation/refresh of
`/organizations/org-522d84ab/insights/collaboration` remained authenticated.
There were no page errors, console errors, unexpected 401s, or dynamic-import
failures.

Test data was created only through authenticated production HTTP entry points:
one worker, two runtime agents, one Plan, three Tasks, Plan membership, a Task
dependency, assignment, and reassignment events. The default URL had no query
parameters and rendered a non-empty SVG graph containing:

- two Agent nodes;
- Agent-Task and Agent-Plan structural edges;
- Plan-Task and Task dependency structure;
- a mixed reassignment effect edge with one source evidence event.

The browser loaded `InsightCollaboration-BxjfcLlA.js` plus its dependent chunks
successfully. The organization graph API completed in 5 ms in the captured
resource timing. A `lod=cluster&max_nodes=3` production query returned HTTP 200,
`lod=cluster`, three nodes, one cluster, and `truncated=true`.

## Interaction checks

- Project search selected Alpha and added `project_id`; Clear filters restored
  the parameter-free organization graph.
- Zoom in/out, Fit, Reset, node focus, and neighbor-focus reset remained
  operational on the deployed candidate.
- Clicking the aggregated reassignment edge opened the Evidence drawer and
  returned the real source audit event.
- Full-page screenshots were captured for the organization graph, Agent focus,
  and Evidence drawer. Raw before/after HTTP headers and bodies are under the
  task-local `evidence/` directory and are intentionally not committed.

## Automated gates

- `go test ./internal/webconsole/spa -v`: pass (8 tests).
- `go test ./internal/webconsole/spa ./internal/webconsole/api ./internal/observability/collaborationeffect`: pass.
- targeted Web auth/client/Collaboration suite: 3 files, 46 tests passed.
- `make test`: pass, including deployed/integration packages.
- `pnpm test`: 200 files, 1920 tests passed.
- `make lint`: the fresh-main baseline first exposed a new forbidden checkbox
  in `WorkItemFilterBar`; the candidate converts the boolean control to the
  required `role="switch"`. Its focused test and `pnpm lint` pass. The final
  composite lint is rerun after this report commit and recorded in delivery.
- `make build`: pass for the runtime candidate; final delivery SHA build and
  deployed health are recorded in delivery.
