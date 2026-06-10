import type React from 'react';
import type { AgentLifecycle, Availability } from '@/api/types';

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
