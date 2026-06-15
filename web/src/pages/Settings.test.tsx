import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { server } from '@/test/mswServer';
import Settings from './Settings';

function wrap(path = '/settings') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Settings />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('Settings version panel', () => {
  it('renders the 4 build-identity fields from /api/system/version', async () => {
    server.use(
      http.get('/api/system/version', () =>
        HttpResponse.json({
          version: 'v2.8.1-9908825',
          branch: 'v2.8.1',
          commit: '9908825',
          built_at: '2026-06-08T07:34:21Z',
        }),
      ),
    );
    wrap();
    expect(await screen.findByTestId('version-version')).toHaveTextContent('v2.8.1-9908825');
    expect(screen.getByTestId('version-branch')).toHaveTextContent('v2.8.1');
    expect(screen.getByTestId('version-commit')).toHaveTextContent('9908825');
    // built_at renders via formatLocalTime (#215), not raw UTC ISO.
    const built = screen.getByTestId('version-built-at');
    expect(built).not.toHaveTextContent('2026-06-08T07:34');
    expect(built).toHaveTextContent('2026');
  });

  it('shows an error state when the version endpoint fails', async () => {
    server.use(
      http.get('/api/system/version', () => HttpResponse.json({ error: 'x' }, { status: 500 })),
    );
    wrap();
    expect(await screen.findByTestId('version-error')).toBeInTheDocument();
  });

  // v2.10.1 [M7] the System module mobile 二级段控 (Environment | Settings).
  it('renders the mobile System segmented nav with Settings active', async () => {
    server.use(
      http.get('/api/system/version', () =>
        HttpResponse.json({ version: 'v', branch: 'b', commit: 'c', built_at: '2026-06-08T07:34:21Z' }),
      ),
    );
    wrap('/settings');
    const nav = await screen.findByTestId('segmented-nav');
    expect(within(nav).getByTestId('system-seg-settings')).toHaveAttribute('data-active', 'true');
    expect(within(nav).getByTestId('system-seg-environment')).toHaveAttribute('data-active', 'false');
  });
});
