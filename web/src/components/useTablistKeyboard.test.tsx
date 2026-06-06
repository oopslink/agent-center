import { render, screen, fireEvent } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useState } from 'react';
import { useTablistKeyboard } from './useTablistKeyboard';

const KEYS = ['a', 'b', 'c'] as const;
type K = (typeof KEYS)[number];

function Harness() {
  const [active, setActive] = useState<K>('a');
  const { tablistRef, onKeyDown, tabIndexFor } = useTablistKeyboard({
    keys: KEYS,
    active,
    onActivate: setActive,
  });
  return (
    <nav role="tablist" ref={tablistRef} onKeyDown={onKeyDown} data-testid="tl">
      {KEYS.map((k) => (
        <button
          key={k}
          type="button"
          role="tab"
          aria-selected={active === k}
          tabIndex={tabIndexFor(k)}
          data-testid={`tab-${k}`}
        >
          {k}
        </button>
      ))}
    </nav>
  );
}

describe('useTablistKeyboard', () => {
  it('roving tabindex: only the active tab is in the Tab order', () => {
    render(<Harness />);
    expect(screen.getByTestId('tab-a')).toHaveAttribute('tabindex', '0');
    expect(screen.getByTestId('tab-b')).toHaveAttribute('tabindex', '-1');
    expect(screen.getByTestId('tab-c')).toHaveAttribute('tabindex', '-1');
  });

  it('ArrowRight moves selection + focus to the next tab', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowRight' });
    const b = screen.getByTestId('tab-b');
    expect(b).toHaveAttribute('aria-selected', 'true');
    expect(b).toHaveFocus();
    expect(b).toHaveAttribute('tabindex', '0');
    expect(screen.getByTestId('tab-a')).toHaveAttribute('tabindex', '-1');
  });

  it('ArrowLeft from first wraps to last', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowLeft' });
    expect(screen.getByTestId('tab-c')).toHaveAttribute('aria-selected', 'true');
  });

  it('ArrowRight from last wraps to first', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'End' });
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowRight' });
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true');
  });

  it('Home/End jump to first/last', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'End' });
    expect(screen.getByTestId('tab-c')).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'Home' });
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true');
  });

  it('ArrowDown=next / ArrowUp=prev (orientation-agnostic)', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowDown' });
    expect(screen.getByTestId('tab-b')).toHaveAttribute('aria-selected', 'true');
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'ArrowUp' });
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true');
  });

  it('ignores non-navigation keys', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByTestId('tl'), { key: 'x' });
    expect(screen.getByTestId('tab-a')).toHaveAttribute('aria-selected', 'true');
  });
});
