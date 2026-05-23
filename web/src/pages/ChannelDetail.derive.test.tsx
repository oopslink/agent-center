import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import ChannelDetail from './ChannelDetail';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/channels/:name" element={<ChannelDetail />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

const seedHandlers = [
  http.get('/api/conversations', () =>
    HttpResponse.json([
      { id: 'C-alpha', kind: 'channel', name: 'alpha', status: 'active', description: '' },
    ]),
  ),
  http.get('/api/conversations/:id', () =>
    HttpResponse.json({
      id: 'C-alpha',
      kind: 'channel',
      name: 'alpha',
      status: 'active',
      participants: [
        {
          identity_id: 'user:hayang',
          role: 'owner',
          joined_at: '2026-05-24T00:00:00Z',
          joined_by: 'user:hayang',
        },
      ],
    }),
  ),
  http.get('/api/conversations/:id/messages', () =>
    HttpResponse.json([
      {
        id: 'M1',
        conversation_id: 'C-alpha',
        sender_identity_id: 'user:hayang',
        content_kind: 'text',
        content: 'first',
        direction: 'inbound',
        posted_at: '2026-05-24T01:00:00Z',
      },
      {
        id: 'M2',
        conversation_id: 'C-alpha',
        sender_identity_id: 'user:hayang',
        content_kind: 'text',
        content: 'second',
        direction: 'inbound',
        posted_at: '2026-05-24T01:01:00Z',
      },
    ]),
  ),
];

describe('ChannelDetail — derive flow integration', () => {
  afterEach(() => cleanup());

  it('toggles select mode, selects messages, derive bar appears, derive issue end-to-end', async () => {
    server.use(
      ...seedHandlers,
      http.post('/api/issues', () =>
        HttpResponse.json(
          {
            issue_id: 'IS-NEW',
            conversation_id: 'I-NEW',
            reference_count: 2,
            issue_event_id: 'E-i',
            carry_over_event_id: 'E-c',
          },
          { status: 201 },
        ),
      ),
    );
    wrap('/channels/alpha');
    // Wait for messages to render then enter select mode.
    await waitFor(() => expect(screen.getByText('first')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('select-mode-toggle'));
    const checks = screen.getAllByTestId('message-select') as HTMLInputElement[];
    expect(checks).toHaveLength(2);
    // No bar yet — selection is empty.
    expect(screen.queryByTestId('derive-bar')).not.toBeInTheDocument();
    fireEvent.click(checks[0]);
    fireEvent.click(checks[1]);
    expect(screen.getByTestId('derive-bar-count')).toHaveTextContent('2 messages selected');
    // Open Issue modal + submit.
    fireEvent.click(screen.getByTestId('derive-open-issue'));
    await userEvent.type(screen.getByTestId('derive-title-input'), 'continuing discussion');
    fireEvent.click(screen.getByTestId('derive-modal-submit'));
    await waitFor(() => expect(screen.getByTestId('derive-success-link')).toHaveAttribute('href', '/issues/I-NEW'));
  });

  it('Cancel from derive bar exits select mode + hides the bar', async () => {
    server.use(...seedHandlers);
    wrap('/channels/alpha');
    await waitFor(() => expect(screen.getByText('first')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('select-mode-toggle'));
    const checks = screen.getAllByTestId('message-select') as HTMLInputElement[];
    fireEvent.click(checks[0]);
    expect(screen.getByTestId('derive-bar')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('derive-cancel'));
    expect(screen.queryByTestId('derive-bar')).not.toBeInTheDocument();
    expect(screen.queryByTestId('message-select')).not.toBeInTheDocument();
  });
});
