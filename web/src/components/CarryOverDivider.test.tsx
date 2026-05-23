import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { CarryOverDivider } from './CarryOverDivider';
import type {
  ConversationMessageReference,
  Message,
} from '@/api/types';

const msg = (id: string, convId: string, content: string): Message => ({
  id,
  conversation_id: convId,
  sender_identity_id: 'user:hayang',
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
  afterEach(() => cleanup());

  it('renders nothing when refs is empty', () => {
    const { container } = render(<CarryOverDivider refs={[]} messages={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when refs point at messages not in the messages list', () => {
    const { container } = render(
      <CarryOverDivider refs={[ref('r1', 'C-SRC', 'M-MISSING')]} messages={[msg('M-OTHER', 'C-SRC', 'noise')]} />,
    );
    expect(container.firstChild).toBeNull();
  });

  it('groups referenced messages by source conversation + adds divider', () => {
    render(
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
    render(
      <CarryOverDivider
        refs={[ref('r1', 'C-SRC', 'M1'), ref('r2', 'C-SRC', 'M2')]}
        messages={[msg('M1', 'C-SRC', 'one'), msg('M2', 'C-SRC', 'two')]}
      />,
    );
    expect(screen.getAllByTestId('carry-over-block')).toHaveLength(1);
    expect(screen.getAllByTestId('carry-over-message')).toHaveLength(2);
  });
});
