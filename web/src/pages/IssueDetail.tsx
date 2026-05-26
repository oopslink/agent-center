import type React from 'react';
import { useMemo, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  useConversation,
  useConversationRefs,
  useMessages,
} from '@/api/conversations';
import { useIssue, useReopenIssue } from '@/api/issues';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { CarryOverDivider } from '@/components/CarryOverDivider';
import { ConversationDeriveControls } from '@/components/ConversationDeriveControls';
import { IssueConcludeModal } from '@/components/IssueConcludeModal';
import { IssueEditModal } from '@/components/IssueEditModal';
import { useSelection } from '@/components/useSelection';
import { api } from '@/api/client';
import type { Message } from '@/api/types';
import { useQuery } from '@tanstack/react-query';

// IssueDetail page (/issues/:id).
//
// v2.3-5b route shape (per § 0.6, Option B): `:id` is now the
// ISSUE_ID (Discussion BC), not the conversation_id. The header is
// driven by the Issue projection from Discussion BC; the message
// thread + carry-over still pull from Conversation BC, scoped to the
// `conversation_id` the Issue projection points at. Conversation BC
// stays the owner of message content — only the header attribution
// (title / opener / opened_at / project link / status) is owed to
// Discussion BC, which now answers directly instead of being faked
// through `name` / `status` of the bound conversation.
export default function IssueDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const issue = useIssue(id);
  // Conversation lookups are enabled only after we know the bound
  // conversation_id from the Issue projection. Avoids an extra round
  // trip + a stale fetch when navigating between issues.
  const convId = issue.data?.conversation_id;
  const conv = useConversation(convId);
  const messages = useMessages(convId);
  const refs = useConversationRefs(convId);
  const selection = useSelection();
  const [concludeOpen, setConcludeOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const reopen = useReopenIssue(id);

  // Unique source conversation ids from the refs.
  const sourceIds = useMemo(() => {
    const set = new Set<string>();
    (refs.data ?? []).forEach((r) => set.add(r.source_conversation_id));
    return Array.from(set);
  }, [refs.data]);

  // One query per source conv. Each one fetches /messages and we pluck
  // referenced ids on the render side.
  const sourceMessages = useSourceMessages(sourceIds);

  if (issue.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-IssueDetail">
        Loading issue…
      </section>
    );
  }
  if (issue.isError) {
    return (
      <section className="space-y-3" data-testid="page-IssueDetail">
        <p className="text-sm text-danger" data-testid="issue-not-found">
          {(issue.error as Error).message}
        </p>
        <Link to="/issues" className="text-accent hover:underline">
          Back to issues
        </Link>
      </section>
    );
  }
  if (!issue.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-IssueDetail">
        Issue lookup failed.
      </section>
    );
  }

  const participants = conv.data?.participants ?? [];
  const iss = issue.data;
  const isTerminal =
    iss.status === 'closed_no_action' ||
    iss.status === 'closed_with_tasks' ||
    iss.status === 'withdrawn';

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-IssueDetail"
      data-issue-id={iss.id}
    >
      <header className="flex items-start justify-between border-b border-border-base pb-3">
        <div className="space-y-1">
          <h2 className="text-xl font-semibold">{iss.title || iss.id}</h2>
          <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
            <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
              {iss.status.replace(/_/g, ' ')}
            </span>
            <span>
              opened <span className="font-mono">{formatRelative(iss.opened_at)}</span> by{' '}
              <span className="font-mono">{iss.opener}</span>
            </span>
            {iss.project_id && (
              <Link
                to={`/projects/${encodeURIComponent(iss.project_id)}`}
                className="text-accent hover:underline"
                data-testid="issue-project-link"
              >
                project · {iss.project_id}
              </Link>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!isTerminal && (
            <button
              type="button"
              onClick={() => setEditOpen(true)}
              className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="issue-edit-button"
            >
              Edit
            </button>
          )}
          {!isTerminal && (
            <button
              type="button"
              onClick={() => setConcludeOpen(true)}
              className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
              data-testid="issue-conclude-button"
            >
              Conclude
            </button>
          )}
          {isTerminal && (
            <button
              type="button"
              onClick={() => reopen.mutate()}
              disabled={reopen.isPending}
              className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base disabled:opacity-50"
              data-testid="issue-reopen-button"
            >
              {reopen.isPending ? 'Reopening…' : 'Reopen'}
            </button>
          )}
          <button
            type="button"
            onClick={selection.toggleSelectMode}
            className={[
              'rounded px-2.5 py-1 text-xs font-medium',
              selection.selectMode
                ? 'bg-text-primary text-bg-elevated'
                : 'bg-bg-subtle text-text-primary hover:bg-border-base',
            ].join(' ')}
            data-testid="select-mode-toggle"
            aria-pressed={selection.selectMode}
          >
            {selection.selectMode ? 'Cancel select' : 'Select messages'}
          </button>
        </div>
      </header>
      {concludeOpen && (
        <IssueConcludeModal
          issueId={iss.id}
          onClose={() => setConcludeOpen(false)}
        />
      )}
      {editOpen && (
        <IssueEditModal
          issue={iss}
          onClose={() => setEditOpen(false)}
        />
      )}

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden p-4">
          <CarryOverDivider refs={refs.data ?? []} messages={sourceMessages} />
          {messages.isLoading && (
            <p className="text-sm text-text-muted" data-testid="issue-messages-loading">
              Loading messages…
            </p>
          )}
          {messages.isError && (
            <p className="text-sm text-danger" data-testid="issue-messages-error">
              {(messages.error as Error).message}
            </p>
          )}
          {messages.isSuccess && (
            <MessageList
              messages={messages.data}
              selectable={selection.selectMode}
              isSelected={selection.isSelected}
              onToggle={selection.toggle}
            />
          )}
          {convId && (
            <>
              <ConversationDeriveControls
                conversationId={convId}
                selection={selection}
              />
              <MessageComposer conversationId={convId} />
            </>
          )}
        </div>
        {convId && (
          <ParticipantsPanel conversationId={convId} participants={participants} />
        )}
      </div>
    </section>
  );
}

// useSourceMessages — fetches messages from each unique source conv id
// and returns a single flattened Message[]. Used by CarryOverDivider
// which filters by source_message_id.
function useSourceMessages(sourceIds: string[]): Message[] {
  // Single combined query keyed by the joined list keeps this hook
  // simple (avoids react-query's `useQueries` API churn).
  const key = sourceIds.slice().sort().join('|');
  const q = useQuery({
    queryKey: ['source-messages', key],
    queryFn: async () => {
      if (sourceIds.length === 0) return [];
      const lists = await Promise.all(
        sourceIds.map((id) =>
          api.get<Message[]>(`/conversations/${id}/messages`),
        ),
      );
      return lists.flat();
    },
    enabled: sourceIds.length > 0,
  });
  return q.data ?? [];
}

function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}
