// listOrder — persisted manual ordering for the col② secondary-nav lists
// (Channels, My DMs, A2A, Humans, Agents). @oopslink: these lists are
// drag-reorderable. The chosen order is PER-USER and CLIENT-SIDE only
// (localStorage, like theme.ts and the DM-group collapse state) — there is no
// backend ordering column. The pure helpers below are unit-tested; the React
// glue lives in useListOrder.

const KEY_PREFIX = 'ac.navorder.';

// mergeOrder — project a saved id order onto the CURRENT ids. Saved ids that
// still exist keep their saved relative order and come first; ids not present in
// the saved order (newly created channels / DMs / agents) are appended in their
// natural incoming order; saved ids that no longer exist are dropped. Pure.
export function mergeOrder(savedIds: readonly string[], currentIds: readonly string[]): string[] {
  const current = new Set(currentIds);
  const seen = new Set<string>();
  const ordered: string[] = [];
  for (const id of savedIds) {
    if (current.has(id) && !seen.has(id)) {
      ordered.push(id);
      seen.add(id);
    }
  }
  for (const id of currentIds) {
    if (!seen.has(id)) {
      ordered.push(id);
      seen.add(id);
    }
  }
  return ordered;
}

// moveId — return a new ordering with `dragId` repositioned to immediately
// BEFORE `overId` (the predictable "insert before the row you dropped on" rule,
// consistent whether dragging up or down). No-op (returns a copy) if either id
// is missing or the two are equal. Pure.
export function moveId(ids: readonly string[], dragId: string, overId: string): string[] {
  if (dragId === overId) return ids.slice();
  const from = ids.indexOf(dragId);
  const to = ids.indexOf(overId);
  if (from === -1 || to === -1) return ids.slice();
  const next = ids.slice();
  next.splice(from, 1);
  const insertAt = next.indexOf(overId);
  next.splice(insertAt, 0, dragId);
  return next;
}

// readOrder / writeOrder — dependency-free localStorage access, guarded against
// disabled storage / quota / corrupt JSON (mirrors theme.ts). Keys are namespaced
// under ac.navorder.<key>; callers scope <key> per org + list.
export function readOrder(key: string): string[] {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return [];
    const raw = localStorage.getItem(KEY_PREFIX + key);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
}

export function writeOrder(key: string, ids: readonly string[]): void {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.setItem !== 'function') return;
    localStorage.setItem(KEY_PREFIX + key, JSON.stringify(ids));
  } catch {
    // ignore quota / disabled storage
  }
}
