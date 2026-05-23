import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { ChannelCreateModal } from './ChannelCreateModal';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('ChannelCreateModal', () => {
  beforeEach(() => {
    server.use(
      http.post('/api/conversations', async ({ request }) => {
        const body = (await request.json()) as { name?: string };
        if (!body.name) {
          return HttpResponse.json(
            { error: 'invalid_input', message: 'name required' },
            { status: 400 },
          );
        }
        return HttpResponse.json(
          { conversation_id: 'C-NEW', event_id: 'E-1', kind: 'channel' },
          { status: 201 },
        );
      }),
    );
  });
  afterEach(() => cleanup());

  it('renders nothing when open=false', () => {
    wrap(<ChannelCreateModal open={false} onClose={() => undefined} />);
    expect(screen.queryByTestId('channel-create-modal')).not.toBeInTheDocument();
  });

  it('submits + calls onCreated + closes the modal', async () => {
    const onClose = vi.fn();
    const onCreated = vi.fn();
    wrap(<ChannelCreateModal open onClose={onClose} onCreated={onCreated} />);
    await userEvent.type(screen.getByTestId('create-channel-name'), 'alpha');
    fireEvent.click(screen.getByTestId('create-channel-submit'));
    await waitFor(() => expect(onCreated).toHaveBeenCalledWith('alpha'));
    expect(onClose).toHaveBeenCalled();
  });

  it('Cancel button closes without submitting', () => {
    const onClose = vi.fn();
    wrap(<ChannelCreateModal open onClose={onClose} />);
    fireEvent.click(screen.getByTestId('create-channel-cancel'));
    expect(onClose).toHaveBeenCalled();
  });

  it('shows error when backend rejects', async () => {
    server.use(
      http.post('/api/conversations', () =>
        HttpResponse.json(
          { error: 'conflict', message: 'name taken' },
          { status: 409 },
        ),
      ),
    );
    const onClose = vi.fn();
    wrap(<ChannelCreateModal open onClose={onClose} />);
    await userEvent.type(screen.getByTestId('create-channel-name'), 'dup');
    fireEvent.click(screen.getByTestId('create-channel-submit'));
    await waitFor(() =>
      expect(screen.getByTestId('create-channel-error')).toHaveTextContent(/name taken/),
    );
    expect(onClose).not.toHaveBeenCalled();
  });
});
