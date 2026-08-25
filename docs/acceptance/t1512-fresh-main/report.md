# T1512 Fresh Evidence Report

Verdict: BLOCKED, not PASS.

The required code object was verified from the live remote:

- `origin/main` resolved to `1a8a55e9e9b862c15c05300689c1b08cd328df7d`.
- Candidate `2b531b9d5a81a7857b707ed425886b2d2ac96774` is an ancestor of that `origin/main`.
- Merge point `976aa24f0f60e85ffc77b82d26b0410482ec4d94` is an ancestor of that `origin/main`.

Fresh build did not produce a deployable product binary. `make build` failed during `web` `tsc -b`:

```text
src/pages/Access.test.tsx(411,25): error TS2339: Property 'permission_keys' does not exist on type 'never'.
src/pages/Access.test.tsx(412,25): error TS2339: Property 'resources' does not exist on type 'never'.
make: *** [build-frontend] Error 2
```

Raw log: `docs/acceptance/t1512-fresh-main/raw/make-build.log`.

Because the fresh build failed before `bin/agent-center` was created, I did not start a service, did not bind independent ports, and did not collect UI/MCP/HTTP screenshots. Reporting PASS would violate the task's evidence-only rule.

## Provenance

- First remote fresh clone path: `.runtime-evidence/t1512-20260825T063958Z/fresh-clone`.
- Remote partial clone succeeded, but checkout/blob restore made no progress for several minutes and was interrupted.
- Fallback isolated clone path: `.runtime-evidence/t1512-20260825T063958Z/fresh-clone-local-064920`.
- Fallback clone re-fetched `origin main`, verified exact SHA, and checked out detached `1a8a55e9e9b862c15c05300689c1b08cd328df7d`.
- Toolchain observed: Node `v25.6.0`, pnpm `10.31.0`, Go `go1.26.3 darwin/arm64`.

## Checklist

| Requirement | Status | Evidence |
| --- | --- | --- |
| Live `origin/main` exact SHA verified | PASS | `verdict.json` |
| `2b531b9d...` ancestor of main | PASS | `verdict.json` |
| `976aa24f...` ancestor of main | PASS | `verdict.json` |
| Fresh clone/build | FAIL | `raw/make-build.log` |
| Independent data directory and ports | NOT RUN | Build failed first |
| Real HTTP/MCP/UI deployment | NOT RUN | Build failed first |
| Fresh browser product screenshots | NOT RUN | Build failed first |
| RAM Roles System/Custom, Managed/Internal | NOT RUN | Build failed first |
| Direct binding default no resource | NOT RUN | Build failed first |
| Permission/resource-kind linkage | NOT RUN | Build failed first |
| No visible `role-access-*` proliferation | NOT RUN | Build failed first |
| Console/network capture | NOT RUN | Build failed first |
| Product code changes | PASS | No product code changed |
