import type React from 'react';
import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { server } from '@/test/mswServer';
import { OrgContext } from '@/OrgContext';
import { ActivityRefText } from './ActivityRefText';

// oopslink DM 2026-07-04: the agent-activity timeline must render bare entity ids
// (task- / plan- / issue- / agent-<id>) as clickable ref links, reusing the same
// resolvers as MentionText — but keeping the LITERAL id as link text (the payload
// JSON is a faithful debug copy). exec-<id> has no detail page → stays plain text.

function renderInOrg(ui: React.ReactElement, slug = 'test-org') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OrgContext.Provider value={{ slug, orgId: 'O', orgName: 'Test Org' }}>{ui}</OrgContext.Provider>
    </QueryClientProvider>,
  );
}

// Default empty org lists so a single-kind test doesn't trip an unhandled request
// (every render loads members + tasks + plans + issues via the resolvers).
function mockEmpty() {
  server.use(
    http.get('/api/members', () => HttpResponse.json([])),
    http.get('/api/tasks', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/plans', () => HttpResponse.json({ items: [], total: 0 })),
    http.get('/api/issues', () => HttpResponse.json({ items: [], total: 0 })),
  );
}

describe('ActivityRefText', () => {
  afterEach(() => cleanup());

  it('linkifies a known task-<id> to the task detail page, keeping the LITERAL id as text (new tab)', async () => {
    mockEmpty();
    server.use(
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [{ id: 'task-5779df52', org_ref: 'T77', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'running', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
    );
    renderInOrg(<ActivityRefText text={'"task_ref": "task-5779df52"'} />);
    const link = await screen.findByTestId('activity-task-ref-link');
    expect(link.tagName).toBe('A');
    // literal id preserved (NOT the human "T77" label) — debug-faithful.
    expect(link).toHaveTextContent('task-5779df52');
    expect(link).not.toHaveTextContent('T77');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/tasks/task-5779df52');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    expect(link).toHaveAttribute('data-task-id', 'task-5779df52');
    // the surrounding JSON chrome (the "task_ref" key + quotes) is preserved.
    expect(screen.getByText(/"task_ref":/)).toBeInTheDocument();
  });

  it('linkifies a known plan-<id> to the plan detail page (literal id text)', async () => {
    mockEmpty();
    server.use(
      http.get('/api/plans', () =>
        HttpResponse.json({
          items: [{ id: 'plan-abc', org_ref: 'P42', project: { id: 'proj-x', name: 'X' }, name: 'p', status: 'running', has_failed: false, progress: { done: 0, total: 0 }, created_at: 'x', updated_at: 'x' }],
          total: 1,
        }),
      ),
    );
    renderInOrg(<ActivityRefText text={'plan-abc'} />);
    const link = await screen.findByTestId('activity-plan-ref-link');
    expect(link).toHaveTextContent('plan-abc');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/plans/plan-abc');
    expect(link).toHaveAttribute('data-plan-id', 'plan-abc');
  });

  it('linkifies a known issue-<id> to the issue detail page (literal id text)', async () => {
    mockEmpty();
    server.use(
      http.get('/api/issues', () =>
        HttpResponse.json({
          items: [{ id: 'issue-abc', org_ref: 'I7', project: { id: 'proj-x', name: 'X' }, title: 'i', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
    );
    renderInOrg(<ActivityRefText text={'issue-abc'} />);
    const link = await screen.findByTestId('activity-issue-ref-link');
    expect(link).toHaveTextContent('issue-abc');
    expect(link).toHaveAttribute('href', '/organizations/test-org/projects/proj-x/issues/issue-abc');
    expect(link).toHaveAttribute('data-issue-id', 'issue-abc');
  });

  it('linkifies a KNOWN agent-<id> to the org-scoped agent detail page (literal id text)', async () => {
    mockEmpty();
    server.use(
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'mem-1', organization_id: 'O', identity_id: 'agent-35ac0e16', display_name: 'agent-center-dev4', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
        ]),
      ),
    );
    renderInOrg(<ActivityRefText text={'"agent_ref": "agent:agent-35ac0e16"'} />);
    const link = await screen.findByTestId('activity-agent-ref-link');
    expect(link.tagName).toBe('A');
    // literal id preserved (not the display name — this is a debug surface).
    expect(link).toHaveTextContent('agent-35ac0e16');
    expect(link).toHaveAttribute('href', '/organizations/test-org/agents/agent-35ac0e16');
    expect(link).toHaveAttribute('data-agent-ref', 'agent:agent-35ac0e16');
  });

  it('leaves an exec-<id> as plain text (no detail page → never a dangling link)', async () => {
    mockEmpty();
    renderInOrg(<ActivityRefText text={'"executor_id": "exec-86303eb9"'} />);
    await waitFor(() => expect(screen.getByText(/exec-86303eb9/)).toBeInTheDocument());
    expect(screen.queryByTestId('activity-task-ref-link')).toBeNull();
    expect(screen.queryByTestId('activity-agent-ref-link')).toBeNull();
  });

  it('leaves an UNKNOWN / out-of-org task-<id> as plain text (verify-not-trust)', async () => {
    mockEmpty();
    renderInOrg(<ActivityRefText text={'task-zzz not in org'} />);
    await waitFor(() => expect(screen.getByText(/task-zzz not in org/)).toBeInTheDocument());
    expect(screen.queryByTestId('activity-task-ref-link')).toBeNull();
  });

  it('leaves an unknown agent-<id> as plain text (verify-not-trust)', async () => {
    mockEmpty();
    renderInOrg(<ActivityRefText text={'agent-deadbeef here'} />);
    await waitFor(() => expect(screen.getByText(/agent-deadbeef here/)).toBeInTheDocument());
    expect(screen.queryByTestId('activity-agent-ref-link')).toBeNull();
  });

  it('does NOT match a kind- prefix embedded in a larger word (subtask- boundary guard)', async () => {
    mockEmpty();
    server.use(
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [{ id: 'task-abc', org_ref: 'T1', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'open', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
    );
    // "subtask-abc" must NOT linkify even though "task-abc" is a real task.
    renderInOrg(<ActivityRefText text={'a subtask-abc reference'} />);
    await waitFor(() => expect(screen.getByText(/a subtask-abc reference/)).toBeInTheDocument());
    expect(screen.queryByTestId('activity-task-ref-link')).toBeNull();
  });

  it('linkifies MULTIPLE kinds in one pretty-printed JSON payload, preserving chrome', async () => {
    mockEmpty();
    server.use(
      http.get('/api/tasks', () =>
        HttpResponse.json({
          items: [{ id: 'task-5779df52', org_ref: 'T77', project: { id: 'proj-x', name: 'X' }, title: 't', status: 'running', assignee: null, updated_at: 'x', created_at: 'x' }],
          total: 1,
        }),
      ),
      http.get('/api/members', () =>
        HttpResponse.json([
          { id: 'mem-1', organization_id: 'O', identity_id: 'agent-35ac0e16', display_name: 'dev4', kind: 'agent', role: 'member', status: 'joined', joined_at: 'x' },
        ]),
      ),
    );
    const json = JSON.stringify(
      { event: 'executor.progress', executor_id: 'exec-86303eb9', task_ref: 'task-5779df52', by: 'agent-35ac0e16' },
      null,
      2,
    );
    renderInOrg(<ActivityRefText text={json} />);
    // task + agent both link; exec stays plain text.
    const taskLink = await screen.findByTestId('activity-task-ref-link');
    const agentLink = await screen.findByTestId('activity-agent-ref-link');
    expect(taskLink).toHaveTextContent('task-5779df52');
    expect(agentLink).toHaveTextContent('agent-35ac0e16');
    // the JSON keys survive (chrome preserved around the linked ids).
    expect(screen.getByText(/"executor_id":/)).toBeInTheDocument();
    expect(screen.getByText(/exec-86303eb9/)).toBeInTheDocument();
  });

  it('stays plain text (no crash) with NO org context — resolvers disabled', async () => {
    // No OrgContext: slug undefined → task/plan/issue queries disabled. Members
    // still loads (empty), so the agent resolver simply finds nothing.
    server.use(http.get('/api/members', () => HttpResponse.json([])));
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <ActivityRefText text={'task-5779df52 and agent-35ac0e16'} />
      </QueryClientProvider>,
    );
    await waitFor(() =>
      expect(screen.getByText(/task-5779df52 and agent-35ac0e16/)).toBeInTheDocument(),
    );
    expect(screen.queryByTestId('activity-task-ref-link')).toBeNull();
    expect(screen.queryByTestId('activity-agent-ref-link')).toBeNull();
  });
});
