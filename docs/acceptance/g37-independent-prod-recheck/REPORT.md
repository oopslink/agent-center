# G37 Independent Production Recheck

Date: 2026-08-31

## Verdict

PASS.

Fresh independent verification was run from the live deployed `main` SHA after
remote readback and fast-forward:

- Deployed/remote SHA: `d44fb334286bee544795d7af272b57874c6e97ad`
- Local checkout before fetch: `ccf4acb3262edbb86bfe3f467df67a2eaf64e0c3`
- Local checkout under test: `d44fb334286bee544795d7af272b57874c6e97ad`
- Source implementation: `65eab385a93199d98fe3b367d4d07bc627cda7f2`
- Required production-chain anchor: `7f4cfcc43e0360f31e756bb453e675db50cc26c6`

No G35/G36 evidence was reused. All evidence below was generated in
`docs/acceptance/g37-independent-prod-recheck/raw/`.

## Criteria

| Criterion | Result | Raw evidence |
| --- | --- | --- |
| Exact deployed SHA | PASS | `03-remote-deployed-sha.log`, `04-fetch-and-sha-readback.log`, `05-fast-forward-to-deployed-sha.log` |
| Four hard-red fail-closed checks | PASS | `07-workerdaemon-hard-reds.log` |
| Health/version | PASS | `09-build-health-version.log`, `11-e2e-runtime-version.log` |
| Lineage/provenance | PASS | `06-lineage-provenance-deployed-sha.log` |
| API/UI regressions | PASS | `08-admin-api-provenance-taskinput.log`, `12-spa-tsc-ui-rerun.log` |
| tsc/race regressions | PASS | `12-spa-tsc-ui-rerun.log`, `14-make-test-race.log` |
| task-input/v1 | PASS | `02-task-input-v1.log`, `08-admin-api-provenance-taskinput.log` |
| Full regression/vet | PASS | `13-go-test-all.log`, `15-go-vet.log` |

## Raw Evidence Highlights

- Remote exact SHA readback showed both `origin` and `origin-push` live `main`
  at `d44fb334286bee544795d7af272b57874c6e97ad`.
- The workspace was fast-forwarded to that SHA before acceptance tests.
- Lineage readback proved both required commits are ancestors of the tested
  SHA:
  - `source_ancestor_exit=0`
  - `anchor_ancestor_exit=0`
- Worker hard-red tests passed stale SHA, unavailable projection, unhealthy
  status, disconnect, timeout, missing/short/wrong running SHA, missing/wrong
  running version, non-terminal restart, and unhealthy post-restart result.
- Runtime version e2e passed control-stream on/off and adopted-old-runtime skew
  negative regression.
- `make lint-spa-tsc` passed on rerun after dependency install settled, and the
  focused UI suite passed 49 tests.
- `go test ./...`, `make test-race`, and `go vet ./...` passed.

## Notes

- `10-spa-tsc-ui.log` records an initial SPA command failure caused by running
  concurrently with the first dependency install from `make build`; after the
  build completed and dependencies were present, `12-spa-tsc-ui-rerun.log`
  passed cleanly.
- The task-input package had no attachments. Its README and manifest were read
  and hashed in `00-environment.log` and `02-task-input-v1.log`.
