import type React from 'react';
import { Fragment, useMemo } from 'react';
import { useMembers, normalizeIdentityRef, identityRefOf } from '@/api/members';

// v2.8.1 #281 (mention-sidebar) entry ②: @mention tokens in message content.
//
// Message content is plain markdown; @mentions were NOT previously distinct
// tokens (the only "mention" concept was the conversation-level mention_count
// badge). So we add minimal, SAFE inline mention detection: an `@handle` whose
// handle resolves to a KNOWN org member (agent/user) becomes a clickable token
// that opens the kind-routed SenderDetailSidebar for that member's identity ref.
//
// Scope (kept surgical + safe):
//  - Only `@handle` that resolves to a known member is interactive; an unknown
//    `@foo` is left as plain text (no dangling/guessed ref → no wrong-kind body).
//  - Handle match is case-insensitive against the member display_name with
//    spaces removed AND against the bare identity tail (so `@BotOne` or
//    `@bot-1` both resolve). The resolved identity ref carries the kind prefix
//    (agent:/user:) so the sidebar routes to the right body (verify-not-trust).
//  - We only linkify plain text NOT inside code (the caller passes already-split
//    text nodes; this component just tokenizes one text string).

// useMentionResolver builds a handle→identity-ref map from the org members list
// (already loaded + cached). Keyed by BOTH a normalized display name (lowercased,
// spaces stripped) and the bare identity tail, so common author spellings of an
// @mention resolve. Value is the PREFIXED identity ref (user:/agent:) so the
// sidebar dispatches to the right kind.
export function useMentionResolver(): (handle: string) => string | null {
  const members = useMembers();
  const byHandle = useMemo(() => {
    const m = new Map<string, string>();
    for (const mem of members.data ?? []) {
      const ref = identityRefOf(mem);
      const tail = normalizeIdentityRef(mem.identity_id).toLowerCase();
      if (tail) m.set(tail, ref);
      if (mem.display_name) {
        const name = mem.display_name.toLowerCase().replace(/\s+/g, '');
        if (name) m.set(name, ref);
      }
    }
    return m;
  }, [members.data]);
  return (handle: string) => byHandle.get(handle.toLowerCase().replace(/\s+/g, '')) ?? null;
}

// MENTION_RE matches an @handle token: @ + word/.-/_ chars. A leading boundary
// (start or non-word char) is enforced via the surrounding split so we don't
// match an email's local-part-ish "@" mid-word. Capture group = the handle.
const MENTION_RE = /@([A-Za-z0-9][A-Za-z0-9._-]*)/g;

interface MentionTextProps {
  text: string;
  /** open the SenderDetailSidebar for the resolved (prefixed) identity ref. */
  onMention: (ref: string) => void;
  resolve: (handle: string) => string | null;
  /** link/mention text color — context-aware (mirrors MarkdownMessage's linkClass).
   * On the own chat bubble (#D1E3FF, a FIXED light surface that does NOT flip per
   * theme) this must be a FIXED-dark color (e.g. text-blue-700), NOT the text-accent
   * theme token (#3b82f6 → blue-on-blue <4.5 on #D1E3FF — the both-mode 命门). */
  linkClass?: string;
}

// MentionText tokenizes one plain-text string, turning each @handle that
// resolves to a known member into a clickable, keyboard-accessible mention
// button. Unresolved @handles stay plain text.
export function MentionText({
  text,
  onMention,
  resolve,
  linkClass = 'text-accent',
}: MentionTextProps): React.ReactElement {
  const parts: React.ReactNode[] = [];
  let last = 0;
  let match: RegExpExecArray | null;
  MENTION_RE.lastIndex = 0;
  let key = 0;
  while ((match = MENTION_RE.exec(text)) !== null) {
    const handle = match[1];
    const ref = resolve(handle);
    if (!ref) continue; // leave unknown @handle as plain text
    if (match.index > last) parts.push(<Fragment key={key++}>{text.slice(last, match.index)}</Fragment>);
    parts.push(
      <button
        key={key++}
        type="button"
        // stopPropagation: a mention click must NOT bubble to the message-row /
        // other message handlers (#281 critical: no click-conflict).
        onClick={(e) => {
          e.stopPropagation();
          onMention(ref);
        }}
        data-testid="mention-token"
        data-mention-ref={ref}
        aria-label={`View ${handle} details`}
        // both-mode: the mention reads as a link and uses the SAME context-aware
        // linkClass as MarkdownMessage's links — text-accent on theme surfaces, but a
        // FIXED-dark color (text-blue-700) on the own #D1E3FF bubble (avoids the
        // blue-on-blue <4.5 命门). NO alpha-tint fill (would render transparent).
        className={`rounded font-medium ${linkClass} hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`}
      >
        @{handle}
      </button>,
    );
    last = match.index + match[0].length;
  }
  if (last < text.length) parts.push(<Fragment key={key++}>{text.slice(last)}</Fragment>);
  return <>{parts}</>;
}
