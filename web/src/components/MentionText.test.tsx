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
    http.get('/api/tasks', () =>
      HttpResponse.json({
        items: [
          {
            id: 'task-abc', org_ref: 'T123', project: { id: 'proj-x', name: 'Project X' },
            title: 'Wire the thing', status: 'open', assignee: null,
            updated_at: 'x', created_at: 'x',
          },
          {
            // a task WITHOUT an org_ref → label falls back to #id-tail.
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

  it('requests ALL statuses so a ref to a COMPLETED task still linkifies (T62/task-336335c5)', async () => {
    // T62 root cause: the resolver read the org task list with the DEFAULT
    // filter, which excludes terminal {completed, discarded}. Agents reference
    // completed tasks constantly, so those refs silently stayed plain text. The
    // fix: the resolver must ask the backend to INCLUDE every status (status=all).
    let requestedStatus: string[] = [];
    server.use(
      http.get('/api/members', () => HttpResponse.json([])),
      http.get('/api/tasks', ({ request }) => {
        requestedStatus = new URL(request.url).searchParams.getAll('status');
        return HttpResponse.json({
          items: [
            {
              id: 'task-done', org_ref: 'T99', project: { id: 'proj-x', name: 'Project X' },
              title: 'Finished work', status: 'completed', assignee: null,
              updated_at: 'x', created_at: 'x',
            },
          ],
          total: 1,
        });
      }),
    );
    renderInOrg(<MarkdownMessage content={'shipped in task-done, see notes'} />);
    const link = await screen.findByTestId('task-ref-token');
    expect(link).toHaveTextContent('T99');
    expect(link).toHaveAttribute('data-task-id', 'task-done');
    // The resolver must have asked the backend to include terminal tasks.
    expect(requestedStatus).toContain('all');
  });

  it('falls back to the #id-tail handle when the task has no org_ref', async () => {
    mockOrgTasks();
    renderInOrg(<MarkdownMessage content={'blocked on task-noref now'} />);
    const link = await screen.findByTestId('task-ref-token');
    expect(link).toHaveTextContent('#');
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
