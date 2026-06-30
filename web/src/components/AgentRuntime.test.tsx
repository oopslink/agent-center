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

  it('renders an image file inline (base64 data URL)', async () => {
    server.use(
      http.get('/api/agents/:id/runtime/list', ({ request }) => {
        const path = new URL(request.url).searchParams.get('path') ?? '';
        if (path === '') {
          return HttpResponse.json({ path: '', type: 'directory', entries: rootEntries, truncated: false });
        }
        if (path === 'workspace') {
          return HttpResponse.json({
            path, type: 'directory', truncated: false,
            entries: [{ name: 'pic.png', path: 'workspace/pic.png', type: 'file', size: 81000, mtime: 'x' }],
          });
        }
        return HttpResponse.json({ path, type: 'directory', entries: [], truncated: false });
      }),
      http.get('/api/agents/:id/runtime/read', ({ request }) => {
        expect(new URL(request.url).searchParams.get('path')).toBe('workspace/pic.png');
        return HttpResponse.json({
          type: 'file', size: 81000, mtime: 'x', content_type: 'image/png',
          binary: false, image: true, encoding: 'base64', truncated: false, content: 'iVBORw0KGgo=',
        });
      }),
    );
    wrap();
    fireEvent.click(await within(await screen.findByTestId('runtime-tree')).findByText('workspace/'));
    fireEvent.click(await screen.findByText('pic.png'));
    const img = await screen.findByTestId('runtime-file-image-img');
    expect(img.getAttribute('src')).toBe('data:image/png;base64,iVBORw0KGgo=');
  });

  it('keeps the git log reachable from a memory file via the History tab + shows a commit diff', async () => {
    server.use(
      http.get('/api/agents/:id/runtime/list', ({ request }) => {
        const path = new URL(request.url).searchParams.get('path') ?? '';
        if (path === '') {
          return HttpResponse.json({ path: '', type: 'directory', entries: rootEntries, truncated: false });
        }
        if (path === 'memory') {
          return HttpResponse.json({
            path, type: 'directory', truncated: false,
            entries: [{ name: 'CLAUDE.md', path: 'memory/CLAUDE.md', type: 'file', size: 254, mtime: 'x' }],
          });
        }
        return HttpResponse.json({ path, type: 'directory', entries: [], truncated: false });
      }),
      http.get('/api/agents/:id/runtime/read', () =>
        HttpResponse.json({
          type: 'file', size: 254, mtime: 'x', content_type: 'text/markdown',
          binary: false, truncated: false, content: '# agent-center Memory',
        }),
      ),
      http.get('/api/agents/:id/runtime/gitlog', () =>
        HttpResponse.json({ commits: [{ sha: 'a1b2c3d4e5', message: 'sync working tree', author: 'pd', date: 'x' }], truncated: false }),
      ),
      http.get('/api/agents/:id/runtime/gitdiff', ({ request }) => {
        expect(new URL(request.url).searchParams.get('ref')).toBe('a1b2c3d4e5');
        return HttpResponse.json({ sha: 'a1b2c3d4e5', diff: 'diff --git a/CLAUDE.md\n+added line\n-removed line', truncated: false });
      }),
    );
    wrap();
    // Select a file under memory → Content tab shows the file.
    fireEvent.click(await within(await screen.findByTestId('runtime-tree')).findByText('memory/'));
    fireEvent.click(await screen.findByText('CLAUDE.md'));
    expect(await screen.findByTestId('runtime-file-content')).toHaveTextContent('# agent-center Memory');
    // History tab is still reachable (the old UI replaced the log entirely).
    fireEvent.click(screen.getByTestId('runtime-memory-tab-history'));
    const list = await screen.findByTestId('runtime-gitlog-list');
    expect(list).toHaveTextContent('sync working tree');
    // Expanding a commit loads its diff.
    fireEvent.click(screen.getByTestId('runtime-gitlog-row-toggle'));
    const diff = await screen.findByTestId('runtime-gitdiff');
    expect(diff).toHaveTextContent('added line');
    expect(diff).toHaveTextContent('removed line');
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
