import React, { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { useMembers } from '@/api/members';
import { useAgents } from '@/api/agents';
import { useOptionalOrgContext } from '@/OrgContext';

export default function MembersAgents(): React.ReactElement {
  const members = useMembers();
  const agents = useAgents();
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';
  // Use `kind` field from v2.6 member response; fall back to identity_id prefix for compatibility.
  const agentMembers = (members.data ?? []).filter(
    (m) => m.kind === 'agent' || m.identity_id.startsWith('agent-') || m.identity_id.startsWith('agent:'),
  );
  // v2.7 #157: resolve agent member → its execution Agent for the AgentDetail
  // link. The execution Agent carries identity_member_id == the member's
  // identity_id (the unified-create link). No match → no link yet (legacy
  // identity-only member or pre-#157 row).
  const agentIDByIdentity = useMemo(() => {
    const m = new Map<string, string>();
    for (const a of agents.data ?? []) {
      if (a.identity_member_id) m.set(a.identity_member_id, a.id);
    }
    return m;
  }, [agents.data]);

  return (
    <section className="space-y-4" data-testid="page-MembersAgents">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-text-primary">成员 — Agent</h2>
        <Link
          to={`${base}/members/new?kind=agent`}
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          添加 Agent
        </Link>
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
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">运行于</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">状态</th>
              </tr>
            </thead>
            <tbody>
              {agentMembers.map((m) => (
                <tr key={m.id} className="border-b border-border last:border-0">
                  <td className="py-2 px-3 text-sm font-mono">
                    {agentIDByIdentity.get(m.identity_id) ? (
                      <Link
                        to={`${base}/agents/${agentIDByIdentity.get(m.identity_id)}`}
                        className="text-brand hover:underline"
                        data-testid={`agent-member-link-${m.identity_id}`}
                      >
                        {m.display_name || m.identity_id}
                      </Link>
                    ) : (
                      <span className="text-text-primary">{m.display_name || m.identity_id}</span>
                    )}
                  </td>
                  <td className="py-2 px-3 text-sm text-text-secondary">{m.role}</td>
                  <td className="py-2 px-3 text-sm text-text-secondary">
                    {m.worker_id ? (
                      <span className="font-mono text-xs">running on {m.worker_id}</span>
                    ) : (
                      <span className="text-text-muted italic">未绑定 worker</span>
                    )}
                  </td>
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
