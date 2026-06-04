import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { DMStartModal } from './DMStartModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('DMStartModal (#215 single-select)', () => {
  beforeEach(() => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'm1', organization_id: 'O-1', identity_id: 'agent-bot1', kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Bot One' },
          { id: 'm2', organization_id: 'O-1', identity_id: 'user-alice', kind: 'user', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z', display_name: 'Alice' },
        ]),
      ),
    );
  });
  afterEach(() => cleanup());

  it('renders nothing when open=false', () => {
    wrap(<DMStartModal open={false} onClose={() => undefined} />);
    expect(screen.queryByTestId('dm-start-modal')).not.toBeInTheDocument();
  });

  it('start stays disabled until a peer is selected', async () => {
    wrap(<DMStartModal open onClose={() => undefined} />);
    expect(screen.getByTestId('dm-start-submit')).toBeDisabled();
    await waitFor(() => expect(screen.getAllByTestId('dm-peer-candidate').length).toBeGreaterThan(0));
    fireEvent.click(screen.getAllByTestId('dm-peer-candidate')[0]);
    expect(screen.getByTestId('dm-start-submit')).not.toBeDisabled();
  });

  it('single-selects a peer and submits members:[<kind>:<id>] + calls onCreated', async () => {
    let posted: { kind: string; members?: string[] } | undefined;
    server.use(
      http.post('/api/conversations', async ({ request }) => {
        posted = (await request.json()) as { kind: string; members?: string[] };
        return HttpResponse.json({ conversation_id: 'C-DM-NEW', event_id: 'E-1', kind: 'dm' }, { status: 201 });
      }),
    );
    const onClose = vi.fn();
    const onCreated = vi.fn();
    wrap(<DMStartModal open onClose={onClose} onCreated={onCreated} />);
    const alice = await screen.findByText('Alice');
    fireEvent.click(alice);
    await act(async () => {
      fireEvent.click(screen.getByTestId('dm-start-submit'));
    });
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('C-DM-NEW'));
    expect(posted).toMatchObject({ kind: 'dm', members: ['user:user-alice'] });
    expect(onClose).toHaveBeenCalled();
  });

  it('filters candidates by search', async () => {
    wrap(<DMStartModal open onClose={() => undefined} />);
    await waitFor(() => expect(screen.getAllByTestId('dm-peer-candidate')).toHaveLength(2));
    fireEvent.change(screen.getByTestId('dm-peer-search'), { target: { value: 'bot' } });
    expect(screen.getAllByTestId('dm-peer-candidate')).toHaveLength(1);
    expect(screen.getByText('Bot One')).toBeInTheDocument();
  });

  it('cancel closes without submitting', () => {
    const onClose = vi.fn();
    wrap(<DMStartModal open onClose={onClose} />);
    fireEvent.click(screen.getByTestId('dm-start-cancel'));
    expect(onClose).toHaveBeenCalled();
  });

  it('shows server error inline when create fails', async () => {
    server.use(
      http.post('/api/conversations', () =>
        HttpResponse.json({ error: 'invalid_input', message: 'bad member id' }, { status: 400 }),
      ),
    );
    const onClose = vi.fn();
    wrap(<DMStartModal open onClose={onClose} />);
    fireEvent.click(await screen.findByText('Alice'));
    await act(async () => {
      fireEvent.click(screen.getByTestId('dm-start-submit'));
    });
    await waitFor(() => expect(screen.getByTestId('dm-start-error')).toHaveTextContent(/bad member id/));
    expect(onClose).not.toHaveBeenCalled();
  });
});
