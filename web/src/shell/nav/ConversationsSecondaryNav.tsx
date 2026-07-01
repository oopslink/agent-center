import type React from 'react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, useLocation, useNavigate } from 'react-router-dom';
import {
  useConversations,
  useDeleteConversation,
  conversationDeleteErrorMessage,
} from '@/api/conversations';
import { UnreadBadge } from '@/components/UnreadBadge';
import { ConfirmModal } from '@/components/ConfirmModal';
import { dmDisplayName, dmParticipantLabels } from '@/components/dmDisplay';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';
import type { Conversation } from '@/api/types';
import { UnreadConversationsSection } from './UnreadConversationsSection';
import { useListOrder, rowDragClass, type ListOrder } from './useListOrder';

// orderList — apply a ListOrder's saved order to the live items, dropping any id
// the order knows about but the data no longer has.
function orderList<T>(order: ListOrder, items: readonly T[], getId: (t: T) => string): T[] {
  const byId = new Map(items.map((it) => [getId(it), it]));
  return order.orderedIds.map((id) => byId.get(id)).filter((t): t is T => t !== undefined);
}

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

// T321: the DM subgroup collapse state is persisted in localStorage (keyed by
// group) so a collapsed "My DMs" / "A2A" stays collapsed across navigation —
// mirroring the shell's group-expand persistence.
const DM_GROUP_COLLAPSE_KEY = 'conv-dm-group-collapsed';
function readDmGroupCollapsed(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(DM_GROUP_COLLAPSE_KEY);
    return raw ? (JSON.parse(raw) as Record<string, boolean>) : {};
  } catch {
    return {};
  }
}

// SubHeader — a small in-section group label (e.g. "My DMs" / "A2A"), lighter
// than the SectionHeader so DM subgroups read as nested groups (T308). T321: it
// is now a collapse toggle (chevron rotates when open, aria-expanded) so a long
// DM subgroup can be folded away.
function SubHeader({
  label,
  collapsed,
  onToggle,
  testId,
}: {
  label: string;
  collapsed: boolean;
  onToggle: () => void;
  testId?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={!collapsed}
      data-testid={testId}
      className="flex w-full items-center gap-1 px-2 pb-0.5 pt-2 text-[0.625rem] font-medium uppercase tracking-wide text-text-muted hover:text-text-secondary"
    >
      <svg
        viewBox="0 0 12 12"
        aria-hidden="true"
        className={`h-2.5 w-2.5 shrink-0 transition-transform ${collapsed ? '' : 'rotate-90'}`}
      >
        <path d="M4 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      <span>{label}</span>
    </button>
  );
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
  const { t } = useTranslation('common');
  const location = useLocation();
  const navigate = useNavigate();
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });
  const deleteConversation = useDeleteConversation();
  const [pendingDeleteDM, setPendingDeleteDM] = useState<{ id: string; to: string; label: string } | null>(
    null,
  );
  // T321: per-DM-subgroup collapse state (persisted), default expanded.
  const [collapsedGroups, setCollapsedGroups] = useState<Record<string, boolean>>(readDmGroupCollapsed);
  const isGroupCollapsed = (key: string): boolean => !!collapsedGroups[key];
  const toggleGroup = (key: string): void =>
    setCollapsedGroups((m) => {
      const next = { ...m, [key]: !m[key] };
      try {
        localStorage.setItem(DM_GROUP_COLLAPSE_KEY, JSON.stringify(next));
      } catch {
        /* ignore quota / disabled storage */
      }
      return next;
    });

  const activeChannels = (channels.data ?? []).filter((c) => c.status !== 'archived');
  const dmList = dms.data ?? [];
  // T308: group DMs like the DMs page — "My DMs" (the viewer is a participant)
  // vs "Agent-to-agent" (between two agents; the human viewer isn't a peer, so
  // these previously all read "Direct message"). The agent rows now show "@A ↔ @B".
  const agentAgentDMs = dmList.filter((d) => d.dm_type === 'agent_agent_dm');
  const myDMs = dmList.filter((d) => d.dm_type !== 'agent_agent_dm');

  // Drag-reorder (per-user, persisted) for each col② list — @oopslink.
  const channelOrder = useListOrder(`${orgBase}/conv/channels`, activeChannels.map((c) => c.id));
  const myDMOrder = useListOrder(`${orgBase}/conv/dms.mine`, myDMs.map((d) => d.id));
  const a2aOrder = useListOrder(`${orgBase}/conv/dms.a2a`, agentAgentDMs.map((d) => d.id));
  const orderedChannels = orderList(channelOrder, activeChannels, (c) => c.id);
  const orderedMyDMs = orderList(myDMOrder, myDMs, (d) => d.id);
  const orderedA2ADMs = orderList(a2aOrder, agentAgentDMs, (d) => d.id);

  // A DM whose peer identity exists but whose display name is gone = a
  // deleted-peer DM; it keeps a manual delete action (same rule as the prior
  // shell default: canDelete = has peer ref but no resolvable display name).
  const dmLabel = (d: Conversation): string =>
    d.dm_type === 'agent_agent_dm'
      ? dmDisplayName(d)
      : d.peer_display_name
        ? `@${d.peer_display_name}`
        : d.peer_identity_id
          ? t('shell.conv.deletedPeer')
          : t('shell.conv.directMessageFallback');
  // T344 (@oopslink: "电脑端也要支持删除 DM"): desktop now allows deleting ANY DM
  // (mobile already did via the DMs page). The backend lets an org owner delete a
  // DM even when not a participant — so stray DMs (e.g. old single-party "Reminder"
  // DMs) can be cleaned up from the rail. Was previously gated to deleted-peer DMs.
  const dmCanDelete = (_d: Conversation): boolean => true;

  // One DM row (NavLink + optional delete) — shared by both groups. T318: an
  // agent↔agent row STACKS its two participants on separate lines ("@A" / "↔ @B")
  // so both agents stay legible in the narrow rail (the old single-line "@A ↔ @B"
  // truncated the second name). The per-row "agent" tag is dropped — the
  // "Agent-to-agent" group header already conveys the kind, and it was eating the
  // width that caused the truncation.
  const renderDmRow = (d: Conversation, order: ListOrder): React.ReactElement => {
    const isA2A = d.dm_type === 'agent_agent_dm';
    const a2aLabels = isA2A ? dmParticipantLabels(d) : [];
    return (
    <li key={d.id} {...order.rowProps(d.id)} className={rowDragClass(order, d.id)}>
      <div className="flex items-center gap-1">
        <NavLink
          to={`${orgBase}/dms/${encodeURIComponent(d.id)}`}
          className={rowClass}
          data-testid="conv-nav-dm"
          data-dm-type={d.dm_type ?? 'my_dm'}
        >
          {isA2A && a2aLabels.length > 0 ? (
            <span className="flex min-w-0 flex-1 flex-col gap-0.5 py-0.5" data-testid="conv-nav-dm-a2a">
              {a2aLabels.map((label, i) => (
                <span key={i} className="flex min-w-0 items-center gap-1 leading-tight">
                  {i > 0 && (
                    <span aria-hidden="true" className="shrink-0 text-[0.625rem] text-text-muted">
                      ↔
                    </span>
                  )}
                  <span className="min-w-0 truncate">{label}</span>
                </span>
              ))}
            </span>
          ) : (
            <span className="flex min-w-0 flex-1 items-center gap-2">
              <span className="min-w-0 truncate">{dmLabel(d)}</span>
            </span>
          )}
          <UnreadBadge unreadCount={d.unread_count} mentionCount={d.mention_count} />
        </NavLink>
        {dmCanDelete(d) && (
          <button
            type="button"
            className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-text-muted hover:bg-danger/10 hover:text-danger"
            data-testid="sidebar-dm-delete-button"
            aria-label={t('shell.conv.deleteDmLabel', { name: dmLabel(d) })}
            title={t('shell.conv.deleteDmTitle')}
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
    );
  };

  return (
    <div className="space-y-4" data-testid="conversations-secondary-nav">
      {/* I23 (T332): cross-source "未读会话" digest — dynamic, only when unread. */}
      <UnreadConversationsSection orgBase={orgBase} />

      {/* Channels */}
      <div>
        <SectionHeader
          label={t('shell.conv.channels')}
          createTo={`${orgBase}/channels`}
          createTestId="conv-new-channel"
          createLabel={t('shell.conv.newChannel')}
        />
        <ul className="space-y-0.5">
          {activeChannels.length === 0 && <EmptyRow text={t('shell.conv.noChannels')} />}
          {orderedChannels.map((c) => (
            <li key={c.id} {...channelOrder.rowProps(c.id)} className={rowDragClass(channelOrder, c.id)}>

              <NavLink
                to={`${orgBase}/channels/${encodeURIComponent(c.id)}`}
                className={rowClass}
                data-testid="conv-nav-channel"
              >
                <span className="flex min-w-0 flex-1 items-center gap-2">
                  <span aria-hidden="true" className="shrink-0 text-text-muted">
                    #
                  </span>
                  <span className="min-w-0 truncate">{c.name}</span>
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
          label={t('shell.conv.directMessages')}
          createTo={`${orgBase}/dms`}
          createTestId="conv-new-dm"
          createLabel={t('shell.conv.newDirectMessage')}
        />
        {dmList.length === 0 && (
          <ul className="space-y-0.5">
            <EmptyRow text={t('shell.conv.noDirectMessages')} />
          </ul>
        )}
        {/* My DMs (the subheader only appears when there are ALSO agent-agent DMs,
            so a viewer with only personal DMs sees a clean flat list). T321: the
            subheader collapses its list; with no subheader the list always shows. */}
        {myDMs.length > 0 && (
          <>
            {agentAgentDMs.length > 0 && (
              <SubHeader
                label={t('shell.conv.myDms')}
                collapsed={isGroupCollapsed('mine')}
                onToggle={() => toggleGroup('mine')}
                testId="conv-nav-subheader-mine"
              />
            )}
            {!(agentAgentDMs.length > 0 && isGroupCollapsed('mine')) && (
              <ul className="space-y-0.5" data-testid="conv-nav-dms-mine">
                {orderedMyDMs.map((d) => renderDmRow(d, myDMOrder))}
              </ul>
            )}
          </>
        )}
        {/* A2A (agent-to-agent) DMs — rows show the two agents stacked (T318). */}
        {agentAgentDMs.length > 0 && (
          <>
            <SubHeader
              label={t('shell.conv.a2a')}
              collapsed={isGroupCollapsed('a2a')}
              onToggle={() => toggleGroup('a2a')}
              testId="conv-nav-subheader-a2a"
            />
            {!isGroupCollapsed('a2a') && (
              <ul className="space-y-0.5" data-testid="conv-nav-dms-agent">
                {orderedA2ADMs.map((d) => renderDmRow(d, a2aOrder))}
              </ul>
            )}
          </>
        )}
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
        title={t('shell.conv.deleteDmTitle')}
        message={
          pendingDeleteDM
            ? t('shell.conv.deleteDmConfirm', { name: pendingDeleteDM.label })
            : undefined
        }
        confirmLabel={t('shell.conv.deleteConfirmLabel')}
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
