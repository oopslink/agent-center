// Centralised query key factory so cache invalidations stay in sync
// between writers and readers.
//
// v2.6 multi-org isolation: every org-scoped key is prefixed with the current
// organization slug (read from the /organizations/{slug}/... URL). This makes
// the React Query cache org-aware — switching orgs or opening a second tab on
// a different org no longer reuses the previous org's cached results.

// currentOrgScope reads the org slug from the browser URL. Returns 'no-org'
// when not on an org-scoped route (e.g. /signup) or in non-browser tests.
export function currentOrgScope(): string {
  try {
    if (typeof window === 'undefined' || !window.location) return 'no-org';
    const m = window.location.pathname.match(/^\/organizations\/([a-z0-9-]+)/);
    return m ? m[1] : 'no-org';
  } catch {
    return 'no-org';
  }
}

// o() builds an org-prefixed key tuple.
function o(...parts: readonly unknown[]): readonly unknown[] {
  return ['org', currentOrgScope(), ...parts];
}

export const qk = {
  conversations: (kind?: string) =>
    kind ? (o('conversations', { kind })) : (o('conversations')),
  conversation: (id: string) => o('conversation', id),
  conversationByOwner: (ownerRef: string) => o('conversationByOwner', ownerRef),
  messages: (convId: string) => o('messages', convId),
  refs: (convId: string) => o('refs', convId),
  agents: () => o('agents'),
  agent: (id: string) => o('agent', id),
  agentWorkItems: (id: string) => o('agentWorkItems', id),
  agentActivity: (id: string) => o('agentActivity', id),
  secrets: () => o('secrets'),
  projects: () => o('projects'),
  // v2.9 #298: archived-only project list (GET /projects?status=archived).
  // Distinct key from the active `projects()` list so the collapsed "已归档"
  // group fetches + caches independently and never collides with the active list.
  projectsArchived: () => o('projects', 'archived'),
  project: (id: string) => o('project', id),
  fleet: () => o('fleet'),
  workers: () => o('workers'),
  worker: (id: string) => o('worker', id),
  transferSessions: () => o('transferSessions'),
  unread: (convId: string) => o('unread', convId),
  // v2.7 ProjectManager BC: Issues/Tasks are per-project. Lists are
  // keyed by projectId; detail keys stay by id.
  issuesByProject: (projectId: string) => o('issuesByProject', projectId),
  issue: (id: string) => o('issue', id),
  tasksByProject: (projectId: string) => o('tasksByProject', projectId),
  task: (id: string) => o('task', id),
  codeReposByProject: (projectId: string) => o('codeReposByProject', projectId),
  membersByProject: (projectId: string) => o('membersByProject', projectId),
  // Coarse no-arg list keys kept so derive.ts (deferred scope) keeps
  // compiling — it invalidates these after a derive-from-message POST.
  issues: () => o('issues'),
  tasksList: () => o('tasksList'),
  // v2.8 #258: org-scope cross-project aggregation, keyed by the filter set.
  orgIssues: (filters?: unknown) => o('orgIssues', filters ?? null),
  orgTasks: (filters?: unknown) => o('orgTasks', filters ?? null),
  // v2.9 #286 Plan orchestration: Plans are per-project. The parallel list is
  // keyed by projectId; a single Plan (nodes + derived) keyed by plan id.
  plansByProject: (projectId: string) => o('plansByProject', projectId),
  plan: (id: string) => o('plan', id),
  // v2.9 #291 Work Board: the Backlog column = the project's UNPLANNED tasks
  // (plan_id null), GET /projects/{pid}/tasks?unplanned=1. Distinct key from the
  // full task list so the board's Backlog refetches when a task is added to a
  // Plan (add-task invalidates tasksByProject; we mirror it for this key).
  unplannedTasksByProject: (projectId: string) => o('unplannedTasksByProject', projectId),
};
