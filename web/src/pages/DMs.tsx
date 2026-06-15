import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  conversationDeleteErrorMessage,
  useConversations,
  useDeleteConversation,
} from '@/api/conversations';
import { DMStartModal } from '@/components/DMStartModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EntityRef } from '@/components/EntityRef';
import { UnreadBadge } from '@/components/UnreadBadge';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { useSSEConversationSubscribe } from '@/sse/useSSEConversationSubscribe';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { CONVERSATION_SEGMENTS } from './conversationSegments';

// DMList page (/dms). Lists kind=dm conversations + "Start a DM" button.
export default function DMs(): React.ReactElement {
  const dms = useConversations({ kind: 'dm' });
  const [startOpen, setStartOpen] = useState(false);
  // v2.7 #198: per-row delete (hard-delete) gated behind a confirm dialog.
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null);
  const del = useDeleteConversation();
  const navigate = useNavigate();
  useSSEConversationSubscribe(dms.data?.map((c) => c.id));

  return (
    <section className="space-y-4" data-testid="page-DMs">
      {/* v2.10.2 [T129] Mobile (<md): Conversations module 二级段控 (Channels |
          DMs) — desktop keeps the col② nav. */}
      <SegmentedNav items={CONVERSATION_SEGMENTS} ariaLabel="Conversations sections" />
      <header className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">DMs</h1>
        <button
          type="button"
          className="rounded bg-text-primary px-3 py-1.5 text-sm font-medium text-bg-elevated hover:opacity-90"
          onClick={() => setStartOpen(true)}
          data-testid="dms-new-button"
        >
          Start a DM
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
          title="No DMs yet"
          body="DMs are private conversations between two parties (human or agent). Start one to message someone directly."
          action={{ label: 'Start a DM', onClick: () => setStartOpen(true) }}
        />
      )}
      {dms.isSuccess && dms.data.length > 0 && (
        <ul className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-text-primary">
          {dms.data.map((c) => (
            <li key={c.id} data-testid="dm-row" data-dm-id={c.id} className="flex items-center">
              <OrgLink
                to={`/dms/${encodeURIComponent(c.id)}`}
                className="flex min-w-0 flex-1 items-center justify-between px-4 py-3 hover:bg-bg-subtle"
              >
                <span className="flex items-center gap-3">
                  {/* v2.7.1 #215 / Rule 2a: show the DM peer as @name (hover peer id,
                      #192); a deleted peer → "(deleted)"; a malformed DM (no peer)
                      → "Direct message". Never the raw conversation id. */}
                  {c.peer_identity_id ? (
                    <EntityRef
                      id={c.peer_identity_id}
                      name={c.peer_display_name ? `@${c.peer_display_name}` : undefined}
                      testId="dm-name"
                      className="font-medium"
                    />
                  ) : (
                    <span className="font-medium" data-testid="dm-name">Direct message</span>
                  )}
                  <UnreadBadge unreadCount={c.unread_count} mentionCount={c.mention_count} />
                  <span className="rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary">
                    {c.status}
                  </span>
                </span>
              </OrgLink>
              <button
                type="button"
                data-testid="dm-delete-button"
                data-dm-id={c.id}
                aria-label={`Delete DM ${c.name || c.id}`}
                title="Delete DM"
                onClick={() => {
                  del.reset();
                  setPendingDelete({ id: c.id, name: c.name || c.id });
                }}
                className="mr-2 shrink-0 rounded px-2 py-1 text-xs text-text-muted hover:bg-danger/10 hover:text-danger"
              >
                Delete
              </button>
            </li>
          ))}
        </ul>
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
        title="Delete DM"
        message={
          pendingDelete
            ? `Delete the DM "${pendingDelete.name}"? This permanently removes the conversation and all its messages for everyone. This cannot be undone.`
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
