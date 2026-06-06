import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useState } from 'react';
import { useTablistKeyboard } from './useTablistKeyboard';

const KEYS = ['a', 'b', 'c'] as const;
type K = (typeof KEYS)[number];

function Harness() {
  const [active, setActive] = useState<K>('a');
  const tl = useTablistKeyboard({ keys: KEYS, active });
  return (
    <nav
      role="tablist"
      ref={tl.tablistRef}
      onKeyDown={tl.onKeyDown}
      onBlur={tl.onBlur}
      data-testid="tl"
    >
      {KEYS.map((k) => (
        <button
          key={k}
          type="button"
          role="tab"
          aria-selected={active === k}
          tabIndex={tl.tabIndexFor(k)}
          data-testid={`tab-${k}`}
          onClick={() => setActive(k)}
        >
          {k}
        </button>
      ))}
    </nav>
  );
}

describe('useTablistKeyboard (manual activation)', () => {
  it('roving tabindex: only the active tab is in the Tab order initially', () => {
    render(<Harness />);
    expect(screen.getByTestId('tab-a')).toHaveAttribute('tabindex', '0');
    expect(screen.getByTestId('tab-b')).toHaveAttribute('tabindex', '-1');
    expect(screen.getByTestId('tab-c')).toHaveAttribute('tabindex', '-1');
  });

  it('ArrowRight moves FOCUS only — does NOT change the active tab (manual)', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowRight' });
    const b = screen.getByTestId('tab-b');
    expect(b).toHaveFocus();
    expect(b).toHaveAttribute('tabindex', '0'); // roving follows focus
    expect(screen.getByTestId('tab-a')).toHaveAttribute('tabindex', '-1');
    // selection unchanged — arrow does not activate
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true');
    expect(b).toHaveAttribute('aria-selected', 'false');
  });

  it('click activates a tab (native button → selection changes)', () => {
    render(<Harness />);
    fireEvent.click(screen.getByTestId('tab-b'));
    expect(screen.getByTestId('tab-b')).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'false');
  });

  it('ArrowLeft from first wraps focus to last (no activation)', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowLeft' });
    expect(screen.getByTestId('tab-c')).toHaveFocus();
    expect(screen.getByTestId('tab-c')).toHaveAttribute('tabindex', '0');
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true'); // still active
  });

  it('Home/End move focus to first/last (no activation)', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'End' });
    expect(screen.getByTestId('tab-c')).toHaveFocus();
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'Home' });
    expect(screen.getByTestId('tab-a')).toHaveFocus();
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true');
  });

  it('ArrowDown=next / ArrowUp=prev focus movement (orientation-lenient)', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowDown' });
    expect(screen.getByTestId('tab-b')).toHaveFocus();
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowUp' });
    expect(screen.getByTestId('tab-a')).toHaveFocus();
  });

  it('ignores non-navigation keys', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'x' });
    expect(screen.getByTestId('tab-a')).toHaveAttribute('tabindex', '0');
  });

  it('blur resets roving to the active tab (Tab re-enters at active, not last-scanned)', () => {
    render(<Harness />);
    // arrow focus to b (roving=b), selection still a
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowRight' });
    expect(screen.getByTestId('tab-b')).toHaveAttribute('tabindex', '0');
    // focus leaves the tablist
    fireEvent.blur(screen.getByTestId('tl'), { relatedTarget: document.body });
    // roving resets to active (a)
    expect(screen.getByTestId('tab-a')).toHaveAttribute('tabindex', '0');
    expect(screen.getByTestId('tab-b')).toHaveAttribute('tabindex', '-1');
  });
});
