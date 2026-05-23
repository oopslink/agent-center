import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type React from 'react';
import { server } from '@/test/mswServer';
import { RespondInputRequestModal } from './RespondInputRequestModal';
import type { InputRequest } from '@/api/types';

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const ir: InputRequest = {
  id: 'IR-1',
  status: 'pending',
  execution_id: 'E-1',
  question: 'proceed?',
  urgency: 'normal',
  created_at: '2026-05-24T01:00:00Z',
  options: ['yes', 'no'],
};

describe('RespondInputRequestModal', () => {
  beforeEach(() => {
    server.use(
      http.post('/api/input_requests/:id/respond', () =>
        HttpResponse.json({ event_id: 'E-r' }),
      ),
    );
  });
  afterEach(() => cleanup());

  it('renders nothing when closed', () => {
    wrap(<RespondInputRequestModal open={false} ir={ir} onClose={() => undefined} />);
    expect(screen.queryByTestId('ir-respond-modal')).not.toBeInTheDocument();
  });

  it('renders nothing when ir is null', () => {
    wrap(<RespondInputRequestModal open ir={null} onClose={() => undefined} />);
    expect(screen.queryByTestId('ir-respond-modal')).not.toBeInTheDocument();
  });

  it('shows option chips that pre-fill the textarea', () => {
    wrap(<RespondInputRequestModal open ir={ir} onClose={() => undefined} />);
    fireEvent.click(screen.getAllByTestId('ir-option-chip')[0]);
    expect((screen.getByTestId('ir-answer-textarea') as HTMLTextAreaElement).value).toBe('yes');
  });

  it('renders adopt-suggestion button only when suggested_response present', () => {
    wrap(<RespondInputRequestModal open ir={ir} onClose={() => undefined} />);
    expect(screen.queryByTestId('ir-adopt-suggestion')).not.toBeInTheDocument();
    cleanup();
    const withHint = { ...ir, suggested_response: 'approve with caveat' } as InputRequest;
    wrap(<RespondInputRequestModal open ir={withHint} onClose={() => undefined} />);
    fireEvent.click(screen.getByTestId('ir-adopt-suggestion'));
    expect((screen.getByTestId('ir-answer-textarea') as HTMLTextAreaElement).value).toBe(
      'approve with caveat',
    );
  });

  it('submits + closes on success', async () => {
    const onClose = vi.fn();
    wrap(<RespondInputRequestModal open ir={ir} onClose={onClose} />);
    await userEvent.type(screen.getByTestId('ir-answer-textarea'), 'sure');
    fireEvent.click(screen.getByTestId('ir-respond-submit'));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it('shows server error inline + keeps modal open', async () => {
    server.use(
      http.post('/api/input_requests/:id/respond', () =>
        HttpResponse.json({ error: 'invalid_input', message: 'empty' }, { status: 400 }),
      ),
    );
    const onClose = vi.fn();
    wrap(<RespondInputRequestModal open ir={ir} onClose={onClose} />);
    await userEvent.type(screen.getByTestId('ir-answer-textarea'), 'x');
    fireEvent.click(screen.getByTestId('ir-respond-submit'));
    await waitFor(() => expect(screen.getByTestId('ir-respond-error')).toHaveTextContent(/empty/));
    expect(onClose).not.toHaveBeenCalled();
  });
});
