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
  it('renders Home / Overview at / (v2.3 P3 — previously redirected to /channels)', async () => {
    await renderAt('/');
    await waitFor(() => {
      expect(screen.getByTestId('page-Home')).toBeInTheDocument();
    });
    expect(window.location.pathname).toBe('/');
  });

  it('renders ChannelDetail for /channels/:name', async () => {
    await renderAt('/channels/alpha');
    await waitFor(() => {
      expect(screen.getByTestId('page-ChannelDetail')).toBeInTheDocument();
    });
  });

  it('renders DMs / Issues / IssueDetail / Tasks / TaskDetail / TaskTrace / Agents / AgentDetail / InputRequests / Secrets / Fleet / Settings', async () => {
    const cases: Array<[string, string]> = [
      ['/dms', 'page-DMs'],
      ['/dms/01HXXX', 'page-DMDetail'],
      ['/issues', 'page-Issues'],
      ['/issues/01HXXX', 'page-IssueDetail'],
      ['/tasks', 'page-Tasks'],
      ['/tasks/01HXXX', 'page-TaskDetail'],
      ['/tasks/01HXXX/trace', 'page-TaskTrace'],
      ['/agents', 'page-Agents'],
      ['/agents/worker-1', 'page-AgentDetail'],
      ['/inputrequests', 'page-InputRequests'],
      ['/secrets', 'page-Secrets'],
      ['/fleet', 'page-Fleet'],
      ['/settings', 'page-Settings'],
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
    await renderAt('/definitely-not-a-route');
    await waitFor(() => {
      expect(screen.getByTestId('page-NotFound')).toBeInTheDocument();
    });
    const home = screen.getByTestId('nav-home');
    expect(home).toHaveAttribute('href', '/channels');
  });

  it('renders the sidebar nav with the 7 sections', async () => {
    await renderAt('/channels');
    await waitFor(() => {
      expect(screen.getByTestId('page-Channels')).toBeInTheDocument();
    });
    const nav = screen.getByRole('navigation', { name: /primary/i });
    for (const label of [
      'Channels',
      'DMs',
      'Issues',
      'Tasks',
      'Input Requests',
      'Agents',
      'Settings',
    ]) {
      expect(nav).toHaveTextContent(label);
    }
  });
});

// silence unused React import
const _react: typeof React = (undefined as unknown) as typeof React;
void _react;
