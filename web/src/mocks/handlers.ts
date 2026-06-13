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
      // v2.8.1 #5th: assign is a metadata write — status stays unchanged (open).
      const body = (await request.json()) as { assignee?: string };
      return ok(baseTask(String(params.pid), String(params.id), 'open', { assignee: body.assignee }));
    }),
    http.post('/api/projects/:pid/tasks/:id/start', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'running')),
    ),
    http.post('/api/projects/:pid/tasks/:id/block', async ({ params, request }) => {
      // ADR-0046: block sets the `blocked_reason` "stuck" annotation; the task
      // STAYS running (blocked is no longer a status).
      const body = (await request.json()) as { reason?: string };
      return ok(baseTask(String(params.pid), String(params.id), 'running', { blocked_reason: body.reason }));
    }),
    http.post('/api/projects/:pid/tasks/:id/unblock', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'running', { blocked_reason: '' })),
    ),
    http.post('/api/projects/:pid/tasks/:id/complete', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'completed', { completed_by: 'agent:builder' })),
    ),
    http.post('/api/projects/:pid/tasks/:id/discard', ({ params }) =>
      ok(baseTask(String(params.pid), String(params.id), 'discarded')),
    ),
    http.post('/api/projects/:pid/tasks/:id/unassign', ({ params }) =>
      // v2.8.1 #5th: unassign clears the assignee (metadata) — status unchanged.
      ok(baseTask(String(params.pid), String(params.id), 'open', { assignee: null })),
    ),
  ];
}

// planHandlers — v2.9 #286 Plan orchestration. mock=contract: shapes track the
// LOCKED v2.9 backend contract (Plan DTO with derived progress/has_failed; Node
// DTO §9.2). Stateless echo mocks sufficient for the UI + hook tests; the real
// orchestrator derivation lands backend-side.
function planHandlers() {
  const baseNode = (taskId: string, extra: Record<string, unknown> = {}) => ({
    task_id: taskId,
    title: 'sample task',
    assignee_ref: 'agent:builder',
    task_status: 'open',
    node_status: 'ready',
    depends_on: [] as string[],
    dispatched_at: null,
    ...extra,
  });
  const basePlan = (pid: string, id: string, extra: Record<string, unknown> = {}) => ({
    id,
    project_id: pid,
    name: 'Sample plan',
    description: '',
    status: 'draft',
    creator_ref: 'user:owner',
    conversation_id: 'conv-plan-1',
    target_date: null,
    has_failed: false,
    progress: { done: 0, total: 0 },
    created_at: '2026-06-01T01:00:00Z',
    // NB: no `nodes` / `nodes_preview` here — the LIST rows add the enriched
    // nodes_preview/node_count, the DETAIL + write responses add `nodes`. This
    // keeps each mock shape matched to its real DTO (pmPlanSummaryMap vs
    // pmPlanDetailMap) instead of leaking a field into the wrong response.
    ...extra,
  });
  return [
    // GET / — parallel Plan list (wrapped under `plans`). mock=contract to the
    // ENRICHED list DTO (merged PR #272 → v2.9 trunk 654d30e, pmPlanSummaryMap):
    // each row carries progress{done,total} + has_failed + node_count (TOTAL) +
    // nodes_preview (capped 4, FULL PlanNode shape incl task_status so the card
    // StatusChip reads it without crashing). NB: the list row carries
    // nodes_preview/node_count, NOT the detail `nodes` field.
    http.get('/api/projects/:pid/plans', ({ params }) =>
      ok({
        plans: [
          basePlan(String(params.pid), 'PL-1', {
            name: 'Onboarding flow',
            status: 'running',
            has_failed: true,
            progress: { done: 2, total: 5 },
            target_date: '2026-07-01T00:00:00Z',
            // 6 total nodes; preview capped at 4 → board shows "…and 2 more".
            node_count: 6,
            nodes_preview: [
              baseNode('TS-1', { title: 'Design intake form', task_status: 'done', node_status: 'done' }),
              baseNode('TS-2', { title: 'Wire welcome email', task_status: 'running', node_status: 'running', assignee_ref: 'user:hayang' }),
              baseNode('TS-3', { title: 'Set up SSO', task_status: 'open' }),
              baseNode('TS-4', { title: 'Seed sample data', task_status: 'open' }),
            ],
          }),
          basePlan(String(params.pid), 'PL-2', {
            name: 'Billing rework',
            status: 'draft',
            progress: { done: 0, total: 0 },
            node_count: 0,
            nodes_preview: [],
          }),
        ],
      }),
    ),
    // POST / — create empty Plan.
    http.post('/api/projects/:pid/plans', async ({ params, request }) => {
      const body = (await request.json()) as {
        name?: string;
        description?: string;
        target_date?: string | null;
      };
      return ok(
        basePlan(String(params.pid), 'PL-NEW', {
          name: body.name ?? 'new plan',
          description: body.description ?? '',
          target_date: body.target_date ?? null,
          nodes: [] as unknown[], // detail-shaped write response (pmPlanDetailMap).
        }),
        201,
      );
    }),
    // GET /:id — single Plan with derived nodes.
    http.get('/api/projects/:pid/plans/:id', ({ params }) =>
      ok(
        basePlan(String(params.pid), String(params.id), {
          name: 'Onboarding flow',
          progress: { done: 0, total: 1 },
          nodes: [baseNode('TS-1', { title: 'sample task' })],
        }),
      ),
    ),
    // PATCH /:id — draft-only edit (name/goal/target_date).
    http.patch('/api/projects/:pid/plans/:id', async ({ params, request }) => {
      const body = (await request.json()) as Record<string, unknown>;
      return ok(basePlan(String(params.pid), String(params.id), { nodes: [], ...body }));
    }),
    // POST /:id/tasks — select a backlog task into the Plan.
    http.post('/api/projects/:pid/plans/:id/tasks', async ({ params, request }) => {
      const body = (await request.json()) as { task_id?: string };
      return ok(
        basePlan(String(params.pid), String(params.id), {
          progress: { done: 0, total: 1 },
          nodes: [baseNode(body.task_id ?? 'TS-1')],
        }),
      );
    }),
    // DELETE /:id/tasks/:taskId — remove a task from the Plan.
    http.delete('/api/projects/:pid/plans/:id/tasks/:taskId', () =>
      new HttpResponse(null, { status: 204 }),
    ),
    // #287 deps + lifecycle (stubbed contract surface).
    http.post('/api/projects/:pid/plans/:id/dependencies', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { nodes: [] })),
    ),
    http.delete('/api/projects/:pid/plans/:id/dependencies', () =>
      new HttpResponse(null, { status: 204 }),
    ),
    http.post('/api/projects/:pid/plans/:id/start', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'running', nodes: [] })),
    ),
    http.post('/api/projects/:pid/plans/:id/stop', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'draft', nodes: [] })),
    ),
    http.post('/api/projects/:pid/plans/:id/advance', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'running', nodes: [] })),
    ),
    // v2.9 Stage B (#280): DELETE /:id → { deleted: true }. Non-running only
    // (running → 409 plan_conflict on the real backend); the plan is gone after.
    http.delete('/api/projects/:pid/plans/:id', () => ok({ deleted: true })),
    // v2.9 Stage B (#290): POST /:id/archive → the archived plan detail. Cascade
    // plan→archived + ALL plan tasks→archived (task.status preserved).
    http.post('/api/projects/:pid/plans/:id/archive', ({ params }) =>
      ok(
        basePlan(String(params.pid), String(params.id), {
          status: 'archived',
          archived_at: '2026-06-11T00:00:00Z',
          archived_by: 'user:owner',
          nodes: [
            baseNode('TS-1', {
              title: 'sample task',
              archived: true,
              archived_at: '2026-06-11T00:00:00Z',
              archived_by: 'user:owner',
            }),
          ],
        }),
      ),
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
    // v2.7 #186/#77: POST /api/agents removed; agent creation = POST /api/members/agent.
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

const baseHandlers = [
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
  // v2.9.1 Threads: default-empty so every conversation surface that renders the
  // thread affordance / thread list doesn't trip onUnhandledRequest:'error'.
  http.get('/api/conversations/:id/threads', () => ok([])),
  http.get('/api/conversations/:id/messages/:mid/replies', () => ok([])),
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

  // v2.7 ProjectManager BC — nested Tasks under a project. v2.9 #291 Work Board:
  // the `?unplanned=1` filter (Dev's endpoint, org-gated) returns only the
  // project tasks with NO plan (plan_id null) — the Backlog column source. Same
  // Task[] shape as the full list (mock=contract). Without the filter the full
  // project task list is returned (existing behaviour, unchanged).
  http.get('/api/projects/:pid/tasks', ({ params, request }) => {
    const unplanned = new URL(request.url).searchParams.get('unplanned');
    const tasks = [
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
    ];
    if (unplanned === '1') {
      // Backlog: distinct unplanned task with an assignee so the avatar renders.
      return ok({
        tasks: [
          {
            id: 'TS-BL1',
            project_id: String(params.pid),
            title: 'unplanned backlog task',
            description: '',
            status: 'open',
            assignee: 'agent:builder',
            version: 1,
            created_at: '2026-05-24T01:00:00Z',
            updated_at: '2026-05-24T01:00:00Z',
          },
        ],
      });
    }
    return ok({ tasks });
  }),
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

  // v2.9 #286 Plan orchestration — mock=contract to the LOCKED v2.9 backend
  // contract (base /api/projects/:pid/plans). Plan DTO + Node DTO (§9.2 derived)
  // + create/list/get/add-task/remove-task + #287 deps/lifecycle stubs.
  ...planHandlers(),

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
  http.get('/api/projects', ({ request }) => {
    // v2.9 #298: the backend default-EXCLUDES archived; ?status=archived →
    // archived-only; ?status=all → both. Mirror that here so the active list,
    // the archived group, and the all-case are independently testable.
    const status = new URL(request.url).searchParams.get('status');
    const active = {
      id: 'proj-a',
      organization_id: 'org-test',
      name: 'Project Alpha',
      description: 'First sample project',
      status: 'active',
      created_by: 'user:hayang',
      version: 1,
      created_at: '2026-05-20T01:00:00Z',
      updated_at: '2026-05-20T01:00:00Z',
    };
    const archived = {
      id: 'proj-z',
      organization_id: 'org-test',
      name: 'Project Zeta (archived)',
      description: 'A shelved project',
      status: 'archived',
      created_by: 'user:hayang',
      version: 2,
      created_at: '2026-04-01T01:00:00Z',
      updated_at: '2026-05-01T01:00:00Z',
    };
    if (status === 'archived') return ok({ projects: [archived] });
    if (status === 'all') return ok({ projects: [active, archived] });
    return ok({ projects: [active] });
  }),
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

  // Workers (Environment fleet list). Org-scoped → also gets an /orgs/:slug
  // variant via the duplication below. Previously unhandled, which made every
  // org-route render log an MSW onUnhandledRequest error; a default empty list
  // keeps heavy full-tree renders quiet and fast.
  http.get('/api/workers', () => ok({ workers: [] })),

  // System build info (org-agnostic → exempt, bare only).
  http.get('/api/system/version', () => ok({ version: 'test', commit: 'test' })),
];

// v2.9 org-routing: the web client now path-routes org-scoped calls as
// /api/orgs/{slug}/<resource> (see withOrgSlug in api/client.ts) instead of the
// legacy ?org_slug= query. Component/integration tests that render at an
// /organizations/{slug}/* route therefore fetch the path-routed URL. To keep
// BOTH conventions working under test — org-route tests (scoped) and
// pure-unit tests that hit bare /api/* (no org slug in the jsdom URL) — every
// org-scoped handler is registered twice: once bare and once under
// /api/orgs/:slug. Exempt resource classes (auth, orgs CRUD, users, sse,
// health, system) are NEVER path-scoped, matching the backend's locked route
// table, so they keep only their bare registration.
const ORG_EXEMPT_PREFIXES = [
  '/api/auth',
  '/api/orgs',
  '/api/users',
  '/api/sse',
  '/api/health',
  '/api/system',
];

function isExemptHandlerPath(path: string): boolean {
  return ORG_EXEMPT_PREFIXES.some((p) => path === p || path.startsWith(`${p}/`));
}

function orgScopedVariant(handler: (typeof baseHandlers)[number]) {
  // `resolver` is a protected field on RequestHandler; it is present at runtime
  // and reading it lets us re-register the same resolver under the path-scoped
  // URL without duplicating ~80 handler bodies.
  const h = handler as unknown as {
    info: { method: string; path: string };
    resolver: Parameters<typeof http.get>[1];
  };
  const { method, path } = h.info;
  // path is like "/api/projects/:pid/plans" → "/api/orgs/:slug/projects/:pid/plans"
  const scopedPath = `/api/orgs/:slug${path.slice('/api'.length)}`;
  const verb = String(method).toLowerCase() as 'get' | 'post' | 'patch' | 'delete' | 'put';
  return http[verb](scopedPath, h.resolver);
}

export const handlers = [
  ...baseHandlers,
  ...baseHandlers
    .filter((h) => !isExemptHandlerPath((h.info as { path: string }).path))
    .map(orgScopedVariant),
];
