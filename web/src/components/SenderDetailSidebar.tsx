import type React from 'react';
import { useInRouterContext } from 'react-router-dom';
import { useAgent, useAgentActivity } from '@/api/agents';
import { useUser } from '@/api/users';
import {
  isResolvedName,
  normalizeIdentityRef,
  refKind,
  useDisplayNameResolver,
  useMembers,
} from '@/api/members';
import { ApiError } from '@/api/client';
import { useModalA11y } from './useModalA11y';
import { useOpenDm } from './useOpenDm';
import { Avatar } from './Avatar';
import { LifecycleBadge, AvailabilityBadge } from './AgentBadges';
import { AgentActivityRow, CheckingGroup } from './AgentActivityRow';
import { groupActivity } from './agentActivityGrouping';

// SenderDetailSidebar (v2.8.1 7th DM redesign, increment 2). A right slide-in
// panel that surfaces a message sender's detail when their avatar/name is
// clicked in MessageList. Kind is dispatched off the identity-ref prefix
// (agent:/user:) — agents resolve via useAgent, humans via useUser.
//
// a11y: mounts useModalA11y (Escape close + Tab focus-trap + focus-restore),
// role="dialog" + aria-modal + aria-label, a real <button> close affordance
// (ASCII × glyph, NOT an emoji per the no-emoji-icon guardrail). The overlay
// is dimmed-but-visible and click-to-close. The slide-in transition is
// disabled under motion-reduce.

interface Props {
  open: boolean;
  /** an identity ref like `agent:<id>` or `user:<id>`; null when closed. */
  senderRef: string | null;
  onClose: () => void;
}

export function SenderDetailSidebar({
  open,
  senderRef,
  onClose,
}: Props): React.ReactElement | null {
  const containerRef = useModalA11y({ open, onClose });
  const displayName = useDisplayNameResolver();
  // T136: the header "Open DM" button navigates (useOpenDm → useNavigate), which
  // needs a Router ancestor. This sidebar is embedded in many surfaces (mentions,
  // message list, threads) whose unit tests render WITHOUT a Router; gating the
  // button on useInRouterContext keeps it out of those router-less renders (the
  // live app always mounts within a Router) and isolates the navigate hook in the
  // AgentDmButton child so it only runs when actually rendered.
  const inRouter = useInRouterContext();

  const ref = senderRef ?? '';
  const kind = refKind(ref);
  const id = ref ? normalizeIdentityRef(ref) : '';

  // Both queries are gated on `open` + kind so only the relevant one fires.
  const agentQuery = useAgent(open && kind === 'agent' && id ? id : undefined);
  const userQuery = useUser(open && kind === 'user' && id ? id : undefined);

  if (!open || !senderRef) return null;

  // F1 consistency (v2.8.1 #192): the resolver returns the RAW ref on a miss
  // (that's the preserved miss-sentinel — see members.ts). We detect the miss
  // here via isResolvedName and render a muted "(deleted)" header label instead
  // of the raw `agent:agent-xxx`; the clean handle + raw ref stay on title=.
  const resolvedName = displayName(senderRef);
  const nameResolved = isResolvedName(senderRef, resolvedName);
  const headerName = nameResolved ? resolvedName : '(deleted)';
  // Avatar still seeds off the clean handle so it renders a stable glyph.
  const avatarSeed = nameResolved ? resolvedName : normalizeIdentityRef(senderRef);

  return (
    <>
      {/* Dimmed-but-visible overlay — click to close. */}
      <div
        className="fixed inset-0 z-30 bg-black/30"
        data-testid="sender-sidebar-overlay"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label={`${headerName} detail`}
        data-testid="sender-sidebar"
        className="fixed inset-y-0 right-0 z-40 flex h-full w-80 translate-x-0 transform flex-col border-l border-border-base bg-bg-elevated text-text-primary shadow-2 transition-transform duration-200 ease-out motion-reduce:transition-none sm:w-96"
      >
        {/* Header: avatar + resolved display name + close button. */}
        <div className="flex items-start gap-3 border-b border-border-base p-4">
          <Avatar name={avatarSeed} kind={kind === 'agent' ? 'agent' : 'human'} size="lg" />
          <div className="min-w-0 flex-1">
            <div
              className={`truncate text-base font-semibold ${nameResolved ? '' : 'italic text-text-secondary'}`}
              title={nameResolved ? senderRef : `${avatarSeed} (${senderRef})`}
              data-testid="sender-sidebar-name"
              data-name-resolved={nameResolved ? 'true' : 'false'}
            >
              {headerName}
            </div>
            <div className="text-xs uppercase tracking-wide text-text-muted">
              {kind === 'agent' ? 'Agent' : 'User'}
            </div>
          </div>
          <div className="flex items-center gap-1">
            {/* T136: agent-only "Open DM" icon button (no text; tooltip on hover).
                Rendered only within a Router (see inRouter note above). */}
            {kind === 'agent' && inRouter && (
              <AgentDmButton senderRef={senderRef} onClose={onClose} />
            )}
            <button
              type="button"
              onClick={onClose}
              data-testid="sender-sidebar-close"
              aria-label="Close sender detail"
              className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
            >
              {/* plain ASCII "X" close glyph (per the #208 lesson — NOT the U+2715
                  multiplication-x, which is in the a11y guardrail's pictograph
                  range). aria-hidden; the button's aria-label is the accessible name. */}
              <span aria-hidden="true">X</span>
            </button>
          </div>
        </div>

        {/* Body: kind-dispatched detail. */}
        <div className="flex-1 overflow-y-auto p-4">
          {kind === 'agent' ? (
            <AgentDetailBody agentId={id} query={agentQuery} />
          ) : (
            <UserDetailBody query={userQuery} memberRef={senderRef} />
          )}
        </div>
      </div>
    </>
  );
}

// AgentDmButton — T136 header "Open DM" icon button. Opens (or reuses) the 1:1 DM
// with the agent and navigates to it, then closes the sidebar so the operator
// lands in the conversation. Isolated in its own component so its useOpenDm →
// useNavigate hook only runs when the button is actually rendered (i.e. within a
// Router — see the SenderDetailSidebar inRouter gate).
function AgentDmButton({
  senderRef,
  onClose,
}: {
  senderRef: string;
  onClose: () => void;
}): React.ReactElement {
  const openDm = useOpenDm();
  return (
    <button
      type="button"
      onClick={() => {
        if (openDm.pending) return;
        openDm.open(senderRef);
        onClose();
      }}
      disabled={openDm.pending}
      data-testid="sender-sidebar-dm"
      aria-label="Open DM"
      title="Open DM"
      className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
    >
      <ChatIcon />
    </button>
  );
}

// ChatIcon — T136 "Open DM" glyph (speech bubble), inline SVG (NOT an emoji, per
// the a11y guardrail). aria-hidden; the button's aria-label/title is the name.
function ChatIcon(): React.ReactElement {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M21 11.5a8.38 8.38 0 0 1-8.5 8.5 9 9 0 0 1-3.9-.9L3 21l1.9-5.6a9 9 0 0 1-.9-3.9A8.38 8.38 0 0 1 12.5 3 8.38 8.38 0 0 1 21 11.5z" />
    </svg>
  );
}

// LabelRow — a small label/value pair used across both detail bodies.
function LabelRow({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-xs uppercase tracking-wide text-text-muted">{label}</span>
      <span className="text-sm text-text-primary">{value}</span>
    </div>
  );
}

function StateMessage({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <div className="text-sm text-text-muted" data-testid="sender-sidebar-state">
      {children}
    </div>
  );
}

// is404 reports whether a react-query error is a 404 (resource gone). A
// force-deleted agent's GET /api/agents/{id} returns 404 → ApiError(status:404).
// F2 (v2.8.1): used to show the FRIENDLY "unavailable (deleted)" message instead
// of the generic "couldn't load" / a bare "not found" — and never a blank panel.
function is404(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404;
}

function AgentDetailBody({
  agentId,
  query,
}: {
  agentId: string;
  query: ReturnType<typeof useAgent>;
}): React.ReactElement {
  // Activity loads in parallel with the agent detail (both gated on the same id).
  const activity = useAgentActivity(agentId);

  if (query.isLoading) return <StateMessage>Loading agent…</StateMessage>;
  // F2 (v2.8.1): a force-deleted agent's GET 404s. Show a FRIENDLY deleted
  // message (not a generic "couldn't load", not a bare "not found", never
  // blank). Other (non-404) errors keep the generic load-failure message.
  if (query.isError) {
    return is404(query.error) ? (
      <StateMessage>This agent is unavailable (deleted).</StateMessage>
    ) : (
      <StateMessage>Couldn&apos;t load this agent.</StateMessage>
    );
  }
  const agent = query.data;
  // Defensive: if the query somehow settles with no data (e.g. a 200 tombstone
  // body), still render a friendly message rather than a blank panel.
  if (!agent) return <StateMessage>This agent is unavailable (deleted).</StateMessage>;

  return (
    <div className="flex flex-col gap-4" data-testid="sender-sidebar-agent">
      <div className="flex flex-wrap items-center gap-2">
        <LifecycleBadge lifecycle={agent.lifecycle} />
        <AvailabilityBadge availability={agent.availability} />
      </div>

      {/* Compact basic info (v2.8.1 @oopslink: dense dl, name dropped — it's in
          the header). A two-column label/value grid keeps it tight vs. the old
          stacked two-line rows. */}
      <dl
        className="grid grid-cols-[4.5rem_1fr] gap-x-3 gap-y-1 text-sm"
        data-testid="sender-sidebar-agent-info"
      >
        <dt className="text-text-muted">CLI</dt>
        <dd className="min-w-0 break-words text-text-primary">{agent.cli || '—'}</dd>
        <dt className="text-text-muted">Model</dt>
        <dd className="min-w-0 break-words text-text-primary">{agent.model || '—'}</dd>
        <dt className="text-text-muted">Worker</dt>
        <dd className="min-w-0 break-words text-text-primary">
          {agent.computer?.name || agent.worker_id || '—'}
        </dd>
        <dt className="text-text-muted">Description</dt>
        <dd className="min-w-0 break-words text-text-primary">{agent.description || '—'}</dd>
      </dl>

      <AgentActivitySection activity={activity} />
    </div>
  );
}

// AgentActivitySection renders the agent's activity feed inside the sidebar,
// reusing the same grouping + row components as the AgentDetail page (#274).
function AgentActivitySection({
  activity,
}: {
  activity: ReturnType<typeof useAgentActivity>;
}): React.ReactElement {
  const events = activity.data?.pages.flatMap((p) => p.activity) ?? [];

  return (
    <section className="border-t border-border-base pt-4" data-testid="sender-sidebar-activity">
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-text-muted">Activity</h3>
        <button
          type="button"
          className="rounded border border-border-strong px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
          data-testid="sender-sidebar-activity-refresh"
          onClick={() => void activity.refetch()}
          disabled={activity.isFetching}
          aria-busy={activity.isFetching}
        >
          {activity.isFetching ? 'Refreshing…' : 'Refresh'}
        </button>
      </div>
      {activity.isLoading && (
        <p className="text-xs text-text-muted" data-testid="sender-sidebar-activity-loading">
          Loading activity…
        </p>
      )}
      {activity.isError && (
        <p className="text-xs text-danger" data-testid="sender-sidebar-activity-error">
          {(activity.error as Error).message}
        </p>
      )}
      {activity.isSuccess && events.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="sender-sidebar-activity-empty">
          No activity yet.
        </p>
      )}
      {activity.isSuccess && events.length > 0 && (
        <>
          <ul className="divide-y divide-border-base" data-testid="sender-sidebar-activity-list">
            {groupActivity(events).map((item) =>
              item.kind === 'checking-group' ? (
                <CheckingGroup key={item.events[0].id} events={item.events} />
              ) : (
                <AgentActivityRow key={item.event.id} event={item.event} />
              ),
            )}
          </ul>
          {activity.hasNextPage && (
            <button
              type="button"
              className="mt-2 w-full rounded border border-border-base px-2 py-1.5 text-xs text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
              data-testid="sender-sidebar-activity-load-older"
              onClick={() => void activity.fetchNextPage()}
              disabled={activity.isFetchingNextPage}
              aria-busy={activity.isFetchingNextPage}
            >
              {activity.isFetchingNextPage ? 'Loading…' : 'Load older'}
            </button>
          )}
        </>
      )}
    </section>
  );
}

function UserDetailBody({
  query,
  memberRef,
}: {
  query: ReturnType<typeof useUser>;
  memberRef: string;
}): React.ReactElement {
  // Role is enrichment-only: looked up from the org members list (cheap, already
  // cached) keyed by the normalized identity ref. Absent => omitted.
  const members = useMembers();
  const key = normalizeIdentityRef(memberRef);
  const role = (members.data ?? []).find(
    (m) => normalizeIdentityRef(m.identity_id) === key,
  )?.role;

  if (query.isLoading) return <StateMessage>Loading user…</StateMessage>;
  // F2 (v2.8.1): same friendly-deleted treatment as the agent branch for a 404
  // (deleted user). Non-404 errors keep the generic load-failure message.
  if (query.isError) {
    return is404(query.error) ? (
      <StateMessage>This user is unavailable (deleted).</StateMessage>
    ) : (
      <StateMessage>Couldn&apos;t load this user.</StateMessage>
    );
  }
  const user = query.data;
  if (!user) return <StateMessage>This user is unavailable (deleted).</StateMessage>;

  return (
    <div className="flex flex-col gap-4" data-testid="sender-sidebar-user">
      <LabelRow label="Name" value={user.display_name} />
      <LabelRow label="Kind" value="User" />
      {role && <LabelRow label="Role" value={role} />}
      {user.email && <LabelRow label="Email" value={user.email} />}
    </div>
  );
}
