import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { ObjectAuditTimeline } from './ObjectAuditTimeline';
import type { AuditEntry } from '@/api/audit';

function renderWithProvider(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function entry(over: Partial<AuditEntry>): AuditEntry {
  return {
    id: 'a1',
    object_type: 'task',
    object_id: 'task-1',
    change_type: 'status_changed',
    field: 'status',
    from: 'open',
    to: 'running',
    actor: 'user:alice',
    detail: {},
    occurred_at: '2026-07-03T10:00:00Z',
    ...over,
  };
}

afterEach(() => cleanup());

describe('ObjectAuditTimeline', () => {
  it('renders the ledger newest-first with human-readable sentences + actor', async () => {
    server.use(
      http.get('/api/projects/:pid/tasks/:tid/audit', () =>
        HttpResponse.json({
          entries: [
            entry({ id: 'a2', change_type: 'status_changed', from: 'open', to: 'running', actor: 'user:alice' }),
            entry({ id: 'a1', change_type: 'assigned', field: 'assignee', from: '', to: 'agent:bot', actor: 'user:pd' }),
            entry({ id: 'a0', change_type: 'created', from: '', to: 'open', actor: 'user:pd', detail: {} }),
          ],
          next_cursor: '',
        }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);

    await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
    const rows = screen.getAllByTestId('audit-row');
    expect(rows).toHaveLength(3);
    // Newest-first: first row is the status change (open → running).
    expect(rows[0]).toHaveAttribute('data-change-type', 'status_changed');
    expect(rows[0]).toHaveTextContent(/open → running/);
    // Actor rendered without the ADR-0033 scheme prefix, as @handle.
    expect(rows[0]).toHaveTextContent('@alice');
    // The assigned row composes "assigned to <agent>".
    expect(rows[1]).toHaveTextContent(/assigned to bot/);
  });

  it('shows the empty state when the ledger has no entries', async () => {
    server.use(
      http.get('/api/projects/:pid/tasks/:tid/audit', () =>
        HttpResponse.json({ entries: [], next_cursor: '' }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="task" projectId="proj-a" objectId="task-1" />);
    await waitFor(() => expect(screen.getByTestId('audit-empty')).toBeInTheDocument());
  });

  it('renders plan dependency + node changes from structured detail', async () => {
    server.use(
      http.get('/api/projects/:pid/plans/:planId/audit', () =>
        HttpResponse.json({
          entries: [
            entry({
              id: 'p2',
              object_type: 'plan',
              change_type: 'dependency_added',
              from: '',
              to: '',
              actor: 'user:pd',
              detail: { from: 'T1', to: 'T2', kind: 'seq' },
            }),
            entry({
              id: 'p1',
              object_type: 'plan',
              change_type: 'started',
              actor: 'user:pd',
              detail: { status: 'running' },
            }),
          ],
          next_cursor: '',
        }),
      ),
    );
    renderWithProvider(<ObjectAuditTimeline objectType="plan" projectId="proj-a" objectId="plan-1" />);
    await waitFor(() => expect(screen.getByTestId('audit-list')).toBeInTheDocument());
    expect(screen.getByText(/added dependency T1 → T2/)).toBeInTheDocument();
    expect(screen.getByText(/started the plan/)).toBeInTheDocument();
  });
});
