import type React from 'react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { FakeEventSource } from '@/sse/fakeEventSource';
import { ContextPanel } from '@/shell/contextPanel';
// v2.10.0 [T64]: this suite asserts the DEFAULT col② (built-in NavGroup per
// module). Conversations now registers a per-module override, so mock the
// registry empty to keep exercising the default-fallback path (the registered
// override is covered by shell/nav/ConversationsSecondaryNav.test.tsx).
vi.mock('@/shell/secondaryNav', () => ({ SECONDARY_NAV_REGISTRY: {} }));
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
            <Route path="/reminders" element={<div data-testid="page-Reminders">x</div>} />
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

  // T207 [提醒]: Reminders is a TOP-LEVEL module (peer of Members), not a
  // Workspace col② item. The rail + mobile tab bar both expose it.
  it('Reminders is a top-level rail module (peer of Members), linking to /reminders', () => {
    renderShell('/reminders');
    const rail = screen.getByRole('navigation', { name: /^modules$/ });
    expect(within(rail).getByTestId('rail-module-reminders')).toHaveAttribute('href', '/reminders');
    expect(within(rail).getByTestId('rail-module-reminders')).toHaveAttribute('data-active', 'true');
    // and on the mobile bottom tab bar.
    const tabbar = screen.getByRole('navigation', { name: 'modules mobile' });
    expect(within(tabbar).getByTestId('tab-reminders')).toHaveAttribute('href', '/reminders');
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

  // v2.10.2 [T128]: the col④ COLUMN itself is the resizable surface — the
  // left-edge grip drives the whole sidebar's width (a CSS var consumed by
  // md:w-[var(--ctx-w)]), not the inner panel content (the bug T128 fixed).
  it('col④ has a left-edge resize handle; a left-drag widens the whole column and persists it', () => {
    // jsdom's localStorage is method-less here; install a real Map-backed stub.
    const store = new Map<string, string>();
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
      setItem: (k: string, v: string) => void store.set(k, String(v)),
      removeItem: (k: string) => void store.delete(k),
      clear: () => void store.clear(),
    });
    renderShell('/panel');
    const ctx = screen.getByTestId('context-panel');
    // Default = the prior w-64 (256px), carried on the --ctx-w CSS var.
    expect(ctx.getAttribute('style')).toContain('--ctx-w: 256px');
    const handle = screen.getByTestId('context-panel-resize');
    expect(handle).toHaveAttribute('aria-orientation', 'vertical');
    // Left-edge handle on a right-anchored column: dragging LEFT widens.
    fireEvent.mouseDown(handle, { clientX: 900 });
    fireEvent.mouseMove(window, { clientX: 850 }); // 50px left -> +50
    fireEvent.mouseUp(window, { clientX: 850 });
    expect(ctx.getAttribute('style')).toContain('--ctx-w: 306px');
    expect(localStorage.getItem('ac.contextpanel.width')).toBe('306');
    vi.unstubAllGlobals();
  });

  // v2.10.1 [M1] mobile (<768) shell: bottom Tab Bar (col①) + top bar actions
  // + col④ context as a dismissible bottom sheet + account/org/theme sheet.
  it('mobile bottom tab bar lists the four modules, links to defaults, marks active, ≥44px targets', () => {
    renderShell('/channels');
    const tabbar = screen.getByRole('navigation', { name: 'modules mobile' });
    expect(within(tabbar).getByTestId('tab-workspace')).toHaveAttribute('href', '/projects');
    expect(within(tabbar).getByTestId('tab-conversations')).toHaveAttribute('href', '/channels');
    expect(within(tabbar).getByTestId('tab-members')).toHaveAttribute('href', '/members/humans');
    expect(within(tabbar).getByTestId('tab-system')).toHaveAttribute('href', '/environment');
    // On /channels the Conversations tab is active.
    expect(within(tabbar).getByTestId('tab-conversations')).toHaveAttribute('data-active', 'true');
    expect(within(tabbar).getByTestId('tab-workspace')).toHaveAttribute('data-active', 'false');
    // Touch baseline: every tab is a ≥44px target.
    expect(within(tabbar).getByTestId('tab-workspace').className).toContain('min-h-[44px]');
  });

  it('the old hamburger drawer is gone (replaced by the bottom tab bar)', () => {
    renderShell('/channels');
    expect(screen.queryByTestId('nav-toggle')).not.toBeInTheDocument();
  });

  it('mobile top bar does not show a context panel toggle (context panel is desktop-only)', () => {
    renderShell('/panel');
    const ctx = screen.getByTestId('context-panel');
    expect(ctx).toHaveAttribute('data-open', 'true');
    // No mobile context toggle — context panel content is desktop-only;
    // mobile pages use their own info surfaces (Actions > Show info).
    expect(screen.queryByTestId('mobile-context-toggle')).not.toBeInTheDocument();
  });

  it('the top-bar ⓘ is absent when no context panel is mounted', () => {
    renderShell('/channels');
    expect(screen.queryByTestId('mobile-context-toggle')).not.toBeInTheDocument();
  });

  it('mobile account sheet exposes org switch / account / theme / sign out', () => {
    renderShell('/channels');
    expect(screen.queryByTestId('account-sheet')).not.toBeInTheDocument();
    fireEvent.click(screen.getByTestId('mobile-account-toggle'));
    const sheet = screen.getByTestId('account-sheet');
    expect(sheet).toHaveAttribute('role', 'dialog');
    expect(within(sheet).getByTestId('account-profile-link')).toHaveAttribute('href', '/me');
    expect(within(sheet).getByTestId('theme-toggle')).toBeInTheDocument();
    expect(within(sheet).getByTestId('account-signout')).toBeInTheDocument();
  });
});
