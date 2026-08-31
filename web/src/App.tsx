import type React from 'react';
import { lazy } from 'react';
import { BrowserRouter, Routes, Route, Navigate, useParams } from 'react-router-dom';
import AppLayout from './AppLayout';
import { OrgGuard, OrgRedirect } from './OrgContext';

// Auth pages render outside AppLayout (no nav/sidebar).
const Signup = lazy(() => import('./pages/Signup'));
const Signin = lazy(() => import('./pages/Signin'));
const InvitationAccept = lazy(() => import('./pages/InvitationAccept'));

// All pages are lazy-loaded so the initial bundle stays small and each
// route ships as its own chunk (per F3 oversight #3). The Suspense
// boundary inside AppLayout renders a fallback while a chunk streams.
// v2.10.0 [T1]: the Overview/Home dashboard is removed — the org index
// redirects into the Workspace module (see the index route below).
const Unread = lazy(() => import('./pages/Unread'));
const Channels = lazy(() => import('./pages/Channels'));
const ChannelDetail = lazy(() => import('./pages/ChannelDetail'));
const DMs = lazy(() => import('./pages/DMs'));
const DMDetail = lazy(() => import('./pages/DMDetail'));
const IssueDetail = lazy(() => import('./pages/IssueDetail'));
const TaskDetail = lazy(() => import('./pages/TaskDetail'));
// dev2/v281→members-into-teams: the /agents route now redirects into the merged
// Teams directory (teams/agents = TeamsDirectoryAgents). The Agents page
// component survives only as the Org Settings > Agents section panel (imported
// directly there), so it is no longer lazy-loaded here.
const AgentDetail = lazy(() => import('./pages/AgentDetail'));
const Projects = lazy(() => import('./pages/Projects'));
const ProjectDetail = lazy(() => import('./pages/ProjectDetail'));
// v2.9 #286: per-project Plan orchestration (parallel list + Plan detail).
const ProjectPlans = lazy(() => import('./pages/ProjectPlans'));
const PlanDetail = lazy(() => import('./pages/PlanDetail'));
const OrgWorkItems = lazy(() => import('./pages/OrgWorkItems'));
// v2.10.0 [T6]: global cross-project Plan list (Workspace > Plan).
const OrgPlans = lazy(() => import('./pages/OrgPlans'));
// T575 (issue-f980c8de): workspace-level code-repo registry (Workspace > Repos).
const OrgRepos = lazy(() => import('./pages/OrgRepos'));
const Reminders = lazy(() => import('./pages/Reminders'));
const InsightOverview = lazy(() => import('./pages/InsightOverview'));
const InsightAgents = lazy(() => import('./pages/InsightAgents'));
const InsightAgentDetail = lazy(() => import('./pages/InsightAgents').then((m) => ({ default: m.InsightAgentDetailPage })));
const InsightExecutions = lazy(() => import('./pages/InsightOverview').then((m) => ({ default: m.InsightExecutionsPage })));
const InsightExecutionDetail = lazy(() => import('./pages/InsightOverview').then((m) => ({ default: m.InsightExecutionDetailPage })));
const Access = lazy(() => import('./pages/Access'));
const Secrets = lazy(() => import('./pages/Secrets'));
const Environment = lazy(() => import('./pages/Environment'));
const AiRuntime = lazy(() => import('./pages/AiRuntime'));
const WorkerDetail = lazy(() => import('./pages/WorkerDetail'));
const Settings = lazy(() => import('./pages/Settings'));
const OrganizationSettings = lazy(() => import('./pages/OrganizationSettings'));
const Version = lazy(() => import('./pages/Version'));
const Me = lazy(() => import('./pages/Me'));
// members-into-teams: /members/humans redirects into teams/humans
// (TeamsDirectoryHumans, the merged surface); the MembersHumans page component
// survives only for its isolated unit tests, so it is no longer lazy-loaded here.
// Team WebUI (Phase-1) — Team BC surface (rail module 'teamui').
const Teams = lazy(() => import('./pages/Teams'));
const TeamDetail = lazy(() => import('./pages/TeamDetail'));
const TeamRoleDetail = lazy(() => import('./pages/TeamRoleDetail'));
const TeamsDirectoryAgents = lazy(() => import('./pages/TeamsDirectoryAgents'));
const TeamsDirectoryHumans = lazy(() => import('./pages/TeamsDirectoryHumans'));
// dev2/v29-s42 §4.2: MemberNew is no longer routed (orphan retired → redirect
// below); the page component stays for its isolated unit tests. No lazy import
// here so the unreachable chunk isn't shipped.
const UserDetail = lazy(() => import('./pages/UserDetail'));
const NotFound = lazy(() => import('./pages/NotFound'));

function OrgAiRuntimeRedirect({ tab }: { tab?: 'models' }): React.ReactElement {
  const { slug } = useParams<{ slug?: string }>();
  const suffix = tab ? `?tab=${tab}` : '';
  return <Navigate to={slug ? `/organizations/${slug}/ai-runtime${suffix}` : `/ai-runtime${suffix}`} replace />;
}

export function App(): React.ReactElement {
  return (
    <BrowserRouter>
      <Routes>
        {/* Auth routes — rendered outside AppLayout (no nav/sidebar). */}
        <Route path="/signup" element={<Signup />} />
        <Route path="/signin" element={<Signin />} />
        <Route path="/organizations/:slug/invitations/:token/accept" element={<InvitationAccept />} />

        {/* Legacy root redirect → first org home (v2.6-FE-6) */}
        <Route index element={<OrgRedirect />} />
        <Route path="/ai-runtime" element={<OrgRedirect to="ai-runtime" />} />

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
          {/* T343: mobile cross-source unread digest (desktop uses col②). */}
          <Route path="unread" element={<Unread />} />
          <Route path="channels" element={<Channels />} />
          <Route path="channels/:channelId" element={<ChannelDetail />} />
          <Route path="dms" element={<DMs />} />
          <Route path="dms/:id" element={<DMDetail />} />
          {/* members-into-teams: the org-level /agents list is merged into the
              Teams directory. /agents redirects to the merged teams/agents page
              (mirrors the members/agents → ../agents and fleet → ../environment
              precedents). The agents/:id detail page keeps its original URL. */}
          <Route path="agents" element={<Navigate to="../teams/agents" replace />} />
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
          {/* T575 (issue-f980c8de): workspace-level Repos — top-level code-repo
              registry (CRUD + credentials + remote viewing). */}
          <Route path="repos" element={<OrgRepos />} />
          {/* Legacy model catalog UI now converges on AI Runtime Models. */}
          <Route path="model-catalog" element={<OrgAiRuntimeRedirect tab="models" />} />
          {/* T207 [提醒-3]: Reminder management (Cognition BC). */}
          <Route path="reminders" element={<Reminders />} />
          <Route path="insights" element={<Navigate to="overview" replace />} />
          <Route path="insights/overview" element={<InsightOverview />} />
          <Route path="insights/agents" element={<InsightAgents />} />
          <Route path="insights/agents/:agentRef" element={<InsightAgentDetail />} />
          <Route path="insights/executions" element={<InsightExecutions />} />
          <Route path="insights/executions/:executionId" element={<InsightExecutionDetail />} />
          <Route path="access" element={<Navigate to="ram-roles" replace />} />
          <Route path="access/ram-roles" element={<Access page="ram-roles" />} />
          <Route path="access/subject-access" element={<Access page="subject-access" />} />
          <Route path="access/grant-access" element={<Access page="grant-access" />} />
          <Route path="secrets" element={<Secrets />} />
          {/* v2.7 #164: Fleet merged into Environment — keep /fleet working as a redirect. */}
          <Route path="fleet" element={<Navigate to="../environment" replace />} />
          <Route path="environment" element={<Environment />} />
          <Route path="workers/:id" element={<WorkerDetail />} />
          <Route path="settings" element={<Settings />} />
          {/* I41 (T470): the 5 Org Settings sections are routed sub-paths so they
              render via the shell's col② secondary nav (OrgSettingsSecondaryNav),
              not a page-internal card-nav. The bare path redirects to Profile. */}
          <Route path="organization-settings" element={<Navigate to="profile" replace />} />
          <Route path="organization-settings/ai-runtime" element={<OrgAiRuntimeRedirect />} />
          <Route path="organization-settings/:section" element={<OrganizationSettings />} />
          <Route path="ai-runtime" element={<AiRuntime />} />
          <Route path="version" element={<Version />} />
          <Route path="me" element={<Me />} />
          {/* members-into-teams: the org Members → Humans list is merged into the
              Teams directory. /members/humans redirects to the merged teams/humans
              page (same route-relative style as members/agents → ../agents). */}
          <Route path="members/humans" element={<Navigate to="../teams/humans" replace />} />
          {/* Team WebUI (Phase-1). Static children rank above teams/:teamId. */}
          <Route path="teams" element={<Teams />} />
          {/* ADR-0059: RAM Roles are edited on each Team Role, not on a
              standalone relationship-management page. Keep stale bookmarks
              recoverable without retaining a second product surface. */}
          <Route path="teams/roles" element={<Navigate to="../teams" replace />} />
          <Route path="teams/templates" element={<NotFound />} />
          <Route path="teams/templates/:templateId" element={<NotFound />} />
          <Route path="teams/agents" element={<TeamsDirectoryAgents />} />
          <Route path="teams/humans" element={<TeamsDirectoryHumans />} />
          <Route path="teams/:teamId/roles/:role" element={<TeamRoleDetail />} />
          <Route path="teams/:teamId" element={<TeamDetail />} />
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
