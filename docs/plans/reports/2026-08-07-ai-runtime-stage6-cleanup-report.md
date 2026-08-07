# AI Runtime Stage 6 Cleanup Report

Date: 2026-08-07

Scope: remove legacy fallback/write surfaces after Stage 5 cutover, while keeping historical runtime/catalog storage readable.

## Gate

This isolated executor did not access production metrics, agent-center control-plane state, database files, admin sockets, worker tokens, or raw HTTP endpoints. Therefore the production gate cannot be independently proven from this workspace:

- `runtime_legacy_fallback_total{object_type}` must be zero for one full release window.
- The latest migration dry-run/apply report must have zero unmapped and zero pending object selections.
- Production consumers of the retired `/model-catalog` and `*_model_catalog*` tool surfaces must be confirmed absent outside this repository.

The code changes below assume that release-management evidence exists outside this isolated workspace. This report is not a substitute for that deployment-level signoff.

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

## Local Acceptance Coverage

The cleanup must be validated with:

- Retry/resume and historical execution readability tests.
- AI Runtime migration dry-run/apply idempotence and zero-pending report behavior.
- Absence of public fallback/rollback controls after cleanup.
- Frontend create/edit flows proving model and CLI are catalog-backed.
- Secret API regression tests proving plaintext values are not echoed.

Commands run for this branch are recorded in the final executor report.

