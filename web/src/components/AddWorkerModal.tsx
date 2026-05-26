// AddWorkerModal — v2.4-D-F2 (task #42).
// 7-state machine per docs/plans/v2.4-deployment-ui-design.md § 4.2:
//   - State 0 Minting (transient, < 1s while POSTing mint-enroll)
//   - State 1 Ready (main state — command shown, waiting for worker)
//   - State 3 Success (worker connected; "Add another" / "Done")
//   - State 4 Token used (one-time-use token consumed by ANOTHER worker)
//   - State 5 Token expired (30min TTL elapsed)
//   - State 6 Timeout-hint (5 min with no enrolled event — soft hint)
// (State 2 mid-handshake merged into State 1 — TCP enroll is <1s.)
//
// SSE wire-up (F3 #43) subscribes to `workforce.worker.enrolled` event;
// when one arrives with our minted token's owner tag, Modal transitions
// to State 3. Token-expired state derives from local countdown (no
// backend SSE per A4 deferral).
//
// Modal close + unused token: auto-revokes per UI design § 9 D2.
import React, { useEffect, useMemo, useState } from 'react';
import { useAppStore } from '@/store/app';

type ModalState =
  // v2.4-D-X1 @oopslink a-i: ask the user for a friendly name first;
  // mint only fires once they click Generate. id is server-generated;
  // name is what they type here and embeds into the install command.
  | { kind: 'name_prompt'; name: string }
  | { kind: 'minting' }
  | { kind: 'mint_error'; message: string; lastName: string }
  | {
      kind: 'ready';
      tokenID: string;
      token: string;
      expiresAt: Date;
      mintedAt: Date;
      command: string;
    }
  | { kind: 'success'; worker: { id: string; name: string; capabilities: string[] } }
  | { kind: 'token_used' }
  | { kind: 'token_expired' }
  | {
      kind: 'timeout_hint';
      tokenID: string;
      token: string;
      expiresAt: Date;
      command: string;
    };

interface Props {
  onClose: () => void;
}

// MintResponse mirrors what POST /api/admintoken/mint-enroll returns
// from the Web Console webconsole/api handler (loopback-only, so no
// bearer auth needed). The server fills in fingerprint + bootstrap
// host from the admin TCP listener config + cert; the Modal renders
// them straight into the install command (no client-side guesses).
interface MintResponse {
  id: string;
  token: string;
  expires_at: string; // RFC3339
  fingerprint: string;
  bootstrap_host: string;
  worker_id: string;
  worker_name: string;
}

async function mintEnrollToken(name: string): Promise<MintResponse> {
  const resp = await fetch('/api/admintoken/mint-enroll', {
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

async function revokeEnrollToken(tokenID: string): Promise<void> {
  if (!tokenID) return;
  try {
    await fetch(`/api/admintoken/revoke?id=${encodeURIComponent(tokenID)}`, { method: 'POST' });
  } catch {
    // ignore — Modal close is fire-and-forget
  }
}

// renderCommand assembles the operator-facing install command. All
// substantive values come from the mint-enroll response — the client
// fills in nothing here, so a wrong bootstrap host or fingerprint can
// only come from the backend (which has authoritative knowledge of
// the cert + listener config).
function renderCommand(
  token: string,
  bootstrapHost: string,
  fingerprint: string,
  workerID: string,
  workerName: string,
): string {
  const lines = [
    `./install worker \\`,
    `  --bootstrap=tcp://${bootstrapHost} \\`,
    `  --server-fingerprint=${fingerprint} \\`,
    `  --worker-id=${workerID} \\`,
  ];
  if (workerName) {
    lines.push(`  --worker-name=${shellQuote(workerName)} \\`);
  }
  lines.push(`  --token=${token}`);
  return lines.join('\n');
}

// shellQuote single-quotes a string for safe paste into a POSIX
// shell, escaping any internal single-quotes via '\''. Worker names
// can have spaces / shell metacharacters when typed by a user (e.g.
// "tenant Foo's box"); without quoting the command line would break.
function shellQuote(s: string): string {
  return "'" + s.replace(/'/g, "'\\''") + "'";
}

export function AddWorkerModal({ onClose }: Props): React.ReactElement {
  // v2.4-D-X1 @oopslink a-i: Modal opens on the name_prompt step.
  // The user types a friendly name, clicks Generate, and only then
  // do we mint. id is server-generated as part of mint-enroll.
  const [state, setState] = useState<ModalState>({ kind: 'name_prompt', name: '' });

  // Tell the global WorkerEnrolledToast (F4) to suppress itself while
  // the Modal is mounted; on close we let the toast handle subsequent
  // enrollments.
  useEffect(() => {
    useAppStore.getState().setAddWorkerModalOpen(true);
    return () => useAppStore.getState().setAddWorkerModalOpen(false);
  }, []);

  const startMint = (name: string) => {
    setState({ kind: 'minting' });
    void (async () => {
      try {
        const resp = await mintEnrollToken(name);
        const expires = new Date(resp.expires_at);
        setState({
          kind: 'ready',
          tokenID: resp.id,
          token: resp.token,
          expiresAt: expires,
          mintedAt: new Date(),
          command: renderCommand(
            resp.token,
            resp.bootstrap_host,
            resp.fingerprint,
            resp.worker_id,
            resp.worker_name,
          ),
        });
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        setState({ kind: 'mint_error', message, lastName: name });
      }
    })();
  };

  // Countdown for expiry → transition to State 5.
  useEffect(() => {
    if (state.kind !== 'ready' && state.kind !== 'timeout_hint') return;
    const stateRef = state;
    const checkExpiry = () => {
      if (new Date() > stateRef.expiresAt) {
        setState({ kind: 'token_expired' });
      }
    };
    const id = setInterval(checkExpiry, 5_000);
    return () => clearInterval(id);
  }, [state]);

  // 5-min timeout → State 6 hint (only fires from Ready).
  useEffect(() => {
    if (state.kind !== 'ready') return;
    const elapsed = Date.now() - state.mintedAt.getTime();
    const remaining = 5 * 60_000 - elapsed;
    if (remaining <= 0) {
      setState({
        kind: 'timeout_hint',
        tokenID: state.tokenID,
        token: state.token,
        expiresAt: state.expiresAt,
        command: state.command,
      });
      return;
    }
    const id = setTimeout(() => {
      // Re-check state at fire time (might have transitioned away).
      setState((s) =>
        s.kind === 'ready'
          ? {
              kind: 'timeout_hint',
              tokenID: s.tokenID,
              token: s.token,
              expiresAt: s.expiresAt,
              command: s.command,
            }
          : s,
      );
    }, remaining);
    return () => clearTimeout(id);
  }, [state]);

  // SSE wire-up for worker.enrolled → State 3. useSSE dispatches the
  // DOM event "agent-center:worker-enrolled" whenever it sees the
  // backend `workforce.worker.enrolled` SSE message. Backend payload
  // currently ships {worker_id, capabilities}; the success card shows
  // only those (no host/os/version placeholders — see PD note in
  // task #44 thread: empty dashes look like a failed connect).
  useEffect(() => {
    if (state.kind !== 'ready' && state.kind !== 'timeout_hint') return;
    const handler = (ev: Event) => {
      const detail = (ev as CustomEvent).detail as {
        worker_id?: string;
        name?: string;
        capabilities?: string[];
      };
      setState({
        kind: 'success',
        worker: {
          id: detail.worker_id || 'unknown',
          name: detail.name || detail.worker_id || 'unknown',
          capabilities: Array.isArray(detail.capabilities) ? detail.capabilities : [],
        },
      });
    };
    window.addEventListener('agent-center:worker-enrolled', handler);
    return () => window.removeEventListener('agent-center:worker-enrolled', handler);
  }, [state]);

  // Auto-revoke unused token on Modal close (UI § 9 D2). Revokes by
  // the token's AdminToken ID returned by mint-enroll (not by
  // plaintext — the backend never sees plaintext after mint).
  const handleClose = () => {
    if (state.kind === 'ready' || state.kind === 'timeout_hint') {
      void revokeEnrollToken(state.tokenID);
    }
    onClose();
  };

  const handleAddAnother = () => {
    // Restart the flow at name_prompt — the next worker needs its
    // own friendly name (and gets a fresh server-generated id).
    setState({ kind: 'name_prompt', name: '' });
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="add-worker-modal"
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-2xl rounded-lg bg-white p-6 shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Add a Worker</h2>
          <button
            type="button"
            className="text-slate-400 hover:text-slate-700"
            onClick={handleClose}
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
          onAddAnother={handleAddAnother}
          onClose={handleClose}
        />
      </div>
    </div>
  );
}

interface BodyProps {
  state: ModalState;
  onSubmitName: (name: string) => void;
  onNameChange: (name: string) => void;
  onAddAnother: () => void;
  onClose: () => void;
}

function ModalBody({ state, onSubmitName, onNameChange, onAddAnother, onClose }: BodyProps): React.ReactElement {
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
            className="mb-1 block text-sm font-medium text-slate-700"
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
            className="block w-full rounded border border-slate-300 px-3 py-2 text-sm focus:border-blue-500"
          />
          <p className="mt-1 text-xs text-slate-500">
            Use a unique name (e.g. <code className="font-mono">test-1</code>,{' '}
            <code className="font-mono">tenant-foo</code>) so you can spot this worker in Fleet.
            You can rename it later from the Fleet row.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-50"
              onClick={onClose}
              data-testid="modal-name-prompt-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-slate-300"
              data-testid="modal-name-prompt-submit"
            >
              Generate install command
            </button>
          </div>
        </form>
      );
    }
    case 'minting':
      return (
        <div className="py-8 text-center" data-testid="modal-state-minting">
          <p className="text-sm text-slate-600">Preparing your worker install command...</p>
          <div className="mt-4 inline-block h-6 w-6 animate-spin rounded-full border-2 border-slate-300 border-t-blue-600" />
        </div>
      );
    case 'mint_error':
      return (
        <div data-testid="modal-state-mint-error">
          <p className="mb-3 text-sm font-medium text-danger">
            Could not mint an enroll token.
          </p>
          <p className="mb-3 text-sm text-slate-700">{state.message}</p>
          <p className="mb-4 text-xs text-slate-500">
            Common causes: admin TCP listener is not enabled on the
            center (set <code className="font-mono">server.admin_tcp_listen</code> in the config,
            e.g. <code className="font-mono">0.0.0.0:7300</code>) or the AdminToken service is not
            wired. Check the server logs.
          </p>
          <div className="flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-50"
              onClick={onClose}
              data-testid="modal-mint-error-close"
            >
              Close
            </button>
            <button
              type="button"
              className="rounded bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700"
              onClick={() => onSubmitName(state.lastName)}
              data-testid="modal-mint-error-retry"
            >
              Try again
            </button>
          </div>
        </div>
      );
    case 'ready':
    case 'timeout_hint': {
      const isTimeout = state.kind === 'timeout_hint';
      return (
        <div data-testid={isTimeout ? 'modal-state-timeout-hint' : 'modal-state-ready'}>
          <p className="mb-3 text-sm text-slate-600">
            On your worker machine, make sure the AgentCenter tarball is extracted, then run:
          </p>
          <CommandBlock command={state.command} />
          <ExpiresHint expiresAt={state.expiresAt} />
          {!isTimeout && (
            <p className="mt-4 flex items-center text-sm text-slate-600">
              <span className="mr-2 inline-block h-4 w-4 animate-pulse rounded-full bg-blue-400" />
              Waiting for worker to connect…
            </p>
          )}
          {isTimeout && (
            <div className="mt-4 rounded border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800">
              <p className="font-medium">Worker hasn't connected yet (5 min).</p>
              <p className="mt-1">Common causes:</p>
              <ul className="ml-4 list-disc">
                <li>Network can't reach center at the address above</li>
                <li>Firewall blocking TCP 7300</li>
                <li>Worker tarball not yet extracted</li>
              </ul>
            </div>
          )}
        </div>
      );
    }
    case 'success':
      return (
        <div data-testid="modal-state-success">
          <p className="mb-3 text-sm font-medium text-emerald-600">Worker connected.</p>
          <dl className="grid grid-cols-2 gap-x-4 gap-y-1 rounded border border-slate-200 bg-slate-50 p-3 text-sm">
            <dt className="text-slate-500">Name</dt>
            <dd className="text-sm" data-testid="modal-success-worker-name">
              {state.worker.name}
            </dd>
            <dt className="text-slate-500">ID</dt>
            <dd className="font-mono text-xs" data-testid="modal-success-worker-id">
              {state.worker.id}
            </dd>
            {state.worker.capabilities.length > 0 && (
              <>
                <dt className="text-slate-500">Capabilities</dt>
                <dd className="text-xs" data-testid="modal-success-capabilities">
                  {state.worker.capabilities.join(', ')}
                </dd>
              </>
            )}
            <dt className="text-slate-500">Status</dt>
            <dd>Online</dd>
          </dl>
          <p className="mt-3 text-xs text-slate-500">
            Your worker is now visible in the Fleet table.
          </p>
          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-slate-300 px-3 py-1.5 text-sm hover:bg-slate-50"
              onClick={onAddAnother}
              data-testid="modal-add-another"
            >
              + Add another worker
            </button>
            <button
              type="button"
              className="rounded bg-blue-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-blue-700"
              onClick={onClose}
              data-testid="modal-done"
            >
              Done
            </button>
          </div>
        </div>
      );
    case 'token_used':
      return (
        <div data-testid="modal-state-token-used">
          <p className="mb-2 text-sm font-medium text-amber-700">This token was just used by another worker.</p>
          <p className="mb-4 text-xs text-slate-600">
            Generate a new token to add another worker.
          </p>
          <button
            type="button"
            className="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
            onClick={onAddAnother}
            data-testid="modal-generate-new"
          >
            Generate new token
          </button>
        </div>
      );
    case 'token_expired':
      return (
        <div data-testid="modal-state-token-expired">
          <p className="mb-2 text-sm font-medium text-slate-700">Token expired (30 min cap).</p>
          <p className="mb-4 text-xs text-slate-600">
            Generate a new token if you'd still like to add this worker.
          </p>
          <button
            type="button"
            className="rounded bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-700"
            onClick={onAddAnother}
            data-testid="modal-generate-new"
          >
            Generate new token
          </button>
        </div>
      );
  }
}

function CommandBlock({ command }: { command: string }): React.ReactElement {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard.writeText(command);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  return (
    <div className="relative">
      <pre className="overflow-x-auto rounded bg-slate-900 p-3 text-xs text-slate-100">
        <code data-testid="modal-command">{command}</code>
      </pre>
      <button
        type="button"
        className="absolute right-2 top-2 rounded bg-slate-700 px-2 py-1 text-xs text-white hover:bg-slate-600"
        onClick={copy}
        data-testid="modal-copy-btn"
      >
        {copied ? 'Copied!' : 'Copy'}
      </button>
    </div>
  );
}

function ExpiresHint({ expiresAt }: { expiresAt: Date }): React.ReactElement {
  const text = useMemo(() => {
    const remaining = Math.max(0, expiresAt.getTime() - Date.now());
    const minutes = Math.floor(remaining / 60_000);
    const hhmm = expiresAt.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
    return `Token expires at ${hhmm} (${minutes} min remaining)`;
  }, [expiresAt]);
  return <p className="mt-2 text-xs text-slate-500">{text}</p>;
}
