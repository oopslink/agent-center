import type React from 'react';
import { Fragment, useMemo } from 'react';
import { useMembers, normalizeIdentityRef, identityRefOf } from '@/api/members';
import { useOrgWorkItems } from '@/api/orgWorkItems';
import { useOptionalOrgContext, orgPath } from '@/OrgContext';
import { taskDetailPath } from './TaskTitleLink';
import { idHandle } from './workItemDisplay';

// v2.8.1 #281 (mention-sidebar) entry ②: @mention tokens in message content.
//
// Message content is plain markdown; @mentions were NOT previously distinct
// tokens (the only "mention" concept was the conversation-level mention_count
// badge). So we add minimal, SAFE inline mention detection: an `@handle` whose
// handle resolves to a KNOWN org member (agent/user) becomes a clickable token
// that opens the kind-routed SenderDetailSidebar for that member's identity ref.
//
// Scope (kept surgical + safe):
//  - Only `@handle` that resolves to a known member is interactive; an unknown
//    `@foo` is left as plain text (no dangling/guessed ref → no wrong-kind body).
//  - Handle match is case-insensitive against the member display_name with
//    spaces removed AND against the bare identity tail (so `@BotOne` or
//    `@bot-1` both resolve). The resolved identity ref carries the kind prefix
//    (agent:/user:) so the sidebar routes to the right body (verify-not-trust).
//  - We only linkify plain text NOT inside code (the caller passes already-split
//    text nodes; this component just tokenizes one text string).

// useMentionResolver builds a handle→identity-ref map from the org members list
// (already loaded + cached). Keyed by BOTH a normalized display name (lowercased,
// spaces stripped) and the bare identity tail, so common author spellings of an
// @mention resolve. Value is the PREFIXED identity ref (user:/agent:) so the
// sidebar dispatches to the right kind.
export function useMentionResolver(): (handle: string) => string | null {
  const members = useMembers();
  const byHandle = useMemo(() => {
    const m = new Map<string, string>();
    for (const mem of members.data ?? []) {
      const ref = identityRefOf(mem);
      const tail = normalizeIdentityRef(mem.identity_id).toLowerCase();
      if (tail) m.set(tail, ref);
      if (mem.display_name) {
        const name = mem.display_name.toLowerCase().replace(/\s+/g, '');
        if (name) m.set(name, ref);
      }
    }
    return m;
  }, [members.data]);
  return (handle: string) => byHandle.get(handle.toLowerCase().replace(/\s+/g, '')) ?? null;
}

// ResolvedTaskRef — a `task-<id>` reference resolved to its display label + the
// task-detail href. `label` is the human Task id ("T123", org_ref) or the
// #id-tail handle when unallocated (#192 id-as-content). `href` routes to the
// task detail page, org-prefixed.
export interface ResolvedTaskRef {
  label: string;
  href: string;
}

// useTaskRefResolver builds a reference → { label, href } resolver from the ORG
// task list (GET /api/orgs/{slug}/tasks, cached) so a task reference in a message
// — even one pointing at ANOTHER project — linkifies to the task detail page with
// its "T123" label. v2.9.2 (task-82915d7c). Returns a function that yields null
// for an unknown / out-of-org reference, so it stays plain text instead of
// dangling to a wrong/forbidden target (verify-not-trust).
//
// The resolver accepts BOTH reference forms, keyed into one map:
//   - the bare task id `task-<id>` (the entity ref), and
//   - the human org_ref `T<number>` (e.g. "T123") — T76 (task-c780999a): chat
//     messages reference tasks by their T-number, both received and sent.
//
// T62 (task-336335c5): the list is fetched with status=all, NOT the default
// "all open" view. The default excludes terminal {completed, discarded}; agents
// reference completed tasks constantly, so a ref to one would silently stay
// plain text. status=all surfaces every status for reference resolution.
export function useTaskRefResolver(): (ref: string) => ResolvedTaskRef | null {
  const ctx = useOptionalOrgContext();
  const slug = ctx?.slug;
  const tasks = useOrgWorkItems('task', slug, { status: ['all'] });
  return useMemo(() => {
    const byRef = new Map<string, { label: string; projectId: string; taskId: string }>();
    for (const it of tasks.data?.items ?? []) {
      const entry = {
        label: it.org_ref || `#${idHandle(it.id)}`,
        projectId: it.project.id,
        taskId: it.id,
      };
      byRef.set(it.id, entry); // task-<id> form
      if (it.org_ref) byRef.set(it.org_ref, entry); // T76: T<number> org_ref form
    }
    return (ref: string): ResolvedTaskRef | null => {
      const entry = byRef.get(ref);
      if (!entry) return null;
      return {
        label: entry.label,
        href: orgPath(taskDetailPath(entry.projectId, entry.taskId), slug),
      };
    };
  }, [tasks.data, slug]);
}

// TOKEN_RE matches an @handle (group 1), a `task-<id>` reference (group 2), OR a
// `T<number>` org_ref (group 3) in one ordered pass, so a string carrying any mix
// linkifies correctly.
//   - @handle: @ + word/.-/_ chars. A leading boundary is enforced by the
//     surrounding split so we don't match an email's local-part-ish "@" mid-word.
//   - task-<id>: a NEGATIVE LOOKBEHIND `(?<![A-Za-z0-9])` guards the left
//     boundary so "subtask-1" does NOT match (only a standalone `task-…` does).
//     The id is [A-Za-z0-9]+ (hash tail / ULID), terminated by any other char
//     (so "task-x." stops at the dot). v2.9.2 (task-82915d7c).
//   - T<number> org_ref (T76 / task-c780999a): "T" + digits, guarded by BOTH a
//     negative lookbehind AND lookahead on [A-Za-z0-9] so it only matches a
//     STANDALONE token (not "T1" inside "PART1", "ROUTE53", "T12ab", etc.).
//     Resolution is still gated on a real org_ref (resolveTask returns null for
//     an unknown T-number → stays plain text), so a bare "T1" that is not a task
//     never becomes a (wrong) link.
const TOKEN_RE =
  /@([A-Za-z0-9][A-Za-z0-9._-]*)|(?<![A-Za-z0-9])(task-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(T\d+)(?![A-Za-z0-9])/g;

interface MentionTextProps {
  text: string;
  /** open the SenderDetailSidebar for the resolved (prefixed) identity ref. */
  onMention: (ref: string) => void;
  resolve: (handle: string) => string | null;
  /** link/mention text color — context-aware (mirrors MarkdownMessage's linkClass).
   * On the own chat bubble (#D1E3FF, a FIXED light surface that does NOT flip per
   * theme) this must be a FIXED-dark color (e.g. text-chatbubble-link), NOT the text-accent
   * theme token (#3b82f6 → blue-on-blue <4.5 on #D1E3FF — the both-mode 命门). */
  linkClass?: string;
  /** v2.9.2 (task-82915d7c) + T76 (task-c780999a): optional reference resolver.
   * Accepts EITHER a `task-<id>` or a `T<number>` org_ref; when provided, a
   * resolvable reference becomes a link to the task detail page labelled with its
   * "T123" org_ref; an unresolved reference stays plain text. */
  resolveTask?: (ref: string) => ResolvedTaskRef | null;
}

// MentionText tokenizes one plain-text string, turning each @handle that
// resolves to a known member into a clickable, keyboard-accessible mention
// button. Unresolved @handles stay plain text.
export function MentionText({
  text,
  onMention,
  resolve,
  linkClass = 'text-accent',
  resolveTask,
}: MentionTextProps): React.ReactElement {
  const parts: React.ReactNode[] = [];
  let last = 0;
  let match: RegExpExecArray | null;
  TOKEN_RE.lastIndex = 0;
  let key = 0;
  while ((match = TOKEN_RE.exec(text)) !== null) {
    const handle = match[1];
    // A task reference in either form: the bare `task-<id>` (group 2) or the
    // `T<number>` org_ref (group 3). Both resolve through the same resolveTask.
    const taskRef = match[2] ?? match[3];
    let node: React.ReactNode = null;
    if (handle !== undefined) {
      const ref = resolve(handle);
      if (ref) {
        node = (
          <button
            key={key++}
            type="button"
            // stopPropagation: a mention click must NOT bubble to the message-row /
            // other message handlers (#281 critical: no click-conflict).
            onClick={(e) => {
              e.stopPropagation();
              onMention(ref);
            }}
            data-testid="mention-token"
            data-mention-ref={ref}
            aria-label={`View ${handle} details`}
            // both-mode: the mention reads as a link and uses the SAME context-aware
            // linkClass as MarkdownMessage's links — text-accent on theme surfaces, but a
            // FIXED-dark color (text-chatbubble-link) on the own #D1E3FF bubble (avoids the
            // blue-on-blue <4.5 命门). NO alpha-tint fill (would render transparent).
            className={`rounded font-medium ${linkClass} hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`}
          >
            @{handle}
          </button>
        );
      }
    } else if (taskRef !== undefined && resolveTask) {
      const t = resolveTask(taskRef);
      if (t) {
        node = (
          <a
            key={key++}
            href={t.href}
            // New tab + opener/referrer guards, mirroring TaskTitleLink — opening
            // the task detail without losing the conversation. stopPropagation so a
            // ref click never bubbles to the message-row handlers (#281).
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => e.stopPropagation()}
            data-testid="task-ref-token"
            data-task-id={taskRef}
            title={`Open ${t.label} in a new tab`}
            // Same context-aware linkClass as mentions (both-mode AA on theme +
            // own-bubble surfaces); keyboard-accessible as a native anchor.
            className={`rounded font-medium ${linkClass} hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`}
          >
            {t.label}
          </a>
        );
      }
    }
    // Unresolved (or task linkify disabled) → leave the token as plain text: skip
    // without advancing `last`, so the matched chars stay in the next text slice.
    if (node === null) continue;
    if (match.index > last) parts.push(<Fragment key={key++}>{text.slice(last, match.index)}</Fragment>);
    parts.push(node);
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push(<Fragment key={key++}>{text.slice(last)}</Fragment>);
  return <>{parts}</>;
}
