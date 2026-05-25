import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';

// P5 a11y guardrails. These are static-text checks against the source
// tree — cheap to run, catch regressions, and don't require a real
// browser. We avoid pulling in axe-core for this milestone; a deeper
// audit can happen in P6.
//
// Each test below pins one rule from the skill checklist
// (docs/design/web-console-design-system.md § 6):
//   - focus-states: no `focus:outline-none` antipattern
//   - no-emoji-icons: no emoji used as a UI affordance
//   - color-not-only: no raw text-red-* / bg-red-* outside the token
//     layer (so danger states stay theme-able)

// Vitest runs from project root (web/). Resolve src/ absolutely so the
// walker doesn't get confused by ".".
const SRC = resolve(process.cwd(), 'src');

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (entry === 'node_modules' || entry === 'test') continue;
    const p = join(dir, entry);
    const s = statSync(p);
    if (s.isDirectory()) walk(p, out);
    else if (/\.(tsx?|css)$/.test(entry) && !entry.endsWith('.test.tsx') && entry !== 'a11y.test.tsx') {
      out.push(p);
    }
  }
  return out;
}

describe('a11y guardrails (v2.3 P5)', () => {
  const files = walk(SRC);

  it('no focus:outline-none without a replacement visible focus indicator', () => {
    const offenders: string[] = [];
    for (const f of files) {
      const txt = readFileSync(f, 'utf8');
      if (txt.includes('focus:outline-none')) {
        offenders.push(f.replace(SRC, ''));
      }
    }
    expect(offenders).toEqual([]);
  });

  it('no raw text-red-* utilities in src/components or src/pages — use text-danger', () => {
    const offenders: string[] = [];
    for (const f of files) {
      if (!f.includes('/components/') && !f.includes('/pages/')) continue;
      const txt = readFileSync(f, 'utf8');
      if (/\btext-red-\d{3}\b/.test(txt) || /\bbg-red-\d{3}\b/.test(txt)) {
        offenders.push(f.replace(SRC, ''));
      }
    }
    expect(offenders).toEqual([]);
  });

  it('no emoji used as icon (skill rule no-emoji-icons)', () => {
    // We exempt formatted-output utilities (e.g. CarryOverDivider's
    // status glyphs that ARE intentionally textual). The check looks
    // for unicode pictographs in JSX text positions.
    const PICTO = /[\u{1F300}-\u{1FAFF}\u{2600}-\u{27BF}]/u;
    const offenders: string[] = [];
    for (const f of files) {
      const txt = readFileSync(f, 'utf8');
      const lines = txt.split('\n');
      lines.forEach((line, i) => {
        if (PICTO.test(line)) {
          offenders.push(`${f.replace(SRC, '')}:${i + 1}: ${line.trim().slice(0, 80)}`);
        }
      });
    }
    expect(offenders).toEqual([]);
  });
});
