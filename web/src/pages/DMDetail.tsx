import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useParams } from 'react-router-dom';
import { useConversation } from '@/api/conversations';
import { useDisplayNameResolver, normalizeIdentityRef } from '@/api/members';
import { useAgent } from '@/api/agents';
import { useAppStore } from '@/store/app';
import { ConversationView } from '@/components/ConversationView';
import { ConversationSidebar } from '@/components/ConversationSidebar';
import { ContextPanel } from '@/shell/contextPanel';
import { FollowToggle } from '@/components/FollowToggle';
import { TypeChip } from '@/components/TypeChip';
import { Avatar } from '@/components/Avatar';
import { Breadcrumb } from '@/components/Breadcrumb';
import { SenderSidebarProvider, useSenderSidebar } from '@/components/SenderSidebarContext';

// DMDetail page (/dms/:id). Mirrors ChannelDetail layout but skips the
// ParticipantsPanel — DM membership is fixed at create time (per
// ADR-0032 § 6) and not mutable from the UI.
//
// v2.8.1 #7thDM: a richer 3-zone layout — a header zone (back / avatar /
// @name + Bot badge + online dot / "Direct message" subtitle / follow /
// search / overflow) over the surface-agnostic message+composer zone
// (ConversationView, owned elsewhere — kept verbatim).
//
// SSE live updates flow through F5's dispatchToQueryClient, same as
// channels — `conversation.message_added` invalidates the messages
// query.
// DMDetail wraps the page in a SenderSidebarProvider so the header peer (entry ①)
// AND the @mention tokens inside message content (entry ②) AND the message-sender
// clicks all open the ONE existing kind-routed SenderDetailSidebar. The provider
// owns the sidebar state + renders the single sidebar instance.
export default function DMDetail(): React.ReactElement {
  return (
    <SenderSidebarProvider>
      <DMDetailInner />
    </SenderSidebarProvider>
  );
}

function DMDetailInner(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  // #281 entry ①: open the SenderDetailSidebar for the DM peer when the header
  // avatar/@name is clicked. Provided by the wrapping SenderSidebarProvider.
  const openSender = useSenderSidebar();
  const conv = useConversation(id);
  // v2.7.1 #238 fix: the DM detail GET doesn't enrich peer_display_name (only the
  // list does), so resolve the peer from participants − self for direct loads.
  const me = useAppStore((s) => s.currentUserId);
  const resolveName = useDisplayNameResolver();

  // Derive the peer ref BEFORE the early returns so the agent query below is a
  // stable, unconditional hook call (React rules-of-hooks). conv.data may be
  // undefined here — guard each access; the query is `enabled`-gated on a ref.
  const meBare = me ? normalizeIdentityRef(me) : '';
  const peerRef =
    conv.data?.peer_identity_id ||
    (conv.data?.participants ?? [])
      .filter((p) => !p.left_at)
      .map((p) => p.identity_id)
      .find((pid) => normalizeIdentityRef(pid) !== meBare) ||
    '';
  const isAgentPeer = peerRef.startsWith('agent:');
  // Only fetch the execution Agent for an agent peer (users have no /agents row).
  // useAgent is `enabled: !!id` — passing undefined for a user/empty peer keeps it
  // idle, so user/deleted/loading peers degrade gracefully (no fetch, no dot).
  const agent = useAgent(isAgentPeer ? normalizeIdentityRef(peerRef) : undefined);
  // Online = the agent is actually running and reachable. We treat a `running`
  // lifecycle with a non-`unavailable` availability as online (green dot); every
  // other state (stopped/error/archived/stopping, or unavailable) → no dot.
  // Omitted entirely while the query is loading / for user / deleted peers.
  const a = agent.data;
  const online =
    isAgentPeer && a ? a.lifecycle === 'running' && a.availability !== 'unavailable' : false;

  if (conv.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-DMDetail">
        Loading DM…
      </section>
    );
  }
  if (conv.isError) {
    return (
      <section className="space-y-3 text-sm" data-testid="page-DMDetail">
        <p className="text-danger" data-testid="dm-not-found">
          {(conv.error as Error).message}
        </p>
        <OrgLink to="/dms" className="text-accent hover:underline">
          Back to DMs
        </OrgLink>
      </section>
    );
  }
  if (!conv.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-DMDetail">
        DM lookup failed.
      </section>
    );
  }

  // v2.7.1 #215 + #238 fix: DMs are strict 1:1; heading = the resolved peer as
  // @name. Prefer the backend-enriched peer fields (present when navigated from
  // the list), else derive the peer from participants − self and resolve its
  // display name — so a DIRECT load (detail GET, no peer enrichment) still shows
  // @peer instead of the generic "Direct message". Deleted/unresolved → fallback.
  const resolvedPeer = conv.data.peer_display_name || (peerRef ? resolveName(peerRef) : '');
  // resolveName returns the ref itself on a miss → treat that as unresolved.
  const peerName = resolvedPeer && resolvedPeer !== peerRef ? resolvedPeer : '';
  const heading = peerName ? `@${peerName}` : peerRef ? '(deleted)' : 'Direct message';
  const isDeleted = !peerName && !!peerRef;

  // Avatar identity: agent peers render the rounded-square (agent) avatar, users
  // the circle. Name = resolved peer name, else the bare handle (so a deleted /
  // unresolved peer still seeds stable initials instead of "?").
  const avatarName = peerName || (peerRef ? normalizeIdentityRef(peerRef) : 'Direct message');

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-DMDetail"
      data-dm-id={conv.data.id}
    >
      <div className="mb-2">
        <Breadcrumb items={[{ label: 'DMs', to: '/dms' }, { label: heading }]} />
      </div>

      {/* ── Header zone ─────────────────────────────────────────────────────
          Left cluster: back arrow + peer avatar + identity (name/badge/dot +
          subtitle). Right cluster: search / follow / overflow. */}
      <header className="flex items-center justify-between gap-3 border-b border-border-base pb-3">
        <div className="flex min-w-0 items-center gap-3">
          {/* Back to the DM list. */}
          <OrgLink
            to="/dms"
            data-testid="dm-back"
            aria-label="Back to DMs"
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          >
            <BackIcon />
          </OrgLink>

          {/* #281 entry ①: the peer avatar opens the SenderDetailSidebar for the
              peer (agent → AgentDetailBody, user → UserDetailBody). A real
              <button> (Tab + Enter/Space), aria-label, cursor-pointer hover. Only
              interactive when there's a peer ref to open. */}
          <button
            type="button"
            onClick={() => peerRef && openSender?.(peerRef)}
            disabled={!peerRef}
            aria-label={`View ${peerName || avatarName} details`}
            data-testid="dm-peer-avatar-button"
            className="shrink-0 rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-default enabled:cursor-pointer"
          >
            <Avatar
              name={avatarName}
              kind={isAgentPeer ? 'agent' : 'human'}
              size="lg"
              // Only feed an online state for a resolved, non-deleted agent — the
              // Avatar renders its status dot only when `online` is defined, so a
              // user / deleted / loading peer omits it entirely.
              online={isAgentPeer && !isDeleted && a ? online : undefined}
            />
          </button>

          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              {/* #281 entry ①: the @name also opens the SenderDetailSidebar.
                  Kept as an <h2> heading landmark with a nested <button> so it's
                  both a heading AND keyboard-accessible (Tab + Enter/Space).
                  title carries the raw ref on hover per the #192 chrome rule. */}
              <h2 className="min-w-0 text-xl font-semibold">
                <button
                  type="button"
                  onClick={() => peerRef && openSender?.(peerRef)}
                  disabled={!peerRef}
                  aria-label={`View ${peerName || avatarName} details`}
                  data-testid="dm-heading"
                  title={peerRef || undefined}
                  className="block max-w-full truncate rounded text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent enabled:cursor-pointer enabled:hover:underline"
                >
                  {heading}
                </button>
              </h2>
              <TypeChip kind="dm" />
              {/* Bot badge — only for an agent peer. Labelled text (not emoji /
                  color-only) so it reads in any theme + to AT. */}
              {isAgentPeer && (
                <span
                  data-testid="dm-bot-badge"
                  className="rounded bg-status-blue-bg px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-status-blue-fg"
                >
                  Bot
                </span>
              )}
              {/* Online dot — only for a running/available agent. aria-label +
                  title carry the meaning, so it is not color-only. */}
              {isAgentPeer && !isDeleted && online && (
                <span
                  data-testid="dm-online-dot"
                  role="img"
                  aria-label="Online"
                  title="Online"
                  className="inline-block h-2.5 w-2.5 rounded-full bg-status-green-solid-soft"
                />
              )}
            </div>
            <p className="text-xs text-text-secondary" data-testid="dm-subtitle">
              Direct message
            </p>
          </div>
        </div>

        {/* Right cluster. (No search button — message-search UI doesn't exist on
            this surface; per [[no-middle-state]] we don't ship a dead disabled
            affordance. Add it when search ships.) */}
        <div className="flex shrink-0 items-center gap-1.5">
          {/* #264 P1 / #176 §4: follow/unfollow this DM. */}
          <FollowToggle conversationId={conv.data.id} followed={conv.data.followed ?? false} />

          {/* Overflow — a keyboard-accessible details/summary menu. Holds the
              same Follow/Unfollow action (kept visible in the cluster too) plus a
              copy-link affordance. */}
          <details className="relative" data-testid="dm-overflow">
            <summary
              aria-label="More actions"
              title="More actions"
              className="inline-flex h-8 w-8 cursor-pointer list-none items-center justify-center rounded text-text-secondary marker:content-none hover:bg-bg-subtle hover:text-text-primary [&::-webkit-details-marker]:hidden"
            >
              <OverflowIcon />
            </summary>
            <div
              role="menu"
              className="absolute right-0 z-10 mt-1 w-44 rounded border border-border-base bg-bg-elevated py-1 shadow-md"
            >
              <button
                type="button"
                role="menuitem"
                data-testid="dm-overflow-copy"
                onClick={() => {
                  void navigator.clipboard?.writeText(window.location.href);
                }}
                className="block w-full px-3 py-1.5 text-left text-xs text-text-secondary hover:bg-bg-subtle"
              >
                Copy link to DM
              </button>
            </div>
          </details>
        </div>
      </header>

      {/* ── Message + composer zone ─────────────────────────────────────────
          #264 P1: message body + read-cursor + SSE live updates flow through
          the surface-agnostic shell. Owned by another dev — kept verbatim. */}
      <ConversationView surface="dm" conversationId={conv.data.id} />
      {/* T184: DMs get the shared col④ sidebar too, but WITHOUT the Participants
          tab — a DM is a fixed 1:1 (nothing to invite/remove). Threads / Files only. */}
      <ContextPanel>
        <ConversationSidebar conversationId={conv.data.id} showParticipants={false} />
      </ContextPanel>
    </section>
  );
}

// ── Inline SVG icons (no emoji; every icon button carries its own aria-label).

function BackIcon(): React.ReactElement {
  return (
    <svg
      viewBox="0 0 20 20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      aria-hidden="true"
      className="h-5 w-5"
    >
      <path strokeLinecap="round" strokeLinejoin="round" d="M12.5 5 7.5 10l5 5" />
    </svg>
  );
}


function OverflowIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" aria-hidden="true" className="h-5 w-5">
      <circle cx="10" cy="4" r="1.5" />
      <circle cx="10" cy="10" r="1.5" />
      <circle cx="10" cy="16" r="1.5" />
    </svg>
  );
}
