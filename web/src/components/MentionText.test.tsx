import type React from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { MarkdownMessage } from './MarkdownMessage';
import { SenderSidebarProvider } from './SenderSidebarContext';
import { OrgContext } from '@/OrgContext';

// #281 entry ②: @mention tokens in message content open the existing kind-routed
// SenderDetailSidebar. Mentions weren't previously distinct tokens — this asserts
// the new detection: an @handle that resolves to a known member becomes a
// clickable, keyboard-accessible token routed to the right detail body.
function renderInProvider(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SenderSidebarProvider>{ui}</SenderSidebarProvider>
    </QueryClientProvider>,
  );
}

// members: one agent (Bot One / bot-1) + one human (Alice / alice). Used to
// resolve @mention handles → prefixed identity refs for kind routing.
function mockMembers() {
  server.use(
    http.get('/api/members', () =>
      HttpResponse.json([
        { id: 'm1', organization_id: 'O', identity_id: 'agent:bot-1', display_name: 'Bot One', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
        { id: 'm2', organization_id: 'O', identity_id: 'user:alice', display_name: 'Alice', kind: 'user', role: 'member', status: 'joined', joined_at: 'x' },
      ]),
    ),
  );
}

describe('MentionText (#281 entry ②)', () => {
  afterEach(() => cleanup());

  it('renders a known @agent mention as a clickable token; click opens the agent body', async () => {
    mockMembers();
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O', name: 'Bot One', description: '',
          model: 'claude-opus', cli: 'claudecode', env_vars: {}, skills: [], worker_id: 'w-1',
          lifecycle: 'running', availability: 'available', created_by: 'user:hayang',
          version: 1, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z',
        }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'hey @bot-1 please check this'} />);
    // mention is tokenized once the members query resolves.
    const token = await screen.findByTestId('mention-token');
    expect(token.tagName).toBe('BUTTON');
    expect(token).toHaveTextContent('@bot-1');
    // a11y: aria-label present.
    expect(token).toHaveAttribute('aria-label', 'View bot-1 details');
    // kind-routing: ref carries the agent: prefix.
    expect(token).toHaveAttribute('data-mention-ref', 'agent:bot-1');
    expect(screen.queryByTestId('sender-sidebar')).toBeNull();
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    // agent ref → AgentDetailBody.
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-agent')).toBeInTheDocument());
  });

  it('resolves an @agent by display name too (case/space-insensitive)', async () => {
    mockMembers();
    server.use(
      http.get('/api/agents/:id', ({ params }) =>
        HttpResponse.json({
          id: String(params.id), organization_id: 'O', name: 'Bot One', description: '',
          model: 'm', cli: 'c', env_vars: {}, skills: [], worker_id: 'w', lifecycle: 'running',
          availability: 'available', created_by: 'user:hayang', version: 1,
          created_at: 'x', updated_at: 'x',
        }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'cc @BotOne'} />);
    const token = await screen.findByTestId('mention-token');
    expect(token).toHaveAttribute('data-mention-ref', 'agent:bot-1');
  });

  it('a @human mention opens the user body (UserDetailBody) — kind routing', async () => {
    mockMembers();
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({
          user_id: String(params.id), display_name: 'Alice', email: 'alice@example.com',
          created_at: 'x', orgs: [],
        }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'thanks @alice'} />);
    const token = await screen.findByTestId('mention-token');
    expect(token).toHaveAttribute('data-mention-ref', 'user:alice');
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    // user ref → UserDetailBody, NOT the agent body.
    await waitFor(() => expect(screen.getByTestId('sender-sidebar-user')).toBeInTheDocument());
    expect(screen.queryByTestId('sender-sidebar-agent')).toBeNull();
  });

  it('keyboard activation (Enter on the native button) opens the sidebar', async () => {
    mockMembers();
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({ user_id: String(params.id), display_name: 'Alice', created_at: 'x', orgs: [] }),
      ),
    );
    renderInProvider(<MarkdownMessage content={'ping @alice'} />);
    const token = await screen.findByTestId('mention-token');
    token.focus();
    fireEvent.keyDown(token, { key: 'Enter' });
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
  });

  it('a mention click does NOT bubble to an outer (message-row) handler', async () => {
    mockMembers();
    server.use(
      http.get('/api/users/:id', ({ params }) =>
        HttpResponse.json({ user_id: String(params.id), display_name: 'Alice', created_at: 'x', orgs: [] }),
      ),
    );
    const outer = vi.fn();
    render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <SenderSidebarProvider>
          <div
            data-testid="outer-row"
            role="button"
            tabIndex={0}
            onClick={outer}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ' ') outer();
            }}
          >
            <MarkdownMessage content={'hi @alice'} />
          </div>
        </SenderSidebarProvider>
      </QueryClientProvider>,
    );
    const token = await screen.findByTestId('mention-token');
    fireEvent.click(token);
    await waitFor(() => expect(screen.getByTestId('sender-sidebar')).toBeInTheDocument());
    // stopPropagation: the outer row handler must NOT have fired.
    expect(outer).not.toHaveBeenCalled();
  });

  it('leaves an UNKNOWN @handle as plain text (no token, no wrong-kind body)', async () => {
    mockMembers();
    renderInProvider(<MarkdownMessage content={'hello @nobody here'} />);
    // members resolve, but @nobody matches no member → stays plain text.
    await waitFor(() => expect(screen.getByTestId('markdown-message')).toHaveTextContent('hello @nobody here'));
    expect(screen.queryByTestId('mention-token')).toBeNull();
  });
});

// v2.9.2 (task-82915d7c): a `task-<id>` reference in message content linkifies to
// the task detail page, labelled with the human Task id ("T123", org_ref). The
// resolver reads the org task list; an unknown / out-of-org id stays plain text.
function renderInOrg(ui: React.ReactElement, slug = 'test-org') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug, orgId: 'O', orgName: 'Test Org' }}>
        <SenderSidebarProvider>{ui}</SenderSidebarProvider>
      </OrgContext.Provider>
    </QueryClientProvider>,
  );
}

// One known org task (task-abc → T123 in proj-x). Mentions resolve to no member
// (empty list) — task-only content still needs the members handler since the
// markdown mention pipeline loads it.
function mockOrgTasks() {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    // T99: the plan-ref resolver also fetches the org plan list — default empty
    // so task-ref tests don't trip an unhandled request.
    http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
    // the issue-ref resolver also fetches the org issue list — default empty.
    http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/tasks', () =>
      HttpResponse.json({
        items: [
          {
            id: 'task-abc', org_ref: 'T123', project: { id: 'proj-x', name: 'Project X' },
            title: 'Wire the thing', status: 'open', assignee: null,
            updated_at: 'x', created_at: 'x',
          },
          {
            // a task WITHOUT an org_ref → label falls back to the FULL id (T126).
            id: 'task-noref', project: { id: 'proj-x', name: 'Project X' },
            title: 'No ref task', status: 'open', assignee: null,
            updated_at: 'x', created_at: 'x',
          },
        ],
        total: 2,
      }),
    ),
  );
}

describe('MentionText task-ref linkify (task-82915d7c)', () => {
  afterEach(() => cleanup());

  it('renders a known task-<id> as a T123 link to the task detail page (new tab)', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'please see task-abc for context'} />);
    const link = await screen.findByTestId('task-ref-token');
    expect(link.tagName).toBe('A');
    // label = org_ref, NOT the raw task id.
    expect(link).toHaveTextContent('T123');
    expect(link).not.toHaveTextContent('task-abc');
    // org-prefixed task detail route, opened in a new tab with opener guards.
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-abc');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(link).toHaveAttribute('data-task-id', 'task-abc');
  });

  it('falls back to the FULL id (never a #id-tail hash) when the task has no org_ref (T126)', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'blocked on task-noref now'} />);
    const link = await screen.findByTestId('task-ref-token');
    expect(link).toHaveTextContent('task-noref');
    expect(link).not.toHaveTextContent('#');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-noref');
  });

  it('leaves an UNKNOWN / out-of-org task-<id> as plain text (no dangling link)', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'ref task-zzz does not exist'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('ref task-zzz does not exist'),
    );
    expect(screen.queryByTestId('task-ref-token')).toBeNull();
  });

  it('does NOT linkify task- embedded in a larger word (subtask-abc)', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'this subtask-abc is not a ref'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('this subtask-abc is not a ref'),
    );
    // the negative lookbehind prevents matching task-abc inside subtask-abc.
    expect(screen.queryByTestId('task-ref-token')).toBeNull();
  });

  it('linkifies a mention AND a task-ref in the same line', async () => {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'm2', organization_id: 'O', identity_id: 'user:alice', display_name: 'Alice', kind: 'user', role: 'member', status: 'joined', joined_at: 'x' },
        ]),
      ),
      http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [{ id: 'task-abc', org_ref: 'T123', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
    );
    renderInOrg(<MarkdownMessage content={'@alice please take task-abc'} />);
    expect(await screen.findByTestId('mention-token')).toHaveTextContent('@alice');
    expect(await screen.findByTestId('task-ref-token')).toHaveTextContent('T123');
  });

  it('does NOT linkify a task-ref inside a fenced code block', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'```\nrun task-abc here\n```'} />);
    // code fences render through CollapsibleCodeBlock, never the linkify pipeline.
    await waitFor(() => expect(screen.getByTestId('markdown-message')).toBeInTheDocument());
    expect(screen.queryByTestId('task-ref-token')).toBeNull();
  });
});

// T76 (task-c780999a): a `T<number>` org_ref (e.g. T123) in a message linkifies
// to the SAME task detail page as the bare task-<id>, resolving through the org
// task list (status=all, so terminal tasks resolve too — T62/task-336335c5).
describe('MentionText T-number org_ref linkify (T76 / task-c780999a)', () => {
  afterEach(() => cleanup());

  it('linkifies a T<number> org_ref to its task detail page', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'please review T123 before merge'} />);
    const link = await screen.findByTestId('task-ref-token');
    expect(link.tagName).toBe('A');
    expect(link).toHaveTextContent('T123');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-abc');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('resolves a COMPLETED task by T-number (status=all) — and requests all statuses', async () => {
    // T62 root cause: the resolver must ask the backend to include terminal tasks.
    let requestedStatus: string[] = [];
    server.use(
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/tasks', ({ request }) => {
        requestedStatus = new URL(request.url).searchParams.getAll('status');
        return HttpResponse.json({
          items: [
            {
              id: 'task-done', org_ref: 'T99', project: { id: 'proj-x', name: 'X' },
              title: 'finished', status: 'completed', assignee: null, updated_at: 'x', created_at: 'x',
            },
          ],
          total: 1,
        });
      }),
    );
    renderInOrg(<MarkdownMessage content={'fixed in T99, see notes'} />);
    const link = await screen.findByTestId('task-ref-token');
    expect(link).toHaveTextContent('T99');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-done');
    expect(requestedStatus).toContain('all');
  });

  it('leaves an UNKNOWN / unresolvable T-number as plain text (no dangling link)', async () => {
    mockOrgTasks(); // only T123 exists
    renderInOrg(<MarkdownMessage content={'ticket T999 is not a real task'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('ticket T999 is not a real task'),
    );
    expect(screen.queryByTestId('task-ref-token')).toBeNull();
  });

  it('does NOT match a T-number embedded in a larger token (word boundary)', async () => {
    mockOrgTasks();
    // "PART123" (left-adjacent letters), "T12ab" (right-adjacent letters) — neither
    // is a standalone T-number, so the boundary guards prevent a (wrong) match.
    renderInOrg(<MarkdownMessage content={'PART123 and T12ab are not refs'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('PART123 and T12ab are not refs'),
    );
    expect(screen.queryByTestId('task-ref-token')).toBeNull();
  });

  it('linkifies BOTH a task-<id> and a T-number in the same line', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'see task-abc which is the same as T123'} />);
    const links = await screen.findAllByTestId('task-ref-token');
    expect(links).toHaveLength(2);
    // both point at the same task detail page (task-abc / T123 are the same task).
    expect(links[0]).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-abc');
    expect(links[1]).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-abc');
  });
});

// v2.10.1 [T99]: a `plan-<id>` or `P<number>` reference in a message linkifies to
// the plan detail page, labelled with the human Plan id ("P123", org_ref) —
// symmetric with the task-ref / T-number linkify above. Resolves through the org
// plan list; an unknown / out-of-org reference stays plain text. Code spans are
// excluded by the same pipeline.
function mockOrgPlans() {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/tasks', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/plans', () =>
      HttpResponse.json({
        items: [
          {
            id: 'plan-abc', org_ref: 'P42', project: { id: 'proj-x', name: 'Project X' },
            name: 'Ship it', status: 'running', has_failed: false,
            progress: { done: 1, total: 3 }, created_at: 'x', updated_at: 'x',
          },
          {
            // a plan WITHOUT an org_ref → label falls back to the FULL id (T126).
            id: 'plan-noref', project: { id: 'proj-x', name: 'Project X' },
            name: 'No ref plan', status: 'draft', has_failed: false,
            progress: { done: 0, total: 0 }, created_at: 'x', updated_at: 'x',
          },
        ],
        total: 2,
      }),
    ),
  );
}

describe('MentionText plan-ref linkify (T99)', () => {
  afterEach(() => cleanup());

  it('renders a known plan-<id> as a P42 link to the plan detail page (new tab)', async () => {
    mockOrgPlans();
    renderInOrg(<MarkdownMessage content={'tracked in plan-abc this week'} />);
    const link = await screen.findByTestId('plan-ref-token');
    expect(link.tagName).toBe('A');
    expect(link).toHaveTextContent('P42');
    expect(link).not.toHaveTextContent('plan-abc');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/plans/plan-abc');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(link).toHaveAttribute('data-plan-id', 'plan-abc');
  });

  it('linkifies a P<number> org_ref to its plan detail page', async () => {
    mockOrgPlans();
    renderInOrg(<MarkdownMessage content={'see P42 for the rollout'} />);
    const link = await screen.findByTestId('plan-ref-token');
    expect(link).toHaveTextContent('P42');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/plans/plan-abc');
  });

  it('falls back to the FULL id (never a #id-tail hash) when the plan has no org_ref (T126)', async () => {
    mockOrgPlans();
    renderInOrg(<MarkdownMessage content={'blocked on plan-noref now'} />);
    const link = await screen.findByTestId('plan-ref-token');
    expect(link).toHaveTextContent('plan-noref');
    expect(link).not.toHaveTextContent('#');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/plans/plan-noref');
  });

  it('leaves an UNKNOWN plan-<id> / P-number as plain text (no dangling link)', async () => {
    mockOrgPlans();
    renderInOrg(<MarkdownMessage content={'ref plan-zzz and P999 do not exist'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('ref plan-zzz and P999 do not exist'),
    );
    expect(screen.queryByTestId('plan-ref-token')).toBeNull();
  });

  it('does NOT match plan- embedded in a larger word, nor a P-number mid-token', async () => {
    mockOrgPlans();
    renderInOrg(<MarkdownMessage content={'a subplan-abc and PART42 and P42x are not refs'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('a subplan-abc and PART42 and P42x are not refs'),
    );
    expect(screen.queryByTestId('plan-ref-token')).toBeNull();
  });

  it('does NOT linkify a plan-ref inside a fenced code block', async () => {
    mockOrgPlans();
    renderInOrg(<MarkdownMessage content={'```\ndeploy plan-abc here\n```'} />);
    await waitFor(() => expect(screen.getByTestId('markdown-message')).toBeInTheDocument());
    expect(screen.queryByTestId('plan-ref-token')).toBeNull();
  });

  it('linkifies a task-ref AND a plan-ref in the same line', async () => {
    server.use(
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [{ id: 'task-abc', org_ref: 'T123', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
      http.get('/api/plans', () =>
        HttpResponse.json({
          items: [{ id: 'plan-abc', org_ref: 'P42', project: { id: 'proj-x', name: 'X' }, name: 'p', status: 'running', has_failed: false, progress: { done: 0, total: 0 }, created_at: 'x', updated_at: 'x' }],
          total: 1,
        }),
      ),
    );
    renderInOrg(<MarkdownMessage content={'T123 lands under P42'} />);
    expect(await screen.findByTestId('task-ref-token')).toHaveTextContent('T123');
    expect(await screen.findByTestId('plan-ref-token')).toHaveTextContent('P42');
  });
});

// an `issue-<id>` or `I<number>` reference in a message linkifies to the issue
// detail page, labelled with the human Issue id ("I123", org_ref) — symmetric
// with the task-ref / plan-ref linkify above. Resolves through the org issue list
// (status=all so closed issues resolve too); an unknown / out-of-org reference
// stays plain text. Code spans are excluded by the same pipeline.
function mockOrgIssues() {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/tasks', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/issues', () =>
      HttpResponse.json({
        items: [
          {
            id: 'issue-abc', org_ref: 'I7', project: { id: 'proj-x', name: 'Project X' },
            title: 'A nasty bug', status: 'open', assignee: null,
            updated_at: 'x', created_at: 'x',
          },
          {
            // an issue WITHOUT an org_ref → label falls back to the FULL id (T126).
            id: 'issue-noref', project: { id: 'proj-x', name: 'Project X' },
            title: 'No ref issue', status: 'open', assignee: null,
            updated_at: 'x', created_at: 'x',
          },
        ],
        total: 2,
      }),
    ),
  );
}

describe('MentionText issue-ref linkify (issue-<id> / I<number>)', () => {
  afterEach(() => cleanup());

  it('renders a known issue-<id> as an I7 link to the issue detail page (new tab)', async () => {
    mockOrgIssues();
    renderInOrg(<MarkdownMessage content={'see issue-abc for the repro'} />);
    const link = await screen.findByTestId('issue-ref-token');
    expect(link.tagName).toBe('A');
    expect(link).toHaveTextContent('I7');
    expect(link).not.toHaveTextContent('issue-abc');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/issues/issue-abc');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(link).toHaveAttribute('data-issue-id', 'issue-abc');
  });

  it('linkifies an I<number> org_ref to its issue detail page', async () => {
    mockOrgIssues();
    renderInOrg(<MarkdownMessage content={'fixed in I7, see notes'} />);
    const link = await screen.findByTestId('issue-ref-token');
    expect(link).toHaveTextContent('I7');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/issues/issue-abc');
  });

  it('falls back to the FULL id (never a #id-tail hash) when the issue has no org_ref (T126)', async () => {
    mockOrgIssues();
    renderInOrg(<MarkdownMessage content={'blocked on issue-noref now'} />);
    const link = await screen.findByTestId('issue-ref-token');
    expect(link).toHaveTextContent('issue-noref');
    expect(link).not.toHaveTextContent('#');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/issues/issue-noref');
  });

  it('leaves an UNKNOWN issue-<id> / I-number as plain text (no dangling link)', async () => {
    mockOrgIssues();
    renderInOrg(<MarkdownMessage content={'ref issue-zzz and I999 do not exist'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('ref issue-zzz and I999 do not exist'),
    );
    expect(screen.queryByTestId('issue-ref-token')).toBeNull();
  });

  it('does NOT match issue- mid-word, an I-number mid-token, nor the bare pronoun "I"', async () => {
    mockOrgIssues();
    renderInOrg(<MarkdownMessage content={'a reissue-abc and PART7 and I7x and I think these are not refs'} />);
    await waitFor(() =>
      expect(screen.getByTestId('markdown-message')).toHaveTextContent('a reissue-abc and PART7 and I7x and I think these are not refs'),
    );
    expect(screen.queryByTestId('issue-ref-token')).toBeNull();
  });

  it('does NOT linkify an issue-ref inside a fenced code block', async () => {
    mockOrgIssues();
    renderInOrg(<MarkdownMessage content={'```\nclose issue-abc here\n```'} />);
    await waitFor(() => expect(screen.getByTestId('markdown-message')).toBeInTheDocument());
    expect(screen.queryByTestId('issue-ref-token')).toBeNull();
  });

  it('linkifies a task-ref, a plan-ref AND an issue-ref in the same line', async () => {
    server.use(
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [{ id: 'task-abc', org_ref: 'T123', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
      http.get('/api/plans', () =>
        HttpResponse.json({
          items: [{ id: 'plan-abc', org_ref: 'P42', project: { id: 'proj-x', name: 'X' }, name: 'p', status: 'running', has_failed: false, progress: { done: 0, total: 0 }, created_at: 'x', updated_at: 'x' }],
          total: 1,
        }),
      ),
      http.get('/api/issues', () =>
        HttpResponse.json({
          items: [{ id: 'issue-abc', org_ref: 'I7', project: { id: 'proj-x', name: 'X' }, title: 'i', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
    );
    renderInOrg(<MarkdownMessage content={'T123 under P42 tracks I7'} />);
    expect(await screen.findByTestId('task-ref-token')).toHaveTextContent('T123');
    expect(await screen.findByTestId('plan-ref-token')).toHaveTextContent('P42');
    expect(await screen.findByTestId('issue-ref-token')).toHaveTextContent('I7');
  });

  // @all broadcast (per @oopslink): rendered as a distinct, non-clickable mention
  // token regardless of member resolution (no member named "all").
  it('renders @all as a distinct broadcast token (not a clickable member link)', async () => {
    server.use(http.get('/api/members', () => HttpResponse.json([])));
    renderInProvider(<MarkdownMessage content={'@all standup in 5'} />);
    const token = await screen.findByTestId('mention-all-token');
    expect(token).toHaveTextContent('@all');
    expect(token.tagName).not.toBe('BUTTON'); // not clickable (a span)
    expect(screen.queryByTestId('mention-token')).not.toBeInTheDocument();
  });
});

// T317: a bare `agent-<id>` identity reference auto-linkifies to a clickable
// token that opens the agent's SenderDetailSidebar (same target as an @mention).
describe('MentionText agent-ref linkify (T336)', () => {
  afterEach(() => cleanup());

  // REAL data shape: an agent's member identity_id is "agent-<id>" (hyphen) — the
  // "agent-" is PART of the bare id (normalizeIdentityRef doesn't strip it); the
  // prefixed ref is "agent:agent-<id>". So the whole `agent-<id>` token is the id.
  function mockAgentMembers() {
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'mem-1', organization_id: 'O', identity_id: 'agent-35ac0e16', display_name: 'agent-center-dev5', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
          { id: 'mem-2', organization_id: 'O', identity_id: 'agent-f9dc523f', display_name: 'agent-center-pd', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
        ]),
      ),
    );
  }

  it('linkifies a bare agent-<id> ref (the full token is the member identity id)', async () => {
    mockAgentMembers();
    renderInProvider(<MarkdownMessage content={'integrate done by agent-35ac0e16 now'} />);
    const token = await screen.findByTestId('agent-ref-token');
    expect(token.tagName).toBe('BUTTON');
    expect(token).toHaveTextContent('agent-35ac0e16');
    expect(token).toHaveAttribute('data-agent-ref', 'agent:agent-35ac0e16');
  });

  it('linkifies an agent ref embedded after "=" (e.g. [...owner=agent-f9dc523f])', async () => {
    mockAgentMembers();
    renderInProvider(<MarkdownMessage content={'F5 Dev T311 owner=agent-f9dc523f done'} />);
    const token = await screen.findByTestId('agent-ref-token');
    expect(token).toHaveTextContent('agent-f9dc523f');
    expect(token).toHaveAttribute('data-agent-ref', 'agent:agent-f9dc523f');
  });

  it('leaves an unknown agent-<id> as plain text (verify-not-trust)', async () => {
    mockAgentMembers();
    renderInProvider(<MarkdownMessage content={'ref to agent-deadbeef here'} />);
    await screen.findByText(/ref to/);
    expect(screen.queryByTestId('agent-ref-token')).not.toBeInTheDocument();
  });
});
