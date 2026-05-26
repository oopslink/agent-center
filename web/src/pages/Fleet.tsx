import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useFleet } from '@/api/fleet';
import { qk } from '@/api/queryKeys';
import type { FleetWorkerRow } from '@/api/types';
import { AddWorkerModal } from '@/components/AddWorkerModal';
import { InstallCommandModal } from '@/components/InstallCommandModal';

// Fleet (/fleet). 4-segment overview: workers + active executions +
// open input requests + pending issues. Warnings (when the backend
// returned a partial snapshot) get a yellow banner at the top.
//
// SSE invalidation: F5 wires worker.* + agent_instance.* +
// task_execution.state_changed → invalidate qk.fleet().
//
// v2.4-D-F4: newly-enrolled worker rows briefly highlight green
// (3s fade) so the user sees which row is the one they just added.
const HIGHLIGHT_MS = 3_000;

// v2.5-F2 (#54): Fleet row install-command actions. Tracks which
// worker row's command modal is open + which mode (show vs re-mint).
type InstallCommandModalState = { workerID: string; mode: 'show' | 'remint' } | null;

export default function Fleet(): React.ReactElement {
  const fleet = useFleet();
  // v2.4-D-F1 (task #41): "Add Worker" button + Modal launch.
  const [modalOpen, setModalOpen] = useState(false);
  // v2.5-F2 (#54): per-row install-command modal.
  const [installModal, setInstallModal] = useState<InstallCommandModalState>(null);
  // v2.4-D-F4 (task #44): worker_ids currently flashing the "just
  // enrolled" highlight. Map of id → expiry timestamp.
  const [highlighted, setHighlighted] = useState<Record<string, number>>({});

  useEffect(() => {
    const handler = (ev: Event) => {
      const detail = (ev as CustomEvent<{ worker_id?: string }>).detail || {};
      if (!detail.worker_id) return;
      const id = detail.worker_id;
      setHighlighted((prev) => ({ ...prev, [id]: Date.now() + HIGHLIGHT_MS }));
      setTimeout(() => {
        setHighlighted((prev) => {
          if (!prev[id]) return prev;
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }, HIGHLIGHT_MS);
    };
    window.addEventListener('agent-center:worker-enrolled', handler);
    return () => window.removeEventListener('agent-center:worker-enrolled', handler);
  }, []);

  return (
    <section className="space-y-6" data-testid="page-Fleet">
      <header className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold">Fleet</h2>
          {fleet.data?.generated_at && (
            <span className="text-xs text-text-muted" data-testid="fleet-generated-at">
              generated {fleet.data.generated_at}
            </span>
          )}
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setModalOpen(true)}
          data-testid="fleet-add-worker-btn"
        >
          + Add Worker
        </button>
      </header>

      {modalOpen && <AddWorkerModal onClose={() => setModalOpen(false)} />}

      {installModal && (
        <InstallCommandModal
          workerID={installModal.workerID}
          mode={installModal.mode}
          onClose={() => setInstallModal(null)}
        />
      )}

      {fleet.isLoading && (
        <p className="text-sm text-text-muted" data-testid="fleet-loading">
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
          className="rounded border border-warning/40 bg-warning/10 p-3 text-sm text-warning"
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
                className="w-full table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
                data-testid="fleet-workers-table"
              >
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-text-muted">
                    <th className="border-b border-border-base px-3 py-2">Worker</th>
                    <th className="border-b border-border-base px-3 py-2">Status</th>
                    <th className="border-b border-border-base px-3 py-2">Active</th>
                    <th className="border-b border-border-base px-3 py-2">Mappings</th>
                    <th className="border-b border-border-base px-3 py-2">Last heartbeat</th>
                    <th className="border-b border-border-base px-3 py-2 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {fleet.data.workers.map((w) => {
                    const flashing = Boolean(highlighted[w.worker_id]);
                    return (
                    <tr
                      key={w.worker_id}
                      data-testid="fleet-worker-row"
                      data-worker-id={w.worker_id}
                      data-just-enrolled={flashing ? 'true' : undefined}
                      className={
                        flashing
                          ? 'motion-safe:animate-pulse bg-success/10 motion-safe:transition-colors motion-safe:duration-700'
                          : 'motion-safe:transition-colors motion-safe:duration-700'
                      }
                    >
                      <td className="border-b border-border-base px-3 py-2">
                        <WorkerNameCell worker={w} />
                      </td>
                      <td className="border-b border-border-base px-3 py-2">
                        <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                          {w.status}
                        </span>
                      </td>
                      <td className="border-b border-border-base px-3 py-2 font-mono text-xs">
                        {w.active_count}
                      </td>
                      <td className="border-b border-border-base px-3 py-2 font-mono text-xs">
                        {w.mappings_count}
                      </td>
                      <td className="border-b border-border-base px-3 py-2 text-xs text-text-muted">
                        {w.last_heartbeat_at || '—'}
                      </td>
                      <td className="border-b border-border-base px-3 py-2 text-right">
                        <WorkerRowActions
                          worker={w}
                          onShowInstall={() => setInstallModal({ workerID: w.worker_id, mode: 'show' })}
                          onReMintInstall={() => setInstallModal({ workerID: w.worker_id, mode: 'remint' })}
                        />
                      </td>
                    </tr>
                    );
                  })}
                </tbody>
              </table>
            )}
            {fleet.data.workers.length === 0 && (
              <div
                className="rounded border border-dashed border-border-strong bg-bg-subtle p-6 text-center"
                data-testid="fleet-workers-empty"
              >
                <p className="text-sm text-text-secondary">
                  No workers connected yet.
                </p>
                <p className="mt-2 text-xs text-text-muted">
                  A worker is a machine where agents actually run.
                  Add at least one to start dispatching tasks.
                </p>
                <button
                  type="button"
                  className="mt-4 rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover"
                  onClick={() => setModalOpen(true)}
                  data-testid="fleet-workers-empty-cta"
                >
                  + Add your first worker
                </button>
              </div>
            )}
          </Section>

          <Section title="Active executions" empty="No active executions.">
            {fleet.data.executions.length > 0 && (
              <ul className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary" data-testid="fleet-exec-list">
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
                        className="font-mono text-accent hover:underline"
                      >
                        {e.task_id}
                      </Link>{' '}
                      <span className="text-text-muted">on worker</span>{' '}
                      <span className="font-mono">{e.worker_id}</span>
                    </span>
                    <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
                      {e.status}
                    </span>
                  </li>
                ))}
              </ul>
            )}
            {fleet.data.executions.length === 0 && (
              <p
                className="text-xs text-text-muted"
                data-testid="fleet-exec-empty"
              >
                Nothing running right now.
              </p>
            )}
          </Section>

          <Section title="Open input requests" empty="No open input requests.">
            {fleet.data.open_input_requests.length > 0 && (
              <ul
                className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
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
                      className="text-accent hover:underline"
                    >
                      respond →
                    </Link>
                  </li>
                ))}
              </ul>
            )}
            {fleet.data.open_input_requests.length === 0 && (
              <p className="text-xs text-text-muted">No open input requests.</p>
            )}
          </Section>

          <Section title="Pending issues" empty="No pending issues.">
            {fleet.data.pending_issues.length > 0 && (
              <ul
                className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
                data-testid="fleet-issues-list"
              >
                {fleet.data.pending_issues.map((i) => (
                  <li key={i.issue_id} className="px-3 py-2 text-xs">
                    <Link
                      to={`/issues/${encodeURIComponent(i.issue_id)}`}
                      className="text-accent hover:underline"
                    >
                      {i.title}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
            {fleet.data.pending_issues.length === 0 && (
              <p className="text-xs text-text-muted">No pending issues.</p>
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
      <h3 className="mb-2 text-sm font-semibold text-text-primary">{title}</h3>
      {children ?? <p className="text-xs text-text-muted">{empty}</p>}
    </section>
  );
}

// WorkerNameCell shows the friendly name + worker id, with inline
// edit on click. v2.4-D-X1 @oopslink ask. Falls back to id when the
// projection is missing a name (older rows pre-migration 0030 are
// backfilled to name=id so this only triggers on partial responses).
function WorkerNameCell({ worker }: { worker: FleetWorkerRow }): React.ReactElement {
  const qc = useQueryClient();
  const [editing, setEditing] = useState(false);
  const displayName = worker.name || worker.worker_id;
  const [draft, setDraft] = useState(displayName);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const save = async () => {
    const next = draft.trim();
    if (!next || next === displayName) {
      setEditing(false);
      setDraft(displayName);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const resp = await fetch(`/api/workers/${encodeURIComponent(worker.worker_id)}/name`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: next }),
      });
      if (!resp.ok) {
        const body = (await resp.json().catch(() => ({}))) as { error?: string; message?: string };
        throw new Error(body.message || body.error || `HTTP ${resp.status}`);
      }
      setEditing(false);
      // Local cache flip; the SSE workforce.worker.renamed event
      // will re-invalidate fleet for any other tab.
      void qc.invalidateQueries({ queryKey: qk.fleet() });
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  if (editing) {
    return (
      <form
        className="flex items-center gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <input
          autoFocus
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          disabled={busy}
          className="w-40 rounded border border-border-base bg-bg-elevated px-2 py-0.5 text-sm text-text-primary focus:border-accent"
          data-testid="fleet-worker-name-input"
        />
        <button
          type="submit"
          disabled={busy}
          className="rounded bg-brand px-2 py-0.5 text-xs text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
          data-testid="fleet-worker-name-save"
        >
          Save
        </button>
        <button
          type="button"
          disabled={busy}
          onClick={() => {
            setEditing(false);
            setDraft(displayName);
            setError(null);
          }}
          className="text-xs text-text-muted hover:text-text-primary"
        >
          Cancel
        </button>
        {error && <span className="text-xs text-danger">{error}</span>}
      </form>
    );
  }
  return (
    <div className="flex flex-col">
      <button
        type="button"
        className="text-left text-sm font-medium text-text-primary hover:text-accent"
        onClick={() => setEditing(true)}
        title="Click to rename"
        data-testid="fleet-worker-name"
      >
        {displayName}
      </button>
      <span className="font-mono text-[0.6875rem] text-text-muted" data-testid="fleet-worker-id">
        {worker.worker_id}
      </span>
    </div>
  );
}

// WorkerRowActions hosts the v2.5-F2 / F3 per-row affordances:
// install command show + re-mint for offline rows (those buttons
// are useless once the daemon connects), plus a Remove button on
// every row (online included — operator may want to tear down a
// stuck worker).
function WorkerRowActions({
  worker,
  onShowInstall,
  onReMintInstall,
}: {
  worker: FleetWorkerRow;
  onShowInstall: () => void;
  onReMintInstall: () => void;
}): React.ReactElement {
  const showInstallActions = worker.status === 'offline';
  return (
    <div className="flex justify-end gap-2" data-testid="fleet-worker-actions">
      {showInstallActions && (
        <>
          <button
            type="button"
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            onClick={onShowInstall}
            data-testid="fleet-worker-show-install"
          >
            Show install command
          </button>
          <button
            type="button"
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            onClick={() => {
              if (window.confirm(
                'Re-mint will revoke the current install token and issue a fresh one. ' +
                  'Use this if the original command expired or got lost. Continue?',
              )) {
                onReMintInstall();
              }
            }}
            data-testid="fleet-worker-remint-install"
          >
            Re-mint install command
          </button>
        </>
      )}
      <RemoveWorkerButton worker={worker} />
    </div>
  );
}

// RemoveWorkerButton fires DELETE /api/workers/{id} after operator
// confirms. The SSE workforce.worker.removed event invalidates
// Fleet so the row disappears without an explicit refetch. v2.5-F3.
function RemoveWorkerButton({ worker }: { worker: FleetWorkerRow }): React.ReactElement {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const handleRemove = async () => {
    const message =
      worker.status === 'online'
        ? `Remove worker "${worker.name || worker.worker_id}"?\n\n` +
          'This will revoke the worker token and remove the record. ' +
          'The worker daemon will hit 401 next cycle.'
        : `Remove worker "${worker.name || worker.worker_id}"?\n\n` +
          'This will revoke any active install token and remove the record.';
    if (!window.confirm(message)) return;
    setBusy(true);
    setError(null);
    try {
      const resp = await fetch(`/api/workers/${encodeURIComponent(worker.worker_id)}`, {
        method: 'DELETE',
      });
      if (!resp.ok && resp.status !== 204) {
        let detail = `HTTP ${resp.status}`;
        try {
          const body = (await resp.json()) as { message?: string };
          if (body.message) detail = body.message;
        } catch {
          // ignore parse failure
        }
        throw new Error(detail);
      }
      // SSE workforce.worker.removed will invalidate the fleet query
      // and retire this row; no further client-side cleanup needed.
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="flex items-center gap-2">
      {error && (
        <span className="text-xs text-danger" data-testid="fleet-worker-remove-error">
          {error}
        </span>
      )}
      <button
        type="button"
        disabled={busy}
        className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10 disabled:cursor-not-allowed disabled:text-text-muted"
        onClick={() => void handleRemove()}
        data-testid="fleet-worker-remove"
      >
        {busy ? 'Removing...' : 'Remove'}
      </button>
    </span>
  );
}
