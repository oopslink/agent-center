import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, fireEvent, act } from '@testing-library/react';
import { useModalA11y } from './useModalA11y';

// Test harness: minimal modal that uses the hook. Two buttons inside
// so the focus-trap path has something to cycle between.
function HarnessModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const ref = useModalA11y({ open, onClose });
  if (!open) return null;
  return (
    <div ref={ref} role="dialog" aria-modal="true" data-testid="modal">
      <button type="button" data-testid="first">
        first
      </button>
      <button type="button" data-testid="last">
        last
      </button>
    </div>
  );
}

describe('useModalA11y', () => {
  afterEach(() => cleanup());

  it('Escape calls onClose when open', () => {
    const onClose = vi.fn();
    render(<HarnessModal open onClose={onClose} />);
    act(() => {
      fireEvent.keyDown(document, { key: 'Escape' });
    });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('Escape no-op when closed', () => {
    const onClose = vi.fn();
    render(<HarnessModal open={false} onClose={onClose} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
  });

  it('Tab from last wraps to first', () => {
    const onClose = vi.fn();
    const { getByTestId } = render(<HarnessModal open onClose={onClose} />);
    const last = getByTestId('last');
    last.focus();
    act(() => {
      fireEvent.keyDown(document, { key: 'Tab' });
    });
    expect(document.activeElement).toBe(getByTestId('first'));
  });

  it('Shift+Tab from first wraps to last', () => {
    const onClose = vi.fn();
    const { getByTestId } = render(<HarnessModal open onClose={onClose} />);
    const first = getByTestId('first');
    first.focus();
    act(() => {
      fireEvent.keyDown(document, { key: 'Tab', shiftKey: true });
    });
    expect(document.activeElement).toBe(getByTestId('last'));
  });

  it('Tab with no focusables inside is no-op (defensive)', () => {
    function EmptyModal({ open }: { open: boolean }) {
      const ref = useModalA11y({ open, onClose: () => {} });
      if (!open) return null;
      return <div ref={ref} role="dialog" aria-modal="true" />;
    }
    render(<EmptyModal open />);
    // Should not throw.
    expect(() => {
      fireEvent.keyDown(document, { key: 'Tab' });
    }).not.toThrow();
  });

  it('non-Escape non-Tab keys ignored', () => {
    const onClose = vi.fn();
    render(<HarnessModal open onClose={onClose} />);
    fireEvent.keyDown(document, { key: 'a' });
    fireEvent.keyDown(document, { key: 'Enter' });
    expect(onClose).not.toHaveBeenCalled();
  });
});
