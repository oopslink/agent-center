import type React from 'react';
import { Fragment, useMemo } from 'react';
import { useMembers, normalizeIdentityRef, identityRefOf } from '@/api/members';
import { useOrgWorkItems } from '@/api/orgWorkItems';
import { useOrgPlans } from '@/api/plans';
import { useOptionalOrgContext, orgPath } from '@/OrgContext';
import { taskDetailPath } from './TaskTitleLink';
import { refLabel } from './workItemDisplay';

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
        label: refLabel(it.org_ref, it.id),
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

// ResolvedPlanRef — a `plan-<id>` / `P<number>` reference resolved to its display
// label + the plan-detail href. v2.10.1 [T99] (symmetric with ResolvedTaskRef).
export interface ResolvedPlanRef {
  label: string;
  href: string;
}

// planDetailPath — the plan detail route (mirrors taskDetailPath); org-prefixed
// by the caller via orgPath.
function planDetailPath(projectId: string, planId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(planId)}`;
}

// usePlanRefResolver builds a reference → { label, href } resolver from the ORG
// plan list (GET /api/orgs/{slug}/plans, cached), symmetric with
// useTaskRefResolver. Accepts BOTH the bare `plan-<id>` and the human `P<number>`
// org_ref (T99). Yields null for an unknown / out-of-org reference so it stays
// plain text (verify-not-trust). Archived-project plans are excluded by the list
// default, so a ref to one stays plain text.
export function usePlanRefResolver(): (ref: string) => ResolvedPlanRef | null {
  const ctx = useOptionalOrgContext();
  const slug = ctx?.slug;
  const plans = useOrgPlans(slug);
  return useMemo(() => {
    const byRef = new Map<string, { label: string; projectId: string; planId: string }>();
    for (const it of plans.data?.items ?? []) {
      const entry = {
        label: refLabel(it.org_ref, it.id),
        projectId: it.project.id,
        planId: it.id,
      };
      byRef.set(it.id, entry); // plan-<id> form
      if (it.org_ref) byRef.set(it.org_ref, entry); // P<number> org_ref form
    }
    return (ref: string): ResolvedPlanRef | null => {
      const entry = byRef.get(ref);
      if (!entry) return null;
      return {
        label: entry.label,
        href: orgPath(planDetailPath(entry.projectId, entry.planId), slug),
      };
    };
  }, [plans.data, slug]);
}

// ResolvedIssueRef — an `issue-<id>` / `I<number>` reference resolved to its
// display label + the issue-detail href. Symmetric with ResolvedTaskRef /
// ResolvedPlanRef.
export interface ResolvedIssueRef {
  label: string;
  href: string;
}

// issueDetailPath — the issue detail route (mirrors taskDetailPath /
// planDetailPath); org-prefixed by the caller via orgPath.
function issueDetailPath(projectId: string, issueId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/issues/${encodeURIComponent(issueId)}`;
}

// useIssueRefResolver builds a reference → { label, href } resolver from the ORG
// issue list (GET /api/orgs/{slug}/issues, cached), symmetric with
// useTaskRefResolver / usePlanRefResolver. Accepts BOTH the bare `issue-<id>` and
// the human `I<number>` org_ref. status=['all'] so a ref to a closed issue still
// resolves (mirrors T62's status=all for tasks). Yields null for an unknown /
// out-of-org reference so it stays plain text (verify-not-trust).
export function useIssueRefResolver(): (ref: string) => ResolvedIssueRef | null {
  const ctx = useOptionalOrgContext();
  const slug = ctx?.slug;
  const issues = useOrgWorkItems('issue', slug, { status: ['all'] });
  return useMemo(() => {
    const byRef = new Map<string, { label: string; projectId: string; issueId: string }>();
    for (const it of issues.data?.items ?? []) {
      const entry = {
        label: refLabel(it.org_ref, it.id),
        projectId: it.project.id,
        issueId: it.id,
      };
      byRef.set(it.id, entry); // issue-<id> form
      if (it.org_ref) byRef.set(it.org_ref, entry); // I<number> org_ref form
    }
    return (ref: string): ResolvedIssueRef | null => {
      const entry = byRef.get(ref);
      if (!entry) return null;
      return {
        label: entry.label,
        href: orgPath(issueDetailPath(entry.projectId, entry.issueId), slug),
      };
    };
  }, [issues.data, slug]);
}

// TOKEN_RE matches an @handle (group 1), a `task-<id>` reference (group 2), a
// `T<number>` org_ref (group 3), a `plan-<id>` reference (group 4), a
// `P<number>` org_ref (group 5), an `issue-<id>` reference (group 6), OR an
// `I<number>` org_ref (group 7) in one ordered pass, so a string carrying any mix
// linkifies correctly. The plan groups (T99) and issue groups mirror the task
// groups exactly (same boundary guards); `P\d+` / `I\d+` are case-sensitive so
// lowercase "plan" / "issue" never match them, and "I" must be followed by digits
// (so the English pronoun "I" never matches).
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
  /@([A-Za-z0-9][A-Za-z0-9._-]*)|(?<![A-Za-z0-9])(task-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(T\d+)(?![A-Za-z0-9])|(?<![A-Za-z0-9])(plan-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(P\d+)(?![A-Za-z0-9])|(?<![A-Za-z0-9])(issue-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(I\d+)(?![A-Za-z0-9])/g;

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
  /** v2.10.1 [T99]: optional plan reference resolver. Accepts EITHER a
   * `plan-<id>` or a `P<number>` org_ref; a resolvable reference becomes a link
   * to the plan detail page labelled with its "P123" org_ref; an unresolved
   * reference stays plain text. Symmetric with resolveTask. */
  resolvePlan?: (ref: string) => ResolvedPlanRef | null;
  /** optional issue reference resolver. Accepts EITHER an `issue-<id>` or an
   * `I<number>` org_ref; a resolvable reference becomes a link to the issue
   * detail page labelled with its "I123" org_ref; an unresolved reference stays
   * plain text. Symmetric with resolveTask / resolvePlan. */
  resolveIssue?: (ref: string) => ResolvedIssueRef | null;
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
  resolvePlan,
  resolveIssue,
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
    // A plan reference in either form: the bare `plan-<id>` (group 4) or the
    // `P<number>` org_ref (group 5). Both resolve through resolvePlan (T99).
    const planRef = match[4] ?? match[5];
    // An issue reference in either form: the bare `issue-<id>` (group 6) or the
    // `I<number>` org_ref (group 7). Both resolve through resolveIssue.
    const issueRef = match[6] ?? match[7];
    let node: React.ReactNode = null;
    if (handle !== undefined && handle.toLowerCase() === 'all') {
      // @all broadcast (per @oopslink): a non-clickable but visually distinct
      // mention token. It addresses everyone in the conversation and is effective
      // only when a human sends it (the backend gates @all on a human sender).
      node = (
        <span
          key={key++}
          data-testid="mention-all-token"
          className={`rounded font-medium ${linkClass}`}
          title="Broadcast to everyone in this conversation (humans only)"
        >
          @{handle}
        </span>
      );
    } else if (handle !== undefined) {
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
    } else if (planRef !== undefined && resolvePlan) {
      const pl = resolvePlan(planRef);
      if (pl) {
        node = (
          <a
            key={key++}
            href={pl.href}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => e.stopPropagation()}
            data-testid="plan-ref-token"
            data-plan-id={planRef}
            title={`Open ${pl.label} in a new tab`}
            className={`rounded font-medium ${linkClass} hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`}
          >
            {pl.label}
          </a>
        );
      }
    } else if (issueRef !== undefined && resolveIssue) {
      const is = resolveIssue(issueRef);
      if (is) {
        node = (
          <a
            key={key++}
            href={is.href}
            target="_blank"
            rel="noopener noreferrer"
            onClick={(e) => e.stopPropagation()}
            data-testid="issue-ref-token"
            data-issue-id={issueRef}
            title={`Open ${is.label} in a new tab`}
            className={`rounded font-medium ${linkClass} hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`}
          >
            {is.label}
          </a>
        );
      }
    }
    // Unresolved (or task/plan/issue linkify disabled) → leave the token as plain text: skip
    // without advancing `last`, so the matched chars stay in the next text slice.
    if (node === null) continue;
    if (match.index > last) parts.push(<Fragment key={key++}>{text.slice(last, match.index)}</Fragment>);
    parts.push(node);
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push(<Fragment key={key++}>{text.slice(last)}</Fragment>);
  return <>{parts}</>;
}
