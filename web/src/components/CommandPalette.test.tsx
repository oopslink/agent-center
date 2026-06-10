import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import { CommandPalette } from './CommandPalette';

// v2.8.1 fix: CommandPalette items hold app-absolute paths (/channels, …) but
// real routes live under /organizations/{slug}; committing must org-scope the
// path (else navigate hits a non-route → OrgRedirect → "click did nothing").
beforeAll(() => {
  server.use(http.get('/api/conversations', () => HttpResponse.json([])));
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

  it('leaves the path unchanged when there is no org context (orgPath no-op)', async () => {
    renderPalette(null);
    const input = await screen.findByTestId('palette-input');
    fireEvent.change(input, { target: { value: 'channels' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(screen.getByTestId('loc')).toHaveTextContent('/channels');
  });
});
