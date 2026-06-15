import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import MembersHumans from './MembersHumans';

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <MembersHumans />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('MembersHumans page (#193 columns + link)', () => {
  afterEach(() => cleanup());

  it('renders email / created / last-active columns and links to UserDetail by member-id', async () => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'M-1', organization_id: 'O-1', identity_id: 'user:user-abc12345', kind: 'user',
            role: 'owner', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Alice',
            email: 'alice@example.com', created_at: '2026-05-20T01:00:00Z', last_session_at: '2026-06-01T09:00:00Z',
          },
        ]),
      ),
    );
    wrap();
    // v2.10.1 M6: name renders in both the desktop table and the mobile card,
    // so it appears twice in jsdom — assert ≥1 and scope columns via testids.
    await waitFor(() => expect(screen.getAllByText('Alice').length).toBeGreaterThan(0));
    expect(screen.getByTestId('human-email')).toHaveTextContent('alice@example.com');
    expect(screen.getByTestId('human-created')).not.toHaveTextContent('—');
    expect(screen.getByTestId('human-last-session')).not.toHaveTextContent('—');
    // Identity links to UserDetail by member-id (prefix stripped).
    const link = screen.getByTestId('human-member-link');
    expect(link.getAttribute('href')).toContain('/users/user-abc12345');
  });

  it('shows an em dash for null email / last session (v2.7.0 upgrade user)', async () => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'M-2', organization_id: 'O-1', identity_id: 'user:user-old', kind: 'user',
            role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Legacy',
            email: null, created_at: '2026-05-20T01:00:00Z', last_session_at: null,
          },
        ]),
      ),
    );
    wrap();
    await waitFor(() => expect(screen.getAllByText('Legacy').length).toBeGreaterThan(0));
    expect(screen.getByTestId('human-email')).toHaveTextContent('—');
    expect(screen.getByTestId('human-last-session')).toHaveTextContent('—');
  });
});
