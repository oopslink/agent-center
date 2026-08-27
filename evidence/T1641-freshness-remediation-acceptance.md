# T1641 freshness remediation acceptance

- Verdict: PASS
- Reviewed SHA: `ca953dbd2a6b58e04b542d62a1a2ea196d278f68`
- Remote ref: `origin/ac-exec/task-3f17fd52/exec-824c2088`
- Baseline and merge-base: `origin/main@16dc58155dfa0cafd79c08595c8a8e378b5eede9`
- Candidate distance: one commit

## Acceptance matrix

- Fresh runtime: DuckDB `TIMESTAMPTZ` checkpoint text parses and Overview plus
  execution detail return `fresh` with a non-empty `refreshed_at`.
- Boundary: age exactly equal to TTL remains `fresh`; TTL plus 1 ms becomes
  `stale`, with the expected `age_ms`.
- Rebuild: rebuilding the DuckDB read model restores `fresh` and preserves the
  completed-execution projection.
- API: Overview and execution endpoints expose consistent fresh state,
  non-negative age, and the configured threshold.
- UI: all 193 test files / 1820 tests pass, including the Insight fresh, stale,
  rebuilding, unavailable, overview, drill-down, and execution-detail states.
- Regression: Insight and webconsole API packages pass; focused race runs pass.

## Commands and results

```text
go test ./internal/insight ./internal/webconsole/api \
  -run '<freshness/rebuild/API selection>' -count=10
PASS

go test -race ./internal/insight ./internal/webconsole/api \
  -run '<freshness/rebuild/API selection>' -count=3 -timeout=15m
PASS (insight 20.878s; webconsole/api 38.044s)

go test ./internal/insight ./internal/webconsole/api -count=1 -timeout=15m
PASS (insight 2.362s; webconsole/api 60.404s)

pnpm test
PASS (193 files, 1820 tests)

pnpm typecheck
PASS

go test ./... -count=1 -timeout=20m
PASS, including tests/e2e and tests/integration
```

The first attempted UI command was issued from the repository root and failed
with `package.json` not found; rerunning from `web/` after the frozen pnpm
install passed. This was a harness working-directory error, not a candidate
failure.
