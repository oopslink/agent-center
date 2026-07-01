import { useQuery } from '@tanstack/react-query';
import { api } from './client';
import { qk } from './queryKeys';

// v2.26.0 I61 — the "Needs your attention" panel data source.
//
// GET /api/orgs/{slug}/attention → { items: AttentionItem[] }. The backend UNIONs
// two sources into ONE deduped, severity-then-recency sorted list (cap 100):
//   - kind=task    — actionable STUCK tasks (running + blocked_reason of type
//                    input_required→urgent / obstacle→warning). The panel's
//                    pre-existing source, now mirrored server-side.
//   - kind=mention — the human's DIRECTED unread: every unread DM + every unread
//                    @mention of them in any other conversation (channel / task /
//                    issue / plan). The I61 gap-fill: an agent→human escalation
//                    (@mention with NO human-owned task) surfaces here anyway.
// A mention whose conversation is already a kind=task item is deduped away by the
// backend (the task item is richer), so the panel renders `items` verbatim.

export type AttentionKind = 'task' | 'mention';
export type AttentionSeverity = 'urgent' | 'warning' | 'info';
export type StuckReasonType = 'input_required' | 'obstacle';

export interface AttentionItem {
  // Common envelope (always present).
  kind: AttentionKind;
  severity: AttentionSeverity;
  ref: string;
  title: string;
  snippet: string;
  actor: string;
  /** RFC3339Nano — used for the "N ago" time + stable ordering (already sorted). */
  ts: string;
  /** org-RELATIVE route (e.g. /projects/{pid}/tasks/{tid}, /dms/{id}); prefix with orgBase. */
  route: string;
  conversation_id: string;

  // kind=task extras.
  task_id?: string;
  reason_type?: StuckReasonType;
  project_id?: string;
  project_name?: string;
  org_ref?: string;

  // kind=mention extras.
  conversation_kind?: string;
  unread_count?: number;
  mention_count?: number;
  /** dismiss target: mark_seen advances the read cursor past this message id. */
  message_id?: string;
}

interface AttentionList {
  items: AttentionItem[];
}

// useAttention — the panel data source. Org-scoped (the /orgs/{slug} segment is
// auto-injected by the api client from the current URL); human-only on the
// backend (agents get an empty list) and fail-soft (a degraded source yields an
// empty band, never a 500). Returns the unified item list already sorted +
// deduped server-side, so the panel renders it as-is.
export function useAttention(slug: string | undefined) {
  const q = useQuery({
    queryKey: qk.attention(),
    queryFn: () => api.get<AttentionList>('/attention'),
    enabled: !!slug,
  });
  const items = q.data?.items ?? [];
  return { ...q, items };
}
