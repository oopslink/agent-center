import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import type React from 'react';
import { App } from './App';
import { server } from './test/mswServer';
import { FakeEventSource } from './sse/fakeEventSource';

// AppLayout opens a single EventSource on mount via useSSE. jsdom has no
// EventSource global, so install the FakeEventSource shim for the
// duration of these tests.
beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource =
    FakeEventSource;
});

afterEach(() => cleanup());

// MSW mock for /api/orgs returns slug='test', so org base is /organizations/test.
const ORG_BASE = '/organizations/test';

async function renderAt(path: string): Promise<void> {
  window.history.pushState({}, '', path);
  render(
    <QueryClientProvider client={new QueryClient()}>
      <App />
    </QueryClientProvider>,
  );
}

// Helper for the round-trip test (each iteration owns its render lifecycle).
function renderAppAt(path: string) {
  window.history.pushState({}, '', path);
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <App />
    </QueryClientProvider>,
  );
}

describe('App shell + route tree', () => {
  // v2.10.0 [T1]: Overview/Home is removed — the org index redirects into the
  // Workspace module's default page (Projects).
  it('redirects the org index to the Workspace module (Projects), no Overview/Home', async () => {
    await renderAt(`${ORG_BASE}`);
    await waitFor(() => {
      expect(screen.getByTestId('page-Projects')).toBeInTheDocument();
    });
    expect(window.location.pathname).toBe(`${ORG_BASE}/projects`);
    expect(screen.queryByTestId('page-Home')).not.toBeInTheDocument();
  });

  it('renders ChannelDetail for /organizations/test/channels/:name', async () => {
    await renderAt(`${ORG_BASE}/channels/alpha`);
    await waitFor(() => {
      expect(screen.getByTestId('page-ChannelDetail')).toBeInTheDocument();
    });
  }, 20000);

  it('renders DMs / nested IssueDetail / nested TaskDetail / Agents / AgentDetail / Projects / ProjectDetail / Secrets / Fleet / Settings', async () => {
    const cases: Array<[string, string]> = [
      [`${ORG_BASE}/dms`, 'page-DMs'],
      [`${ORG_BASE}/dms/01HXXX`, 'page-DMDetail'],
      [`${ORG_BASE}/projects/proj-a/issues/01HXXX`, 'page-IssueDetail'],
      [`${ORG_BASE}/projects/proj-a/tasks/01HXXX`, 'page-TaskDetail'],
      [`${ORG_BASE}/agents`, 'page-Agents'],
      [`${ORG_BASE}/agents/worker-1`, 'page-AgentDetail'],
      [`${ORG_BASE}/projects`, 'page-Projects'],
      [`${ORG_BASE}/projects/proj-a`, 'page-ProjectDetail'],
      // v2.9 #286: Plan orchestration — parallel list + Plan detail.
      [`${ORG_BASE}/projects/proj-a/plans`, 'page-ProjectPlans'],
      [`${ORG_BASE}/projects/proj-a/plans/PL-1`, 'page-PlanDetail'],
      [`${ORG_BASE}/secrets`, 'page-Secrets'],
      [`${ORG_BASE}/environment`, 'page-Environment'],
      // v2.7 #164: Fleet merged into Environment; /fleet redirects to /environment.
      [`${ORG_BASE}/fleet`, 'page-Environment'],
      [`${ORG_BASE}/settings`, 'page-Settings'],
    ];
    for (const [path, testId] of cases) {
      const { unmount } = renderAppAt(path);
      await waitFor(() => {
        expect(screen.getByTestId(testId)).toBeInTheDocument();
      });
      unmount();
    }
  }, 20000);

  // v2.9 #286 §4.2 reachability: Plans are reached via the project detail page.
  it('reaches the Plan list + Plan detail via the project detail page (not direct-URL-only)', async () => {
    await renderAt(`${ORG_BASE}/projects/proj-a`);
    const plansLink = await screen.findByTestId('project-plans-link');
    expect(plansLink).toHaveAttribute('href', `${ORG_BASE}/projects/proj-a/plans`);
    fireEvent.click(plansLink);
    await waitFor(() => expect(screen.getByTestId('page-ProjectPlans')).toBeInTheDocument());
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const open = screen.getByTestId('plan-open-PL-1');
    fireEvent.click(open);
    await waitFor(() => expect(screen.getByTestId('page-PlanDetail')).toBeInTheDocument());
    expect(window.location.pathname).toBe(`${ORG_BASE}/projects/proj-a/plans/PL-1`);
  });

  // v2.10.0 [T1] reachability: the col① module rail links point at the
  // canonical org-scoped default page for each module (Workspace/Conversations/
  // Members/System). Asserts the LINK/href, not a direct-URL render.
  it('rail module icons point at the canonical org-scoped module defaults', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    expect(screen.getByTestId('rail-module-workspace')).toHaveAttribute('href', `${ORG_BASE}/projects`);
    expect(screen.getByTestId('rail-module-conversations')).toHaveAttribute('href', `${ORG_BASE}/channels`);
    expect(screen.getByTestId('rail-module-members')).toHaveAttribute('href', `${ORG_BASE}/members/humans`);
    expect(screen.getByTestId('rail-module-system')).toHaveAttribute('href', `${ORG_BASE}/environment`);
  });

  // v2.10.0 [T1] reachability: each module's col② shows ITS second-level nav,
  // pointing at the canonical org-scoped routes. Rendered per module route.
  it('each module col② links point at the canonical org-scoped routes', async () => {
    const perModule: Array<[string, Array<[string, string]>]> = [
      [`${ORG_BASE}/projects`, [
        ['Projects', `${ORG_BASE}/projects`],
        ['Issues', `${ORG_BASE}/issues`],
        ['Tasks', `${ORG_BASE}/tasks`],
      ]],
      [`${ORG_BASE}/channels`, [
        ['Channels', `${ORG_BASE}/channels`],
        ['DMs', `${ORG_BASE}/dms`],
      ]],
      // v2.10.0 [T7]: Members owns a custom col② (MembersSecondaryNav) — Humans
      // / Agents sections each with an "All …" row to the list/table page.
      [`${ORG_BASE}/members/humans`, [
        ['All humans', `${ORG_BASE}/members/humans`],
        ['All agents', `${ORG_BASE}/agents`],
      ]],
      [`${ORG_BASE}/environment`, [
        ['Environment', `${ORG_BASE}/environment`],
        ['Settings', `${ORG_BASE}/settings`],
      ]],
    ];
    for (const [route, expected] of perModule) {
      const { unmount } = renderAppAt(route);
      const nav = await screen.findByRole('navigation', { name: /^primary$/ });
      for (const [label, href] of expected) {
        const link = within(nav)
          .getAllByRole('link')
          .find((a) => a.textContent?.trim().startsWith(label));
        expect(link, `col② link for ${label} on ${route}`).toBeDefined();
        expect(link).toHaveAttribute('href', href);
      }
      unmount();
    }
  }, 20000);

  // §4.2 reachability: ProjectDetail is reached from the Projects list row.
  it('reaches ProjectDetail via a Projects-list row link', async () => {
    await renderAt(`${ORG_BASE}/projects`);
    await waitFor(() => expect(screen.getByTestId('page-Projects')).toBeInTheDocument());
    const row = await screen.findByRole('link', { name: /project alpha/i });
    expect(row).toHaveAttribute('href', `${ORG_BASE}/projects/proj-a`);
    fireEvent.click(row);
    await waitFor(() => expect(screen.getByTestId('page-ProjectDetail')).toBeInTheDocument());
  });

  // §4.2 reachability (A6 task→new-tab): canonical TaskDetail route reachable
  // via the new-tab TaskTitleLink anchor on the Plan board cards.
  it('A6 TaskDetail is reachable via the new-tab TaskTitleLink anchor on the Plan board', async () => {
    await renderAt(`${ORG_BASE}/projects/proj-a/plans`);
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    const taskAnchors = await screen.findAllByTestId(/^task-open-link-/);
    expect(taskAnchors.length).toBeGreaterThan(0);
    const anchor = taskAnchors[0];
    expect(anchor).toHaveAttribute('target', '_blank');
    expect(anchor).toHaveAttribute('rel', expect.stringContaining('noopener'));
    expect(anchor.getAttribute('href')).toMatch(
      new RegExp(`^${ORG_BASE}/projects/proj-a/tasks/`),
    );
  });

  // §4.2 reachability (self change-password): the self Account tab is reachable
  // via the rail user link → /me redirect → UserDetail?tab=account.
  it('self change-password (Account tab) is reachable via the rail user link → /me redirect', async () => {
    server.use(
      http.get('/api/users/user-test', () =>
        HttpResponse.json({
          user_id: 'user-test',
          display_name: 'Test User',
          email: 'test@example.com',
          created_at: '2026-05-20T01:00:00Z',
          memberships: [],
        }),
      ),
    );
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    // The rail user entry is the real "me" nav pointer (renders once the
    // /api/auth/me display_name resolves).
    const userLink = await screen.findByTestId('sidebar-user');
    expect(userLink).toHaveAttribute('href', `${ORG_BASE}/me`);
    fireEvent.click(userLink);
    await waitFor(() => expect(screen.getByTestId('page-UserDetail')).toBeInTheDocument());
    await waitFor(() => expect(screen.getByTestId('account-panel')).toBeInTheDocument());
    expect(screen.getByRole('button', { name: /change password/i })).toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/users/user-test`);
  });

  // §4.2 reachability (orphan hunt): the legacy /members/new "Add Agent" page
  // redirects to the canonical /agents page.
  it('redirects the orphaned /members/new URL to the canonical /agents page', async () => {
    await renderAt(`${ORG_BASE}/members/new?kind=agent`);
    await waitFor(() => expect(screen.getByTestId('page-Agents')).toBeInTheDocument());
    expect(screen.queryByTestId('page-MemberNew')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/agents`);
  });

  // dev2/v281: the old /members/agents URL redirects to the canonical /agents.
  it('redirects the retired /members/agents URL to the canonical /agents page', async () => {
    await renderAt(`${ORG_BASE}/members/agents`);
    await waitFor(() => {
      expect(screen.getByTestId('page-Agents')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('page-MembersAgents')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/agents`);
  });

  it('"Agents" col② nav item (Members module) points to the enhanced /agents route', async () => {
    await renderAt(`${ORG_BASE}/members/humans`);
    await waitFor(() => expect(screen.getByTestId('page-MembersHumans')).toBeInTheDocument());
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    // v2.10.0 [T7]: the Agents section's "All agents" row is the canonical link.
    const agentsLink = within(nav)
      .getAllByRole('link')
      .find((a) => a.textContent?.trim().startsWith('All agents'));
    expect(agentsLink).toBeDefined();
    expect(agentsLink).toHaveAttribute('href', `${ORG_BASE}/agents`);
    expect(agentsLink).not.toHaveAttribute('href', `${ORG_BASE}/members/agents`);
    fireEvent.click(agentsLink!);
    await waitFor(() => expect(screen.getByTestId('page-Agents')).toBeInTheDocument());
  });

  // v2.10.0 [T1]: ⌘1..4 jump to the four modules' default pages (org-scoped).
  it('⌘1 / ⌘2 jump to the Workspace / Conversations module defaults (org-scoped)', async () => {
    await renderAt(`${ORG_BASE}/environment`);
    await waitFor(() => expect(screen.getByTestId('page-Environment')).toBeInTheDocument());
    fireEvent.keyDown(window, { key: '1', metaKey: true });
    await waitFor(() => expect(screen.getByTestId('page-Projects')).toBeInTheDocument());
    expect(window.location.pathname).toBe(`${ORG_BASE}/projects`);
    fireEvent.keyDown(window, { key: '2', metaKey: true });
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    expect(window.location.pathname).toBe(`${ORG_BASE}/channels`);
  });

  it('opens per-org settings as a modal from the switcher gear, no standalone entry (#186-6)', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('org-switcher'));
    expect(screen.queryByTestId('org-dropdown-settings')).not.toBeInTheDocument();
    const gear = await screen.findByTestId('org-settings-gear');
    expect(gear).toHaveAttribute('data-org-id', 'org-test');
    fireEvent.click(gear);
    const modal = await screen.findByTestId('org-settings-modal');
    expect(modal).toBeInTheDocument();
    await waitFor(() =>
      expect((screen.getByTestId('org-settings-name') as HTMLInputElement).value).toBe('Test Org'),
    );
  });

  it('falls back to the 404 page with a nav link home', async () => {
    await renderAt(`${ORG_BASE}/definitely-not-a-route`);
    await waitFor(() => {
      expect(screen.getByTestId('page-NotFound')).toBeInTheDocument();
    });
    const home = screen.getByTestId('nav-home');
    expect(home).toHaveAttribute('href', '/');
  });

  it('renders the four module rail icons + the active module col② items', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => {
      expect(screen.getByTestId('page-Channels')).toBeInTheDocument();
    });
    // col① — all four modules present on the rail.
    for (const id of ['workspace', 'conversations', 'members', 'system']) {
      expect(screen.getByTestId(`rail-module-${id}`)).toBeInTheDocument();
    }
    // col② — the active (Conversations) module's items; other modules' items
    // are NOT in the secondary nav (only the rail switches to them).
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(nav).toHaveTextContent('Channels');
    expect(nav).toHaveTextContent('DMs');
    expect(within(nav).queryByRole('link', { name: /^settings$/i })).not.toBeInTheDocument();
    // No Overview/Home anywhere.
    expect(nav).not.toHaveTextContent('Overview');
  });
});

// silence unused React import
const _react: typeof React = (undefined as unknown) as typeof React;
void _react;
