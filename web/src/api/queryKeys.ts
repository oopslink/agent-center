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
  // v2.9.1 (task-169c598d): the ARCHIVED-only conversation list (?status=archived),
  // a SEPARATE cache key from the active list so the collapsed "Archived" group
  // caches independently and never collides with qk.conversations(kind).
  conversationsArchived: (kind?: string) =>
    kind ? o('conversations', 'archived', { kind }) : o('conversations', 'archived'),
  // I23 (T332): the cross-source unread-conversations digest (GET
  // /unread-conversations) for the main-sidebar "未读会话" region. Single org-scoped
  // list key; SSE invalidates it on message/read-state/lifecycle events.
  unreadConversations: () => o('unreadConversations'),
  // v2.26.0 I61: the "Needs your attention" panel data source (GET
  // /orgs/{slug}/attention) — stuck tasks UNIONed with the human's directed
  // unread (DM + @mention). Single org-scoped key; SSE invalidates it on the
  // same message / read-state / task block-lifecycle events that move its two
  // underlying sources (orgTasks + unread digest).
  attention: () => o('attention'),
  conversation: (id: string) => o('conversation', id),
  conversationByOwner: (ownerRef: string) => o('conversationByOwner', ownerRef),
  messages: (convId: string) => o('messages', convId),
  // v2.9.1 Threads: the replies of one root message
  // (GET /conversations/{convId}/messages/{rootId}/replies). Keyed by both ids
  // so each open thread caches independently and a reply invalidates only its
  // own thread (plus the messages list, for the root's reply_count bump).
  threadReplies: (convId: string, rootId: string) => o('threadReplies', convId, rootId),
  // The PREFIX key matching EVERY open thread's replies in one conversation
  // (the rootId is the 4th tuple element; this 3-element prefix partial-matches
  // all threadReplies(convId, *)). SSE conversation.message_added only carries
  // the conversation_id (no root/parent id), so it invalidates this prefix to
  // refresh the currently-open thread panel's replies — without it an agent's
  // reply landed in the cache for messages()/conversationThreads() but the open
  // thread panel kept reading its own un-invalidated threadReplies cache and
  // required a manual refresh. Only one thread panel is open at a time, so the
  // coarse prefix invalidation has no practical over-fetch.
  threadRepliesByConversation: (convId: string) => o('threadReplies', convId),
  // v2.9.1 Threads P2: all thread summaries in a conversation
  // (GET /conversations/{convId}/threads) — drives the Participants thread list.
  conversationThreads: (convId: string) => o('conversationThreads', convId),
  refs: (convId: string) => o('refs', convId),
  agents: () => o('agents'),
  invitations: () => o('invitations'),
  agent: (id: string) => o('agent', id),
  agentTasks: (id: string) => o('agentTasks', id),
  agentActivity: (id: string) => o('agentActivity', id),
  // I28/F6 per-agent analytics dashboard. Keyed by id + window so a different
  // from/to range caches independently; the task drill-down keys by task too.
  agentAnalytics: (id: string, from?: string, to?: string) =>
    o('agentAnalytics', id, from ?? '', to ?? ''),
  agentAnalyticsTask: (id: string, taskId: string) => o('agentAnalyticsTask', id, taskId),
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
  // v2.10.0 [T73]: task/issue-scoped file attachments, keyed by the scope id.
  taskFiles: (taskId: string) => o('taskFiles', taskId),
  issueFiles: (issueId: string) => o('issueFiles', issueId),
  codeReposByProject: (projectId: string) => o('codeReposByProject', projectId),
  // T583 (issue-921db054 / I5): read-only agent runtime browser.
  runtimeList: (agentId: string, path: string) => o('runtimeList', agentId, path),
  runtimeRead: (agentId: string, path: string) => o('runtimeRead', agentId, path),
  runtimeGitLog: (agentId: string, path: string) => o('runtimeGitLog', agentId, path),
  runtimeGitDiff: (agentId: string, path: string, ref: string) =>
    o('runtimeGitDiff', agentId, path, ref),
  // T593: per-agent live concurrency slots (3s poll), overlaid on the Tasks tab.
  agentConcurrency: (agentId: string) => o('agentConcurrency', agentId),
  // T575 (issue-f980c8de): workspace-level code-repo registry + remote viewing.
  templates: () => o('templates'),
  template: (id: string) => o('templates', id),
  workspaceRepos: () => o('workspaceRepos'),
  repoCommits: (repoId: string, branch: string) => o('repoCommits', repoId, branch),
  repoBranches: (repoId: string) => o('repoBranches', repoId),
  membersByProject: (projectId: string) => o('membersByProject', projectId),
  // Coarse no-arg list keys kept so derive.ts (deferred scope) keeps
  // compiling — it invalidates these after a derive-from-message POST.
  issues: () => o('issues'),
  tasksList: () => o('tasksList'),
  // v2.8 #258: org-scope cross-project aggregation, keyed by the filter set.
  orgIssues: (filters?: unknown) => o('orgIssues', filters ?? null),
  orgTasks: (filters?: unknown) => o('orgTasks', filters ?? null),
  // v2.10.0 [T6]: org-scoped cross-project Plan list (global Workspace > Plan).
  orgPlans: (filters?: unknown) => o('orgPlans', filters ?? null),
  // T207 reminders — org-scoped list (filter/status) + a single reminder detail.
  // No-arg form returns the 3-element PREFIX (no trailing param) so an
  // invalidateQueries(qk.reminders()) prefix-matches every filtered list key
  // o('reminders', {filters}); appending a `null` param (the old bug) made the
  // prefix [...,'reminders',null] never match [...,'reminders',{filters}], so
  // cancel/pause/resume/delete never refreshed the list. Mirrors qk.conversations.
  reminders: (filters?: unknown) => (filters ? o('reminders', filters) : o('reminders')),
  reminder: (id: string) => o('reminder', id),
  // T181: the PREFIX keys matching EVERY filtered orgTasks(...) / orgPlans(...)
  // query (the filter object is the 4th tuple element; this 3-element prefix
  // partial-matches all of them — same trick as plansByProjectAll). SSE
  // invalidation uses these so a `pm.task.created` / `pm.plan.created` (an agent
  // creating a task/plan it then references in a message) refreshes the org
  // aggregation list the message ref-resolver reads from — without this the list
  // stayed stale (30s) and the new task/plan ref never linkified.
  orgTasksAll: () => o('orgTasks'),
  // T233: the PREFIX key matching EVERY filtered orgIssues(...) query (mirrors
  // orgTasksAll). useCreateIssue invalidates this so the cross-project org Issues
  // list (useOrgWorkItems) refreshes immediately after a create — without it the
  // new issue stayed hidden until the 30s staleTime lapsed (only the project-
  // scoped issuesByProject list was being invalidated).
  orgIssuesAll: () => o('orgIssues'),
  orgPlansAll: () => o('orgPlans'),
  // v2.9 #286 Plan orchestration: Plans are per-project. The parallel list is
  // keyed by projectId; a single Plan (nodes + derived) keyed by plan id.
  plansByProject: (projectId: string) => o('plansByProject', projectId),
  // v2.9.2 (task-0543ece9): the PREFIX key matching every per-project Work Board
  // list. SSE invalidation can't always know the project id from a task event, so
  // it invalidates this prefix — react-query prefix-matches it against every
  // plansByProject(projectId), so the board's progress + node-status chips refresh
  // live when a child task moves (closes the "done child not visible on the card"
  // staleness gap; plan list was never wired to task SSE before).
  plansByProjectAll: () => o('plansByProject'),
  plan: (id: string) => o('plan', id),
  // v2.9 #291 Work Board: the Backlog column = the project's UNPLANNED tasks
  // (plan_id null), GET /projects/{pid}/tasks?unplanned=1. Distinct key from the
  // full task list so the board's Backlog refetches when a task is added to a
  // Plan (add-task invalidates tasksByProject; we mirror it for this key).
  unplannedTasksByProject: (projectId: string) => o('unplannedTasksByProject', projectId),
};
