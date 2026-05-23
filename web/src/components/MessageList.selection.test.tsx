import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MessageList } from './MessageList';
import type { Message } from '@/api/types';

const sample = (id: string): Message => ({
  id,
  conversation_id: 'C1',
  sender_identity_id: 'user:hayang',
  content_kind: 'text',
  content: `body ${id}`,
  direction: 'inbound',
  posted_at: '2026-05-24T01:00:00Z',
});

describe('MessageList selection mode', () => {
  afterEach(() => cleanup());

  it('hides checkboxes by default', () => {
    render(<MessageList messages={[sample('M1')]} />);
    expect(screen.queryByTestId('message-select')).not.toBeInTheDocument();
  });

  it('renders a checkbox per row when selectable', () => {
    render(
      <MessageList
        messages={[sample('M1'), sample('M2')]}
        selectable
        isSelected={() => false}
        onToggle={() => undefined}
      />,
    );
    expect(screen.getAllByTestId('message-select')).toHaveLength(2);
  });

  it('reflects isSelected per row + calls onToggle on click', () => {
    const onToggle = vi.fn();
    const selected = new Set(['M2']);
    render(
      <MessageList
        messages={[sample('M1'), sample('M2')]}
        selectable
        isSelected={(id) => selected.has(id)}
        onToggle={onToggle}
      />,
    );
    const checks = screen.getAllByTestId('message-select') as HTMLInputElement[];
    expect(checks[0].checked).toBe(false);
    expect(checks[1].checked).toBe(true);
    fireEvent.click(checks[0]);
    expect(onToggle).toHaveBeenCalledWith('M1');
  });
});
