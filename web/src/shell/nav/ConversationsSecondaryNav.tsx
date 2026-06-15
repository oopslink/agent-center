import type React from 'react';
import { useState } from 'react';
import { NavLink, useLocation, useNavigate } from 'react-router-dom';
import {
  useConversations,
  useDeleteConversation,
  conversationDeleteErrorMessage,
} from '@/api/conversations';
import { UnreadBadge } from '@/components/UnreadBadge';
import { ConfirmModal } from '@/components/ConfirmModal';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';
import type { Conversation } from '@/api/types';

// ============================================================================
// v2.10.0 [T2 / T64] — Conversations col② (per-module secondary-nav, registered
// in SECONDARY_NAV_REGISTRY). Mockup: docs/design/v2.10.0/shell-conversations-
// tasks.html 例1 — two always-visible sections (Channels / Direct messages),
// each row linking to the conversation and carrying its UnreadBadge.
//
// This REPLACES the shell-default expandable nav-item form for the Conversations
// module. It preserves the prior capabilities the default carried: archived
// channels are excluded; a deleted-peer DM keeps its manual delete action (the
// confirm flow lives here now, mirroring SecondaryNavBody's, since the default
// NavGroup that hosted it no longer renders for this module). Header / footer /
// collapse / col④ host stay shell chrome (col② contract).
// ============================================================================

// Row look mirrors the shell default sub-item link (active = brand-hover/white).
function rowClass({ isActive }: { isActive: boolean }): string {
  return [
    'flex min-w-0 flex-1 items-center justify-between gap-2 rounded px-2 py-1 text-sm motion-safe:transition-colors',
    isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
  ].join(' ');
}

function SectionHeader({
  label,
  createTo,
  createTestId,
  createLabel,
}: {
  label: string;
  createTo: string;
  createTestId: string;
  createLabel: string;
}): React.ReactElement {
  return (
    <div className="flex items-center justify-between px-1 pb-1">
      <h3
        data-testid="conv-section-label"
        className="text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted"
      >
        {label}
      </h3>
      <NavLink
        to={createTo}
        data-testid={createTestId}
        aria-label={createLabel}
        title={createLabel}
        className="rounded px-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary"
      >
        +
      </NavLink>
    </div>
  );
}

function EmptyRow({ text }: { text: string }): React.ReactElement {
  return <li className="px-2 py-0.5 text-xs italic text-text-muted">{text}</li>;
}

// Trash glyph as an SVG (NOT an emoji/pictograph — the a11y no-emoji-icons gate
// forbids unicode pictographs as icons); mirrors the shell default's TrashIcon.
function TrashIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4.5 6h11M8 6V4.5h4V6M7 8.5l.5 7h5l.5-7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function ConversationsSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const location = useLocation();
  const navigate = useNavigate();
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });
  const deleteConversation = useDeleteConversation();
  const [pendingDeleteDM, setPendingDeleteDM] = useState<{ id: string; to: string; label: string } | null>(
    null,
  );

  const activeChannels = (channels.data ?? []).filter((c) => c.status !== 'archived');
  const dmList = dms.data ?? [];

  // A DM whose peer identity exists but whose display name is gone = a
  // deleted-peer DM; it keeps a manual delete action (same rule as the prior
  // shell default: canDelete = has peer ref but no resolvable display name).
  const dmLabel = (d: Conversation): string =>
    d.peer_display_name ? `@${d.peer_display_name}` : d.peer_identity_id ? '(deleted)' : 'Direct message';
  const dmCanDelete = (d: Conversation): boolean => !!d.peer_identity_id && !d.peer_display_name;

  return (
    <div className="space-y-4" data-testid="conversations-secondary-nav">
      {/* Channels */}
      <div>
        <SectionHeader
          label="Channels"
          createTo={`${orgBase}/channels`}
          createTestId="conv-new-channel"
          createLabel="New channel"
        />
        <ul className="space-y-0.5">
          {activeChannels.length === 0 && <EmptyRow text="No channels" />}
          {activeChannels.map((c) => (
            <li key={c.id}>
              <NavLink
                to={`${orgBase}/channels/${encodeURIComponent(c.id)}`}
                className={rowClass}
                data-testid="conv-nav-channel"
              >
                <span className="flex min-w-0 items-center gap-2">
                  <span aria-hidden="true" className="text-text-muted">
                    #
                  </span>
                  <span className="truncate">{c.name}</span>
                </span>
                <UnreadBadge unreadCount={c.unread_count} mentionCount={c.mention_count} />
              </NavLink>
            </li>
          ))}
        </ul>
      </div>

      {/* Direct messages */}
      <div>
        <SectionHeader
          label="Direct messages"
          createTo={`${orgBase}/dms`}
          createTestId="conv-new-dm"
          createLabel="New direct message"
        />
        <ul className="space-y-0.5">
          {dmList.length === 0 && <EmptyRow text="No direct messages" />}
          {dmList.map((d) => (
            <li key={d.id}>
              <div className="flex items-center gap-1">
                <NavLink
                  to={`${orgBase}/dms/${encodeURIComponent(d.id)}`}
                  className={rowClass}
                  data-testid="conv-nav-dm"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate">{dmLabel(d)}</span>
                  </span>
                  <UnreadBadge unreadCount={d.unread_count} mentionCount={d.mention_count} />
                </NavLink>
                {dmCanDelete(d) && (
                  <button
                    type="button"
                    className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-text-muted hover:bg-danger/10 hover:text-danger"
                    data-testid="sidebar-dm-delete-button"
                    aria-label={`Delete DM ${dmLabel(d)}`}
                    title="Delete DM"
                    onClick={() => {
                      deleteConversation.reset();
                      setPendingDeleteDM({
                        id: d.id,
                        to: `${orgBase}/dms/${encodeURIComponent(d.id)}`,
                        label: dmLabel(d),
                      });
                    }}
                  >
                    <TrashIcon />
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      </div>

      {deleteConversation.isError && (
        <p className="px-3 pb-1 text-xs text-danger" data-testid="sidebar-dm-delete-error" role="alert">
          {conversationDeleteErrorMessage(deleteConversation.error)}
        </p>
      )}

      <ConfirmModal
        open={pendingDeleteDM !== null}
        danger
        busy={deleteConversation.isPending}
        title="Delete DM"
        message={
          pendingDeleteDM
            ? `Delete the DM "${pendingDeleteDM.label}"? This permanently removes the conversation and all its messages for everyone. This cannot be undone.`
            : undefined
        }
        confirmLabel="Delete"
        onCancel={() => {
          if (deleteConversation.isPending) return;
          setPendingDeleteDM(null);
          deleteConversation.reset();
        }}
        onConfirm={() => {
          if (!pendingDeleteDM?.id) return;
          const deletedPath = pendingDeleteDM.to;
          deleteConversation.mutate(pendingDeleteDM.id, {
            onSuccess: () => {
              if (location.pathname === deletedPath) {
                navigate(`${orgBase}/dms`);
              }
            },
            onSettled: () => setPendingDeleteDM(null),
          });
        }}
      />
    </div>
  );
}
