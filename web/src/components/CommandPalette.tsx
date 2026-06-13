import type React from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useConversations } from '@/api/conversations';
import { orgPath, useOptionalOrgContext } from '@/OrgContext';

// CommandPalette — Cmd/Ctrl-K quick-switcher (v2.3 P6). Searches
// channels + DMs client-side (substring match on name/id;
// case-insensitive). No new dependency: the dropdown + keyboard
// navigation are ~80 lines instead of pulling cmdk (~13KB).
//
// Hidden by default; AppLayout owns the open/close state so the global
// ⌘K hook can flip it.

interface Item {
  label: string;
  href: string;
  hint: string;
}

export function CommandPalette({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): React.ReactElement | null {
  const navigate = useNavigate();
  // v2.8.1 fix: items hold app-absolute paths (/channels, …) but real routes
  // live under /organizations/{slug}; rewrite via orgPath so clicks navigate
  // instead of hitting a non-route → OrgRedirect → "nothing happened".
  const org = useOptionalOrgContext();
  const [query, setQuery] = useState('');
  const [selected, setSelected] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });

  // Build the full item list once per data change. Static nav targets
  // come first so an empty query still surfaces top-level pages.
  const items = useMemo<Item[]>(() => {
    const out: Item[] = [
      { label: 'Overview', href: '/', hint: 'page' },
      { label: 'Channels', href: '/channels', hint: 'page' },
      { label: 'DMs', href: '/dms', hint: 'page' },
      { label: 'Projects', href: '/projects', hint: 'page' },
      { label: 'Agents', href: '/agents', hint: 'page' },
      { label: 'Environment', href: '/environment', hint: 'page' },
      { label: 'Secrets', href: '/secrets', hint: 'page' },
      { label: 'Settings', href: '/settings', hint: 'page' },
    ];
    for (const c of channels.data ?? []) {
      out.push({
        label: `# ${c.name}`,
        href: `/channels/${encodeURIComponent(c.id)}`,
        hint: 'channel',
      });
    }
    for (const c of dms.data ?? []) {
      out.push({
        label: `◐ ${c.name || c.id}`,
        href: `/dms/${encodeURIComponent(c.id)}`,
        hint: 'dm',
      });
    }
    return out;
  }, [channels.data, dms.data]);

  const filtered = useMemo<Item[]>(() => {
    const q = query.trim().toLowerCase();
    if (!q) return items.slice(0, 12);
    return items.filter((it) => it.label.toLowerCase().includes(q)).slice(0, 20);
  }, [items, query]);

  // Reset focus + selection every time the palette opens.
  useEffect(() => {
    if (open) {
      setQuery('');
      setSelected(0);
      // Defer to next tick so the input is in the DOM.
      requestAnimationFrame(() => inputRef.current?.focus());
    }
  }, [open]);

  // Clamp selection if the result set shrinks below it.
  useEffect(() => {
    if (selected >= filtered.length) setSelected(Math.max(0, filtered.length - 1));
  }, [filtered.length, selected]);

  if (!open) return null;

  const commit = (idx: number) => {
    const item = filtered[idx];
    if (!item) return;
    navigate(orgPath(item.href, org?.slug));
    onClose();
  };

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      onClose();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelected((s) => Math.min(filtered.length - 1, s + 1));
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelected((s) => Math.max(0, s - 1));
      return;
    }
    if (e.key === 'Enter') {
      e.preventDefault();
      commit(selected);
    }
  };

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-label="Command palette"
      data-testid="command-palette"
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
      onClick={onClose}
    >
      <div
        className="pointer-events-none absolute inset-0 bg-black/40"
        aria-hidden="true"
      />
      <div
        className="relative w-full max-w-lg overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-3"
        onClick={(e) => e.stopPropagation()}
      >
        <input
          ref={inputRef}
          type="text"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setSelected(0);
          }}
          onKeyDown={handleKey}
          placeholder="Search pages, channels, DMs…"
          aria-label="Search"
          data-testid="palette-input"
          className="w-full border-b border-border-base bg-transparent px-4 py-3 text-sm text-text-primary placeholder:text-text-muted"
        />
        <ul role="listbox" aria-label="Results" className="max-h-80 overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <li className="px-4 py-6 text-center text-xs text-text-muted">
              No matches for “{query}”
            </li>
          ) : (
            filtered.map((it, i) => (
              <li
                key={`${it.hint}-${it.href}`}
                role="option"
                aria-selected={i === selected}
                onMouseEnter={() => setSelected(i)}
                onClick={() => commit(i)}
                className={[
                  'flex cursor-pointer items-center justify-between px-4 py-2 text-sm motion-safe:transition-colors',
                  i === selected ? 'bg-bg-subtle text-text-primary' : 'text-text-secondary',
                ].join(' ')}
                data-testid="palette-result"
              >
                <span>{it.label}</span>
                <span className="ml-3 text-[0.6875rem] uppercase tracking-wide text-text-muted">
                  {it.hint}
                </span>
              </li>
            ))
          )}
        </ul>
        <div className="border-t border-border-base bg-bg-subtle px-3 py-1.5 text-[0.6875rem] text-text-muted">
          <kbd className="rounded bg-bg-elevated px-1">↑↓</kbd> navigate ·{' '}
          <kbd className="rounded bg-bg-elevated px-1">↵</kbd> open ·{' '}
          <kbd className="rounded bg-bg-elevated px-1">Esc</kbd> close
        </div>
      </div>
    </div>
  );
}
