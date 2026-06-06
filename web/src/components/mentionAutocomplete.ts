// Core logic for the #275 #/@ mention picker — pure functions so the locked
// contracts (trigger detection + insert-with-trailing-space for the wake
// word-boundary) are unit-testable independent of React.

export type MentionTrigger = '@' | '#';

export interface ActiveTrigger {
  trigger: MentionTrigger; // '@' (members) or '#' (channels)
  query: string; // text typed after the trigger (may be empty)
  start: number; // index of the trigger char in the value
}

// detectTrigger finds an active @/# mention trigger immediately before the
// cursor: a trigger char at a word start (line start or after whitespace) with
// only non-whitespace, non-trigger chars between it and the cursor. Returns null
// when the cursor is not inside such a token (so the picker stays closed).
export function detectTrigger(value: string, cursor: number): ActiveTrigger | null {
  const before = value.slice(0, Math.max(0, cursor));
  // (start|whitespace) ( @ | # ) (query = no whitespace / no trigger chars)$
  const m = /(^|\s)([@#])([^\s@#]*)$/.exec(before);
  if (!m) return null;
  const trigger = m[2] as MentionTrigger;
  const query = m[3];
  const start = cursor - query.length - 1; // position of the @/# char
  return { trigger, query, start };
}

// insertToken replaces the active trigger+query (from `start` to `cursor`) with
// `token`, which the caller builds WITH a trailing space (e.g. "@Alice " or
// "#general ") — the trailing space guarantees the wake matcher's right
// word-boundary (`@Alicex` would not wake) and lets the user keep typing. Returns
// the new value and the cursor position after the inserted token.
export function insertToken(
  value: string,
  start: number,
  cursor: number,
  token: string,
): { value: string; cursor: number } {
  const next = value.slice(0, start) + token + value.slice(cursor);
  return { value: next, cursor: start + token.length };
}

// mentionToken builds the text inserted for a chosen option: the trigger + the
// name + a single trailing space. Always exactly one trailing space.
export function mentionToken(trigger: MentionTrigger, name: string): string {
  return `${trigger}${name} `;
}
