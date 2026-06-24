// Shared model series palette for the F6 trend chart + Top Cost Tasks bars,
// matching the mockup legend (opus = blue, sonnet = green, haiku = purple,
// fable = amber). These
// are data-viz series colors applied via inline SVG fill / style (NOT Tailwind
// palette classNames), so the no-raw-colors-spa lint does not apply; the mid-tone
// hues read acceptably on both the light and dark elevated card backgrounds.
// raw-color-ok: data-visualization series palette (not theme chrome).

const PALETTE: Record<string, string> = {
  opus: '#3b82f6', // blue
  sonnet: '#22c55e', // green
  haiku: '#8b5cf6', // purple
  fable: '#f59e0b', // amber
};

const FALLBACK = '#94a3b8'; // slate — unknown / unmapped model

// modelColor maps a model id (e.g. "claude-opus-4-8") to its series color by
// matching the family keyword; unknown models get a neutral slate.
export function modelColor(model: string): string {
  const m = model.toLowerCase();
  for (const key of Object.keys(PALETTE)) {
    if (m.includes(key)) return PALETTE[key];
  }
  return FALLBACK;
}

// modelShortLabel trims a full model id to its family for compact chips
// ("claude-opus-4-8" → "opus"); unknown ids pass through unchanged.
export function modelShortLabel(model: string): string {
  const m = model.toLowerCase();
  for (const key of Object.keys(PALETTE)) {
    if (m.includes(key)) return key;
  }
  return model;
}
