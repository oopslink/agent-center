import type React from 'react';

// Shared display helpers for issue/task rows — used by the per-project tables
// (#242 ProjectDetail) and the org-scope aggregation pages (#258). Extracted so
// the status colours + id handle + date format stay identical across pages
// (v2.7.1 retro: single source, no per-page drift).

// StatusChip — colored pill covering the FULL issue + task status machines
// (v2.7.1 #258: zero fallback-gray, zero bare string). v2.8.1 #5th: UNIFIED to
// the SAME palette as StatusBlock (IssueTaskSidebar) — one source of truth.
// @oopslink FINAL lock: white text on a saturated color background
// (bg-<color> text-white). Palette:
//   open                  → slate (not started)
//   in_progress/running   → blue (in flight)
//   blocked               → orange (attention)
//   resolved/completed    → green (done)
//   verified              → purple (done + checked, distinct hue from green)
//   closed (Issue)        → cyan (terminal, distinct from open's slate)
//   discarded (both)      → deep-rust (terminal, replaces canceled/withdrawn)
//   reopened              → pink (back in play)
// @oopslink has explicitly accepted that some pairs (orange-500, slate-500,
// blue-500, pink-600 vs white) fall below WCAG-AA 4.5:1 — intentional.
const STATUS_CLS: Record<string, string> = {
  open: 'bg-slate-500 text-white',
  in_progress: 'bg-blue-500 text-white',
  running: 'bg-blue-500 text-white',
  blocked: 'bg-orange-500 text-white',
  resolved: 'bg-green-600 text-white',
  completed: 'bg-green-600 text-white',
  verified: 'bg-purple-600 text-white',
  closed: 'bg-cyan-600 text-white',
  discarded: 'bg-rust-700 text-white',
  reopened: 'bg-pink-600 text-white',
};

export function StatusChip({ status }: { status: string }): React.ReactElement {
  const cls = STATUS_CLS[status] ?? 'bg-bg-subtle text-text-muted';
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
