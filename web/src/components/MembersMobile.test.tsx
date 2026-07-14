import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { MembersSegmentControl } from './MembersSegmentControl';
import MembersAgents from '@/pages/MembersAgents';
import MembersHumans from '@/pages/MembersHumans';

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

const fullAgent = (id: string, identityMemberID: string, availability = 'available', lifecycle = 'running') => ({
  id, organization_id: 'O-1', name: id, description: '', model: 'claude-opus', cli: 'claudecode',
  env_vars: {}, worker_id: 'w-1', lifecycle, availability, created_by: 'user:hayang',
  identity_member_id: identityMemberID, version: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
});

describe('MembersSegmentControl (mobile Humans/Agents switch)', () => {
  it('renders both segments, marks the active one, and links to both list routes', () => {
    wrap(<MembersSegmentControl active="agents" />);
    const humans = screen.getByTestId('members-seg-humans');
    const agents = screen.getByTestId('members-seg-agents');
    expect(humans.getAttribute('href')).toContain('/teams/humans');
    expect(agents.getAttribute('href')).toContain('/teams/agents');
    expect(agents).toHaveAttribute('data-active', 'true');
    expect(humans).toHaveAttribute('data-active', 'false');
    expect(agents).toHaveAttribute('aria-selected', 'true');
  });
});

describe('MembersAgents — mobile card list', () => {
  it('renders a card with online dot + "role · lifecycle"; the avatar opens a DM', async () => {
    let dmBody: { kind?: string; members?: string[] } | undefined;
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'M-1', organization_id: 'O-1', identity_id: 'agent:agent-bot1', kind: 'agent',
            role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Bot One' },
        ]),
      ),
      http.get('/api/agents', () => HttpResponse.json({ agents: [fullAgent('A-99', 'agent:agent-bot1', 'available', 'running')] })),
      http.post('/api/conversations', async ({ request }) => {
        dmBody = (await request.json()) as { kind: string; members: string[] };
        return HttpResponse.json({ conversation_id: 'C-DM', event_id: 'E-1', kind: 'dm' }, { status: 201 });
      }),
    );
    wrap(<MembersAgents />);

    const card = await screen.findByTestId('agent-member-card');
    expect(card).toBeInTheDocument();
    // online dot reflects availability
    const dot = screen.getByTestId('agent-online-dot');
    expect(dot).toHaveAttribute('data-availability', 'available');
    // subtitle = role · lifecycle
    expect(screen.getByTestId('agent-member-card')).toHaveTextContent('member · running');
    // card links to AgentDetail (resolved execution agent)
    expect(screen.getByTestId('agent-card-link').getAttribute('href')).toContain('/agents/A-99');
    // tapping the avatar opens a DM with the agent identity ref
    fireEvent.click(screen.getByTestId('agent-card-dm'));
    await waitFor(() => expect(dmBody).toBeDefined());
    expect(dmBody?.kind).toBe('dm');
    expect(dmBody?.members).toEqual(['agent:agent-bot1']);
  });
});

describe('MembersHumans — mobile card list', () => {
  it('renders a card linking to UserDetail; the avatar opens a DM', async () => {
    let dmBody: { kind?: string; members?: string[] } | undefined;
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'M-2', organization_id: 'O-1', identity_id: 'user:user-abc12345', kind: 'user',
            role: 'owner', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Alice' },
        ]),
      ),
      http.post('/api/conversations', async ({ request }) => {
        dmBody = (await request.json()) as { kind: string; members: string[] };
        return HttpResponse.json({ conversation_id: 'C-DM', event_id: 'E-1', kind: 'dm' }, { status: 201 });
      }),
    );
    wrap(<MembersHumans />);

    const card = await screen.findByTestId('human-member-card');
    expect(card).toBeInTheDocument();
    expect(screen.getByTestId('human-card-link').getAttribute('href')).toContain('/users/user-abc12345');
    fireEvent.click(screen.getByTestId('human-card-dm'));
    await waitFor(() => expect(dmBody).toBeDefined());
    expect(dmBody?.members).toEqual(['user:user-abc12345']);
  });
});
