import type React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';

// TaskDetail (/tasks/:id). Renders the task conversation + a link to
// the trace view; the trace itself lives at /tasks/:id/trace so users
// can deep-link directly to it without the composer noise.
export default function TaskDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const conv = useConversation(id);
  const messages = useMessages(id);

  if (conv.isLoading) {
    return (
      <section className="text-sm text-slate-500" data-testid="page-TaskDetail">
        Loading task…
      </section>
    );
  }
  if (conv.isError) {
    return (
      <section className="space-y-3" data-testid="page-TaskDetail">
        <p className="text-sm text-red-600" data-testid="task-not-found">
          {(conv.error as Error).message}
        </p>
        <Link to="/tasks" className="text-blue-600 hover:underline">
          Back to tasks
        </Link>
      </section>
    );
  }
  if (!conv.data) {
    return (
      <section className="text-sm text-red-600" data-testid="page-TaskDetail">
        Task lookup failed.
      </section>
    );
  }

  const participants = conv.data.participants ?? [];

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-TaskDetail"
      data-task-id={conv.data.id}
    >
      <header className="flex items-start justify-between border-b border-slate-200 pb-3">
        <div>
          <h2 className="text-xl font-semibold">{conv.data.name || conv.data.id}</h2>
          {conv.data.description && (
            <p className="text-sm text-slate-500">{conv.data.description}</p>
          )}
          <p className="text-xs text-slate-500">
            state: <span className="font-mono">{conv.data.status}</span>
          </p>
        </div>
        <Link
          to={`/tasks/${encodeURIComponent(conv.data.id)}/trace`}
          className="rounded bg-slate-100 px-3 py-1.5 text-xs text-slate-700 hover:bg-slate-200"
          data-testid="task-view-trace"
        >
          View trace →
        </Link>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden">
          {messages.isLoading && (
            <p className="p-4 text-sm text-slate-500" data-testid="task-messages-loading">
              Loading messages…
            </p>
          )}
          {messages.isError && (
            <p className="p-4 text-sm text-red-600" data-testid="task-messages-error">
              {(messages.error as Error).message}
            </p>
          )}
          {messages.isSuccess && <MessageList messages={messages.data} />}
          <MessageComposer conversationId={conv.data.id} />
        </div>
        <ParticipantsPanel conversationId={conv.data.id} participants={participants} />
      </div>
    </section>
  );
}
