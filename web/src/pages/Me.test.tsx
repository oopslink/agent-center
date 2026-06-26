import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes, useParams, useSearchParams } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import Me from './Me';

// Probe stands in for UserDetail so we can assert where /me redirected to.
function UserProbe() {
  const { userId } = useParams<{ userId: string }>();
  const [sp] = useSearchParams();
  return (
    <div data-testid="user-probe" data-user-id={userId} data-tab={sp.get('tab') ?? ''} />
  );
}

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route
            path="/organizations/:slug/me"
            element={
              <OrgContext.Provider value={{ slug: 'acme', orgId: 'org-1', orgName: 'Acme' }}>
                <Me />
              </OrgContext.Provider>
            }
          />
          <Route path="/organizations/:slug/users/:userId" element={<UserProbe />} />
          <Route path="/signin" element={<div data-testid="signin" />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Me page (#8 — /me alias for self UserDetail)', () => {
  afterEach(() => cleanup());

  it('renders the standalone account settings page', async () => {
    wrap('/organizations/acme/me');
    await waitFor(() => expect(screen.getByTestId('page-Me')).toBeInTheDocument());
    expect(screen.getByText('Account settings')).toBeInTheDocument();
  });

  it('renders account settings even when auth check fails (page handles its own loading)', async () => {
    server.use(
      http.get('/api/auth/me', () =>
        HttpResponse.json({ error: 'unauthenticated', message: 'no session' }, { status: 401 }),
      ),
    );
    wrap('/organizations/acme/me');
    await waitFor(() => expect(screen.getByTestId('page-Me')).toBeInTheDocument());
  });
});
