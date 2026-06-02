import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeAll, describe, expect, it } from 'vitest';
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
  });

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
    // Issues / Tasks no longer have global nav entries (v2.7).
    expect(nav).not.toHaveTextContent('Issues');
    expect(nav).not.toHaveTextContent('Tasks');
  });
});

// silence unused React import
const _react: typeof React = (undefined as unknown) as typeof React;
void _react;
