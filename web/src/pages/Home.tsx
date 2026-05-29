import React from 'react';
import { OrgLink } from '@/OrgContext';

import { useFleet } from '@/api/fleet';
import { useInputRequests } from '@/api/inputRequests';
import { useConversations } from '@/api/conversations';
import type { Conversation } from '@/api/types';
import { Skeleton } from '@/components/Skeleton';

// Home / Overview (v2.3 P3). Bento-grid dashboard surface designed
// per docs/design/web-console-design-system.md § 2.2.
//
// All data is read from existing hooks (no new endpoints). Each card
// owns its loading + empty state so a slow backend on one slice
// doesn't blank the whole page.
export default function Home(): React.ReactElement {
  const fleet = useFleet();
  const irs = useInputRequests();
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });

  const pendingIRCount = (irs.data ?? []).filter((ir) => ir.status === 'pending').length;
  const onlineWorkers = (fleet.data?.workers ?? []).filter((w) => w.status === 'online').length;
  const runningExecs = (fleet.data?.executions ?? []).filter(
    (e) => e.status === 'working' || e.status === 'submitted',
  );
  const failedExecs = (fleet.data?.executions ?? []).filter((e) => e.status === 'failed');
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
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard
          label="Pending input requests"
          value={pendingIRCount}
          tone={pendingIRCount > 0 ? 'warning' : 'neutral'}
          href="/inputrequests"
          loading={irs.isLoading}
        />
        <StatCard
          label="Failed executions"
          value={failedExecs.length}
          tone={failedExecs.length > 0 ? 'danger' : 'neutral'}
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
          title="Running executions"
          to="/fleet"
          empty="No running executions"
          loading={fleet.isLoading}
          data-testid="home-running-execs"
        >
          {runningExecs.slice(0, 5).map((e) => (
            <li key={e.execution_id} className="flex items-center justify-between gap-3 py-1.5">
              <OrgLink
                to={`/tasks/${encodeURIComponent(e.task_id)}/trace`}
                className="truncate font-mono text-xs text-accent hover:underline"
              >
                {e.execution_id.slice(0, 12)}
              </OrgLink>
              <span className="text-xs text-text-secondary">{e.worker_id}</span>
              <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                {e.status}
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
                  <span className="text-text-muted">{c.kind === 'dm' ? '◐' : '#'}</span> {c.name || c.id}
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
