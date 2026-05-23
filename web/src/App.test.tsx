import { render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { App } from './App';

// Each test pushes a fresh URL onto history before rendering so the
// router picks the right leaf. Lazy chunks resolve asynchronously, so
// page assertions use waitFor.

async function renderAt(path: string): Promise<void> {
  window.history.pushState({}, '', path);
  render(<App />);
}

describe('App shell + route tree', () => {
  it('redirects / to /channels and renders the Channels page', async () => {
    await renderAt('/');
    await waitFor(() => {
      expect(screen.getByTestId('page-Channels')).toBeInTheDocument();
    });
    expect(window.location.pathname).toBe('/channels');
  });

  it('renders ChannelDetail for /channels/:name', async () => {
    await renderAt('/channels/alpha');
    await waitFor(() => {
      expect(screen.getByTestId('page-ChannelDetail')).toBeInTheDocument();
    });
  });

  it('renders DMs / DMDetail / Issues / Tasks / TaskTrace / Agents / AgentDetail / InputRequests / Secrets / Fleet / Settings', async () => {
    const cases: Array<[string, string]> = [
      ['/dms', 'page-DMs'],
      ['/dms/01HXXX', 'page-DMDetail'],
      ['/issues', 'page-Issues'],
      ['/tasks', 'page-Tasks'],
      ['/tasks/01HXXX/trace', 'page-TaskTrace'],
      ['/agents', 'page-Agents'],
      ['/agents/worker-1', 'page-AgentDetail'],
      ['/inputrequests', 'page-InputRequests'],
      ['/secrets', 'page-Secrets'],
      ['/fleet', 'page-Fleet'],
      ['/settings', 'page-Settings'],
    ];
    for (const [path, testId] of cases) {
      window.history.pushState({}, '', path);
      const { unmount } = render(<App />);
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

  it('renders the sidebar nav with the 6 sections', async () => {
    await renderAt('/channels');
    await waitFor(() => {
      expect(screen.getByTestId('page-Channels')).toBeInTheDocument();
    });
    const nav = screen.getByRole('navigation', { name: /primary/i });
    for (const label of ['Channels', 'DMs', 'Issues', 'Tasks', 'Agents', 'Settings']) {
      expect(nav).toHaveTextContent(label);
    }
  });
});
