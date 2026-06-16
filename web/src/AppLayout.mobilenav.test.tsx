// Mobile (<768) module nav sheet — on mobile col② has no column, so the active
// module's secondary nav reflows into a bottom sheet opened from the top-bar
// title. Without it, tapping Workspace on mobile only lands on Projects with no
// way to reach Issues / Tasks / Plans. (jsdom has no viewport media, so the
// md:-hidden trigger + sheet both render in the DOM — we drive them by testid.)
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function renderShell(initial = '/projects') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/projects" element={<div data-testid="page-Projects">projects</div>} />
            <Route path="/issues" element={<div data-testid="page-Issues">issues</div>} />
            <Route path="/tasks" element={<div data-testid="page-Tasks">tasks</div>} />
            <Route path="/plans" element={<div data-testid="page-Plans">plans</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('Mobile module nav sheet — col② reflowed into a bottom sheet', () => {
  afterEach(() => cleanup());

  it('top-bar title opens a sheet exposing the Workspace sections (Issues/Tasks/Plans), not just Projects', async () => {
    renderShell('/projects');
    const toggle = screen.getByTestId('mobile-nav-toggle');
    expect(toggle).toHaveTextContent('Workspace');
    // Closed initially.
    expect(screen.queryByTestId('mobile-nav-sheet')).toBeNull();
    fireEvent.click(toggle);
    const sheet = await screen.findByTestId('mobile-nav-sheet');
    // The Workspace top-level nav lives inside the sheet — all sections reachable.
    const nav = within(sheet).getByTestId('workspace-nav-toplevel');
    expect(within(nav).getByText('Projects')).toBeInTheDocument();
    expect(within(nav).getByText('Issues')).toBeInTheDocument();
    expect(within(nav).getByText('Tasks')).toBeInTheDocument();
    expect(within(nav).getByText('Plans')).toBeInTheDocument();
  });

  it('navigating from the sheet routes to the section and closes the sheet', async () => {
    renderShell('/projects');
    fireEvent.click(screen.getByTestId('mobile-nav-toggle'));
    const sheet = await screen.findByTestId('mobile-nav-sheet');
    fireEvent.click(within(sheet).getByText('Tasks'));
    // Route-change effect dismisses the sheet so it doesn't cover the new screen.
    await waitFor(() => expect(screen.queryByTestId('mobile-nav-sheet')).toBeNull());
    expect(screen.getByTestId('page-Tasks')).toBeInTheDocument();
  });
});
