# AI Runtime Stage 6 Recovery Report

Date: 2026-08-07

## Result

Stage 6 remains fail-closed. The destructive cleanup from `36676f14` was
reversed because the release-owned production evidence is not present. Stage 5
and the replacement AI Runtime surfaces remain intact, and the cleanup may be
replayed as one auditable commit after the preflight returns `allowed: true`.

`POST /api/orgs/{slug}/ai-runtime/cleanup/preflight` now requires all of:

- attributed zero fallback samples spanning the declared release window;
- a zero-pending migration report plus the report artifact digest;
- a production consumer inventory proving zero retired consumers, with its
  artifact digest, and a successful probe of the replacement path;
- isolated acceptance with deployment and process fingerprints;
- a tested rollback artifact followed by owner confirmation.

Missing or incomplete evidence returns HTTP 409. The endpoint validates only
and cannot delete data.

## Local verification

The recovery was tested after reconstructing the clean merge of
`86112b34` and accepted Stage 5 `e288a668`, then reversing only the Stage 6
cleanup delta.

- `go test ./internal/airuntime ./internal/airuntime/sqlite ./internal/webconsole/api ./internal/admin/api ./internal/mcphost`: pass.
- `go test ./internal/agentruntime/... ./internal/secretmgmt/...`: pass. This
  covers resume/retry execution behavior and Secret storage/redaction paths.
- `pnpm --dir web exec vitest run src/pages/OrgModelCatalog.test.tsx src/components/AgentCreateModal.test.tsx src/components/AgentConfigEditModal.test.tsx`: 3 files, 21 tests pass.

These local results do not claim a production release window or a real isolated
deployment fingerprint. Those values must come from release-owned artifacts;
the strengthened gate rejects their absence instead of inferring them from
tests.

## Release completion procedure

1. Submit the metric, migration, consumer-audit, replacement-probe, isolated
   deployment, and rollback artifacts to the preflight endpoint.
2. Record the returned evidence SHA-256 alongside the release.
3. Only after `allowed: true`, replay the single cleanup delta represented by
   `1f20a0a3..36676f14` and run the same verification suite against the deployed
   artifact.
4. Keep the pre-cleanup artifact available until the declared rollback window
   closes.
