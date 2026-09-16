# I166 reproducible staged-DAG acceptance harness

This harness runs the I166 candidate as a production-built frontend embedded in
the real backend binary. It creates all test state in a fresh temporary SQLite
database and drives the UI with Chromium. It does not read an installed
agent-center configuration, database, admin socket, runtime configuration, or
credential.

## One-command run

From the repository root:

```sh
./scripts/acceptance/i166-repro.sh
```

The wrapper verifies that product commit
`724c9b8660c1c269bf6e05913f2815d97828d40b` is an ancestor of the checkout,
builds the production SPA and backend, installs the pinned Playwright dependency
and Chromium, then runs the real-chain browser verification. Set
`I166_SKIP_BROWSER_INSTALL=1` only when the matching Playwright Chromium is
already installed.

## Fixture topology

The test creates a fresh organization, owner identity, project, and these plans:

- A normal branch/join orchestration graph.
- A two-stage graph with two authored tasks in each stage. Each stage has a
  within-stage dependency. Stage B depends on Stage A, which produces the real
  gate-to-entry barrier in the orchestration graph.
- A legacy single-node plan and an empty plan for regression coverage.

Identity/project/task/plan setup goes through the real HTTP service. The
test-only `tests/harness/i166stagefixture` command opens only the harness-created
database and calls the Project Manager application service for `CreateStage`,
`AssignTaskToStage`, and `AddPlanDependency`. It neither inserts rows directly
nor mocks the graph response. Both staged and ordinary plans are started through
the real HTTP service before the browser reads them.

## Isolation, provenance, and cleanup

- Data directory: a new `mkdtemp` directory per run.
- Ports: two independently allocated loopback-only ports.
- Identity: a new randomized test identity and organization per run.
- Secrets: a random harness-local master key with mode `0600`.
- Provenance: `docs/acceptance/i166-evidence/real-chain-results.json` records
  `product_sha`, `harness_sha`, ports, fixture IDs, API counts, DOM counts, and
  every pass/fail check.
- Cleanup: the server receives `SIGTERM` (then `SIGKILL` if needed), Chromium is
  closed, and the temporary directory is recursively removed in `finally`.
  Set `I166_KEEP_TEMP=1` only for local debugging; remove the recorded
  `provenance.temp_dir` manually afterward.

Evidence is regenerated under `docs/acceptance/i166-evidence/`. The command exits
non-zero if any API/DOM/topology check fails.
