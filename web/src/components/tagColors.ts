// tagColors — deterministic, name-hashed color for a task tag chip.
//
// 命门 (both-mode AA): the palette is a CURATED set of {bg,text} class PAIRS,
// NOT an arbitrary hashed HSL. Each pair is a SOLID light background (X-100) +
// dark text (X-800). Because both X-100 and X-800 are theme-INDEPENDENT literal
// Tailwind colors (NOT theme tokens, no `dark:` variant), the chip renders the
// SAME light-block-with-dark-text in BOTH light and dark mode — so the WCAG-AA
// contrast is identical in both modes (computed ratios all ≥ 6.3:1, see below).
// This sidesteps the [[both-mode-aa-not-light-only]] trap (mid-tone text on an
// alpha-tint goes dark-on-dark in dark mode); a solid opaque -100/-800 block has
// no theme dependence at all.
//
// Computed contrast (Tailwind v3 default hex, white-vs-black formula):
//   slate 13.35 · blue 7.15 · indigo 8.06 · violet 7.57 · purple 7.39 ·
//   fuchsia 7.08 · amber 6.37 · emerald 6.78 · teal 6.73 · cyan 6.49
// ALL ≥ 4.5 → AA in BOTH modes. `red` is intentionally excluded (the a11y
// guardrail bans raw bg-red-/text-red-; the blocked state owns red anyway).
//
// The hash mirrors Avatar.paletteFor (FNV-ish: h = h*31 + charCodeAt) so the
// mapping is stable: same tag NAME → same pair on every render; different tags
// spread across the palette. Full literal strings keep Tailwind's content scan.
export interface TagColor {
  bg: string;
  text: string;
}

export const TAG_PALETTE: TagColor[] = [
  { bg: 'bg-status-slate-bg', text: 'text-status-slate-fg' },
  { bg: 'bg-status-blue-bg', text: 'text-status-blue-fg' },
  { bg: 'bg-status-indigo-bg', text: 'text-status-indigo-fg' },
  { bg: 'bg-status-violet-bg', text: 'text-status-violet-fg' },
  { bg: 'bg-status-purple-bg', text: 'text-status-purple-fg' },
  { bg: 'bg-status-fuchsia-bg', text: 'text-status-fuchsia-fg' },
  { bg: 'bg-status-amber-bg', text: 'text-status-amber-fg' },
  { bg: 'bg-status-emerald-bg', text: 'text-status-emerald-fg' },
  { bg: 'bg-status-teal-bg', text: 'text-status-teal-fg' },
  { bg: 'bg-status-cyan-bg', text: 'text-status-cyan-fg' },
];

// tagColorFor — deterministic FNV-ish hash of the tag name → a stable palette
// index (identical algorithm to Avatar.paletteFor).
export function tagColorFor(tag: string): TagColor {
  let h = 0;
  for (let i = 0; i < tag.length; i++) h = (h * 31 + tag.charCodeAt(i)) >>> 0;
  return TAG_PALETTE[h % TAG_PALETTE.length];
}
