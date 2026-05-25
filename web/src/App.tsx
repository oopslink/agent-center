import type React from 'react';
import { lazy } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import AppLayout from './AppLayout';

// All pages are lazy-loaded so the initial bundle stays small and each
// route ships as its own chunk (per F3 oversight #3). The Suspense
// boundary inside AppLayout renders a fallback while a chunk streams.
const Home = lazy(() => import('./pages/Home'));
const Channels = lazy(() => import('./pages/Channels'));
const ChannelDetail = lazy(() => import('./pages/ChannelDetail'));
const DMs = lazy(() => import('./pages/DMs'));
const DMDetail = lazy(() => import('./pages/DMDetail'));
const Issues = lazy(() => import('./pages/Issues'));
const IssueDetail = lazy(() => import('./pages/IssueDetail'));
const Tasks = lazy(() => import('./pages/Tasks'));
const TaskDetail = lazy(() => import('./pages/TaskDetail'));
const TaskTrace = lazy(() => import('./pages/TaskTrace'));
const Agents = lazy(() => import('./pages/Agents'));
const AgentDetail = lazy(() => import('./pages/AgentDetail'));
const Projects = lazy(() => import('./pages/Projects'));
const ProjectDetail = lazy(() => import('./pages/ProjectDetail'));
const InputRequests = lazy(() => import('./pages/InputRequests'));
const Secrets = lazy(() => import('./pages/Secrets'));
const Fleet = lazy(() => import('./pages/Fleet'));
const Settings = lazy(() => import('./pages/Settings'));
const NotFound = lazy(() => import('./pages/NotFound'));

export function App(): React.ReactElement {
  return (
    <BrowserRouter>
      <Routes>
        <Route element={<AppLayout />}>
          {/* v2.3 P3: `/` is the Home / Overview dashboard. Previously
              redirected straight to /channels. */}
          <Route index element={<Home />} />
          <Route path="/channels" element={<Channels />} />
          <Route path="/channels/:name" element={<ChannelDetail />} />
          <Route path="/dms" element={<DMs />} />
          <Route path="/dms/:id" element={<DMDetail />} />
          <Route path="/issues" element={<Issues />} />
          <Route path="/issues/:id" element={<IssueDetail />} />
          <Route path="/tasks" element={<Tasks />} />
          <Route path="/tasks/:id" element={<TaskDetail />} />
          <Route path="/tasks/:id/trace" element={<TaskTrace />} />
          <Route path="/agents" element={<Agents />} />
          <Route path="/agents/:name" element={<AgentDetail />} />
          <Route path="/projects" element={<Projects />} />
          <Route path="/projects/:id" element={<ProjectDetail />} />
          <Route path="/inputrequests" element={<InputRequests />} />
          <Route path="/secrets" element={<Secrets />} />
          <Route path="/fleet" element={<Fleet />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="*" element={<NotFound />} />
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
