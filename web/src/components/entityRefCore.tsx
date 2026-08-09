import type React from 'react';
import { Fragment } from 'react';

export type EntityRefKind = 'task' | 'plan' | 'issue' | 'agent' | 'executor';

export interface EntityRefToken {
  kind: EntityRefKind;
  token: string;
  index: number;
  length: number;
}

interface SpanRange {
  start: number;
  end: number;
}

// Stable IDs and org refs share one tokenizer. It deliberately skips URLs and
// markdown code spans/blocks; markdown callers already avoid code nodes, but
// plain-text surfaces such as Activity / executor status need the same guard.
const TOKEN_RE =
  /(?<![A-Za-z0-9_-])(?:(task-[A-Za-z0-9][A-Za-z0-9-]*)|(plan-[A-Za-z0-9][A-Za-z0-9-]*)|(issue-[A-Za-z0-9][A-Za-z0-9-]*)|(agent-[A-Za-z0-9][A-Za-z0-9-]*)|(exec-[A-Za-z0-9][A-Za-z0-9-]*)|(T\d+)|(P\d+)|(I\d+))(?![A-Za-z0-9_-])/g;
const URL_RE = /\b(?:https?:\/\/|mailto:|ftp:\/\/|www\.)[^\s<>()]+/gi;
const FENCED_CODE_RE = /```[\s\S]*?```/g;
const INLINE_CODE_RE = /`[^`\n]+`/g;

function rangesFor(re: RegExp, text: string): SpanRange[] {
  const out: SpanRange[] = [];
  re.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    out.push({ start: m.index, end: m.index + m[0].length });
  }
  return out;
}

function inAnyRange(index: number, ranges: SpanRange[]): boolean {
  return ranges.some((r) => index >= r.start && index < r.end);
}

export function tokenizeEntityRefs(text: string): EntityRefToken[] {
  const skipped = [
    ...rangesFor(URL_RE, text),
    ...rangesFor(FENCED_CODE_RE, text),
    ...rangesFor(INLINE_CODE_RE, text),
  ];
  const tokens: EntityRefToken[] = [];
  TOKEN_RE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = TOKEN_RE.exec(text)) !== null) {
    if (inAnyRange(m.index, skipped)) continue;
    const kind: EntityRefKind =
      m[1] || m[6]
        ? 'task'
        : m[2] || m[7]
          ? 'plan'
          : m[3] || m[8]
            ? 'issue'
            : m[4]
              ? 'agent'
              : 'executor';
    tokens.push({ kind, token: m[0], index: m.index, length: m[0].length });
  }
  return tokens;
}

export interface ResolvedEntityRef {
  kind: EntityRefKind;
  token: string;
  label: string;
  href?: string;
  onClick?: (event: React.MouseEvent<HTMLElement>) => void;
  title?: string;
  dataAttrs?: Record<string, string>;
}

export type EntityRefSurface = 'message' | 'activity' | 'executor' | 'generic';
export type EntityRefVariant = 'label' | 'id';

function testIdFor(surface: EntityRefSurface, kind: EntityRefKind): string {
  if (surface === 'activity') return `activity-${kind}-ref-link`;
  if (surface === 'executor') return `executor-${kind}-ref-token`;
  if (surface === 'message') return `${kind}-ref-token`;
  return `entity-${kind}-ref-token`;
}

export function EntityRefInline({
  refInfo,
  variant,
  surface,
  linkClass,
}: {
  refInfo: ResolvedEntityRef;
  variant: EntityRefVariant;
  surface: EntityRefSurface;
  linkClass: string;
}): React.ReactElement {
  const text = variant === 'id' ? refInfo.token : refInfo.label;
  const title = refInfo.title ?? (text !== refInfo.token ? refInfo.token : undefined);
  const common = {
    'data-testid': testIdFor(surface, refInfo.kind),
    'data-entity-kind': refInfo.kind,
    'data-entity-token': refInfo.token,
    title,
    className: `rounded font-medium ${linkClass} hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`,
    ...(refInfo.dataAttrs ?? {}),
  };

  if (refInfo.href) {
    return (
      <a
        href={refInfo.href}
        target="_blank"
        rel="noopener noreferrer"
        onClick={(e) => e.stopPropagation()}
        {...common}
      >
        {text}
      </a>
    );
  }

  if (refInfo.onClick) {
    return (
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          refInfo.onClick?.(e);
        }}
        {...common}
      >
        {text}
      </button>
    );
  }

  return <span {...common}>{text}</span>;
}

export function renderEntityRefParts({
  text,
  tokens,
  resolve,
  variant,
  surface,
  linkClass,
}: {
  text: string;
  tokens: EntityRefToken[];
  resolve: (token: EntityRefToken) => ResolvedEntityRef | null;
  variant: EntityRefVariant;
  surface: EntityRefSurface;
  linkClass: string;
}): React.ReactNode[] {
  const parts: React.ReactNode[] = [];
  let last = 0;
  let key = 0;
  for (const token of tokens) {
    const resolved = resolve(token);
    if (!resolved) continue;
    if (token.index > last) {
      parts.push(<Fragment key={key++}>{text.slice(last, token.index)}</Fragment>);
    }
    parts.push(
      <EntityRefInline
        key={key++}
        refInfo={resolved}
        variant={variant}
        surface={surface}
        linkClass={linkClass}
      />,
    );
    last = token.index + token.length;
  }
  if (last < text.length) parts.push(<Fragment key={key++}>{text.slice(last)}</Fragment>);
  return parts;
}
