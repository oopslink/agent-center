import type React from 'react';
import { OrgLink } from '@/OrgContext';
import { useState } from 'react';
import { useParams } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import {
  useBindTaskConversation,
  useResumeTask,
  useSuspendTask,
  useTask,
} from '@/api/tasks';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { ConversationDeriveControls } from '@/components/ConversationDeriveControls';
import { TaskAbandonModal } from '@/components/TaskAbandonModal';
import { TaskEditModal } from '@/components/TaskEditModal';
import { useSelection } from '@/components/useSelection';

// TaskDetail (/tasks/:id).
//
// v2.3-5b route shape (per § 0.6, Option B): `:id` is now the TASK_ID
// (TaskRuntime BC), not the conversation_id. Header is driven by the
// Task projection (title / status / priority / created_at / project
// link / optional current_execution_id); message thread + composer
// stay on Conversation BC, scoped to the `conversation_id` the Task
// projection points at.
export default function TaskDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const task = useTask(id);
  const convId = task.data?.conversation_id;
  const conv = useConversation(convId);
  const messages = useMessages(convId);
  const selection = useSelection();
  const [abandonOpen, setAbandonOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const suspend = useSuspendTask(id);
  const resume = useResumeTask(id);
  const bindConv = useBindTaskConversation(id);

  if (task.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-TaskDetail">
        Loading task…
      </section>
    );
  }
  if (task.isError) {
    return (
      <section className="space-y-3" data-testid="page-TaskDetail">
        <p className="text-sm text-danger" data-testid="task-not-found">
          {(task.error as Error).message}
        </p>
        <OrgLink to="/tasks" className="text-accent hover:underline">
          Back to tasks
        </OrgLink>
      </section>
    );
  }
  if (!task.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-TaskDetail">
        Task lookup failed.
      </section>
    );
  }

  const participants = conv.data?.participants ?? [];
  const tk = task.data;
  const isOpen = tk.status === 'open';
  const isSuspended = tk.status === 'suspended';
  const isTerminal = tk.status === 'done' || tk.status === 'abandoned';

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-TaskDetail"
      data-task-id={tk.id}
    >
      <header className="flex items-start justify-between border-b border-border-base pb-3">
        <div className="space-y-1">
          <h2 className="text-xl font-semibold">{tk.title || tk.id}</h2>
          <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
            <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
              {tk.status}
            </span>
            <span className="rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
              {tk.priority}
            </span>
            <span>
              created <span className="font-mono">{formatRelative(tk.created_at)}</span>
            </span>
            {tk.project_id && (
              <OrgLink
                to={`/projects/${encodeURIComponent(tk.project_id)}`}
                className="text-accent hover:underline"
                data-testid="task-project-link"
              >
                project · {tk.project_id}
              </OrgLink>
            )}
            {tk.current_execution_id && (
              <span className="font-mono">exec · {tk.current_execution_id}</span>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {!isTerminal && (
            <button
              type="button"
              onClick={() => setEditOpen(true)}
              className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="task-edit-button"
            >
              Edit
            </button>
          )}
          {isOpen && (
            <button
              type="button"
              onClick={() => suspend.mutate()}
              disabled={suspend.isPending}
              className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base disabled:opacity-50"
              data-testid="task-suspend-button"
            >
              {suspend.isPending ? 'Suspending…' : 'Suspend'}
            </button>
          )}
          {isSuspended && (
            <button
              type="button"
              onClick={() => resume.mutate()}
              disabled={resume.isPending}
              className="rounded bg-bg-subtle px-2.5 py-1 text-xs font-medium text-text-primary hover:bg-border-base disabled:opacity-50"
              data-testid="task-resume-button"
            >
              {resume.isPending ? 'Resuming…' : 'Resume'}
            </button>
          )}
          {!isTerminal && (
            <button
              type="button"
              onClick={() => setAbandonOpen(true)}
              className="rounded bg-danger px-2.5 py-1 text-xs font-medium text-white hover:opacity-90"
              data-testid="task-abandon-button"
            >
              Abandon
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
          <OrgLink
            to={`/tasks/${encodeURIComponent(tk.id)}/trace`}
            className="rounded bg-bg-subtle px-3 py-1.5 text-xs text-text-primary hover:bg-border-base"
            data-testid="task-view-trace"
          >
            View trace →
          </OrgLink>
        </div>
      </header>
      {abandonOpen && (
        <TaskAbandonModal
          taskId={tk.id}
          onClose={() => setAbandonOpen(false)}
        />
      )}
      {editOpen && (
        <TaskEditModal
          task={tk}
          onClose={() => setEditOpen(false)}
        />
      )}

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden">
          {!convId && (
            // v2.5.16 (#69): legacy task without a bound conversation.
            // Offer an explicit "Start discussion" CTA so the operator
            // can attach a chat thread on demand.
            <div
              className="m-4 flex flex-col items-start gap-3 rounded border border-dashed border-border-base bg-bg-subtle p-4 text-sm text-text-secondary"
              data-testid="task-no-conversation"
            >
              <div>
                <p className="font-medium text-text-primary">No discussion thread yet</p>
                <p className="mt-1 text-text-muted">
                  This task was created without a conversation. Start one to
                  chat about it with collaborators or agents.
                </p>
              </div>
              <button
                type="button"
                onClick={() => bindConv.mutate()}
                disabled={bindConv.isPending}
                data-testid="task-start-discussion"
                className="rounded bg-brand px-3 py-1.5 text-xs font-medium text-white hover:bg-brand-hover disabled:opacity-50"
              >
                {bindConv.isPending ? 'Starting…' : 'Start discussion'}
              </button>
              {bindConv.isError && (
                <p className="text-xs text-danger" data-testid="task-start-discussion-error">
                  {(bindConv.error as Error).message}
                </p>
              )}
            </div>
          )}
          {convId && messages.isLoading && (
            <p className="p-4 text-sm text-text-muted" data-testid="task-messages-loading">
              Loading messages…
            </p>
          )}
          {convId && messages.isError && (
            <p className="p-4 text-sm text-danger" data-testid="task-messages-error">
              {(messages.error as Error).message}
            </p>
          )}
          {convId && messages.isSuccess && (
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

function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}
