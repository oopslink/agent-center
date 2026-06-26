import type React from 'react';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { EntityOption } from './EntitySelect';

interface EntityMultiSelectProps {
  /** Base test id; renders `${testId}-trigger|-search|-options|-option|-empty|-chip`. */
  testId: string;
  options: EntityOption[];
  /** Controlled selected values. */
  values: string[];
  onChange: (values: string[]) => void;
  placeholder?: string;
  searchPlaceholder?: string;
  emptyLabel?: string;
  disabled?: boolean;
  ariaLabel?: string;
}

// EntityMultiSelect (§1a) — the multi-select sibling of EntitySelect: a
// searchable dropdown whose chosen entities render as removable chips in the
// trigger. It exists so multi-pick UIs (e.g. a reminder's remindees) never fall
// back to a grid of toggle-pills or a column of bare checkboxes — both of which
// the UX standards forbid (see docs/rules/ux-standards.md §1 / §1a). Selection
// toggles in-place and the popover stays open so several picks need one trip.
// The popover is PORTALED to <body> with fixed positioning (same rationale as
// EntitySelect / T194) so an `overflow:auto` ancestor never clips it.
export function EntityMultiSelect({
  testId,
  options,
  values,
  onChange,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyLabel = 'No matches.',
  disabled = false,
  ariaLabel,
}: EntityMultiSelectProps): React.ReactElement {
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState('');
  const rootRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{
    left: number;
    minWidth: number;
    maxHeight: number;
    top?: number;
    bottom?: number;
  } | null>(null);

  const selectedSet = useMemo(() => new Set(values), [values]);
  const selected = useMemo(
    () => options.filter((o) => selectedSet.has(o.value)),
    [options, selectedSet],
  );

  const filtered = useMemo(() => {
    const f = q.trim().toLowerCase();
    if (!f) return options;
    return options.filter(
      (o) => o.label.toLowerCase().includes(f) || o.value.toLowerCase().includes(f),
    );
  }, [options, q]);

  const computePos = useCallback(() => {
    const el = triggerRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const margin = 4;
    const spaceBelow = window.innerHeight - r.bottom - 8;
    const spaceAbove = r.top - 8;
    const below = spaceBelow >= 220 || spaceBelow >= spaceAbove;
    setPos({
      left: r.left,
      minWidth: Math.max(r.width, 208),
      maxHeight: Math.max(160, (below ? spaceBelow : spaceAbove) - margin),
      ...(below ? { top: r.bottom + margin } : { bottom: window.innerHeight - r.top + margin }),
    });
  }, []);

  useLayoutEffect(() => {
    if (!open) return;
    computePos();
    const onMove = () => computePos();
    window.addEventListener('scroll', onMove, true);
    window.addEventListener('resize', onMove);
    return () => {
      window.removeEventListener('scroll', onMove, true);
      window.removeEventListener('resize', onMove);
    };
  }, [open, computePos]);

  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const t = e.target as Node;
      if (!rootRef.current?.contains(t) && !popoverRef.current?.contains(t)) setOpen(false);
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  useEffect(() => {
    if (!open) setQ('');
  }, [open]);

  const toggle = (v: string) => {
    onChange(selectedSet.has(v) ? values.filter((x) => x !== v) : [...values, v]);
  };
  const remove = (v: string) => onChange(values.filter((x) => x !== v));

  return (
    <div className="relative" ref={rootRef}>
      <button
        ref={triggerRef}
        type="button"
        data-testid={`${testId}-trigger`}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        onClick={() => !disabled && setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-1.5 rounded border border-border-base bg-bg-elevated px-2.5 py-2 text-left text-sm text-text-primary focus:border-accent disabled:opacity-50"
      >
        <span className="flex min-w-0 flex-1 flex-wrap items-center gap-1.5">
          {selected.length === 0 ? (
            <span className="text-text-muted">{placeholder}</span>
          ) : (
            selected.map((o) => (
              <span
                key={o.value}
                data-testid={`${testId}-chip`}
                data-value={o.value}
                className="inline-flex max-w-full items-center gap-1 rounded-full border border-brand/40 bg-brand/10 px-2 py-0.5 text-xs text-brand"
              >
                {o.leading}
                <span className="truncate">{o.label}</span>
                {/* Remove this chip. A nested <button> would be invalid inside the
                    trigger <button>, so this is a role=button span with its own
                    click that stops the open/close toggle from firing. */}
                <span
                  role="button"
                  tabIndex={0}
                  aria-label={`Remove ${o.label}`}
                  data-testid={`${testId}-chip-remove`}
                  onClick={(e) => {
                    e.stopPropagation();
                    remove(o.value);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      e.stopPropagation();
                      remove(o.value);
                    }
                  }}
                  className="cursor-pointer rounded-full px-0.5 leading-none text-brand/70 hover:text-brand"
                >
                  ×
                </span>
              </span>
            ))
          )}
        </span>
        <span aria-hidden="true" className="ml-1 shrink-0 text-text-muted">⌄</span>
      </button>

      {open && createPortal(
        <div
          ref={popoverRef}
          className="fixed z-50 flex flex-col overflow-hidden rounded border border-border-base bg-bg-elevated shadow-lg"
          style={{
            left: pos?.left,
            top: pos?.top,
            bottom: pos?.bottom,
            minWidth: pos?.minWidth,
            maxWidth: 'min(20rem, calc(100vw - 16px))',
            maxHeight: pos?.maxHeight,
            visibility: pos ? 'visible' : 'hidden',
          }}
        >
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
                if (filtered.length > 0) toggle(filtered[0].value);
              }
            }}
            placeholder={searchPlaceholder}
            aria-label="Search"
            className="block w-full rounded-t border-0 border-b border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:ring-2 focus-visible:ring-[var(--ring-color)]"
          />
          <ul className="min-h-0 flex-1 overflow-y-auto py-1" data-testid={`${testId}-options`} role="listbox" aria-multiselectable="true">
            {filtered.length === 0 && (
              <li className="px-3 py-2 text-xs text-text-muted" data-testid={`${testId}-empty`}>
                {emptyLabel}
              </li>
            )}
            {filtered.map((o) => {
              const on = selectedSet.has(o.value);
              return (
                <li key={o.value} role="none">
                  <button
                    type="button"
                    role="option"
                    aria-selected={on}
                    data-testid={`${testId}-option`}
                    data-value={o.value}
                    onClick={() => toggle(o.value)}
                    className={`flex w-full items-center justify-between gap-2 px-3 py-1.5 text-left text-sm hover:bg-bg-subtle ${
                      on ? 'font-medium text-brand' : 'text-text-primary'
                    }`}
                  >
                    <span className="flex min-w-0 items-center gap-1.5">
                      {o.leading}
                      <span className="min-w-0">
                        <span className="block truncate">{o.label}</span>
                        {o.hint && <span className="block truncate text-xs text-text-muted">{o.hint}</span>}
                      </span>
                    </span>
                    {/* Selected marker: an SVG check icon, never an emoji glyph
                        (a11y no-emoji-icons) and never a checkbox input (UX
                        standards §1a). aria-selected carries the real state; the
                        marker keeps its slot (invisible when unselected) so rows
                        don't shift. */}
                    <span aria-hidden="true" className={`shrink-0 text-brand ${on ? '' : 'invisible'}`}>
                      <CheckIcon />
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
        </div>,
        document.body,
      )}
    </div>
  );
}

// CheckIcon — the selected-option marker (inline SVG, stroke-current inherits the
// parent's text-brand). Matches the project's local CheckIcon convention
// (ProjectMemberAddModal / MemberInviteModal) and replaces the prior check glyph
// that tripped the no-emoji-icons a11y rule.
function CheckIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M5 10.5l3.5 3.5L15 6.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
