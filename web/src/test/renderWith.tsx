import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, type RenderResult } from '@testing-library/react';

// Test helper: render UI inside a fresh QueryClient so cache leaks
// between tests are impossible. Used by hook + page tests.
export function withQueryClient(children: React.ReactNode): React.ReactElement {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

export function renderWithQuery(children: React.ReactNode): RenderResult {
  return render(withQueryClient(children));
}

export function makeWrapper() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return function Wrapper({ children }: { children: React.ReactNode }): React.ReactElement {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}
