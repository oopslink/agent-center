import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Environment from './Environment';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

// v2.7 #164: Environment now sources workers from /api/fleet (canonical
// workforce.Worker + active work-item count), with agents grouped per worker +
// work items + pending issues + transfers.
const fleetSnapshot = (workers: unknown[], extra: Record<string, unknown> = {}) => ({
  generated_at: '2026-05-24T02:00:00Z',
  workers,
  work_items: [],
  pending_issues: [],
  warnings: [],
  ...extra,
});

const fleetWorker = (id: string, extra: Record<string, unknown> = {}) => ({
  worker_id: id,
  name: id,
  status: 'online',
  active_count: 0,
  last_heartbeat_at: '2026-05-24T02:00:00Z',
  ...extra,
});

const agent = (id: string, workerID: string, extra: Record<string, unknown> = {}) => ({
  id,
  organization_id: 'O-1',
  name: id,
  description: '',
  model: 'claude-opus',
  cli: 'claudecode',
  env_vars: {},
  skills: [],
  worker_id: workerID,
  lifecycle: 'running',
  availability: 'busy',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

describe('Environment page (#164 merged Fleet+Environment)', () => {
  afterEach(() => cleanup());

  it('renders workers with status + active count and agents grouped by worker', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(
          fleetSnapshot([
            fleetWorker('w-1', { status: 'online', active_count: 3 }),
            fleetWorker('w-2', { status: 'offline', active_count: 0 }),
          ]),
        ),
      ),
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent('bot-a', 'w-1'), agent('bot-b', 'w-1')] }),
      ),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);

    await waitFor(() => expect(screen.getAllByTestId('environment-worker')).toHaveLength(2));
    const rows = screen.getAllByTestId('environment-worker');
    expect(rows[0]).toHaveAttribute('data-worker-id', 'w-1');
    expect(rows[0]).toHaveAttribute('data-status', 'online');
    expect(rows[0]).toHaveTextContent('3 active'); // active_count from /api/fleet

    // w-1 has its two agents grouped under it; w-2 has none.
    expect(screen.getAllByTestId('environment-agent')).toHaveLength(2);
    expect(screen.getByText('bot-a')).toBeInTheDocument();
    expect(screen.getByTestId('environment-worker-noagents')).toBeInTheDocument();
  });

  it('shows the empty state when there are no workers', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-workers-empty')).toBeInTheDocument());
    expect(screen.getByTestId('environment-workers-empty')).toHaveTextContent(/worker/i);
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({ error: 'fleet_error', message: 'db down' }, { status: 500 }),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-error')).toHaveTextContent(/db down/),
    );
  });

  // v2.8.1 #281: work items + issues now live behind the Activity tablist. The
  // default "All" tab shows the union; the dedicated tabs show one stream each.
  it('renders work items + pending issues from the fleet snapshot in the Activity All tab', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(
          fleetSnapshot([fleetWorker('w-1', { active_count: 1 })], {
            work_items: [
              { work_item_id: 'wi-1', task_id: 'task-1', agent_id: 'bot-a', status: 'active', current_activity: 'coding' },
            ],
            pending_issues: [{ issue_id: 'iss-1', title: 'Fix login' }],
          }),
        ),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    // Default tab = All → union of work items + issues shows immediately.
    await waitFor(() => expect(screen.getByTestId('environment-activity-all-list')).toBeInTheDocument());
    expect(screen.getByText('task-1')).toBeInTheDocument();
    expect(screen.getByText('Fix login')).toBeInTheDocument();
    expect(screen.getAllByTestId('environment-activity-all-row')).toHaveLength(2);

    // The dedicated Work Items tab shows the work-item rows.
    fireEvent.click(screen.getByTestId('environment-activity-tab-work_items'));
    await waitFor(() => expect(screen.getByTestId('environment-workitem-row')).toBeInTheDocument());
    expect(screen.getByText('task-1')).toBeInTheDocument();

    // The dedicated Issues tab shows the issue rows.
    fireEvent.click(screen.getByTestId('environment-activity-tab-issues'));
    await waitFor(() => expect(screen.getByTestId('environment-issue-row')).toBeInTheDocument());
    expect(screen.getByText('Fix login')).toBeInTheDocument();
  });

  it('renders in-flight transfer sessions in the Activity Transfers tab', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () =>
        HttpResponse.json({
          transfer_sessions: [
            {
              id: 't-1',
              file_uri: 'ac://files/abc',
              transfer_uri: 'ac://transfers/t-1',
              direction: 'upload',
              status: 'open',
              scope: 'project',
              scope_id: 'p-1',
              content_type: 'application/pdf',
              size: 2048,
              created_by: 'user:hayang',
              created_at: '2026-05-24T01:00:00Z',
              expires_at: '2026-05-24T02:00:00Z',
            },
          ],
        }),
      ),
    );
    wrap(<Environment />);
    // Transfers also appear in the default All stream.
    await waitFor(() =>
      expect(screen.getByTestId('environment-activity-all-list')).toBeInTheDocument(),
    );
    // The dedicated Transfers tab shows the table.
    fireEvent.click(screen.getByTestId('environment-activity-tab-transfers'));
    await waitFor(() => expect(screen.getByTestId('transfer-row')).toBeInTheDocument());
    const row = screen.getByTestId('transfer-row');
    expect(row).toHaveAttribute('data-scope', 'project');
    expect(row).toHaveTextContent('upload');
    expect(row).toHaveTextContent('project/p-1');
  });

  // v2.8.1 #281: stats strip — four big-number cells derived from the snapshots.
  it('renders the four stats cells with derived counts', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(
          fleetSnapshot(
            [
              fleetWorker('w-1', { status: 'online' }),
              fleetWorker('w-2', { status: 'offline' }),
            ],
            {
              work_items: [
                { work_item_id: 'wi-1', agent_id: 'bot-a', status: 'active' },
                { work_item_id: 'wi-2', agent_id: 'bot-a', status: 'active' },
              ],
              pending_issues: [{ issue_id: 'iss-1', title: 'Fix login' }],
            },
          ),
        ),
      ),
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [
            agent('bot-a', 'w-1', { lifecycle: 'running' }),
            agent('bot-b', 'w-1', { lifecycle: 'stopped' }),
          ],
        }),
      ),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-stat-workers-online-value')).toHaveTextContent('1'),
    );
    // Agents-Running counts only lifecycle==='running' and goes green when >0.
    const running = screen.getByTestId('environment-stat-agents-running-value');
    expect(running).toHaveTextContent('1');
    expect(running.className).toContain('text-success');
    expect(screen.getByTestId('environment-stat-work-items-value')).toHaveTextContent('2');
    expect(screen.getByTestId('environment-stat-pending-issues-value')).toHaveTextContent('1');
  });

  it('renders 0 (gray) for Agents Running when none are running', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([fleetWorker('w-1')]))),
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent('bot-a', 'w-1', { lifecycle: 'stopped' })] }),
      ),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-stat-agents-running-value')).toHaveTextContent('0'),
    );
    const running = screen.getByTestId('environment-stat-agents-running-value');
    expect(running.className).toContain('text-text-muted');
  });

  // v2.8.1 #281: worker card has three sub-sections (header / CLI / Agents) and
  // a status dot; agent rows carry CLI + model badges.
  it('renders the worker status dot and per-agent CLI + model badges', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(fleetSnapshot([fleetWorker('w-1', { status: 'online' })])),
      ),
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [agent('bot-a', 'w-1', { cli: 'claude-code', model: 'claude-sonnet-4' })],
        }),
      ),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-agent')).toBeInTheDocument());
    const cli = screen.getByTestId('environment-agent-cli');
    expect(cli).toHaveAttribute('data-cli', 'claude-code');
    expect(cli).toHaveTextContent('claude-code');
    const model = screen.getByTestId('environment-agent-model');
    expect(model).toHaveAttribute('data-model', 'claude-sonnet-4');
    expect(model).toHaveTextContent('claude-sonnet-4');
    // status surface still present (dot + uppercase text).
    expect(screen.getByTestId('environment-worker-status')).toHaveAttribute('data-status', 'online');
  });

  // v2.8.1 #281: Activity empty state — clock icon + copy when the active tab's
  // stream is empty.
  it('shows the Activity empty state when every stream is empty', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([fleetWorker('w-1')]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-activity-empty')).toBeInTheDocument(),
    );
    expect(screen.getByTestId('environment-activity-empty')).toHaveTextContent('No active operations');
  });

  // WAI-ARIA tablist contract (#273 pattern reused).
  it('exposes the Activity tablist with aria-selected and arrow-key roving', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([fleetWorker('w-1')]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-activity-tabs')).toBeInTheDocument());
    const all = screen.getByTestId('environment-activity-tab-all');
    expect(all).toHaveAttribute('role', 'tab');
    expect(all).toHaveAttribute('aria-selected', 'true');
    fireEvent.click(screen.getByTestId('environment-activity-tab-issues'));
    await waitFor(() =>
      expect(screen.getByTestId('environment-activity-tab-issues')).toHaveAttribute(
        'aria-selected',
        'true',
      ),
    );
  });

  // #169: native window.confirm → ConfirmModal for worker remove + install re-mint.
  it('removing a worker opens a confirm modal, then DELETEs on confirm', async () => {
    let deletedId: string | undefined;
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(fleetSnapshot([fleetWorker('w-1', { status: 'online' })])),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
      http.delete('/api/workers/:id', ({ params }) => {
        deletedId = params.id as string;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-worker-remove')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('environment-worker-remove'));
    const modal = await screen.findByTestId('confirm-modal');
    expect(modal).toHaveTextContent('w-1');
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(deletedId).toBe('w-1'));
  });

  it('cancelling the remove confirm modal makes no DELETE', async () => {
    const hit = vi.fn();
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(fleetSnapshot([fleetWorker('w-1', { status: 'online' })])),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
      http.delete('/api/workers/:id', () => {
        hit();
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-worker-remove')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('environment-worker-remove'));
    await screen.findByTestId('confirm-modal');
    fireEvent.click(screen.getByTestId('confirm-modal-cancel'));
    await waitFor(() => expect(screen.queryByTestId('confirm-modal')).toBeNull());
    expect(hit).not.toHaveBeenCalled();
  });

  it('re-mint install opens a confirm modal, then opens the install command modal on confirm', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(fleetSnapshot([fleetWorker('w-1', { status: 'offline' })])),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
      http.post('/api/workers/:id/install-command/re-mint', () =>
        HttpResponse.json({ command: 'install …', expires_at: '2026-05-24T03:00:00Z' }),
      ),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-worker-remint-install')).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByTestId('environment-worker-remint-install'));
    await screen.findByTestId('confirm-modal');
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(screen.getByTestId('install-command-modal')).toBeInTheDocument());
  });

  // #176 (FINDING-C visibility): the worker card shows probed agent-CLI
  // capabilities so the operator sees what each worker discovered (§5 exit).
  it('renders a worker’s detected CLI capabilities with enabled/disabled state', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json(
          fleetSnapshot([
            fleetWorker('w-1', {
              capabilities: [
                { agent_cli: 'claude-code', detected: true, enabled: true, version: '1.2' },
                { agent_cli: 'codex', detected: true, enabled: false },
                { agent_cli: 'opencode', detected: false, enabled: false },
              ],
            }),
          ]),
        ),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() => expect(screen.getByTestId('environment-worker')).toBeInTheDocument());

    const caps = screen.getAllByTestId('environment-worker-capability');
    // Only detected CLIs are shown — opencode (detected=false) is hidden.
    expect(caps).toHaveLength(2);
    const claude = caps.find((c) => c.getAttribute('data-agent-cli') === 'claude-code')!;
    expect(claude).toHaveAttribute('data-enabled', 'true');
    expect(claude).toHaveTextContent('claude-code');
    const codex = caps.find((c) => c.getAttribute('data-agent-cli') === 'codex')!;
    expect(codex).toHaveAttribute('data-enabled', 'false');
    expect(codex).toHaveTextContent(/disabled/);
    // opencode (detected=false) gets no capability chip.
    expect(caps.find((c) => c.getAttribute('data-agent-cli') === 'opencode')).toBeUndefined();
    // v2.7 #181: an explicit note clarifies detected ≠ runnable.
    expect(screen.getByTestId('environment-worker-executable-note')).toHaveTextContent(
      /Executable: claude-code only/,
    );
  });

  it('shows an empty hint when a worker has detected no CLIs', async () => {
    server.use(
      http.get('/api/fleet', () => HttpResponse.json(fleetSnapshot([fleetWorker('w-1')]))),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
      http.get('/api/files/transfers', () => HttpResponse.json({ transfer_sessions: [] })),
    );
    wrap(<Environment />);
    await waitFor(() =>
      expect(screen.getByTestId('environment-worker-nocaps')).toBeInTheDocument(),
    );
  });
});
