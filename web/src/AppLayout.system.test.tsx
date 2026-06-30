// v2.10.0 [T8] System — the System module in the three-column shell. System
// uses the shell-default col② (Environment / Settings — no custom registry
// override, no expandable sub-lists) and is intentionally THREE columns: no
// col④ context panel (mockup `docs/design/v2.10.0/system.html` — "多为三栏无需
// 第四栏"). This pins that integration.
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function renderShell(initial = '/environment') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/environment" element={<div data-testid="page-Environment">env</div>} />
            <Route path="/settings" element={<div data-testid="page-Settings">settings</div>} />
            <Route path="/version" element={<div data-testid="page-Version">version</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('col②/④ System module — three-column shell integration (v2.10.0 [T8])', () => {
  afterEach(() => cleanup());

  it('System is the active rail module on /environment, defaulting to Environment', () => {
    renderShell('/environment');
    expect(screen.getByTestId('rail-module-system')).toHaveAttribute('data-active', 'true');
    expect(screen.getByTestId('rail-module-system')).toHaveAttribute('href', '/environment');
  });

  it('col② shows the shell-default System group with Environment + Settings (no expandable sub-lists)', () => {
    renderShell('/environment');
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav).getByTestId('section-label')).toHaveTextContent('System');
    expect(within(nav).getByRole('link', { name: /environment/i })).toHaveAttribute('href', '/environment');
    expect(within(nav).getByRole('link', { name: /settings/i })).toHaveAttribute('href', '/settings');
    // System nav items are flat — no channel/DM/agent-style sub-list toggles.
    expect(within(nav).queryByTestId('sidebar-subitem-toggle-/environment')).not.toBeInTheDocument();
    expect(within(nav).queryByTestId('sidebar-subitem-toggle-/settings')).not.toBeInTheDocument();
  });

  it('is three-column: no col④ context panel is revealed for System views', () => {
    renderShell('/environment');
    expect(screen.getByTestId('context-panel')).toHaveAttribute('data-open', 'false');
    cleanup();
    renderShell('/settings');
    expect(screen.getByTestId('context-panel')).toHaveAttribute('data-open', 'false');
  });

  it('the Settings col② item navigates to the Settings content', () => {
    renderShell('/environment');
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    fireEvent.click(within(nav).getByRole('link', { name: /settings/i }));
    expect(screen.getByTestId('page-Settings')).toBeInTheDocument();
  });

  // Regression (issue: clicking Version flipped col② to the Workspace group).
  // Root cause: 'version' was missing from the System module's pathPrefixes, so
  // detectActiveModule fell through to its workspace default on /version.
  it('keeps System active on /version (col② stays the System group, not Workspace)', () => {
    renderShell('/version');
    expect(screen.getByTestId('rail-module-system')).toHaveAttribute('data-active', 'true');
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav).getByTestId('section-label')).toHaveTextContent('System');
    expect(within(nav).getByRole('link', { name: /version/i })).toHaveAttribute('href', '/version');
    // Workspace items must NOT be present in col②.
    expect(within(nav).queryByRole('link', { name: /projects/i })).not.toBeInTheDocument();
  });
});
