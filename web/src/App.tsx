import type React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, staleTime: 30_000 },
  },
});

export function App(): React.ReactElement {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<Navigate to="/channels" replace />} />
          <Route path="/channels" element={<PagePlaceholder name="channels" />} />
          <Route path="*" element={<PagePlaceholder name="not-found" />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}

// PagePlaceholder is replaced by real pages in F3+ subtasks. F1 ships
// just enough shell to prove the scaffold compiles, renders, and is
// testable.
function PagePlaceholder({ name }: { name: string }): React.ReactElement {
  return (
    <main className="p-6">
      <h1 className="text-2xl font-semibold">agent-center</h1>
      <p className="mt-2 text-slate-600">F1 scaffold — page: {name}</p>
    </main>
  );
}
