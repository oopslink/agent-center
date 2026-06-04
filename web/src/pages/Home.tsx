import React from 'react';
import { OrgLink } from '@/OrgContext';

import { useFleet } from '@/api/fleet';
import { useConversations } from '@/api/conversations';
import { useAgents } from '@/api/agents';
import type { Conversation } from '@/api/types';
import { Skeleton } from '@/components/Skeleton';
import { EntityRef } from '@/components/EntityRef';

// Home / Overview (v2.3 P3). Bento-grid dashboard surface designed
// per docs/design/web-console-design-system.md § 2.2.
//
// All data is read from existing hooks (no new endpoints). Each card
// owns its loading + empty state so a slow backend on one slice
// doesn't blank the whole page.
export default function Home(): React.ReactElement {
  const fleet = useFleet();
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });
  const agents = useAgents();
  // v2.7 #192: resolve a work-item's agent ref → agent name (raw id on hover).
  const agentName = (ref: string): string | undefined => {
    const bare = ref.replace(/^agent:/, '');
    return (agents.data ?? []).find((a) => a.id === bare || a.identity_member_id === bare)?.name || undefined;
  };

  const onlineWorkers = (fleet.data?.workers ?? []).filter((w) => w.status === 'online').length;
  // v2.7 #107/#118: fleet now returns only non-terminal work items
  // {queued,active,waiting_input}; terminal (incl failed) is not surfaced here.
  const workItems = fleet.data?.work_items ?? [];
  // Merge channels + DMs, take 5 newest by opened_at desc. Conversations
  // don't carry an updated_at on the read DTO (would require a server-
  // side projection change); opened_at is "close enough" for the
  // overview surface.
  const allConvs: Conversation[] = [...(channels.data ?? []), ...(dms.data ?? [])];
  const recentConvs = allConvs
    .slice()
    .sort((a, b) => ((a.opened_at ?? '') < (b.opened_at ?? '') ? 1 : -1))
    .slice(0, 5);

  // v2.4-D-F5 (task #45): "Get started: add your first worker" card.
  // Shows when no workers are enrolled — the first-mile gap that blocks
  // the user from running any agent.
  const noWorkers = (fleet.data?.workers ?? []).length === 0 && !fleet.isLoading;

  return (
    <section className="space-y-4" data-testid="page-Home">
      <header className="flex items-center justify-between">
        <h1 className="font-heading text-2xl font-semibold text-text-primary">Overview</h1>
        {fleet.data?.generated_at && (
          <span className="text-xs text-text-muted" data-testid="home-generated-at">
            last sync · {formatRelative(fleet.data.generated_at)}
          </span>
        )}
      </header>

      {noWorkers && (
        <div
          className="rounded-lg border border-accent/40 bg-accent/10 p-5"
          data-testid="home-get-started"
        >
          <h2 className="text-base font-semibold text-text-primary">
            Get started: add your first worker
          </h2>
          <p className="mt-1 text-sm text-text-secondary">
            AgentCenter is running, but no workers are connected yet. Workers
            are where agents (claude-code, codex, opencode, …) actually run.
          </p>
          <OrgLink
            to="/fleet"
            className="mt-3 inline-block rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover"
            data-testid="home-get-started-cta"
          >
            Add a worker →
          </OrgLink>
        </div>
      )}

      {/* Row 1 — at-a-glance stat cards. */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <StatCard
          label="Active work items"
          value={workItems.length}
          tone={workItems.length > 0 ? 'success' : 'neutral'}
          href="/fleet"
          loading={fleet.isLoading}
        />
        <StatCard
          label="Workers online"
          value={onlineWorkers}
          subValue={`/ ${(fleet.data?.workers ?? []).length} total`}
          tone={onlineWorkers > 0 ? 'success' : 'neutral'}
          href="/fleet"
          loading={fleet.isLoading}
        />
      </div>

      {/* Row 2 — running tasks + recent conversations. */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <PanelCard
          title="Work items"
          to="/fleet"
          empty="No active work items"
          loading={fleet.isLoading}
          data-testid="home-active-work-items"
        >
          {workItems.slice(0, 5).map((wi) => (
            <li
              key={wi.work_item_id}
              // v2.7.1 #206: task title is the visible handle (links to the task);
              // the task/work-item id stays on hover (#192). Falls back to the
              // agent name until the projection carries task_title/project_id.
              title={wi.task_id || wi.work_item_id}
              className="flex items-center justify-between gap-3 py-1.5"
            >
              {wi.task_title && wi.project_id && wi.task_id ? (
                <OrgLink
                  to={`/projects/${encodeURIComponent(wi.project_id)}/tasks/${encodeURIComponent(wi.task_id)}`}
                  className="truncate text-xs text-text-secondary hover:text-accent"
                  data-testid="home-wi-task"
                >
                  {wi.task_title}
                </OrgLink>
              ) : (
                <EntityRef
                  id={wi.agent_id}
                  name={agentName(wi.agent_id)}
                  fallback={wi.agent_id}
                  testId="home-wi-agent"
                  className="truncate text-xs text-text-secondary"
                />
              )}
              <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                {wi.status}
              </span>
            </li>
          ))}
        </PanelCard>

        <PanelCard
          title="Recent conversations"
          to="/channels"
          empty="No conversations yet"
          loading={channels.isLoading || dms.isLoading}
          data-testid="home-recent-convs"
        >
          {recentConvs.map((c) => {
            const href = c.kind === 'dm' ? `/dms/${encodeURIComponent(c.id)}` : `/channels/${encodeURIComponent(c.name)}`;
            return (
              <li key={c.id} className="flex items-center justify-between gap-3 py-1.5">
                <OrgLink to={href} className="truncate text-sm text-text-primary hover:text-accent">
                  <span className="text-text-muted">{c.kind === 'dm' ? '◐' : '#'}</span>{' '}
                  {c.kind === 'dm' ? (
                    // v2.7.1 #215/Rule 2a: DM peer as @name (hover peer id); deleted
                    // peer → "(deleted)"; malformed DM → "Direct message".
                    c.peer_identity_id ? (
                      <EntityRef
                        id={c.peer_identity_id}
                        name={c.peer_display_name ? `@${c.peer_display_name}` : undefined}
                        testId="home-conv-name"
                      />
                    ) : (
                      'Direct message'
                    )
                  ) : (
                    c.name || c.id
                  )}
                </OrgLink>
                <span className="text-xs text-text-muted tabular-nums">
                  {c.opened_at ? formatRelative(c.opened_at) : ''}
                </span>
              </li>
            );
          })}
        </PanelCard>
      </div>
    </section>
  );
}

// ----------------------------------------------------------------------------
// Card sub-components
// ----------------------------------------------------------------------------

const toneClass: Record<'neutral' | 'success' | 'warning' | 'danger', string> = {
  neutral: 'text-text-primary',
  success: 'text-success',
  warning: 'text-warning',
  danger: 'text-danger',
};

function StatCard({
  label,
  value,
  subValue,
  tone,
  href,
  loading,
}: {
  label: string;
  value: number;
  subValue?: string;
  tone: 'neutral' | 'success' | 'warning' | 'danger';
  href: string;
  loading: boolean;
}): React.ReactElement {
  return (
    <OrgLink
      to={href}
      className="block rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1 motion-safe:transition-shadow hover:shadow-2"
    >
      <span className="text-xs font-medium uppercase tracking-wider text-text-muted">{label}</span>
      <div className="mt-2 flex items-baseline gap-2">
        {loading ? (
          <Skeleton width="3rem" height="2rem" />
        ) : (
          <span className={`font-heading text-3xl font-semibold tabular-nums ${toneClass[tone]}`}>
            {value}
          </span>
        )}
        {subValue && <span className="text-xs text-text-muted">{subValue}</span>}
      </div>
    </OrgLink>
  );
}

function PanelCard({
  title,
  to,
  empty,
  loading,
  children,
  ...rest
}: {
  title: string;
  to: string;
  empty: string;
  loading: boolean;
  children: React.ReactNode;
} & React.HTMLAttributes<HTMLDivElement>): React.ReactElement {
  const items = React.Children.toArray(children);
  return (
    <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1" {...rest}>
      <div className="mb-2 flex items-center justify-between">
        <h2 className="font-heading text-sm font-semibold text-text-primary">{title}</h2>
        <OrgLink to={to} className="text-xs text-accent hover:underline">
          View all →
        </OrgLink>
      </div>
      {loading ? (
        <div className="space-y-2 py-2">
          <Skeleton height="1.5rem" />
          <Skeleton height="1.5rem" />
        </div>
      ) : items.length === 0 ? (
        <p className="py-4 text-center text-xs text-text-muted">{empty}</p>
      ) : (
        <ul className="divide-y divide-border-base">{items}</ul>
      )}
    </div>
  );
}

// ----------------------------------------------------------------------------
// Tiny relative-time helper. We don't need Intl.RelativeTimeFormat or
// date-fns for "Xs / Xm / Xh / Xd ago" precision; this avoids a new dep.
// ----------------------------------------------------------------------------
function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}
