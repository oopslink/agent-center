import type React from 'react';
import { useState } from 'react';

// Shared client state + UI for server-side list pagination + column sorting,
// reused by the Issues / Tasks / Plans / Reminders lists (per @oopslink). The
// backend (handlers_pm_org applyPageItems / reminders) accepts sort/dir +
// page/page_size and returns { items: <page>, total }. These helpers turn that
// into a sortable table header + a pagination bar with a single state object.

export type SortDir = 'asc' | 'desc';

export interface ListControls {
  sort: string | undefined;
  dir: SortDir;
  page: number; // 1-based
  pageSize: number;
  /** Click a column header: same key flips direction; a new key selects it
   *  (defaulting to desc for time columns, asc otherwise). Resets to page 1. */
  toggleSort: (key: string) => void;
  setPage: (p: number) => void;
}

const TIME_KEYS = new Set(['created_at', 'updated_at', 'next_run_at', 'last_fired_at']);

export function useListControls(opts?: {
  pageSize?: number;
  defaultSort?: string;
  defaultDir?: SortDir;
}): ListControls {
  const [sort, setSort] = useState<string | undefined>(opts?.defaultSort);
  const [dir, setDir] = useState<SortDir>(opts?.defaultDir ?? 'desc');
  const [page, setPage] = useState(1);

  const toggleSort = (key: string): void => {
    setPage(1);
    if (sort === key) {
      setDir((d) => (d === 'asc' ? 'desc' : 'asc'));
      return;
    }
    setSort(key);
    setDir(TIME_KEYS.has(key) ? 'desc' : 'asc');
  };

  return { sort, dir, page, pageSize: opts?.pageSize ?? 25, toggleSort, setPage };
}

// SortHeader — a sortable table column header button. Shows an arrow on the
// active column and toggles direction on click; keyboard-accessible.
export function SortHeader({
  label,
  sortKey,
  controls,
  className,
}: {
  label: string;
  sortKey: string;
  controls: ListControls;
  className?: string;
}): React.ReactElement {
  const active = controls.sort === sortKey;
  const arrow = active ? (controls.dir === 'asc' ? '▲' : '▼') : '';
  return (
    <th className={className}>
      <button
        type="button"
        onClick={() => controls.toggleSort(sortKey)}
        className="inline-flex items-center gap-1 font-medium hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        data-testid={`sort-${sortKey}`}
        data-active={active ? 'true' : 'false'}
        aria-label={`Sort by ${label}${active ? (controls.dir === 'asc' ? ' (ascending)' : ' (descending)') : ''}`}
      >
        {label}
        {arrow && <span aria-hidden="true" className="text-[0.625rem] text-accent">{arrow}</span>}
      </button>
    </th>
  );
}

// Pagination — a compact prev/next bar with "page X of N" + a total count.
// Renders nothing when everything fits on one page (total <= pageSize).
export function Pagination({
  page,
  pageSize,
  total,
  onPageChange,
}: {
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (p: number) => void;
}): React.ReactElement | null {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  if (total <= pageSize) return null;
  const from = (page - 1) * pageSize + 1;
  const to = Math.min(page * pageSize, total);
  const btn =
    'rounded border border-border-base px-2 py-1 text-xs text-text-secondary hover:bg-bg-subtle disabled:opacity-40 disabled:hover:bg-transparent';
  return (
    <div
      className="mt-3 flex items-center justify-between gap-3 text-xs text-text-muted"
      data-testid="list-pagination"
    >
      <span data-testid="pagination-range">
        {from}–{to} of {total}
      </span>
      <span className="flex items-center gap-2">
        <button
          type="button"
          className={btn}
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
          data-testid="pagination-prev"
        >
          Prev
        </button>
        <span data-testid="pagination-page" className="tabular-nums">
          Page {page} / {pages}
        </span>
        <button
          type="button"
          className={btn}
          disabled={page >= pages}
          onClick={() => onPageChange(page + 1)}
          data-testid="pagination-next"
        >
          Next
        </button>
      </span>
    </div>
  );
}
