import { useEffect, useId, useMemo, useState } from 'react';
import type React from 'react';
import { useMembers } from '@/api/members';
import { useConversations } from '@/api/conversations';
import { detectTrigger, insertToken, mentionToken, type ActiveTrigger } from './mentionAutocomplete';
import { optionElementId, type MentionOption } from './MentionPicker';

const MAX_OPTIONS = 50; // large-list cap (Tester2 §4.3 perf gate)

// useMentionAutocomplete wires the #275 picker into a textarea: trigger detection
// (from the textarea's live value+cursor — read directly to avoid stale closure),
// option filtering (@→members, #→channels), roving active option (by stable id),
// keyboard, and token-with-trailing-space insertion at the cursor.
export function useMentionAutocomplete({
  setValue,
  textareaRef,
}: {
  setValue: (v: string) => void;
  textareaRef: React.RefObject<HTMLTextAreaElement | null>;
}) {
  const listboxId = useId();
  const [active, setActive] = useState<ActiveTrigger | null>(null);
  const [activeId, setActiveId] = useState<string | null>(null);
  // The trigger the user dismissed with Esc — kept closed until the trigger
  // context (position/query) changes, so the keyup after Esc can't reopen it.
  const [dismissed, setDismissed] = useState<ActiveTrigger | null>(null);

  const members = useMembers();
  const conversations = useConversations();

  const options: MentionOption[] = useMemo(() => {
    if (!active) return [];
    const q = active.query.toLowerCase();
    if (active.trigger === '@') {
      // @all broadcast (per @oopslink): a synthetic option surfaced when the
      // query is a prefix of "all" (or empty). Selecting it inserts "@all ",
      // which addresses everyone — effective ONLY for human senders (the backend
      // wake/badge layers gate @all on a human sender). Appended AFTER members so
      // it never hijacks the default (first) selection for an ambiguous prefix
      // like "@a" (which should still default to a member named Alice).
      const all: MentionOption[] = 'all'.startsWith(q)
        ? [{ id: '@all', name: 'all', secondary: 'Everyone in this conversation' }]
        : [];
      return (members.data ?? [])
        .filter((m) => (m.display_name ?? '').toLowerCase().includes(q))
        .map((m) => ({
          id: m.identity_id,
          name: m.display_name ?? m.identity_id,
          secondary: m.identity_id,
        }))
        .concat(all)
        .slice(0, MAX_OPTIONS);
    }
    return (conversations.data ?? [])
      .filter((c) => c.kind === 'channel' && c.name.toLowerCase().includes(q))
      .slice(0, MAX_OPTIONS)
      .map((c) => ({ id: c.id, name: c.name, secondary: c.id }));
  }, [active, members.data, conversations.data]);

  // Keep activeId valid (default to the first option) when the list re-filters.
  useEffect(() => {
    if (options.length === 0) {
      setActiveId(null);
      return;
    }
    setActiveId((cur) => (cur && options.some((o) => o.id === cur) ? cur : options[0].id));
  }, [options]);

  const open = active !== null;

  // Recompute the active trigger from the textarea's CURRENT value + cursor.
  // Call on input / keyup / click so the picker tracks the caret.
  const sync = () => {
    const ta = textareaRef.current;
    if (!ta) return;
    const t = detectTrigger(ta.value, ta.selectionStart ?? ta.value.length);
    // Stay closed if the user Esc-dismissed this exact trigger (same position +
    // query) — otherwise the keyup that follows Escape re-detects the still-
    // present trigger and reopens the picker (Esc runtime no-op, Tester2 §4.3).
    if (
      t &&
      dismissed &&
      t.trigger === dismissed.trigger &&
      t.start === dismissed.start &&
      t.query === dismissed.query
    ) {
      setActive(null);
      return;
    }
    if (dismissed) setDismissed(null); // trigger changed / gone → dismissal expires
    setActive(t);
  };

  // Close + remember the dismissed trigger so the trailing keyup can't reopen it.
  const close = () => {
    setDismissed(active);
    setActive(null);
  };

  const onSelect = (o: MentionOption) => {
    const ta = textareaRef.current;
    if (!ta || !active) return;
    const cursor = ta.selectionStart ?? ta.value.length;
    const token = mentionToken(active.trigger, o.name); // includes trailing space
    const r = insertToken(ta.value, active.start, cursor, token);
    setValue(r.value);
    setActive(null);
    // Restore focus + place the caret after the inserted token (post-render).
    requestAnimationFrame(() => {
      const t = textareaRef.current;
      if (t) {
        t.focus();
        t.setSelectionRange(r.cursor, r.cursor);
      }
    });
  };

  const move = (delta: number) => {
    if (options.length === 0) return;
    const idx = options.findIndex((o) => o.id === activeId);
    const next = (idx + delta + options.length) % options.length;
    setActiveId(options[next].id);
  };

  // Returns true when it handled the key — the composer must then NOT send /
  // newline (the picker owns ↑↓/Enter/Tab/Esc while open).
  const onKeyDown = (e: React.KeyboardEvent): boolean => {
    if (!open) return false;
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        move(1);
        return true;
      case 'ArrowUp':
        e.preventDefault();
        move(-1);
        return true;
      case 'Escape':
        e.preventDefault();
        close();
        return true;
      case 'Enter':
      case 'Tab': {
        const o = options.find((x) => x.id === activeId);
        if (o) {
          e.preventDefault();
          onSelect(o);
          return true;
        }
        return false;
      }
      default:
        return false;
    }
  };

  return {
    open,
    listboxId,
    options,
    activeId,
    activeOptionId: activeId ? optionElementId(listboxId, activeId) : undefined,
    onKeyDown,
    onSelect,
    onHoverActivate: setActiveId,
    sync,
    close,
  };
}
