import type React from 'react';
import type {
  ConversationMessageReference,
  Message,
} from '@/api/types';

interface Props {
  refs: ConversationMessageReference[];
  /** All messages from BOTH the source conv (carry-over snippets) +
   *  this child conv. We split them visually based on which ids are
   *  referenced. */
  messages: Message[];
}

// CarryOverDivider — renders the "from <source>" header + the carried
// messages, followed by a "Discussion below:" divider + the rest of
// the messages in this child conversation. Per CV3 (ADR-0035) the
// child's own messages have conversation_id == this conv; carried
// messages reference messages whose conversation_id is the SOURCE
// conv.
//
// If refs is empty (or no messages match), renders nothing — caller
// falls back to the normal MessageList rendering.
export function CarryOverDivider({
  refs,
  messages,
}: Props): React.ReactElement | null {
  if (refs.length === 0) return null;
  const referencedIds = new Set(refs.map((r) => r.source_message_id));
  const carried = messages.filter((m) => referencedIds.has(m.id));
  if (carried.length === 0) return null;
  // Group carried messages by source_conversation_id so the divider
  // can name each source.
  const bySource = new Map<string, Message[]>();
  for (const ref of refs) {
    const m = carried.find((mm) => mm.id === ref.source_message_id);
    if (!m) continue;
    const arr = bySource.get(ref.source_conversation_id) ?? [];
    arr.push(m);
    bySource.set(ref.source_conversation_id, arr);
  }

  return (
    <div className="space-y-3" data-testid="carry-over-section">
      {Array.from(bySource.entries()).map(([sourceConvId, msgs]) => (
        <section
          key={sourceConvId}
          className="rounded-lg border border-blue-200 bg-blue-50/40 p-3"
          data-testid="carry-over-block"
          data-source-conversation-id={sourceConvId}
        >
          <h4 className="mb-2 text-xs font-medium uppercase tracking-wide text-blue-700">
            from <span className="font-mono">{sourceConvId}</span>
          </h4>
          <ul className="space-y-2">
            {msgs.map((m) => (
              <li
                key={m.id}
                className="rounded border border-slate-200 bg-white p-2 text-xs"
                data-testid="carry-over-message"
                data-message-id={m.id}
              >
                <div className="mb-0.5 font-mono text-slate-500">
                  {m.sender_identity_id}
                </div>
                <div className="whitespace-pre-wrap text-slate-900">
                  {m.content}
                </div>
              </li>
            ))}
          </ul>
        </section>
      ))}
      <div
        className="my-2 flex items-center gap-2 text-xs uppercase tracking-wide text-slate-500"
        data-testid="carry-over-divider"
      >
        <span className="flex-1 border-t border-slate-200" />
        Discussion below
        <span className="flex-1 border-t border-slate-200" />
      </div>
    </div>
  );
}
