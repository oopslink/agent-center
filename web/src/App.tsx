import type React from 'react';
import { lazy } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import AppLayout from './AppLayout';

// All pages are lazy-loaded so the initial bundle stays small and each
// route ships as its own chunk (per F3 oversight #3). The Suspense
// boundary inside AppLayout renders a fallback while a chunk streams.
const Channels = lazy(() => import('./pages/Channels'));
const ChannelDetail = lazy(() => import('./pages/ChannelDetail'));
const DMs = lazy(() => import('./pages/DMs'));
const DMDetail = lazy(() => import('./pages/DMDetail'));
const Issues = lazy(() => import('./pages/Issues'));
const Tasks = lazy(() => import('./pages/Tasks'));
const TaskTrace = lazy(() => import('./pages/TaskTrace'));
const Agents = lazy(() => import('./pages/Agents'));
const AgentDetail = lazy(() => import('./pages/AgentDetail'));
const InputRequests = lazy(() => import('./pages/InputRequests'));
const Secrets = lazy(() => import('./pages/Secrets'));
const Fleet = lazy(() => import('./pages/Fleet'));
const Settings = lazy(() => import('./pages/Settings'));
const NotFound = lazy(() => import('./pages/NotFound'));

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
          <Route element={<AppLayout />}>
            <Route index element={<Navigate to="/channels" replace />} />
            <Route path="/channels" element={<Channels />} />
            <Route path="/channels/:name" element={<ChannelDetail />} />
            <Route path="/dms" element={<DMs />} />
            <Route path="/dms/:id" element={<DMDetail />} />
            <Route path="/issues" element={<Issues />} />
            <Route path="/tasks" element={<Tasks />} />
            <Route path="/tasks/:id/trace" element={<TaskTrace />} />
            <Route path="/agents" element={<Agents />} />
            <Route path="/agents/:name" element={<AgentDetail />} />
            <Route path="/inputrequests" element={<InputRequests />} />
            <Route path="/secrets" element={<Secrets />} />
            <Route path="/fleet" element={<Fleet />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
