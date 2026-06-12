import type React from 'react';
import type { Message } from '@/api/types';
import { useThreadReplies } from '@/api/conversations';
import { useModalA11y } from './useModalA11y';
import { MessageList } from './MessageList';
import { MessageComposer } from './MessageComposer';

// ThreadSidebar (v2.9.1 Threads P1) — a right slide-in panel that opens a single
// message's thread: the root message + all its replies + a thread composer that
// sends replies (POST carries parent_message_id). Surface-agnostic — opened from
// any conversation type via the per-message ThreadButton.
//
// Reuse: the body renders root + replies through the SAME <MessageList> as the
// main conversation (avatars / own-styling / markdown stay consistent), with
// `showThreads={false}` so a reply never grows its own thread button — P1 is a
// single level (no thread-in-thread).
//
// a11y / chrome modeled on SenderDetailSidebar: useModalA11y (Esc close + Tab
// focus-trap + focus-restore), role="dialog" + aria-modal + aria-label, dimmed
// click-to-close overlay, an ASCII "X" close button (no emoji-icon), solid theme
// tokens only (no alpha-tint), both-mode AA.

interface Props {
  open: boolean;
  /** the root (top-level) message whose thread is shown; null when closed. */
  rootMessage: Message | null;
  onClose: () => void;
}

export function ThreadSidebar({ open, rootMessage, onClose }: Props): React.ReactElement | null {
  const containerRef = useModalA11y({ open, onClose });
  const conversationId = rootMessage?.conversation_id;
  // Gated on open + a root id so a closed sidebar fires no request.
  const replies = useThreadReplies(
    open ? conversationId : undefined,
    open ? rootMessage?.id : undefined,
  );

  if (!open || !rootMessage) return null;

  const replyList = replies.data ?? [];
  // The root message renders at the top, followed by its replies, all through
  // the shared MessageList (thread affordance suppressed inside the thread).
  const threadMessages: Message[] = [rootMessage, ...replyList];

  return (
    <>
      {/* Dimmed-but-visible overlay — click to close. */}
      <div
        className="fixed inset-0 z-30 bg-black/30"
        data-testid="thread-sidebar-overlay"
        onClick={onClose}
        aria-hidden="true"
      />
      <div
        ref={containerRef}
        role="dialog"
        aria-modal="true"
        aria-label="Message thread"
        data-testid="thread-sidebar"
        className="fixed inset-y-0 right-0 z-40 flex h-full w-80 translate-x-0 transform flex-col border-l border-border-base bg-bg-elevated text-text-primary shadow-2 transition-transform duration-200 ease-out motion-reduce:transition-none sm:w-[28rem]"
      >
        {/* Header: title + reply count + close. */}
        <div className="flex items-center justify-between gap-3 border-b border-border-base p-4">
          <div className="min-w-0">
            <div className="text-base font-semibold" data-testid="thread-sidebar-title">
              Thread
            </div>
            {/* §-1 Finding 2: readable subtitle uses text-text-secondary (AA in
                both modes), NOT the sub-AA text-text-muted (B2 / decision #2). */}
            <div className="text-xs text-text-secondary" data-testid="thread-sidebar-subtitle">
              {replyList.length === 0
                ? 'No replies yet'
                : `${replyList.length} ${replyList.length === 1 ? 'reply' : 'replies'}`}
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            data-testid="thread-sidebar-close"
            aria-label="Close thread"
            className="rounded p-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
          >
            {/* plain ASCII "X" (NOT U+2715) per the no-emoji-icon a11y guardrail. */}
            <span aria-hidden="true">X</span>
          </button>
        </div>

        {/* Body: root message + replies, via the shared MessageList. */}
        <div className="flex min-h-0 flex-1 flex-col">
          {replies.isError ? (
            <p className="p-4 text-sm text-danger" data-testid="thread-sidebar-error">
              {(replies.error as Error).message}
            </p>
          ) : (
            <MessageList messages={threadMessages} surface="task-thread" showThreads={false} />
          )}
        </div>

        {/* Footer: the thread composer — every send carries parent_message_id. */}
        {conversationId && (
          <MessageComposer conversationId={conversationId} parentMessageId={rootMessage.id} />
        )}
      </div>
    </>
  );
}
