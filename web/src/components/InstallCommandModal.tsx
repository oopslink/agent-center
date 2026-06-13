// InstallCommandModal — v2.5-F2 (#54).
//
// Renders the install command for a previously-added Worker so the
// operator can copy it onto the worker machine. Two entry points
// from the Environment worker row:
//   - "Show install command"  → mode: 'show' → GET /api/workers/{id}/install-command
//   - "Re-mint install command" → mode: 'remint' (after confirm) → POST .../install-command/re-mint
//
// Both endpoints share the response shape (B2/B3 keep this in sync),
// so the renderer below is the same once we have the payload.
import React, { useEffect, useState } from 'react';
import { withOrgSlug } from '@/api/client';

interface InstallCommandPayload {
  id: string;
  token: string;
  expires_at: string;
  fingerprint: string;
  bootstrap_host: string;
  worker_id: string;
  worker_name: string;
}

type Mode = 'show' | 'remint';

interface Props {
  workerID: string;
  mode: Mode;
  onClose: () => void;
}

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; payload: InstallCommandPayload }
  | { kind: 'expired'; message: string }
  | { kind: 'gone'; message: string } // 401 no_active_enroll_token
  | { kind: 'no_master_key'; message: string }
  | { kind: 'conflict'; message: string } // 409 worker_already_online (re-mint)
  | { kind: 'not_found'; message: string }
  | { kind: 'error'; message: string };

async function fetchInstall(workerID: string, mode: Mode): Promise<LoadState> {
  const url = `/api/workers/${encodeURIComponent(workerID)}/install-command${
    mode === 'remint' ? '/re-mint' : ''
  }`;
  const init: RequestInit = mode === 'remint' ? { method: 'POST' } : { method: 'GET' };
  try {
    const resp = await fetch(withOrgSlug(url), init);
    if (resp.ok) {
      const payload = (await resp.json()) as InstallCommandPayload;
      return { kind: 'ready', payload };
    }
    let code = '';
    let message = `HTTP ${resp.status}`;
    try {
      const body = (await resp.json()) as { error?: string; message?: string };
      code = body.error ?? '';
      if (body.message) message = body.message;
    } catch {
      // empty/non-JSON body — keep status fallback
    }
    switch (code) {
      case 'enroll_token_expired':
        return { kind: 'expired', message };
      case 'no_active_enroll_token':
        return { kind: 'gone', message };
      case 'show_install_no_master_key':
        return { kind: 'no_master_key', message };
      case 'worker_already_online':
        return { kind: 'conflict', message };
      case 'worker_not_found':
        return { kind: 'not_found', message };
      default:
        return { kind: 'error', message };
    }
  } catch (err) {
    return { kind: 'error', message: err instanceof Error ? err.message : String(err) };
  }
}

// renderCommand mirrors the AddWorkerModal v2.4 helper: assembles
// the operator-facing install line so the SPA never re-derives
// fingerprint/bootstrap from elsewhere.
function renderCommand(p: InstallCommandPayload): string {
  const lines = [
    `./install worker \\`,
    `  --bootstrap=tcp://${p.bootstrap_host} \\`,
    `  --server-fingerprint=${p.fingerprint} \\`,
    `  --worker-id=${p.worker_id} \\`,
  ];
  if (p.worker_name && p.worker_name !== p.worker_id) {
    lines.push(`  --worker-name=${shellQuote(p.worker_name)} \\`);
  }
  lines.push(`  --token=${p.token}`);
  return lines.join('\n');
}

function shellQuote(s: string): string {
  return "'" + s.replace(/'/g, "'\\''") + "'";
}

export function InstallCommandModal({ workerID, mode, onClose }: Props): React.ReactElement {
  const [state, setState] = useState<LoadState>({ kind: 'loading' });

  useEffect(() => {
    let active = true;
    void fetchInstall(workerID, mode).then((s) => {
      if (active) setState(s);
    });
    return () => {
      active = false;
    };
  }, [workerID, mode]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="install-command-modal"
      role="dialog"
      aria-modal="true"
    >
      <div className="w-full max-w-2xl rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">
            {mode === 'remint' ? 'Re-minted install command' : 'Install command'}
          </h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="install-command-close"
          >
            X
          </button>
        </div>
        <Body state={state} workerID={workerID} onClose={onClose} />
      </div>
    </div>
  );
}

function Body({
  state,
  workerID,
  onClose,
}: {
  state: LoadState;
  workerID: string;
  onClose: () => void;
}): React.ReactElement {
  switch (state.kind) {
    case 'loading':
      return (
        <div className="py-8 text-center" data-testid="install-command-loading">
          <p className="text-sm text-text-secondary">Loading...</p>
        </div>
      );
    case 'ready':
      return <ReadyBody payload={state.payload} onClose={onClose} />;
    case 'expired':
    case 'gone':
      return (
        <ReMintPromptBody
          message={state.message}
          workerID={workerID}
          dataTestID={state.kind === 'expired' ? 'install-command-expired' : 'install-command-gone'}
          onClose={onClose}
        />
      );
    case 'no_master_key':
      return (
        <div data-testid="install-command-no-master-key">
          <p className="mb-3 text-sm font-medium text-warning">Server not configured.</p>
          <p className="mb-4 text-xs text-text-secondary">{state.message}</p>
          <CloseButton onClose={onClose} />
        </div>
      );
    case 'conflict':
      return (
        <div data-testid="install-command-conflict">
          <p className="mb-3 text-sm font-medium text-warning">Worker is already online.</p>
          <p className="mb-4 text-xs text-text-secondary">{state.message}</p>
          <CloseButton onClose={onClose} />
        </div>
      );
    case 'not_found':
      return (
        <div data-testid="install-command-not-found">
          <p className="mb-3 text-sm font-medium text-danger">Worker not found.</p>
          <p className="mb-4 text-xs text-text-secondary">{state.message}</p>
          <CloseButton onClose={onClose} />
        </div>
      );
    case 'error':
      return (
        <div data-testid="install-command-error">
          <p className="mb-3 text-sm font-medium text-danger">Could not load install command.</p>
          <p className="mb-4 text-xs text-text-secondary">{state.message}</p>
          <CloseButton onClose={onClose} />
        </div>
      );
  }
}

function ReadyBody({
  payload,
  onClose,
}: {
  payload: InstallCommandPayload;
  onClose: () => void;
}): React.ReactElement {
  const command = renderCommand(payload);
  const expires = new Date(payload.expires_at);
  const remainingMin = Math.max(0, Math.floor((expires.getTime() - Date.now()) / 60_000));
  return (
    <div data-testid="install-command-ready">
      <p className="mb-3 text-sm text-text-secondary">
        On your worker machine, make sure the AgentCenter tarball is extracted, then run:
      </p>
      <CommandBlock command={command} />
      <p className="mt-2 text-xs text-text-muted">
        Token expires at {expires.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' })} (
        {remainingMin} min remaining).
      </p>
      <div className="mt-4 flex justify-end">
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={onClose}
          data-testid="install-command-done"
        >
          Done
        </button>
      </div>
    </div>
  );
}

function ReMintPromptBody({
  message,
  workerID,
  dataTestID,
  onClose,
}: {
  message: string;
  workerID: string;
  dataTestID: string;
  onClose: () => void;
}): React.ReactElement {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Inline transition: clicking Re-mint hits the re-mint endpoint
  // and replaces the body with the ReadyBody on success — no need
  // to spawn a second Modal layer for the same workflow.
  const [reMinted, setReMinted] = useState<InstallCommandPayload | null>(null);

  if (reMinted) {
    return <ReadyBody payload={reMinted} onClose={onClose} />;
  }

  const handleReMint = async () => {
    setBusy(true);
    setError(null);
    const result = await fetchInstall(workerID, 'remint');
    setBusy(false);
    if (result.kind === 'ready') {
      setReMinted(result.payload);
      return;
    }
    setError(result.kind === 'conflict'
      ? 'Worker is already online — remove it from Environment first if you want to reset.'
      : result.kind === 'not_found'
        ? 'Worker not found.'
        : `Re-mint failed: ${'message' in result ? result.message : 'unknown'}`);
  };

  return (
    <div data-testid={dataTestID}>
      <p className="mb-3 text-sm font-medium text-text-primary">No active install command.</p>
      <p className="mb-4 text-xs text-text-secondary">{message}</p>
      {error && <p className="mb-3 text-xs text-danger" data-testid="install-command-remint-error">{error}</p>}
      <div className="flex justify-end gap-2">
        <button
          type="button"
          className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
          onClick={onClose}
          data-testid="install-command-close-empty"
        >
          Close
        </button>
        <button
          type="button"
          disabled={busy}
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
          onClick={() => void handleReMint()}
          data-testid="install-command-remint"
        >
          {busy ? 'Re-minting...' : 'Re-mint install command'}
        </button>
      </div>
    </div>
  );
}

function CloseButton({ onClose }: { onClose: () => void }): React.ReactElement {
  return (
    <div className="flex justify-end">
      <button
        type="button"
        className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
        onClick={onClose}
      >
        Close
      </button>
    </div>
  );
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
      {/* Terminal-style block; intentionally dark in both modes (mimics
          a shell window). Not a dark-mode-token candidate. */}
      <pre className="overflow-x-auto rounded bg-slate-900 p-3 text-xs text-slate-100"> {/* raw-color-ok: terminal block */}
        <code data-testid="install-command-text">{command}</code>
      </pre>
      <button
        type="button"
        className="absolute right-2 top-2 rounded bg-slate-700 px-2 py-1 text-xs text-white hover:bg-slate-600" /* raw-color-ok: terminal copy btn */
        onClick={copy}
        data-testid="install-command-copy"
      >
        {copied ? 'Copied!' : 'Copy'}
      </button>
    </div>
  );
}
