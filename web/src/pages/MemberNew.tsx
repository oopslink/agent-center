import React, { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useAddMember, useAddAgentMember } from '@/api/members';
import { useWorkers } from '@/api/workers';
import { ApiError } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';
import { EntitySelect } from '@/components/EntitySelect';

// MemberNew backs /organizations/{slug}/members/new?kind=agent|user.
// Acceptance plan §3 references /members/new?kind=agent as the Add Agent entry.
export default function MemberNew(): React.ReactElement {
  const [params] = useSearchParams();
  const kind = params.get('kind') === 'user' ? 'user' : 'agent';
  const navigate = useNavigate();
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';

  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [role, setRole] = useState('member');
  // v2.7 #157: Members→Add Agent is one step — also create the execution Agent
  // (model/cli + the worker it runs on). worker_id is required for an agent.
  const [model, setModel] = useState('');
  // v2.7 #181 / FINDING-F: only claude-code is executable — single-option
  // select (codex/opencode become selectable in v2.8 #180).
  const [cli, setCli] = useState('claude-code');
  const [workerID, setWorkerID] = useState('');
  const [error, setError] = useState('');
  const [tempPasscode, setTempPasscode] = useState('');

  const addUser = useAddMember();
  const addAgent = useAddAgentMember();
  const workers = useWorkers();
  const pending = addUser.isPending || addAgent.isPending;

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (kind === 'agent') {
      addAgent.mutate(
        {
          display_name: displayName.trim(),
          description: description.trim(),
          role,
          model: model.trim() || undefined,
          cli,
          worker_id: workerID || undefined,
        },
        {
          // v2.7 #185/#77: business-layer agent id = member identity_id (entity
          // id is internal-only). Navigate to AgentDetail by identity_id
          // (GET /api/agents/{identity_id} resolves via the member→entity bridge).
          onSuccess: (res) =>
            navigate(res.identity_id ? `${base}/agents/${res.identity_id}` : `${base}/members/agents`),
          onError: (err) => setError(err instanceof ApiError ? err.message : 'Create failed'),
        },
      );
    } else {
      addUser.mutate(
        { display_name: displayName.trim(), role },
        {
          onSuccess: (res) => {
            if (res.temp_passcode) setTempPasscode(res.temp_passcode);
            else navigate(`${base}/members/humans`);
          },
          onError: (err) => setError(err instanceof ApiError ? err.message : 'Create failed'),
        },
      );
    }
  };

  if (tempPasscode) {
    return (
      <section className="space-y-4 max-w-md" data-testid="page-MemberNew">
        <h2 className="text-xl font-semibold text-text-primary">User created</h2>
        <p className="text-sm text-text-secondary">Temporary passcode (shown once — hand it over now):</p>
        <div className="rounded bg-bg-subtle border border-border-strong px-3 py-3 text-center">
          <code className="text-2xl font-mono tracking-widest text-text-primary">{tempPasscode}</code>
        </div>
        <button
          type="button"
          onClick={() => navigate(`${base}/members/humans`)}
          className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          I've saved it, back to members
        </button>
      </section>
    );
  }

  return (
    <section className="space-y-4 max-w-md" data-testid="page-MemberNew">
      <h2 className="text-xl font-semibold text-text-primary">
        {kind === 'agent' ? 'Add agent' : 'Add user'}
      </h2>
      {error && (
        <div role="alert" className="rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}
      <form onSubmit={handleSubmit} noValidate className="space-y-3 bg-bg-elevated border border-border rounded-lg p-4">
        <div className="space-y-1">
          <label htmlFor="mn-name" className="block text-sm text-text-primary">Display name</label>
          <input
            id="mn-name"
            type="text"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
            placeholder={kind === 'agent' ? 'Agent name' : 'User name'}
          />
        </div>
        {kind === 'agent' && (
          <div className="space-y-1">
            <label htmlFor="mn-desc" className="block text-sm text-text-primary">Description (optional)</label>
            <input
              id="mn-desc"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
            />
          </div>
        )}
        {kind === 'agent' && (
          <>
            {/* v2.7 #157: execution-agent fields — one-step create runs the agent on a worker. */}
            <div className="space-y-1">
              <span className="block text-sm text-text-primary">Run on worker</span>
              {/* v2.7 #191: shared searchable EntitySelect instead of a raw <select>. */}
              <EntitySelect
                testId="mn-worker"
                value={workerID}
                onChange={setWorkerID}
                options={(workers.data ?? []).map((w) => ({
                  value: w.worker_id,
                  label: w.name || w.worker_id,
                }))}
                placeholder="Select a worker…"
                searchPlaceholder="Search workers…"
                ariaLabel="Run on worker"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="mn-model" className="block text-sm text-text-primary">Model (optional)</label>
              <input
                id="mn-model"
                type="text"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="e.g. claude-opus-4"
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="mn-cli" className="block text-sm text-text-primary">CLI</label>
              <select
                id="mn-cli"
                value={cli}
                onChange={(e) => setCli(e.target.value)}
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              >
                <option value="claude-code">claude-code</option>
              </select>
              <p className="text-xs text-text-muted">v2.7 runs claude-code only (codex/opencode coming in v2.8).</p>
            </div>
          </>
        )}
        <div className="space-y-1">
          <label htmlFor="mn-role" className="block text-sm text-text-primary">Role</label>
          <select
            id="mn-role"
            value={role}
            onChange={(e) => setRole(e.target.value)}
            className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary"
          >
            <option value="member">member</option>
            <option value="admin">admin</option>
            {kind === 'user' && <option value="owner">owner</option>}
          </select>
        </div>
        <div className="flex gap-2 justify-end">
          <button
            type="button"
            onClick={() => navigate(`${base}/members/${kind === 'agent' ? 'agents' : 'humans'}`)}
            className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={pending || !displayName.trim() || (kind === 'agent' && !workerID)}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          >
            {pending ? 'Creating…' : 'Create'}
          </button>
        </div>
      </form>
    </section>
  );
}
