import type React from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { CollapsibleCodeBlock } from './CollapsibleCodeBlock';

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

// `textClass` overrides the body text color. Default `text-text-primary` (theme
// token, adapts both modes) is correct on theme-adaptive backgrounds. On a FIXED
// light surface (e.g. the own chat bubble's #D1E3FF, which does NOT flip per theme),
// pass a FIXED dark class (text-slate-900) so the body stays dark in BOTH modes —
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
  return (
    <div className={`markdown-body space-y-2 leading-relaxed ${textClass}`} data-testid="markdown-message">
      <Markdown
        remarkPlugins={[remarkGfm]}
        components={{
          // fenced code → shared collapsible block (handles with/without language).
          pre: PreBlock,
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
