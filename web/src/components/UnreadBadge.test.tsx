import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { UnreadBadge } from './UnreadBadge';

// v2.8 #264 P1 / #176 contract: the badge is now PROP-DRIVEN from the
// Conversation row's embedded counts (unread_count / mention_count), not an
// N-times standalone /unread fetch. Render rules (#176 §5):
//   - 0 unread + 0 mention → nothing (no element).
//   - mention > 0 → red badge with the PRECISE mention number (99+ cap).
//   - unread > 0, no mention → neutral dot (no number, bold row elsewhere).
//   - not color-only: SR aria-label "(N unread, M mention(s))".
describe('UnreadBadge (prop-driven, #176)', () => {
  afterEach(() => cleanup());

  it('renders nothing when caught up (0 unread, 0 mention)', () => {
    render(<UnreadBadge unreadCount={0} mentionCount={0} />);
    expect(screen.queryByTestId('conversation-mention-badge')).toBeNull();
    expect(screen.queryByTestId('conversation-unread-dot')).toBeNull();
  });

  it('treats undefined counts as 0 (legacy payload) → nothing', () => {
    render(<UnreadBadge />);
    expect(screen.queryByTestId('conversation-mention-badge')).toBeNull();
    expect(screen.queryByTestId('conversation-unread-dot')).toBeNull();
  });

  it('unread-only (no mention) → neutral dot, no number, SR says N unread', () => {
    render(<UnreadBadge unreadCount={5} mentionCount={0} />);
    const dot = screen.getByTestId('conversation-unread-dot');
    expect(dot).toHaveAttribute('data-unread-count', '5');
    // a dot, not a number
    expect(dot).not.toHaveTextContent('5');
    expect(dot).toHaveAttribute('aria-label', '5 unread');
    expect(screen.queryByTestId('conversation-mention-badge')).toBeNull();
  });

  it('mention → red badge with precise mention number; SR announces both', () => {
    render(<UnreadBadge unreadCount={8} mentionCount={3} />);
    const badge = screen.getByTestId('conversation-mention-badge');
    expect(badge).toHaveTextContent('3');
    expect(badge).toHaveAttribute('data-mention-count', '3');
    expect(badge).toHaveAttribute('aria-label', '8 unread, 3 mentions');
    // mention supersedes the plain unread dot
    expect(screen.queryByTestId('conversation-unread-dot')).toBeNull();
  });

  it('uses singular "mention" in the SR label when mention_count === 1', () => {
    render(<UnreadBadge unreadCount={1} mentionCount={1} />);
    expect(screen.getByTestId('conversation-mention-badge')).toHaveAttribute(
      'aria-label',
      '1 unread, 1 mention',
    );
  });

  it('caps mention overflow at 99+', () => {
    render(<UnreadBadge unreadCount={999} mentionCount={150} />);
    expect(screen.getByTestId('conversation-mention-badge')).toHaveTextContent('99+');
  });
});
