import type React from 'react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
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
  /**
   * v2.10.1 [T96]: render the embedded thread list at the bottom. Default true
   * (the standalone panel). The channel Participants tab passes false because
   * Threads is now its own sibling tab.
   */
  showThreads?: boolean;
}

// ParticipantsPanel — list participants + role + invite/remove controls.
// Only the channel owner (or supervisor) gets the action buttons; non-owners
// see a read-only list. v2.7 #167: invite is a search+multi-select modal
// (MemberInviteModal).
//
// v2.10.2 [T128]: this is now pure tab CONTENT inside the channel col④ sidebar
// (ChannelSidebarTabs → the "Participants" tab). The panel no longer owns its own
// width/resize (the col④ COLUMN is the resizable surface now — AppLayout) nor a
// collapse header — the tab label already names it, so the inner title block was a
// duplicate and is gone; the list renders directly.
export function ParticipantsPanel({
  conversationId,
  participants,
  showThreads = true,
}: Props): React.ReactElement {
  const { t } = useTranslation('chat');
  const me = useAppStore((s) => s.currentUserId);
  const displayName = useDisplayNameResolver();
  const isOwner = participants.some(
    (p) => p.identity_id === me && p.role === 'owner' && !p.left_at,
  );
  const active = participants.filter((p) => !p.left_at);
  const remove = useRemoveParticipant();
  const [inviteOpen, setInviteOpen] = useState(false);

  return (
    <div className="p-4" aria-label={t('panels.participants.ariaLabel')} data-testid="participants-panel">
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
                  {t('panels.participants.remove')}
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
            className="mt-4 inline-flex w-full items-center justify-center gap-1.5 rounded-md bg-btn-primary-bg px-3 py-2 text-xs font-semibold text-btn-primary-fg hover:opacity-90 focus-visible:ring-2 focus-visible:ring-brand"
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
            {t('panels.participants.invite')}
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
          opens the shared ThreadSidebar (mounted by ConversationView).
          v2.10.1 [T96]: omitted when Threads is rendered as its own sibling tab
          (the channel 3-tab sidebar). */}
      {showThreads && <ConversationThreadList conversationId={conversationId} />}
    </div>
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
  const { t } = useTranslation('chat');
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
      {isOwner ? t('panels.participants.roleOwner') : t('panels.participants.roleMember')}
    </span>
  );
}
