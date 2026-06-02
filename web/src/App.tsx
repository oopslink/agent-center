import type React from 'react';
import { lazy } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import AppLayout from './AppLayout';
import { OrgGuard, OrgRedirect } from './OrgContext';

// Auth pages render outside AppLayout (no nav/sidebar).
const Signup = lazy(() => import('./pages/Signup'));
const Signin = lazy(() => import('./pages/Signin'));

// All pages are lazy-loaded so the initial bundle stays small and each
// route ships as its own chunk (per F3 oversight #3). The Suspense
// boundary inside AppLayout renders a fallback while a chunk streams.
const Home = lazy(() => import('./pages/Home'));
const Channels = lazy(() => import('./pages/Channels'));
const ChannelDetail = lazy(() => import('./pages/ChannelDetail'));
const DMs = lazy(() => import('./pages/DMs'));
const DMDetail = lazy(() => import('./pages/DMDetail'));
const IssueDetail = lazy(() => import('./pages/IssueDetail'));
const TaskDetail = lazy(() => import('./pages/TaskDetail'));
const Agents = lazy(() => import('./pages/Agents'));
const AgentDetail = lazy(() => import('./pages/AgentDetail'));
const Projects = lazy(() => import('./pages/Projects'));
const ProjectDetail = lazy(() => import('./pages/ProjectDetail'));
const Secrets = lazy(() => import('./pages/Secrets'));
const Environment = lazy(() => import('./pages/Environment'));
const Settings = lazy(() => import('./pages/Settings'));
const Me = lazy(() => import('./pages/Me'));
const OrgSettings = lazy(() => import('./pages/OrgSettings'));
const MembersHumans = lazy(() => import('./pages/MembersHumans'));
const MembersAgents = lazy(() => import('./pages/MembersAgents'));
const MemberNew = lazy(() => import('./pages/MemberNew'));
const NotFound = lazy(() => import('./pages/NotFound'));

export function App(): React.ReactElement {
  return (
    <BrowserRouter>
      <Routes>
        {/* Auth routes — rendered outside AppLayout (no nav/sidebar). */}
        <Route path="/signup" element={<Signup />} />
        <Route path="/signin" element={<Signin />} />

        {/* Legacy root redirect → first org home (v2.6-FE-6) */}
        <Route index element={<OrgRedirect />} />

        {/* /organizations/:slug — all org-scoped routes */}
        <Route
          path="/organizations/:slug"
          element={
            <OrgGuard>
              <AppLayout />
            </OrgGuard>
          }
        >
          <Route index element={<Home />} />
          <Route path="channels" element={<Channels />} />
          <Route path="channels/:name" element={<ChannelDetail />} />
          <Route path="dms" element={<DMs />} />
          <Route path="dms/:id" element={<DMDetail />} />
          <Route path="agents" element={<Agents />} />
          <Route path="agents/:id" element={<AgentDetail />} />
          <Route path="projects" element={<Projects />} />
          <Route path="projects/:id" element={<ProjectDetail />} />
          <Route path="projects/:projectId/issues/:id" element={<IssueDetail />} />
          <Route path="projects/:projectId/tasks/:id" element={<TaskDetail />} />
          <Route path="secrets" element={<Secrets />} />
          {/* v2.7 #164: Fleet merged into Environment — keep /fleet working as a redirect. */}
          <Route path="fleet" element={<Navigate to="../environment" replace />} />
          <Route path="environment" element={<Environment />} />
          <Route path="settings" element={<Settings />} />
          <Route path="me" element={<Me />} />
          <Route path="org/settings" element={<OrgSettings />} />
          <Route path="members/humans" element={<MembersHumans />} />
          <Route path="members/agents" element={<MembersAgents />} />
          <Route path="members/new" element={<MemberNew />} />
          <Route path="*" element={<NotFound />} />
        </Route>

        {/* Legacy paths without org prefix — redirect to first org */}
        <Route path="*" element={<OrgRedirect />} />
      </Routes>
    </BrowserRouter>
  );
}
