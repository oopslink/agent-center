import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import type React from 'react';
import { App } from './App';
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
      // members-into-teams: /agents redirects into the merged Teams directory.
      [`${ORG_BASE}/agents`, 'page-TeamsDirectoryAgents'],
      [`${ORG_BASE}/agents/worker-1`, 'page-AgentDetail'],
      [`${ORG_BASE}/projects`, 'page-Projects'],
      [`${ORG_BASE}/projects/proj-a`, 'page-ProjectDetail'],
      // v2.9 #286: Plan orchestration — parallel list + Plan detail.
      [`${ORG_BASE}/projects/proj-a/plans`, 'page-ProjectPlans'],
      [`${ORG_BASE}/projects/proj-a/plans/PL-1`, 'page-PlanDetail'],
      // v2.10.0 [T6]: global cross-project Plan list.
      [`${ORG_BASE}/plans`, 'page-OrgPlans'],
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
    // T144: the plan NAME (not a separate "Open ▸") opens the Plan detail.
    const open = screen.getByTestId('plan-name-link-PL-1');
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
    // members-into-teams: the Members module is merged into Teams (teamui).
    expect(screen.getByTestId('rail-module-teamui')).toHaveAttribute('href', `${ORG_BASE}/teams`);
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
        // v2.10.0 [T6]: global Plan list, Tasks 平级.
        ['Plan', `${ORG_BASE}/plans`],
      ]],
      // v2.10.0 [T64]: Conversations owns a custom col② (ConversationsSecondaryNav)
      // — Channels / Direct messages SECTIONS (not nav-item links), asserted
      // separately below by their canonical create links.
      // members-into-teams: Teams owns a custom col② (TeamUISecondaryNav) — the
      // TEAMS group + the DIRECTORY group (Agents / Humans directory pages, the
      // merged surfaces).
      [`${ORG_BASE}/teams`, [
        ['All teams', `${ORG_BASE}/teams`],
        ['Templates', `${ORG_BASE}/teams/templates`],
        ['Agents', `${ORG_BASE}/teams/agents`],
        ['Humans', `${ORG_BASE}/teams/humans`],
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
    // v2.10.0 [T64]: Conversations custom col② — the Channels / Direct messages
    // sections carry create links to the canonical org-scoped index routes.
    {
      const { unmount } = renderAppAt(`${ORG_BASE}/channels`);
      const nav = await screen.findByRole('navigation', { name: /^primary$/ });
      expect(within(nav).getByTestId('conv-new-channel')).toHaveAttribute('href', `${ORG_BASE}/channels`);
      expect(within(nav).getByTestId('conv-new-dm')).toHaveAttribute('href', `${ORG_BASE}/dms`);
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

  // v2.10.0 [T4]: inside a project, the Workspace col② becomes the project
  // sub-nav (Issues/Tasks/Work Board/Members/Code repos + back to Projects),
  // and a sub-nav tab navigates the project's ?tab= (synced with the in-page
  // tab bar). The bare /projects list shows the top-level Workspace nav.
  it('project detail shows the col② project sub-nav; a tab drives ?tab=', async () => {
    await renderAt(`${ORG_BASE}/projects/proj-a`);
    await waitFor(() => expect(screen.getByTestId('page-ProjectDetail')).toBeInTheDocument());
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    // project sub-nav (not the top-level Projects/Issues/Tasks/Plan list).
    expect(within(nav).getByTestId('workspace-nav-project')).toBeInTheDocument();
    expect(within(nav).getByTestId('project-subnav-back')).toHaveAttribute('href', `${ORG_BASE}/projects`);
    expect(within(nav).getByTestId('project-subnav-tasks')).toHaveAttribute(
      'href',
      `${ORG_BASE}/projects/proj-a?tab=tasks`,
    );
    expect(within(nav).getByTestId('project-subnav-workboard')).toHaveAttribute(
      'href',
      `${ORG_BASE}/projects/proj-a/plans`,
    );
    // clicking the Tasks sub-nav entry drives ?tab=tasks → the Tasks panel shows.
    fireEvent.click(within(nav).getByTestId('project-subnav-tasks'));
    await waitFor(() => expect(screen.getByTestId('project-tasks-panel')).toBeInTheDocument());
    expect(window.location.search).toContain('tab=tasks');
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
  it('self change-password (Account tab) is reachable via the rail user link → /me page', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    // v2.10.1 [T105]: the rail user avatar opens a popout panel; "Your account"
    // inside it is the real "me" nav pointer (renders once /api/auth/me resolves).
    const userBtn = await screen.findByTestId('sidebar-user');
    fireEvent.click(userBtn);
    const userLink = await screen.findByTestId('rail-account-link');
    expect(userLink).toHaveAttribute('href', `${ORG_BASE}/me`);
    fireEvent.click(userLink);
    // Hex redesign: /me is now a standalone Account settings page.
    await waitFor(() => expect(screen.getByTestId('page-Me')).toBeInTheDocument());
    expect(screen.getByRole('button', { name: /change password/i })).toBeInTheDocument();
  });

  // §4.2 reachability (orphan hunt): the legacy /members/new "Add Agent" page
  // redirects to the canonical /agents page.
  // members-into-teams: /members/new → ../agents, which now chains to the merged
  // teams/agents directory (the org agents list is folded into Teams).
  it('redirects the orphaned /members/new URL to the merged teams/agents page', async () => {
    await renderAt(`${ORG_BASE}/members/new?kind=agent`);
    await waitFor(() => expect(screen.getByTestId('page-TeamsDirectoryAgents')).toBeInTheDocument());
    expect(screen.queryByTestId('page-MemberNew')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/teams/agents`);
  });

  // members-into-teams: /members/agents → ../agents, which now chains to the
  // merged teams/agents directory.
  it('redirects the retired /members/agents URL to the merged teams/agents page', async () => {
    await renderAt(`${ORG_BASE}/members/agents`);
    await waitFor(() => {
      expect(screen.getByTestId('page-TeamsDirectoryAgents')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('page-MembersAgents')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/teams/agents`);
  });

  // members-into-teams: the legacy /members/humans list is merged into the Teams
  // directory. Hitting the old URL redirects to the merged teams/humans page.
  it('redirects the retired /members/humans URL to the merged teams/humans page', async () => {
    await renderAt(`${ORG_BASE}/members/humans`);
    await waitFor(() => expect(screen.getByTestId('page-TeamsDirectoryHumans')).toBeInTheDocument());
    expect(screen.queryByTestId('page-MembersHumans')).not.toBeInTheDocument();
    expect(window.location.pathname).toBe(`${ORG_BASE}/teams/humans`);
  });

  // members-into-teams: the org-level /agents list is merged into teams/agents.
  it('redirects the org /agents list URL to the merged teams/agents page', async () => {
    await renderAt(`${ORG_BASE}/agents`);
    await waitFor(() => expect(screen.getByTestId('page-TeamsDirectoryAgents')).toBeInTheDocument());
    expect(window.location.pathname).toBe(`${ORG_BASE}/teams/agents`);
  });

  it('the Teams directory col② links point at the merged agents / humans pages', async () => {
    await renderAt(`${ORG_BASE}/teams`);
    await waitFor(() => expect(screen.getByTestId('page-Teams')).toBeInTheDocument());
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(within(nav).getByTestId('teamui-nav-agents')).toHaveAttribute('href', `${ORG_BASE}/teams/agents`);
    const humansLink = within(nav).getByTestId('teamui-nav-humans');
    expect(humansLink).toHaveAttribute('href', `${ORG_BASE}/teams/humans`);
    fireEvent.click(humansLink);
    await waitFor(() => expect(screen.getByTestId('page-TeamsDirectoryHumans')).toBeInTheDocument());
  });

  it('organization-settings/agents renders the canonical Agents page', async () => {
    await renderAt(`${ORG_BASE}/organization-settings/agents`);
    await waitFor(() => expect(screen.getByTestId('page-Agents')).toBeInTheDocument());
    expect(screen.queryByTestId('page-MembersAgents')).not.toBeInTheDocument();
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

  // I41 (T470): the switcher gear opens Organization Settings as a routed page
  // whose 5 sections live in the shell's col② secondary nav (NOT a page-internal
  // card-nav, and no longer a modal). The bare path lands on the Profile section.
  it('opens per-org settings as a col② page from the switcher gear, no standalone entry (#186-6 / T470)', async () => {
    await renderAt(`${ORG_BASE}/channels`);
    await waitFor(() => expect(screen.getByTestId('page-Channels')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('org-switcher'));
    expect(screen.queryByTestId('org-dropdown-settings')).not.toBeInTheDocument();
    const gear = await screen.findByTestId('org-settings-gear');
    expect(gear).toHaveAttribute('data-org-id', 'org-test');
    fireEvent.click(gear);
    // Navigates to the org-settings page (bare path redirects to /profile).
    await waitFor(() =>
      expect(window.location.pathname).toBe(`${ORG_BASE}/organization-settings/profile`),
    );
    // Profile section (the default) shows the current org name.
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
    // col① — all four modules present on the rail (members-into-teams: teamui).
    for (const id of ['workspace', 'conversations', 'teamui', 'system']) {
      expect(screen.getByTestId(`rail-module-${id}`)).toBeInTheDocument();
    }
    // col② — the active (Conversations) module's custom nav (T64): the Channels
    // and Direct messages sections; other modules' items are NOT in the secondary
    // nav (only the rail switches to them).
    const nav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(nav).toHaveTextContent('Channels');
    expect(nav).toHaveTextContent('Direct messages');
    expect(within(nav).queryByRole('link', { name: /^settings$/i })).not.toBeInTheDocument();
    // No Overview/Home anywhere.
    expect(nav).not.toHaveTextContent('Overview');
  });
});

// silence unused React import
const _react: typeof React = (undefined as unknown) as typeof React;
void _react;
