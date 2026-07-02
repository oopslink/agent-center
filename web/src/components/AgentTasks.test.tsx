import { readFileSync } from 'node:fs';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { AgentTasks } from './AgentTasks';
import type { AgentTask, AgentTaskStatus } from '@/api/types';

const wi = (id: string, status: AgentTaskStatus, extra: Partial<AgentTask> = {}): AgentTask => ({
  id,
  agent_id: 'A1',
  task_ref: `pm://tasks/${id}`,
  status,
  interactions: 0,
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

function stub(items: AgentTask[]) {
  server.use(http.get('/api/agents/:id/tasks', () => HttpResponse.json({ tasks: items })));
}

// Default /concurrency handler so tests that don't care about the overlay don't
// hit an unhandled request; overlay tests override it via stubConcurrency (a later
// server.use wins). 404 → the overlay query errors → no summary (degrade path).
beforeEach(() => {
  server.use(http.get('/api/agents/:id/concurrency', () => HttpResponse.json({ message: 'no snapshot' }, { status: 404 })));
  // v2.24.x: the Tasks tab now embeds AgentContextPanel (Current task + owning
  // Plan) at the top, which fetches /projects/:pid/plans for any task carrying a
  // project_id. Default it to empty so these AgentTasks-focused tests don't hit an
  // unhandled request (onUnhandledRequest:'error'); AgentContextPanel's own tests
  // cover plan resolution.
  server.use(http.get('/api/projects/:pid/plans', () => HttpResponse.json({ plans: [] })));
});

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AgentTasks agentId="A1" />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AgentTasks (#228 PR(d); v2.14.0 I14)', () => {
  afterEach(() => cleanup());

  it('shows the Dev empty-state copy when there are no tasks (no +New)', async () => {
    stub([]);
    wrap();
    await waitFor(() =>
      expect(screen.getByTestId('agent-workitems-empty')).toHaveTextContent(
        /Tasks appear here when they are assigned/i,
      ),
    );
    // (A) read-only: no create affordance anywhere.
    expect(screen.queryByText('+ New')).not.toBeInTheDocument();
  });

  it('summarises counts by bucket (Total / In Progress / Pending / Done / Blocked)', async () => {
    stub([
      wi('a1', 'active'),
      wi('a2', 'active'),
      wi('q1', 'queued'),
      wi('d1', 'done'),
      wi('f1', 'failed'),
      wi('w1', 'waiting_input'),
    ]);
    wrap();
    const summary = await screen.findByTestId('agent-workitems-summary');
    expect(summary).toHaveTextContent('6 Total');
    expect(summary).toHaveTextContent('2 In Progress');
    expect(summary).toHaveTextContent('1 Pending');
    expect(summary).toHaveTextContent('1 Done');
    // blocked = failed + waiting_input.
    expect(summary).toHaveTextContent('2 Blocked');
  });

  it('maps paused → its own "Paused" bucket, count and chip (v2.8.1 #278 D scheduling)', async () => {
    stub([wi('a1', 'active'), wi('p1', 'paused'), wi('p2', 'paused'), wi('q1', 'queued')]);
    wrap();
    const summary = await screen.findByTestId('agent-workitems-summary');
    expect(summary).toHaveTextContent('4 Total');
    expect(summary).toHaveTextContent('1 In Progress');
    // paused is a distinct, visible bucket — NOT collapsed into pending/blocked.
    expect(summary).toHaveTextContent('2 Paused');
    expect(summary).toHaveTextContent('1 Pending');
    // the row status chip shows the "Paused" label (not a bare "paused" fallback).
    const chips = screen.getAllByTestId('agent-workitem-status').map((el) => el.textContent);
    expect(chips).toContain('Paused');
  });

  it('paused/queued chips carry both-mode AA classes (dark: variant + queued light fix)', async () => {
    // v2.8.1 dark-mode AA fix: fixed mid-tone text on alpha-tint is dark-on-dark
    // in dark mode (FAIL); add lighter dark: variants. queued also FAILed light
    // (orange-600 3.21:1, pre-existing #228) → orange-700.
    stub([wi('p1', 'paused'), wi('q1', 'queued')]);
    wrap();
    await screen.findByTestId('agent-workitems-summary');
    const cls = (label: string) =>
      screen
        .getAllByTestId('agent-workitem-status')
        .find((el) => el.textContent === label)
        ?.querySelector('span')?.className ?? '';
    expect(cls('Paused')).toContain('dark:text-violet-400');
    expect(cls('Pending')).toContain('text-orange-700'); // raw-color-ok: asserts the component's dark-paired light fix (not 600)
    expect(cls('Pending')).toContain('dark:text-orange-400');
    expect(cls('Pending')).not.toContain('text-orange-600'); // raw-color-ok: regression guard, old failing shade
  });

  it('filters rows to the Paused bucket', async () => {
    stub([wi('a1', 'active'), wi('p1', 'paused'), wi('q1', 'queued')]);
    wrap();
    await screen.findByTestId('agent-workitems-summary');
    fireEvent.change(screen.getByTestId('agent-workitems-filter-status'), { target: { value: 'paused' } });
    const rows = screen.getAllByTestId('agent-workitem-row');
    expect(rows).toHaveLength(1);
    expect(rows[0].getAttribute('data-status')).toBe('paused');
  });

  it('renders the columns: FULL id when no org_ref (T126: no #id-tail hash), Task type, "—" priority, mapped status', async () => {
    stub([wi('abcdef123456', 'active')]);
    wrap();
    const row = await screen.findByTestId('agent-workitem-row');
    expect(row).toHaveAttribute('data-status', 'active');
    // T126: no org_ref → the FULL id is shown verbatim (never a #id-tail hash),
    // with the full id also on hover.
    const id = screen.getByTestId('agent-workitem-id');
    expect(id).toHaveTextContent('abcdef123456');
    expect(id).not.toHaveTextContent('#');
    expect(id).toHaveAttribute('title', 'abcdef123456');
    // Type fallback chip + Priority fallback.
    expect(screen.getByTestId('agent-workitem-type')).toHaveTextContent('Task');
    expect(screen.getByTestId('agent-workitem-priority')).toHaveTextContent('—');
    // active → "In Progress".
    expect(screen.getByTestId('agent-workitem-status')).toHaveTextContent('In Progress');
  });

  it('shows the task org_ref (T<n>) in the ID column when present, id-tail only as fallback (T100)', async () => {
    stub([wi('wi-aab6eb82', 'active', { org_ref: 'T84' })]);
    wrap();
    await screen.findByTestId('agent-workitem-row');
    const id = screen.getByTestId('agent-workitem-id');
    // org_ref (T84) replaces the id-tail handle (#b6eb82) the owner reported.
    expect(id).toHaveTextContent('T84');
    expect(id).not.toHaveTextContent('#');
    // full work-item id still on hover (#192 zero-raw-id-as-chrome).
    expect(id).toHaveAttribute('title', 'wi-aab6eb82');
  });

  it('gives near-simultaneous ULIDs distinct handles (tail, not shared timestamp prefix)', async () => {
    // Two ULIDs created in the same ms share the leading timestamp; only the
    // trailing random segment differs — the handle must reflect that.
    stub([wi('01KT8DABCD0001', 'active'), wi('01KT8DABCD0002', 'queued')]);
    wrap();
    await waitFor(() => expect(screen.getAllByTestId('agent-workitem-id')).toHaveLength(2));
    const handles = screen.getAllByTestId('agent-workitem-id').map((n) => n.textContent);
    expect(new Set(handles).size).toBe(2); // distinct
  });

  it('links the title to its task when resolved (#206)', async () => {
    stub([wi('w9', 'active', { task_id: 'task-9', task_title: 'Build login flow', project_id: 'proj-x' })]);
    wrap();
    const link = await screen.findByTestId('agent-workitem-task');
    expect(link).toHaveTextContent('Build login flow');
    expect(link.getAttribute('href')).toContain('/projects/proj-x/tasks/task-9');
  });

  // v2.24.x (@oopslink): the Current task + owning Plan block (formerly the
  // right-hand col④ sidebar) now renders inline at the TOP of the Tasks tab.
  it('embeds the Current task + Plan context block above the table', async () => {
    stub([wi('w9', 'active', { task_id: 'task-9', task_title: 'Build login flow', project_id: 'proj-x' })]);
    wrap();
    // it surfaces the agent's current (active) task…
    const card = await screen.findByTestId('agent-context-task');
    expect(card).toHaveTextContent('Build login flow');
    const panel = screen.getByTestId('agent-context-panel');
    // …and sits above the task table (DOM order: panel before the table).
    const table = await screen.findByTestId('agent-workitems-table');
    expect(panel.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('falls back to "Task" (no link) when the task is unresolved', async () => {
    stub([wi('w1', 'queued')]);
    wrap();
    const row = await screen.findByTestId('agent-workitem-row');
    expect(row).toHaveTextContent('Task');
    expect(screen.queryByTestId('agent-workitem-task')).not.toBeInTheDocument();
  });

  it('filters rows by status bucket', async () => {
    stub([wi('a1', 'active'), wi('d1', 'done'), wi('q1', 'queued')]);
    wrap();
    await waitFor(() => expect(screen.getAllByTestId('agent-workitem-row')).toHaveLength(3));
    fireEvent.change(screen.getByTestId('agent-workitems-filter-status'), { target: { value: 'done' } });
    await waitFor(() => expect(screen.getAllByTestId('agent-workitem-row')).toHaveLength(1));
    expect(screen.getByTestId('agent-workitem-row')).toHaveAttribute('data-status', 'done');
  });

  it('shows a no-match note when a filter excludes everything', async () => {
    stub([wi('a1', 'active')]);
    wrap();
    await screen.findByTestId('agent-workitem-row');
    fireEvent.change(screen.getByTestId('agent-workitems-filter-status'), { target: { value: 'blocked' } });
    await waitFor(() => expect(screen.getByTestId('agent-workitems-no-match')).toBeInTheDocument());
    expect(screen.queryByTestId('agent-workitem-row')).not.toBeInTheDocument();
  });
});

// T593: live concurrency overlay on the Tasks tab.
function stubConcurrency(data: Record<string, unknown>) {
  server.use(http.get('/api/agents/:id/concurrency', () => HttpResponse.json(data)));
}
const inProg = (taskId: string) =>
  wi('w1', 'active', { task_id: taskId, task_title: 'Build it', project_id: 'p' });

describe('AgentTasks — concurrency overlay (T593)', () => {
  afterEach(() => cleanup());

  it('slots summary: active/cap occupancy + queued + snapshot age (not stale)', async () => {
    stub([inProg('t1')]);
    stubConcurrency({ agent_id: 'A1', cap: 3, active: 2, queued: 1, stale: false, snapshot_age_ms: 1000, executors: [] });
    wrap();
    const sum = await screen.findByTestId('agent-concurrency-summary');
    expect(sum).toHaveAttribute('data-stale', 'false');
    expect(screen.getByTestId('agent-concurrency-slots')).toHaveTextContent('2/3');
    expect(screen.getByTestId('agent-concurrency-queued')).toHaveTextContent('1 queued');
    expect(screen.getByTestId('agent-concurrency-age')).toHaveTextContent(/updated/i);
  });

  it('in-progress row overlays the executor joined by task_id (cli·model / slot / elapsed / heartbeat)', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 3, active: 1, queued: 0, stale: false, snapshot_age_ms: 1000,
      executors: [{ executor_id: 'e1', task_id: 't1', cli: 'claude-code', model: 'sonnet', state: 'running', started_at: '2026-05-24T01:55:00Z' }],
    });
    wrap();
    const overlay = await screen.findByTestId('agent-task-overlay');
    expect(within(overlay).getByTestId('agent-task-cli-model')).toHaveTextContent('claude-code · sonnet');
    expect(within(overlay).getByTestId('agent-task-slot')).toHaveTextContent('slot 1');
    expect(within(overlay).getByTestId('agent-task-elapsed')).toBeInTheDocument();
    expect(within(overlay).getByTestId('agent-task-heartbeat')).toBeInTheDocument();
  });

  it('orphan executor shows the orphan·monitored badge', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 3, active: 1, queued: 0, stale: false, snapshot_age_ms: 1000,
      executors: [{ executor_id: 'e1', task_id: 't1', cli: 'claude-code', model: 'sonnet', state: 'orphan-monitored', started_at: '2026-05-24T01:55:00Z' }],
    });
    wrap();
    expect(await screen.findByTestId('agent-task-orphan')).toBeInTheDocument();
  });

  // T606: expired snapshot (worker online, snapshot present but past TTL) → "last
  // known", NOT "worker unreachable". Row overlay still marked stale, list visible.
  it('expired snapshot: summary shows last-known; row overlay stale; list visible; heartbeat hidden', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 3, active: 1, queued: 0, stale: true, reachable: true, has_snapshot: true, snapshot_age_ms: 74000,
      executors: [{ executor_id: 'e1', task_id: 't1', cli: 'claude-code', model: 'sonnet', state: 'running', started_at: '2026-05-24T01:55:00Z' }],
    });
    wrap();
    const sum = await screen.findByTestId('agent-concurrency-summary');
    expect(sum).toHaveAttribute('data-mode', 'expired');
    expect(sum).toHaveTextContent(/last known/i);
    expect(sum).not.toHaveTextContent(/worker unreachable/i);
    expect(screen.getByTestId('agent-task-overlay-stale')).toBeInTheDocument();
    expect(screen.getByTestId('agent-workitems-table')).toBeInTheDocument(); // list always visible
    expect(screen.queryByTestId('agent-task-heartbeat')).toBeNull(); // no live heartbeat when stale
  });

  // T606: worker truly OFFLINE → "worker offline" (the only case that blames the worker).
  it('worker offline: summary shows offline state', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 3, active: 0, queued: 0, stale: true, reachable: false, has_snapshot: false, snapshot_age_ms: 0, executors: [],
    });
    wrap();
    const sum = await screen.findByTestId('agent-concurrency-summary');
    expect(sum).toHaveAttribute('data-mode', 'offline');
    expect(sum).toHaveTextContent(/worker offline/i);
    expect(screen.getByTestId('agent-workitems-table')).toBeInTheDocument();
  });

  // Back-compat (no concurrency_enabled from a pre-fix Center): no snapshot → neutral
  // 'nodata' (awaiting live data), NOT "worker unreachable" (the original I54 misreport).
  it('no snapshot, no enabled flag: neutral awaiting-data state, not "unreachable"', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 3, active: 0, queued: 0, stale: true, reachable: true, has_snapshot: false, snapshot_age_ms: 0, executors: [],
    });
    wrap();
    const sum = await screen.findByTestId('agent-concurrency-summary');
    expect(sum).toHaveAttribute('data-mode', 'nodata');
    expect(sum).toHaveTextContent(/awaiting live slot data/i);
    expect(sum).not.toHaveTextContent(/unreachable/i);
    expect(sum).not.toHaveTextContent(/offline/i);
    expect(screen.getByTestId('agent-workitems-table')).toBeInTheDocument();
  });

  // issue-c44ccf6b: a genuinely single-active agent (concurrency_enabled=false) is the
  // HONEST "concurrency not active" case → mode 'disabled'. The center-known running
  // count is shown as the occupancy fallback (~1/1), never a bare "—".
  it('single-active agent (concurrency disabled): honest "not active" + running fallback', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 1, active: 0, queued: 0, running: 1, concurrency_enabled: false,
      stale: true, reachable: true, has_snapshot: false, snapshot_age_ms: 0, executors: [],
    });
    wrap();
    const sum = await screen.findByTestId('agent-concurrency-summary');
    expect(sum).toHaveAttribute('data-mode', 'disabled');
    expect(sum).toHaveTextContent(/concurrency not active/i);
    expect(screen.getByTestId('agent-concurrency-slots')).toHaveTextContent('~1/1'); // fallback, not "—"
  });

  // issue-c44ccf6b core fix: an ENABLED, running agent with no fresh snapshot must NOT
  // be labeled "concurrency not active" — it's 'nodata' (awaiting live data) and shows
  // the center-known running count as occupancy (~2/3), not "—".
  it('enabled but no snapshot: awaiting-data (NOT "not active") + running fallback', async () => {
    stub([inProg('t1')]);
    stubConcurrency({
      agent_id: 'A1', cap: 3, active: 0, queued: 0, running: 2, concurrency_enabled: true,
      stale: true, reachable: true, has_snapshot: false, snapshot_age_ms: 0, executors: [],
    });
    wrap();
    const sum = await screen.findByTestId('agent-concurrency-summary');
    expect(sum).toHaveAttribute('data-mode', 'nodata');
    expect(sum).toHaveTextContent(/awaiting live slot data/i);
    expect(sum).not.toHaveTextContent(/concurrency not active/i);
    expect(screen.getByTestId('agent-concurrency-slots')).toHaveTextContent('~2/3'); // fallback, not "—"
  });

  it('pending row shows the queued-for-slot hint', async () => {
    stub([wi('q1', 'queued', { task_id: 't9', task_title: 'Q', project_id: 'p' })]);
    stubConcurrency({ agent_id: 'A1', cap: 3, active: 0, queued: 1, stale: false, snapshot_age_ms: 1000, executors: [] });
    wrap();
    expect(await screen.findByTestId('agent-task-queued')).toHaveTextContent(/Queued for a slot/i);
  });

  it('degrades gracefully: /concurrency error → no overlay, task list intact', async () => {
    stub([inProg('t1')]);
    server.use(http.get('/api/agents/:id/concurrency', () => HttpResponse.json({ message: 'nope' }, { status: 500 })));
    wrap();
    await screen.findByTestId('agent-workitem-row');
    expect(screen.queryByTestId('agent-concurrency-summary')).toBeNull();
    expect(screen.queryByTestId('agent-task-overlay')).toBeNull();
  });
});

describe('tailwind dark mode config', () => {
  it('is class-based so dark: variants align with the :root.dark token trigger', () => {
    // The chip dark: variants (above) only work if Tailwind gates dark: by the
    // `.dark` CLASS (matching index.css :root.dark / <html class="dark">), NOT
    // the prefers-color-scheme media default. Without darkMode:'class' the dark:
    // variants misfire on OS-dark+app-light and won't fire under the dual-theme
    // toggle (10th). Load-bearing for every dark: variant — guard it.
    // vitest runs with cwd = web/, where tailwind.config.js lives.
    const cfg = readFileSync('tailwind.config.js', 'utf8');
    expect(cfg).toMatch(/darkMode:\s*['"](class|selector)['"]/);
  });
});
