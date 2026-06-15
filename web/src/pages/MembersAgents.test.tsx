import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import MembersAgents from './MembersAgents';

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

afterEach(() => cleanup());

const fullAgent = (id: string, identityMemberID: string) => ({
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
  identity_member_id: identityMemberID,
  version: 1,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
});

// v2.7 #157 §-1: an agent member row links to its execution Agent's AgentDetail,
// resolved by member.identity_id == agent.identity_member_id (the unified-create
// link). This is the resolution-correctness PD called out for review.
describe('MembersAgents — agent member → AgentDetail nav (#157)', () => {
  it('links the agent member to /agents/{id} via identity_member_id', async () => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'M-1',
            organization_id: 'O-1',
            identity_id: 'agent-bot1',
            kind: 'agent',
            role: 'member',
            status: 'joined',
            joined_at: '2026-01-01T00:00:00Z',
            display_name: 'Bot One',
          },
        ]),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [fullAgent('A-99', 'agent-bot1')] })),
    );

    wrap(<MembersAgents />);

    const link = await screen.findByTestId('agent-member-link-agent-bot1');
    expect(link.getAttribute('href')).toContain('/agents/A-99');
    expect(link.textContent).toBe('Bot One');
  });

  it('renders a non-link when no execution Agent is linked', async () => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'M-2',
            organization_id: 'O-1',
            identity_id: 'agent-orphan',
            kind: 'agent',
            role: 'member',
            status: 'joined',
            joined_at: '2026-01-01T00:00:00Z',
          },
        ]),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [] })),
    );

    wrap(<MembersAgents />);

    // v2.10.1 M6: name renders in both desktop table + mobile card (both in jsdom).
    await waitFor(() => expect(screen.getAllByText('agent-orphan').length).toBeGreaterThan(0));
    expect(screen.queryByTestId('agent-member-link-agent-orphan')).toBeNull();
  });
});
