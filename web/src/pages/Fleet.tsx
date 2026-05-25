import type React from 'react';
import { Link } from 'react-router-dom';
import { useFleet } from '@/api/fleet';

// Fleet (/fleet). 4-segment overview: workers + active executions +
// open input requests + pending issues. Warnings (when the backend
// returned a partial snapshot) get a yellow banner at the top.
//
// SSE invalidation: F5 wires worker.* + agent_instance.* +
// task_execution.state_changed → invalidate qk.fleet().
export default function Fleet(): React.ReactElement {
  const fleet = useFleet();

  return (
    <section className="space-y-6" data-testid="page-Fleet">
      <header className="flex items-center justify-between">
        <h2 className="text-xl font-semibold">Fleet</h2>
        {fleet.data?.generated_at && (
          <span className="text-xs text-slate-500" data-testid="fleet-generated-at">
            generated {fleet.data.generated_at}
          </span>
        )}
      </header>

      {fleet.isLoading && (
        <p className="text-sm text-slate-500" data-testid="fleet-loading">
          Loading…
        </p>
      )}
      {fleet.isError && (
        <p className="text-sm text-danger" data-testid="fleet-error">
          {(fleet.error as Error).message}
        </p>
      )}

      {fleet.data?.warnings && fleet.data.warnings.length > 0 && (
        <div
          className="rounded border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800"
          data-testid="fleet-warnings"
        >
          <p className="font-medium">Partial snapshot:</p>
          <ul className="ml-4 list-disc text-xs">
            {fleet.data.warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}

      {fleet.isSuccess && (
        <>
          <Section title="Workers" empty="No workers enrolled yet — see install docs.">
            {fleet.data.workers.length > 0 && (
              <table
                className="w-full table-fixed border-separate border-spacing-0 rounded border border-slate-200 bg-white text-sm"
                data-testid="fleet-workers-table"
              >
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-slate-500">
                    <th className="border-b border-slate-200 px-3 py-2">Worker</th>
                    <th className="border-b border-slate-200 px-3 py-2">Status</th>
                    <th className="border-b border-slate-200 px-3 py-2">Active</th>
                    <th className="border-b border-slate-200 px-3 py-2">Mappings</th>
                    <th className="border-b border-slate-200 px-3 py-2">Last heartbeat</th>
                  </tr>
                </thead>
                <tbody>
                  {fleet.data.workers.map((w) => (
                    <tr key={w.worker_id} data-testid="fleet-worker-row" data-worker-id={w.worker_id}>
                      <td className="border-b border-slate-100 px-3 py-2 font-mono text-xs">
                        {w.worker_id}
                      </td>
                      <td className="border-b border-slate-100 px-3 py-2">
                        <span className="rounded bg-slate-100 px-2 py-0.5 text-xs uppercase">
                          {w.status}
                        </span>
                      </td>
                      <td className="border-b border-slate-100 px-3 py-2 font-mono text-xs">
                        {w.active_count}
                      </td>
                      <td className="border-b border-slate-100 px-3 py-2 font-mono text-xs">
                        {w.mappings_count}
                      </td>
                      <td className="border-b border-slate-100 px-3 py-2 text-xs text-slate-500">
                        {w.last_heartbeat_at || '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {fleet.data.workers.length === 0 && (
              <p
                className="text-xs text-slate-500"
                data-testid="fleet-workers-empty"
              >
                No workers enrolled. Enroll a worker via{' '}
                <code className="rounded bg-slate-100 px-1.5 py-0.5 font-mono">
                  agent-center worker enroll
                </code>
                .
              </p>
            )}
          </Section>

          <Section title="Active executions" empty="No active executions.">
            {fleet.data.executions.length > 0 && (
              <ul className="divide-y divide-slate-200 rounded border border-slate-200 bg-white text-sm" data-testid="fleet-exec-list">
                {fleet.data.executions.map((e) => (
                  <li
                    key={e.execution_id}
                    className="flex items-center justify-between px-3 py-2 text-xs"
                    data-testid="fleet-exec-row"
                    data-execution-id={e.execution_id}
                  >
                    <span>
                      <Link
                        to={`/tasks/${encodeURIComponent(e.task_id)}`}
                        className="font-mono text-blue-600 hover:underline"
                      >
                        {e.task_id}
                      </Link>{' '}
                      <span className="text-slate-500">on worker</span>{' '}
                      <span className="font-mono">{e.worker_id}</span>
                    </span>
                    <span className="rounded bg-slate-100 px-2 py-0.5 uppercase text-slate-600">
                      {e.status}
                    </span>
                  </li>
                ))}
              </ul>
            )}
            {fleet.data.executions.length === 0 && (
              <p
                className="text-xs text-slate-500"
                data-testid="fleet-exec-empty"
              >
                Nothing running right now.
              </p>
            )}
          </Section>

          <Section title="Open input requests" empty="No open input requests.">
            {fleet.data.open_input_requests.length > 0 && (
              <ul
                className="divide-y divide-slate-200 rounded border border-slate-200 bg-white text-sm"
                data-testid="fleet-ir-list"
              >
                {fleet.data.open_input_requests.map((ir) => (
                  <li
                    key={ir.input_request_id}
                    className="flex items-center justify-between px-3 py-2 text-xs"
                  >
                    <span>{ir.question}</span>
                    <Link
                      to="/inputrequests"
                      className="text-blue-600 hover:underline"
                    >
                      respond →
                    </Link>
                  </li>
                ))}
              </ul>
            )}
            {fleet.data.open_input_requests.length === 0 && (
              <p className="text-xs text-slate-500">No open input requests.</p>
            )}
          </Section>

          <Section title="Pending issues" empty="No pending issues.">
            {fleet.data.pending_issues.length > 0 && (
              <ul
                className="divide-y divide-slate-200 rounded border border-slate-200 bg-white text-sm"
                data-testid="fleet-issues-list"
              >
                {fleet.data.pending_issues.map((i) => (
                  <li key={i.issue_id} className="px-3 py-2 text-xs">
                    <Link
                      to={`/issues/${encodeURIComponent(i.issue_id)}`}
                      className="text-blue-600 hover:underline"
                    >
                      {i.title}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
            {fleet.data.pending_issues.length === 0 && (
              <p className="text-xs text-slate-500">No pending issues.</p>
            )}
          </Section>
        </>
      )}
    </section>
  );
}

function Section({
  title,
  empty,
  children,
}: {
  title: string;
  empty: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold text-slate-700">{title}</h3>
      {children ?? <p className="text-xs text-slate-500">{empty}</p>}
    </section>
  );
}
