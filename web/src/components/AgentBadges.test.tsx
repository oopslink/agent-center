import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { ActivityBadge, AGENT_IDLE_MS, deriveAgentActivity } from './AgentBadges';
import type { Agent } from '@/api/types';

afterEach(() => cleanup());

// A fixed "now" so the idle threshold maths is deterministic.
const NOW = Date.parse('2026-06-20T12:00:00Z');
const agent = (over: Partial<Agent>): Pick<Agent, 'lifecycle' | 'last_activity_at'> => ({
  lifecycle: 'running',
  ...over,
});

describe('deriveAgentActivity (T235)', () => {
  it('returns null for non-running agents (lifecycle already describes them)', () => {
    for (const lc of ['stopped', 'stopping', 'resetting', 'error', 'archived'] as const) {
      expect(deriveAgentActivity(agent({ lifecycle: lc }), NOW)).toBeNull();
    }
  });

  it('running + no activity recorded → idle', () => {
    expect(deriveAgentActivity(agent({ last_activity_at: undefined }), NOW)).toBe('idle');
  });

  it('running + recent activity (<5 min) → busy', () => {
    const oneMinAgo = new Date(NOW - 60_000).toISOString();
    expect(deriveAgentActivity(agent({ last_activity_at: oneMinAgo }), NOW)).toBe('busy');
  });

  it('running + stale activity (≥5 min) → idle', () => {
    const sixMinAgo = new Date(NOW - 6 * 60_000).toISOString();
    expect(deriveAgentActivity(agent({ last_activity_at: sixMinAgo }), NOW)).toBe('idle');
  });

  it('the 5-min boundary is inclusive (exactly 5 min → idle)', () => {
    const exactly = new Date(NOW - AGENT_IDLE_MS).toISOString();
    expect(deriveAgentActivity(agent({ last_activity_at: exactly }), NOW)).toBe('idle');
    const justUnder = new Date(NOW - AGENT_IDLE_MS + 1).toISOString();
    expect(deriveAgentActivity(agent({ last_activity_at: justUnder }), NOW)).toBe('busy');
  });

  it('running + unparseable timestamp → idle (no activity to count as busy)', () => {
    expect(deriveAgentActivity(agent({ last_activity_at: 'not-a-date' }), NOW)).toBe('idle');
  });
});

describe('ActivityBadge (T235)', () => {
  it('renders the status as a text label (not color-only) with a data attribute', () => {
    render(<ActivityBadge status="idle" />);
    const badge = screen.getByTestId('agent-activity-status-badge');
    expect(badge).toHaveTextContent('idle');
    expect(badge).toHaveAttribute('data-activity-status', 'idle');
    // idle is green (T235 §2): the success text token.
    expect(badge.className).toContain('text-success');
  });

  it('busy reads distinct from idle (brand token, not the green idle token)', () => {
    render(<ActivityBadge status="busy" />);
    const badge = screen.getByTestId('agent-activity-status-badge');
    expect(badge).toHaveTextContent('busy');
    expect(badge.className).toContain('text-brand');
    expect(badge.className).not.toContain('text-success');
  });
});
