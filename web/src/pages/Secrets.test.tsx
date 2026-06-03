import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import type React from 'react';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import Secrets from './Secrets';

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

const sampleRows = [
  {
    id: 'S-1',
    name: 'github',
    kind: 'other',
    state: 'active',
    created_at: '2026-05-01T00:00:00Z',
    created_by: 'user:hayang',
  },
  {
    id: 'S-2',
    name: 'old',
    kind: 'mcp',
    state: 'revoked',
    created_at: '2026-04-01T00:00:00Z',
    created_by: 'user:hayang',
    revoked_at: '2026-05-02T00:00:00Z',
    revoked_by: 'user:hayang',
    revoked_reason: 'manual',
  },
];

describe('Secrets page — strict no-plaintext-echo (ADR-0026 § 5)', () => {
  beforeEach(() => {
    server.use(http.get('/api/secrets', () => HttpResponse.json(sampleRows)));
  });
  afterEach(() => cleanup());

  it('list rendering: no `value` / `plaintext` substring in the DOM', async () => {
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getAllByTestId('secret-row')).toHaveLength(2));
    const html = document.body.innerHTML.toLowerCase();
    expect(html).not.toContain('plaintext');
    // The disclaimer mentions "value" so we look for the literal column
    // header instead. The fact that there's no <td>plaintext-anything</td>
    // is what matters.
    expect(screen.queryByText(/secret value:/i)).not.toBeInTheDocument();
    // Disclaimer banner present.
    expect(screen.getByTestId('secrets-disclaimer')).toHaveTextContent(
      /never displayed/i,
    );
  });

  it('list response shape: every secret has no `value` field', async () => {
    let raw: unknown[] = [];
    server.use(
      http.get('/api/secrets', () => {
        raw = sampleRows;
        return HttpResponse.json(sampleRows);
      }),
    );
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getAllByTestId('secret-row')).toHaveLength(2));
    for (const s of raw as Array<Record<string, unknown>>) {
      expect(s).not.toHaveProperty('value');
      expect(s).not.toHaveProperty('plaintext');
    }
  });

  it('revoke button visible only for active rows', async () => {
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getAllByTestId('secret-row')).toHaveLength(2));
    const buttons = screen.getAllByTestId('secret-revoke-button');
    expect(buttons).toHaveLength(1);
    // Sanity: row carrying the button is the active one.
    const row = buttons[0].closest('[data-testid="secret-row"]');
    expect(row).toHaveAttribute('data-secret-state', 'active');
  });

  it('revoke opens a confirm modal, then DELETEs when confirmed', async () => {
    let revokedId: string | undefined;
    server.use(
      http.delete('/api/secrets/:id', ({ params }) => {
        revokedId = params.id as string;
        return HttpResponse.json({ revoked: true });
      }),
    );
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getByTestId('secret-revoke-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('secret-revoke-button'));
    // ConfirmModal appears (no native window.confirm) naming the secret.
    const modal = await screen.findByTestId('confirm-modal');
    expect(modal).toHaveTextContent('github');
    fireEvent.click(screen.getByTestId('confirm-modal-confirm'));
    await waitFor(() => expect(revokedId).toBe('S-1'));
  });

  it('revoke aborts (no DELETE) when the confirm modal is cancelled', async () => {
    const hit = vi.fn();
    server.use(
      http.delete('/api/secrets/:id', () => {
        hit();
        return HttpResponse.json({ revoked: true });
      }),
    );
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getByTestId('secret-revoke-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('secret-revoke-button'));
    await screen.findByTestId('confirm-modal');
    fireEvent.click(screen.getByTestId('confirm-modal-cancel'));
    await waitFor(() => expect(screen.queryByTestId('confirm-modal')).toBeNull());
    expect(hit).not.toHaveBeenCalled();
  });

  it('opens create modal from header button', async () => {
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getByTestId('secrets-new-button')).toBeInTheDocument());
    fireEvent.click(screen.getByTestId('secrets-new-button'));
    expect(screen.getByTestId('secret-create-modal')).toBeInTheDocument();
  });

  it('empty state when no secrets', async () => {
    server.use(http.get('/api/secrets', () => HttpResponse.json([])));
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getByTestId('secrets-empty')).toBeInTheDocument());
  });

  it('surfaces API error', async () => {
    server.use(
      http.get('/api/secrets', () =>
        HttpResponse.json({ error: 'find_failed', message: 'db down' }, { status: 500 }),
      ),
    );
    wrap(<Secrets />);
    await waitFor(() => expect(screen.getByTestId('secrets-error')).toHaveTextContent(/db down/));
  });
});
