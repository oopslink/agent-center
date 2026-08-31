# G5 Insight Independent Reacceptance

Candidate SHA: `e6c4d311652b1f7676900d4a9bc0283a1cacd29c`

Verdict: REJECT

## Provenance

- Local HEAD under test: `e6c4d311652b1f7676900d4a9bc0283a1cacd29c`.
- Remote candidate ref: `refs/heads/candidate/g4-insight-i1-i5-e6c4d311` resolved to `e6c4d311652b1f7676900d4a9bc0283a1cacd29c`.
- Remote main ref observed during verification: `ad7959c60ea0d3004c44d65cfbe2c93de34c9406`.
- Served HEAD proof: `logs/served-head.json` reports `candidate_sha=e6c4d311652b1f7676900d4a9bc0283a1cacd29c` and served assets from `internal/webconsole/spa/dist`.
- Evidence branch contains only acceptance artifacts under `docs/acceptance/g5-insight-e6c4d311/`.

## Checks Run

- `pnpm --dir web exec vitest run src/pages/InsightOverview.test.tsx`: PASS, 8 tests.
- `pnpm --dir web build`: PASS. Build emitted one pre-existing CSS minifier warning around an invalid CSS token.
- Browser served-production-bundle inspection through `mock-server.mjs`: PASS for loading the product bundle from the candidate SHA.
- IA coverage: overview, agents, agent detail, task executions, task execution detail, projects, project detail delivery/evolution, and plan lineage.
- State matrix: fresh, empty, rebuilding, forbidden, not-found, unavailable, invalid time quality, unknown enum, structured object drilldown.

## Failing Evidence

The raw enum regression gate fails.

- `screenshots/10-agent-detail.png` and `logs/10-agent-detail.txt` show user-facing reason chips `unknown_status` and `raw_future_enum`.
- `screenshots/13-plan-lineage.png` and `logs/13-plan-lineage.txt` show user-facing recovery outcome `future_outcome`.
- These are visible copy, not intentional technical JSON disclosure. They violate the requirement to catch raw enum regressions.

Non-failing observations:

- `screenshots/12-project-detail-delivery-evolution.png` shows raw object drilldowns rendered as `Structured filter`, avoiding `[object Object]`.
- `logs/raw-regression-grep.txt` contains only the preserved initial mock-contract error plus the raw enum failures described above.
- `logs/final-browser-errors.txt` is empty; `logs/final-browser-console.txt` contains only expected HTTP status entries from intentional 404/403/503 state-matrix probes and no final JavaScript exception.

## Screenshots

- `00-initial-mock-contract-error.png`: preserved red-state evidence from an initial evidence-server fixture mismatch.
- `01-overview-desktop.png`: overview, desktop.
- `02-executions-filtered-desktop.png`: filtered task execution list.
- `03-execution-detail-invalid-quality.png`: invalid time quality detail.
- `04-execution-detail-not-found.png`: not-found detail state.
- `05-overview-rebuilding.png`: overview rebuilding state.
- `06-overview-forbidden.png`: overview forbidden state.
- `07-overview-empty.png`: empty overview state.
- `08-executions-rebuilding.png`: executions rebuilding state.
- `09-agents-list.png`: agents list.
- `10-agent-detail.png`: failing raw enum reason chips.
- `11-projects-list.png`: projects list.
- `12-project-detail-delivery-evolution.png`: delivery/evolution drilldowns.
- `13-plan-lineage.png`: failing raw enum recovery outcome.
- `14-overview-mobile.png`: mobile overview.
- `15-execution-detail-unavailable.png`: unavailable detail state.

## Conclusion

Reject `e6c4d311652b1f7676900d4a9bc0283a1cacd29c` for G5 independent reacceptance because raw enum values remain visible in Insight user-facing copy.
