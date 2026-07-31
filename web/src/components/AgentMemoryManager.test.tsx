import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { AgentMemoryManager } from './AgentMemoryManager';

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AgentMemoryManager agentId="A1" />
    </QueryClientProvider>,
  );
}

afterEach(() => cleanup());

describe('AgentMemoryManager', () => {
  it('renders memory browser and sends an optimization request to the agent DM', async () => {
    let created: Record<string, unknown> | null = null;
    let sentContent = '';
    server.use(
      http.get('/api/agents/:id/runtime/list', () =>
        HttpResponse.json({
          path: 'memory',
          type: 'directory',
          truncated: false,
          entries: [{ name: 'MEMORY.md', path: 'memory/MEMORY.md', type: 'file', size: 42, mtime: 'x' }],
        }),
      ),
      http.get('/api/agents/:id/runtime/gitlog', () =>
        HttpResponse.json({
          commits: [{ sha: 'abc123456', message: 'memory: sync working tree', author: 'pd', date: 'x' }],
          truncated: false,
        }),
      ),
      http.post('/api/conversations', async ({ request }) => {
        created = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ conversation_id: 'C1', event_id: 'E1', kind: 'dm' }, { status: 201 });
      }),
      http.post('/api/conversations/:id/messages', async ({ params, request }) => {
        expect(params.id).toBe('C1');
        const body = (await request.json()) as { content?: string };
        sentContent = body.content ?? '';
        return HttpResponse.json({ message_id: 'M1', event_id: 'E2' }, { status: 201 });
      }),
    );

    wrap();
    expect(await screen.findByTestId('agent-tabpanel-memory')).toBeInTheDocument();
    expect(await screen.findByTestId('runtime-gitlog-list')).toHaveTextContent('memory: sync working tree');

    fireEvent.change(screen.getByTestId('agent-memory-optimize-direction'), {
      target: { value: '重点保留 agent-center production deploy 经验。' },
    });
    fireEvent.click(screen.getByTestId('agent-memory-optimize-submit'));

    await waitFor(() => expect(sentContent).not.toBe(''));
    expect(created).toMatchObject({ kind: 'dm', members: ['agent:A1'] });
    expect(sentContent).toContain('重点保留 agent-center production deploy 经验');
    expect(sentContent).toContain('MEMORY.md');
    expect(screen.getByTestId('agent-memory-optimize-sent')).toBeInTheDocument();
  });
});
