import { http, HttpResponse, type JsonBodyType } from 'msw';

// MSW handlers for all 17 Web Console endpoints. Used by tests via
// src/test/mswServer.ts (Node setupServer). Per F4 oversight #4 these
// are NOT registered in the dev runtime — dev mode hits the real backend
// via the vite proxy.

const ok = (body: JsonBodyType, status = 200) => HttpResponse.json(body, { status });
const err = (status: number, error: string, message: string) =>
  HttpResponse.json({ error, message }, { status });

export const handlers = [
  // Health
  http.get('/api/health', () => ok({ status: 'ok' })),

  // Conversations
  http.get('/api/conversations', ({ request }) => {
    const url = new URL(request.url);
    const kind = url.searchParams.get('kind') ?? 'channel';
    return ok([
      { id: 'C1', kind, name: 'alpha', status: 'active', description: 'plan' },
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

  // BC-native Issue list/show (v2.3-5a). Discussion BC ownership; the
  // /api/conversations?kind=issue path is retained for backwards-compat
  // but the SPA cutover (v2.3-5b) reads from these.
  http.get('/api/issues', ({ request }) => {
    const url = new URL(request.url);
    const projectId = url.searchParams.get('project_id');
    if (!projectId) {
      return err(400, 'missing_project_id', 'project_id required');
    }
    return ok([
      {
        id: 'IS-1',
        project_id: projectId,
        conversation_id: 'I-1',
        title: 'sample issue',
        status: 'open',
        opened_at: '2026-05-24T01:00:00Z',
        opener: 'user:hayang',
      },
    ]);
  }),
  http.get('/api/issues/:id', ({ params }) =>
    ok({
      id: String(params.id),
      project_id: 'proj-a',
      conversation_id: 'I-1',
      title: 'sample issue',
      status: 'open',
      opened_at: '2026-05-24T01:00:00Z',
      opener: 'user:hayang',
    }),
  ),

  // BC-native Task list/show (v2.3-5a). TaskRuntime BC ownership.
  http.get('/api/tasks', ({ request }) => {
    const url = new URL(request.url);
    const projectId = url.searchParams.get('project_id');
    if (!projectId) {
      return err(400, 'missing_project_id', 'project_id required');
    }
    return ok([
      {
        id: 'TS-1',
        project_id: projectId,
        conversation_id: 'T-1',
        title: 'sample task',
        status: 'open',
        priority: 'medium',
        created_at: '2026-05-24T01:00:00Z',
      },
    ]);
  }),
  http.get('/api/tasks/:id', ({ params }) =>
    ok({
      id: String(params.id),
      project_id: 'proj-a',
      conversation_id: 'T-1',
      title: 'sample task',
      status: 'open',
      priority: 'medium',
      created_at: '2026-05-24T01:00:00Z',
    }),
  ),

  // Derivation
  http.post('/api/issues', () =>
    ok(
      {
        issue_id: 'IS-1',
        conversation_id: 'I-1',
        reference_count: 0,
        issue_event_id: 'E-i',
        carry_over_event_id: '',
      },
      201,
    ),
  ),
  http.post('/api/tasks', () =>
    ok(
      {
        task_id: 'TS-1',
        conversation_id: 'T-1',
        reference_count: 0,
        task_event_id: 'E-t',
        carry_over_event_id: '',
      },
      201,
    ),
  ),

  // Input requests
  http.get('/api/input_requests', () =>
    ok([
      {
        id: 'IR-1',
        status: 'pending',
        execution_id: 'E-1',
        question: 'go?',
        urgency: 'normal',
        created_at: '2026-05-24T01:00:00Z',
      },
    ]),
  ),
  http.post('/api/input_requests/:id/respond', () => ok({ event_id: 'E-resp' })),
  http.post('/api/input_requests/:id/cancel', () => ok({ cancelled: true })),

  // Agents
  http.get('/api/agents', () =>
    ok([
      { id: 'A-1', identity_id: 'agent:A-1', name: 'aa', agent_cli: 'claudecode', state: 'idle' },
    ]),
  ),
  http.get('/api/agents/:name', ({ params }) =>
    ok({
      id: 'A-1',
      identity_id: 'agent:A-1',
      name: params.name,
      agent_cli: 'claudecode',
      state: 'idle',
    }),
  ),

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

  // Projects (v2.5.5 projection: id / name / description / tags /
  // version / created_at / updated_at — kind + default_agent_cli
  // retired alongside ProjectKind).
  http.get('/api/projects', () =>
    ok([
      {
        id: 'proj-a',
        name: 'Project Alpha',
        description: 'First sample project',
        tags: ['coding'],
        version: 1,
        created_at: '2026-05-20T01:00:00Z',
        updated_at: '2026-05-20T01:00:00Z',
      },
    ]),
  ),
  http.get('/api/projects/:id', ({ params }) =>
    ok({
      id: String(params.id),
      name: 'Project Alpha',
      description: 'First sample project',
      tags: ['coding'],
      version: 1,
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    }),
  ),

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
    const body = (await request.json()) as { display_name?: string; role?: string };
    return ok({
      id: 'mem-new', organization_id: 'org-test', identity_id: `user:${body.display_name ?? 'new'}`,
      role: body.role ?? 'member', status: 'joined', joined_at: '2026-01-01T00:00:00Z',
    }, 201);
  }),
  http.patch('/api/members/:id/role', () => new HttpResponse(null, { status: 204 })),
  http.post('/api/members/:id/disable', () => new HttpResponse(null, { status: 204 })),
  http.post('/api/members/:id/reenable', () => new HttpResponse(null, { status: 204 })),

  // Fleet + trace
  http.get('/api/fleet', () =>
    ok({
      executions: [],
      workers: [],
      open_input_requests: [],
      pending_issues: [],
      generated_at: '2026-05-24T01:00:00Z',
    }),
  ),
  http.get('/api/tasks/:id/trace', () => ok([])),
];
