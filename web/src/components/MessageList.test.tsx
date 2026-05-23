import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { MessageList } from './MessageList';
import type { Message } from '@/api/types';

const sample = (id: string, content: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

describe('MessageList', () => {
  afterEach(() => cleanup());

  it('renders empty state when no messages', () => {
    render(<MessageList messages={[]} />);
    expect(screen.getByTestId('message-list-empty')).toBeInTheDocument();
  });

  it('renders one row per message with sender + content', () => {
    render(<MessageList messages={[sample('M1', 'hi'), sample('M2', 'two')]} />);
    const rows = screen.getAllByTestId('message-row');
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('hi');
    expect(rows[1]).toHaveTextContent('two');
    expect(rows[0]).toHaveAttribute('data-message-id', 'M1');
  });
});
