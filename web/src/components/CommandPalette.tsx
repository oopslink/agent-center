import type React from 'react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useConversations, useCreateConversation } from '@/api/conversations';
import { useMembers, identityRefOf, normalizeIdentityRef } from '@/api/members';
import { useAppStore } from '@/store/app';
import { orgPath, useOptionalOrgContext } from '@/OrgContext';

// CommandPalette — Cmd/Ctrl-K quick-switcher (v2.3 P6). Searches
// channels + DMs client-side (substring match on name/id;
// case-insensitive). No new dependency: the dropdown + keyboard
// navigation are ~80 lines instead of pulling cmdk (~13KB).
//
// `@` MODE: when the query starts with "@", the palette becomes an agent/member
// picker — it lists the org members you can DM (agents + humans, joined, minus
// self) filtered by the text after "@". Committing opens your DM with that member,
// creating it if none exists (the create endpoint is idempotent: an existing DM is
// reused). This makes ⌘K + "@name" a one-keystroke way to jump into a DM.
//
// Hidden by default; AppLayout owns the open/close state so the global
// ⌘K hook can flip it.

// A palette row: either a navigation target (page/channel/dm) or an agent/member
// to open a DM with. `kind` drives commit (navigate vs open-or-create DM).
type Item =
  | { kind: 'nav'; label: string; href: string; hint: string; key: string }
  | { kind: 'dm-agent'; label: string; ref: string; hint: string; key: string };

export function CommandPalette({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): React.ReactElement | null {
  const { t } = useTranslation('common');
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
  const members = useMembers();
  const createConversation = useCreateConversation();
  const me = useAppStore((s) => s.currentUserId);
  const meBare = me ? normalizeIdentityRef(me) : '';

  // `@`-prefixed queries switch to the agent/member DM picker.
  const atMode = query.startsWith('@');

  // Build the full nav item list once per data change. Static nav targets
  // come first so an empty query still surfaces top-level pages.
  const items = useMemo<Item[]>(() => {
    const out: Item[] = [
      { kind: 'nav', label: 'Overview', href: '/', hint: 'page', key: 'nav-/' },
      { kind: 'nav', label: 'Channels', href: '/channels', hint: 'page', key: 'nav-/channels' },
      { kind: 'nav', label: 'DMs', href: '/dms', hint: 'page', key: 'nav-/dms' },
      { kind: 'nav', label: 'Projects', href: '/projects', hint: 'page', key: 'nav-/projects' },
      { kind: 'nav', label: 'Issues', href: '/issues', hint: 'page', key: 'nav-/issues' },
      { kind: 'nav', label: 'Tasks', href: '/tasks', hint: 'page', key: 'nav-/tasks' },
      { kind: 'nav', label: 'Plans', href: '/plans', hint: 'page', key: 'nav-/plans' },
      { kind: 'nav', label: 'Repos', href: '/repos', hint: 'page', key: 'nav-/repos' },
      { kind: 'nav', label: 'Templates', href: '/templates', hint: 'page', key: 'nav-/templates' },
      { kind: 'nav', label: 'Agents', href: '/agents', hint: 'page', key: 'nav-/agents' },
      { kind: 'nav', label: 'Environment', href: '/environment', hint: 'page', key: 'nav-/environment' },
      { kind: 'nav', label: 'Secrets', href: '/secrets', hint: 'page', key: 'nav-/secrets' },
      { kind: 'nav', label: 'Settings', href: '/settings', hint: 'page', key: 'nav-/settings' },
    ];
    for (const c of channels.data ?? []) {
      out.push({
        kind: 'nav',
        label: `# ${c.name}`,
        href: `/channels/${encodeURIComponent(c.id)}`,
        hint: 'channel',
        key: `channel-${c.id}`,
      });
    }
    for (const c of dms.data ?? []) {
      out.push({
        kind: 'nav',
        label: `◐ ${c.name || c.id}`,
        href: `/dms/${encodeURIComponent(c.id)}`,
        hint: 'dm',
        key: `dm-${c.id}`,
      });
    }
    return out;
  }, [channels.data, dms.data]);

  // `@` mode: the org members you can DM — agents + humans that have joined,
  // excluding yourself. Labeled "@name" (falls back to the identity id).
  const agentItems = useMemo<Item[]>(() => {
    return (members.data ?? [])
      .filter((m) => m.status === 'joined' && normalizeIdentityRef(m.identity_id) !== meBare)
      .map((m) => ({
        kind: 'dm-agent' as const,
        label: `@${m.display_name || normalizeIdentityRef(m.identity_id)}`,
        ref: identityRefOf(m),
        hint: m.kind === 'agent' ? 'agent' : 'human',
        key: `member-${m.identity_id}`,
      }));
  }, [members.data, meBare]);

  const filtered = useMemo<Item[]>(() => {
    if (atMode) {
      const q = query.slice(1).trim().toLowerCase();
      const list = q
        ? agentItems.filter((it) => it.label.toLowerCase().includes(q))
        : agentItems;
      return list.slice(0, 20);
    }
    const q = query.trim().toLowerCase();
    if (!q) return items.slice(0, 14); // show all top-level pages before channels/DMs
    return items.filter((it) => it.label.toLowerCase().includes(q)).slice(0, 20);
  }, [atMode, agentItems, items, query]);

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
    if (item.kind === 'dm-agent') {
      // Open my DM with this member, creating it if none exists (the create
      // endpoint is idempotent — an existing DM is returned, not duplicated).
      if (createConversation.isPending) return;
      createConversation.mutate(
        { kind: 'dm', members: [item.ref] },
        {
          onSuccess: (res) => {
            navigate(orgPath(`/dms/${encodeURIComponent(res.conversation_id)}`, org?.slug));
            onClose();
          },
        },
      );
      return;
    }
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
      aria-label={t('commandPalette.dialogAriaLabel')}
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
          placeholder={t('commandPalette.searchPlaceholder')}
          aria-label={t('commandPalette.searchAriaLabel')}
          data-testid="palette-input"
          className="w-full border-b border-border-base bg-transparent px-4 py-3 text-sm text-text-primary placeholder:text-text-muted"
        />
        <ul role="listbox" aria-label={t('commandPalette.resultsAriaLabel')} className="max-h-80 overflow-y-auto py-1">
          {filtered.length === 0 ? (
            <li className="px-4 py-6 text-center text-xs text-text-muted">
              {t('commandPalette.noMatches', { query })}
            </li>
          ) : (
            filtered.map((it, i) => (
              <li
                key={it.key}
                role="option"
                aria-selected={i === selected}
                onMouseEnter={() => setSelected(i)}
                onClick={() => commit(i)}
                className={[
                  'flex cursor-pointer items-center justify-between px-4 py-3 md:py-2 text-sm motion-safe:transition-colors',
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
          <kbd className="rounded bg-bg-elevated px-1">↑↓</kbd> {t('commandPalette.navigate')} ·{' '}
          <kbd className="rounded bg-bg-elevated px-1">↵</kbd> {t('commandPalette.open')} ·{' '}
          <kbd className="rounded bg-bg-elevated px-1">Esc</kbd> {t('commandPalette.close')}
        </div>
      </div>
    </div>
  );
}
