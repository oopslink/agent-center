import type React from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { useProjects } from '@/api/projects';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

// Tasks page (/tasks). Lists kind=task conversations. Like issues but
// task lifecycle is owned by TaskRuntime BC; here we only render the
// conversation surface.
//
// PROJECT FILTER: the Conversation projection does NOT carry
// project_id today (the Task AR holds the project link in its own BC).
// The chip row is rendered for UX continuity with the v2.3-4 project
// surface and writes the selection back to the URL (?project=…).
// Filtering is COSMETIC for now — wiring requires projecting
// project_id onto the conversation read model (follow-up pass).
export default function Tasks(): React.ReactElement {
  const [searchParams, setSearchParams] = useSearchParams();
  const projectFilter = searchParams.get('project') ?? 'all';
  const projects = useProjects();
  const all = useConversations({ kind: 'task' });

  const setProject = (id: string) => {
    const next = new URLSearchParams(searchParams);
    if (id === 'all') next.delete('project');
    else next.set('project', id);
    setSearchParams(next, { replace: true });
  };

  return (
    <section className="space-y-4" data-testid="page-Tasks">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Tasks</h2>
      </header>

      <div
        className="flex flex-wrap items-center gap-1"
        role="tablist"
        aria-label="project filter"
        data-testid="tasks-project-filter"
      >
        <span className="mr-1 text-[0.6875rem] uppercase tracking-wider text-text-muted">
          Project:
        </span>
        <ProjectChip
          label="All"
          value="all"
          selected={projectFilter === 'all'}
          onClick={() => setProject('all')}
        />
        {(projects.data ?? []).map((p) => (
          <ProjectChip
            key={p.id}
            label={p.name}
            value={p.id}
            selected={projectFilter === p.id}
            onClick={() => setProject(p.id)}
          />
        ))}
      </div>

      {all.isLoading && (
        <div className="space-y-2" data-testid="tasks-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {all.isError && (
        <p className="text-sm text-danger" data-testid="tasks-error">
          {(all.error as Error).message}
        </p>
      )}
      {all.isSuccess && all.data.length === 0 && (
        <EmptyState
          testId="tasks-empty"
          title="No tasks yet"
          body="Tasks are units of work an agent can pick up and execute. Create one from a conversation via Derive → Task, or via the CLI."
        />
      )}
      {all.isSuccess && all.data.length > 0 && (
        <ul className="divide-y divide-slate-200 rounded border border-slate-200 bg-white">
          {all.data.map((c) => (
            <li key={c.id} data-testid="task-row" data-task-id={c.id}>
              <Link
                to={`/tasks/${encodeURIComponent(c.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-slate-50"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{c.name || c.id}</span>
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-600">
                    {c.status}
                  </span>
                </span>
                <Link
                  to={`/tasks/${encodeURIComponent(c.id)}/trace`}
                  className="text-xs text-blue-600 hover:underline"
                  onClick={(e) => e.stopPropagation()}
                >
                  view trace →
                </Link>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function ProjectChip({
  label,
  value,
  selected,
  onClick,
}: {
  label: string;
  value: string;
  selected: boolean;
  onClick: () => void;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={onClick}
      data-testid="tasks-project-chip"
      data-project={value}
      className={[
        'rounded-full px-3 py-0.5 text-xs',
        selected
          ? 'bg-brand text-white'
          : 'bg-bg-subtle text-text-secondary hover:bg-bg-elevated',
      ].join(' ')}
    >
      {label}
    </button>
  );
}
