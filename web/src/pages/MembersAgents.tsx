import React, { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { useMembers } from '@/api/members';
import { useAgents } from '@/api/agents';
import { useWorkers } from '@/api/workers';
import { useOptionalOrgContext } from '@/OrgContext';
import type { Agent } from '@/api/types';
import { EntityRef } from '@/components/EntityRef';
import { Avatar } from '@/components/Avatar';
import { MembersSegmentControl } from '@/components/MembersSegmentControl';
import { useOpenDm } from '@/components/useOpenDm';

// Availability → online dot color (mockup: green online, grey idle). Solid tokens
// only — `bg-success` / `bg-warning` / `bg-border-strong` (no alpha-on-token).
function dotClass(availability?: Agent['availability']): string {
  if (availability === 'available') return 'bg-success';
  if (availability === 'busy') return 'bg-warning';
  return 'bg-border-strong';
}

export default function MembersAgents(): React.ReactElement {
  const members = useMembers();
  const agents = useAgents();
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';
  const openDm = useOpenDm();
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
  // identity_id → execution Agent, so the mobile cards can show lifecycle +
  // availability (the member row alone carries neither).
  const agentByIdentity = useMemo(() => {
    const m = new Map<string, Agent>();
    for (const a of agents.data ?? []) {
      if (a.identity_member_id) m.set(a.identity_member_id, a);
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

      {/* Mobile (col② is hidden <md): segmented Humans/Agents switch. */}
      <MembersSegmentControl active="agents" />

      {members.isLoading && <p className="text-sm text-text-muted">Loading…</p>}

      {!members.isLoading && agentMembers.length === 0 && (
        <p className="text-sm text-text-muted">No agent members yet</p>
      )}

      {/* Mobile (<md): card rows — avatar (tap → DM) + name + online dot +
          "Role · lifecycle"; tap the row → AgentDetail (when resolved). */}
      {agentMembers.length > 0 && (
        <ul className="space-y-2 md:hidden" data-testid="members-agents-cards">
          {agentMembers.map((m) => {
            const aid = agentIDByIdentity.get(m.identity_id);
            const agent = agentByIdentity.get(m.identity_id);
            const name = m.display_name || m.identity_id;
            const lifecycle = agent?.lifecycle ?? (m.status === 'joined' ? 'joined' : 'disabled');
            const body = (
              <span className="min-w-0 flex-1">
                <span className="flex items-center gap-1.5">
                  <span className="truncate text-sm font-medium text-text-primary">{name}</span>
                  <span
                    className={`h-2 w-2 flex-none rounded-full ${dotClass(agent?.availability)}`}
                    data-testid="agent-online-dot"
                    data-availability={agent?.availability ?? 'unknown'}
                    aria-hidden="true"
                  />
                </span>
                <span className="block truncate text-xs text-text-muted">
                  {m.role} · {lifecycle}
                </span>
              </span>
            );
            return (
              <li
                key={m.id}
                className="flex items-center gap-3 rounded-lg border border-border-base bg-bg-elevated p-2"
                data-testid="agent-member-card"
                data-identity={m.identity_id}
              >
                {/* Avatar tap → open DM (mockup: 点头像可开 DM). */}
                <button
                  type="button"
                  onClick={() => openDm.open(m.identity_id)}
                  disabled={openDm.pending}
                  aria-label={`Message ${name}`}
                  data-testid="agent-card-dm"
                  className="flex min-h-[44px] min-w-[44px] items-center justify-center rounded-lg disabled:opacity-50"
                >
                  <Avatar name={name} kind="agent" size="md" />
                </button>
                {aid ? (
                  <Link
                    to={`/agents/${aid}`}
                    className="flex min-h-[44px] min-w-0 flex-1 items-center"
                    data-testid="agent-card-link"
                  >
                    {body}
                  </Link>
                ) : (
                  <span className="flex min-h-[44px] min-w-0 flex-1 items-center">{body}</span>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {/* Desktop (≥md): the full table. */}
      {agentMembers.length > 0 && (
        <div className="hidden overflow-x-auto md:block">
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
