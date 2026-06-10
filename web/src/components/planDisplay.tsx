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
