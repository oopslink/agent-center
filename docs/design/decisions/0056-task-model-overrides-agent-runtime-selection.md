# ADR-0056: `task.model` overrides Agent Runtime Selection during migration

Status: Accepted

## Context

F3 already defines `task.model` as a hard, judge-bypassing model override. AI
Runtime Stage 2 adds an immutable Agent-derived Runtime Snapshot. Silently letting
the new snapshot defeat an existing task override would change running behavior
while the feature is still gated.

## Decision

For Stage 2, the effective executor model precedence is:

1. explicit `fork_executor` request override;
2. persisted `task.model`;
3. immutable Agent Runtime Snapshot model;
4. legacy F3 modelrouter.

When a Runtime Snapshot exists its `cli_key` remains authoritative for the
adapter. A task model override changes only the model. The adapter must fail
closed if that model is not valid for the frozen CLI. Team Role and Executor
candidate selections remain outside this chain.

The decision is transitional. Stage 5 may replace `task.model` with a Catalog
reference after shadow-diff evidence shows that the old override can be retired.

## Consequences

- Feature-flag OFF preserves the current F3 behavior.
- Feature-flag ON preserves explicit task intent while freezing Agent selection.
- No second mutable profile read is introduced at executor launch.
