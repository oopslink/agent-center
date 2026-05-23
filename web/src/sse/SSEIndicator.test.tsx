import { render, screen, cleanup } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { useAppStore } from '@/store/app';
import { SSEIndicator } from './SSEIndicator';

describe('SSEIndicator', () => {
  beforeEach(() => {
    useAppStore.setState({
      currentUserId: 'user:test',
      sseStatus: 'idle',
      sseLastEventId: null,
      navBadges: { inputRequests: 0 },
    });
  });
  afterEach(() => cleanup());

  it('reflects each status with the matching label', () => {
    const cases = [
      ['idle', 'idle'],
      ['connecting', 'connecting'],
      ['open', 'live'],
      ['reconnecting', 'reconnecting'],
      ['closed', 'offline'],
    ] as const;
    for (const [status, label] of cases) {
      useAppStore.setState({ sseStatus: status });
      const { unmount } = render(<SSEIndicator />);
      const node = screen.getByTestId('sse-indicator');
      expect(node).toHaveAttribute('data-status', status);
      expect(node).toHaveTextContent(label);
      unmount();
    }
  });
});
