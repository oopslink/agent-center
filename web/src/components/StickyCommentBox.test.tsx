import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { StickyCommentBox } from './StickyCommentBox';

// Isolate StickyCommentBox's own responsibility (sticky wrapper + height
// reporting) from MessageComposer's internals (send mutation / query client).
vi.mock('./MessageComposer', () => ({
  MessageComposer: ({ conversationId }: { conversationId: string }) => (
    <div data-testid="mock-composer">{conversationId}</div>
  ),
}));

// jsdom has no ResizeObserver — capture the callback so a test can fire it.
let roCallback: ResizeObserverCallback | null = null;
beforeEach(() => {
  roCallback = null;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(cb: ResizeObserverCallback) {
        roCallback = cb;
      }
      observe() {}
      disconnect() {}
    },
  );
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('StickyCommentBox', () => {
  it('renders the shared MessageComposer for the conversation', () => {
    render(<StickyCommentBox conversationId="conv-1" />);
    expect(screen.getByTestId('mock-composer')).toHaveTextContent('conv-1');
  });

  it('is sticky-positioned at the bottom (so it does not scroll away)', () => {
    render(<StickyCommentBox conversationId="conv-1" />);
    const box = screen.getByTestId('sticky-comment-box');
    expect(box.className).toContain('sticky');
    expect(box.className).toContain('bottom-0');
  });

  it('reports its height on mount so the scroll area can pad-bottom (no occlusion)', () => {
    const onHeightChange = vi.fn();
    render(<StickyCommentBox conversationId="conv-1" onHeightChange={onHeightChange} />);
    // initial measure fires synchronously in the mount effect
    expect(onHeightChange).toHaveBeenCalled();
  });

  it('reports the new height when the box resizes (e.g. textarea grows)', () => {
    const onHeightChange = vi.fn();
    render(<StickyCommentBox conversationId="conv-1" onHeightChange={onHeightChange} />);
    onHeightChange.mockClear();
    // simulate a resize — the observed callback drives a re-report
    expect(roCallback).toBeTypeOf('function');
    roCallback!([], {} as ResizeObserver);
    expect(onHeightChange).toHaveBeenCalled();
  });
});
