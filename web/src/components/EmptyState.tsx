import type React from 'react';

// EmptyState — unified empty-state surface used by list pages.
// Replaces the prior ad-hoc "No X yet." dashed-border + link snippets.
//
// Design per docs/design/web-console-design-system.md § 6 ("Forms &
// Feedback / empty-states"): helpful message + recovery path. We
// surface (1) a small monochrome SVG glyph so the state isn't all
// text, (2) a one-line headline, (3) an optional body sentence
// explaining WHAT this surface is for, (4) an optional action — either
// a primary button (onClick) or a navigation link (to).
//
// Why no decorative illustration: skill rule `style-match` for
// Minimalism & Swiss prefers structural typography over imagery.
export interface EmptyStateProps {
  title: string;
  body?: string;
  /** Optional inline SVG icon (16/20px). Defaults to a generic dotted-square. */
  icon?: React.ReactElement;
  /** Primary CTA — choose one of action / to. */
  action?: { label: string; onClick: () => void };
  /** Navigation CTA — choose one of action / to. */
  to?: { label: string; href: string };
  testId?: string;
}

export function EmptyState({
  title,
  body,
  icon,
  action,
  to,
  testId,
}: EmptyStateProps): React.ReactElement {
  return (
    <div
      className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-border-strong bg-bg-elevated px-6 py-10 text-center"
      data-testid={testId ?? 'empty-state'}
      role="status"
      aria-live="polite"
    >
      <span className="text-text-muted">{icon ?? <DefaultIcon />}</span>
      <h3 className="text-sm font-semibold text-text-primary">{title}</h3>
      {body && <p className="max-w-md text-xs text-text-secondary">{body}</p>}
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          className="mt-2 rounded-md bg-brand px-3 py-1.5 text-xs font-medium text-white motion-safe:transition-colors hover:bg-brand-hover"
        >
          {action.label}
        </button>
      )}
      {to && (
        <a
          href={to.href}
          className="mt-2 text-xs font-medium text-accent hover:underline"
        >
          {to.label} →
        </a>
      )}
    </div>
  );
}

function DefaultIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className="h-6 w-6 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="4" y="4" width="16" height="16" rx="2" strokeDasharray="2 3" />
      <path d="M9 12h6" strokeLinecap="round" />
    </svg>
  );
}
