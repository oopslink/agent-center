import type React from 'react';
import type { Agent, AgentLifecycle, Availability } from '@/api/types';

// Shared status chips for the Agent BC surface (v2.7 #101). Reused by the
// Agents list + AgentDetail header so the colour mapping stays in one place.

const AVAILABILITY_CLASS: Record<Availability, string> = {
  available: 'bg-success/10 text-success',
  busy: 'bg-warning/10 text-warning',
  unavailable: 'bg-bg-subtle text-text-muted',
};

export function AvailabilityBadge({
  availability,
}: {
  availability: Availability;
}): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        AVAILABILITY_CLASS[availability],
      ].join(' ')}
      data-testid="agent-availability-badge"
      data-availability={availability}
    >
      {availability}
    </span>
  );
}

// v2.8.1 list-enrich: provider badges for the Agents list (CLI + model). Reuses
// the same neutral chip style as Lifecycle/Availability above (solid bg-subtle
// token + text-secondary, AA in BOTH modes — NOT an alpha-tint-on-token which
// renders transparent, the recurring trap). Text labels, never color-only. An
// absent/blank value is omitted by the caller (this just renders the chip).
export function ProviderBadge({
  label,
  testId,
}: {
  label: string;
  testId?: string;
}): React.ReactElement {
  return (
    <span
      className="rounded bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-medium tracking-wide text-text-secondary"
      data-testid={testId}
    >
      {label}
    </span>
  );
}

// ── T235: derived activity status (Idle / Busy) ────────────────────────────
// Lifecycle + Availability describe the agent's MACHINE state; they don't tell
// an operator whether a *running* agent is actively working or sitting idle.
// T235 adds a derived activity status from `last_activity_at`: a running agent
// with no activity for ≥5 min (or none ever recorded) reads as "idle" — free to
// take new work; one with recent activity reads as "busy". Only running agents
// have an activity status (others are fully described by lifecycle → null).
export type AgentActivityStatus = 'idle' | 'busy';

// The idle threshold: ≥5 min since the last activity event ⇒ idle (per T235 §2).
export const AGENT_IDLE_MS = 5 * 60 * 1000;

// deriveAgentActivity is a PURE compute (now is injectable for tests, mirroring
// formatStatusDuration). A non-running agent has no activity status; a running
// agent with a missing/unparseable last_activity_at is treated as idle (it has
// produced no activity to count as busy).
export function deriveAgentActivity(
  agent: Pick<Agent, 'lifecycle' | 'last_activity_at'>,
  now: number = Date.now(),
): AgentActivityStatus | null {
  if (agent.lifecycle !== 'running') return null;
  if (!agent.last_activity_at) return 'idle';
  const last = new Date(agent.last_activity_at).getTime();
  if (Number.isNaN(last)) return 'idle';
  return now - last >= AGENT_IDLE_MS ? 'idle' : 'busy';
}

// Idle is GREEN (per T235 §2: "展示为 idle 状态（绿色）") — the success token reads
// as "available for work". Busy uses the neutral brand tint to stay distinct
// from Availability's amber "busy" chip (a different axis: machine availability
// vs. live activity). Text label carries the meaning; color is supplementary
// (never color-only).
const ACTIVITY_CLASS: Record<AgentActivityStatus, string> = {
  idle: 'bg-success/10 text-success',
  busy: 'bg-brand/10 text-brand',
};

export function ActivityBadge({
  status,
}: {
  status: AgentActivityStatus;
}): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        ACTIVITY_CLASS[status],
      ].join(' ')}
      data-testid="agent-activity-status-badge"
      data-activity-status={status}
    >
      {status}
    </span>
  );
}

export function LifecycleBadge({
  lifecycle,
}: {
  lifecycle: AgentLifecycle;
}): React.ReactElement {
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        lifecycle === 'error'
          ? 'bg-danger/10 text-danger'
          : 'bg-bg-subtle text-text-secondary',
      ].join(' ')}
      data-testid="agent-lifecycle-badge"
      data-lifecycle={lifecycle}
    >
      {lifecycle}
    </span>
  );
}
