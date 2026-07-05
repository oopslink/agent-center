import { afterEach, describe, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type React from 'react';
import type { Task } from '@/api/types';
import { server } from '@/test/mswServer';
import { TaskDetailSidebar } from './TaskDetailSidebar';
import { SenderSidebarProvider } from './SenderSidebarContext';

// T102: the assignee name in the Task detail sidebar is clickable → opens the
// shared SenderDetailSidebar (agent info + activity feed), reusing the same
// openSender path as @mentions / message senders.

function makeTask(over: Partial<Task> = {}): Task {
  return {
    id: 'task-01KT8DXYZ789',
    project_id: 'proj-a',
    title: 'ship docs',
    description: '',
    status: 'open',
    assignee: 'agent:agent-bot9',
    tags: [],
    version: 1,
    created_at: '2026-05-24T01:00:00Z',
    updated_at: '2026-05-24T01:00:00Z',
    ...over,
  };
}

function renderWithProvider(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <SenderSidebarProvider>{ui}</SenderSidebarProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

// renderQC wraps a test that supplies its OWN MemoryRouter (the no-SenderSidebar-
// provider cases) in just a QueryClientProvider — the sidebar now embeds
// ObjectAuditTimeline (change-log §7), which useQuery's the audit endpoint. No msw
// handler is registered here, so the query stays pending/errors harmlessly; these
// tests assert the surrounding layout, not the (independently tested) timeline.
function renderQC(ui: React.ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

describe('TaskDetailSidebar — assignee → agent activity sidebar (T102)', () => {
  afterEach(() => cleanup());

  it('clicking the assignee opens the shared sender (agent activity) sidebar', () => {
    server.use(
      http.get('/api/agents/:id', () =>
        HttpResponse.json({ id: 'agent-bot9', name: 'Bot Nine', status: 'idle' }),
      ),
    );
    renderWithProvider(
      <TaskDetailSidebar task={makeTask()} projectName="Project A" assigneeName="Bot Nine" onEdit={() => {}} editable />,
    );
    // The assignee is a clickable trigger when a provider is present.
    const btn = screen.getByTestId('task-assignee-open');
    expect(screen.queryByTestId('sender-sidebar-overlay')).not.toBeInTheDocument();
    fireEvent.click(btn);
    // The single shared sender sidebar opens (the activity panel).
    expect(screen.getByTestId('sender-sidebar-overlay')).toBeInTheDocument();
  });

  it('without a provider the assignee is non-interactive (no-op safe, still shown)', () => {
    renderQC(
      <MemoryRouter>
        <TaskDetailSidebar task={makeTask()} projectName="Project A" assigneeName="Bot Nine" onEdit={() => {}} editable />
      </MemoryRouter>,
    );
    expect(screen.queryByTestId('task-assignee-open')).not.toBeInTheDocument();
    expect(screen.getByTestId('task-assignee')).toHaveTextContent('Bot Nine');
  });

  it('an unassigned task shows no clickable assignee', () => {
    renderWithProvider(
      <TaskDetailSidebar task={makeTask({ assignee: '' })} projectName="Project A" onEdit={() => {}} editable />,
    );
    expect(screen.queryByTestId('task-assignee-open')).not.toBeInTheDocument();
    expect(screen.getByTestId('task-assignee-empty')).toBeInTheDocument();
  });
});

describe('TaskDetailSidebar — owning plan link (T106)', () => {
  afterEach(() => cleanup());

  it('shows a clickable Plan link to the plan detail when the task is in a plan', () => {
    renderQC(
      <MemoryRouter>
        <TaskDetailSidebar
          task={makeTask({ plan_id: 'plan-xyz' })}
          projectName="Project A"
          plan={{ id: 'plan-xyz', name: 'v2.10.1 mobile', org_ref: 'P12' }}
          onEdit={() => {}}
          editable
        />
      </MemoryRouter>,
    );
    const link = screen.getByTestId('task-plan-link');
    expect(link).toHaveTextContent('v2.10.1 mobile');
    expect(link).toHaveAttribute('data-plan-id', 'plan-xyz');
    expect(link.getAttribute('href')).toContain('/projects/proj-a/plans/plan-xyz');
    // T574 sidebar polish: the human Plan id (P12) renders as a tag.
    expect(screen.getByTestId('task-plan-ref-tag')).toHaveTextContent('P12');
  });

  it('hides the Plan section for a backlog task / built-in pool (no plan passed)', () => {
    renderQC(
      <MemoryRouter>
        <TaskDetailSidebar task={makeTask()} projectName="Project A" onEdit={() => {}} editable />
      </MemoryRouter>,
    );
    expect(screen.queryByTestId('task-sidebar-plan')).not.toBeInTheDocument();
  });

  // T193: the "Related Issue" row (derived_from_issue → issue ref + title, click).
  it('shows the Related Issue row (ref + title) linking to the issue when derivedIssue is passed', () => {
    renderQC(
      <MemoryRouter>
        <TaskDetailSidebar
          task={makeTask({ derived_from_issue: 'issue-abc' })}
          projectName="Project A"
          derivedIssue={{ id: 'issue-abc', org_ref: 'I42', title: 'Login is broken' }}
          onEdit={() => {}}
          editable
        />
      </MemoryRouter>,
    );
    const row = screen.getByTestId('task-sidebar-derived-issue');
    expect(row).toHaveTextContent('Login is broken');
    // T574 sidebar polish: the Issue id (I42) renders as a tag (was inline mono).
    expect(screen.getByTestId('task-derived-issue-ref-tag')).toHaveTextContent('I42');
    const link = screen.getByTestId('task-derived-issue-link');
    expect(link).toHaveAttribute('data-issue-id', 'issue-abc');
    expect(link.getAttribute('href')).toContain('/projects/proj-a/issues/issue-abc');
  });

  it('hides the Related Issue row when the task has no derived issue', () => {
    renderQC(
      <MemoryRouter>
        <TaskDetailSidebar task={makeTask()} projectName="Project A" onEdit={() => {}} editable />
      </MemoryRouter>,
    );
    expect(screen.queryByTestId('task-sidebar-derived-issue')).not.toBeInTheDocument();
  });
});
