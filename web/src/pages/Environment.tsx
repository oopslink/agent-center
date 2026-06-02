import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useFleet } from '@/api/fleet';
import { useAgents } from '@/api/agents';
import { useTransferSessions } from '@/api/workers';
import { qk } from '@/api/queryKeys';
import { withOrgSlug } from '@/api/client';
import { useOptionalOrgContext, OrgLink } from '@/OrgContext';
import type { Agent, FleetWorkerRow } from '@/api/types';
import { LifecycleBadge } from '@/components/AgentBadges';
import { AddWorkerModal } from '@/components/AddWorkerModal';
import { InstallCommandModal } from '@/components/InstallCommandModal';
import { ConfirmModal } from '@/components/ConfirmModal';

// Environment page (/environment). v2.7 #164: Fleet merged into Environment — this
// is the single operational page for the organization's workers + agents + work
// items + file transfers. Worker data comes from /api/fleet (canonical
// workforce.Worker + active work-item count); each worker shows the agents bound
// to it (grouped from the org-scoped /api/agents by worker_id), its install/remove
// actions, and inline rename. Work items + pending issues + in-flight transfers
// follow. (The old separate Fleet page + sidebar entry were removed; /fleet now
// redirects here.)
const HIGHLIGHT_MS = 3_000;

type InstallCommandModalState = { workerID: string; mode: 'show' | 'remint' } | null;

export default function Environment(): React.ReactElement {
  const fleet = useFleet();
  const agents = useAgents();
  const transfers = useTransferSessions();
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';

  const [modalOpen, setModalOpen] = useState(false);
  const [installModal, setInstallModal] = useState<InstallCommandModalState>(null);
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

  const agentsByWorker = (workerID: string): Agent[] =>
    (agents.data ?? []).filter((a) => a.worker_id === workerID);

  return (
    <section className="space-y-6" data-testid="page-Environment">
      <header className="flex items-center justify-between">
        <div>
          <h2 className="text-xl font-semibold">Environment</h2>
          <p className="text-xs text-text-muted">
            Workers in this organization, the agents bound to them, in-flight work
            items, and file transfers.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setModalOpen(true)}
          data-testid="environment-add-worker-btn"
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
        <p className="text-sm text-text-muted" data-testid="environment-loading">
          Loading…
        </p>
      )}
      {fleet.isError && (
        <p className="text-sm text-danger" data-testid="environment-error">
          {(fleet.error as Error).message}
        </p>
      )}

      {fleet.data?.warnings && fleet.data.warnings.length > 0 && (
        <div
          className="rounded border border-warning/40 bg-warning/10 p-3 text-sm text-warning"
          data-testid="environment-warnings"
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
          <Section title="Workers">
            {fleet.data.workers.length === 0 ? (
              <div
                className="rounded border border-dashed border-border-strong bg-bg-subtle p-6 text-center"
                data-testid="environment-workers-empty"
              >
                <p className="text-sm text-text-secondary">No workers connected yet.</p>
                <p className="mt-2 text-xs text-text-muted">
                  A worker is a machine where agents actually run. Add at least one to
                  start dispatching tasks.
                </p>
                <button
                  type="button"
                  className="mt-4 rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover"
                  onClick={() => setModalOpen(true)}
                  data-testid="environment-workers-empty-cta"
                >
                  + Add your first worker
                </button>
              </div>
            ) : (
              <ul className="space-y-3" data-testid="environment-workers">
                {fleet.data.workers.map((wk) => {
                  const flashing = Boolean(highlighted[wk.worker_id]);
                  const wkAgents = agentsByWorker(wk.worker_id);
                  return (
                    <li
                      key={wk.worker_id}
                      className={`rounded border border-border-base bg-bg-elevated p-3 ${
                        flashing
                          ? 'motion-safe:animate-pulse bg-success/10 motion-safe:transition-colors motion-safe:duration-700'
                          : ''
                      }`}
                      data-testid="environment-worker"
                      data-worker-id={wk.worker_id}
                      data-status={wk.status}
                      data-just-enrolled={flashing ? 'true' : undefined}
                    >
                      <div className="flex items-start justify-between gap-2">
                        <WorkerNameCell worker={wk} />
                        <div className="flex items-center gap-3">
                          <span
                            className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary"
                            data-testid="environment-worker-status"
                            data-status={wk.status}
                          >
                            {wk.status}
                          </span>
                          <span className="font-mono text-xs text-text-muted" data-testid="environment-worker-active">
                            {wk.active_count} active
                          </span>
                          <span className="text-xs text-text-muted">
                            {wk.last_heartbeat_at ? `hb ${wk.last_heartbeat_at}` : 'hb —'}
                          </span>
                        </div>
                      </div>

                      <div className="mt-2 flex items-center justify-between gap-2">
                        {wkAgents.length === 0 ? (
                          <p className="text-xs text-text-muted" data-testid="environment-worker-noagents">
                            No agents bound to this worker.
                          </p>
                        ) : (
                          <ul className="space-y-1" data-testid="environment-worker-agents">
                            {wkAgents.map((a) => (
                              <li
                                key={a.id}
                                className="flex items-center gap-2 text-sm"
                                data-testid="environment-agent"
                                data-agent-id={a.id}
                              >
                                <span>{a.name}</span>
                                <LifecycleBadge lifecycle={a.lifecycle} />
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
                        <WorkerRowActions
                          worker={wk}
                          onShowInstall={() => setInstallModal({ workerID: wk.worker_id, mode: 'show' })}
                          onReMintInstall={() => setInstallModal({ workerID: wk.worker_id, mode: 'remint' })}
                        />
                      </div>
                    </li>
                  );
                })}
              </ul>
            )}
          </Section>

          <Section title="Work items">
            {fleet.data.work_items.length === 0 ? (
              <p className="text-xs text-text-muted" data-testid="environment-workitem-empty">
                Nothing running right now.
              </p>
            ) : (
              <ul
                className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
                data-testid="environment-workitem-list"
              >
                {fleet.data.work_items.map((wi) => (
                  <li
                    key={wi.work_item_id}
                    className="flex items-center justify-between px-3 py-2 text-xs"
                    data-testid="environment-workitem-row"
                    data-work-item-id={wi.work_item_id}
                  >
                    <span>
                      {wi.task_id ? (
                        <Link
                          to={`${base}/tasks/${encodeURIComponent(wi.task_id)}`}
                          className="font-mono text-accent hover:underline"
                        >
                          {wi.task_id}
                        </Link>
                      ) : (
                        <span className="font-mono text-text-muted">{wi.work_item_id}</span>
                      )}{' '}
                      <span className="text-text-muted">agent</span>{' '}
                      <span className="font-mono">{wi.agent_id}</span>
                      {wi.current_activity ? (
                        <span className="text-text-muted"> · {wi.current_activity}</span>
                      ) : null}
                    </span>
                    <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
                      {wi.status}
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </Section>

          <Section title="Pending issues">
            {fleet.data.pending_issues.length === 0 ? (
              <p className="text-xs text-text-muted">No pending issues.</p>
            ) : (
              <ul
                className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
                data-testid="environment-issues-list"
              >
                {fleet.data.pending_issues.map((i) => (
                  <li key={i.issue_id} className="px-3 py-2 text-xs">
                    <Link
                      to={`${base}/issues/${encodeURIComponent(i.issue_id)}`}
                      className="text-accent hover:underline"
                    >
                      {i.title}
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </Section>
        </>
      )}

      <Section title="In-flight file transfers">
        <p className="text-xs text-text-muted">
          Open file-transfer sessions in this organization (resolved by scope).
        </p>
        {transfers.isLoading && (
          <div className="mt-2" data-testid="transfers-loading">
            <p className="text-xs text-text-muted">Loading…</p>
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
      </Section>
    </section>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }): React.ReactElement {
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold text-text-primary">{title}</h3>
      {children}
    </section>
  );
}

// WorkerNameCell: friendly name + worker id with inline rename (PATCH
// /api/workers/{id}/name). Ported from the retired Fleet page (#164).
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
      const resp = await fetch(withOrgSlug(`/api/workers/${encodeURIComponent(worker.worker_id)}/name`), {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: next }),
      });
      if (!resp.ok) {
        const body = (await resp.json().catch(() => ({}))) as { error?: string; message?: string };
        throw new Error(body.message || body.error || `HTTP ${resp.status}`);
      }
      setEditing(false);
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
          data-testid="environment-worker-name-input"
        />
        <button
          type="submit"
          disabled={busy}
          className="rounded bg-brand px-2 py-0.5 text-xs text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
          data-testid="environment-worker-name-save"
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
        data-testid="environment-worker-name"
      >
        {displayName}
      </button>
      <span className="font-mono text-[0.6875rem] text-text-muted" data-testid="environment-worker-id">
        {worker.worker_id}
      </span>
    </div>
  );
}

// WorkerRowActions: install command (offline only) + Remove. Ported from Fleet
// (#164). #169: native window.confirm replaced with ConfirmModal.
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
  const [confirmReMint, setConfirmReMint] = useState(false);
  return (
    <div className="flex shrink-0 justify-end gap-2" data-testid="environment-worker-actions">
      {showInstallActions && (
        <>
          <button
            type="button"
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            onClick={onShowInstall}
            data-testid="environment-worker-show-install"
          >
            Show install command
          </button>
          <button
            type="button"
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            onClick={() => setConfirmReMint(true)}
            data-testid="environment-worker-remint-install"
          >
            Re-mint install command
          </button>
          <ConfirmModal
            open={confirmReMint}
            title="Re-mint install command?"
            message={
              'Re-mint will revoke the current install token and issue a fresh one. ' +
              'Use this if the original command expired or got lost. Continue?'
            }
            confirmLabel="Re-mint"
            onConfirm={() => {
              setConfirmReMint(false);
              onReMintInstall();
            }}
            onCancel={() => setConfirmReMint(false)}
          />
        </>
      )}
      <RemoveWorkerButton worker={worker} />
    </div>
  );
}

// RemoveWorkerButton: DELETE /api/workers/{id} after confirm. Ported from Fleet
// (#164). #169: native window.confirm replaced with ConfirmModal.
function RemoveWorkerButton({ worker }: { worker: FleetWorkerRow }): React.ReactElement {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const confirmMessage =
    worker.status === 'online'
      ? `Remove worker "${worker.name || worker.worker_id}"?\n\n` +
        'This will revoke the worker token and remove the record. ' +
        'The worker daemon will hit 401 next cycle.'
      : `Remove worker "${worker.name || worker.worker_id}"?\n\n` +
        'This will revoke any active install token and remove the record.';
  const handleRemove = async () => {
    setBusy(true);
    setError(null);
    try {
      const resp = await fetch(withOrgSlug(`/api/workers/${encodeURIComponent(worker.worker_id)}`), {
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
      setConfirmOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setConfirmOpen(false);
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="flex items-center gap-2">
      {error && (
        <span className="text-xs text-danger" data-testid="environment-worker-remove-error">
          {error}
        </span>
      )}
      <button
        type="button"
        disabled={busy}
        className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10 disabled:cursor-not-allowed disabled:text-text-muted"
        onClick={() => setConfirmOpen(true)}
        data-testid="environment-worker-remove"
      >
        {busy ? 'Removing...' : 'Remove'}
      </button>
      <ConfirmModal
        open={confirmOpen}
        title="Remove worker?"
        message={confirmMessage}
        confirmLabel="Remove"
        danger
        busy={busy}
        onConfirm={() => void handleRemove()}
        onCancel={() => setConfirmOpen(false)}
      />
    </span>
  );
}
