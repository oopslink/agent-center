# AI Runtime Stage 6 Cleanup Report

Date: 2026-08-07

Scope: remove legacy fallback/write surfaces after Stage 5 cutover, while keeping historical runtime/catalog storage readable.

## Gate and delivery boundary

This feature branch prepares the cleanup but does not merge or deploy it. The
production gate still requires:

- `runtime_legacy_fallback_total{object_type}` must be zero for one full release window.
- The latest migration dry-run/apply report must have zero unmapped and zero pending object selections.
- Production consumers of the retired `/model-catalog` and `*_model_catalog*` tool surfaces must be confirmed absent outside this repository.

No production evidence was invented or inferred from unit tests. The
production-reachable cleanup preflight remains fail-closed and rejects missing
evidence with HTTP 409. Merging or deploying this branch is prohibited until
release management submits the artifacts and receives `allowed: true`.

The branch preparation continued under the owner's explicit direction on
2026-08-07. That direction authorizes completing and testing the feature branch;
it does not assert that the production release-window gate has passed.

## Removed

- Legacy runtime fallback adapter and counter path (`LegacyAdapter`, `LegacyFallbackCounter`).
- Public AI Runtime shadow/cutover HTTP controls (`/ai-runtime/shadow-compare`, `/ai-runtime/cutover`) and the service methods backing runtime fallback selection.
- Legacy model catalog Web API routes (`/model-catalog`, `/model-catalog/import`) and their compatibility adapter.
- Legacy model catalog admin/MCP tools (`list_model_catalog_entry`, `create_model_catalog_entry`, `update_model_catalog_entry`, `delete_model_catalog_entry`, `import_model_catalog`).
- Frontend `/model-catalog` route, `modelCatalog` query key, `modelCatalog.ts` adapter, hardcoded `DEFAULT_AGENT_MODEL` / `KNOWN_MODELS`, and free-text model inputs in Agent create/config forms.
- New writes via legacy `allowed_models` input promotion; `allowed_models` remains only as a derived read mirror for existing runtime readers.

## Preserved

- `pm_model_catalog` storage and repository remain because AI Runtime model definitions and resume-state annotations still read catalog metadata from that table.
- Historical execution/runtime snapshots are not rewritten.
- Migration dry-run/apply remains available for audit and repair reporting.
- Secret values remain write-only; no cleanup path returns plaintext.

## Acceptance evidence

The source-built post-cleanup binary was installed through the real
`install test-instance --id stage6final --with-agent --workers 1` path. It
provisioned an independent center, an org-enrolled worker, seeded entities, a
real agent, and a dispatched task. The binary SHA-256 was
`adc44b02b69be49be9d13d059a1c279157932e725932343179b1e00f3cdc0b8c`.

The session access policy permits agent-center state reads only through the
provided MCP tools, and no MCP connection targeting the isolated instance was
available. Consequently provisioning is recorded without bypassing policy via
SQLite, admin sockets, worker tokens, process arguments, or ad-hoc HTTP reads.

Post-cleanup verification passed:

- `make build` (production SPA plus Go binaries).
- `go test ./internal/airuntime/... ./internal/agent/... ./internal/webconsole/api ./internal/admin/api ./internal/mcphost ./internal/workerdaemon`.
- `go test ./internal/agentruntime/... ./internal/secretmgmt/... ./internal/projectmanager/... ./internal/persistence/...`.
- Focused Vitest coverage for `OrgAIRuntime`, Agent create/config, Member create,
  and Agents: 5 files, 44 tests.

These suites cover migration reruns, catalog-backed runtime selection,
retry/resume and stale-resume recovery, historical execution reads, and Secret
storage/redaction. The legacy `/model-catalog` adapters, model-catalog MCP/admin
tools, shadow/cutover controls, frontend constants, and legacy fallback adapter
are absent; the replacement `/ai-runtime` catalog/profile surface remains.
