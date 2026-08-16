import { http, HttpResponse, type JsonBodyType } from 'msw';
import { teamHandlers } from './teamHandlers';

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
    // v2.9.2 (task-0543ece9): the node DTO now carries org_ref ("T<n>") so the
    // Work Board card shows the human Task id. Derive a stable default from the
    // task-id tail; a node can override via extra.
    org_ref: `T${taskId.replace(/\D/g, '') || '0'}`,
    // The underlying task's creation time — the Plan detail task list renders it
    // in a "Created" column (full local timestamp + tz).
    created_at: '2026-06-01T02:00:00Z',
    ...extra,
  });
  const basePlan = (pid: string, id: string, extra: Record<string, unknown> = {}) => ({
    id,
    project_id: pid,
    name: 'Sample plan',
    description: '',
    status: 'pending',
    creator_ref: 'user:owner',
    conversation_id: 'conv-plan-1',
    target_date: null,
    has_failed: false,
    progress: { done: 0, total: 0 },
    // Compatibility field: first-class AssignmentPool rows are no longer Plans.
    is_builtin: false,
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
            // v2.9.2 (task-0543ece9): NO cap — nodes_preview carries ALL 6 nodes so
            // the board renders the whole set (scrollable), no "…and N more".
            node_count: 6,
            nodes_preview: [
              baseNode('TS-1', { title: 'Design intake form', task_status: 'done', node_status: 'done' }),
              baseNode('TS-2', { title: 'Wire welcome email', task_status: 'running', node_status: 'running', assignee_ref: 'user:hayang' }),
              baseNode('TS-3', { title: 'Set up SSO', task_status: 'open' }),
              baseNode('TS-4', { title: 'Seed sample data', task_status: 'open' }),
              baseNode('TS-5', { title: 'Configure roles', task_status: 'open' }),
              baseNode('TS-6', { title: 'Smoke test flow', task_status: 'open' }),
            ],
          }),
          basePlan(String(params.pid), 'PL-2', {
            name: 'Billing rework',
            status: 'pending',
            progress: { done: 0, total: 0 },
            node_count: 0,
            nodes_preview: [],
          }),
        ],
      }),
    ),
    // AssignmentPool is a first-class Project child, not a synthetic Plan.
    http.get('/api/projects/:pid/assignment-pool', ({ params }) =>
      ok({
        id: `POOL-${String(params.pid)}`,
        project_id: String(params.pid),
        scheduling_class: 'background',
        auto_assign_enabled: true,
        holding_cap: 100,
        tasks: [
          {
            id: 'TS-CLAIM', project_id: String(params.pid), title: 'Claimable pool task',
            description: '', status: 'open', assignee: 'agent:builder', org_ref: 'T701',
            priority: 0, claimable: true, version: 1,
            created_at: '2026-06-01T02:00:00Z', updated_at: '2026-06-01T02:00:00Z',
          },
          {
            id: 'TS-POOL2', project_id: String(params.pid), title: 'Pending pool task',
            description: '', status: 'open', org_ref: 'T702', priority: -10,
            claimable: false, version: 1,
            created_at: '2026-06-01T02:00:00Z', updated_at: '2026-06-01T02:00:00Z',
          },
          {
            id: 'TS-DONE', project_id: String(params.pid), title: 'Done pool task',
            description: '', status: 'completed', org_ref: 'T703', priority: 0,
            claimable: false, version: 1,
            created_at: '2026-06-01T02:00:00Z', updated_at: '2026-06-01T02:00:00Z',
          },
          {
            id: 'TS-DISC', project_id: String(params.pid), title: 'Discarded pool task',
            description: '', status: 'discarded', org_ref: 'T704', priority: 0,
            claimable: false, version: 1,
            created_at: '2026-06-01T02:00:00Z', updated_at: '2026-06-01T02:00:00Z',
          },
        ],
      }),
    ),
    http.post('/api/projects/:pid/assignment-pool/tasks', () => ok({ ok: true })),
    http.delete('/api/projects/:pid/assignment-pool/tasks/:taskId', () => ok({ ok: true })),
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
    http.post('/api/projects/:pid/plans/:id/pause', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'paused', nodes: [] })),
    ),
    http.post('/api/projects/:pid/plans/:id/resume', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'running', nodes: [] })),
    ),
    http.post('/api/projects/:pid/plans/:id/discard', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'discarded', nodes: [] })),
    ),
    http.post('/api/projects/:pid/plans/:id/advance', ({ params }) =>
      ok(basePlan(String(params.pid), String(params.id), { status: 'running', nodes: [] })),
    ),
    // T981 (plan-stage-model §7) — stage-level read model. Default: no stages, so the
    // detail page renders the legacy no-stage view (a staged plan overrides this).
    http.get('/api/projects/:pid/plans/:id/stages', () => ok({ stages: [] })),
    // v2.9 Stage B (#280): DELETE /:id → { deleted: true }. Non-running only
    // (running → 409 plan_conflict on the real backend); the plan is gone after.
    http.delete('/api/projects/:pid/plans/:id', () => ok({ deleted: true })),
    // v2.9 Stage B (#290): POST /:id/archive → the archived plan detail. Cascade
    // plan→archived + ALL plan tasks→archived (task.status preserved).
    http.post('/api/projects/:pid/plans/:id/archive', ({ params }) =>
      ok(
        basePlan(String(params.pid), String(params.id), {
          status: 'done',
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
    http.get('/api/agents/availability', ({ request }) => {
      const url = new URL(request.url);
      const ids = (url.searchParams.get('ids') ?? 'A-1').split(',').filter(Boolean);
      return ok({ availability: ids.map((id) => ({ id, availability: 'available' })) });
    }),
    // v2.7 #186/#77: POST /api/agents removed; agent creation = POST /api/members/agent.
    http.get('/api/agents/:id', ({ params }) =>
      ok(baseAgent(String(params.id))),
    ),
    http.get('/api/agents/:id/concurrency', ({ params }) =>
      ok({
        agent_id: String(params.id),
        cap: 1,
        active: 0,
        queued: 0,
        running: 0,
        concurrency_enabled: false,
        slot_stable: false,
        stale: true,
        reachable: true,
        has_snapshot: false,
        snapshot_age_ms: 0,
        executors: [],
      }),
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
    http.get('/api/agents/:id/tasks', ({ params }) =>
      ok({
        tasks: [
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

function accessHandlers() {
  type Resource = { kind: string; id: string; org_id?: string; project_id?: string; label?: string };
  type BatchRequest = {
    subject_refs?: string[];
    permission_keys?: string[];
    resources?: Resource[];
    expires_at?: string | null;
    reason?: string;
    preview_request_id?: string;
  };
  type BatchItem = {
    id: string;
    subject_ref: string;
    subject_name: string;
    permission: string;
    resource: Resource;
    status: 'allowed' | 'denied' | 'unauthorized' | 'not_applicable';
    risk: 'low' | 'medium' | 'high';
    high_risk: boolean;
    reason: string;
    evidence_ref?: string;
    grant_id?: string;
  };

  const subjects = [
    { ref: 'user:hayang', kind: 'human', name: 'Hayang', role: 'owner', status: 'joined', team_names: ['agent-center core'] },
    { ref: 'user:ops', kind: 'human', name: 'Ops Reviewer', role: 'admin', status: 'joined', team_names: ['release'] },
    { ref: 'agent:builder', kind: 'agent', name: 'Builder', role: 'member', status: 'joined', team_names: ['agent-center core'] },
    { ref: 'agent:external', kind: 'agent', name: 'External Bot', role: 'member', status: 'unavailable', team_names: [] },
  ];
  const catalog = [
    {
      key: 'org.read',
      label: 'Read organization',
      description: 'Open organization-scoped resources.',
      resource_kinds: ['org'],
      actions: ['read'],
      risk: 'low',
      category: 'access',
      legacy_sources: ['org_role'],
    },
    {
      key: 'org.member.role.manage',
      label: 'Manage org roles',
      description: 'Change owner/admin/member assignments.',
      resource_kinds: ['org'],
      actions: ['manage'],
      risk: 'high',
      high_risk: true,
      category: 'access',
      legacy_sources: ['org_role'],
    },
    {
      key: 'project.write',
      label: 'Write project',
      description: 'Create and update project work items.',
      resource_kinds: ['project'],
      actions: ['write'],
      risk: 'medium',
      category: 'access',
      legacy_sources: ['project_member'],
    },
    {
      key: 'team.memory.review',
      label: 'Review team memory',
      description: 'Promote or reject team memory proposals.',
      resource_kinds: ['team'],
      actions: ['review'],
      risk: 'high',
      high_risk: true,
      category: 'access',
      legacy_sources: ['org_role', 'team_memory_policy'],
    },
    {
      key: 'file.download',
      label: 'Download files',
      description: 'Download files reachable through live scope references.',
      resource_kinds: ['file', 'task', 'issue', 'plan', 'conversation'],
      actions: ['download'],
      risk: 'medium',
      category: 'access',
      legacy_sources: ['file_scope'],
    },
  ];
  const roles = [
    {
      id: 'org:owner',
      name: 'Org owner',
      scope_kind: 'org',
      description: 'Full organization administration.',
      permissions: ['org.read', 'org.member.role.manage', 'team.memory.review'],
      editable: false,
      source: 'org_role',
      high_risk: true,
    },
    {
      id: 'org:admin',
      name: 'Org admin',
      scope_kind: 'org',
      description: 'Operational administration without owner transfer.',
      permissions: ['org.read', 'project.write'],
      editable: true,
      source: 'org_role',
    },
    {
      id: 'project:member',
      name: 'Project member',
      scope_kind: 'project',
      description: 'Project read/write membership.',
      permissions: ['project.write'],
      editable: false,
      source: 'project_member',
    },
    {
      id: 'team:curator',
      name: 'Team curator',
      scope_kind: 'team',
      description: 'Review team memory proposals when policy grants it.',
      permissions: ['team.memory.review'],
      editable: true,
      source: 'team_memory_policy',
      high_risk: true,
    },
  ];
  const customGrants = [
    {
      id: 'grant-custom-1',
      subject_ref: 'agent:builder',
      subject_name: 'Builder',
      permission: 'project.write',
      resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
      source: 'project_member',
      status: 'expires_soon',
      starts_at: '2026-08-14T00:00:00Z',
      expires_at: '2026-08-21T00:00:00Z',
      created_by: 'user:hayang',
      created_at: '2026-08-14T00:00:00Z',
      revoked_at: null,
      risk: 'medium',
    },
    {
      id: 'grant-derived-owner',
      subject_ref: 'user:hayang',
      subject_name: 'Hayang',
      permission: 'org.member.role.manage',
      resource: { kind: 'org', id: 'org-test', label: 'Test Org' },
      source: 'org_role',
      status: 'active',
      starts_at: '2026-08-14T00:00:00Z',
      expires_at: null,
      created_by: 'system',
      created_at: '2026-08-14T00:00:00Z',
      revoked_at: null,
      risk: 'high',
    },
  ];
  const baseDecisions = [
    {
      allowed: true,
      subject_ref: 'user:hayang',
      permission: 'org.read',
      resource: { kind: 'org', id: 'org-test', label: 'Test Org' },
      source: 'org_role',
      reason: 'owner role derives org.read',
      evidence_ref: 'members:mem-1',
      status: 'allowed',
      risk: 'low',
    },
    {
      allowed: true,
      subject_ref: 'user:hayang',
      permission: 'org.member.role.manage',
      resource: { kind: 'org', id: 'org-test', label: 'Test Org' },
      source: 'org_role',
      reason: 'owner role derives org.member.role.manage',
      evidence_ref: 'members:mem-1',
      status: 'allowed',
      risk: 'high',
    },
    {
      allowed: true,
      subject_ref: 'agent:builder',
      permission: 'project.write',
      resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
      source: 'project_member',
      reason: 'project membership derives project.write',
      evidence_ref: 'pm_project_members:pmem-1',
      status: 'allowed',
      expires_at: '2026-08-21T00:00:00Z',
      grant_id: 'grant-custom-1',
      risk: 'medium',
    },
    {
      allowed: false,
      subject_ref: 'agent:builder',
      permission: 'team.memory.review',
      resource: { kind: 'team', id: 'team-core', org_id: 'org-test', label: 'agent-center core' },
      source: 'team_memory_policy',
      reason: 'not a curator for this team policy',
      evidence_ref: 'team_memory_policy:team-core',
      status: 'denied',
      risk: 'high',
    },
    {
      allowed: false,
      subject_ref: 'agent:external',
      permission: 'project.write',
      resource: { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' },
      source: 'project_member',
      reason: 'subject is not a joined organization member',
      evidence_ref: 'members:missing',
      status: 'unauthorized',
      risk: 'medium',
    },
    {
      allowed: false,
      subject_ref: 'user:ops',
      permission: 'file.download',
      resource: { kind: 'team', id: 'team-core', org_id: 'org-test', label: 'agent-center core' },
      source: 'file_scope',
      reason: 'file.download does not apply to team resources',
      evidence_ref: 'permission_registry:file.download',
      status: 'not_applicable',
      risk: 'medium',
    },
  ];
  const summaryFor = (decisions: typeof baseDecisions) => ({
    allowed: decisions.filter((d) => d.status === 'allowed').length,
    high_risk: decisions.filter((d) => d.risk === 'high').length,
    expiring: customGrants.filter((g) => g.status === 'expires_soon').length,
    denied: decisions.filter((d) => d.status === 'denied' || d.status === 'unauthorized').length,
    not_applicable: decisions.filter((d) => d.status === 'not_applicable').length,
  });
  const findSubject = (ref: string) => subjects.find((s) => s.ref === ref);
  const findPermission = (key: string) => catalog.find((p) => p.key === key);
  const makeItems = (body: BatchRequest): BatchItem[] => {
    const items: BatchItem[] = [];
    for (const subjectRef of body.subject_refs ?? []) {
      for (const permission of body.permission_keys ?? []) {
        for (const resource of body.resources ?? []) {
          const subject = findSubject(subjectRef);
          const def = findPermission(permission);
          let status: BatchItem['status'] = 'allowed';
          let reason = 'grant can be applied by the permission API';
          if (!subject || subject.status === 'unavailable') {
            status = 'unauthorized';
            reason = 'subject is unavailable or outside this organization';
          } else if (!def?.resource_kinds.includes(resource.kind)) {
            status = 'not_applicable';
            reason = `${permission} does not apply to ${resource.kind}`;
          } else if (permission === 'org.member.role.manage' && subject.kind === 'agent') {
            status = 'unauthorized';
            reason = 'agents cannot receive organization role-management grants';
          }
          items.push({
            id: `item-${items.length + 1}`,
            subject_ref: subjectRef,
            subject_name: subject?.name ?? subjectRef,
            permission,
            resource,
            status,
            risk: (def?.risk ?? 'medium') as BatchItem['risk'],
            high_risk: def?.risk === 'high',
            reason,
            evidence_ref: status === 'allowed' ? 'permission_preview:mock' : undefined,
            grant_id: status === 'allowed' ? `grant-new-${items.length + 1}` : undefined,
          });
        }
      }
    }
    return items;
  };
  return [
    http.get('/api/permissions/effective', ({ request }) => {
      const url = new URL(request.url);
      const q = (url.searchParams.get('q') ?? '').toLowerCase();
      const risk = url.searchParams.get('risk');
      const status = url.searchParams.get('status');
      const filtered = baseDecisions.filter((d) => {
        const subject = findSubject(d.subject_ref);
        if (q && !`${subject?.name ?? ''} ${d.subject_ref} ${d.permission} ${d.reason}`.toLowerCase().includes(q)) return false;
        if (risk && risk !== 'all' && d.risk !== risk) return false;
        if (status && status !== 'all' && d.status !== status) return false;
        return true;
      });
      return ok({
        generated_at: '2026-08-14T08:00:00Z',
        subjects,
        roles,
        catalog,
        decisions: filtered,
        grants: customGrants,
        summary: summaryFor(filtered),
      });
    }),
    http.post('/api/permissions/batch/preview', async ({ request }) => {
      const body = (await request.json()) as BatchRequest;
      const items = makeItems(body);
      return ok({
        request_id: 'preview-mock-1',
        expires_at: body.expires_at ?? null,
        items,
        summary: {
          total: items.length,
          grantable: items.filter((i) => i.status === 'allowed').length,
          high_risk: items.filter((i) => i.high_risk).length,
          unauthorized: items.filter((i) => i.status === 'unauthorized').length,
          not_applicable: items.filter((i) => i.status === 'not_applicable').length,
        },
      });
    }),
    http.post('/api/permissions/batch/apply', async ({ request }) => {
      const body = (await request.json()) as BatchRequest;
      const items: BatchItem[] = makeItems(body).map((item, idx) =>
        idx === 0 && item.status === 'allowed'
          ? { ...item, status: 'denied', reason: 'write conflict: grant already changed' }
          : item,
      );
      const failed = items.filter((i) => i.status !== 'allowed').length;
      return ok({
        operation_id: 'access-op-1',
        applied_at: '2026-08-14T08:01:00Z',
        items,
        summary: {
          total: items.length,
          succeeded: items.filter((i) => i.status === 'allowed').length,
          failed,
          unauthorized: items.filter((i) => i.status === 'unauthorized').length,
          not_applicable: items.filter((i) => i.status === 'not_applicable').length,
          partial_failure: failed > 0,
        },
      });
    }),
    http.post('/api/permissions/batch/revoke', async ({ request }) => {
      const body = (await request.json()) as { grant_ids?: string[]; reason?: string };
      const items: BatchItem[] = (body.grant_ids ?? []).map((id, idx) => ({
        id: `revoke-${idx + 1}`,
        subject_ref: idx === 0 ? 'agent:builder' : 'user:ops',
        subject_name: idx === 0 ? 'Builder' : 'Ops Reviewer',
        permission: idx === 0 ? 'project.write' : 'org.read',
        resource: idx === 0
          ? { kind: 'project', id: 'proj-a', org_id: 'org-test', label: 'Project Alpha' }
          : { kind: 'org', id: 'org-test', label: 'Test Org' },
        status: 'not_applicable',
        risk: idx === 0 ? 'medium' : 'low',
        high_risk: false,
        reason: `${id} is a derived permission and must be revoked at its source`,
        grant_id: id,
      }));
      const failed = items.filter((i) => i.status !== 'allowed').length;
      return ok({
        operation_id: 'access-revoke-1',
        applied_at: '2026-08-14T08:02:00Z',
        items,
        summary: {
          total: items.length,
          succeeded: items.filter((i) => i.status === 'allowed').length,
          failed,
          unauthorized: 0,
          not_applicable: items.filter((i) => i.status === 'not_applicable').length,
          partial_failure: failed > 0,
        },
      });
    }),
    http.patch('/api/permissions/roles/:id', async ({ params, request }) => {
      const body = (await request.json()) as { permissions?: string[]; reason?: string };
      const role = roles.find((r) => r.id === String(params.id)) ?? roles[1];
      return ok({ ...role, permissions: body.permissions ?? role.permissions });
    }),
  ];
}

function aiRuntimeCatalog() {
  return {
    org_id: 'org-test',
    revision: 3,
    clis: [
      {
        id: 'runtime-cli-claude-code',
        key: 'claude-code',
        display_name: 'Claude Code',
        executable: 'claude',
        version_constraint: '>=1.0.0',
        required_features: ['workspace'],
        parameter_schema: {},
        enabled: true,
        system: true,
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
      {
        id: 'runtime-cli-codex',
        key: 'codex',
        display_name: 'Codex CLI',
        executable: 'codex',
        version_constraint: '>=0.1.0',
        required_features: ['workspace'],
        parameter_schema: {},
        enabled: true,
        system: true,
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
    ],
    models: [
      {
        id: 'runtime-model-gpt-5',
        key: 'gpt-5',
        model_key: 'gpt-5',
        display_name: 'GPT-5',
        compatible_cli_keys: ['codex'],
        default_parameters: {},
        enabled: true,
        context_window: 400000,
        input_cost_per_mtok: 1.25,
        output_cost_per_mtok: 10,
        tier: 'frontier',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
      {
        id: 'runtime-model-claude-opus-4-8',
        key: 'claude-opus-4-8',
        model_key: 'claude-opus-4-8',
        display_name: 'Claude Opus 4.8',
        compatible_cli_keys: ['claude-code'],
        default_parameters: {},
        enabled: true,
        context_window: 200000,
        input_cost_per_mtok: 15,
        output_cost_per_mtok: 75,
        tier: 'frontier',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
      {
        id: 'runtime-model-claude-sonnet-4-6',
        key: 'claude-sonnet-4-6',
        model_key: 'claude-sonnet-4-6',
        display_name: 'Claude Sonnet 4.6',
        compatible_cli_keys: ['claude-code'],
        default_parameters: {},
        enabled: true,
        context_window: 200000,
        input_cost_per_mtok: 3,
        output_cost_per_mtok: 15,
        tier: 'standard',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
      {
        id: 'runtime-model-sonnet-5',
        key: 'sonnet-5',
        model_key: 'sonnet-5',
        display_name: 'Sonnet 5',
        compatible_cli_keys: ['claude-code'],
        default_parameters: {},
        enabled: true,
        context_window: 200000,
        input_cost_per_mtok: 3,
        output_cost_per_mtok: 15,
        tier: 'standard',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
      {
        id: 'runtime-model-gpt-5-5',
        key: 'gpt-5.5',
        model_key: 'gpt-5.5',
        display_name: 'GPT-5.5',
        compatible_cli_keys: ['codex'],
        default_parameters: {},
        enabled: true,
        context_window: 400000,
        input_cost_per_mtok: 1.5,
        output_cost_per_mtok: 12,
        tier: 'frontier',
        created_at: '2026-07-01T00:00:00Z',
        updated_at: '2026-07-01T00:00:00Z',
      },
    ],
  };
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
        // creator_ref → the project Tasks tab Creator column resolves it to a name
        // (degrades to a clean handle); plan_id → the Plan column shows the id too.
        creator_ref: 'user:owner',
        plan_id: 'plan-01KT8DPLAN',
        plan_name: 'Onboarding flow',
        created_at: '2026-05-24T01:00:00Z',
        updated_at: '2026-05-24T01:00:00Z',
      },
    ];
    if (unplanned === '1') {
      // Backlog: distinct unplanned task with an assignee so the avatar renders.
      // ADR-0047: a completed + a discarded unplanned task are also returned so
      // tests prove the FE HIDES terminal work in the Backlog (count stays 1).
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
          {
            id: 'TS-BL-DONE',
            project_id: String(params.pid),
            title: 'completed backlog task',
            description: '',
            status: 'completed',
            version: 1,
            created_at: '2026-05-24T01:00:00Z',
            updated_at: '2026-05-24T01:00:00Z',
          },
          {
            id: 'TS-BL-DISC',
            project_id: String(params.pid),
            title: 'discarded backlog task',
            description: '',
            status: 'discarded',
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
  http.get('/api/plans', () =>
    ok({
      items: [
        {
          id: 'PL-1',
          project_id: 'proj-a',
          project: { id: 'proj-a', name: 'Project Alpha' },
          name: 'Onboarding flow',
          description: '',
          status: 'running',
          org_ref: 'P1',
          creator_ref: 'user:owner',
          conversation_id: 'conv-plan-1',
          has_failed: false,
          progress: { done: 2, total: 5 },
          node_count: 5,
          created_at: '2026-06-01T01:00:00Z',
          updated_at: '2026-06-04T02:00:00Z',
        },
      ],
      total: 1,
    }),
  ),

  // Agents — Agent BC (v2.7 #101). Org-scoped, wrapped list shape, lifecycle
  // sub-routes + work-items / activity.
  ...agentHandlers(),

  // Team WebUI Phase-1 facade (teams CRUD + members + projects) — backed by the
  // teamsFixtures store; see teamHandlers.ts.
  ...teamHandlers(),

  // Unified permission / Access module contract.
  ...accessHandlers(),

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
    ok([{ id: 'org-test', slug: 'test', name: 'Test Org', role: 'owner', created_at: '2026-01-01T00:00:00Z' }]),
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
      tasks: [],
      workers: [],
      pending_issues: [],
      generated_at: '2026-05-24T01:00:00Z',
    }),
  ),

  // I23 (T332): cross-source unread-conversations digest. Default empty so the
  // shell nav (which always mounts UnreadConversationsSection) doesn't trip
  // onUnhandledRequest:'error'; tests that exercise the region override this.
  http.get('/api/unread-conversations', () => ok([])),

  // v2.26.0 I61: "Needs your attention" unified panel source. Default empty so the
  // shell (which always mounts the Alerts rail) doesn't trip
  // onUnhandledRequest:'error'; tests that exercise the panel override this.
  http.get('/api/attention', () => ok({ items: [] })),

  // AI Runtime catalog — org-level runtime settings. Readable by organization
  // members; write paths are owner/admin-only in the real backend. The mock keeps
  // the same response envelope so routing/page tests can render the delivered UI.
  http.get('/api/ai-runtime', () => ok(aiRuntimeCatalog())),
  http.get('/api/ai-runtime/export', () => new HttpResponse('kind: agent-center-ai-runtime\n', {
    status: 200,
    headers: { 'Content-Type': 'application/yaml; charset=utf-8' },
  })),
  http.post('/api/ai-runtime/models', async ({ request }) => {
    const body = (await request.json()) as { expected_revision?: number; value?: Record<string, unknown> };
    const key = typeof body.value?.key === 'string' ? body.value.key : 'new-model';
    return ok({ revision: (body.expected_revision ?? 3) + 1, entry: { id: `runtime-model-${key}`, ...(body.value ?? {}) } }, 201);
  }),
  http.patch('/api/ai-runtime/models/:id', async ({ params, request }) => {
    const body = (await request.json()) as { expected_revision?: number; value?: Record<string, unknown> };
    return ok({ revision: (body.expected_revision ?? 3) + 1, entry: { id: String(params.id), ...(body.value ?? {}) } });
  }),
  http.delete('/api/ai-runtime/models/:id', async ({ request }) => {
    const body = (await request.json()) as { expected_revision?: number };
    return ok({ revision: (body.expected_revision ?? 3) + 1 });
  }),
  http.post('/api/ai-runtime/clis', async ({ request }) => {
    const body = (await request.json()) as { expected_revision?: number; value?: Record<string, unknown> };
    const key = typeof body.value?.key === 'string' ? body.value.key : 'new-cli';
    return ok({ revision: (body.expected_revision ?? 3) + 1, entry: { id: `runtime-cli-${key}`, ...(body.value ?? {}) } }, 201);
  }),
  http.patch('/api/ai-runtime/clis/:id', async ({ params, request }) => {
    const body = (await request.json()) as { expected_revision?: number; value?: Record<string, unknown> };
    return ok({ revision: (body.expected_revision ?? 3) + 1, entry: { id: String(params.id), ...(body.value ?? {}) } });
  }),
  http.delete('/api/ai-runtime/clis/:id', async ({ request }) => {
    const body = (await request.json()) as { expected_revision?: number };
    return ok({ revision: (body.expected_revision ?? 3) + 1 });
  }),
  http.post('/api/ai-runtime/import/preview', async ({ request }) => {
    const body = (await request.json()) as {
      document?: { runtime?: { clis?: Array<{ key?: string }>; models?: Array<{ key?: string }> } };
    };
    const runtime = body.document?.runtime ?? {};
    const items = [
      ...(runtime.clis ?? []).map((cli) => ({ entity_type: 'cli', key: cli.key ?? '', action: 'unchanged' })),
      ...(runtime.models ?? []).map((model) => ({ entity_type: 'model', key: model.key ?? '', action: 'update' })),
    ];
    return ok({
      report: { dry_run: true, applied: false, revision: 3, items, diagnostics: [] },
      validation_token: 'mock-runtime-import-token',
      expires_at: '2026-08-08T00:00:00Z',
      document_sha256: 'mock-runtime-import-sha',
    });
  }),
  http.post('/api/ai-runtime/import/apply', async ({ request }) => {
    const body = (await request.json()) as {
      document?: { runtime?: { clis?: Array<{ key?: string }>; models?: Array<{ key?: string }> } };
    };
    const runtime = body.document?.runtime ?? {};
    const items = [
      ...(runtime.clis ?? []).map((cli) => ({ entity_type: 'cli', key: cli.key ?? '', action: 'unchanged' })),
      ...(runtime.models ?? []).map((model) => ({ entity_type: 'model', key: model.key ?? '', action: 'update' })),
    ];
    return ok({ dry_run: false, applied: true, revision: 4, items, diagnostics: [] });
  }),

  // Unified authorization service. Detail pages request these only on the
  // Permissions tab; focused tests override the exact shapes they need.
  http.get('/api/permissions/definitions', () =>
    ok({
      definitions: [
        {
          key: 'org.read',
          category: 'access',
          resource_kinds: ['org'],
          actions: ['read'],
          legacy_sources: ['members'],
        },
        {
          key: 'org.member.list',
          category: 'access',
          resource_kinds: ['org'],
          actions: ['list'],
          legacy_sources: ['members'],
        },
      ],
    }),
  ),
  http.get('/api/permissions/effective', ({ request }) => {
    const url = new URL(request.url);
    const subject = url.searchParams.get('subject_ref') ?? 'user:test';
    const kind = url.searchParams.get('resource_kind') ?? 'org';
    const id = url.searchParams.get('resource_id') ?? 'org-1';
    return ok({
      subject_ref: subject,
      resource: { kind, id, org_id: kind === 'org' ? id : 'org-1' },
      permissions: [],
    });
  }),
  http.post('/api/permissions/explain', async ({ request }) => {
    const body = (await request.json()) as {
      subject_ref?: string;
      permission?: string;
      resource?: Record<string, unknown>;
    };
    return ok({
      decision: {
        allowed: false,
        subject_ref: body.subject_ref ?? '',
        permission: body.permission ?? '',
        resource: body.resource ?? { kind: 'org', id: 'org-1' },
        reason: 'permission_denied',
      },
      effective: [],
      denied_by: [],
    });
  }),
  http.get('/api/permissions/audit', () => ok({ events: [] })),
  http.post('/api/permissions/batch/apply', () =>
    ok({ preview: false, operations: [{ id: 'assignment', type: 'assign_role', status: 'created', assignment_id: 'asgn-mock' }] }),
  ),
  http.post('/api/permissions/batch/revoke', () =>
    ok({ preview: false, operations: [{ id: 'revoke', type: 'revoke_assignment', status: 'revoked', assignment_id: 'asgn-mock' }] }),
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
  // I7-D3 wake-guardrail thresholds (effective config); PUT echoes the body.
  http.get('/api/system/wake-guardrail', () =>
    ok({ max_depth: 4, cycle_window_sec: 300, cycle_threshold: 3, rate_per_min: 10, chain_token_budget: 16 }),
  ),
  http.put('/api/system/wake-guardrail', async ({ request }) => ok(await request.json())),

  // 变更记录 / audit-trail (change-log §6): the object-level change ledger read
  // endpoints. Default to an empty ledger so the detail-view sidebars/tab (which
  // now embed ObjectAuditTimeline) don't trip onUnhandledRequest:'error' on a
  // full-tree render; tests that assert real entries override these with server.use.
  http.get('/api/projects/:pid/issues/:id/audit', () => ok({ entries: [], next_cursor: '' })),
  http.get('/api/projects/:pid/tasks/:id/audit', () => ok({ entries: [], next_cursor: '' })),
  http.get('/api/projects/:pid/plans/:id/audit', () => ok({ entries: [], next_cursor: '' })),
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
