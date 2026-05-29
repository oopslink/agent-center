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
            <Route path="/projects" element={<div data-testid="page-Projects">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout v2 shell (v2.3 P2)', () => {
  afterEach(() => cleanup());

  it('renders the sidebar sections', () => {
    renderShell();
    // Section headings live in the always-mounted desktop sidebar.
    expect(screen.getByText('Conversations')).toBeInTheDocument();
    expect(screen.getByText('Work')).toBeInTheDocument();
    expect(screen.getByText('System')).toBeInTheDocument();
  });

  // v2.3-4: Workspace section + Projects nav item.
  it('renders the Workspace section with a Projects link', () => {
    renderShell();
    expect(screen.getByText('Workspace')).toBeInTheDocument();
    const projectsLink = screen.getByRole('link', { name: /projects/i });
    expect(projectsLink).toHaveAttribute('href', '/projects');
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
    // The Projects nav item links to /projects (sub-list children link
    // to /projects/<id>, so target the exact top-level href).
    const projectsLink = screen
      .getAllByRole('link')
      .find((a) => a.getAttribute('href') === '/projects');
    expect(projectsLink).toBeDefined();
    fireEvent.click(projectsLink!);
    expect(screen.getByTestId('page-Projects')).toBeInTheDocument();
  });
});
