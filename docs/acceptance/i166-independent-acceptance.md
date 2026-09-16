# I166 independent staged-DAG acceptance

Verdict: **PASS**

This run independently verified the delivered harness at
`6060270a30906beec0802712935747fffd967727`. The product candidate under test was
`724c9b8660c1c269bf6e05913f2815d97828d40b`, which was confirmed to be an
ancestor of the harness revision.

## Reproduction

```sh
git checkout test/i166-independent-acceptance
./scripts/acceptance/i166-repro.sh
pnpm --dir web test --run \
  src/pages/PlanDetail.test.tsx \
  src/pages/planGraphLayout.test.ts
```

## Results

- The production SPA and real backend started against a fresh temporary SQLite
  database, random loopback ports, and a fresh test identity.
- The browser harness passed 12/12 checks and wrote eight screenshots with no
  browser console errors.
- The ordinary graph exposed seven API edges and rendered seven visible React
  Flow edge paths, covering the branch/join regression.
- The staged graph exposed two real stages, ten nodes, and eleven edges. The UI
  rendered all eleven edge paths.
- Stage A retained its within-stage dependency. Stage B depended on Stage A and
  the orchestration graph contained the Stage A gate-to-Stage B entry barrier.
- Desktop light, desktop dark, and mobile staged views were inspected; no node
  overlap or material layout error was found.
- The legacy single-node and empty-plan views remained covered.
- The focused PlanDetail and layout suite passed 120/120 tests across two files.
- The temporary directory was removed, `real-chain-error.txt` was absent, and
  `server.log` contained no `panic`, `fatal`, or `error` match.

The generated machine evidence is in
`docs/acceptance/i166-evidence/real-chain-results.json`; screenshots are in the
same directory.

## Provenance note

The evidence committed by delivery revision `6060270a...` recorded
`harness_sha=ff43722e...` because `6060270a...` is the evidence-recording commit
whose sole delta from `ff43722e...` is the generated evidence set. No harness
source changed between those revisions. This independent run executed at
`6060270a...`; its regenerated JSON therefore records
`harness_sha=6060270a30906beec0802712935747fffd967727` while preserving the same
semantic result: 12/12 checks, two stages, ten staged nodes, eleven staged edges,
and zero console errors.
