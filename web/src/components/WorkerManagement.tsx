import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useOptionalOrgContext } from '@/OrgContext';
import { withOrgSlug } from '@/api/client';
import { qk } from '@/api/queryKeys';
import { useAgents } from '@/api/agents';
import { useForceDeleteWorker } from '@/api/workers';
import { ConfirmModal } from '@/components/ConfirmModal';
import { ForceDeleteModal } from '@/components/ForceDeleteModal';
import { InstallCommandModal } from '@/components/InstallCommandModal';
import type { EnvWorker } from '@/api/types';

// WorkerManagement — the #273 Management tab. Worker-lifecycle actions, all on
// existing endpoints (no new backend): rename (PATCH /api/workers/{id}/name),
// Install command + Re-mint (InstallCommandModal, show/remint), Remove worker
// (hard DELETE + ConfirmModal #169 + bound-agent warning). Rename lives here per
// PD (centralised in the detail page; Environment links the name in → here).
export function WorkerManagement({ worker }: { worker: EnvWorker }): React.ReactElement {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const agents = useAgents();
  const boundCount = (agents.data ?? []).filter((a) => a.worker_id === worker.worker_id).length;

  // --- Rename (PATCH /api/workers/{id}/name) ---
  const displayName = worker.name || worker.worker_id;
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(worker.name || '');
  const [renameBusy, setRenameBusy] = useState(false);
  const [renameError, setRenameError] = useState<string | null>(null);
  const saveRename = async () => {
    const next = draft.trim();
    if (!next || next === worker.name) {
      setEditing(false);
      return;
    }
    setRenameBusy(true);
    setRenameError(null);
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
      void qc.invalidateQueries({ queryKey: qk.worker(worker.worker_id) });
      void qc.invalidateQueries({ queryKey: qk.workers() });
      void qc.invalidateQueries({ queryKey: qk.fleet() });
    } catch (e) {
      setRenameError(e instanceof Error ? e.message : String(e));
    } finally {
      setRenameBusy(false);
    }
  };

  // --- Install / Re-mint (InstallCommandModal) ---
  const [installMode, setInstallMode] = useState<'show' | 'remint' | null>(null);

  // --- Force delete (admin escape hatch + typed-name confirm) ---
  // Cleans the center's records regardless of the worker's busy/online state
  // (skips the soft-remove guards) and unbinds its agents; the 200 body reports
  // how many were unbound. Kept open on 409/error (fed into the modal's `error`).
  const forceDelete = useForceDeleteWorker();
  const [forceOpen, setForceOpen] = useState(false);
  const [forceError, setForceError] = useState<string | null>(null);
  const [unboundNote, setUnboundNote] = useState<number | null>(null);
  const handleForceDelete = () => {
    setForceError(null);
    forceDelete.mutate(worker.worker_id, {
      onSuccess: (res) => {
        setForceOpen(false);
        setUnboundNote(res.unbound_agents);
        navigate(org?.slug ? `/organizations/${org.slug}/environment` : '/environment');
      },
      onError: (e) => setForceError(e instanceof Error ? e.message : String(e)),
    });
  };

  // --- Remove worker (hard DELETE + ConfirmModal + bound-agent warning) ---
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [removeBusy, setRemoveBusy] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);
  // Plain-text warning (no emoji — a11y no-emoji standard). The bound-agent line
  // is informed consent: removal does not unbind/delete agents (no such endpoint,
  // #273 PD (a)); they are simply left without a worker (unavailable).
  const boundLine =
    boundCount > 0
      ? `\n\nWarning: ${boundCount} agent(s) are bound to this worker. Removing it leaves them without a worker (unavailable). They are not deleted or unbound — to remove an agent, archive it from its detail page.`
      : '';
  const removeMessage =
    `Remove worker "${displayName}"? This revokes the worker token and removes the record.` + boundLine;
  const handleRemove = async () => {
    setRemoveBusy(true);
    setRemoveError(null);
    try {
      const resp = await fetch(withOrgSlug(`/api/workers/${encodeURIComponent(worker.worker_id)}`), {
        method: 'DELETE',
      });
      if (!resp.ok && resp.status !== 204) {
        const body = (await resp.json().catch(() => ({}))) as { message?: string };
        throw new Error(body.message || `HTTP ${resp.status}`);
      }
      setConfirmOpen(false);
      void qc.invalidateQueries({ queryKey: qk.workers() });
      void qc.invalidateQueries({ queryKey: qk.fleet() });
      navigate(org?.slug ? `/organizations/${org.slug}/environment` : '/environment');
    } catch (e) {
      setRemoveError(e instanceof Error ? e.message : String(e));
      setConfirmOpen(false);
    } finally {
      setRemoveBusy(false);
    }
  };

  return (
    <div className="space-y-6" data-testid="worker-management">
      {/* Rename */}
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">Name</h3>
        {editing ? (
          <form
            className="flex items-center gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              void saveRename();
            }}
          >
            <input
              autoFocus
              value={draft}
              onChange={(e) => setDraft(e.target.value)}
              disabled={renameBusy}
              className="w-56 rounded border border-border-base bg-bg-elevated px-2 py-1 text-sm text-text-primary focus:border-accent"
              data-testid="worker-rename-input"
              aria-label="Worker name"
            />
            <button
              type="submit"
              disabled={renameBusy}
              className="rounded bg-brand px-2 py-1 text-xs text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
              data-testid="worker-rename-save"
            >
              {renameBusy ? 'Saving…' : 'Save'}
            </button>
            <button
              type="button"
              disabled={renameBusy}
              onClick={() => {
                setEditing(false);
                setDraft(worker.name || '');
                setRenameError(null);
              }}
              className="text-xs text-text-muted hover:text-text-primary"
            >
              Cancel
            </button>
            {renameError && (
              <span className="text-xs text-danger" data-testid="worker-rename-error">
                {renameError}
              </span>
            )}
          </form>
        ) : (
          <div className="flex items-center gap-3">
            <span className="text-sm" data-testid="worker-management-name">
              {displayName}
            </span>
            <button
              type="button"
              onClick={() => {
                setDraft(worker.name || '');
                setEditing(true);
              }}
              className="text-xs text-accent hover:underline"
              data-testid="worker-rename-edit"
            >
              Rename
            </button>
          </div>
        )}
      </section>

      {/* Install command + Re-mint */}
      <section className="space-y-2">
        <h3 className="text-sm font-semibold">Install command</h3>
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={() => setInstallMode('show')}
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            data-testid="worker-install-show"
          >
            Show install command
          </button>
          <button
            type="button"
            onClick={() => setInstallMode('remint')}
            className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
            data-testid="worker-install-remint"
          >
            Re-mint install token
          </button>
        </div>
      </section>

      {/* Remove worker (danger) */}
      <section className="space-y-2 border-t border-border-base pt-4">
        <h3 className="text-sm font-semibold text-danger">Remove worker</h3>
        <p className="text-xs text-text-muted">
          Hard-removes this worker. Re-enroll a machine to register it again.
        </p>
        <div className="flex flex-wrap items-center gap-3">
          <button
            type="button"
            disabled={removeBusy}
            onClick={() => setConfirmOpen(true)}
            className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10 disabled:cursor-not-allowed disabled:text-text-muted"
            data-testid="worker-remove"
          >
            {removeBusy ? 'Removing…' : 'Remove worker'}
          </button>
          {/* v2.8.1: force-delete — admin escape hatch that bypasses the
              busy/online guards, removes the center's records and unbinds the
              worker's agents (typed-name confirm). */}
          <button
            type="button"
            disabled={forceDelete.isPending}
            onClick={() => {
              setForceError(null);
              setForceOpen(true);
            }}
            className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10 disabled:cursor-not-allowed disabled:text-text-muted"
            data-testid="worker-force-delete"
          >
            {forceDelete.isPending ? 'Deleting…' : 'Force delete'}
          </button>
          {removeError && (
            <span className="text-xs text-danger" data-testid="worker-remove-error">
              {removeError}
            </span>
          )}
          {unboundNote != null && (
            <span className="text-xs text-text-muted" data-testid="worker-force-delete-note">
              {unboundNote} agent(s) unbound.
            </span>
          )}
        </div>
      </section>

      {installMode && (
        <InstallCommandModal
          workerID={worker.worker_id}
          mode={installMode}
          onClose={() => setInstallMode(null)}
        />
      )}
      <ConfirmModal
        open={confirmOpen}
        title="Remove worker?"
        message={removeMessage}
        confirmLabel="Remove"
        danger
        busy={removeBusy}
        onConfirm={() => void handleRemove()}
        onCancel={() => setConfirmOpen(false)}
      />
      <ForceDeleteModal
        open={forceOpen}
        entityKind="worker"
        entityName={displayName}
        busy={forceDelete.isPending}
        error={forceError}
        onConfirm={handleForceDelete}
        onCancel={() => setForceOpen(false)}
      />
    </div>
  );
}
