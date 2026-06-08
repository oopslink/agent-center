import type React from 'react';

// Shared display helpers for issue/task rows — used by the per-project tables
// (#242 ProjectDetail) and the org-scope aggregation pages (#258). Extracted so
// the status colours + id handle + date format stay identical across pages
// (v2.7.1 retro: single source, no per-page drift).

// StatusChip — colored pill covering the FULL issue + task status machines
// (v2.7.1 #258: zero fallback-gray, zero bare string). v2.8.1 #5th: UNIFIED to
// the SAME solid "深字浅底" X-100 bg / X-900 text palette as StatusBlock
// (IssueTaskSidebar) — one source of truth, theme-independent (the solid light
// block is AA in BOTH light and dark on any page bg). Palette:
//   open                  → slate (not started)
//   in_progress/running   → blue (in flight)
//   blocked               → orange (attention)
//   resolved/completed    → green (done)
//   verified              → teal (done + checked, distinct hue from green)
//   closed (Issue)        → stone (terminal, distinct from open's slate)
//   discarded (both)      → deep-rust (terminal, replaces canceled/withdrawn)
//   reopened              → purple (back in play)
const STATUS_CLS: Record<string, string> = {
  open: 'bg-slate-100 text-slate-700',
  in_progress: 'bg-blue-100 text-blue-900',
  running: 'bg-blue-100 text-blue-900',
  blocked: 'bg-orange-100 text-orange-900',
  resolved: 'bg-green-100 text-green-900',
  completed: 'bg-green-100 text-green-900',
  verified: 'bg-teal-100 text-teal-900',
  closed: 'bg-stone-100 text-stone-700',
  discarded: 'bg-rust-100 text-rust-900',
  reopened: 'bg-purple-100 text-purple-900',
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
