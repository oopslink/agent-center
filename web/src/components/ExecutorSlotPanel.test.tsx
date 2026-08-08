import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { ExecutorSlotPanel, AgentSlotMetricBadge } from './ExecutorSlotPanel';
import type { AgentConcurrency } from '@/api/concurrency';

function conc(overrides: Partial<AgentConcurrency> = {}): AgentConcurrency {
  return {
    agent_id: 'A1',
    cap: 4,
    configured_cap: 4,
    admission_cap: 4,
    slot_count: 4,
    active: 2,
    queued: 1,
    slot_stable: true,
    stale: false,
    reachable: true,
    has_snapshot: true,
    snapshot_age_ms: 1200,
    executors: [],
    slots: [],
    ...overrides,
  };
}

describe('ExecutorSlotPanel', () => {
  afterEach(() => cleanup());

  it('renders a live stable #0..N-1 slot list with 0-based labels', () => {
    render(
      <ExecutorSlotPanel
        data={conc({
          active: 3,
          slots: [
            { slot_index: 0, state: 'running', executor_id: 'exec-0', task_id: 'task-0', cli: 'claude-code', model: 'sonnet', current_activity: 'editing tests' },
            { slot_index: 2, state: 'starting', executor_id: 'exec-2', task_id: 'task-2' },
            { slot_index: 3, state: 'finishing', executor_id: 'exec-3', task_id: 'task-3' },
          ],
        })}
      />,
    );
    expect(screen.getByTestId('executor-slot-panel')).toHaveAttribute('data-mode', 'live');
    expect(screen.getByTestId('agent-concurrency-slots')).toHaveTextContent('3/4');
    expect(screen.getByTestId('agent-concurrency-queued')).toHaveTextContent('1 queued');
    const rows = screen.getAllByTestId('executor-slot-row');
    expect(rows).toHaveLength(4);
    expect(rows.map((row) => within(row).getByTestId('executor-slot-index').textContent)).toEqual(['#0', '#1', '#2', '#3']);
    expect(rows.map((row) => row.getAttribute('data-slot-state'))).toEqual(['running', 'idle', 'starting', 'finishing']);
    expect(screen.getByTestId('executor-slot-current-activity')).toHaveTextContent('Doing: editing tests');
  });

  it('renders expired stale last-known slots and orphan state without inventing idle slots', () => {
    render(
      <ExecutorSlotPanel
        data={conc({
          active: 1,
          configured_cap: 2,
          admission_cap: 2,
          stale: true,
          snapshot_age_ms: 74000,
          slots: [{ slot_index: 3, state: 'orphan', executor_id: 'exec-3', task_id: 'task-3' }],
        })}
      />,
    );
    const panel = screen.getByTestId('executor-slot-panel');
    expect(panel).toHaveAttribute('data-mode', 'expired');
    expect(panel).toHaveAttribute('data-draining', 'true');
    expect(screen.getByTestId('agent-concurrency-age')).toHaveTextContent(/last known/i);
    expect(screen.getByTestId('agent-concurrency-draining')).toHaveTextContent(/target 2/i);
    const rows = screen.getAllByTestId('executor-slot-row');
    expect(rows).toHaveLength(1);
    expect(rows[0]).toHaveAttribute('data-slot-index', '3');
    expect(rows[0]).toHaveAttribute('data-slot-state', 'orphan');
    expect(within(rows[0]).getByTestId('executor-slot-row-draining')).toHaveTextContent('draining');
  });

  it('renders worker offline without asserting unknown idle slots', () => {
    render(
      <ExecutorSlotPanel
        data={conc({
          active: 0,
          queued: 0,
          stale: true,
          reachable: false,
          has_snapshot: false,
          slots: [],
        })}
      />,
    );
    expect(screen.getByTestId('executor-slot-panel')).toHaveAttribute('data-mode', 'offline');
    expect(screen.getByTestId('agent-concurrency-age')).toHaveTextContent(/worker offline/i);
    expect(screen.queryByTestId('executor-slot-row')).toBeNull();
  });

  it('uses center running fallback for enabled nodata snapshots', () => {
    render(
      <ExecutorSlotPanel
        data={conc({
          active: 0,
          running: 2,
          queued: 0,
          stale: true,
          has_snapshot: false,
          concurrency_enabled: true,
          slots: [],
        })}
      />,
    );
    expect(screen.getByTestId('executor-slot-panel')).toHaveAttribute('data-mode', 'nodata');
    expect(screen.getByTestId('agent-concurrency-slots')).toHaveTextContent('~2/4');
    expect(screen.getByTestId('agent-concurrency-age')).toHaveTextContent(/awaiting live data/i);
  });

  it('does not fabricate slot numbers for legacy slot_stable=false payloads', () => {
    render(
      <ExecutorSlotPanel
        data={conc({
          slot_stable: false,
          slots: undefined,
          executors: [{ executor_id: 'exec-legacy-123456789', task_id: 'task-1', cli: 'codex', model: 'gpt-5', state: 'running', started_at: '2026-05-24T01:55:00Z' }],
        })}
      />,
    );
    expect(screen.getByTestId('executor-slot-panel')).toHaveAttribute('data-slot-stable', 'false');
    expect(screen.getByTestId('executor-slot-legacy')).toHaveTextContent(/Slot numbers unavailable/i);
    expect(screen.queryByTestId('executor-slot-index')).toBeNull();
    expect(screen.getByTestId('executor-slot-legacy')).toHaveTextContent('exec 23456789');
  });

  it('exposes accessible labels for the panel and each stable slot row', () => {
    render(
      <ExecutorSlotPanel
        title="Live executor slots"
        data={conc({ slots: [{ slot_index: 0, state: 'running', executor_id: 'exec-0' }] })}
      />,
    );
    expect(screen.getByLabelText('Live executor slots')).toBeInTheDocument();
    expect(screen.getByLabelText('Executor slot 0 is Running')).toBeInTheDocument();
  });
});

describe('AgentSlotMetricBadge', () => {
  afterEach(() => cleanup());

  it('shows live list metrics when present and fallback metrics when absent', () => {
    const { rerender } = render(<AgentSlotMetricBadge agent={{ active: 2, slot_count: 4, slot_stable: true }} />);
    expect(screen.getByTestId('agent-slot-metric')).toHaveTextContent('2/4');
    expect(screen.getByTestId('agent-slot-metric')).toHaveAttribute('data-approximate', 'false');
    rerender(<AgentSlotMetricBadge agent={{ running_tasks: 2, effective_concurrency_cap: 4 }} />);
    expect(screen.getByTestId('agent-slot-metric')).toHaveTextContent('~2/4');
    expect(screen.getByTestId('agent-slot-metric')).toHaveAttribute('data-approximate', 'true');
  });
});
