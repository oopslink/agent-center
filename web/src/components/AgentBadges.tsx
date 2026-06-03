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
