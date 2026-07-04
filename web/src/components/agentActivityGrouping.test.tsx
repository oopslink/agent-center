import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import type { AgentActivityEvent } from '@/api/types';
import { groupActivity } from './agentActivityGrouping';
import { CheckingGroup, ExecutorProgressGroup } from './AgentActivityRow';

const ev = (id: string, event_type: string, time = '01:00'): AgentActivityEvent =>
  ({ id, agent_id: 'A1', event_type, payload: '{}', occurred_at: `2026-05-24T${time}:00Z` }) as AgentActivityEvent;

// an executor.progress heartbeat (event_type='lifecycle', payload.event=executor.progress).
const prog = (
  id: string,
  execId: string,
  opts: { state?: string; taskRef?: string; time?: string; detail?: string } = {},
): AgentActivityEvent =>
  ({
    id,
    agent_id: 'A1',
    event_type: 'lifecycle',
    payload: JSON.stringify({
      event: 'executor.progress',
      executor_id: execId,
      state: opts.state ?? 'running',
      ...(opts.taskRef ? { task_ref: opts.taskRef } : {}),
      ...(opts.detail ? { detail: opts.detail } : {}),
    }),
    occurred_at: `2026-05-24T${opts.time ?? '01:00'}:00Z`,
  }) as AgentActivityEvent;

// The group range renders local wall-clock (toLocaleTimeString), so derive the
// expected text the same way — keeps the assertion timezone-independent.
const localHM = (time: string): string =>
  new Date(`2026-05-24T${time}:00Z`).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });

describe('groupActivity (#274 Checking fold)', () => {
  it('folds consecutive checking events into a group; keeps non-checking separate', () => {
    const items = groupActivity([
      ev('1', 'result'), // output (non-checking)
      ev('2', 'system_init'),
      ev('3', 'system_init'),
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
    // page1 [system_init x2] + page2 [system_init] concatenated → one "× 3" group.
    const items = groupActivity([ev('a', 'system_init'), ev('b', 'system_init'), ev('c', 'system_init')]);
    expect(items).toHaveLength(1);
    expect(items[0].kind).toBe('checking-group');
    if (items[0].kind === 'checking-group') expect(items[0].events).toHaveLength(3);
  });

  // T500: message_delivered must NOT be folded into a checking group even when
  // surrounded by checking events — it maps to CAT_DELIVERED (not CAT_CHECKING).
  it('does not fold message_delivered into a checking group', () => {
    const evt = (id: string, t: string): AgentActivityEvent => ({
      id,
      agent_id: 'ag1',
      event_type: t,
      occurred_at: '2026-06-27T00:00:00Z',
      payload: '{}',
    });
    const items = groupActivity([evt('1', 'system_init'), evt('2', 'message_delivered'), evt('3', 'rate_limit')]);
    // delivered must surface as its own 'event' item, not swallowed by a checking-group.
    expect(items.some((i) => i.kind === 'event' && i.event.event_type === 'message_delivered')).toBe(true);
  });
});

describe('groupActivity (v2.31.1 executor.progress fold)', () => {
  it('folds consecutive same-executor progress heartbeats into one group', () => {
    const items = groupActivity([
      prog('1', 'exec-A'),
      prog('2', 'exec-A'),
      prog('3', 'exec-A'),
    ]);
    expect(items.map((i) => i.kind)).toEqual(['executor-progress-group']);
    if (items[0].kind === 'executor-progress-group') expect(items[0].events).toHaveLength(3);
  });

  it('renders a LONE progress heartbeat as a normal event (no "× 1" group)', () => {
    const items = groupActivity([ev('1', 'result'), prog('2', 'exec-A'), ev('3', 'result')]);
    expect(items.map((i) => i.kind)).toEqual(['event', 'event', 'event']);
  });

  it('starts a fresh group when the executor_id changes', () => {
    const items = groupActivity([
      prog('1', 'exec-A'),
      prog('2', 'exec-A'),
      prog('3', 'exec-B'),
      prog('4', 'exec-B'),
    ]);
    expect(items.map((i) => i.kind)).toEqual([
      'executor-progress-group',
      'executor-progress-group',
    ]);
  });

  it('does not fold executor.start / .stop — only .progress heartbeats', () => {
    const stop: AgentActivityEvent = {
      id: 's',
      agent_id: 'A1',
      event_type: 'lifecycle',
      payload: JSON.stringify({ event: 'executor.stop', executor_id: 'exec-A' }),
      occurred_at: '2026-05-24T01:00:00Z',
    } as AgentActivityEvent;
    const items = groupActivity([stop, prog('1', 'exec-A'), prog('2', 'exec-A')]);
    expect(items.map((i) => i.kind)).toEqual(['event', 'executor-progress-group']);
  });

  it('a non-executor event flushes the progress run', () => {
    const items = groupActivity([prog('1', 'exec-A'), prog('2', 'exec-A'), ev('3', 'result')]);
    expect(items.map((i) => i.kind)).toEqual(['executor-progress-group', 'event']);
  });
});

describe('ExecutorProgressGroup (v2.31.1)', () => {
  afterEach(() => cleanup());

  it('shows the executor summary, "× N", state, time range + expandable raw rows', () => {
    // newest-first: [0]=03:00 latest, [2]=01:00 earliest. No taskRef → the link
    // (which needs a QueryClient) isn't mounted, keeping the test provider-free.
    const events = [
      prog('3', 'exec-2b8d4fe9', { time: '03:00' }),
      prog('2', 'exec-2b8d4fe9', { time: '02:00' }),
      prog('1', 'exec-2b8d4fe9', { time: '01:00' }),
    ];
    render(
      <ul>
        <ExecutorProgressGroup events={events} />
      </ul>,
    );
    const toggle = screen.getByTestId('agent-activity-executor-toggle');
    // "Executor (exec 2b8d4fe9) is Running × 3" — shortExecId trims to the tail.
    expect(toggle).toHaveTextContent('2b8d4fe9');
    expect(toggle).toHaveTextContent('Running');
    expect(toggle).toHaveTextContent('× 3');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByTestId('agent-activity-executor-group')).toHaveTextContent(
      `${localHM('01:00')}–${localHM('03:00')}`,
    );
    expect(screen.queryByTestId('agent-activity-executor-expanded')).toBeNull();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const region = screen.getByTestId('agent-activity-executor-expanded');
    expect(toggle).toHaveAttribute('aria-controls', region.id);
    expect(screen.getAllByTestId('agent-activity-row')).toHaveLength(3);
  });

  // T880: the folded row surfaces the LATEST heartbeat's sanitized "what it's doing"
  // note (detail), so an operator sees the current action without expanding.
  it('surfaces the latest heartbeat detail ("跑 go test") on the folded summary', () => {
    const events = [
      prog('3', 'exec-2b8d4fe9', { time: '03:00', detail: '跑 go test' }), // latest
      prog('2', 'exec-2b8d4fe9', { time: '02:00', detail: '读 task.go' }),
      prog('1', 'exec-2b8d4fe9', { time: '01:00' }),
    ];
    render(
      <ul>
        <ExecutorProgressGroup events={events} />
      </ul>,
    );
    expect(screen.getByTestId('agent-activity-executor-detail')).toHaveTextContent('跑 go test');
    // the older heartbeat's detail is NOT the one shown on the fold
    expect(screen.getByTestId('agent-activity-executor-summary')).not.toHaveTextContent('读 task.go');
  });

  it('omits the detail chip when the latest heartbeat has no activity note', () => {
    render(
      <ul>
        <ExecutorProgressGroup events={[prog('1', 'exec-2b8d4fe9')]} />
      </ul>,
    );
    expect(screen.queryByTestId('agent-activity-executor-detail')).toBeNull();
  });

  // oopslink DM 2026-07-04: the task in the summary must render as its HUMAN
  // org_ref ("T879") link (task FIRST, then exec), not the raw task-<id>. Needs
  // the org resolvers → QueryClient + OrgContext + msw (mirrors ActivityRefText).
  it('renders the task as its "T879" org_ref label linking to the task detail page', async () => {
    server.use(
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [
            {
              id: 'task-4b2339ec',
              org_ref: 'T879',
              project: { id: 'proj-x', name: 'X' },
              title: 't',
              status: 'running',
              assignee: null,
              updated_at: 'x',
              created_at: 'x',
            },
          ],
          total: 1,
        }),
      ),
    );
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <OrgContext.Provider value={{ slug: 'test-org', orgId: 'O', orgName: 'Test Org' }}>
          <ul>
            <ExecutorProgressGroup
              events={[prog('1', 'exec-2b8d4fe9', { taskRef: 'task-4b2339ec' })]}
            />
          </ul>
        </OrgContext.Provider>
      </QueryClientProvider>,
    );
    const link = await screen.findByTestId('activity-executor-task-link');
    expect(link.tagName).toBe('A');
    expect(link).toHaveTextContent('T879');
    expect(link).not.toHaveTextContent('task-4b2339ec');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-4b2339ec');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('data-task-ref', 'task-4b2339ec');
    // task renders BEFORE the exec id in the summary (T879, exec …).
    const summary = screen.getByTestId('agent-activity-executor-summary').textContent ?? '';
    expect(summary.indexOf('T879')).toBeGreaterThanOrEqual(0);
    expect(summary.indexOf('T879')).toBeLessThan(summary.indexOf('exec'));
  });
});

describe('CheckingGroup (#274)', () => {
  afterEach(() => cleanup());

  it('shows "× N" + time range + a disclosure that expands to the raw events', () => {
    // newest-first (ULID DESC): [0]=03:00 latest, [2]=01:00 earliest.
    const events = [ev('3', 'system_init', '03:00'), ev('2', 'system_init', '02:00'), ev('1', 'system_init', '01:00')];
    render(
      <ul>
        <CheckingGroup events={events} />
      </ul>,
    );
    const toggle = screen.getByTestId('agent-activity-checking-toggle');
    expect(toggle).toHaveTextContent('Checking messages × 3');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveAttribute('aria-label', 'Checking messages, 3 events, collapsed');
    // earliest–latest time range, rendered in the viewer's local timezone.
    expect(screen.getByTestId('agent-activity-checking-group')).toHaveTextContent(
      `${localHM('01:00')}–${localHM('03:00')}`,
    );
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
