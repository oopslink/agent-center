import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { DMStartModal } from './DMStartModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('DMStartModal', () => {
  beforeEach(() => {
    server.use(
      http.post('/api/conversations', async ({ request }) => {
        const body = (await request.json()) as { kind: string; members?: string[] };
        if (body.kind !== 'dm') {
          return HttpResponse.json({ error: 'invalid_input', message: 'wrong kind' }, { status: 400 });
        }
        if (!body.members || body.members.length === 0) {
          return HttpResponse.json(
            { error: 'invalid_input', message: 'members required' },
            { status: 400 },
          );
        }
        return HttpResponse.json(
          { conversation_id: 'C-DM-NEW', event_id: 'E-1', kind: 'dm' },
          { status: 201 },
        );
      }),
      http.get('/api/agents', () =>
        HttpResponse.json({
          agents: [
            {
              id: 'bot-1',
              organization_id: 'O-1',
              name: 'bot-1',
              description: '',
              model: 'claude-opus',
              cli: 'claudecode',
              env_vars: {},
              skills: [],
              worker_id: 'w-1',
              lifecycle: 'stopped',
              availability: 'available',
              created_by: 'user:hayang',
              version: 1,
              created_at: '2026-05-24T01:00:00Z',
              updated_at: '2026-05-24T02:00:00Z',
            },
          ],
        }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('renders nothing when open=false', () => {
    wrap(<DMStartModal open={false} onClose={() => undefined} />);
    expect(screen.queryByTestId('dm-start-modal')).not.toBeInTheDocument();
  });

  it('start button stays disabled until a peer is entered', () => {
    wrap(<DMStartModal open onClose={() => undefined} />);
    expect(screen.getByTestId('dm-start-submit')).toBeDisabled();
  });

  it('submits with parsed peer list + calls onCreated with new id', async () => {
    const onClose = vi.fn();
    const onCreated = vi.fn();
    wrap(<DMStartModal open onClose={onClose} onCreated={onCreated} />);
    await userEvent.type(
      screen.getByTestId('dm-peers-input'),
      'agent:bot-1{enter}user:alice',
    );
    fireEvent.click(screen.getByTestId('dm-start-submit'));
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('C-DM-NEW'));
    expect(onClose).toHaveBeenCalled();
  });

  it('agent chip adds the identity to the peer list (dedup)', async () => {
    wrap(<DMStartModal open onClose={() => undefined} />);
    await waitFor(() => expect(screen.getAllByTestId('dm-agent-chip')).toHaveLength(1));
    fireEvent.click(screen.getByTestId('dm-agent-chip'));
    fireEvent.click(screen.getByTestId('dm-agent-chip')); // dedup
    const ta = screen.getByTestId('dm-peers-input') as HTMLTextAreaElement;
    expect(ta.value).toBe('agent:bot-1');
  });

  it('cancel button closes without submitting', () => {
    const onClose = vi.fn();
    wrap(<DMStartModal open onClose={onClose} />);
    fireEvent.click(screen.getByTestId('dm-start-cancel'));
    expect(onClose).toHaveBeenCalled();
  });

  it('shows server error inline when create fails', async () => {
    server.use(
      http.post('/api/conversations', () =>
        HttpResponse.json(
          { error: 'invalid_input', message: 'bad member id' },
          { status: 400 },
        ),
      ),
    );
    const onClose = vi.fn();
    wrap(<DMStartModal open onClose={onClose} />);
    await userEvent.type(screen.getByTestId('dm-peers-input'), 'no-prefix');
    fireEvent.click(screen.getByTestId('dm-start-submit'));
    await waitFor(() => expect(screen.getByTestId('dm-start-error')).toHaveTextContent(/bad member id/));
    expect(onClose).not.toHaveBeenCalled();
  });
});
