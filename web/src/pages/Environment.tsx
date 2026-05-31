import type React from 'react';

import { useAgents } from '@/api/agents';
import { useWorkers, useTransferSessions } from '@/api/workers';
import { OrgLink } from '@/OrgContext';
import { LifecycleBadge } from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import type { Agent } from '@/api/types';

// Environment page (/environment). Environment BC (v2.7 E1 #138) — the
// CONTROL-CONNECTED worker view (environment.Worker): workers that have connected
// the control channel, each with its control-stream state (status / last-acked
// offset / heartbeat) and the agents bound to it (grouped from the org-scoped
// /api/agents by worker_id).
//
// NOTE: this is the control-connected set, DISTINCT from the Fleet page's enrolled
// set (legacy workforce.Worker). The header says so explicitly, so an operator does
// not expect an enrolled-but-never-connected worker to appear here.
export default function Environment(): React.ReactElement {
  const workers = useWorkers();
  const agents = useAgents();
  const transfers = useTransferSessions();

  const agentsByWorker = (workerID: string): Agent[] =>
    (agents.data ?? []).filter((a) => a.worker_id === workerID);

  return (
    <section className="space-y-4" data-testid="page-Environment">
      <header>
        <h2 className="text-xl font-semibold">Environment</h2>
        <p className="text-xs text-text-muted">
          Workers connected to the control channel (control-connected view) and the
          agents bound to them. Enrolled-but-not-connected workers appear on Fleet.
        </p>
      </header>

      {workers.isLoading && (
        <div className="space-y-2" data-testid="environment-loading">
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
        </div>
      )}
      {workers.isError && (
        <p className="text-sm text-danger" data-testid="environment-error">
          {(workers.error as Error).message}
        </p>
      )}
      {workers.isSuccess && workers.data.length === 0 && (
        <EmptyState
          testId="environment-empty"
          title="No control-connected workers"
          body="A worker appears here once its daemon connects the control channel. Enroll a worker on the Fleet page, then start its daemon."
        />
      )}
      {workers.isSuccess && workers.data.length > 0 && (
        <ul className="space-y-3" data-testid="environment-workers">
          {workers.data.map((wk) => {
            const wkAgents = agentsByWorker(wk.worker_id);
            return (
              <li
                key={wk.worker_id}
                className="rounded border border-border-base bg-bg-elevated p-3"
                data-testid="environment-worker"
                data-worker-id={wk.worker_id}
                data-status={wk.status}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{wk.name || wk.worker_id}</span>
                    <span
                      className="rounded px-1.5 py-0.5 text-xs"
                      data-testid="environment-worker-status"
                      data-status={wk.status}
                    >
                      {wk.status}
                    </span>
                  </div>
                  <span className="font-mono text-xs text-text-muted">
                    offset {wk.last_acked_offset}
                  </span>
                </div>

                {wkAgents.length === 0 ? (
                  <p className="mt-2 text-xs text-text-muted" data-testid="environment-worker-noagents">
                    No agents bound to this worker.
                  </p>
                ) : (
                  <ul className="mt-2 space-y-1" data-testid="environment-worker-agents">
                    {wkAgents.map((a) => (
                      <li
                        key={a.id}
                        className="flex items-center justify-between text-sm"
                        data-testid="environment-agent"
                        data-agent-id={a.id}
                      >
                        <span className="flex items-center gap-2">
                          <span>{a.name}</span>
                          <LifecycleBadge lifecycle={a.lifecycle} />
                        </span>
                        <OrgLink
                          to={`/agents/${encodeURIComponent(a.id)}`}
                          className="text-xs text-accent hover:underline"
                        >
                          Open →
                        </OrgLink>
                      </li>
                    ))}
                  </ul>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <div data-testid="environment-transfers-section" className="pt-2">
        <h3 className="text-sm font-semibold">In-flight file transfers</h3>
        <p className="text-xs text-text-muted">
          Open file-transfer sessions in this organization (resolved by scope).
        </p>
        {transfers.isLoading && (
          <div className="mt-2" data-testid="transfers-loading">
            <Skeleton height="2rem" />
          </div>
        )}
        {transfers.isError && (
          <p className="mt-2 text-sm text-danger" data-testid="transfers-error">
            {(transfers.error as Error).message}
          </p>
        )}
        {transfers.isSuccess && transfers.data.length === 0 && (
          <p className="mt-2 text-xs text-text-muted" data-testid="transfers-empty">
            No in-flight transfers.
          </p>
        )}
        {transfers.isSuccess && transfers.data.length > 0 && (
          <table
            className="mt-2 w-full table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-text-primary"
            data-testid="transfers-table"
          >
            <thead>
              <tr className="text-left text-xs uppercase tracking-wide text-text-muted">
                <th className="w-1/5 border-b border-border-base px-3 py-2">Direction</th>
                <th className="w-1/5 border-b border-border-base px-3 py-2">Scope</th>
                <th className="w-2/5 border-b border-border-base px-3 py-2">Content</th>
                <th className="border-b border-border-base px-3 py-2 text-right">Size</th>
              </tr>
            </thead>
            <tbody>
              {transfers.data.map((tr) => (
                <tr
                  key={tr.id}
                  className="text-sm"
                  data-testid="transfer-row"
                  data-transfer-id={tr.id}
                  data-scope={tr.scope}
                >
                  <td className="border-b border-border-base px-3 py-2">{tr.direction}</td>
                  <td className="border-b border-border-base px-3 py-2 font-mono text-xs">
                    {tr.scope}/{tr.scope_id}
                  </td>
                  <td className="border-b border-border-base px-3 py-2 text-xs text-text-muted">
                    {tr.content_type}
                  </td>
                  <td className="border-b border-border-base px-3 py-2 text-right font-mono text-xs">
                    {tr.size}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
