import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import './index.css';
import { App } from './App';
import { ErrorBoundary } from './ErrorBoundary';
import { applyInitialTheme } from './theme';

// Apply the persisted theme before React mounts so the first paint
// matches user preference — no FOUC on dark-mode reload.
applyInitialTheme();

// QueryClient lives at module scope (per F4 oversight #2) so HMR + tests
// can swap App without losing cached queries.
//   - staleTime: 30s — pair with SSE invalidation. SSE pushes invalidate
//     queries; for views without SSE the 30s window keeps things fresh
//     enough without thrashing the network.
//   - mutations.retry: 0 — never auto-retry POST/DELETE (could double-
//     create / double-archive).
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 30_000 },
    mutations: { retry: 0 },
  },
});

const root = document.getElementById('root');
if (!root) {
  throw new Error('root element missing');
}
createRoot(root).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
);
