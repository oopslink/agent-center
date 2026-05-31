import type { Message } from '@/api/types';

// MessageSegment (#137) — a contiguous run of messages sharing one
// work_item_ref. Unassociated messages (no context_refs) form their own
// segment with workItemRef === ''.
export interface MessageSegment {
  // Stable, unique React key. A work item that is re-dispatched produces
  // two separate segments, so the key includes the run's start index.
  key: string;
  workItemRef: string;
  label: string;
  messages: Message[];
}

const UNASSOCIATED_LABEL = '未关联工作项';

// groupMessagesByWorkItem splits a chronological message list into
// contiguous segments keyed by work_item_ref. A segment boundary is drawn
// whenever the ref changes from the previous message, so:
//   - consecutive same-WI messages group together;
//   - a re-dispatched work item (its ref reappearing after an interruption)
//     becomes a NEW segment rather than merging back (no mis-merge);
//   - messages with no work item land in a labeled "未关联工作项" segment in
//     chronological position (no silent drop).
export function groupMessagesByWorkItem(messages: Message[]): MessageSegment[] {
  const segments: MessageSegment[] = [];
  let current: MessageSegment | null = null;
  messages.forEach((m, i) => {
    const ref = m.context_refs?.work_item_ref ?? '';
    if (!current || current.workItemRef !== ref) {
      current = {
        key: `${i}:${ref}`,
        workItemRef: ref,
        label: ref ? `工作项 ${ref}` : UNASSOCIATED_LABEL,
        messages: [],
      };
      segments.push(current);
    }
    current.messages.push(m);
  });
  return segments;
}
