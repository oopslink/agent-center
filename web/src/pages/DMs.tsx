import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import {
  conversationDeleteErrorMessage,
  useConversations,
  useDeleteConversation,
} from '@/api/conversations';
import { DMStartModal } from '@/components/DMStartModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityRef } from '@/components/EntityRef';
import { dmDisplayName } from '@/components/dmDisplay';
import { UnreadBadge } from '@/components/UnreadBadge';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { CONVERSATION_SEGMENTS } from './conversationSegments';

// DMList page (/dms). Lists kind=dm conversations + "Start a DM" button.
export default function DMs(): React.ReactElement {
  const { t } = useTranslation('chat');
  const dms = useConversations({ kind: 'dm' });
  const [view, setView] = useState<'mine' | 'agent_agent' | 'system'>('mine');
  const [startOpen, setStartOpen] = useState(false);
  // v2.7 #198: per-row delete (hard-delete) gated behind a confirm dialog.
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);
  const del = useDeleteConversation();
  const navigate = useNavigate();
  useSSEConversationSubscribe(dms.data?.map((c) => c.id));
  const agentAgentDMs = dms.data?.filter((c) => c.dm_type === 'agent_agent_dm') ?? [];
  const systemDMs = dms.data?.filter((c) => c.dm_type === 'system_dm') ?? [];
  const myDMs =
    dms.data?.filter((c) => c.dm_type !== 'agent_agent_dm' && c.dm_type !== 'system_dm') ?? [];
  const visibleDMs = view === 'agent_agent' ? agentAgentDMs : view === 'system' ? systemDMs : myDMs;

  return (
    <section className="space-y-4" data-testid="page-DMs">
      {/* v2.10.2 [T129] Mobile (<md): Conversations module 二级段控 (Channels |
          DMs) — desktop keeps the col② nav. */}
      <SegmentedNav items={CONVERSATION_SEGMENTS} ariaLabel={t('dms.segmentedNavAriaLabel')} />
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t('dms.title')}</h1>
        <button
          type="button"
          className="rounded bg-btn-primary-bg px-3 py-1.5 text-sm font-medium text-btn-primary-fg hover:opacity-90"
          onClick={() => setStartOpen(true)}
          data-testid="dms-new-button"
        >
          {t('dms.startButton')}
        </button>
      </header>

      {dms.isLoading && (
        <div className="space-y-2" data-testid="dms-loading">
          <Skeleton height="2.5rem" />
          <Skeleton height="2.5rem" />
        </div>
      )}
      {dms.isError && (
        <p className="text-sm text-danger" data-testid="dms-error">
          {(dms.error as Error).message}
        </p>
      )}
      {dms.isSuccess && dms.data.length === 0 && (
        <EmptyState
          testId="dms-empty"
          title={t('dms.empty.title')}
          body={t('dms.empty.body')}
          action={{ label: t('dms.startButton'), onClick: () => setStartOpen(true) }}
        />
      )}
      {dms.isSuccess && dms.data.length > 0 && (
        <div className="space-y-3">
          <div
            className="inline-flex rounded border border-border-base bg-bg-elevated p-0.5 text-sm"
            role="tablist"
            aria-label={t('dms.viewsAriaLabel')}
          >
            <button
              type="button"
              role="tab"
              aria-selected={view === 'mine'}
              onClick={() => setView('mine')}
              data-testid="dms-tab-mine"
              className={`rounded px-3 py-1 min-h-[44px] md:min-h-0 ${
                view === 'mine' ? 'bg-bg-subtle text-text-primary' : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {t('dms.tabs.mine')}
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={view === 'agent_agent'}
              onClick={() => setView('agent_agent')}
              data-testid="dms-tab-agent-agent"
              className={`rounded px-3 py-1 min-h-[44px] md:min-h-0 ${
                view === 'agent_agent' ? 'bg-bg-subtle text-text-primary' : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {t('dms.tabs.agentToAgent')}
              {agentAgentDMs.length > 0 && (
                <span className="ml-1 text-xs text-text-muted">({agentAgentDMs.length})</span>
              )}
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={view === 'system'}
              onClick={() => setView('system')}
              data-testid="dms-tab-system"
              className={`rounded px-3 py-1 min-h-[44px] md:min-h-0 ${
                view === 'system' ? 'bg-bg-subtle text-text-primary' : 'text-text-secondary hover:text-text-primary'
              }`}
            >
              {t('dms.tabs.system')}
              {systemDMs.length > 0 && (
                <span className="ml-1 text-xs text-text-muted">({systemDMs.length})</span>
              )}
            </button>
          </div>
          {visibleDMs.length === 0 ? (
            <EmptyState
              testId="dms-filter-empty"
              title={
                view === 'agent_agent'
                  ? t('dms.filterEmpty.agent.title')
                  : view === 'system'
                    ? t('dms.filterEmpty.system.title')
                    : t('dms.filterEmpty.personal.title')
              }
              body={
                view === 'agent_agent'
                  ? t('dms.filterEmpty.agent.body')
                  : view === 'system'
                    ? t('dms.filterEmpty.system.body')
                    : t('dms.filterEmpty.personal.body')
              }
            />
          ) : (
            <ul className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary">
              {visibleDMs.map((c) => (
                <li key={c.id} data-testid="dm-row" data-dm-id={c.id} className="flex items-center">
                  <OrgLink
                    to={`/dms/${encodeURIComponent(c.id)}`}
                    className="flex min-w-0 flex-1 items-center justify-between px-4 py-3 hover:bg-bg-subtle"
                  >
                    <span className="flex items-center gap-3">
                      {/* v2.7.1 #215 / Rule 2a: show the DM peer as @name (hover peer id,
                      #192); a deleted peer → "(deleted)"; a malformed DM (no peer)
                      → "Direct message". Never the raw conversation id. */}
                      {c.dm_type === 'agent_agent_dm' ? (
                        <span className="font-medium" data-testid="dm-name">
                          {dmDisplayName(c)}
                        </span>
                      ) : c.peer_identity_id ? (
                        <EntityRef
                          id={c.peer_identity_id}
                          name={c.peer_display_name ? `@${c.peer_display_name}` : undefined}
                          testId="dm-name"
                          className="font-medium"
                        />
                      ) : (
                        <span className="font-medium" data-testid="dm-name">{t('dms.directMessage')}</span>
                      )}
                      <UnreadBadge unreadCount={c.unread_count} mentionCount={c.mention_count} />
                      <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                        {c.status}
                      </span>
                      {c.dm_type === 'agent_agent_dm' && (
                        <span className="rounded bg-status-blue-bg px-2 py-0.5 text-xs font-semibold uppercase text-status-blue-fg">
                          {t('dms.agentAgentBadge')}
                        </span>
                      )}
                    </span>
                  </OrgLink>
                  <button
                    type="button"
                    data-testid="dm-delete-button"
                    data-dm-id={c.id}
                    aria-label={t('dms.deleteAriaLabel', { name: c.name || c.id })}
                    title={t('dms.deleteTitle')}
                    onClick={() => {
                      del.reset();
                      setPendingDelete({ id: c.id, name: c.name || c.id });
                    }}
                    className="mr-2 shrink-0 rounded px-2 py-2 md:py-1 text-xs text-text-muted hover:bg-danger/10 hover:text-danger"
                  >
                    {t('dms.deleteButton')}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      {del.isError && (
        <p className="text-sm text-danger" data-testid="dm-delete-error" role="alert">
          {conversationDeleteErrorMessage(del.error)}
        </p>
      )}

      <DMStartModal
        open={startOpen}
        onClose={() => setStartOpen(false)}
        onCreated={(id) => navigate(`/dms/${encodeURIComponent(id)}`)}
      />

      <ConfirmModal
        open={pendingDelete !== null}
        danger
        busy={del.isPending}
        title={t('dms.deleteConfirm.title')}
        message={
          pendingDelete
            ? t('dms.deleteConfirm.message', { name: pendingDelete.name })
            : undefined
        }
        confirmLabel={t('dms.deleteConfirm.confirmLabel')}
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

