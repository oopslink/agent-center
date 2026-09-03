# 0060. Collaboration Effect is an Observability Projection

| Field | Value |
|---|---|
| Status | Accepted |
| Date | 2026-09-03 |

## Context

Issue-f1bad8db asks Insight to show Agent collaboration relationships and their
positive, negative, neutral, or mixed effect on tasks. The tempting shortcut is
to add a new domain event such as `collaboration.effect.created`, infer missing
task facts from ProjectManager tables, or score agents from traces/messages.

Current production code shows three separate ledgers:

- Observability `events`, append-only Domain Events.
- PM `outbox_events`, a realtime cross-BC relay lane.
- PM `pm_audit_log`, permanent object-level semantic changes.

It also explicitly keeps AgentTraceEvent out of `events`.

## Decision

`CollaborationEffectProjection` belongs to Observability BC as a replayable read
model. It derives from committed Domain Events and producer-owned audit mirrors.
It is not a Domain Event and does not become a business truth source.

MVP attribution is rule-based and programmatic. LLMs do not participate in
classification, persistence, replay, or queries.

The projector must fail closed when required fields are missing. It must not read
ProjectManager repositories, Conversation repositories, PM physical tables, or
AgentTraceEvent files to patch gaps. Missing producer facts must be added as
producer/fan-out work first, especially the PM audit mirror needed for review
verdicts and dependency changes.

## Consequences

- Insight can recompute graph/effect edges under a new rule version without
  mutating task, plan, issue, or conversation truth.
- Negative and mixed effects describe progress impact only; they are not Agent
  performance ratings.
- PM must expose durable audit facts as Observability-consumable events before
  full MVP projection implementation.
- Realtime projection may tail `outbox_events`, but historical replay depends on
  `events` plus mirrored audit facts.
- AgentTraceEvent remains an activity/trace artifact and never becomes evidence
  for CollaborationEffectProjection.

## Alternatives Considered

### Emit `collaboration.effect.created` as a Domain Event

Rejected. The effect is derived interpretation, not a domain fact produced by the
business aggregate that owns the task or plan.

### Let Observability read PM tables during replay

Rejected. This violates BC ownership and would make the read model depend on PM
physical schema instead of an explicit producer contract.

### Use LLM judgment over messages and traces

Rejected for MVP. The product principle is result-based attribution from
structured events, not sentiment or black-box scoring.

### Treat negative effect as Agent performance

Rejected. Review rejection or blocking can be locally negative for progress while
positive for quality; the projection records task effect, not person quality.
