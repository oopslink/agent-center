// AddWorkerModal — v2.5-F1 (#53) simplification.
//
// The Modal is now a thin "name + Generate → close" form. Before
// v2.5 it kept a 7-state machine that hung around showing the
// install command, the wait-for-daemon spinner, success/timeout
// hints, and re-mint affordances. v2.5 decouples "add worker"
// (logical: create record, status=offline) from "install worker"
// (operator runs ./install on the worker machine) — see
// #agent-center:5f8a6f7e. Once the mint succeeds the Worker AR
// already exists in Fleet, and the operator picks up the install
// command from the Fleet row's "Show install command" action
// (B2 #50). So the Modal can close immediately.
//
// Remaining states (3):
//   - name_prompt: user types friendly name, clicks Generate
//   - minting:     POST in flight (sub-second)
//   - mint_error:  POST failed (e.g. admin TCP listener not
//                  configured) — show server message + Retry/Close
//
// On mint success the Modal closes; the new offline Fleet row
// arrives via the workforce.worker.added SSE event (wired in
// useSSE).
import React, { useEffect, useState } from 'react';
import { useAppStore } from '@/store/app';
import { withOrgSlug } from '@/api/client';

type ModalState =
  | { kind: 'name_prompt'; name: string }
  | { kind: 'minting' }
  | { kind: 'mint_error'; message: string; lastName: string };

interface Props {
  onClose: () => void;
}

// MintResponse mirrors POST /api/admintoken/mint-enroll. The full
// install-command rebuild lives in the Fleet row's Show Install
// Command Modal (F2 #54) so this file no longer needs the bearer
// or fingerprint.
interface MintResponse {
  id: string;
  worker_id: string;
  worker_name: string;
}

async function mintEnrollToken(name: string): Promise<MintResponse> {
  const resp = await fetch(withOrgSlug('/api/admintoken/mint-enroll'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!resp.ok) {
    let detail = `HTTP ${resp.status}`;
    try {
      const body = (await resp.json()) as { error?: string; message?: string };
      if (body.error || body.message) {
        detail = `${body.error ?? ''}${body.error && body.message ? ': ' : ''}${body.message ?? ''}`;
      }
    } catch {
      // body not JSON; keep status fallback
    }
    throw new Error(detail);
  }
  return resp.json();
}

export function AddWorkerModal({ onClose }: Props): React.ReactElement {
  const [state, setState] = useState<ModalState>({ kind: 'name_prompt', name: '' });

  // The global WorkerEnrolledToast (F4) suppresses itself while
  // this Modal is mounted so the toast doesn't fire on top of the
  // Modal. v2.5 keeps that contract: the toast lives for the
  // workforce.worker.enrolled signal, which now arrives AFTER the
  // Modal closes (so suppression is mostly a noop), but the flag
  // stays so the wiring is uniform with v2.4 expectations.
  useEffect(() => {
    useAppStore.getState().setAddWorkerModalOpen(true);
    return () => useAppStore.getState().setAddWorkerModalOpen(false);
  }, []);

  const startMint = (name: string) => {
    setState({ kind: 'minting' });
    void (async () => {
      try {
        await mintEnrollToken(name);
        // v2.5-F1: close immediately on success. The new Worker
        // row paints in Fleet via the workforce.worker.added SSE
        // event; the operator gets the install command from the
        // Fleet row's "Show install command" action.
        onClose();
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        setState({ kind: 'mint_error', message, lastName: name });
      }
    })();
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="add-worker-modal"
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-2xl rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Add a Worker</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="add-worker-close"
          >
            X
          </button>
        </div>
        <ModalBody
          state={state}
          onSubmitName={(name) => startMint(name)}
          onNameChange={(name) => setState({ kind: 'name_prompt', name })}
          onClose={onClose}
        />
      </div>
    </div>
  );
}

interface BodyProps {
  state: ModalState;
  onSubmitName: (name: string) => void;
  onNameChange: (name: string) => void;
  onClose: () => void;
}

function ModalBody({ state, onSubmitName, onNameChange, onClose }: BodyProps): React.ReactElement {
  switch (state.kind) {
    case 'name_prompt': {
      const trimmed = state.name.trim();
      const canSubmit = trimmed.length > 0;
      return (
        <form
          data-testid="modal-state-name-prompt"
          onSubmit={(e) => {
            e.preventDefault();
            if (canSubmit) onSubmitName(trimmed);
          }}
        >
          <label
            htmlFor="add-worker-name-input"
            className="mb-1 block text-sm font-medium text-text-primary"
          >
            Worker name
          </label>
          <input
            id="add-worker-name-input"
            data-testid="modal-name-input"
            type="text"
            value={state.name}
            onChange={(e) => onNameChange(e.target.value)}
            autoFocus
            placeholder="e.g. test-1 or tenant-foo"
            className="block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent"
          />
          <p className="mt-1 text-xs text-text-muted">
            Use a unique name (e.g. <code className="font-mono">test-1</code>,{' '}
            <code className="font-mono">tenant-foo</code>) so you can spot this worker in Fleet.
            You'll grab the install command from the new Fleet row's Show install command action.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              onClick={onClose}
              data-testid="modal-name-prompt-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
              data-testid="modal-name-prompt-submit"
            >
              Add worker
            </button>
          </div>
        </form>
      );
    }
    case 'minting':
      return (
        <div className="py-8 text-center" data-testid="modal-state-minting">
          <p className="text-sm text-text-secondary">Adding worker...</p>
          <div className="mt-4 inline-block h-6 w-6 animate-spin rounded-full border-2 border-border-base border-t-brand" />
        </div>
      );
    case 'mint_error':
      return (
        <div data-testid="modal-state-mint-error">
          <p className="mb-3 text-sm font-medium text-danger">
            Could not add worker.
          </p>
          <p className="mb-3 text-sm text-text-primary">{state.message}</p>
          <p className="mb-4 text-xs text-text-muted">
            Common causes: admin TCP listener is not enabled on the
            center (set <code className="font-mono">server.admin_tcp_listen</code> in the config,
            e.g. <code className="font-mono">0.0.0.0:7300</code>) or the AdminToken service is not
            wired. Check the server logs.
          </p>
          <div className="flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              onClick={onClose}
              data-testid="modal-mint-error-close"
            >
              Close
            </button>
            <button
              type="button"
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
              onClick={() => onSubmitName(state.lastName)}
              data-testid="modal-mint-error-retry"
            >
              Try again
            </button>
          </div>
        </div>
      );
  }
}
