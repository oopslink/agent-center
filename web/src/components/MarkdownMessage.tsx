import type React from 'react';
import { Children } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { CollapsibleCodeBlock } from './CollapsibleCodeBlock';
import {
  MentionText,
  useMentionResolver,
  useAgentRefResolver,
  useTaskRefResolver,
  usePlanRefResolver,
  useIssueRefResolver,
  type ResolvedTaskRef,
  type ResolvedPlanRef,
  type ResolvedIssueRef,
} from './MentionText';
import { useSenderSidebar } from './SenderSidebarContext';

// v2.8 #276 — render message content as markdown (@oopslink chose A = full
// react-markdown). Security (Q-A1 = strict escape): NO `rehype-raw`, so raw HTML
// in agent/user content is ESCAPED, never rendered as elements — `<script>` can
// never execute. react-markdown's default urlTransform also strips dangerous
// link/image schemes (javascript:, vbscript:, data: …). GFM (Q-A2) via remark-gfm
// adds tables / task lists / strikethrough / autolinks. Fenced code blocks render
// through the shared <CollapsibleCodeBlock>; syntax highlighting is v2.9.
function flattenText(node: React.ReactNode): string {
  if (typeof node === 'string') return node;
  if (Array.isArray(node)) return node.map(flattenText).join('');
  return '';
}

// A fenced code block arrives here as <pre>'s child <code class="language-x">…</code>.
function PreBlock({ children }: { children?: React.ReactNode }): React.ReactElement {
  const codeEl = (Array.isArray(children) ? children[0] : children) as
    | React.ReactElement<{ className?: string; children?: React.ReactNode }>
    | undefined;
  const className = codeEl?.props?.className ?? '';
  const match = /language-(\w+)/.exec(className);
  const code = flattenText(codeEl?.props?.children).replace(/\n$/, '');
  return <CollapsibleCodeBlock code={code} language={match?.[1]} contextLabel="code" />;
}

// linkifyMentions walks a rendered node's children and replaces @mention tokens
// found in raw string children with clickable MentionText buttons (#281 entry ②).
// Only bare string children are tokenized — nested elements (already-rendered
// links / inline code / emphasis) are passed through untouched, so we never
// linkify inside a code span or an existing link. When there's no sidebar
// provider OR no resolver match, the text renders unchanged.
function linkifyMentions(
  children: React.ReactNode,
  onMention: ((ref: string) => void) | null,
  resolve: (handle: string) => string | null,
  linkClass: string,
  resolveTask: (taskId: string) => ResolvedTaskRef | null,
  resolvePlan: (planRef: string) => ResolvedPlanRef | null,
  resolveIssue: (issueRef: string) => ResolvedIssueRef | null,
  resolveAgent: (id: string) => string | null,
): React.ReactNode {
  if (!onMention) return children;
  return Children.map(children, (child) => {
    if (typeof child === 'string') {
      // Tokenize only strings that could carry a token (@mention, task-<id>,
      // T<number>, plan-<id>, P<number>, issue-<id>, I<number>, or agent-<id>) —
      // a plain prose run with none is passed through untouched. The cheap
      // `T\d`/`P\d` tests are pre-filters only; MentionText's boundary-guarded
      // TOKEN_RE + resolver gating decide actual linkification. (T317: the
      // `agent-` pre-filter must be here or a bare agent-<id> never reaches
      // MentionText.)
      if (
        !child.includes('@') &&
        !child.includes('task-') &&
        !child.includes('plan-') &&
        !child.includes('issue-') &&
        !child.includes('agent-') &&
        !/T\d/.test(child) &&
        !/P\d/.test(child) &&
        !/I\d/.test(child)
      ) {
        return child;
      }
      return (
        <MentionText
          text={child}
          onMention={onMention}
          resolve={resolve}
          linkClass={linkClass}
          resolveTask={resolveTask}
          resolvePlan={resolvePlan}
          resolveIssue={resolveIssue}
          resolveAgent={resolveAgent}
        />
      );
    }
    return child;
  });
}

// `textClass` overrides the body text color. Default `text-text-primary` (theme
// token, adapts both modes) is correct on theme-adaptive backgrounds. On a FIXED
// light surface (e.g. the own chat bubble's #D1E3FF, which does NOT flip per theme),
// pass a FIXED dark class (text-chatbubble-fg) so the body stays dark in BOTH modes —
// a theme token would flip light in dark mode = light-on-light-blue FAIL.
export function MarkdownMessage({
  content,
  textClass = 'text-text-primary',
  linkClass = 'text-accent',
}: {
  content: string;
  textClass?: string;
  linkClass?: string;
}): React.ReactElement {
  // #281 entry ②: under a SenderSidebarProvider the @mention tokens become
  // clickable (resolve handle → identity ref → open the kind-routed sidebar).
  // With NO provider (e.g. markdown rendered outside a conversation surface, or a
  // unit test without a QueryClient) we render plain markdown — crucially we do
  // NOT call the members query at all, so MarkdownMessage stays usable standalone.
  const onMention = useSenderSidebar();
  if (onMention) {
    return (
      <MentionAwareMarkdown
        content={content}
        textClass={textClass}
        linkClass={linkClass}
        onMention={onMention}
      />
    );
  }
  return <MarkdownBody content={content} textClass={textClass} linkClass={linkClass} />;
}

// MentionAwareMarkdown is only mounted when a sidebar provider is present, so the
// members-query hook (useMentionResolver) is gated behind that — standalone /
// no-QueryClient renders go through MarkdownBody and never touch react-query.
function MentionAwareMarkdown({
  content,
  textClass,
  linkClass,
  onMention,
}: {
  content: string;
  textClass: string;
  linkClass: string;
  onMention: (ref: string) => void;
}): React.ReactElement {
  const resolve = useMentionResolver();
  // v2.9.2 (task-82915d7c): resolve `task-<id>` references → task-detail links.
  const resolveTask = useTaskRefResolver();
  // v2.10.1 [T99]: resolve `plan-<id>` / `P<number>` references → plan-detail links.
  const resolvePlan = usePlanRefResolver();
  // resolve `issue-<id>` / `I<number>` references → issue-detail links.
  const resolveIssue = useIssueRefResolver();
  // T335: resolve bare `agent-<id>` references (members + agents list) → sidebar.
  const resolveAgent = useAgentRefResolver();
  const linkify = (children: React.ReactNode) =>
    linkifyMentions(children, onMention, resolve, linkClass, resolveTask, resolvePlan, resolveIssue, resolveAgent);
  return <MarkdownBody content={content} textClass={textClass} linkClass={linkClass} linkify={linkify} />;
}

function MarkdownBody({
  content,
  textClass,
  linkClass,
  linkify = (children) => children,
}: {
  content: string;
  textClass: string;
  linkClass: string;
  linkify?: (children: React.ReactNode) => React.ReactNode;
}): React.ReactElement {
  return (
    <div className={`markdown-body space-y-2 leading-relaxed ${textClass}`} data-testid="markdown-message">
      <Markdown
        remarkPlugins={[remarkGfm]}
        components={{
          // fenced code → shared collapsible block (handles with/without language).
          pre: PreBlock,
          // Prose containers: linkify @mention tokens in their direct text
          // children (nested elements — links/inline code — pass through). When
          // there's no provider, `linkify` is identity so these render verbatim.
          p: ({ children }) => <p>{linkify(children)}</p>,
          li: ({ children }) => <li>{linkify(children)}</li>,
          em: ({ children }) => <em>{linkify(children)}</em>,
          strong: ({ children }) => <strong>{linkify(children)}</strong>,
          td: ({ children }) => <td>{linkify(children)}</td>,
          // external-safe links: rel guards window.opener + referrer leakage.
          a({ href, children }) {
            return (
              <a
                href={href}
                rel="noopener noreferrer"
                target="_blank"
                className={`${linkClass} underline`}
              >
                {children}
              </a>
            );
          },
          // images: always carry an alt (decorative '' when the author omitted one).
          img({ src, alt }) {
            return <img src={src} alt={alt ?? ''} loading="lazy" className="max-w-full rounded" />;
          },
        }}
      >
        {content}
      </Markdown>
    </div>
  );
}
