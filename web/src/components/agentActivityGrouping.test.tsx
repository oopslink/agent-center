import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent } from '@testing-library/react';
import type { AgentActivityEvent } from '@/api/types';
import { groupActivity } from './agentActivityGrouping';
import { CheckingGroup } from './AgentActivityRow';

const ev = (id: string, event_type: string, time = '01:00'): AgentActivityEvent =>
  ({ id, agent_id: 'A1', event_type, payload: '{}', occurred_at: `2026-05-24T${time}:00Z` }) as AgentActivityEvent;

describe('groupActivity (#274 Checking fold)', () => {
  it('folds consecutive checking events into a group; keeps non-checking separate', () => {
    const items = groupActivity([
      ev('1', 'result'), // output (non-checking)
      ev('2', 'system_init'),
      ev('3', 'lifecycle'),
      ev('4', 'rate_limit'),
      ev('5', 'result'),
    ]);
    expect(items.map((i) => i.kind)).toEqual(['event', 'checking-group', 'event']);
    const group = items[1];
    expect(group.kind).toBe('checking-group');
    if (group.kind === 'checking-group') expect(group.events).toHaveLength(3);
  });

  it('renders a LONE checking event as a normal event (no "× 1" group)', () => {
    const items = groupActivity([ev('1', 'result'), ev('2', 'system_init'), ev('3', 'result')]);
    expect(items.map((i) => i.kind)).toEqual(['event', 'event', 'event']);
  });

  it('folds a run over the FULL accumulated set (spanning what were separate pages)', () => {
    // page1 [lifecycle,lifecycle] + page2 [lifecycle] concatenated → one "× 3" group.
    const items = groupActivity([ev('a', 'lifecycle'), ev('b', 'lifecycle'), ev('c', 'lifecycle')]);
    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe('checking-group');
    if (items[0].kind === 'checking-group') expect(items[0].events).toHaveLength(3);
  });
});

describe('CheckingGroup (#274)', () => {
  afterEach(() => cleanup());

  it('shows "× N" + time range + a disclosure that expands to the raw events', () => {
    // newest-first (ULID DESC): [0]=03:00 latest, [2]=01:00 earliest.
    const events = [ev('3', 'lifecycle', '03:00'), ev('2', 'lifecycle', '02:00'), ev('1', 'lifecycle', '01:00')];
    render(
      <ul>
        <CheckingGroup events={events} />
      </ul>,
    );
    const toggle = screen.getByTestId('agent-activity-checking-toggle');
    expect(toggle).toHaveTextContent('Checking messages × 3');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveAttribute('aria-label', 'Checking messages, 3 events, collapsed');
    // earliest–latest time range.
    expect(screen.getByTestId('agent-activity-checking-group')).toHaveTextContent('01:00–03:00');
    // collapsed → raw events hidden.
    expect(screen.queryByTestId('agent-activity-checking-expanded')).toBeNull();
    // expand → all 3 raw events, aria-controls points to the expanded region id.
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const region = screen.getByTestId('agent-activity-checking-expanded');
    expect(toggle).toHaveAttribute('aria-controls', region.id);
    expect(screen.getAllByTestId('agent-activity-row')).toHaveLength(3);
  });
});
