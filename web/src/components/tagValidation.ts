// tagValidation — shared tag rules, extracted from TaskEditModal (#278) so the
// inline "+ Add" affordance in the TaskDetail sidebar validates IDENTICALLY to
// the modal (single source — no drift). Mirrors Dev's backend cleanTags:
//   • each tag ≤ 16 RUNES (count with [...tag].length, NOT .length — CJK 命门)
//   • ≤ 10 tags total · trimmed · non-empty · deduped (keep first)
export const MAX_TAG_RUNES = 16;
export const MAX_TAGS = 10;

export function runeLength(tag: string): number {
  return [...tag].length;
}

// validateTags returns an inline error message, or null when the set is valid.
export function validateTags(tags: string[]): string | null {
  if (tags.length > MAX_TAGS) return `Max ${MAX_TAGS} tags`;
  for (const tag of tags) {
    if (tag.trim() === '') return 'Tag cannot be empty';
    if (runeLength(tag) > MAX_TAG_RUNES) return `Tag too long (max ${MAX_TAG_RUNES})`;
  }
  return null;
}

// validateNewTag — validate a single candidate against the existing set for the
// inline add. Returns an error message, or null when the candidate is OK to add.
// A duplicate is NOT an error (the caller dedups silently → returns null).
export function validateNewTag(candidate: string, existing: string[]): string | null {
  const t = candidate.trim();
  if (t === '') return 'Tag cannot be empty';
  if (runeLength(t) > MAX_TAG_RUNES) return `Tag too long (max ${MAX_TAG_RUNES})`;
  if (existing.includes(t)) return null; // dedup — caller no-ops
  if (existing.length >= MAX_TAGS) return `Max ${MAX_TAGS} tags`;
  return null;
}
