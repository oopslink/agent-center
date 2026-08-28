import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import type { PlanProgressControl } from '@/api/plans';
import { PlanProgressCockpit } from './PlanDetail';

describe('PlanProgressCockpit', () => {
  it('collapses action details by default and expands them on demand', async () => {
    const control: PlanProgressControl = {
      as_of: '2026-08-27T12:00:00Z',
      freshness: { state: 'degraded', watermark_lag_ms: 1000, threshold_ms: 120000 },
      decision: 'responsibility_bound', observation_vector_id: 'obs-1', quality: 'suspect',
      open_obligations: [], open_incidents: [], open_holds: [],
      required_actions: [
        { id: 'a1', source_type: 'obligation', source_id: 'o1', category: 'owner_action', action: 'resolve_human_decision', owner_ref: 'user:owner', owner_display: 'Owner', deadline_at: '2026-08-27T13:00:00Z', trigger_fact_refs: ['decision:1'], options: ['unblock_resume', 'reassign_redispatch', 'discard_replace'] },
        { id: 'a2', source_type: 'obligation', source_id: 'o2', category: 'prerequisite_wait', action: 'wait_for_prerequisite', owner_ref: 'service:pm', owner_display: 'PM', deadline_at: '2026-08-27T13:05:00Z', trigger_fact_refs: ['upstream:done'] },
      ],
    };
    render(<PlanProgressCockpit control={control} />);
    expect(screen.getByTestId('plan-progress-cockpit')).toHaveAttribute('data-freshness', 'degraded');
    const toggle = screen.getByTestId('plan-progress-toggle');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByText('2 actions')).toBeInTheDocument();
    expect(screen.queryByText(/owner action/)).not.toBeInTheDocument();

    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByText(/owner action/)).toBeInTheDocument();
    expect(screen.getByText(/prerequisite wait/)).toBeInTheDocument();
    expect(screen.getByText(/facts decision:1/)).toBeInTheDocument();
    expect(screen.getByText(/unblock_resume.*reassign_redispatch.*discard_replace/)).toBeInTheDocument();
  });
});
