# T1723 Insight API semantics closure

Frozen design source: `docs/design/features/insight-metric-semantics-and-information-architecture.md` at `2e04ab8610f2c07bef847b11183a27e2b5cd7512`.
Phase 1 metric formulas remain governed by `docs/design/features/insight-phase-1-contract.md`.

Base recorded before implementation:

- `origin/main`: `1f27bfe74e3dddaf3ffb7e0156c5a61ad55480b4`
- `HEAD`: `1f27bfe74e3dddaf3ffb7e0156c5a61ad55480b4`
- merge-base: `1f27bfe74e3dddaf3ffb7e0156c5a61ad55480b4`

## API additions

Existing fields are unchanged. The Insight API now adds stable semantic fields so
clients do not need to print or infer internal enums:

- `summary.semantics.*`: per-metric envelopes with `value`, `status`,
  `coverage`, `freshness`, `window`, and `sample_count` where applicable.
- execution rows: `command_status`, `status_reason`, `status_message`,
  `failure_message`.
- execution rows: `status` user semantic mapping and `quality_semantic`; raw
  `outcome`, `quality`, and reason fields remain available for audit.
- 503 Insight failures return a parseable envelope containing `window`, `as_of`,
  `refreshed_at`, and `freshness`.

Semantic statuses distinguish real zero (`zero`), missing denominator or samples
(`no_sample`), unknown/unavailable values (`unknown`), low slot coverage
(`low_coverage`), partial slot coverage (`partial_coverage`), stale projected
data (`stale`), and normal values (`ok`).

## Projection changes

DuckDB schema version is `2`. The disposable projection adds nullable message and
queue-status columns to `execution_fact`, plus nullable `status_message` to
`queue_interval_fact`. `ensureSchema` can replay against existing files using
`ALTER TABLE ... ADD COLUMN IF NOT EXISTS`; full rebuild remains supported.

Projection sources remain unchanged:

- queue fields are read from `worker_control_events.status`,
  `status_reason`, and `status_detail`;
- failure messages are read from executor activity payload `detail`;
- no synthetic facts or UI-derived metrics are introduced.

## Fixtures

Focused Go fixtures cover:

- duplicate replay, late start after stop, exact 24h boundaries, quantile_cont,
  rebuild equality, checkpoint crash before and after commit;
- heartbeat duplicate/coalescing, admission-cap interval changes, stale TTL
  coverage;
- invalid time ordering diagnostics;
- execution user semantics for failed, rejected did-not-start, unknown terminal
  outcome, raw-message passthrough, and detail 24h gating;
- HTTP read side-effect freedom, org isolation, invalid window, additive
  execution fields, and 503 freshness envelope.
