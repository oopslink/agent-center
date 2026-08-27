# T1578 Insight Phase 1 integration evidence

## Candidate construction

- Base: `origin/main` at `b66fe30eb3c3d5bbcedda4ef711150d391f67b81`.
- Backend/remediation line: `968dd76157d4b79755cc59a23163bbffbb1e5dc7`.
- Latest verified UI/detail line: `b9e25a6381b55a687b0d894a1f56fefc8ccbc5e0`.
- Integration merges: `36bc3fab78e3db396872381beb0109be18b5161e` and `ace00c7648e7e86d0f6eef6c8649058608c87bd3`.
- Both merges completed with the `ort` strategy and no manual conflict resolution.

## Upstream coverage matrix

| Scope | Verified SHA | Coverage in candidate |
| --- | --- | --- |
| S2A DuckDB read model/API | `16a4120322f23007511d4609d0cb64d5982d0600` | direct ancestor |
| S2B initial UI | `55a55ca3b248709c6b3207c389cb9a20955dd12d` | superseded by equivalent `e4450a38` on latest verified S2B-R line |
| S2B-R detail states | `b9e25a6381b55a687b0d894a1f56fefc8ccbc5e0` | direct ancestor |
| Frozen S2C harness | `738bc0a6769b413dd4d04c6834207c62c2918fae` | direct ancestor through remediation line |
| Projector crash seam | `0f53752aea1061bdc4cc761a5e9fe5a9f11d0f53` | direct ancestor through remediation line |
| Remediation Gate candidate | `968dd76157d4b79755cc59a23163bbffbb1e5dc7` | direct ancestor |

The early S2B SHA and the S2B-R line share `16a41203` as merge-base. The S2B-R line reapplies the Overview UI as `e4450a38` and adds the accepted detail hardening in `b9e25a63`; therefore the early SHA is intentionally not merged separately.

## Conflict and migration decisions

- No textual conflicts occurred.
- The UI merge touched `internal/insight/service.go`; the final combined tree was therefore revalidated with focused race tests and the complete Go suite.
- No new database migration was introduced by S3. DuckDB schema/lifecycle changes remain those from the accepted S2A/remediation ancestry.
- `git diff --check origin/main..HEAD` passed.

## Verification

- `go test ./internal/insight -count=1`: PASS.
- `go test -race -p 1 ./internal/insight -count=3`: PASS.
- `go test -p 2 ./internal/webconsole/api -count=1`: PASS.
- `go test -p 2 ./...`: PASS, including e2e and integration packages.
- `pnpm exec vitest run src/pages/InsightOverview.test.tsx`: PASS (11/11).
- `pnpm exec tsc -b --force`: PASS.
- `pnpm test`: PASS (193 files, 1820 tests).

