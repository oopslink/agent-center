import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useMemo, useState } from 'react';

import { ApiError } from '@/api/client';
import { useAgents, useDeleteAgent } from '@/api/agents';
import { useMembers, normalizeIdentityRef, type MemberResult } from '@/api/members';
import { useWorkers } from '@/api/workers';
import { AgentCreateModal } from '@/components/AgentCreateModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityRef } from '@/components/EntityRef';
import { AvailabilityBadge, LifecycleBadge, ProviderBadge } from '@/components/AgentBadges';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { formatLocalTime } from '@/utils/time';

// v2.7 #197: map the backend's delete-guard codes to friendly copy so the UI
// never shows a raw error code or fails silently (Rule 9).
function agentDeleteErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.code === 'agent_running') return 'This agent must be stopped before it can be deleted.';
    if (err.code === 'agent_has_active_work') {
      return 'This agent has active work items and cannot be deleted until they finish.';
    }
    if (err.code === 'not_found') return 'This agent no longer exists.';
  }
  return err instanceof Error ? err.message : 'Delete failed, please try again.';
}

// Agents page (/agents). Agent BC (v2.7 #101) — lists org-scoped agents
// with lifecycle + availability badges and worker, plus an "+ Add Agent"
// modal. Rows link to /agents/{id}. Replaces the retired
// workforce.AgentInstance list.
export default function Agents(): React.ReactElement {
  const agents = useAgents();
  const workers = useWorkers();
  const workerName = (id: string): string | undefined =>
    (workers.data ?? []).find((w) => w.worker_id === id)?.name || undefined;
  // dev2/v281 canonical-fold: the enhanced /agents page is now the single
  // agents surface, so the retired /members/agents page's Role + membership
  // Status (Joined/Disabled) columns are folded in here so no info is lost.
  // The Agent DTO (useAgents) carries neither field — they are member-level
  // only — so we join the org member list keyed by identity, exactly like the
  // old MembersAgents page did. The execution Agent's identity_member_id ==
  // the agent member's identity_id (the #157 unified-create link); a standalone
  // agent with no member (identity_member_id empty) → no match → '—' fallback.
  const members = useMembers();
  const memberByIdentity = useMemo(() => {
    const m = new Map<string, MemberResult>();
    for (const mem of members.data ?? []) {
      m.set(normalizeIdentityRef(mem.identity_id), mem);
    }
    return m;
  }, [members.data]);
  const memberForAgent = (identityMemberId: string | undefined): MemberResult | undefined =>
    identityMemberId ? memberByIdentity.get(normalizeIdentityRef(identityMemberId)) : undefined;
  const [createOpen, setCreateOpen] = useState(false);
  // v2.7 #197: per-row hard-delete gated behind a confirm dialog.
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);
  const del = useDeleteAgent();

  return (
    <section className="space-y-4" data-testid="page-Agents">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="text-xl font-semibold">Agents</h1>
          <p className="text-xs text-text-muted">
            Organization-scoped agents with a managed lifecycle.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="agents-add-btn"
        >
          + Add Agent
        </button>
      </header>

      {createOpen && <AgentCreateModal onClose={() => setCreateOpen(false)} />}

      {agents.isLoading && (
        <div className="space-y-2" data-testid="agents-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {agents.isError && (
        <p className="text-sm text-danger" data-testid="agents-error">
          {(agents.error as Error).message}
        </p>
      )}
      {agents.isSuccess && agents.data.length === 0 && (
        <EmptyState
          testId="agents-empty"
          title="No agents yet"
          body="Agents are org-scoped entities with a managed lifecycle. Click + Add Agent to create one and bind it to a fleet worker."
        />
      )}
      {agents.isSuccess && agents.data.length > 0 && (
        <table
          className="w-full table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-text-primary"
          data-testid="agents-table"
        >
          <thead>
            <tr className="text-left text-xs uppercase tracking-wide text-text-muted">
              <th className="w-1/6 border-b border-border-base px-3 py-2">Name</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Provider</th>
              <th className="w-1/12 border-b border-border-base px-3 py-2">Lifecycle</th>
              <th className="w-1/12 border-b border-border-base px-3 py-2">Availability</th>
              {/* dev2/v281 canonical-fold: Role + Status folded from the retired
                  /members/agents page so the merge loses no information. */}
              <th className="w-1/12 border-b border-border-base px-3 py-2">Role</th>
              <th className="w-1/12 border-b border-border-base px-3 py-2">Status</th>
              <th className="border-b border-border-base px-3 py-2">Last activity</th>
              <th className="w-1/6 border-b border-border-base px-3 py-2">Worker</th>
              <th className="border-b border-border-base px-3 py-2 text-right" />
            </tr>
          </thead>
          <tbody>
            {agents.data.map((a) => (
              <tr
                key={a.id}
                className="text-sm"
                data-testid="agent-row"
                data-agent-id={a.id}
                data-lifecycle={a.lifecycle}
                data-availability={a.availability}
              >
                <td className="border-b border-border-base px-3 py-2 font-medium">
                  <span className="block truncate">{a.name}</span>
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  {/* v2.8.1 list-enrich: provider = CLI + model badges (text
                      labels, not color-only; reuse the AgentBadges chip style).
                      Each chip omitted gracefully when the value is blank. */}
                  <div className="flex flex-wrap items-center gap-1" data-testid="agent-provider">
                    {a.cli && <ProviderBadge label={a.cli} testId="agent-cli-badge" />}
                    {a.model && <ProviderBadge label={a.model} testId="agent-model-badge" />}
                    {!a.cli && !a.model && <span className="text-xs text-text-muted">—</span>}
                  </div>
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <LifecycleBadge lifecycle={a.lifecycle} />
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <AvailabilityBadge availability={a.availability} />
                </td>
                {/* dev2/v281 canonical-fold: Role + membership Status resolved
                    via the member-list join (see memberForAgent). */}
                <td
                  className="border-b border-border-base px-3 py-2 text-xs text-text-secondary"
                  data-testid="agent-role"
                >
                  {memberForAgent(a.identity_member_id)?.role ?? (
                    <span className="text-text-muted">—</span>
                  )}
                </td>
                <td className="border-b border-border-base px-3 py-2">
                  <MembershipStatus status={memberForAgent(a.identity_member_id)?.status} />
                </td>
                <td className="border-b border-border-base px-3 py-2 align-top">
                  <AgentLastActivity
                    at={a.last_activity_at}
                    content={a.last_activity_content}
                  />
                </td>
                <td className="border-b border-border-base px-3 py-2 text-xs text-text-muted">
                  {a.worker_id ? (
                    <EntityRef
                      id={a.worker_id}
                      name={workerName(a.worker_id)}
                      fallback={a.worker_id}
                      testId="agent-worker-ref"
                    />
                  ) : (
                    '—'
                  )}
                </td>
                <td className="border-b border-border-base px-3 py-2 text-right">
                  <div className="flex items-center justify-end gap-3">
                    <OrgLink
                      to={`/agents/${encodeURIComponent(a.id)}`}
                      className="text-xs text-accent hover:underline"
                    >
                      Open →
                    </OrgLink>
                    <button
                      type="button"
                      data-testid="agent-delete-button"
                      data-agent-id={a.id}
                      aria-label={`Delete agent ${a.name}`}
                      title="Delete agent"
                      onClick={() => {
                        del.reset();
                        setPendingDelete({ id: a.id, name: a.name });
                      }}
                      className="rounded px-2 py-1 text-xs text-text-muted hover:bg-danger/10 hover:text-danger"
                    >
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {del.isError && (
        <p className="text-sm text-danger" data-testid="agent-delete-error" role="alert">
          {agentDeleteErrorMessage(del.error)}
        </p>
      )}

      <ConfirmModal
        open={pendingDelete !== null}
        danger
        busy={del.isPending}
        title="Delete agent"
        message={
          pendingDelete
            ? `Delete the agent "${pendingDelete.name}"? This permanently removes the agent and its membership. The agent must be stopped with no active work. This cannot be undone.`
            : undefined
        }
        confirmLabel="Delete"
        onCancel={() => {
          if (del.isPending) return;
          setPendingDelete(null);
          del.reset();
        }}
        onConfirm={() => {
          if (!pendingDelete) return;
          del.mutate(pendingDelete.id, {
            // Close on both outcomes; an error surfaces as a page-level alert
            // (Rule 9: never silent) that the next delete attempt resets.
            onSettled: () => setPendingDelete(null),
          });
        }}
      />
    </section>
  );
}

// MembershipStatus — membership Status chip (dev2/v281 canonical-fold). Folds
// the retired /members/agents "Status" column: a member-level joined/disabled
// flag the Agent DTO does not carry, resolved via the member-list join. The
// state is conveyed by the TEXT label ("Joined"/"Disabled"), never color-only.
// COLORS = the curated SOLID X-100/X-800 pairs (theme-INDEPENDENT, AA in BOTH
// light + dark, no dark: variant): joined = green-100/green-800, disabled =
// slate-100/slate-700 (muted gray, NOT red — a11y guardrail). Do NOT use
// `bg-success/10 text-success` — that alpha-tint renders transparent + the green
// token is green-500 #22c55e = 2.28 on white = light-AA FAIL (Tester2 §3.3 catch;
// the recurring both-mode alpha-tint-over-token 命门).
// No member match (standalone agent / member list still loading) → a neutral
// "—" placeholder, never blank and never a misleading "Disabled".
function MembershipStatus({
  status,
}: {
  status: MemberResult['status'] | undefined;
}): React.ReactElement {
  if (!status) {
    return (
      <span className="text-xs text-text-muted" data-testid="agent-status" data-status="unknown">
        —
      </span>
    );
  }
  const joined = status === 'joined';
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        joined ? 'bg-green-100 text-green-800' : 'bg-slate-100 text-slate-700',
      ].join(' ')}
      data-testid="agent-status"
      data-status={status}
    >
      {joined ? 'Joined' : 'Disabled'}
    </span>
  );
}

// AgentLastActivity — last-activity cell for an Agents row (v2.8.1 list-enrich).
// Shows the timestamp via formatLocalTime (LOCAL tz, not raw GMT/Z) + a SINGLE
// LINE truncated PLAIN-TEXT preview of the content (ellipsis; never grows the
// row height) with a `title` carrying the full text on hover. No activity (both
// fields absent) → a friendly "No recent activity" placeholder, never blank.
// Soft-ref safe: the content is rendered as a text node — a stale/deleted entity
// reference is just inert text, no lookup, no crash, no raw ref painted.
function AgentLastActivity({
  at,
  content,
}: {
  at: string | undefined;
  content: string | undefined;
}): React.ReactElement {
  const preview = content?.replace(/\s+/g, ' ').trim();
  if (!at && !preview) {
    return (
      <span className="text-xs italic text-text-muted" data-testid="agent-no-activity">
        No recent activity
      </span>
    );
  }
  return (
    <div className="min-w-0" data-testid="agent-last-activity">
      {at && (
        <div
          className="text-xs text-text-muted"
          data-testid="agent-last-activity-at"
          title={formatLocalTime(at)}
        >
          {formatLocalTime(at)}
        </div>
      )}
      {preview && (
        <div
          className="truncate text-xs text-text-secondary"
          data-testid="agent-last-activity-content"
          title={preview}
        >
          {preview}
        </div>
      )}
    </div>
  );
}
