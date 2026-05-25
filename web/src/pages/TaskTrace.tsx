import type React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useTaskTrace } from '@/api/fleet';
import { TraceTimeline } from '@/components/TraceTimeline';

// TaskTrace (/tasks/:id/trace). Live event timeline for one task. The
// SSE dispatch table invalidates the trace query on
// task_execution.state_changed, so the timeline updates as the agent
// runs.
export default function TaskTrace(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const trace = useTaskTrace(id);

  return (
    <section className="space-y-4" data-testid="page-TaskTrace" data-task-id={id}>
      <header className="flex items-center justify-between border-b border-slate-200 pb-3">
        <div>
          <h2 className="text-xl font-semibold">Trace</h2>
          <p className="text-xs text-slate-500">
            task: <span className="font-mono">{id}</span>
          </p>
        </div>
        <Link
          to={`/tasks/${encodeURIComponent(id)}`}
          className="text-sm text-blue-600 hover:underline"
          data-testid="trace-back"
        >
          ← Back to task
        </Link>
      </header>

      {trace.isLoading && (
        <p className="text-sm text-slate-500" data-testid="trace-loading">
          Loading trace…
        </p>
      )}
      {trace.isError && (
        <p className="text-sm text-danger" data-testid="trace-error">
          {(trace.error as Error).message}
        </p>
      )}
      {trace.isSuccess && <TraceTimeline events={trace.data} />}
    </section>
  );
}
