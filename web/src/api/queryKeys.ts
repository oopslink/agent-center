// Centralised query key factory so cache invalidations stay in sync
// between writers and readers.
export const qk = {
  conversations: (kind?: string) =>
    kind ? (['conversations', { kind }] as const) : (['conversations'] as const),
  conversation: (id: string) => ['conversation', id] as const,
  messages: (convId: string) => ['messages', convId] as const,
  refs: (convId: string) => ['refs', convId] as const,
  agents: () => ['agents'] as const,
  agent: (name: string) => ['agent', name] as const,
  secrets: () => ['secrets'] as const,
  projects: () => ['projects'] as const,
  project: (id: string) => ['project', id] as const,
  inputRequests: () => ['inputRequests'] as const,
  fleet: () => ['fleet'] as const,
  taskTrace: (taskId: string) => ['taskTrace', taskId] as const,
  unread: (convId: string) => ['unread', convId] as const,
  // v2.3-5b BC-native Issue/Task reads. Keyed by {projectId, status}
  // so changing either filter slot invalidates / refetches cleanly.
  // Distinct from `taskTrace` — TaskRuntime BC owns both surfaces but
  // they answer different questions (list/show vs execution trace).
  issues: (filter?: { projectId?: string; status?: string }) =>
    filter && (filter.projectId || filter.status)
      ? (['issues', filter] as const)
      : (['issues'] as const),
  issue: (id: string) => ['issue', id] as const,
  tasksList: (filter?: { projectId?: string; status?: string }) =>
    filter && (filter.projectId || filter.status)
      ? (['tasksList', filter] as const)
      : (['tasksList'] as const),
  task: (id: string) => ['task', id] as const,
};
