import type React from 'react';
import { useEffect, useState } from 'react';
import { useRemoveParticipant } from '@/api/conversations';
import { useDisplayNameResolver } from '@/api/members';
import { useAppStore } from '@/store/app';
import type { Participant } from '@/api/types';
import { MemberInviteModal } from './MemberInviteModal';
import { EntityRef } from './EntityRef';
import { Avatar } from './Avatar';
import { ConversationThreadList } from './ConversationThreadList';

interface Props {
  conversationId: string;
  participants: Participant[];
}

const COLLAPSE_KEY = 'ac.participants.collapsed';

function readCollapsed(): boolean {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return false;
    return localStorage.getItem(COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

// ParticipantsPanel — list participants + role + invite/remove controls.
// Only the channel owner (or supervisor) gets the action buttons; non-
// owners see a read-only list. v2.7 #167: invite is a search+multi-select
// modal (MemberInviteModal), and the panel can collapse/expand.
export function ParticipantsPanel({
  conversationId,
  participants,
}: Props): React.ReactElement {
  const me = useAppStore((s) => s.currentUserId);
  const displayName = useDisplayNameResolver();
  const isOwner = participants.some(
    (p) => p.identity_id === me && p.role === 'owner' && !p.left_at,
  );
  const active = participants.filter((p) => !p.left_at);
  const remove = useRemoveParticipant();
  const [inviteOpen, setInviteOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(readCollapsed);

  useEffect(() => {
    try {
      if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
        localStorage.setItem(COLLAPSE_KEY, collapsed ? '1' : '0');
      }
    } catch {
      // ignore
    }
  }, [collapsed]);

  if (collapsed) {
    return (
      <aside
        className="flex w-10 flex-shrink-0 flex-col items-center border-l border-border-base bg-bg-subtle py-3"
        aria-label="participants"
        data-testid="participants-panel"
        data-collapsed="true"
      >
        <button
          type="button"
          onClick={() => setCollapsed(false)}
          aria-label="Expand participants"
          title={`Participants (${active.length})`}
          data-testid="participants-expand"
          className="rounded p-1 text-text-secondary hover:bg-bg-base hover:text-text-primary"
        >
          ‹ {active.length}
        </button>
      </aside>
    );
  }

  return (
    <aside
      className="w-64 flex-shrink-0 border-l border-border-base bg-bg-subtle p-4"
      aria-label="participants"
      data-testid="participants-panel"
      data-collapsed="false"
    >
      <div className="mb-3 flex items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-semibold text-text-primary">
          Participants
          {/* 8th channel redesign: a small count pill (bg-subtle). */}
          <span
            className="inline-flex min-w-[1.25rem] items-center justify-center rounded-full bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary"
            data-testid="participants-count-chip"
          >
            {active.length}
          </span>
        </h3>
        <button
          type="button"
          onClick={() => setCollapsed(true)}
          aria-label="Collapse participants"
          data-testid="participants-collapse"
          className="rounded p-1 text-xs text-text-muted hover:bg-bg-base hover:text-text-primary"
        >
          ›
        </button>
      </div>
      <ul className="space-y-1">
        {active.map((p) => {
          const resolved = displayName(p.identity_id);
          const isResolved = resolved !== p.identity_id;
          return (
            <li
              key={p.identity_id}
              className="group flex items-center gap-2 rounded px-1 py-1 text-sm hover:bg-bg-base"
              data-testid="participant-row"
              data-identity={p.identity_id}
            >
              {/* Avatar (#211): kind from the identity-ref prefix (agent:/user:). */}
              <Avatar
                name={isResolved ? resolved : '?'}
                kind={p.identity_id.startsWith('agent:') ? 'agent' : 'human'}
                size="sm"
              />
              <span className="min-w-0 flex-1 truncate">
                {/* v2.7 #192/E1: a participant whose member was deleted (ref no
                    longer resolves) renders "(deleted)", never the raw agent:/user: ref. */}
                <EntityRef
                  id={p.identity_id}
                  name={isResolved ? resolved : undefined}
                  testId="participant-name"
                  className="text-xs"
                />
              </span>
              <RoleBadge role={p.role} />
              {isOwner && p.role !== 'owner' && (
                <button
                  type="button"
                  className="text-xs text-danger opacity-0 hover:underline focus:opacity-100 group-hover:opacity-100"
                  onClick={() => remove.mutate({ conversationId, identityId: p.identity_id })}
                  data-testid="participant-remove"
                >
                  remove
                </button>
              )}
            </li>
          );
        })}
      </ul>

      {isOwner && (
        <>
          <button
            type="button"
            onClick={() => setInviteOpen(true)}
            className="mt-4 inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-text-primary px-3 py-2 text-xs font-semibold text-bg-elevated hover:opacity-90 focus-visible:ring-2 focus-visible:ring-brand"
            data-testid="invite-open"
          >
            <svg
              viewBox="0 0 20 20"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              className="h-3.5 w-3.5"
              aria-hidden="true"
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M10 4v12M4 10h12" />
            </svg>
            Invite
          </button>
          {inviteOpen && (
            <MemberInviteModal
              conversationId={conversationId}
              participants={participants}
              onClose={() => setInviteOpen(false)}
            />
          )}
        </>
      )}

      {/* v2.9.1 Threads P2: the conversation's thread list. Clicking a thread
          opens the shared ThreadSidebar (mounted by ConversationView). */}
      <ConversationThreadList conversationId={conversationId} />
    </aside>
  );
}

// RoleBadge — OWNER / MEMBER pill (8th channel redesign, §3.3 not-color-only).
// owner → amber; everyone else → slate. Crucially the LITERAL text
// ("OWNER"/"MEMBER") carries the meaning, NOT color alone — amber and gray are
// adjacent enough that color must not be the sole signal.
//
// Uses SOLID theme-independent X-100/X-800 pairs (per Dev2 #245 tag palette), NOT
// `bg-warning/10` opacity tints: an alpha tint on a hex CSS-var token renders
// transparent (Tester2 #244 found bg-warning/10 → text-warning on panel = 1.96
// light / 1.67 dark, FAIL AA). Solid -100 bg + -800 text is AA in BOTH modes
// (amber-100/amber-800 ≈ 6.37, slate ≈ 8+) and reads identically light + dark.
function RoleBadge({ role }: { role: string }): React.ReactElement {
  const isOwner = role === 'owner';
  const cls = isOwner
    ? 'bg-amber-100 text-amber-800' // raw-color-ok: solid OWNER amber, AA both modes
    : 'bg-slate-100 text-slate-700'; // raw-color-ok: solid MEMBER slate, AA both modes
  return (
    <span
      data-testid="participant-role-badge"
      data-role={role}
      className={`shrink-0 rounded px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide ${cls}`}
    >
      {isOwner ? 'OWNER' : 'MEMBER'}
    </span>
  );
}
