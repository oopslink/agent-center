import type React from 'react';

// Shared display helpers for issue/task rows — used by the per-project tables
// (#242 ProjectDetail) and the org-scope aggregation pages (#258). Extracted so
// the status colours + id handle + date format stay identical across pages
// (v2.7.1 retro: single source, no per-page drift).

// StatusChip — colored pill covering the FULL issue + task status machines
// (v2.7.1 #258: zero fallback-gray, zero bare string). Palette per PD ruling:
//   open                         → neutral
//   in_progress/assigned/running → blue (in flight)
//   blocked                      → orange (attention)
//   resolved/completed           → green (done)
//   verified                     → deep green (done + checked)
//   closed/canceled/withdrawn    → muted (terminal, not a win)
//   reopened                     → purple (back in play)
const STATUS_CLS: Record<string, string> = {
  // in flight
  in_progress: 'bg-brand/10 text-brand',
  assigned: 'bg-brand/10 text-brand',
  running: 'bg-brand/10 text-brand',
  // attention
  blocked: 'bg-orange-500/10 text-orange-600',
  // done
  resolved: 'bg-success/10 text-success',
  completed: 'bg-success/10 text-success',
  verified: 'bg-success/20 text-success',
  // terminal, not a win
  closed: 'bg-bg-subtle text-text-secondary',
  canceled: 'bg-bg-subtle text-text-secondary',
  withdrawn: 'bg-bg-subtle text-text-secondary',
  // back in play
  reopened: 'bg-purple-500/10 text-purple-600',
  // new / not started
  open: 'bg-bg-subtle text-text-muted',
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
