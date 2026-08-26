import type React from 'react';
import { Link, useParams } from 'react-router-dom';
import { useInsightsExecution } from '@/api/insights';
import { ErrorState } from '@/components/ErrorState';

export default function InsightExecutionDetail(): React.ReactElement {
  const { slug, executionId } = useParams<{ slug: string; executionId: string }>();
  const query = useInsightsExecution(executionId);
  const base = `/organizations/${slug ?? ''}`;

  if (query.isLoading) {
    return <div className="p-6 text-sm text-text-secondary" data-testid="insight-execution-loading">Loading execution...</div>;
  }
  if (query.isError) {
    const err = query.error as { status?: number };
    if (err.status === 404) {
      return (
        <main className="p-6" data-testid="insight-execution-not-found">
          <Link className="text-sm text-brand hover:underline" to={`${base}/insights`}>Back to Insight</Link>
          <h1 className="mt-4 text-xl font-semibold text-text-primary">Execution not found</h1>
        </main>
      );
    }
    return (
      <main className="p-6">
        <ErrorState message="Could not load execution." error={query.error} testId="insight-execution-error" />
        <button type="button" className="mt-3 rounded border border-border-base px-3 py-1.5 text-sm" onClick={() => void query.refetch()}>
          Retry
        </button>
      </main>
    );
  }
  const row = query.data;
  if (!row) {
    return <div className="p-6 text-sm text-text-secondary" data-testid="insight-execution-empty">Execution detail is empty.</div>;
  }

  return (
    <main className="mx-auto max-w-5xl space-y-6 p-6">
      <header className="space-y-2">
        <Link className="text-sm text-brand hover:underline" to={`${base}/insights`}>Back to Insight</Link>
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-mono text-xl font-semibold text-text-primary" data-testid="execution-id">{row.execution_id}</h1>
            <p className="text-sm text-text-secondary">{row.task_org_ref || row.task_id} {row.task_title || ''}</p>
          </div>
          <div className="flex items-center gap-2 text-xs text-text-secondary">
            <span>Refreshed {formatDateTime(row.refreshed_at)}</span>
            <span className={row.freshness === 'fresh' ? 'text-success' : 'text-warning'} data-testid="execution-freshness">{row.freshness}</span>
            <button type="button" className="rounded border border-border-base px-2 py-1 text-xs" onClick={() => void query.refetch()}>Refresh</button>
          </div>
        </div>
      </header>
      <section className="grid gap-3 md:grid-cols-3">
        <Field label="Status" value={row.status} />
        <Field label="Attempt" value={String(row.attempt)} testId="execution-attempt" />
        <Field label="Agent" value={row.agent_id || '-'} />
        <Field label="Project" value={row.project_name || row.project_id || '-'} />
        <Field label="Worker" value={row.worker_id || '-'} />
        <Field label="Command" value={row.command_id || '-'} />
      </section>
      <section className="grid gap-3 md:grid-cols-2">
        <Field label="Submitted" value={formatDateTime(row.submitted_at)} />
        <Field label="Started" value={formatDateTime(row.started_at)} />
        <Field label="Completed" value={formatDateTime(row.completed_at)} />
        <Field label="Queue wait" value={formatMs(row.queue_wait_ms)} />
        <Field label="Duration" value={formatMs(row.duration_ms)} />
        <Field label="Status reason" value={row.status_reason || '-'} />
      </section>
      {row.status_detail && (
        <section className="rounded border border-border-base bg-bg-surface p-4">
          <h2 className="text-sm font-semibold text-text-primary">Status detail</h2>
          <pre className="mt-2 whitespace-pre-wrap text-xs text-text-secondary" data-testid="execution-status-detail">{row.status_detail}</pre>
        </section>
      )}
    </main>
  );
}

function Field({ label, value, testId }: { label: string; value: string; testId?: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-surface p-3">
      <div className="text-xs font-medium uppercase text-text-muted">{label}</div>
      <div className="mt-1 break-words text-sm text-text-primary" data-testid={testId}>{value}</div>
    </div>
  );
}

function formatMs(v: number): string {
  if (!v) return '0 ms';
  if (v < 1000) return `${v} ms`;
  return `${(v / 1000).toFixed(1)} s`;
}

function formatDateTime(raw?: string): string {
  if (!raw) return '-';
  return new Date(raw).toLocaleString();
}
