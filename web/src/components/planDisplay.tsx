import type React from 'react';
import type { PlanStatus } from '@/api/plans';
import { idHandle } from '@/components/workItemDisplay';

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
//   draft    → slate-100 / slate-800  (13.35)  — not started, neutral
//   running  → blue-100  / blue-800   (7.15)   — in flight
//   done     → emerald-100/emerald-800 (6.78)  — complete
//   archived → stone-100 / stone-800  (13.90)  — terminal / shelved (v2.9 Stage B)
// ALL ≥ 4.5 → AA in BOTH modes. archived uses a WARM neutral (stone) distinct
// from draft's COOL neutral (slate) — terminal-neutral, not a live hue; the
// uppercase "archived" label is the primary distinguisher (never color alone).

const PLAN_STATUS_CLS: Record<PlanStatus, string> = {
  draft: 'bg-status-slate-bg text-status-slate-fg',
  running: 'bg-status-blue-bg text-status-blue-fg',
  done: 'bg-status-emerald-bg text-status-emerald-fg',
  archived: 'bg-status-stone-bg text-status-stone-fg',
};

export function planStatusClass(status: PlanStatus): string {
  return PLAN_STATUS_CLS[status] ?? 'bg-status-slate-bg text-status-slate-fg';
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

// TaskArchivedBadge — v2.9 Stage B (#283). A plan task carries an `archived`
// flag once its plan is archived (cascade), ORTHOGONAL to task_status /
// node_status (both the status chip AND this badge can show on the same row).
// Renders nothing when the task is not archived. Both-mode AA: a CURATED SOLID
// amber-100 / amber-800 pair (theme-independent literal Tailwind colors → the
// same light-block-dark-text in BOTH modes, contrast 6.37 — AA). Distinct from
// the neutral archived PLAN chip (amber = "shelved item" tone) and not red (the
// raw-red guardrail). Text label "Archived" + tiny inline SVG (NOT emoji).
export function TaskArchivedBadge({
  archived,
  taskId,
}: {
  archived: boolean | undefined;
  taskId: string;
}): React.ReactElement | null {
  if (!archived) return null;
  return (
    <span
      className="inline-flex items-center gap-1 rounded bg-status-amber-bg px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-status-amber-fg"
      data-testid={`task-archived-badge-${taskId}`}
      title="This task was archived with its plan (read-only)."
    >
      {/* archive box glyph */}
      <svg viewBox="0 0 24 24" className="h-2.5 w-2.5" fill="none" stroke="currentColor" strokeWidth="2.4" aria-hidden="true">
        <rect x="3" y="4" width="18" height="4" rx="1" />
        <path d="M5 8v11a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8" />
        <path d="M10 12h4" />
      </svg>
      Archived
    </span>
  );
}

// planProgressLabel — "done/total" string for the progress column.
// PlanRefTag (v2.10.1 [T99]) — a small monospace pill showing the human Plan id
// (org_ref "P123"), falling back to "#"+id-tail when there's no org_ref (the
// builtin pool / pre-allocator rows). Mirrors the task TaskIdTag pattern. Solid
// theme tokens (both-mode AA). Full plan id on hover.
export function PlanRefTag({
  planId,
  orgRef,
  testId = 'plan-ref-tag',
}: {
  planId: string;
  orgRef?: string;
  testId?: string;
}): React.ReactElement {
  const label = orgRef || `#${idHandle(planId)}`;
  return (
    <span
      className="inline-flex shrink-0 items-center rounded bg-bg-subtle px-1 py-0.5 font-mono text-[0.625rem] font-semibold text-text-secondary"
      data-testid={testId}
      title={planId}
    >
      {label}
    </span>
  );
}

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
