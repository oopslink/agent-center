import type React from 'react';
import { Link } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

// Tasks page (/tasks). Lists kind=task conversations. Like issues but
// task lifecycle is owned by TaskRuntime BC; here we only render the
// conversation surface.
export default function Tasks(): React.ReactElement {
  const all = useConversations({ kind: 'task' });

  return (
    <section className="space-y-4" data-testid="page-Tasks">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Tasks</h2>
      </header>

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
