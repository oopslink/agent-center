import type React from 'react';
import { useTranslation } from 'react-i18next';
import i18n from '@/i18n';

// Shared display helpers for issue/task rows — used by the per-project tables
// (#242 ProjectDetail) and the org-scope aggregation pages (#258). Extracted so
// the status colours + id handle + date format stay identical across pages
// (v2.7.1 retro: single source, no per-page drift).

// StatusChip — colored pill covering the FULL issue + task status machines
// (v2.7.1 #258: zero fallback-gray, zero bare string). v2.8.1 #5th: UNIFIED to
// the SAME palette as StatusBlock (IssueTaskSidebar) — one source of truth.
// @oopslink REVISION 4 lock: white text on a saturated color background
// (bg-<color> text-white). Palette:
//   open                  → sky (not started)
//   in_progress/running   → blue (in flight)
//   resolved/completed    → green (done)
//   closed (Issue)        → slate (terminal)
//   discarded (both)      → zinc (terminal, replaces canceled/withdrawn)
//   reopened              → amber (back in play)
// ADR-0054: `blocked` is a task status again (a real, non-terminal PARK).
// `verified` remains deleted. The
// blocked_reason annotation still exists and still carries the reason text — TaskDetail
// renders it separately as a "Stuck" chip.
//
// SINGLE SOURCE of the REV4 status→color mapping. `STATUS_BG_CLS` is the bare
// background-color class for each status (literal strings so Tailwind's content
// scan keeps them). The solid StatusChip layers `text-white` on top via
// `statusSolidClass`; the FilterBar status chips reuse `STATUS_BG_CLS` for both
// the solid-selected fill AND the unselected color dot (●) — no hex duplication.
export const STATUS_BG_CLS: Record<string, string> = {
  open: 'bg-status-sky-solid',
  in_progress: 'bg-status-blue-solid',
  running: 'bg-status-blue-solid',
  resolved: 'bg-status-green-solid',
  completed: 'bg-status-green-solid',
  // ADR-0054 parked state. `blocked` takes orange (the alert end of the scale,
  // distinct from reopened's amber).
  blocked: 'bg-status-orange-solid',
  closed: 'bg-status-slate-solid',
  discarded: 'bg-status-zinc-solid',
  reopened: 'bg-status-amber-solid',
};

// statusSolidClass — the saturated REV4 fill + white text for a status (the
// StatusChip / selected-FilterBar-chip look). Unknown → neutral subtle.
export function statusSolidClass(status: string): string {
  const bg = STATUS_BG_CLS[status];
  return bg ? `${bg} text-white` : 'bg-bg-subtle text-text-muted';
}

// statusDotClass — the bare background-color for a status used as a small color
// dot (●) on an unselected/light chip. Unknown → neutral muted.
export function statusDotClass(status: string): string {
  return STATUS_BG_CLS[status] ?? 'bg-text-muted';
}

export function StatusChip({ status }: { status: string }): React.ReactElement {
  const { t } = useTranslation('work');
  const cls = statusSolidClass(status);
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide ${cls}`}
      data-testid="status-chip"
      data-status={status}
    >
      {t(`status.${status}`, { defaultValue: status.replace(/_/g, ' ') })}
    </span>
  );
}

// refLabel — the human identity label for a work item / plan node / plan: its
// org_ref ("T123" / "I7" / "P12") when present, else the FULL entity id shown
// verbatim as id-as-content (the full id also belongs on hover, `title`).
//
// T126: the retired short-hash id-tail encoding is NEVER produced — a missing
// org_ref degrades to the FULL id, not a 6-char hash (e.g. 4e2e71). This is the
// single fallback every "id-as-content" surface uses, replacing the old
// org_ref-or-hash pattern. A grep guard (scripts/lint/no-idtail-hash.sh)
// backstops it so the id-tail-hash form cannot return.
export function refLabel(orgRef: string | undefined | null, id: string): string {
  const ref = (orgRef ?? '').trim();
  return ref !== '' ? ref : id;
}

// IssueRefTag (T574 sidebar polish) — a small monospace pill showing the human
// Issue id (org_ref "I123"), falling back to the full id when absent. Mirrors
// PlanRefTag (planDisplay) so the related-Plan (P123) and related-Issue (I123)
// ids render as identical tags in the Task detail sidebar. Full id on hover.
export function IssueRefTag({
  issueId,
  orgRef,
  testId = 'issue-ref-tag',
}: {
  issueId: string;
  orgRef?: string;
  testId?: string;
}): React.ReactElement {
  return (
    <span
      className="inline-flex shrink-0 items-center rounded bg-bg-subtle px-1 py-0.5 font-mono text-[0.625rem] font-semibold text-text-secondary"
      data-testid={testId}
      title={issueId}
    >
      {refLabel(orgRef, issueId)}
    </span>
  );
}

// shortDate — today → time, yesterday → "Yesterday", else locale date.
export function shortDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return i18n.t('work:shared.yesterday');
  return d.toLocaleDateString();
}

// fullDateTime — the FULL local date-time INCLUDING the timezone (owner ask:
// the Tasks list Created/Updated columns must show an unambiguous absolute
// instant, not the relative "Yesterday"/"14:09" of shortDate). Renders e.g.
// "Jun 1, 2026, 2:00:00 AM GMT+8". Falls back to the raw ISO on an unparseable
// value (mirrors shortDate). Callers keep the raw ISO on the `title` attr.
export function fullDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  // NB: explicit component options — `dateStyle`/`timeStyle` CANNOT be combined
  // with `timeZoneName` (spec throws "Invalid option"); we need the zone, so we
  // list the components. Yields e.g. "Jun 1, 2026, 2:00:00 AM GMT+8".
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
    timeZoneName: 'short',
  });
}
