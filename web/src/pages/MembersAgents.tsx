import React, { useState } from 'react';
import { useMembers, useAddAgentMember } from '@/api/members';
import { ApiError } from '@/api/client';

function AddAgentModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [role, setRole] = useState('member');
  const [error, setError] = useState('');
  const add = useAddAgentMember();

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    add.mutate(
      { display_name: displayName.trim(), description: description.trim(), role },
      {
        onSuccess: () => onClose(),
        onError: (err) => {
          if (err instanceof ApiError) setError(err.message);
          else setError('添加失败');
        },
      },
    );
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      role="dialog"
      aria-modal="true"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-sm rounded-xl bg-bg-elevated border border-border p-6 shadow-[var(--shadow-3)]">
        <h2 className="text-base font-semibold text-text-primary mb-4">添加 Agent</h2>
        <p className="text-xs text-text-muted mb-3">
          创建新 Agent 身份。Agent 后续会被 AgentInstance 绑定（worker 部署时）。
        </p>
        {error && (
          <div role="alert" className="mb-3 rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
            {error}
          </div>
        )}
        <form onSubmit={handleSubmit} noValidate className="space-y-3">
          <div className="space-y-1">
            <label htmlFor="add-agent-name" className="block text-sm text-text-primary">显示名称</label>
            <input
              id="add-agent-name"
              type="text"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="Agent 显示名称"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="add-agent-desc" className="block text-sm text-text-primary">描述（可选）</label>
            <input
              id="add-agent-desc"
              type="text"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="Agent 用途简述"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="add-agent-role" className="block text-sm text-text-primary">角色</label>
            <select
              id="add-agent-role"
              value={role}
              onChange={(e) => setRole(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm bg-bg-elevated text-text-primary"
            >
              <option value="member">member</option>
              <option value="admin">admin</option>
            </select>
          </div>
          <div className="flex gap-2 justify-end pt-1">
            <button type="button" onClick={onClose} className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle">取消</button>
            <button
              type="submit"
              disabled={add.isPending || !displayName.trim()}
              className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            >
              {add.isPending ? '创建中…' : '创建 Agent'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default function MembersAgents(): React.ReactElement {
  const members = useMembers();
  const [addModalOpen, setAddModalOpen] = useState(false);
  // Use `kind` field from v2.6 member response; fall back to identity_id prefix for compatibility.
  const agentMembers = (members.data ?? []).filter(
    (m) => m.kind === 'agent' || m.identity_id.startsWith('agent-') || m.identity_id.startsWith('agent:'),
  );

  return (
    <section className="space-y-4" data-testid="page-MembersAgents">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-text-primary">成员 — Agent</h2>
        <button
          type="button"
          onClick={() => setAddModalOpen(true)}
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          添加 Agent
        </button>
      </div>

      {members.isLoading && <p className="text-sm text-text-muted">加载中…</p>}

      {!members.isLoading && agentMembers.length === 0 && (
        <p className="text-sm text-text-muted">暂无 Agent 成员</p>
      )}

      {agentMembers.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-border">
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Identity</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">角色</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">状态</th>
              </tr>
            </thead>
            <tbody>
              {agentMembers.map((m) => (
                <tr key={m.id} className="border-b border-border last:border-0">
                  <td className="py-2 px-3 text-sm text-text-primary font-mono">{m.identity_id}</td>
                  <td className="py-2 px-3 text-sm text-text-secondary">{m.role}</td>
                  <td className="py-2 px-3 text-sm">
                    <span className={m.status === 'joined' ? 'text-success' : 'text-text-muted'}>
                      {m.status === 'joined' ? '已加入' : '已禁用'}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {addModalOpen && <AddAgentModal onClose={() => setAddModalOpen(false)} />}
    </section>
  );
}
