import type React from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { ContextPanel } from '@/shell/contextPanel';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

// A page that fills col④ via <ContextPanel> — proves the on-demand panel slot.
function PageWithPanel(): React.ReactElement {
  return (
    <div data-testid="page-Channels">
      x
      <ContextPanel>
        <div data-testid="panel-content">参与者 · 3</div>
      </ContextPanel>
    </div>
  );
}

function renderShell(initial = '/channels') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            <Route path="/projects" element={<div data-testid="page-Projects">x</div>} />
            <Route path="/environment" element={<div data-testid="page-Environment">x</div>} />
            <Route path="/panel" element={<PageWithPanel />} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout v5 shell (v2.10.0 [T1] — three-column module rail)', () => {
  afterEach(() => cleanup());

  it('renders the four-column scaffold: module rail (col①), secondary nav (col②), content (col③), context slot (col④)', () => {
    renderShell();
    // col① — the module rail with all four modules.
    const rail = screen.getByRole('navigation', { name: /^modules$/ });
    expect(within(rail).getByTestId('rail-module-workspace')).toBeInTheDocument();
    expect(within(rail).getByTestId('rail-module-conversations')).toBeInTheDocument();
    expect(within(rail).getByTestId('rail-module-members')).toBeInTheDocument();
    expect(within(rail).getByTestId('rail-module-system')).toBeInTheDocument();
    // col② — the active module's secondary nav.
    expect(screen.getByRole('navigation', { name: /^primary$/ })).toBeInTheDocument();
    // col③ — full-width content shell.
    expect(screen.getByTestId('app-content-shell')).toBeInTheDocument();
    // col④ — context panel host, collapsed (no panel mounted yet).
    expect(screen.getByTestId('context-panel')).toHaveAttribute('data-open', 'false');
  });

  it('rail icons link to each module default page; the active module is marked', () => {
    renderShell('/channels');
    expect(screen.getByTestId('rail-module-workspace')).toHaveAttribute('href', '/projects');
    expect(screen.getByTestId('rail-module-conversations')).toHaveAttribute('href', '/channels');
    expect(screen.getByTestId('rail-module-members')).toHaveAttribute('href', '/members/humans');
    expect(screen.getByTestId('rail-module-system')).toHaveAttribute('href', '/environment');
    // On /channels the Conversations module is active.
    expect(screen.getByTestId('rail-module-conversations')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('rail-module-workspace')).toHaveAttribute('data-active', 'false');
  });

  it('col② shows ONLY the active module second-level nav and swaps with the rail', () => {
    renderShell('/channels');
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    // Conversations active → Channels + DMs, NOT Projects (a Workspace item).
    expect(within(nav).getByTestId('section-label')).toHaveTextContent('Conversations');
    expect(within(nav).getByRole('link', { name: /channels/i })).toHaveAttribute('href', '/channels');
    expect(within(nav).queryByRole('link', { name: /^projects$/i })).not.toBeInTheDocument();

    // Click the Workspace rail icon → col② swaps to the Workspace nav.
    fireEvent.click(screen.getByTestId('rail-module-workspace'));
    expect(screen.getByTestId('page-Projects')).toBeInTheDocument();
    const nav2 = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav2).getByTestId('section-label')).toHaveTextContent('Workspace');
    expect(within(nav2).getByRole('link', { name: /projects/i })).toHaveAttribute('href', '/projects');
    expect(within(nav2).getByRole('link', { name: /tasks/i })).toHaveAttribute('href', '/tasks');
  });

  it('content shell uses the full remaining width (no centering / max-width)', () => {
    renderShell();
    const shell = screen.getByTestId('app-content-shell');
    expect(shell.className).toContain('w-full');
    expect(shell.className).not.toContain('mx-auto');
    expect(shell.className).not.toContain('max-w-');
  });

  it('col④ context panel is revealed only when a page mounts <ContextPanel>', () => {
    renderShell('/channels');
    // No panel on a plain page.
    expect(screen.getByTestId('context-panel')).toHaveAttribute('data-open', 'false');
    expect(screen.queryByTestId('panel-content')).not.toBeInTheDocument();
    // Navigate (via the rail is module-level; use the page route) to the panel page.
    cleanup();
    renderShell('/panel');
    const ctx = screen.getByTestId('context-panel');
    expect(ctx).toHaveAttribute('data-open', 'true');
    // The panel content portals INTO the col④ host.
    expect(within(ctx).getByTestId('panel-content')).toHaveTextContent('参与者 · 3');
  });

  it('mobile hamburger toggles the drawer (aria-expanded + dialog overlay)', () => {
    renderShell();
    const toggle = screen.getByTestId('nav-toggle');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });
});
