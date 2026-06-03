import React, { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { useMembers } from '@/api/members';
import { useAgents } from '@/api/agents';
import { useWorkers } from '@/api/workers';
import { useOptionalOrgContext } from '@/OrgContext';
import { EntityRef } from '@/components/EntityRef';

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
  // v2.7 #192: resolve worker_id → worker name for the "Running on" column.
  const workers = useWorkers();
  const workerNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const w of workers.data ?? []) m.set(w.worker_id, w.name || w.worker_id);
    return m;
  }, [workers.data]);

  return (
    <section className="space-y-4" data-testid="page-MembersAgents">
      <div className="flex items-center justify-between">
        <h2 className="text-xl font-semibold text-text-primary">Agents</h2>
        <Link
          to={`${base}/members/new?kind=agent`}
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
        >
          Add agent
        </Link>
      </div>

      {members.isLoading && <p className="text-sm text-text-muted">Loading…</p>}

      {!members.isLoading && agentMembers.length === 0 && (
        <p className="text-sm text-text-muted">No agent members yet</p>
      )}

      {agentMembers.length > 0 && (
        <div className="overflow-x-auto">
          <table className="w-full text-left">
            <thead>
              <tr className="border-b border-border">
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Identity</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Role</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Running on</th>
                <th className="py-2 px-3 text-xs font-semibold text-text-muted uppercase tracking-wider">Status</th>
              </tr>
            </thead>
            <tbody>
              {agentMembers.map((m) => (
                <tr key={m.id} className="border-b border-border last:border-0">
                  {/* v2.7 #192: agent display name, raw identity id on hover; links
                      to AgentDetail when an execution Agent is resolved. */}
                  <td className="py-2 px-3 text-sm text-text-primary">
                    {(() => {
                      const aid = agentIDByIdentity.get(m.identity_id);
                      return (
                        <EntityRef
                          id={m.identity_id}
                          name={m.display_name}
                          fallback={m.identity_id}
                          to={aid ? `/agents/${aid}` : undefined}
                          testId={aid ? `agent-member-link-${m.identity_id}` : undefined}
                          className={aid ? 'text-brand' : undefined}
                        />
                      );
                    })()}
                  </td>
                  <td className="py-2 px-3 text-sm text-text-secondary">{m.role}</td>
                  <td className="py-2 px-3 text-sm text-text-secondary">
                    {m.worker_id ? (
                      <span className="text-xs">
                        running on{' '}
                        <EntityRef
                          id={m.worker_id}
                          name={workerNameById.get(m.worker_id)}
                          fallback={m.worker_id}
                          testId="agent-member-worker"
                        />
                      </span>
                    ) : (
                      <span className="text-text-muted italic">Not bound to a worker</span>
                    )}
                  </td>
                  <td className="py-2 px-3 text-sm">
                    <span className={m.status === 'joined' ? 'text-success' : 'text-text-muted'}>
                      {m.status === 'joined' ? 'Joined' : 'Disabled'}
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
