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
// v2.10.0 [T1]: the Overview/Home dashboard is removed — the org index
// redirects into the Workspace module (see the index route below).
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
// v2.9 #286: per-project Plan orchestration (parallel list + Plan detail).
const ProjectPlans = lazy(() => import('./pages/ProjectPlans'));
const PlanDetail = lazy(() => import('./pages/PlanDetail'));
const OrgWorkItems = lazy(() => import('./pages/OrgWorkItems'));
// v2.10.0 [T6]: global cross-project Plan list (Workspace > Plan).
const OrgPlans = lazy(() => import('./pages/OrgPlans'));
const Reminders = lazy(() => import('./pages/Reminders'));
const Secrets = lazy(() => import('./pages/Secrets'));
const Environment = lazy(() => import('./pages/Environment'));
const WorkerDetail = lazy(() => import('./pages/WorkerDetail'));
const Settings = lazy(() => import('./pages/Settings'));
const Version = lazy(() => import('./pages/Version'));
const Me = lazy(() => import('./pages/Me'));
const MembersHumans = lazy(() => import('./pages/MembersHumans'));
// dev2/v29-s42 §4.2: MemberNew is no longer routed (orphan retired → redirect
// below); the page component stays for its isolated unit tests. No lazy import
// here so the unreachable chunk isn't shipped.
const UserDetail = lazy(() => import('./pages/UserDetail'));
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
          {/* v2.10.0 [T1]: Overview/Home removed. The org index redirects into
              the Workspace module's default page (Projects). */}
          <Route index element={<Navigate to="projects" replace />} />
          <Route path="channels" element={<Channels />} />
          <Route path="channels/:channelId" element={<ChannelDetail />} />
          <Route path="dms" element={<DMs />} />
          <Route path="dms/:id" element={<DMDetail />} />
          <Route path="agents" element={<Agents />} />
          <Route path="agents/:id" element={<AgentDetail />} />
          <Route path="projects" element={<Projects />} />
          <Route path="projects/:id" element={<ProjectDetail />} />
          {/* v2.9 #286: Plan orchestration — parallel list + Plan detail (DAG
              view filled by #287). Reached via the project Plans tab. */}
          <Route path="projects/:id/plans" element={<ProjectPlans />} />
          <Route path="projects/:id/plans/:planId" element={<PlanDetail />} />
          <Route path="projects/:projectId/issues/:id" element={<IssueDetail />} />
          <Route path="projects/:projectId/tasks/:id" element={<TaskDetail />} />
          {/* v2.8 #258: org-scope cross-project Issues/Tasks aggregation. */}
          <Route path="issues" element={<OrgWorkItems kind="issue" />} />
          <Route path="tasks" element={<OrgWorkItems kind="task" />} />
          {/* v2.10.0 [T6]: org-scope cross-project Plan list (Workspace > Plan). */}
          <Route path="plans" element={<OrgPlans />} />
          {/* T207 [提醒-3]: Reminder management (Cognition BC). */}
          <Route path="reminders" element={<Reminders />} />
          <Route path="secrets" element={<Secrets />} />
          {/* v2.7 #164: Fleet merged into Environment — keep /fleet working as a redirect. */}
          <Route path="fleet" element={<Navigate to="../environment" replace />} />
          <Route path="environment" element={<Environment />} />
          <Route path="workers/:id" element={<WorkerDetail />} />
          <Route path="settings" element={<Settings />} />
          <Route path="version" element={<Version />} />
          <Route path="me" element={<Me />} />
          <Route path="members/humans" element={<MembersHumans />} />
          {/* dev2/v281: the enhanced /agents page is the single canonical
              agents surface. The old /members/agents page is retired — it
              redirects so the old URL + any stale link lands on canonical and
              there is no second reachable agents page. (mirrors the /fleet→
              /environment redirect precedent above.) */}
          <Route path="members/agents" element={<Navigate to="../agents" replace />} />
          {/* dev2/v29-s42 §4.2: /members/new is an ORPHAN — its sole inbound
              link lived on the retired /members/agents page, and the canonical
              /agents surface now creates agents via an inline AgentCreateModal
              (and Members→Humans via AddUserModal). Nothing live reaches this
              page, so the legacy URL redirects to the canonical /agents list —
              mirroring the /members/agents and /fleet retirement precedents —
              so a stale link lands on a reachable surface, not a direct-URL-
              only orphan. (MemberNew is kept as a component + unit-tested in
              isolation; it is simply no longer a routed page.) */}
          <Route path="members/new" element={<Navigate to="../agents" replace />} />
          <Route path="users/:userId" element={<UserDetail />} />
          <Route path="*" element={<NotFound />} />
        </Route>

        {/* Legacy paths without org prefix — redirect to first org */}
        <Route path="*" element={<OrgRedirect />} />
      </Routes>
    </BrowserRouter>
  );
}
