import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  server.use(http.get('/api/input_requests', () => HttpResponse.json([])));
});

function renderShell(initial = '/channels') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            <Route path="/tasks" element={<div data-testid="page-Tasks">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout v2 shell (v2.3 P2)', () => {
  afterEach(() => cleanup());

  it('renders the three sidebar sections', () => {
    renderShell();
    // Section headings live in the always-mounted desktop sidebar.
    expect(screen.getByText('Conversations')).toBeInTheDocument();
    expect(screen.getByText('Work')).toBeInTheDocument();
    expect(screen.getByText('System')).toBeInTheDocument();
  });

  it('hamburger toggle exists and toggles aria-expanded', () => {
    renderShell();
    const toggle = screen.getByTestId('nav-toggle');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    // Now there's a dialog overlay
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    // Click again closes
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });

  it('navigates between top-level pages via sidebar', () => {
    renderShell('/channels');
    expect(screen.getByTestId('page-Channels')).toBeInTheDocument();
    // Find the Tasks NavLink (there are two — desktop + drawer copies
    // mount only when open, so a single match here).
    const tasksLink = screen.getByRole('link', { name: /tasks/i });
    fireEvent.click(tasksLink);
    expect(screen.getByTestId('page-Tasks')).toBeInTheDocument();
  });
});
