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
  inputRequests: () => ['inputRequests'] as const,
  fleet: () => ['fleet'] as const,
  taskTrace: (taskId: string) => ['taskTrace', taskId] as const,
};
