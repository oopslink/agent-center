// v2.10.0 [T2 / T64] col④ — "Shared files": aggregates message attachments.
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { SharedFilesPanel } from './SharedFilesPanel';

function renderPanel(id = 'C1') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SharedFilesPanel conversationId={id} />
    </QueryClientProvider>,
  );
}

const msg = (id: string, attachments?: unknown[]) => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:u',
  content_kind: 'text',
  content: 'x',
  direction: 'inbound',
  posted_at: '2026-06-14T00:00:00Z',
  ...(attachments ? { attachments } : {}),
});

afterEach(() => cleanup());

describe('SharedFilesPanel (T64 col④)', () => {
  it('aggregates + dedupes attachments across messages, with download links + count', async () => {
    server.use(
      http.get('/api/conversations/:id/messages', () =>
        HttpResponse.json([
          msg('m1', [
            { uri: 'ac://files/F1', filename: 'spec.pdf', mime_type: 'application/pdf', size: 2048 },
            { uri: 'ac://files/F2', filename: 'img.png', mime_type: 'image/png', size: 1024 },
          ]),
          // m2 re-references F1 (same blob) → must be deduped.
          msg('m2', [{ uri: 'ac://files/F1', filename: 'spec.pdf', mime_type: 'application/pdf', size: 2048 }]),
        ]),
      ),
    );
    renderPanel();
    expect(await screen.findByTestId('shared-files-panel')).toBeInTheDocument();
    expect(screen.getByTestId('shared-files-count')).toHaveTextContent('2'); // F1 + F2, dedup
    const links = screen.getAllByTestId('shared-file-link');
    expect(links).toHaveLength(2);
    expect(screen.getByText('spec.pdf')).toBeInTheDocument();
    expect(links[0].getAttribute('href')).toContain('/files/F1');
  });

  it('renders nothing when the conversation has no attachments', async () => {
    server.use(
      http.get('/api/conversations/:id/messages', () => HttpResponse.json([msg('m1'), msg('m2')])),
    );
    renderPanel();
    // Even after the (attachment-free) messages settle, the panel stays absent.
    await waitFor(() => expect(screen.queryByTestId('shared-files-panel')).toBeNull());
    expect(screen.queryByTestId('shared-files-panel')).toBeNull();
  });
});
