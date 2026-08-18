# T1402 deployed revoke idempotency acceptance

- Verdict: PASS
- Reviewed and deployed SHA: `71a4e08263fea90fc491afbffdc54a76c6e43082`
- Source: fetched `origin/main`; validation used an isolated detached worktree and test-instance.
- Runtime: `/api/health`, `/api/system/version`, and SPA root returned HTTP 200; runtime version was stamped `main-71a4e08` / commit `71a4e08`.
- Revoke chain: grant and preview succeeded; first confirm returned HTTP 200; an exact replay using the same preview ID, token, request body, and idempotency key also returned HTTP 200. The first and replay response projections were equal, and replay did not return the prior `409 revoke_preview_consumed` failure.
- Audit readback: the revoke event was found through the audit API; `request_id` matched the idempotency key and `payload.reason` plus `payload.message` matched the submitted values.
- Regression: targeted `internal/authorization` and `internal/webconsole/api` Access/auth/profile/CAS tests passed; deployed `make smoke` passed (`v22-deployed-pipeline.spec.ts`, Go e2e, 30-second smoke).
- Cleanup/rollback: `uninstall test-instance` completed; the isolated namespace and launchd label were absent and the former health endpoint was unreachable afterward.

Executor `exec-77ceae4a` produced the underlying command audit (all acceptance commands exited 0). Its original delivery snapshot was rejected only because `make smoke` modified tracked Playwright report artifacts in that disposable worktree. This clean evidence commit records the supervisor-reviewed result without product-code changes or secrets.
