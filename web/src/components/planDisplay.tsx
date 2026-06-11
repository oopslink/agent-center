import type React from 'react';
import type { PlanStatus } from '@/api/plans';

// Shared display helpers for v2.9 Plan orchestration (#286 list + #287 DAG).
// Extracted so the Plan status palette + has_failed indicator stay identical
// across the Plan list and the (future) Plan detail / DAG view — single source.
//
// 命门 (both-mode AA): the Plan status chips use the SAME discipline as
// tagColors.ts — a CURATED {bg,text} pair of a SOLID light background (X-100) +
// dark text (X-800). Both are theme-INDEPENDENT literal Tailwind colors (no
// theme token, no `dark:`), so the chip renders the SAME light-block-with-dark-
// text in BOTH light and dark mode → identical WCAG-AA contrast in both modes.
// This sidesteps the both-mode-aa-not-light-only trap (mid-tone text on an
// alpha-tint goes dark-on-dark in dark mode). NO alpha-tint (`bg-{token}/{n}`).
//
// Computed contrast (Tailwind v3 default hex, white-vs-black AA formula):
//   slate-100/slate-800 = 13.35 · sky-100/sky-800 ... we reuse the proven pairs:
//   draft   → slate-100 / slate-800  (13.35)  — not started, neutral
//   running → blue-100  / blue-800   (7.15)   — in flight
//   done    → emerald-100/emerald-800 (6.78)  — complete
// ALL ≥ 4.5 → AA in BOTH modes. Distinct hues (slate/blue/emerald), distinguished
// by FILL + TEXT, never color alone.

const PLAN_STATUS_CLS: Record<PlanStatus, string> = {
  draft: 'bg-slate-100 text-slate-800',
  running: 'bg-blue-100 text-blue-800',
  done: 'bg-emerald-100 text-emerald-800',
};

export function planStatusClass(status: PlanStatus): string {
  return PLAN_STATUS_CLS[status] ?? 'bg-slate-100 text-slate-800';
}

// PlanStatusChip — the draft/running/done pill. Same shape/size idiom as the
// work-items StatusChip (rounded uppercase mini-pill) for visual consistency.
export function PlanStatusChip({ status }: { status: PlanStatus }): React.ReactElement {
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide ${planStatusClass(status)}`}
      data-testid="plan-status-chip"
      data-status={status}
    >
      {status}
    </span>
  );
}

// PlanFailedIndicator — the derived has_failed flag (§9.1). Uses the custom
// `blockedred` token (#dc2626, white text → AA in both modes; same token the
// work-items `blocked` status uses) so the a11y guardrail's raw bg-red-/text-red-
// ban stays green. Text label ("FAILED NODE"), NOT color/emoji alone. Renders
// nothing when the Plan has no failed node.
export function PlanFailedIndicator({ hasFailed }: { hasFailed: boolean }): React.ReactElement | null {
  if (!hasFailed) return null;
  return (
    <span
      className="rounded bg-blockedred px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide text-white"
      data-testid="plan-failed-indicator"
      title="A node in this plan has a failed task (§9.1)"
    >
      Failed node
    </span>
  );
}

// planProgressLabel — "done/total" string for the progress column.
export function planProgressLabel(progress: { done: number; total: number }): string {
  return `${progress.done}/${progress.total}`;
}

// AutoAdvancingIcon — a small inline SVG "cycle / refresh" glyph (NOT emoji),
// communicating the plan self-progresses. Inherits currentColor so it tracks
// whatever text token the caller uses → AA in both modes via the text color.
export function AutoAdvancingIcon({ className = 'h-3 w-3' }: { className?: string }): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2.2" aria-hidden="true">
      <path d="M3 12a9 9 0 0 1 15-6.7L21 8" />
      <path d="M21 3v5h-5" />
      <path d="M21 12a9 9 0 0 1-15 6.7L3 16" />
      <path d="M3 21v-5h5" />
    </svg>
  );
}

// AutoAdvancingIndicator — the v2.9 P2-4 signal that a RUNNING plan auto-advances
// its DAG (the orchestrator dispatches newly-ready nodes automatically as
// upstream tasks complete; manual Advance is kept only as an override). This is
// purely informational/subtle — NOT an alert. Both-mode AA: it uses the
// `text-text-secondary` theme token (NOT text-text-muted, which fails readable-
// AA) and has NO background fill (no alpha-tint). Icon = inline SVG, no emoji.
export function AutoAdvancingIndicator({
  variant = 'detail',
}: {
  variant?: 'detail' | 'column';
}): React.ReactElement {
  const hint = 'The system dispatches ready nodes automatically as upstream tasks complete.';
  if (variant === 'column') {
    // Compact suffix for the ~236px board column header.
    return (
      <span
        className="inline-flex items-center gap-0.5 text-[0.6875rem] text-text-secondary"
        data-testid="plan-col-auto-advancing"
        title={hint}
      >
        <AutoAdvancingIcon className="h-2.5 w-2.5" />
        auto-advancing
      </span>
    );
  }
  return (
    <span
      className="inline-flex items-center gap-1 rounded border border-border-base px-1.5 py-0.5 text-[0.6875rem] font-medium text-text-secondary"
      data-testid="plan-auto-advancing"
      title={hint}
      aria-label={`Auto-advancing — ${hint}`}
    >
      <AutoAdvancingIcon className="h-3 w-3" />
      Auto-advancing
    </span>
  );
}
