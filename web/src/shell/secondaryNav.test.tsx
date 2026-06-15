import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { FakeEventSource } from '@/sse/fakeEventSource';

// v2.10.0 [T1] — col② per-module override contract. A module registered in
// SECONDARY_NAV_REGISTRY supplies the col② nav body; unregistered modules use
// the shell default. We mock the registry so this test pins the wiring AppLayout
// relies on (the extension point the six module tasks build on).
vi.mock('@/shell/secondaryNav', () => ({
  SECONDARY_NAV_REGISTRY: {
    conversations: ({ orgBase }: { orgBase: string }) => (
      <div data-testid="custom-conversations-nav" data-orgbase={orgBase}>
        custom conversations col②
      </div>
    ),
  },
}));

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

// Import AFTER the mock so AppLayout binds the mocked registry.
const { default: AppLayout } = await import('../AppLayout');

function renderAt(initial: string): void {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            <Route path="/projects" element={<div data-testid="page-Projects">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('col② per-module nav registry (v2.10.0 [T1] contract)', () => {
  afterEach(() => cleanup());

  it('renders a registered module nav instead of the shell default', () => {
    renderAt('/channels');
    // Conversations is registered → its custom nav renders; the default group
    // header for Conversations does NOT.
    expect(screen.getByTestId('custom-conversations-nav')).toBeInTheDocument();
    expect(screen.queryByTestId('sidebar-group-toggle-Conversations')).not.toBeInTheDocument();
  });

  it('falls back to the shell default for unregistered modules', () => {
    renderAt('/projects');
    // Workspace is NOT registered → the default NavGroup renders.
    expect(screen.getByTestId('sidebar-group-toggle-Workspace')).toBeInTheDocument();
    expect(screen.queryByTestId('custom-conversations-nav')).not.toBeInTheDocument();
  });
});
