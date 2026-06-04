import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { server } from '@/test/mswServer';
import { FakeEventSource } from '@/sse/fakeEventSource';
import AppLayout from './AppLayout';

// Polyfill localStorage so AppLayout's persist effect works.
beforeAll(() => {
  (globalThis as unknown as { EventSource: typeof FakeEventSource }).EventSource = FakeEventSource;
  const store: Record<string, string> = {};
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (k: string) => (k in store ? store[k] : null),
      setItem: (k: string, v: string) => { store[k] = String(v); },
      removeItem: (k: string) => { delete store[k]; },
      clear: () => { for (const k of Object.keys(store)) delete store[k]; },
    },
    configurable: true,
  });
  server.use(http.get('/api/conversations', () => HttpResponse.json([])));
});

beforeEach(() => {
  localStorage.clear();
  document.documentElement.classList.remove('dark');
});
afterEach(() => cleanup());

function renderShell(initial = '/channels') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initial]}>
        <Routes>
          <Route element={<AppLayout />}>
            <Route path="/channels" element={<div data-testid="page-Channels">x</div>} />
            <Route path="/tasks" element={<div data-testid="page-Tasks">x</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('AppLayout v3 — P6 (theme + collapse + palette + shortcuts)', () => {
  it('theme toggle flips html.dark and persists', () => {
    renderShell();
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    fireEvent.click(screen.getByTestId('theme-toggle'));
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    expect(localStorage.getItem('ac.theme')).toBe('dark');
    fireEvent.click(screen.getByTestId('theme-toggle'));
    expect(document.documentElement.classList.contains('dark')).toBe(false);
    expect(localStorage.getItem('ac.theme')).toBe('light');
  });

  it('sidebar collapse toggle flips width + persists', () => {
    renderShell();
    const desktopNav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(desktopNav.getAttribute('data-collapsed')).toBe('false');
    fireEvent.click(screen.getByTestId('sidebar-collapse-toggle'));
    expect(desktopNav.getAttribute('data-collapsed')).toBe('true');
    expect(localStorage.getItem('ac.sidebar.collapsed')).toBe('1');
  });

  it('collapse toggle: state-based tooltip + aria + a clean single-chevron icon (#253)', () => {
    renderShell();
    const btn = screen.getByTestId('sidebar-collapse-toggle');
    // expanded → "Collapse sidebar"; the icon is a single stroke path (no rect).
    expect(btn).toHaveAttribute('title', 'Collapse sidebar');
    expect(btn).toHaveAttribute('aria-label', 'Collapse sidebar');
    const svg = btn.querySelector('svg');
    expect(svg?.querySelector('rect')).toBeNull();
    expect(svg?.querySelectorAll('path')).toHaveLength(1);
    // collapsed → tooltip + aria flip to "Expand sidebar".
    fireEvent.click(btn);
    expect(btn).toHaveAttribute('title', 'Expand sidebar');
    expect(btn).toHaveAttribute('aria-label', 'Expand sidebar');
  });

  it('Cmd+K opens command palette; Esc closes it', () => {
    renderShell();
    expect(screen.queryByTestId('command-palette')).not.toBeInTheDocument();
    fireEvent.keyDown(window, { key: 'k', metaKey: true });
    expect(screen.getByTestId('command-palette')).toBeInTheDocument();
    fireEvent.keyDown(screen.getByTestId('palette-input'), { key: 'Escape' });
    expect(screen.queryByTestId('command-palette')).not.toBeInTheDocument();
  });

  it('Cmd+B toggles sidebar collapse from keyboard', () => {
    renderShell();
    const desktopNav = screen.getByRole('navigation', { name: /^primary$/ });
    expect(desktopNav.getAttribute('data-collapsed')).toBe('false');
    fireEvent.keyDown(window, { key: 'b', metaKey: true });
    expect(desktopNav.getAttribute('data-collapsed')).toBe('true');
  });

  it('Cmd+D toggles theme from keyboard', () => {
    renderShell();
    fireEvent.keyDown(window, { key: 'd', metaKey: true });
    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});
