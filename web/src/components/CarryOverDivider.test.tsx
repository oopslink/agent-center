import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/mswServer';
import { renderWithQuery } from '@/test/renderWith';
import { CarryOverDivider } from './CarryOverDivider';
import type {
  ConversationMessageReference,
  Message,
} from '@/api/types';

const msg = (id: string, convId: string, content: string, sender = 'user:hayang'): Message => ({
  id,
  conversation_id: convId,
  sender_identity_id: sender,
  content_kind: 'text',
  content,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

const ref = (id: string, src: string, msgId: string): ConversationMessageReference => ({
  id,
  child_conversation_id: 'CHILD',
  source_conversation_id: src,
  source_message_id: msgId,
  created_by: 'user:hayang',
  created_at: '2026-05-24T01:00:00Z',
});

describe('CarryOverDivider', () => {
  // server lifecycle (listen/reset/close) is wired globally in test/setup.ts.
  afterEach(() => cleanup());

  it('renders nothing when refs is empty', () => {
    const { container } = renderWithQuery(<CarryOverDivider refs={[]} messages={[]} />);
    // The component renders null; the QueryClientProvider wrapper has no DOM.
    expect(container.querySelector('[data-testid="carry-over-section"]')).toBeNull();
  });

  it('renders nothing when refs point at messages not in the messages list', () => {
    const { container } = renderWithQuery(
      <CarryOverDivider refs={[ref('r1', 'C-SRC', 'M-MISSING')]} messages={[msg('M-OTHER', 'C-SRC', 'noise')]} />,
    );
    expect(container.querySelector('[data-testid="carry-over-section"]')).toBeNull();
  });

  it('groups referenced messages by source conversation + adds divider', () => {
    renderWithQuery(
      <CarryOverDivider
        refs={[ref('r1', 'C-SRC1', 'M1'), ref('r2', 'C-SRC2', 'M2')]}
        messages={[msg('M1', 'C-SRC1', 'from src1'), msg('M2', 'C-SRC2', 'from src2')]}
      />,
    );
    const blocks = screen.getAllByTestId('carry-over-block');
    expect(blocks).toHaveLength(2);
    expect(blocks[0]).toHaveAttribute('data-source-conversation-id', 'C-SRC1');
    expect(blocks[1]).toHaveAttribute('data-source-conversation-id', 'C-SRC2');
    expect(screen.getByText('from src1')).toBeInTheDocument();
    expect(screen.getByText('from src2')).toBeInTheDocument();
    expect(screen.getByTestId('carry-over-divider')).toHaveTextContent(/discussion below/i);
  });

  it('shows multiple messages from the same source under one block', () => {
    renderWithQuery(
      <CarryOverDivider
        refs={[ref('r1', 'C-SRC', 'M1'), ref('r2', 'C-SRC', 'M2')]}
        messages={[msg('M1', 'C-SRC', 'one'), msg('M2', 'C-SRC', 'two')]}
      />,
    );
    expect(screen.getAllByTestId('carry-over-block')).toHaveLength(1);
    expect(screen.getAllByTestId('carry-over-message')).toHaveLength(2);
  });

  it('renders the sender as the resolved display NAME, not the raw ref', async () => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          {
            id: 'mem-1', organization_id: 'org-test', identity_id: 'agent:builder',
            kind: 'agent', role: 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
            display_name: 'Builder Bot',
          },
        ]),
      ),
    );
    renderWithQuery(
      <CarryOverDivider
        refs={[ref('r1', 'C-SRC', 'M1')]}
        messages={[msg('M1', 'C-SRC', 'hello', 'agent:builder')]}
      />,
    );
    // Resolved → the display name, NEVER the raw "agent:builder" prefixed ref.
    await waitFor(() => expect(screen.getByText('Builder Bot')).toBeInTheDocument());
    expect(screen.queryByText('agent:builder')).not.toBeInTheDocument();
  });

  it('falls back to the CLEAN handle (no prefix) for an unresolved sender', () => {
    renderWithQuery(
      <CarryOverDivider
        refs={[ref('r1', 'C-SRC', 'M1')]}
        messages={[msg('M1', 'C-SRC', 'hello', 'agent:ghost-9999')]}
      />,
    );
    // Unresolved → clean tail handle, never the raw "agent:ghost-9999".
    expect(screen.getByText('ghost-9999')).toBeInTheDocument();
    expect(screen.queryByText('agent:ghost-9999')).not.toBeInTheDocument();
  });
});
