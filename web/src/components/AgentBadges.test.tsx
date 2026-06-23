import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import {
  ActivityBadge,
  AGENT_IDLE_MS,
  AgentBacklogBadge,
  AgentLoadBadge,
  AgentStatusBadge,
  deriveAgentActivity,
  deriveAgentLoad,
  deriveBacklogLevel,
  deriveAgentStatus,
} from './AgentBadges';
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
    expect(badge).toHaveTextContent(/idle/i);
    expect(badge).toHaveAttribute('data-activity-status', 'idle');
    // idle is green (T235 §2): the success text token.
    expect(badge.className).toContain('text-success');
  });

  it('busy reads as "Active" (T320 — disambiguated from Availability\'s "busy") with the brand token', () => {
    render(<ActivityBadge status="busy" />);
    const badge = screen.getByTestId('agent-activity-status-badge');
    // T320: label is "Active", NOT "busy" (kills the BUSY/BUSY collision); the
    // raw status data attribute is still "busy".
    expect(badge).toHaveTextContent(/active/i);
    expect(badge).not.toHaveTextContent(/busy/i);
    expect(badge).toHaveAttribute('data-activity-status', 'busy');
    expect(badge.className).toContain('text-brand');
    expect(badge.className).not.toContain('text-success');
  });
});

// T322: the single unified status folds lifecycle + availability + activity into
// one label by priority (dead/broken first, then availability, then activity).
describe('deriveAgentStatus (T322 single status)', () => {
  const a = (over: Partial<Agent>): Pick<Agent, 'lifecycle' | 'availability' | 'last_activity_at'> => ({
    lifecycle: 'running',
    availability: 'available',
    ...over,
  });
  const recent = new Date(NOW - 60_000).toISOString();
  const stale = new Date(NOW - 10 * 60_000).toISOString();

  it('error lifecycle → error (highest priority)', () => {
    expect(deriveAgentStatus(a({ lifecycle: 'error', availability: 'available' }), NOW)).toBe('error');
  });
  it('non-running → stopped', () => {
    expect(deriveAgentStatus(a({ lifecycle: 'stopped' }), NOW)).toBe('stopped');
  });
  it('running + unavailable → unavailable', () => {
    expect(deriveAgentStatus(a({ availability: 'unavailable' }), NOW)).toBe('unavailable');
  });
  it('running + busy → busy', () => {
    expect(deriveAgentStatus(a({ availability: 'busy' }), NOW)).toBe('busy');
  });
  it('running + available + recent activity → working', () => {
    expect(deriveAgentStatus(a({ availability: 'available', last_activity_at: recent }), NOW)).toBe('working');
  });
  it('running + available + quiet → idle', () => {
    expect(deriveAgentStatus(a({ availability: 'available', last_activity_at: stale }), NOW)).toBe('idle');
  });
});

describe('AgentStatusBadge (T322)', () => {
  it('renders one dot + word with the status + a breakdown tooltip', () => {
    render(<AgentStatusBadge agent={{ lifecycle: 'running', availability: 'busy', last_activity_at: undefined }} now={NOW} />);
    const badge = screen.getByTestId('agent-status-badge');
    expect(badge).toHaveAttribute('data-agent-status', 'busy');
    expect(badge).toHaveTextContent(/busy/i);
    expect(badge.getAttribute('title')).toMatch(/Availability: busy/);
  });

  it('a non-running agent shows only its lifecycle in the tooltip', () => {
    render(<AgentStatusBadge agent={{ lifecycle: 'stopped', availability: 'unavailable', last_activity_at: undefined }} now={NOW} />);
    const badge = screen.getByTestId('agent-status-badge');
    expect(badge).toHaveAttribute('data-agent-status', 'stopped');
    expect(badge.getAttribute('title')).toBe('Lifecycle: stopped');
  });
});

describe('agent load metric (T342)', () => {
  it('derives load + pressure level from running/pending counts', () => {
    // none: no tasks → load 0, level none
    expect(deriveAgentLoad({ running_tasks: 0, pending_tasks: 0 })).toMatchObject({
      total: 0,
      load: 0,
      level: 'none',
    });
    // high: 1 doing, 0 pending → 1.0
    expect(deriveAgentLoad({ running_tasks: 1, pending_tasks: 0 })).toMatchObject({
      load: 1,
      level: 'high',
    });
    // medium: 1 doing, 1 pending → 0.5
    expect(deriveAgentLoad({ running_tasks: 1, pending_tasks: 1 })).toMatchObject({
      load: 0.5,
      level: 'medium',
    });
    // low: 1 doing, 4 pending → 0.2
    expect(deriveAgentLoad({ running_tasks: 1, pending_tasks: 4 }).level).toBe('low');
    // prefers the server task_load when present
    expect(deriveAgentLoad({ running_tasks: 1, pending_tasks: 1, task_load: 0.7 }).load).toBe(0.7);
  });

  it('renders an explicit "load: x.x" tag + a pressure-colored level (T342c)', () => {
    render(<AgentLoadBadge agent={{ running_tasks: 1, pending_tasks: 3 }} />);
    const badge = screen.getByTestId('agent-load-badge');
    expect(badge).toHaveTextContent('load: 0.3'); // 1/4 = 0.25 → 0.3 (toFixed(1) rounds)
    expect(badge).toHaveAttribute('data-load-level', 'low');
  });

  it('shows load: 0.0 (muted) when the agent has no active tasks', () => {
    render(<AgentLoadBadge agent={{ running_tasks: 0, pending_tasks: 0 }} />);
    const badge = screen.getByTestId('agent-load-badge');
    expect(badge).toHaveTextContent('load: 0.0');
    expect(badge).toHaveAttribute('data-load-level', 'none');
  });
});

describe('agent backlog metric (T342b)', () => {
  it('derives backlog level by pending depth', () => {
    expect(deriveBacklogLevel(0)).toBe('none');
    expect(deriveBacklogLevel(2)).toBe('low');
    expect(deriveBacklogLevel(5)).toBe('medium');
    expect(deriveBacklogLevel(6)).toBe('high');
  });

  it('renders an explicit "backlog: N" tag with a depth-colored level (T342c)', () => {
    render(<AgentBacklogBadge agent={{ pending_tasks: 7 }} />);
    const badge = screen.getByTestId('agent-backlog-badge');
    expect(badge).toHaveTextContent('backlog: 7');
    expect(badge).toHaveAttribute('data-backlog-level', 'high');
  });

  it('shows backlog: 0 (muted) when there is no backlog', () => {
    render(<AgentBacklogBadge agent={{ pending_tasks: 0 }} />);
    const badge = screen.getByTestId('agent-backlog-badge');
    expect(badge).toHaveTextContent('backlog: 0');
    expect(badge).toHaveAttribute('data-backlog-level', 'none');
  });
});
