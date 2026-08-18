# T1385 Revoke Fix Independent Review

- reviewed_sha: `252584fd0b4f3507a2090f449855005248a9b536`
- baseline_sha: `142a9e080c44d620a4f18ef44ef83530f7eaa537`
- managed_branch: `ac-exec/task-4addbbeb/exec-0794706e`
- evidence HEAD: carrier only; not the reviewed SHA

## Verdict

FAIL for `252584fd0b4f3507a2090f449855005248a9b536`.

The candidate correctly preserves the original revoke preview token, freezes the previewed reason/message into the confirm request, writes `reason` and `message` into the revoke audit payload, records the stable idempotency key as `request_id`, and keeps CAS drift rejection. Full local gates passed.

The remaining blocking issue is idempotent confirm replay. `ConfirmRevoke` consumes `authorization_revoke_previews` and calls `runBatchInTx` directly; it does not enter the `authorization_idempotency_keys` ledger used by `ApplyBatch`. A successful confirm retried with the same preview/token/idempotency key is rejected as `ErrPreviewRejected`, which the existing service test still asserts. That means the stable key is audit metadata, not a durable replay key for the confirm endpoint.

## Evidence

- Remote: `origin git@github.com:oopslink/agent-center.git`
- Ancestry: baseline is ancestor of reviewed SHA.
- Managed branch was fast-forwarded to the reviewed SHA before review.
- `go test ./internal/authorization ./internal/webconsole/api`: PASS
- `go test ./...`: PASS
- `make lint`: PASS
- `pnpm --dir web test --run src/pages/Access.test.tsx`: PASS, 6 tests
- `pnpm --dir web test --run`: PASS, 191 files / 1773 tests

Structured verdict:

```json
{
  "review_sha": "252584fd0b4f3507a2090f449855005248a9b536",
  "verdict": "FAIL",
  "reason": "The candidate fixes reason/message propagation, audit request_id, preview-token preservation, CAS drift rejection, and passes full gates, but confirm replay remains non-idempotent because the confirm path rejects the consumed preview instead of replaying the completed response for the same idempotency key."
}
```
