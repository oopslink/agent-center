import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Agents from './Agents';

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

const agent = (id: string, extra: Record<string, unknown> = {}) => ({
  id,
  organization_id: 'O-1',
  name: id,
  description: '',
  model: 'claude-opus',
  cli: 'claudecode',
  env_vars: {},
  skills: [],
  worker_id: 'w-1',
  lifecycle: 'stopped',
  availability: 'available',
  created_by: 'user:hayang',
  version: 1,
  created_at: '2026-05-24T01:00:00Z',
  updated_at: '2026-05-24T02:00:00Z',
  ...extra,
});

const seed = [
  agent('bot-1', { name: 'bot-1', lifecycle: 'running', availability: 'busy' }),
  agent('bot-2', { name: 'bot-2', lifecycle: 'stopped', availability: 'available' }),
  agent('bot-3', { name: 'bot-3', lifecycle: 'error', availability: 'unavailable', worker_id: '' }),
];

describe('Agents page', () => {
  beforeEach(() => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: seed })));
  });
  afterEach(() => cleanup());

  it('renders all agents with lifecycle + availability badges; the NAME links to /agents/{id} (T133, no separate Open button)', async () => {
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    expect(screen.getAllByText('bot-1').length).toBeGreaterThanOrEqual(1);

    const badges = screen.getAllByTestId('agent-availability-badge');
    expect(badges[0]).toHaveAttribute('data-availability', 'busy');
    expect(badges[2]).toHaveAttribute('data-availability', 'unavailable');

    // T133: the agent NAME is the open affordance — it is a link to AgentDetail.
    const nameLinks = screen.getAllByTestId('agent-name-link');
    expect(nameLinks[0]).toHaveAttribute('href', '/agents/bot-1');
    expect(nameLinks[0]).toHaveTextContent('bot-1');
    // T133: the separate "Open →" link is removed; Delete stays.
    expect(screen.queryByText(/Open/)).not.toBeInTheDocument();
    expect(screen.getAllByTestId('agent-delete-button')).toHaveLength(3);
  });

  it('shows the add-agent empty state when there are no agents', async () => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: [] })));
    wrap(<Agents />);
    await waitFor(() => expect(screen.getByTestId('agents-empty')).toBeInTheDocument());
    expect(screen.getByTestId('agents-empty')).toHaveTextContent(/Add Agent/);
  });

  it('opens the create modal with a worker picker sourced from the fleet', async () => {
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          tasks: [],
          workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }],
          pending_issues: [],
        }),
      ),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    fireEvent.click(screen.getByTestId('agents-add-btn'));
    expect(screen.getByTestId('agent-create-modal')).toBeInTheDocument();
    // v2.7 #191: worker picker is a searchable EntitySelect — open it to see options.
    fireEvent.click(screen.getByTestId('agent-create-worker-trigger'));
    await waitFor(() =>
      expect(screen.getByTestId('agent-create-worker-options')).toHaveTextContent('box-7'),
    );
  });

  it('creates an agent through the modal', async () => {
    let posted: Record<string, unknown> | null = null;
    server.use(
      http.get('/api/fleet', () =>
        HttpResponse.json({
          tasks: [],
          workers: [{ worker_id: 'w-7', name: 'box-7', status: 'online' }],
          pending_issues: [],
        }),
      ),
      // v2.7 #186/#77: POST /api/agents removed; Add Agent now posts to the
      // unified /api/members/agent (atomic identity-member + execution Agent).
      http.post('/api/members/agent', async ({ request }) => {
        posted = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json(
          { id: 'agent-new', identity_id: 'agent-new', kind: 'agent', display_name: 'newbot' },
          { status: 201 },
        );
      }),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    fireEvent.click(screen.getByTestId('agents-add-btn'));

    await userEvent.type(screen.getByTestId('agent-create-name'), 'newbot');
    // v2.7.1 #232: Model is pre-filled with the explicit default (not a
    // placeholder) so leaving it untouched still submits a concrete value.
    expect((screen.getByTestId('agent-create-model') as HTMLInputElement).value).toBe('claude-opus-4-8');
    // v2.7 #191: pick the worker via the EntitySelect (open → click option).
    fireEvent.click(screen.getByTestId('agent-create-worker-trigger'));
    await waitFor(() =>
      expect(screen.getByTestId('agent-create-worker-options')).toHaveTextContent('box-7'),
    );
    fireEvent.click(screen.getByTestId('agent-create-worker-option'));
    // v2.7 #181 / FINDING-F: cli is a single-option select (claude-code only).
    const cliSelect = screen.getByTestId('agent-create-cli') as HTMLSelectElement;
    expect(cliSelect.tagName).toBe('SELECT');
    expect(Array.from(cliSelect.options).map((o) => o.value)).toEqual(['claude-code']);
    fireEvent.click(screen.getByTestId('agent-create-submit'));

    await waitFor(() => expect(posted).not.toBeNull());
    // Unified create payload: display_name (not name) + role + worker_id + cli.
    expect(posted).toMatchObject({ display_name: 'newbot', role: 'member', worker_id: 'w-7', cli: 'claude-code', model: 'claude-opus-4-8' });
    await waitFor(() =>
      expect(screen.queryByTestId('agent-create-modal')).not.toBeInTheDocument(),
    );
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getByTestId('agents-error')).toHaveTextContent(/db down/));
  });
});

// v2.8.1 list-enrich: each agent row shows provider (CLI + model) badges, the
// last_activity_at (formatLocalTime, local tz) + a truncated content preview,
// and a friendly placeholder when there's no activity.
describe('Agents list-enrichment (v2.8.1)', () => {
  afterEach(() => cleanup());

  it('renders the CLI + model provider badges (text labels, not color-only)', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [agent('bot-1', { name: 'bot-1', cli: 'claudecode', model: 'claude-opus-4-8' })],
        }),
      ),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getByTestId('agent-cli-badge')).toBeInTheDocument());
    expect(screen.getByTestId('agent-cli-badge')).toHaveTextContent('claudecode');
    expect(screen.getByTestId('agent-model-badge')).toHaveTextContent('claude-opus-4-8');
  });

  it('renders last_activity_at via formatLocalTime + a truncated content preview', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [
            agent('bot-1', {
              name: 'bot-1',
              last_activity_at: '2026-05-24T01:00:00Z',
              last_activity_content: 'finished the migration\nand ran tests',
            }),
          ],
        }),
      ),
    );
    wrap(<Agents />);
    const at = await screen.findByTestId('agent-last-activity-at');
    // formatLocalTime shape — never the raw ISO with trailing Z.
    expect(at.textContent).not.toMatch(/\d{4}-\d{2}-\d{2}T.*Z/);
    expect(at.textContent).toMatch(/2026/);
    const content = screen.getByTestId('agent-last-activity-content');
    expect(content.className).toContain('truncate');
    // multi-line flattened to single line + full text on title
    expect(content.textContent).not.toContain('\n');
    expect(content).toHaveAttribute('title', expect.stringContaining('finished the migration'));
  });

  it('shows a friendly placeholder when an agent has no activity', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent('bot-1', { name: 'bot-1' })] }),
      ),
    );
    wrap(<Agents />);
    expect(await screen.findByTestId('agent-no-activity')).toHaveTextContent('No recent activity');
  });
});

// dev2/v281 canonical-fold: the enhanced /agents page is the single agents
// surface, so the retired /members/agents page's Role + membership Status
// (Joined/Disabled) columns are folded in here. Neither field is on the Agent
// DTO — they're resolved via a member-list join keyed by identity (the agent's
// identity_member_id == the member's identity_id).
describe('Agents canonical-fold: Role + Status columns (dev2/v281)', () => {
  afterEach(() => cleanup());

  it('folds Role + membership Status from the member-list join', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [
            agent('bot-1', { name: 'bot-1', identity_member_id: 'agent-1' }),
            agent('bot-2', { name: 'bot-2', identity_member_id: 'agent-2' }),
          ],
        }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'm-1',
            organization_id: 'O-1',
            identity_id: 'agent-1',
            kind: 'agent',
            role: 'admin',
            status: 'joined',
            joined_at: '2026-05-24T01:00:00Z',
          },
          {
            id: 'm-2',
            organization_id: 'O-1',
            identity_id: 'agent-2',
            kind: 'agent',
            role: 'member',
            status: 'disabled',
            joined_at: '2026-05-24T01:00:00Z',
          },
        ]),
      ),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(2));
    const roles = await screen.findAllByTestId('agent-role');
    expect(roles[0]).toHaveTextContent('admin');
    expect(roles[1]).toHaveTextContent('member');
    const statuses = await screen.findAllByTestId('agent-status');
    // Status text label (not color-only): Joined / Disabled.
    await waitFor(() => expect(statuses[0]).toHaveAttribute('data-status', 'joined'));
    expect(statuses[0]).toHaveTextContent('Joined');
    expect(statuses[1]).toHaveAttribute('data-status', 'disabled');
    expect(statuses[1]).toHaveTextContent('Disabled');
  });

  it('shows a neutral placeholder when no member matches (standalone agent)', async () => {
    server.use(
      http.get('/api/agents', () =>
        HttpResponse.json({ agents: [agent('bot-1', { name: 'bot-1', identity_member_id: '' })] }),
      ),
      http.get('/api/members', () => HttpResponse.json([])),
    );
    wrap(<Agents />);
    const status = await screen.findByTestId('agent-status');
    expect(status).toHaveAttribute('data-status', 'unknown');
    expect(status).toHaveTextContent('—');
    expect(status).not.toHaveTextContent('Disabled');
  });
});

// v2.7 #197: agent rows carry a delete action (hard-delete agent + identity-member);
// confirmed via the shared ConfirmModal; the 409 guard codes (agent_running /
// agent_has_active_work) surface as friendly copy, never silent.
describe('Agents delete (#197)', () => {
  beforeEach(() => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: seed })));
  });
  afterEach(() => cleanup());

  it('exposes a delete action per agent row', async () => {
    wrap(<Agents />);
    const btns = await screen.findAllByTestId('agent-delete-button');
    expect(btns).toHaveLength(3);
    expect(btns[1]).toHaveAttribute('data-agent-id', 'bot-2');
  });

  it('confirms (naming the agent) before posting DELETE', async () => {
    let deleted: string | null = null;
    server.use(
      http.delete('/api/agents/bot-2', () => {
        deleted = 'bot-2';
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap(<Agents />);
    const btns = await screen.findAllByTestId('agent-delete-button');
    fireEvent.click(btns[1]);
    const modal = await screen.findByTestId('confirm-modal');
    expect(modal).toHaveTextContent('bot-2');
    await act(async () => {
      fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    });
    await waitFor(() => expect(deleted).toBe('bot-2'));
  });

  it('maps the 409 agent_running guard to friendly copy (Rule 9, no raw code)', async () => {
    server.use(
      http.delete('/api/agents/bot-1', () =>
        HttpResponse.json({ error: 'agent_running', message: 'agent is running' }, { status: 409 }),
      ),
    );
    wrap(<Agents />);
    const btns = await screen.findAllByTestId('agent-delete-button');
    fireEvent.click(btns[0]);
    await act(async () => {
      fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    });
    const err = await screen.findByTestId('agent-delete-error');
    expect(err).toHaveTextContent(/stopped/i);
  });

  it('maps the 409 agent_has_active_work guard to friendly copy', async () => {
    server.use(
      http.delete('/api/agents/bot-1', () =>
        HttpResponse.json({ error: 'agent_has_active_work', message: 'has work' }, { status: 409 }),
      ),
    );
    wrap(<Agents />);
    const btns = await screen.findAllByTestId('agent-delete-button');
    fireEvent.click(btns[0]);
    await act(async () => {
      fireEvent.click(await screen.findByTestId('confirm-modal-confirm'));
    });
    const err = await screen.findByTestId('agent-delete-error');
    expect(err).toHaveTextContent(/active tasks/i);
  });

  it('can be canceled without deleting', async () => {
    let deleted = false;
    server.use(
      http.delete('/api/agents/bot-2', () => {
        deleted = true;
        return new HttpResponse(null, { status: 204 });
      }),
    );
    wrap(<Agents />);
    const btns = await screen.findAllByTestId('agent-delete-button');
    fireEvent.click(btns[1]);
    fireEvent.click(await screen.findByTestId('confirm-modal-cancel'));
    await waitFor(() => expect(screen.queryByTestId('confirm-modal')).not.toBeInTheDocument());
    expect(deleted).toBe(false);
  });
});

// T232: multi-select + batch lifecycle (start/stop/restart/reset) with progress.
describe('Agents page batch operations (T232)', () => {
  beforeEach(() => {
    server.use(http.get('/api/agents', () => HttpResponse.json({ agents: seed })));
  });
  afterEach(() => cleanup());

  it('no toolbar until a row is selected; select-all selects every agent', async () => {
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    expect(screen.queryByTestId('agents-batch-toolbar')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTestId('agents-select-all'));
    expect(screen.getByTestId('agents-batch-toolbar')).toBeInTheDocument();
    expect(screen.getByTestId('agents-batch-selected-count')).toHaveTextContent('3 selected');
    expect(screen.getAllByTestId('agent-select-checkbox').every((c) => (c as HTMLInputElement).checked)).toBe(true);
  });

  it('Start (non-destructive) runs immediately and shows a success summary', async () => {
    const hits: string[] = [];
    server.use(
      http.post('/api/agents/:id/start', ({ params }) => {
        hits.push(String(params.id));
        return HttpResponse.json({ id: params.id, lifecycle: 'running' });
      }),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    // select bot-1 + bot-2
    const checks = screen.getAllByTestId('agent-select-checkbox');
    fireEvent.click(checks[0]);
    fireEvent.click(checks[1]);
    expect(screen.getByTestId('agents-batch-selected-count')).toHaveTextContent('2 selected');

    fireEvent.click(screen.getByTestId('agents-batch-start'));
    const summary = await screen.findByTestId('agents-batch-summary');
    expect(summary).toHaveTextContent('2 succeeded');
    expect(hits).toEqual(['bot-1', 'bot-2']);
  });

  it('Reset (destructive) requires a confirm before running', async () => {
    let resetCount = 0;
    server.use(
      http.post('/api/agents/:id/reset', ({ params }) => {
        resetCount += 1;
        return HttpResponse.json({ id: params.id, lifecycle: 'stopped' });
      }),
    );
    wrap(<Agents />);
    await waitFor(() => expect(screen.getAllByTestId('agent-row')).toHaveLength(3));
    fireEvent.click(screen.getAllByTestId('agent-select-checkbox')[2]);

    fireEvent.click(screen.getByTestId('agents-batch-reset'));
    // confirm dialog gates the destructive batch — nothing fired yet
    expect(await screen.findByTestId('confirm-modal')).toBeInTheDocument();
    expect(resetCount).toBe(0);

    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await screen.findByTestId('agents-batch-summary');
    expect(resetCount).toBe(1);
  });
});
