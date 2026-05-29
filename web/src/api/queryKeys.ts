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
  messages: (convId: string) => o('messages', convId),
  refs: (convId: string) => o('refs', convId),
  agents: () => o('agents'),
  agent: (name: string) => o('agent', name),
  secrets: () => o('secrets'),
  projects: () => o('projects'),
  project: (id: string) => o('project', id),
  inputRequests: () => o('inputRequests'),
  fleet: () => o('fleet'),
  taskTrace: (taskId: string) => o('taskTrace', taskId),
  unread: (convId: string) => o('unread', convId),
  // v2.3-5b BC-native Issue/Task reads. Keyed by {projectId, status}
  // so changing either filter slot invalidates / refetches cleanly.
  issues: (filter?: { projectId?: string; status?: string }) =>
    filter && (filter.projectId || filter.status)
      ? o('issues', filter)
      : o('issues'),
  issue: (id: string) => o('issue', id),
  tasksList: (filter?: { projectId?: string; status?: string }) =>
    filter && (filter.projectId || filter.status)
      ? o('tasksList', filter)
      : o('tasksList'),
  task: (id: string) => o('task', id),
};
