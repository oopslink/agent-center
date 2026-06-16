import type React from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useContextPanelController, useContextPanelCollapse } from './contextPanel';

// This jsdom exposes a method-less `localStorage`, so install a Map-backed stub
// (mirrors useResizablePanel.test) to actually exercise persistence.
function installLocalStorage(): void {
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => (store.has(k) ? (store.get(k) as string) : null),
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => void store.clear(),
  });
}

// T184: the col④ fully-collapse state is persisted (ac.contextpanel.collapsed)
// and exposed to panel content via useContextPanelCollapse. These verify the
// hook contract + persistence without standing up the whole AppLayout shell.

function Consumer(): React.ReactElement {
  const collapse = useContextPanelCollapse();
  return (
    <div>
      <span data-testid="collapsed-state">{String(collapse?.collapsed)}</span>
      <button type="button" data-testid="do-collapse" onClick={() => collapse?.setCollapsed(true)}>
        collapse
      </button>
      <button type="button" data-testid="do-expand" onClick={() => collapse?.setCollapsed(false)}>
        expand
      </button>
    </div>
  );
}

function Harness(): React.ReactElement {
  const { Provider, value } = useContextPanelController();
  return (
    <Provider value={value}>
      <Consumer />
    </Provider>
  );
}

describe('contextPanel collapse (T184)', () => {
  beforeEach(() => installLocalStorage());
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it('defaults to not-collapsed and persists collapse/expand to localStorage', () => {
    render(<Harness />);
    expect(screen.getByTestId('collapsed-state')).toHaveTextContent('false');

    fireEvent.click(screen.getByTestId('do-collapse'));
    expect(screen.getByTestId('collapsed-state')).toHaveTextContent('true');
    expect(window.localStorage.getItem('ac.contextpanel.collapsed')).toBe('1');

    fireEvent.click(screen.getByTestId('do-expand'));
    expect(screen.getByTestId('collapsed-state')).toHaveTextContent('false');
    expect(window.localStorage.getItem('ac.contextpanel.collapsed')).toBe('0');
  });

  it('reads the persisted collapsed=1 on mount', () => {
    window.localStorage.setItem('ac.contextpanel.collapsed', '1');
    render(<Harness />);
    expect(screen.getByTestId('collapsed-state')).toHaveTextContent('true');
  });

  it('useContextPanelCollapse returns null outside the provider', () => {
    render(<Consumer />);
    // No provider → hook returns null → collapsed renders "undefined".
    expect(screen.getByTestId('collapsed-state')).toHaveTextContent('undefined');
  });
});
