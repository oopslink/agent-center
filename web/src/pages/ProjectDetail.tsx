import React from 'react';
import { Link, useParams } from 'react-router-dom';
import { useProject } from '@/api/projects';
import { useIssues } from '@/api/issues';
import { useTasksList } from '@/api/tasks';
import { Skeleton } from '@/components/Skeleton';

// ProjectDetail (/projects/:id). Read-only profile + Issues/Tasks
// preview panels.
//
// v2.3-5b cutover (per § 0.6): the Issues/Tasks panels now read real
// per-project lists via the BC-native endpoints (Discussion BC for
// issues, TaskRuntime BC for tasks). Previously these were a
// cross-BC `useConversations({kind:'issue'|'task'})` read filtered
// purely on the client (the Conversation projection carries no
// project_id), which surfaced cross-project data with a hint chip.
// Cutover deletes that hint — the panels now answer the obvious
// question accurately.
export default function ProjectDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const project = useProject(id);

  if (project.isLoading) {
    return (
      <section className="space-y-3" data-testid="page-ProjectDetail">
        <Skeleton width="14rem" height="1.75rem" />
        <Skeleton width="20rem" height="1rem" />
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Skeleton height="8rem" />
          <Skeleton height="8rem" />
        </div>
      </section>
    );
  }
  if (project.isError) {
    return (
      <section className="space-y-3" data-testid="page-ProjectDetail">
        <p className="text-sm text-danger" data-testid="project-not-found">
          {(project.error as Error).message}
        </p>
        <Link to="/projects" className="text-xs text-accent hover:underline">
          ← Back to projects
        </Link>
      </section>
    );
  }
  if (!project.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-ProjectDetail">
        Project lookup failed.
      </section>
    );
  }

  const p = project.data;
  return (
    <section className="space-y-4" data-testid="page-ProjectDetail" data-project-id={p.id}>
      <header className="space-y-2 border-b border-border-base pb-3">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{p.name}</h1>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-muted">
            {p.id}
          </span>
          {p.kind && (
            <span className="rounded bg-brand/10 px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide text-brand">
              {p.kind}
            </span>
          )}
          {p.default_agent_cli && (
            <span
              className="rounded border border-border-base px-2 py-0.5 font-mono text-[0.6875rem] text-text-secondary"
              data-testid="project-default-agent-cli"
            >
              {p.default_agent_cli}
            </span>
          )}
        </div>
        {p.description && (
          <p className="max-w-3xl text-sm text-text-secondary" data-testid="project-description">
            {p.description}
          </p>
        )}
      </header>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <IssuesPanel projectId={p.id} />
        <TasksPanel projectId={p.id} />
      </div>

      <FleetLinkSection projectId={p.id} />
    </section>
  );
}

// -----------------------------------------------------------------------------
// Inline PanelCard — mirrors the Home.tsx shape (per brief: don't extract a
// shared component this pass, just copy).
// -----------------------------------------------------------------------------
function PanelCard({
  title,
  to,
  empty,
  loading,
  children,
  ...rest
}: {
  title: string;
  to: string;
  empty: string;
  loading: boolean;
  children: React.ReactNode;
} & React.HTMLAttributes<HTMLDivElement>): React.ReactElement {
  const items = React.Children.toArray(children);
  return (
    <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1" {...rest}>
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">{title}</h2>
        <Link to={to} className="text-xs text-accent hover:underline">
          View all →
        </Link>
      </div>
      {loading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : items.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-border-base">{items}</ul>
      )}
    </div>
  );
}

function IssuesPanel({ projectId }: { projectId: string }): React.ReactElement {
  const issues = useIssues({ projectId });
  const recent = (issues.data ?? []).slice(0, 5);
  return (
    <PanelCard
      title="Issues"
      to={`/issues?project=${encodeURIComponent(projectId)}`}
      empty="No issues yet"
      loading={issues.isLoading}
      data-testid="project-issues-panel"
    >
      {recent.map((iss) => (
        <li key={iss.id} className="flex items-center justify-between gap-3 py-1.5">
          <Link
            to={`/issues/${encodeURIComponent(iss.id)}`}
            className="truncate text-sm text-text-primary hover:text-accent"
          >
            {iss.title || iss.id}
          </Link>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
            {iss.status.replace(/_/g, ' ')}
          </span>
        </li>
      ))}
    </PanelCard>
  );
}

function TasksPanel({ projectId }: { projectId: string }): React.ReactElement {
  const tasks = useTasksList({ projectId });
  const recent = (tasks.data ?? []).slice(0, 5);
  return (
    <PanelCard
      title="Tasks"
      to={`/tasks?project=${encodeURIComponent(projectId)}`}
      empty="No tasks yet"
      loading={tasks.isLoading}
      data-testid="project-tasks-panel"
    >
      {recent.map((tk) => (
        <li key={tk.id} className="flex items-center justify-between gap-3 py-1.5">
          <Link
            to={`/tasks/${encodeURIComponent(tk.id)}`}
            className="truncate text-sm text-text-primary hover:text-accent"
          >
            {tk.title || tk.id}
          </Link>
          <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
            {tk.status}
          </span>
        </li>
      ))}
    </PanelCard>
  );
}

function FleetLinkSection({ projectId }: { projectId: string }): React.ReactElement {
  return (
    <div
      className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1"
      data-testid="project-fleet-link"
    >
      <h2 className="font-heading text-sm font-semibold text-text-primary">Workers</h2>
      <p className="mt-1 text-xs text-text-secondary">
        Worker / execution rollups for this project live in the Fleet view.
      </p>
      <Link
        to={`/fleet?project=${encodeURIComponent(projectId)}`}
        className="mt-2 inline-block text-xs text-accent hover:underline"
      >
        View in Fleet →
      </Link>
    </div>
  );
}
