import type React from 'react';

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
//   blocked               → red #dc2626 via custom blockedred token
//   resolved/completed    → green (done)
//   verified              → teal (done + checked, distinct hue from green)
//   closed (Issue)        → slate (terminal)
//   discarded (both)      → zinc (terminal, replaces canceled/withdrawn)
//   reopened              → amber (back in play)
// blocked uses the custom `blockedred` token so the a11y guardrail's raw
// bg-red-/text-red- ban stays green.
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
  blocked: 'bg-blockedred',
  resolved: 'bg-status-green-solid',
  completed: 'bg-status-green-solid',
  verified: 'bg-status-teal-solid',
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
  const cls = statusSolidClass(status);
  return (
    <span
      className={`rounded px-1.5 py-0.5 text-[0.6875rem] uppercase tracking-wide ${cls}`}
      data-testid="status-chip"
      data-status={status}
    >
      {status.replace(/_/g, ' ')}
    </span>
  );
}

// idHandle — short, distinguishable handle for an entity id used as id-as-content
// (#126/#192): the ULID TAIL (the head is a shared-window timestamp). Full id
// belongs on hover (`title`).
export function idHandle(id: string): string {
  return id.slice(-6);
}

// shortDate — today → time, yesterday → "Yesterday", else locale date.
export function shortDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return d.toLocaleDateString();
}
