import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import UserDetail from './UserDetail';

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/users/:userId" element={<UserDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('UserDetail page (#193)', () => {
  afterEach(() => cleanup());

  it('renders profile + org memberships by name (raw org id on hover)', async () => {
    server.use(
      http.get('/api/orgs', () =>
        HttpResponse.json([{ id: 'org-1', slug: 'acme', name: 'Acme', created_at: '2026-01-01T00:00:00Z' }]),
      ),
      http.get('/api/users/user-abc12345', () =>
        HttpResponse.json({
          user_id: 'user-abc12345',
          display_name: 'Alice',
          email: 'alice@example.com',
          created_at: '2026-05-20T01:00:00Z',
          last_session_at: '2026-06-01T09:00:00Z',
          orgs: [{ org_id: 'org-1', role: 'owner' }],
        }),
      ),
    );
    wrap('/users/user-abc12345');
    await waitFor(() => expect(screen.getByTestId('user-detail-name')).toHaveTextContent('Alice'));
    // member-id only on hover, not in visible text.
    expect(screen.getByTestId('user-detail-name')).toHaveAttribute('title', 'user-abc12345');
    // Kind shown as a tag next to the name (not a line beneath it).
    expect(screen.getByTestId('user-detail-kind-tag')).toHaveTextContent('User');
    // Profile is the default tab → email visible.
    expect(screen.getByTestId('user-detail-email')).toHaveTextContent('alice@example.com');
    // Viewing someone else (default /api/auth/me is user-test) → no Account tab.
    expect(screen.queryByTestId('user-tab-account')).not.toBeInTheDocument();
    // Switch to the Organizations tab to see memberships.
    fireEvent.click(screen.getByTestId('user-tab-organizations'));
    const orgRow = screen.getByTestId('user-detail-org-row');
    expect(orgRow).toHaveTextContent('Acme'); // org shown by name (not raw org-1).
    expect(orgRow).toHaveTextContent('owner');
    expect(orgRow).not.toHaveTextContent('org-1');
    expect(orgRow).toHaveAttribute('data-org-id', 'org-1');
  });

  it('shows the self-only Account section (change password + sign out) when viewing your own profile', async () => {
    server.use(
      http.get('/api/orgs', () =>
        HttpResponse.json([{ id: 'org-1', slug: 'acme', name: 'Acme', created_at: '2026-01-01T00:00:00Z' }]),
      ),
      // default /api/auth/me identity_id is 'user-test' → this is "me".
      http.get('/api/users/user-test', () =>
        HttpResponse.json({
          user_id: 'user-test',
          display_name: 'Test User',
          email: 'me@example.com',
          created_at: '2026-05-20T01:00:00Z',
          last_session_at: '2026-06-01T09:00:00Z',
          orgs: [{ org_id: 'org-1', role: 'owner' }],
        }),
      ),
    );
    // Arriving via /me lands on ?tab=account → Account tab is pre-selected.
    wrap('/users/user-test?tab=account');
    await waitFor(() => expect(screen.getByTestId('user-detail-account')).toBeInTheDocument());
    expect(screen.getByTestId('account-panel')).toBeInTheDocument();
    expect(screen.getByLabelText('Current password')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument();
  });

  it('defaults to Profile for self, with an Account tab that reveals the controls on click', async () => {
    server.use(
      http.get('/api/orgs', () =>
        HttpResponse.json([{ id: 'org-1', slug: 'acme', name: 'Acme', created_at: '2026-01-01T00:00:00Z' }]),
      ),
      http.get('/api/users/user-test', () =>
        HttpResponse.json({
          user_id: 'user-test',
          display_name: 'Test User',
          email: 'me@example.com',
          created_at: '2026-05-20T01:00:00Z',
          last_session_at: '2026-06-01T09:00:00Z',
          orgs: [{ org_id: 'org-1', role: 'owner' }],
        }),
      ),
    );
    wrap('/users/user-test'); // no ?tab → Profile is the default.
    await waitFor(() => expect(screen.getByTestId('user-detail-email')).toBeInTheDocument());
    // Account tab is present (self) but its panel isn't shown until selected.
    expect(screen.getByTestId('user-tab-account')).toBeInTheDocument();
    expect(screen.queryByTestId('user-detail-account')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('user-tab-account'));
    expect(screen.getByTestId('account-panel')).toBeInTheDocument();
  });

  it('renders an em dash for a null email / last session (v2.7.0 upgrade user)', async () => {
    server.use(
      http.get('/api/orgs', () => HttpResponse.json([])),
      http.get('/api/users/user-old', () =>
        HttpResponse.json({
          user_id: 'user-old',
          display_name: 'Legacy',
          email: null,
          created_at: '2026-05-20T01:00:00Z',
          last_session_at: null,
          orgs: [],
        }),
      ),
    );
    wrap('/users/user-old');
    await waitFor(() => expect(screen.getByTestId('user-detail-name')).toHaveTextContent('Legacy'));
    expect(screen.getByTestId('user-detail-email')).toHaveTextContent('—');
    expect(screen.getByTestId('user-detail-last-session')).toHaveTextContent('—');
    // Org membership empty-state lives on the Organizations tab.
    fireEvent.click(screen.getByTestId('user-tab-organizations'));
    expect(screen.getByTestId('user-detail-orgs-empty')).toBeInTheDocument();
  });

  it('surfaces a not-found error', async () => {
    server.use(
      http.get('/api/orgs', () => HttpResponse.json([])),
      http.get('/api/users/user-missing', () =>
        HttpResponse.json({ error: 'not_found', message: 'no such user' }, { status: 404 }),
      ),
    );
    wrap('/users/user-missing');
    await waitFor(() => expect(screen.getByTestId('user-detail-not-found')).toBeInTheDocument());
  });
});
