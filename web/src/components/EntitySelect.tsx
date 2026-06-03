import type React from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';

export interface EntityOption {
  /** The value submitted on select (e.g. a worker_id or an `agent:<id>` ref). */
  value: string;
  /** Human label shown in the trigger + list. */
  label: string;
  /** Optional secondary line under the label. */
  hint?: string;
  /** Optional short uppercase tag rendered on the right (e.g. kind/status). */
  badge?: string;
}

interface EntitySelectProps {
  /** Base test id; renders `${testId}-trigger|-search|-options|-option|-empty`. */
  testId: string;
  options: EntityOption[];
  /** Controlled selected value. */
  value?: string;
  onChange: (value: string) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyLabel?: string;
  disabled?: boolean;
  ariaLabel?: string;
}

// EntitySelect (#191) — a single shared searchable dropdown for picking an
// entity (worker / agent / member / …), replacing the assortment of plain
// `<select>`s and bespoke pick-lists. A trigger shows the current selection;
// clicking opens a popover with a filter input + the matching options. The
// component is controlled (value + onChange) and closes on select / Escape /
// click-outside.
export function EntitySelect({
  testId,
  options,
  value,
  onChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyLabel = 'No matches.',
  disabled = false,
  ariaLabel,
}: EntitySelectProps): React.ReactElement {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const rootRef = useRef<HTMLDivElement>(null);

  const selected = options.find((o) => o.value === value);

  const filtered = useMemo(() => {
    const f = q.trim().toLowerCase();
    if (!f) return options;
    return options.filter(
      (o) => o.label.toLowerCase().includes(f) || o.value.toLowerCase().includes(f),
    );
  }, [options, q]);

  // Close on outside click.
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  // Reset the query whenever the popover closes so it reopens clean.
  useEffect(() => {
    if (!open) setQ('');
  }, [open]);

  const choose = (v: string) => {
    onChange(v);
    setOpen(false);
  };

  return (
    <div className="relative" ref={rootRef}>
      <button
        type="button"
        data-testid={`${testId}-trigger`}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        onClick={() => !disabled && setOpen((v) => !v)}
        className="flex w-full items-center justify-between rounded border border-border-base bg-bg-elevated px-3 py-2 text-left text-sm text-text-primary focus:border-accent disabled:opacity-50"
      >
        <span className={selected ? '' : 'text-text-muted'}>
          {selected ? selected.label : placeholder}
        </span>
        <span aria-hidden="true" className="ml-2 text-text-muted">⌄</span>
      </button>

      {open && (
        <div className="absolute left-0 right-0 z-30 mt-1 rounded border border-border-base bg-bg-elevated shadow-lg">
          <input
            data-testid={`${testId}-search`}
            value={q}
            autoFocus
            onChange={(e) => setQ(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Escape') {
                setOpen(false);
              } else if (e.key === 'Enter') {
                e.preventDefault();
                if (filtered.length > 0) choose(filtered[0].value);
              }
            }}
            placeholder={searchPlaceholder}
            className="block w-full rounded-t border-0 border-b border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
          />
          <ul className="max-h-60 overflow-y-auto py-1" data-testid={`${testId}-options`} role="listbox">
            {filtered.length === 0 && (
              <li className="px-3 py-2 text-xs text-text-muted" data-testid={`${testId}-empty`}>
                {emptyLabel}
              </li>
            )}
            {filtered.map((o) => (
              <li key={o.value} role="none">
                <button
                  type="button"
                  role="option"
                  aria-selected={o.value === value}
                  data-testid={`${testId}-option`}
                  data-value={o.value}
                  onClick={() => choose(o.value)}
                  className={`flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-sm hover:bg-bg-subtle ${
                    o.value === value ? 'font-medium text-brand' : 'text-text-primary'
                  }`}
                >
                  <span className="min-w-0">
                    <span className="block truncate">{o.label}</span>
                    {o.hint && <span className="block truncate text-xs text-text-muted">{o.hint}</span>}
                  </span>
                  {o.badge && (
                    <span className="shrink-0 rounded bg-bg-subtle px-1.5 py-0.5 text-xs uppercase text-text-muted">
                      {o.badge}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
