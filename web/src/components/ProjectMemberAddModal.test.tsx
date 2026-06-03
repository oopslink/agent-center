import { afterEach, describe, expect, it } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type React from 'react';
import { server } from '@/test/mswServer';
import { ProjectMemberAddModal } from './ProjectMemberAddModal';
import type { ProjectMember } from '@/api/types';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const orgMembers = [
  { id: 'm1', organization_id: 'O-1', identity_id: 'user-alice', kind: 'user', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Alice' },
  { id: 'm2', organization_id: 'O-1', identity_id: 'agent-bot', kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Bot' },
];

describe('ProjectMemberAddModal (#207)', () => {
  afterEach(() => cleanup());

  it('lists org members as candidates, excluding those already on the project', async () => {
    server.use(http.get('/api/members', () => HttpResponse.json(orgMembers)));
    const existing: ProjectMember[] = [
      { id: 'pm1', project_id: 'proj-a', identity_id: 'user:user-alice', role: 'owner', added_by: 'x', created_at: '2026-01-01T00:00:00Z' },
    ];
    wrap(<ProjectMemberAddModal projectId="proj-a" existing={existing} onClose={() => {}} />);
    // Alice is already a member → excluded; Bot remains a candidate.
    await waitFor(() => expect(screen.getAllByTestId('project-add-candidate')).toHaveLength(1));
    expect(screen.getByText('Bot')).toBeInTheDocument();
    expect(screen.queryByText('Alice')).not.toBeInTheDocument();
  });

  it('multi-selects + confirms, posting each as a <kind>:<id> ref', async () => {
    const posted: Array<Record<string, unknown>> = [];
    server.use(
      http.get('/api/members', () => HttpResponse.json(orgMembers)),
      http.post('/api/projects/proj-a/members', async ({ request }) => {
        posted.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ ok: true });
      }),
    );
    let closed = false;
    wrap(<ProjectMemberAddModal projectId="proj-a" existing={[]} onClose={() => { closed = true; }} />);
    await waitFor(() => expect(screen.getAllByTestId('project-add-candidate')).toHaveLength(2));
    const checks = screen.getAllByTestId('project-add-candidate-check');
    fireEvent.click(checks[0]); // Alice → user:user-alice
    fireEvent.click(checks[1]); // Bot → agent:agent-bot
    await act(async () => {
      fireEvent.click(screen.getByTestId('project-add-confirm'));
    });
    await waitFor(() => expect(posted).toHaveLength(2));
    expect(posted).toContainEqual({ identity_id: 'user:user-alice', role: 'member' });
    expect(posted).toContainEqual({ identity_id: 'agent:agent-bot', role: 'member' });
    expect(closed).toBe(true);
  });

  it('filters candidates by the search query', async () => {
    server.use(http.get('/api/members', () => HttpResponse.json(orgMembers)));
    wrap(<ProjectMemberAddModal projectId="proj-a" existing={[]} onClose={() => {}} />);
    await waitFor(() => expect(screen.getAllByTestId('project-add-candidate')).toHaveLength(2));
    fireEvent.change(screen.getByTestId('project-add-search'), { target: { value: 'bot' } });
    expect(screen.getAllByTestId('project-add-candidate')).toHaveLength(1);
    expect(screen.getByText('Bot')).toBeInTheDocument();
  });
});
