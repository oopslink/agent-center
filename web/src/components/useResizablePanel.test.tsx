import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import type React from 'react';
import { useResizablePanel, type ResizablePanelOptions } from './useResizablePanel';

// This jsdom exposes a method-less `localStorage` object, so the app's storage
// guards no-op in tests. Install a real Map-backed stub so persistence is exercised.
function installLocalStorage(): void {
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  });
}

// A tiny harness that surfaces the hook's width / resizing state as data-attrs
// and spreads the handle props onto a div we can fire pointer/keyboard events at.
function Harness(opts: ResizablePanelOptions): React.ReactElement {
  const { width, resizing, handleProps } = useResizablePanel(opts);
  return (
    <div data-testid="panel" data-width={width} data-resizing={resizing}>
      <div data-testid="handle" {...handleProps} />
    </div>
  );
}

function widthOf(): number {
  return Number(screen.getByTestId('panel').getAttribute('data-width'));
}

const KEY = 'ac.test.panel.w';

describe('useResizablePanel', () => {
  beforeEach(() => installLocalStorage());
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it('starts at defaultWidth when nothing is stored', () => {
    render(<Harness storageKey={KEY} defaultWidth={448} minWidth={320} maxWidth={768} />);
    expect(widthOf()).toBe(448);
  });

  it('restores a stored width (clamped) on mount', () => {
    localStorage.setItem(KEY, '600');
    render(<Harness storageKey={KEY} defaultWidth={448} minWidth={320} maxWidth={768} />);
    expect(widthOf()).toBe(600);
  });

  it('clamps a stored width above max down to max', () => {
    localStorage.setItem(KEY, '9999');
    render(<Harness storageKey={KEY} defaultWidth={448} minWidth={320} maxWidth={768} />);
    expect(widthOf()).toBe(768);
  });

  it('a left-edge drag leftward widens the panel and persists it', () => {
    render(<Harness storageKey={KEY} defaultWidth={448} minWidth={320} maxWidth={768} edge="left" />);
    const handle = screen.getByTestId('handle');
    fireEvent.mouseDown(handle, { clientX: 600 });
    expect(screen.getByTestId('panel').getAttribute('data-resizing')).toBe('true');
    fireEvent.mouseMove(window, { clientX: 500 }); // moved 100px left
    expect(widthOf()).toBe(548); // 448 + 100
    fireEvent.mouseUp(window, { clientX: 500 });
    expect(screen.getByTestId('panel').getAttribute('data-resizing')).toBe('false');
    expect(localStorage.getItem(KEY)).toBe('548');
  });

  it('clamps drag to [min, max]', () => {
    render(<Harness storageKey={KEY} defaultWidth={448} minWidth={320} maxWidth={768} edge="left" />);
    const handle = screen.getByTestId('handle');
    // drag far left -> exceed max -> capped
    fireEvent.mouseDown(handle, { clientX: 600 });
    fireEvent.mouseMove(window, { clientX: 0 });
    expect(widthOf()).toBe(768);
    fireEvent.mouseUp(window, { clientX: 0 });
    // drag far right -> below min -> capped
    fireEvent.mouseDown(handle, { clientX: 600 });
    fireEvent.mouseMove(window, { clientX: 5000 });
    expect(widthOf()).toBe(320);
  });

  it('supports a function maxWidth (viewport-relative) and re-clamps on resize', () => {
    // jsdom default innerWidth is 1024 -> 75% = 768
    localStorage.setItem(KEY, '700');
    render(
      <Harness
        storageKey={KEY}
        defaultWidth={448}
        minWidth={320}
        maxWidth={() => window.innerWidth * 0.75}
      />,
    );
    expect(widthOf()).toBe(700);
    // shrink viewport so 75% (450) is below the current width -> re-clamped
    (window as unknown as { innerWidth: number }).innerWidth = 600;
    fireEvent(window, new Event('resize'));
    expect(widthOf()).toBe(450);
    (window as unknown as { innerWidth: number }).innerWidth = 1024; // restore
  });

  it('keyboard arrows resize a left-edge handle (ArrowLeft grows, ArrowRight shrinks)', () => {
    render(<Harness storageKey={KEY} defaultWidth={448} minWidth={320} maxWidth={768} edge="left" />);
    const handle = screen.getByTestId('handle');
    fireEvent.keyDown(handle, { key: 'ArrowLeft' });
    expect(widthOf()).toBe(448 + 24);
    fireEvent.keyDown(handle, { key: 'ArrowRight' });
    expect(widthOf()).toBe(448);
  });
});
