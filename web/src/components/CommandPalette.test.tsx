import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import { useAppStore } from '@/store/app';
import { CommandPalette } from './CommandPalette';

// v2.8.1 fix: CommandPalette items hold app-absolute paths (/channels, …) but
// real routes live under /organizations/{slug}; committing must org-scope the
// path (else navigate hits a non-route → OrgRedirect → "click did nothing").
beforeAll(() => {
  server.use(
    http.get('/api/conversations', () => HttpResponse.json([])),
    http.get('/api/members', () => HttpResponse.json([])),
  );
});
afterEach(() => cleanup());

function Loc(): React.ReactElement {
  return <div data-testid="loc">{useLocation().pathname}</div>;
}

function renderPalette(org: { slug: string; orgId: string; orgName: string } | null) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/start']}>
        <OrgContext.Provider value={org}>
          <CommandPalette open onClose={() => {}} />
          <Loc />
        </OrgContext.Provider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('CommandPalette org-scoped navigation (v2.8.1 fix)', () => {
  it('navigates to /organizations/{slug}/… when an org context is present', async () => {
    renderPalette({ slug: 'acme', orgId: 'o1', orgName: 'Acme' });
    const input = await screen.findByTestId('palette-input');
    fireEvent.change(input, { target: { value: 'channels' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(screen.getByTestId('loc')).toHaveTextContent('/organizations/acme/channels');
  });

  it.each([
    ['issues', '/organizations/acme/issues'],
    ['tasks', '/organizations/acme/tasks'],
    ['plans', '/organizations/acme/plans'],
    ['repos', '/organizations/acme/repos'],
    ['templates', '/organizations/acme/templates'],
  ])('jumps to the workspace %s list', async (query, expected) => {
    renderPalette({ slug: 'acme', orgId: 'o1', orgName: 'Acme' });
    const input = await screen.findByTestId('palette-input');
    fireEvent.change(input, { target: { value: query } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(screen.getByTestId('loc')).toHaveTextContent(expected);
  });

  it('leaves the path unchanged when there is no org context (orgPath no-op)', async () => {
    renderPalette(null);
    const input = await screen.findByTestId('palette-input');
    fireEvent.change(input, { target: { value: 'channels' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(screen.getByTestId('loc')).toHaveTextContent('/channels');
  });
});

describe('CommandPalette @ mode — quick DM an agent', () => {
  const members = [
    { id: 'm1', organization_id: 'o1', identity_id: 'agent-dev1', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x', display_name: 'agent-center-dev1' },
    { id: 'm2', organization_id: 'o1', identity_id: 'agent-dev2', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x', display_name: 'agent-center-dev2' },
    { id: 'me', organization_id: 'o1', identity_id: 'me', kind: 'user', role: 'owner', status: 'joined', joined_at: 'x', display_name: 'Me' },
  ];

  it('typing "@" lists the org members you can DM, excluding yourself', async () => {
    useAppStore.getState().setCurrentUserId('user:me');
    server.use(http.get('/api/members', () => HttpResponse.json(members)));
    renderPalette({ slug: 'acme', orgId: 'o1', orgName: 'Acme' });
    const input = await screen.findByTestId('palette-input');
    fireEvent.change(input, { target: { value: '@' } });
    expect(await screen.findByText('@agent-center-dev1')).toBeInTheDocument();
    expect(screen.getByText('@agent-center-dev2')).toBeInTheDocument();
    // self is excluded from the DM picker
    expect(screen.queryByText('@Me')).toBeNull();
  });

  it('"@dev1" filters, and Enter opens (creates) the DM and navigates to it', async () => {
    useAppStore.getState().setCurrentUserId('user:me');
    let posted: unknown = null;
    server.use(
      http.get('/api/members', () => HttpResponse.json(members)),
      http.post('/api/conversations', async ({ request }) => {
        posted = await request.json();
        return HttpResponse.json({ conversation_id: 'conv-1' });
      }),
    );
    renderPalette({ slug: 'acme', orgId: 'o1', orgName: 'Acme' });
    const input = await screen.findByTestId('palette-input');
    fireEvent.change(input, { target: { value: '@dev1' } });
    // only dev1 matches the "@dev1" filter
    expect(await screen.findByText('@agent-center-dev1')).toBeInTheDocument();
    expect(screen.queryByText('@agent-center-dev2')).toBeNull();
    fireEvent.keyDown(input, { key: 'Enter' });
    // opens (or reuses) my DM with dev1 and navigates to it
    await waitFor(() =>
      expect(screen.getByTestId('loc')).toHaveTextContent('/organizations/acme/dms/conv-1'),
    );
    expect(posted).toEqual({ kind: 'dm', members: ['agent:agent-dev1'] });
  });
});
