import React from 'react';
import { useMembers } from '@/api/members';

export default function MembersAgents(): React.ReactElement {
  const members = useMembers();
  // Use `kind` field from v2.6 member response; fall back to identity_id prefix for compatibility.
  const agentMembers = (members.data ?? []).filter(
    (m) => m.kind === 'agent' || m.identity_id.startsWith('agent-') || m.identity_id.startsWith('agent:'),
  );

  return (
    <section className="space-y-4" data-testid="page-MembersAgents">
      <h2 className="text-xl font-semibold text-text-primary">成员 — Agent</h2>
      <p className="text-sm text-text-muted">
        通过 <code className="font-mono text-xs">agent-center agent add</code> CLI 命令添加 Agent 身份。
      </p>

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
    </section>
  );
}
