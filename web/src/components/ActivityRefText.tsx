import type React from 'react';
import { Fragment } from 'react';
import { useOptionalOrgContext, orgPath } from '@/OrgContext';
import {
  useTaskRefResolver,
  usePlanRefResolver,
  useIssueRefResolver,
  useAgentRefResolver,
} from './MentionText';

// ActivityRefText (oopslink DM 2026-07-04): the agent-activity timeline surfaces
// raw entity ids in its expanded detail — the structured `task` ref field and the
// #192-EXEMPT payload JSON (e.g. `"task_ref": "task-5779df52"`, `"executor_id":
// "exec-…"`). They were plain, unclickable text. This linkifies the STANDALONE
// entity ids (task- / plan- / issue- / agent-<id>) into ref links, reusing the
// SAME resolvers as MentionText (single source) so a click routes to the right
// detail page — while keeping the LITERAL id as the link text.
//
// Why literal id (not the human label like MentionText's "T123")? These are
// operator/debug surfaces: the payload JSON must stay a faithful copy of the raw
// event (a label would corrupt it), and the `task` field already shows the raw id
// in font-mono — so keeping the id and merely making it clickable is zero visual
// regression. That is the one intentional divergence from MentionText, which
// renders resolved labels for chat prose.
//
// Divergences from MentionText's TOKEN_RE, by design:
//   - only the BARE `<kind>-<id>` forms (no @handle, no T/P/I org_ref) — activity
//     payloads carry entity ids, not human org_refs.
//   - `exec-` / executor ids are intentionally NOT matched: an executor has no
//     detail page, so linking would dangle (verify-not-trust → stays plain text).
//   - an `agent-<id>` links to the org-scoped agent detail page (/agents/:id),
//     NOT a sidebar callback (this surface has no onMention plumbing).
// The left-boundary negative lookbehind mirrors MentionText exactly, so
// "subtask-1" / "reissue-x" / "myagent-1" do NOT match (only a standalone id).
const REF_RE =
  /(?<![A-Za-z0-9])(task-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(plan-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(issue-[A-Za-z0-9]+)|(?<![A-Za-z0-9])(agent-[A-Za-z0-9][A-Za-z0-9-]*)/g;

const LINK_CLASS =
  'rounded font-medium text-accent hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent';

interface ActivityRefTextProps {
  /** The plain text (a ref field value or pretty-printed payload JSON) to linkify. */
  text: string;
  className?: string;
}

// ActivityRefText tokenizes one plain string, turning each STANDALONE, RESOLVABLE
// entity id into an anchor to its detail page (new tab, opener-guarded, click
// stopPropagation — mirroring MentionText's task/plan/issue anchors). An
// unknown / out-of-org / non-linkable id (incl. every exec-<id>) stays plain
// text, so no link ever dangles.
export function ActivityRefText({ text, className }: ActivityRefTextProps): React.ReactElement {
  const ctx = useOptionalOrgContext();
  const slug = ctx?.slug;
  const resolveTask = useTaskRefResolver();
  const resolvePlan = usePlanRefResolver();
  const resolveIssue = useIssueRefResolver();
  const resolveAgent = useAgentRefResolver();

  const parts: React.ReactNode[] = [];
  let last = 0;
  let key = 0;
  let match: RegExpExecArray | null;
  REF_RE.lastIndex = 0;
  while ((match = REF_RE.exec(text)) !== null) {
    const taskRef = match[1];
    const planRef = match[2];
    const issueRef = match[3];
    const agentRef = match[4];
    const token = match[0];

    let href: string | null = null;
    let testId = '';
    // The kind-specific data-* attribute (mirrors MentionText's data-task-id etc.)
    // so tests / tooling can anchor on the exact linked id.
    let dataAttrs: Record<string, string> = {};

    if (taskRef !== undefined) {
      const r = resolveTask(taskRef);
      if (r) {
        href = r.href;
        testId = 'activity-task-ref-link';
        dataAttrs = { 'data-task-id': taskRef };
      }
    } else if (planRef !== undefined) {
      const r = resolvePlan(planRef);
      if (r) {
        href = r.href;
        testId = 'activity-plan-ref-link';
        dataAttrs = { 'data-plan-id': planRef };
      }
    } else if (issueRef !== undefined) {
      const r = resolveIssue(issueRef);
      if (r) {
        href = r.href;
        testId = 'activity-issue-ref-link';
        dataAttrs = { 'data-issue-id': issueRef };
      }
    } else if (agentRef !== undefined) {
      // Only a KNOWN agent linkifies (verify-not-trust); the bare token IS the
      // agent's member id, so the detail route is /agents/<token> (org-scoped).
      const a = resolveAgent(agentRef);
      if (a && a.ref.startsWith('agent:')) {
        href = orgPath(`/agents/${encodeURIComponent(agentRef)}`, slug);
        testId = 'activity-agent-ref-link';
        dataAttrs = { 'data-agent-ref': a.ref };
      }
    }

    // Unresolved / non-linkable → leave the token in the plain-text slice.
    if (href === null) continue;
    if (match.index > last) {
      parts.push(<Fragment key={key++}>{text.slice(last, match.index)}</Fragment>);
    }
    parts.push(
      <a
        key={key++}
        href={href}
        // New tab + opener/referrer guards; stopPropagation so a ref click never
        // bubbles to the activity-row toggle — mirrors MentionText's ref anchors.
        target="_blank"
        rel="noopener noreferrer"
        onClick={(e) => e.stopPropagation()}
        data-testid={testId}
        {...dataAttrs}
        className={LINK_CLASS}
      >
        {token}
      </a>,
    );
    last = match.index + token.length;
  }
  if (last < text.length) parts.push(<Fragment key={key++}>{text.slice(last)}</Fragment>);
  return <span className={className}>{parts}</span>;
}
