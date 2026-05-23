import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Issues from './Issues';

beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
});

function wrap(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

const seedHandler = http.get('/api/conversations', () =>
  HttpResponse.json([
    { id: 'I-1', kind: 'issue', name: 'login bug', status: 'active', description: '' },
    { id: 'I-2', kind: 'issue', name: 'old issue', status: 'archived', description: '' },
  ]),
);

describe('Issues page', () => {
  afterEach(() => cleanup());

  it('renders all issues by default', async () => {
    server.use(seedHandler);
    wrap(<Issues />);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
  });

  it('filter tab narrows to a single status', async () => {
    server.use(seedHandler);
    wrap(<Issues />);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /^archived$/i }));
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(1));
  });

  it('empty state shows when filter has no matches', async () => {
    server.use(seedHandler);
    wrap(<Issues />);
    await waitFor(() => expect(screen.getAllByTestId('issue-row')).toHaveLength(2));
    fireEvent.click(screen.getByRole('tab', { name: /^closed$/i }));
    await waitFor(() => expect(screen.getByTestId('issues-empty')).toBeInTheDocument());
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/conversations', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Issues />);
    await waitFor(() => expect(screen.getByTestId('issues-error')).toHaveTextContent(/db down/));
  });
});
