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
  it('renders Home / Overview at /organizations/test (v2.6 org-scoped routing)', async () => {
    await renderAt(`${ORG_BASE}`);
    await waitFor(() => {
      expect(screen.getByTestId('page-Home')).toBeInTheDocument();
    });
  });

  it('renders ChannelDetail for /organizations/test/channels/:name', async () => {
    await renderAt(`${ORG_BASE}/channels/alpha`);
    await waitFor(() => {
      expect(screen.getByTestId('page-ChannelDetail')).toBeInTheDocument();
    });
    // Full-route-tree render is heavy; give headroom under full-suite runner load
    // (passes well under default in isolation, but the saturated 97-file run can
    // exceed the 5s default → explicit timeout keeps the full suite reliably green).
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
    // 14 sequential full-route-tree renders — heavy; explicit timeout for headroom
    // under full-suite runner load (green in isolation; saturated run can exceed 5s).
  }, 20000);

  // v2.9 #286 §4.2 reachability: Plans are reached via the project detail page
  // (Plans link), then a Plan card reaches the Plan detail (DAG #287) — a real
  // nav chain, NOT a direct-URL-only orphan route.
  it('reaches the Plan list + Plan detail via the project detail page (not direct-URL-only)', async () => {
    await renderAt(`${ORG_BASE}/projects/proj-a`);
    // project detail → Plans entry link points at the per-project plans route.
    const plansLink = await screen.findByTestId('project-plans-link');
    expect(plansLink).toHaveAttribute('href', `${ORG_BASE}/projects/proj-a/plans`);
    fireEvent.click(plansLink);
    // lands on the Work Board (the #291 refactored ProjectPlans board).
    await waitFor(() => expect(screen.getByTestId('page-ProjectPlans')).toBeInTheDocument());
    await waitFor(() => expect(screen.getByTestId('work-board')).toBeInTheDocument());
    // a Plan column's "Open ▸" → Plan detail (the #287 DAG view route).
    const open = screen.getByTestId('plan-open-PL-1');
    fireEvent.click(open);
    await waitFor(() => expect(screen.getByTestId('page-PlanDetail')).toBeInTheDocument());
    expect(window.location.pathname).toBe(`${ORG_BASE}/projects/proj-a/plans/PL-1`);
  });

  // §4.2 reachability: the primary sidebar nav must point at the canonical
  // org-scoped routes (not bare/legacy paths). These assert the LINK/href, not
  // a direct-URL render — the whole point of the audit.
  it('sidebar nav links point at the canonical org-scoped Workspace/Conversations/System routes', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    const nav = screen.getByRole('navigation', { name: /primary/i });
    const linkByLabel = (label: string) =>
      within(nav)
        .getAllByRole('link')
        .find((a) => a.textContent?.trim().startsWith(label));
    const expected: Array<[string, string]> = [
      ['Projects', `${ORG_BASE}/projects`],
      ['Issues', `${ORG_BASE}/issues`],
      ['Tasks', `${ORG_BASE}/tasks`],
      ['Channels', `${ORG_BASE}/channels`],
      ['DMs', `${ORG_BASE}/dms`],
      ['Humans', `${ORG_BASE}/members/humans`],
      ['Environment', `${ORG_BASE}/environment`],
      ['Settings', `${ORG_BASE}/settings`],
    ];
    for (const [label, href] of expected) {
      const link = linkByLabel(label);
      expect(link, `nav link for ${label}`).toBeDefined();
      expect(link).toHaveAttribute('href', href);
    }
  });

  // §4.2 reachability: ProjectDetail is reached from the Projects list (a real
  // row link), not direct-URL-only.
  it('reaches ProjectDetail via a Projects-list row link', async () => {
    await renderAt(`${ORG_BASE}/projects`);
    await waitFor(() => expect(screen.getByTestId('page-Projects')).toBeInTheDocument());
    const row = await screen.findByRole('link', { name: /project alpha/i });
    expect(row).toHaveAttribute('href', `${ORG_BASE}/projects/proj-a`);
    fireEvent.click(row);
    await waitFor(() => expect(screen.getByTestId('page-ProjectDetail')).toBeInTheDocument());
  });

  // §4.2 reachability (A6 task→new-tab): the canonical TaskDetail route is
  // reachable via the new-tab TaskTitleLink anchor on the Plan board cards —
  // a real anchor with target=_blank pointing at the org-scoped task route, not
  // a direct-URL-only orphan. (Asserts the href; new tabs can't be followed in
  // jsdom, but the anchor IS the real reachability pointer.)
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

  // §4.2 reachability (self change-password): the self Account tab (the
  // change-password panel) must be reachable via a REAL entry — the sidebar
  // user link → /me → UserDetail?tab=account — not just by typing the
  // users/:id?tab=account URL. This was specifically flagged.
  it('self change-password (Account tab) is reachable via the sidebar user link → /me redirect', async () => {
    // UserDetail has no canonical mock handler (per-test server.use); register
    // the self user so /me's redirect resolves the account tab.
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
    // The sidebar user entry is the real "me" nav pointer (renders once the
    // /api/auth/me display_name resolves).
    const userLink = await screen.findByTestId('sidebar-user');
    expect(userLink).toHaveAttribute('href', `${ORG_BASE}/me`);
    fireEvent.click(userLink);
    // /me redirects (replace) to the self UserDetail with the Account tab.
    await waitFor(() => expect(screen.getByTestId('page-UserDetail')).toBeInTheDocument());
    await waitFor(() => expect(screen.getByTestId('account-panel')).toBeInTheDocument());
    expect(screen.getByRole('button', { name: /change password/i })).toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/users/user-test`);
  });

  // §4.2 reachability (orphan hunt): the legacy /members/new "Add Agent" page is
  // an ORPHAN — its only inbound link lived on the retired /members/agents page,
  // and the canonical /agents surface now creates agents via an inline modal. It
  // must redirect to the canonical /agents page (matching the /members/agents and
  // /fleet retirement precedent) so the stale URL lands on a reachable surface
  // and there is no direct-URL-only orphan page.
  it('redirects the orphaned /members/new URL to the canonical /agents page', async () => {
    await renderAt(`${ORG_BASE}/members/new?kind=agent`);
    await waitFor(() => expect(screen.getByTestId('page-Agents')).toBeInTheDocument());
    expect(screen.queryByTestId('page-MemberNew')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/agents`);
  });

  // dev2/v281: the enhanced /agents page is the single canonical agents
  // surface. The old /members/agents URL must redirect there (no second
  // reachable agents page) and the "Agents" nav click must reach the
  // enhanced page, not the old one.
  it('redirects the retired /members/agents URL to the canonical /agents page', async () => {
    await renderAt(`${ORG_BASE}/members/agents`);
    await waitFor(() => {
      // lands on the ENHANCED Agents page (Name/Provider/Lifecycle/…)
      expect(screen.getByTestId('page-Agents')).toBeInTheDocument();
    });
    // the old MembersAgents page is NOT what rendered.
    expect(screen.queryByTestId('page-MembersAgents')).not.toBeInTheDocument();
    // URL landed on canonical /agents (replace — no /members/agents in history).
    expect(window.location.pathname).toBe(`${ORG_BASE}/agents`);
  });

  it('"Agents" nav item points to the enhanced /agents route, not /members/agents', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    const nav = screen.getByRole('navigation', { name: /primary/i });
    const agentsLink = within(nav)
      .getAllByRole('link')
      .find((a) => a.textContent?.trim().startsWith('Agents'));
    expect(agentsLink).toBeDefined();
    expect(agentsLink).toHaveAttribute('href', `${ORG_BASE}/agents`);
    expect(agentsLink).not.toHaveAttribute('href', `${ORG_BASE}/members/agents`);
    // Clicking it reaches the ENHANCED page.
    fireEvent.click(agentsLink!);
    await waitFor(() => expect(screen.getByTestId('page-Agents')).toBeInTheDocument());
  });

  it('⌘6 jumps to the enhanced /agents page (org-scoped), not /members/agents', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    fireEvent.keyDown(window, { key: '6', metaKey: true });
    await waitFor(() => expect(screen.getByTestId('page-Agents')).toBeInTheDocument());
    expect(window.location.pathname).toBe(`${ORG_BASE}/agents`);
    expect(screen.queryByTestId('page-MembersAgents')).not.toBeInTheDocument();
  });

  it('opens per-org settings as a modal from the switcher gear, no standalone entry (#186-6)', async () => {
    await renderAt(`${ORG_BASE}`);
    await waitFor(() => expect(screen.getByTestId('page-Home')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('org-switcher'));
    // The old single "Organization Settings" dropdown entry is gone.
    expect(screen.queryByTestId('org-dropdown-settings')).not.toBeInTheDocument();
    // Each org row has its own gear → opens the per-org settings modal.
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

  it('renders the sidebar nav sections', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => {
      expect(screen.getByTestId('page-Channels')).toBeInTheDocument();
    });
    const nav = screen.getByRole('navigation', { name: /primary/i });
    for (const label of [
      'Channels',
      'DMs',
      'Projects',
      // v2.8 #258: org-scope cross-project Issues/Tasks aggregation nav (Workspace).
      'Issues',
      'Tasks',
      'Agents',
      'Settings',
      // v2.7 #166: org people group is "Members" (Humans + single "Agents").
      'Members',
      'Humans',
      // v2.7 #164: Fleet merged into Environment — single "Environment" entry.
      'Environment',
    ]) {
      expect(nav).toHaveTextContent(label);
    }
    // v2.7 #166-1: org group renamed Organization → Members.
    expect(nav).not.toHaveTextContent('Organization');
    // v2.7 #166-2: Organization Settings moved off the sidebar into the org switcher.
    expect(nav).not.toHaveTextContent('Organization Settings');
    expect(nav).not.toHaveTextContent('Org Settings');
    expect(nav).not.toHaveTextContent('Agents (org)');
    // v2.7 #164: Fleet entry removed (merged into Environment).
    expect(nav).not.toHaveTextContent('Fleet');
    // Input Requests nav entry removed (#131 PR-4).
    expect(nav).not.toHaveTextContent('Input Requests');
    // v2.8 #258: Issues / Tasks reintroduced as org-scope cross-project nav
    // (asserted present in the loop above).
  });
});

// silence unused React import
const _react: typeof React = (undefined as unknown) as typeof React;
void _react;
