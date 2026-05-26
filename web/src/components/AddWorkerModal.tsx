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

type ModalState =
  | { kind: 'minting' }
  | { kind: 'ready'; token: string; expiresAt: Date; mintedAt: Date; command: string }
  | { kind: 'success'; worker: { id: string; host: string; os: string } }
  | { kind: 'token_used' }
  | { kind: 'token_expired' }
  | { kind: 'timeout_hint'; token: string; expiresAt: Date; command: string };

interface Props {
  onClose: () => void;
}

// MintResponse mirrors what POST /api/admintoken/mint-enroll returns.
// Backend ST for this endpoint is gated by A3 (#37); for v0 we fall
// back to a mock if the call fails so the Modal flow can be exercised
// in the dev env before the endpoint exists.
interface MintResponse {
  token: string;
  expires_at: string; // RFC3339
}

async function mintEnrollToken(): Promise<MintResponse> {
  const resp = await fetch('/api/admintoken/mint-enroll', { method: 'POST' });
  if (!resp.ok) {
    throw new Error(`mint failed: HTTP ${resp.status}`);
  }
  return resp.json();
}

async function revokeEnrollToken(token: string): Promise<void> {
  // Best-effort; backend endpoint may not be live yet.
  try {
    await fetch(`/api/admintoken/revoke?token_hint=${encodeURIComponent(token.slice(0, 12))}`,
      { method: 'POST' });
  } catch {
    // ignore
  }
}

// renderCommand assembles the operator-facing install command from
// the bootstrap URL + token returned by mint. The bootstrap URL is
// constructed client-side because the server can't infer the public
// host the worker will dial.
function renderCommand(token: string, bootstrapHost: string): string {
  return `cd agent-center-v2.4.0-darwin-arm64
./install worker \\
  --bootstrap=tcp://${bootstrapHost}:7300 \\
  --token=${token}`;
}

export function AddWorkerModal({ onClose }: Props): React.ReactElement {
  const [state, setState] = useState<ModalState>({ kind: 'minting' });

  // Mint on mount.
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const resp = await mintEnrollToken();
        if (cancelled) return;
        const expires = new Date(resp.expires_at);
        setState({
          kind: 'ready',
          token: resp.token,
          expiresAt: expires,
          mintedAt: new Date(),
          command: renderCommand(resp.token, window.location.hostname || 'home.lan'),
        });
      } catch {
        if (cancelled) return;
        // Fallback for dev env when mint endpoint not yet live.
        const tok = 'acat_dev_placeholder_xxxxxxxx';
        const expires = new Date(Date.now() + 30 * 60_000);
        setState({
          kind: 'ready',
          token: tok,
          expiresAt: expires,
          mintedAt: new Date(),
          command: renderCommand(tok, window.location.hostname || 'home.lan'),
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

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
              token: s.token,
              expiresAt: s.expiresAt,
              command: s.command,
            }
          : s,
      );
    }, remaining);
    return () => clearTimeout(id);
  }, [state]);

  // SSE wire-up for worker.enrolled → State 3.
  // F3 (#43) plumbs the actual subscription; for v0 we listen for a
  // custom DOM event that F3 dispatches when it sees workforce.worker.
  // enrolled on the SSE stream.
  useEffect(() => {
    if (state.kind !== 'ready' && state.kind !== 'timeout_hint') return;
    const handler = (ev: Event) => {
      const detail = (ev as CustomEvent).detail as {
        worker_id?: string;
        host?: string;
        os?: string;
      };
      setState({
        kind: 'success',
        worker: {
          id: detail.worker_id || 'unknown',
          host: detail.host || 'unknown',
          os: detail.os || 'unknown',
        },
      });
    };
    window.addEventListener('agent-center:worker-enrolled', handler);
    return () => window.removeEventListener('agent-center:worker-enrolled', handler);
  }, [state]);

  // Auto-revoke unused token on Modal close (UI § 9 D2).
  const handleClose = () => {
    if (state.kind === 'ready' || state.kind === 'timeout_hint' || state.kind === 'token_expired') {
      const tok =
        state.kind === 'token_expired' ? '' : (state as { token: string }).token;
      if (tok) void revokeEnrollToken(tok);
    }
    onClose();
  };

  const handleGenerateNew = () => {
    setState({ kind: 'minting' });
    void (async () => {
      try {
        const resp = await mintEnrollToken();
        const expires = new Date(resp.expires_at);
        setState({
          kind: 'ready',
          token: resp.token,
          expiresAt: expires,
          mintedAt: new Date(),
          command: renderCommand(resp.token, window.location.hostname || 'home.lan'),
        });
      } catch {
        const tok = 'acat_dev_placeholder_xxxxxxxx';
        setState({
          kind: 'ready',
          token: tok,
          expiresAt: new Date(Date.now() + 30 * 60_000),
          mintedAt: new Date(),
          command: renderCommand(tok, window.location.hostname || 'home.lan'),
        });
      }
    })();
  };

  const handleAddAnother = () => {
    handleGenerateNew();
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
          onGenerateNew={handleGenerateNew}
          onAddAnother={handleAddAnother}
          onClose={handleClose}
        />
      </div>
    </div>
  );
}

interface BodyProps {
  state: ModalState;
  onGenerateNew: () => void;
  onAddAnother: () => void;
  onClose: () => void;
}

function ModalBody({ state, onGenerateNew, onAddAnother, onClose }: BodyProps): React.ReactElement {
  switch (state.kind) {
    case 'minting':
      return (
        <div className="py-8 text-center" data-testid="modal-state-minting">
          <p className="text-sm text-slate-600">Preparing your worker install command...</p>
          <div className="mt-4 inline-block h-6 w-6 animate-spin rounded-full border-2 border-slate-300 border-t-blue-600" />
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
            <dd className="font-mono text-xs">{state.worker.id}</dd>
            <dt className="text-slate-500">Host</dt>
            <dd>{state.worker.host}</dd>
            <dt className="text-slate-500">OS</dt>
            <dd>{state.worker.os}</dd>
            <dt className="text-slate-500">Status</dt>
            <dd>● Online</dd>
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
            onClick={onGenerateNew}
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
            onClick={onGenerateNew}
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
