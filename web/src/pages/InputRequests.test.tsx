import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import InputRequests from './InputRequests';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const sampleList = [
  {
    id: 'IR-pending',
    status: 'pending',
    execution_id: 'E-1',
    question: 'go?',
    urgency: 'normal',
    created_at: '2026-05-24T01:00:00Z',
  },
  {
    id: 'IR-done',
    status: 'responded',
    execution_id: 'E-2',
    question: 'old?',
    urgency: 'normal',
    created_at: '2026-05-24T00:00:00Z',
    answer: 'yes',
    decided_by: 'user:hayang',
    decided_at: '2026-05-24T00:30:00Z',
  },
];

describe('InputRequests page', () => {
  beforeEach(() => {
    server.use(http.get('/api/input_requests', () => HttpResponse.json(sampleList)));
  });
  afterEach(() => cleanup());

  it('defaults to the Pending tab and shows only pending rows', async () => {
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getAllByTestId('ir-row')).toHaveLength(1));
    expect(screen.getByText('go?')).toBeInTheDocument();
    expect(screen.queryByText('old?')).not.toBeInTheDocument();
  });

  it('Responded tab narrows to responded rows', async () => {
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getAllByTestId('ir-row')).toHaveLength(1));
    fireEvent.click(screen.getByRole('tab', { name: /^responded$/i }));
    await waitFor(() => expect(screen.getByText('old?')).toBeInTheDocument());
    expect(screen.getByTestId('ir-answer-preview')).toHaveTextContent('yes');
  });

  it('empty state when no rows match', async () => {
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getAllByTestId('ir-row')).toHaveLength(1));
    fireEvent.click(screen.getByRole('tab', { name: /^cancelled$/i }));
    await waitFor(() => expect(screen.getByTestId('ir-empty')).toBeInTheDocument());
  });

  it('Respond opens the modal and submits', async () => {
    server.use(
      http.post('/api/input_requests/:id/respond', () => HttpResponse.json({ event_id: 'E-r' })),
    );
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getByTestId('ir-respond-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('ir-respond-button'));
    expect(screen.getByTestId('ir-respond-modal')).toBeInTheDocument();
    await userEvent.type(screen.getByTestId('ir-answer-textarea'), 'sure');
    fireEvent.click(screen.getByTestId('ir-respond-submit'));
    await waitFor(() => expect(screen.queryByTestId('ir-respond-modal')).not.toBeInTheDocument());
  });

  it('Cancel triggers native confirm + POST when accepted', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    let cancelled: string | undefined;
    server.use(
      http.post('/api/input_requests/:id/cancel', ({ params }) => {
        cancelled = params.id as string;
        return HttpResponse.json({ cancelled: true });
      }),
    );
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getByTestId('ir-cancel-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('ir-cancel-button'));
    await waitFor(() => expect(cancelled).toBe('IR-pending'));
    confirmSpy.mockRestore();
  });

  it('Cancel does nothing when confirm is dismissed', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    const cancelHit = vi.fn();
    server.use(
      http.post('/api/input_requests/:id/cancel', () => {
        cancelHit();
        return HttpResponse.json({ cancelled: true });
      }),
    );
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getByTestId('ir-cancel-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('ir-cancel-button'));
    expect(cancelHit).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/input_requests', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<InputRequests />);
    await waitFor(() => expect(screen.getByTestId('ir-error')).toHaveTextContent(/db down/));
  });
});
