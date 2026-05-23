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

  // Derivation
  http.post('/api/issues', () => ok({ conversation_id: 'I-1', event_id: 'E-i' }, 201)),
  http.post('/api/tasks', () => ok({ conversation_id: 'T-1', event_id: 'E-t' }, 201)),

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
  http.post('/api/secrets', () =>
    ok(
      {
        id: 'S-NEW',
        name: 'new',
        kind: 'other',
        state: 'active',
        created_at: '2026-05-24T01:00:00Z',
        created_by: 'user:hayang',
      },
      201,
    ),
  ),
  http.delete('/api/secrets/:id', () => ok({ event_id: 'E-rev' })),

  // Fleet + trace
  http.get('/api/fleet', () =>
    ok({ executions: [], workers: [], open_input_requests: [], pending_issues: [] }),
  ),
  http.get('/api/tasks/:id/trace', () => ok([])),
];
