import React, { useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useAddMember, useAddAgentMember } from '@/api/members';
import { useWorkers } from '@/api/workers';
import { ApiError } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';

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
  const [cli, setCli] = useState('');
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
          cli: cli.trim() || undefined,
          worker_id: workerID || undefined,
        },
        {
          // Unified create returns the execution agent's id → open its management
          // page (AgentDetail) directly; fall back to the agents list otherwise.
          onSuccess: (res) =>
            navigate(res.agent_id ? `${base}/agents/${res.agent_id}` : `${base}/members/agents`),
          onError: (err) => setError(err instanceof ApiError ? err.message : '创建失败'),
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
          onError: (err) => setError(err instanceof ApiError ? err.message : '创建失败'),
        },
      );
    }
  };

  if (tempPasscode) {
    return (
      <section className="space-y-4 max-w-md" data-testid="page-MemberNew">
        <h2 className="text-xl font-semibold text-text-primary">用户创建成功</h2>
        <p className="text-sm text-text-secondary">临时密码（只显示一次，请立即转交）：</p>
        <div className="rounded bg-bg-subtle border border-border-strong px-3 py-3 text-center">
          <code className="text-2xl font-mono tracking-widest text-text-primary">{tempPasscode}</code>
        </div>
        <button
          type="button"
          onClick={() => navigate(`${base}/members/humans`)}
          className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          我已记下，返回成员列表
        </button>
      </section>
    );
  }

  return (
    <section className="space-y-4 max-w-md" data-testid="page-MemberNew">
      <h2 className="text-xl font-semibold text-text-primary">
        {kind === 'agent' ? '添加 Agent' : '添加用户'}
      </h2>
      {error && (
        <div role="alert" className="rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
          {error}
        </div>
      )}
      <form onSubmit={handleSubmit} noValidate className="space-y-3 bg-bg-elevated border border-border rounded-lg p-4">
        <div className="space-y-1">
          <label htmlFor="mn-name" className="block text-sm text-text-primary">显示名称</label>
          <input
            id="mn-name"
            type="text"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
            placeholder={kind === 'agent' ? 'Agent 名称' : '用户名称'}
          />
        </div>
        {kind === 'agent' && (
          <div className="space-y-1">
            <label htmlFor="mn-desc" className="block text-sm text-text-primary">描述（可选）</label>
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
              <label htmlFor="mn-worker" className="block text-sm text-text-primary">运行 Worker</label>
              <select
                id="mn-worker"
                value={workerID}
                onChange={(e) => setWorkerID(e.target.value)}
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary"
              >
                <option value="">选择一个 worker…</option>
                {(workers.data ?? []).map((w) => (
                  <option key={w.worker_id} value={w.worker_id}>
                    {w.name || w.worker_id}
                  </option>
                ))}
              </select>
            </div>
            <div className="space-y-1">
              <label htmlFor="mn-model" className="block text-sm text-text-primary">模型（可选）</label>
              <input
                id="mn-model"
                type="text"
                value={model}
                onChange={(e) => setModel(e.target.value)}
                placeholder="如 claude-opus-4"
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              />
            </div>
            <div className="space-y-1">
              <label htmlFor="mn-cli" className="block text-sm text-text-primary">CLI（可选）</label>
              <input
                id="mn-cli"
                type="text"
                value={cli}
                onChange={(e) => setCli(e.target.value)}
                placeholder="如 claudecode"
                className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
              />
            </div>
          </>
        )}
        <div className="space-y-1">
          <label htmlFor="mn-role" className="block text-sm text-text-primary">角色</label>
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
            取消
          </button>
          <button
            type="submit"
            disabled={pending || !displayName.trim() || (kind === 'agent' && !workerID)}
            className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          >
            {pending ? '创建中…' : '创建'}
          </button>
        </div>
      </form>
    </section>
  );
}
