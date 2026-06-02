import { http, HttpResponse, type JsonBodyType } from 'msw';

// MSW handlers for all 17 Web Console endpoints. Used by tests via
// src/test/mswServer.ts (Node setupServer). Per F4 oversight #4 these
// are NOT registered in the dev runtime — dev mode hits the real backend
// via the vite proxy.

const ok = (body: JsonBodyType, status = 200) => HttpResponse.json(body, { status });
const err = (status: number, error: string, message: string) =>
  HttpResponse.json({ error, message }, { status });

// taskActionHandlers — the v2.7 task lifecycle sub-routes. Each returns
// the refreshed TaskMap with a status derived from the action.
function taskActionHandlers() {
  const baseTask = (pid: string, id: string, status: string, extra: Record<string, unknown> = {}) => ({
    id,
    project_id: pid,
    title: 'sample task',
    description: '',
    status,
    version: 2,
    created_at: '2026-05-24T01:00:00Z',
    updated_at: '2026-05-24T02:00:00Z',
    ...extra,
  });
  return [
    http.post('/api/projects/:pid/tasks/:id/assign', async ({ params, request }) => {
      const body = (await request.json()) as { assignee?: string };
      return ok(baseTask(String(params.pid), String(params.id), 'assigned', { assignee: body.assignee }));
    }),
    http.post('/api/projects/:pid/tasks/:id/start', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'running')),
    ),
    http.post('/api/projects/:pid/tasks/:id/block', async ({ params, request }) => {
      const body = (await request.json()) as { reason?: string };
      return ok(baseTask(String(params.pid), String(params.id), 'blocked', { blocked_reason: body.reason }));
    }),
    http.post('/api/projects/:pid/tasks/:id/unblock', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'running')),
    ),
    http.post('/api/projects/:pid/tasks/:id/complete', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'completed', { completed_by: 'agent:builder' })),
    ),
    http.post('/api/projects/:pid/tasks/:id/verify', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'verified')),
    ),
    http.post('/api/projects/:pid/tasks/:id/cancel', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'canceled')),
    ),
  ];
}

// agentHandlers — Agent BC (v2.7 #101) endpoints. The default agent 'aa'
// (id A-1) is used by the shared hooks.test fixtures. Lifecycle sub-routes
// echo back an AgentMap with a derived lifecycle.
function agentHandlers() {
  const baseAgent = (id: string, extra: Record<string, unknown> = {}) => ({
    id,
    organization_id: 'O-1',
    name: 'aa',
    description: '',
    model: 'claude-opus',
    cli: 'claudecode',
    env_vars: {},
    skills: [],
    worker_id: 'w-1',
    lifecycle: 'stopped',
    availability: 'available',
    created_by: 'user:hayang',
    version: 1,
    created_at: '2026-05-24T01:00:00Z',
    updated_at: '2026-05-24T02:00:00Z',
    ...extra,
  });
  return [
    http.get('/api/agents', () => ok({ agents: [baseAgent('A-1')] })),
    http.post('/api/agents', async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      return ok(baseAgent('A-NEW', { ...body }), 201);
    }),
    http.get('/api/agents/:id', ({ params }) =>
      ok(baseAgent(String(params.id))),
    ),
    http.post('/api/agents/:id/start', ({ params }) =>
      ok(baseAgent(String(params.id), { lifecycle: 'running' })),
    ),
    http.post('/api/agents/:id/stop', ({ params }) =>
      ok(baseAgent(String(params.id), { lifecycle: 'stopped' })),
    ),
    http.post('/api/agents/:id/restart', ({ params }) =>
      ok(baseAgent(String(params.id), { lifecycle: 'running' })),
    ),
    http.post('/api/agents/:id/reset', ({ params }) =>
      ok(baseAgent(String(params.id), { lifecycle: 'stopped' })),
    ),
    http.get('/api/agents/:id/work-items', ({ params }) =>
      ok({
        work_items: [
          {
            id: 'WI-1',
            agent_id: String(params.id),
            task_ref: 'task:T-1',
            status: 'queued',
            interactions: 0,
            version: 1,
            created_at: '2026-05-24T01:00:00Z',
            updated_at: '2026-05-24T01:00:00Z',
          },
        ],
      }),
    ),
    http.get('/api/agents/:id/activity', ({ params }) =>
      ok({
        activity: [
          {
            id: 'AC-1',
            agent_id: String(params.id),
            event_type: 'agent.started',
            payload: '{}',
            occurred_at: '2026-05-24T01:00:00Z',
          },
        ],
      }),
    ),
  ];
}

export const handlers = [
  // Health
  http.get('/api/health', () => ok({ status: 'ok' })),

  // Conversations
  http.get('/api/conversations', ({ request }) => {
    const url = new URL(request.url);
    const kind = url.searchParams.get('kind') ?? 'channel';
    // Distinct id per kind so a component merging channels + dms (e.g. Home,
    // sidebar) never sees two rows with the same React key.
    const id = kind === 'dm' ? 'D1' : 'C1';
    return ok([
      { id, kind, name: 'alpha', status: 'active', description: 'plan' },
    ]);
  }),
  http.post('/api/conversations', async ({ request }) => {
    const body = (await request.json()) as { kind: string; name?: string; members?: string[] };
    if (body.kind === 'channel' && !body.name) {
      return err(400, 'invalid_input', 'name required');
    }
    if (body.kind === 'dm' && (!body.members || body.members.length === 0)) {
      return err(400, 'invalid_input', 'members required');
    }
    return ok({ conversation_id: 'C-NEW', event_id: 'E-1', kind: body.kind }, 201);
  }),
  http.get('/api/conversations/:id', ({ params }) =>
    ok({ id: params.id, kind: 'channel', name: 'alpha', status: 'active', participants: [] }),
  ),
  http.post('/api/conversations/:id/archive', () => ok({ event_id: 'E-arch' })),
  http.get('/api/conversations/:id/refs', () => ok([])),
  http.get('/api/conversations/:id/messages', () =>
    ok([
      {
        id: 'M1',
        conversation_id: 'C1',
        sender_identity_id: 'user:hayang',
        content_kind: 'text',
        content: 'hi',
        direction: 'inbound',
        posted_at: '2026-05-24T01:00:00Z',
      },
    ]),
  ),
  http.post('/api/conversations/:id/messages', () =>
    ok({ message_id: 'M-NEW', event_id: 'E-2' }, 201),
  ),
  http.post('/api/conversations/:id/participants', () => ok({ event_id: 'E-inv' })),
  http.delete('/api/conversations/:id/participants/:identity_id', () =>
    ok({ event_id: 'E-kick' }),
  ),
  http.get('/api/conversations/:id/unread', ({ params }) =>
    ok({
      conversation_id: String(params.id),
      user_id: 'user:hayang',
      last_seen_message_id: '',
      unread_count: 0,
    }),
  ),
  http.post('/api/conversations/:id/seen', () =>
    ok({ last_seen_message_id: 'M1', version: 1, bumped: true, event_id: 'E-seen' }),
  ),

  // v2.7 ProjectManager BC — nested Issues under a project.
  http.get('/api/projects/:pid/issues', ({ params }) =>
    ok({
      issues: [
        {
          id: 'IS-1',
          project_id: String(params.pid),
          title: 'sample issue',
          description: '',
          status: 'open',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        },
      ],
    }),
  ),
  http.post('/api/projects/:pid/issues', async ({ params, request }) => {
    const body = (await request.json()) as { title?: string; description?: string };
    return ok(
      {
        id: 'IS-NEW',
        project_id: String(params.pid),
        title: body.title ?? 'new issue',
        description: body.description ?? '',
        status: 'open',
        created_by: 'user:hayang',
        version: 1,
        created_at: '2026-05-24T01:00:00Z',
        updated_at: '2026-05-24T01:00:00Z',
      },
      201,
    );
  }),
  http.get('/api/projects/:pid/issues/:id', ({ params }) =>
    ok({
      id: String(params.id),
      project_id: String(params.pid),
      title: 'sample issue',
      description: '',
      status: 'open',
      created_by: 'user:hayang',
      version: 1,
      created_at: '2026-05-24T01:00:00Z',
      updated_at: '2026-05-24T01:00:00Z',
    }),
  ),
  http.patch('/api/projects/:pid/issues/:id', async ({ params, request }) => {
    const body = (await request.json()) as { title?: string; description?: string };
    return ok({
      id: String(params.id),
      project_id: String(params.pid),
      title: body.title ?? 'sample issue',
      description: body.description ?? '',
      status: 'open',
      created_by: 'user:hayang',
      version: 2,
      created_at: '2026-05-24T01:00:00Z',
      updated_at: '2026-05-24T02:00:00Z',
    });
  }),
  http.post('/api/projects/:pid/issues/:id/transition', async ({ params, request }) => {
    const body = (await request.json()) as { status?: string };
    return ok({
      id: String(params.id),
      project_id: String(params.pid),
      title: 'sample issue',
      description: '',
      status: body.status ?? 'open',
      created_by: 'user:hayang',
      version: 2,
      created_at: '2026-05-24T01:00:00Z',
      updated_at: '2026-05-24T02:00:00Z',
    });
  }),

  // v2.7 ProjectManager BC — nested Tasks under a project.
  http.get('/api/projects/:pid/tasks', ({ params }) =>
    ok({
      tasks: [
        {
          id: 'TS-1',
          project_id: String(params.pid),
          title: 'sample task',
          description: '',
          status: 'open',
          version: 1,
          created_at: '2026-05-24T01:00:00Z',
          updated_at: '2026-05-24T01:00:00Z',
        },
      ],
    }),
  ),
  http.post('/api/projects/:pid/tasks', async ({ params, request }) => {
    const body = (await request.json()) as { title?: string; description?: string };
    return ok(
      {
        id: 'TS-NEW',
        project_id: String(params.pid),
        title: body.title ?? 'new task',
        description: body.description ?? '',
        status: 'open',
        version: 1,
        created_at: '2026-05-24T01:00:00Z',
        updated_at: '2026-05-24T01:00:00Z',
      },
      201,
    );
  }),
  http.get('/api/projects/:pid/tasks/:id', ({ params }) =>
    ok({
      id: String(params.id),
      project_id: String(params.pid),
      title: 'sample task',
      description: '',
      status: 'open',
      version: 1,
      created_at: '2026-05-24T01:00:00Z',
      updated_at: '2026-05-24T01:00:00Z',
    }),
  ),
  http.patch('/api/projects/:pid/tasks/:id', async ({ params, request }) => {
    const body = (await request.json()) as { title?: string; description?: string };
    return ok({
      id: String(params.id),
      project_id: String(params.pid),
      title: body.title ?? 'sample task',
      description: body.description ?? '',
      status: 'open',
      version: 2,
      created_at: '2026-05-24T01:00:00Z',
      updated_at: '2026-05-24T02:00:00Z',
    });
  }),
  ...taskActionHandlers(),

  // Code repos (read-only).
  http.get('/api/projects/:pid/code-repos', () => ok({ code_repos: [] })),

  // Project members (read-only).
  http.get('/api/projects/:pid/members', () => ok({ members: [] })),

  // Agents — Agent BC (v2.7 #101). Org-scoped, wrapped list shape, lifecycle
  // sub-routes + work-items / activity.
  ...agentHandlers(),

  // Secrets
  http.get('/api/secrets', () =>
    ok([
      {
        id: 'S-1',
        name: 'github',
        kind: 'other',
        state: 'active',
        created_at: '2026-05-01T00:00:00Z',
        created_by: 'user:hayang',
      },
    ]),
  ),
  // Create response is intentionally bare per ADR-0026 § 5: id + name +
  // event_id, no value field, no full secret projection. Mirror the
  // backend exactly so tests catch shape drift.
  http.post('/api/secrets', () =>
    ok({ id: 'S-NEW', name: 'new', event_id: 'E-c' }, 201),
  ),
  http.delete('/api/secrets/:id', () => ok({ revoked: true })),

  // SSE subscribe / unsubscribe (no streaming — the EventSource side
  // is intentionally not mocked here; tests that need stream data use
  // the fakeEventSource in src/sse/fakeEventSource.ts directly).
  http.post('/api/sse/subscribe', () => ok({ subscribed: true })),
  http.post('/api/sse/unsubscribe', () => ok({ unsubscribed: true })),

  // Projects (v2.7 ProjectManager BC projection: wrapped list response;
  // tags retired; status + organization_id + created_by added).
  http.get('/api/projects', () =>
    ok({
      projects: [
        {
          id: 'proj-a',
          organization_id: 'org-test',
          name: 'Project Alpha',
          description: 'First sample project',
          status: 'active',
          created_by: 'user:hayang',
          version: 1,
          created_at: '2026-05-20T01:00:00Z',
          updated_at: '2026-05-20T01:00:00Z',
        },
      ],
    }),
  ),
  http.post('/api/projects', async ({ request }) => {
    const body = (await request.json()) as { name?: string; description?: string };
    return ok(
      {
        id: 'proj-new',
        organization_id: 'org-test',
        name: body.name ?? 'New Project',
        description: body.description ?? '',
        status: 'active',
        created_by: 'user:hayang',
        version: 1,
        created_at: '2026-05-20T01:00:00Z',
        updated_at: '2026-05-20T01:00:00Z',
      },
      201,
    );
  }),
  http.get('/api/projects/:id', ({ params }) =>
    ok({
      id: String(params.id),
      organization_id: 'org-test',
      name: 'Project Alpha',
      description: 'First sample project',
      status: 'active',
      created_by: 'user:hayang',
      version: 1,
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    }),
  ),
  http.patch('/api/projects/:id', async ({ params, request }) => {
    const body = (await request.json()) as { name?: string; description?: string };
    return ok({
      id: String(params.id),
      organization_id: 'org-test',
      name: body.name ?? 'Project Alpha',
      description: body.description ?? '',
      status: 'active',
      created_by: 'user:hayang',
      version: 2,
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T02:00:00Z',
    });
  }),
  http.delete('/api/projects/:id', () => ok({ ok: true, status: 'archived' })),

  // Auth
  http.get('/api/auth/me', () =>
    ok({ identity_id: 'user-test', display_name: 'Test User', kind: 'user' }),
  ),
  http.post('/api/auth/signin', () => ok({ identity_id: 'user-test' })),
  http.post('/api/auth/signup', () =>
    ok({ identity_id: 'user-test', organization_id: 'org-test', display_name: 'Test User' }, 201),
  ),
  http.post('/api/auth/signout', () => new HttpResponse(null, { status: 204 })),
  http.patch('/api/auth/me/passcode', () => new HttpResponse(null, { status: 204 })),

  // Orgs
  http.get('/api/orgs', () =>
    ok([{ id: 'org-test', slug: 'test', name: 'Test Org', created_at: '2026-01-01T00:00:00Z' }]),
  ),
  http.post('/api/orgs', async ({ request }) => {
    const body = (await request.json()) as { name?: string; slug?: string };
    return ok({ id: 'org-new', slug: body.slug ?? 'new', name: body.name ?? 'New', created_at: '2026-01-01T00:00:00Z' }, 201);
  }),
  http.patch('/api/orgs/:id', () => new HttpResponse(null, { status: 204 })),
  http.delete('/api/orgs/:id', () => new HttpResponse(null, { status: 204 })),

  // Members
  http.get('/api/members', () =>
    ok([
      {
        id: 'mem-1', organization_id: 'org-test', identity_id: 'user:hayang',
        role: 'owner', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
      },
    ]),
  ),
  http.post('/api/members', async ({ request }) => {
    const body = (await request.json()) as { display_name?: string; role?: string; reuse?: boolean };
    const resp: Record<string, unknown> = {
      id: 'mem-new', organization_id: 'org-test',
      identity_id: `user-${(body.display_name ?? 'new').slice(0, 8)}`,
      kind: 'user',
      role: body.role ?? 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
      display_name: body.display_name ?? 'new',
    };
    if (!body.reuse) resp.temp_passcode = '123456';
    return ok(resp, 201);
  }),
  http.post('/api/members/agent', async ({ request }) => {
    const body = (await request.json()) as { display_name?: string; role?: string; worker_id?: string };
    const res: Record<string, unknown> = {
      id: 'mem-agent', organization_id: 'org-test',
      identity_id: `agent-${(body.display_name ?? 'new').slice(0, 8)}`,
      kind: 'agent', role: body.role ?? 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
      display_name: body.display_name ?? 'new',
    };
    // v2.7 #157: when worker_id is present the backend also creates the execution
    // Agent (unified one-step create) and returns its id.
    if (body.worker_id) res.agent_id = 'A-new';
    return ok(res, 201);
  }),
  http.patch('/api/members/:id/role', () => new HttpResponse(null, { status: 204 })),
  http.post('/api/members/:id/disable', () => new HttpResponse(null, { status: 204 })),
  http.post('/api/members/:id/reenable', () => new HttpResponse(null, { status: 204 })),

  // Fleet
  http.get('/api/fleet', () =>
    ok({
      work_items: [],
      workers: [],
      pending_issues: [],
      generated_at: '2026-05-24T01:00:00Z',
    }),
  ),

  // File transfers (v2.7 #164: Environment surfaces in-flight transfer sessions).
  http.get('/api/files/transfers', () => ok({ transfer_sessions: [] })),
];
