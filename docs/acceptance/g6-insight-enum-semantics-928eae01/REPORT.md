# G6 Insight Enum Semantics Acceptance

## Structured Verdict

REJECT

## Candidate Binding

- Candidate SHA: `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`
- Candidate ref: `refs/heads/candidate/g6-insight-enum-semantics-928eae01`
- Delivery branch used for evidence: `ac-exec/task-e912da96/exec-b143271d`
- Remote candidate readback: `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`
- Remote `origin/main` at validation time: `ad7959c60ea0d3004c44d65cfbe2c93de34c9406`
- Served UI: Vite preview from this worktree at `http://127.0.0.1:4173` after `pnpm --dir web run build`

## Scope Executed

The executor validated the exact requested candidate SHA only. The local `task-input/v1` package was read first, but it describes a stale T1850 replay package rather than this G6 Insight task; that mismatch is recorded in `raw-logs/00-provenance.log`. No agent-center control-plane tools, databases, sockets, worker tokens, runtime configs, or admin endpoints were used.

Frozen Insight surfaces exercised:

- Insight overview
- Task executions list
- TaskExecution detail
- Insight projects list
- Project delivery/evolution detail
- Plan lineage detail
- Insight agents list
- Agent detail
- Empty, forbidden, rebuilding, and unavailable states
- Desktop `1440x1000` and mobile `390x844` visual passes

Enum/data injections covered known, null, unknown, and arbitrary future values for freshness state, execution outcome, failure/status reasons, command status, data quality, v2 health status, v2 reason codes, funnel break kinds, lineage reasons, recovery outcome, acceptance verdict, metric confidence, and drilldown payload values.

## Evidence Summary

Green checks:

- Remote candidate ref and local `HEAD` both read back as `928eae010f2f8925e3deab6e4cb6cf2f1da58e7b`.
- Frozen web install passed with `pnpm --dir web install --frozen-lockfile`.
- Production web build passed with `pnpm --dir web run build`.
- Targeted Insight tests passed: 4 files, 71 tests.
- Backend Insight package passed: `go test ./internal/insight`.
- Browser matrix generated 22 screenshots and rendered actual Insight UI, not the sign-in screen.
- Browser page errors were empty in the final fetch-interceptor run.
- Automated raw token scan found no injected raw enum tokens such as `future_outcome`, `raw_future_enum`, `backend_new_kind`, `unknown_status`, `[object Object]`, or quoted drilldown keys.

Rejecting findings:

1. `future-overview-1440.png` and `mobile-overview-future-390.png` show the primary UI rendering the raw i18n key `insight.state.unknown` under the future freshness-state injection. This violates the frozen copy requirement for unknown/future enum presentation.
2. `known-lineage-1440.png` and `future-lineage-1440.png` show primary lineage Evidence and Node changes sections as pretty-printed JSON arrays/objects. The task explicitly requires the primary UI to expose no raw JSON.

Because those defects are visible in the primary UI, the candidate is rejected even though the narrower automated raw-token list passed.

## Commands And Logs

- `raw-logs/00-provenance.log` records local/remote SHA readback and task-input package contents.
- `raw-logs/01-web-pnpm-install-frozen-lockfile.log` records frozen dependency installation.
- `raw-logs/02-targeted-insight-vitest.log` records the incorrect first Vitest invocation; retained for command-log completeness.
- `raw-logs/02b-targeted-insight-vitest.log` records the successful targeted Insight test run.
- `raw-logs/03-web-build.log` records the production web build.
- `raw-logs/04-vite-preview.log` records the served preview URL.
- `raw-logs/05*` through `raw-logs/11*` record browser-harness attempts, including rejected false-green runs and the final cache-isolated run.
- `raw-logs/07-go-test-internal-insight.log` records backend Insight package verification.

## Screenshots

Final screenshots are in `screenshots/`. Key rejecting evidence:

- `screenshots/future-overview-1440.png`
- `screenshots/mobile-overview-future-390.png`
- `screenshots/known-lineage-1440.png`
- `screenshots/future-lineage-1440.png`

Red/state evidence:

- `screenshots/forbidden-projects-state-1440.png`
- `screenshots/rebuilding-executions-state-1440.png`
- `screenshots/unavailable-execution-detail-state-1440.png`
- `screenshots/empty-projects-state-1440.png`

## Notes

Team rules referenced by the task could not be read with `get_team_rule` because this isolated executor has no center/MCP access by instruction. The acceptance approach followed the supplied rule descriptions: provenance was read back from git remote/local state, evidence was captured before verdict, and the verdict is bound to the candidate SHA.
