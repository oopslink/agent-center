import type React from 'react';
import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useTasksList } from '@/api/tasks';
import { useProjects } from '@/api/projects';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import type { TaskStatus } from '@/api/types';

// Tasks page (/tasks). Lists TaskRuntime BC Tasks with status + project
// filter rows.
//
// v2.3-5b cutover (per § 0.6): this page now reads from the TaskRuntime
// BC-native `GET /api/tasks?project_id=...` endpoint (was previously
// `useConversations({kind:'task'})`, a cross-BC reach). The project
// chip filter is now REAL — the backend rejects requests without a
// project_id. Status enum here is TaskRuntime BC's 4-value Task.Status
// (different from ConversationStatus). Trace link is wired off
// `current_execution_id` which the Task projection populates only
// when an execution is in flight.
const STATUS_TABS: Array<{ label: string; value: TaskStatus | 'all' }> = [
  { label: 'All', value: 'all' },
  { label: 'Open', value: 'open' },
  { label: 'Suspended', value: 'suspended' },
  { label: 'Done', value: 'done' },
  { label: 'Abandoned', value: 'abandoned' },
];

export default function Tasks(): React.ReactElement {
  const [filter, setFilter] = useState<TaskStatus | 'all'>('all');
  const [searchParams, setSearchParams] = useSearchParams();
  const projectFilter = searchParams.get('project') ?? 'all';
  const projects = useProjects();
  const tasks = useTasksList({
    projectId: projectFilter === 'all' ? undefined : projectFilter,
    status: filter === 'all' ? undefined : filter,
  });
  const data = tasks.data ?? [];

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

      <div className="flex flex-wrap gap-1" role="tablist" aria-label="status filter">
        {STATUS_TABS.map((t) => (
          <button
            key={t.value}
            type="button"
            role="tab"
            aria-selected={filter === t.value}
            onClick={() => setFilter(t.value)}
            className={[
              'rounded px-3 py-1 text-xs uppercase tracking-wide',
              filter === t.value
                ? 'bg-slate-900 text-white'
                : 'bg-slate-100 text-slate-600 hover:bg-slate-200',
            ].join(' ')}
            data-testid="tasks-status-tab"
            data-status={t.value}
          >
            {t.label}
          </button>
        ))}
      </div>

      {projectFilter === 'all' && (
        <EmptyState
          testId="tasks-pick-project"
          title="Pick a project"
          body="Tasks live inside a project — choose one from the chip row above to see its tasks."
        />
      )}
      {projectFilter !== 'all' && tasks.isLoading && (
        <div className="space-y-2" data-testid="tasks-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {projectFilter !== 'all' && tasks.isError && (
        <p className="text-sm text-danger" data-testid="tasks-error">
          {(tasks.error as Error).message}
        </p>
      )}
      {projectFilter !== 'all' && tasks.isSuccess && data.length === 0 && (
        <EmptyState
          testId="tasks-empty"
          title={filter === 'all' ? 'No tasks yet' : `No ${filter} tasks`}
          body={
            filter === 'all'
              ? 'Tasks are units of work an agent can pick up and execute. Create one from a conversation via Derive → Task, or via the CLI.'
              : 'Switch the filter above or create a new task.'
          }
        />
      )}
      {projectFilter !== 'all' && data.length > 0 && (
        <ul className="divide-y divide-slate-200 rounded border border-slate-200 bg-white">
          {data.map((tk) => (
            <li key={tk.id} data-testid="task-row" data-task-id={tk.id}>
              <Link
                to={`/tasks/${encodeURIComponent(tk.id)}`}
                className="flex items-center justify-between px-4 py-3 hover:bg-slate-50"
              >
                <span className="flex items-center gap-3">
                  <span className="font-medium">{tk.title || tk.id}</span>
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-600">
                    {tk.status}
                  </span>
                  <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase text-slate-600">
                    {tk.priority}
                  </span>
                </span>
                <span className="flex items-center gap-3 text-xs text-slate-500">
                  <span>{formatRelative(tk.created_at)}</span>
                  {tk.current_execution_id && (
                    <Link
                      to={`/tasks/${encodeURIComponent(tk.id)}/trace`}
                      className="text-blue-600 hover:underline"
                      onClick={(e) => e.stopPropagation()}
                      data-testid="task-row-trace-link"
                    >
                      view trace →
                    </Link>
                  )}
                </span>
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

function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}
