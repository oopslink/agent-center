// T583 (issue-921db054 / I5) — AgentRuntime: read-only runtime browser. Covers the
// tree + collapse, text preview, redacted placeholder, binary/metadata-only,
// git-log, and the worker-offline "unavailable" degrade.
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentRuntime } from './AgentRuntime';

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentRuntime agentId="A1" />
    </QueryClientProvider>,
  );
}

// Root listing: a dir (memory, git), a dir (workspace), a text file, a sensitive
// config, and a lock file.
const rootEntries = [
  { name: 'memory', path: 'memory', type: 'directory', size: 0, mtime: 'x' },
  { name: 'workspace', path: 'workspace', type: 'directory', size: 0, mtime: 'x' },
  { name: 'events.jsonl', path: 'events.jsonl', type: 'file', size: 18000, mtime: '2026-06-29T10:05:00Z' },
  { name: 'mcp_config.runtime.json', path: 'mcp_config.runtime.json', type: 'file', size: 705, mtime: 'x', sensitive: true },
  { name: 'supervisor.lock', path: 'supervisor.lock', type: 'file', size: 12, mtime: 'x', sensitive: true },
];

function mockRoot(entries = rootEntries) {
  server.use(
    http.get('/api/agents/:id/runtime/list', ({ request }) => {
      const path = new URL(request.url).searchParams.get('path') ?? '';
      if (path === '') {
        return HttpResponse.json({ path: '', type: 'directory', entries, truncated: false });
      }
      // nested dirs → empty (tests that exercise children override this)
      return HttpResponse.json({ path, type: 'directory', entries: [], truncated: false });
    }),
  );
}

afterEach(() => cleanup());

describe('AgentRuntime (T583)', () => {
  it('renders the file tree and toggles the collapsible sidebar', async () => {
    mockRoot();
    wrap();
    await screen.findByTestId('runtime-tree');
    expect(screen.getAllByTestId('runtime-tree-row').length).toBe(rootEntries.length);
    // collapse → sidebar gone, expand affordance shown
    fireEvent.click(screen.getByTestId('runtime-sidebar-collapse'));
    expect(screen.queryByTestId('runtime-sidebar')).toBeNull();
    expect(screen.getByTestId('runtime-sidebar-expand')).toBeInTheDocument();
    // expand again
    fireEvent.click(screen.getByTestId('runtime-sidebar-expand'));
    expect(screen.getByTestId('runtime-sidebar')).toBeInTheDocument();
  });

  it('previews a text file (content + truncated/size meta)', async () => {
    mockRoot();
    server.use(
      http.get('/api/agents/:id/runtime/read', ({ request }) => {
        const path = new URL(request.url).searchParams.get('path');
        expect(path).toBe('events.jsonl');
        return HttpResponse.json({
          type: 'file', size: 18000, mtime: '2026-06-29T10:05:00Z', content_type: 'text/plain',
          binary: false, truncated: true, content: '{"type":"session_started"}',
        });
      }),
    );
    wrap();
    fireEvent.click(await within(await screen.findByTestId('runtime-tree')).findByText('events.jsonl'));
    const content = await screen.findByTestId('runtime-file-content');
    expect(content).toHaveTextContent('session_started');
  });

  it('shows the redacted placeholder for a sensitive file (no content)', async () => {
    mockRoot();
    server.use(
      http.get('/api/agents/:id/runtime/read', () =>
        HttpResponse.json({
          type: 'file', size: 705, mtime: 'x', content_type: 'application/json',
          binary: false, redacted: true, truncated: false, content: null,
        }),
      ),
    );
    wrap();
    fireEvent.click(await within(await screen.findByTestId('runtime-tree')).findByText('mcp_config.runtime.json'));
    expect(await screen.findByTestId('runtime-file-redacted')).toBeInTheDocument();
    expect(screen.queryByTestId('runtime-file-content')).toBeNull();
  });

  it('shows a metadata-only view for a binary/special (lock) file', async () => {
    mockRoot();
    server.use(
      http.get('/api/agents/:id/runtime/read', () =>
        HttpResponse.json({
          type: 'file', size: 12, mtime: 'x', content_type: 'application/octet-stream',
          binary: true, truncated: false, content: null,
        }),
      ),
    );
    wrap();
    fireEvent.click(await within(await screen.findByTestId('runtime-tree')).findByText('supervisor.lock'));
    expect(await screen.findByTestId('runtime-file-binary')).toBeInTheDocument();
  });

  it('opening the memory dir shows the git-log', async () => {
    mockRoot();
    server.use(
      http.get('/api/agents/:id/runtime/gitlog', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('memory');
        return HttpResponse.json({
          commits: [
            { sha: 'a1b2c3d4', message: 'record v2.18.4 shipped', author: 'pd', date: '2026-06-29T08:00:00Z' },
            { sha: '9f8e7d6c', message: 'cred-non-leak must cover error path', author: 'pd', date: '2026-06-29T05:00:00Z' },
          ],
          truncated: false,
        });
      }),
    );
    wrap();
    fireEvent.click(await within(await screen.findByTestId('runtime-tree')).findByText('memory/'));
    const list = await screen.findByTestId('runtime-gitlog-list');
    expect(within(list).getAllByRole('listitem')).toHaveLength(2);
    expect(list).toHaveTextContent('record v2.18.4 shipped');
    expect(list).toHaveTextContent('a1b2c3d'); // short sha
  });

  it('degrades to "Runtime unavailable" when the worker is offline', async () => {
    server.use(
      http.get('/api/agents/:id/runtime/list', () => HttpResponse.json({ unavailable: true, reason: 'worker offline' })),
    );
    wrap();
    const un = await screen.findByTestId('runtime-unavailable');
    expect(un).toHaveTextContent(/Runtime unavailable/i);
    expect(un).toHaveTextContent('worker offline');
    // no tree when the whole tab is unavailable
    expect(screen.queryByTestId('runtime-tree')).toBeNull();
  });
});
