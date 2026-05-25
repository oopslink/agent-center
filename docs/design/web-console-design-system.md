# Web Console Design System — v2.3 UX/UI pass

> Source of truth for visual + interaction conventions in `web/` (the
> embedded SPA). Generated via the `ui-ux-pro-max` Claude Code skill
> (`.claude/skills/ui-ux-pro-max/`); see § 7 for the queries that
> produced the choices below.

## 1. Product framing

| Dimension | Value |
|---|---|
| Product type | Developer Tool / IDE (primary) + Productivity Tool (secondary) |
| Audience | Single user (loopback-only, ADR-0037), developer / operator running their own agent fleet |
| Density | Dashboard — denser than marketing, less dense than a trading terminal |
| Update model | Realtime (SSE) — UI must absorb event-driven mutations without flicker |
| Constraints | No remote auth (loopback); no multi-tenant; bundled into a single Go binary via `go:embed` (size budget — no heavy frameworks) |

## 2. Style — Minimalism & Swiss Style

Skill match (`--domain style`, `Minimalism & Swiss Style`):
- WCAG AAA accessible, tailwind-friendly, Low implementation complexity
- Best-for: enterprise apps, **dashboards**, documentation, **SaaS platforms**, **professional tools**
- Keywords: clean, spacious, functional, white space, high contrast, grid-based, essential

Composition rules:
- Grid-based main layout (12-col on ≥1024px, single col on <768px)
- Type hierarchy by **size + weight + spacing**, not by color
- Subtle hover 200–250ms; sharp shadows only for elevation; no gradients

**Secondary pattern:** Bento Box Grid for Home/Overview and Fleet pages
(group multiple at-a-glance widgets into cards of varying spans).

## 3. Color tokens — semantic, dual-mode-ready

Keep light mode as default (continuity with current SPA); add tokens so
dark mode is one CSS-vars flip away when we get to it.

### Light mode tokens (`:root` in `index.css`)

| Token | Value | Use |
|---|---|---|
| `--color-bg-base`       | `#F8FAFC` (slate-50)  | page background |
| `--color-bg-elevated`   | `#FFFFFF`              | cards, panels, modals |
| `--color-bg-subtle`     | `#F1F5F9` (slate-100) | sidebar, code blocks |
| `--color-border`        | `#E2E8F0` (slate-200) | divider, outline |
| `--color-border-strong` | `#CBD5E1` (slate-300) | input outline, table grid |
| `--color-text-primary`  | `#0F172A` (slate-900) | headings, body |
| `--color-text-secondary`| `#475569` (slate-600) | helper, captions |
| `--color-text-muted`    | `#94A3B8` (slate-400) | timestamps, placeholders |
| `--color-brand`         | `#1E40AF` (blue-800)  | brand wordmark, primary CTA bg |
| `--color-brand-hover`   | `#1D4ED8` (blue-700)  | CTA hover |
| `--color-accent`        | `#3B82F6` (blue-500)  | links, active nav, focus ring |
| `--color-success`       | `#22C55E` (green-500) | online dot, task done |
| `--color-warning`       | `#F59E0B` (amber-500) | connecting, IR pending |
| `--color-danger`        | `#EF4444` (red-500)   | failed task, revoke, destructive |

Source: Analytics Dashboard palette (`--domain color`, blue+amber) +
Developer Tool / IDE (green CTA convention).

### Dark mode tokens (deferred — Pass 3+)

Same names, mapped to dark values (`--color-bg-base: #0F172A` etc.).
Toggle via `<html class="dark">` + tailwind `darkMode: 'class'`.
Per skill rule `color-dark-mode`: use desaturated tonal variants, not
inverted; test contrast separately.

## 4. Typography — keep Tech Startup pair + add JetBrains Mono

Current SPA loads no fonts (system stack). Skill recommended pairs:

| Pair | Heading | Body | Mood | Decision |
|---|---|---|---|---|
| **Tech Startup** | Space Grotesk | DM Sans | "tech, startup, modern, SaaS, **developer tools**, AI products" | ✅ Adopt for UI |
| **Developer Mono** | JetBrains Mono | IBM Plex Sans | "code, developer, technical, hacker" | ➕ Adopt mono half (JetBrains Mono for code/IDs/timestamps) |
| Dashboard Data | Fira Code | Fira Sans | "dashboard, data, analytics" | Considered, rejected — Space Grotesk gives stronger brand voice |

CSS import (web-fonts hosted on Google Fonts, loaded once in
`index.html`):

```html
<link href="https://fonts.googleapis.com/css2?family=Space+Grotesk:wght@400;500;600;700&family=DM+Sans:wght@400;500;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
```

Tailwind config (`web/tailwind.config.js`):

```js
fontFamily: {
  sans:    ['"DM Sans"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
  heading: ['"Space Grotesk"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
  mono:    ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
}
```

Type scale (rem):

| Token | Size | Line | Use |
|---|---|---|---|
| `text-xs`   | 0.75  | 1.5 | timestamps, captions |
| `text-sm`   | 0.875 | 1.5 | body, table cells |
| `text-base` | 1.0   | 1.5 | composer, modal body |
| `text-lg`   | 1.125 | 1.4 | section labels |
| `text-xl`   | 1.25  | 1.3 | page subtitle |
| `text-2xl`  | 1.5   | 1.25| page title (h1) |
| `text-3xl`  | 1.875 | 1.2 | dashboard hero stat |

Per skill `font-pairing`: heading uses 600–700 weight; body 400; labels 500.
Per `number-tabular`: use `font-variant-numeric: tabular-nums` on data
columns (counts in sidebar badges, fleet metrics, timestamps).

## 5. Spacing, radius, elevation

- **Spacing scale**: tailwind default 4-pt scale (0.25rem step). Per skill `spacing-scale`.
- **Border radius**: `0.375rem` (rounded-md) for inputs/buttons; `0.5rem` (rounded-lg) for cards/modals. No fully rounded except status dots and avatars.
- **Elevation scale** (per `elevation-consistent`):
  - `--shadow-1`: `0 1px 2px rgba(15,23,42,0.04)` — resting cards
  - `--shadow-2`: `0 4px 8px rgba(15,23,42,0.08)` — popovers, dropdowns
  - `--shadow-3`: `0 12px 24px rgba(15,23,42,0.12)` — modals, sheets

## 6. Component conventions — checklist

Each row is a skill rule (id from SKILL.md § Quick Reference). Already-shipped items are pre-checked.

### Accessibility (CRITICAL)
- [x] `viewport-meta` set (`web/index.html`)
- [ ] `focus-states` — 2px visible focus ring on all interactive elements (current SPA misses focus rings on NavLink, MessageComposer, modal close)
- [ ] `color-contrast` — verify text on `bg-slate-100` sidebar meets 4.5:1 (`slate-700` on `slate-100` is borderline)
- [ ] `aria-labels` — icon-only buttons in MessageComposer / DeriveBar / modals
- [ ] `keyboard-nav` — tab order audit
- [ ] `reduced-motion` — wrap all transitions in `motion-safe:` tailwind variant

### Touch & Interaction (CRITICAL)
- [ ] `touch-target-size` — min 44×44 for primary actions (NavLink height currently 28px)
- [ ] `cursor-pointer` — buttons explicit
- [ ] `loading-buttons` — disable + spinner during async (create channel, send message)
- [ ] `error-feedback` — inline near field, not toast-only

### Performance
- [x] `bundle-splitting` — already done via React.lazy per route
- [x] `lazy-loading` — Suspense fallback in place
- [ ] `progressive-loading` — replace "Loading…" text fallback with skeleton screens
- [ ] `content-jumping` — reserve space for SSE-arriving rows so list doesn't reflow on each event

### Style Selection
- [x] `consistency` — single SPA, no style mixing
- [ ] `no-emoji-icons` — audit for emoji usage (especially status indicators)
- [ ] `icon-style-consistent` — pick Heroicons (matches Minimalism + Tailwind house style) and stop using stock unicode dots
- [ ] `primary-action` — one primary CTA per screen; ensure modals don't compete

### Layout & Responsive
- [x] `breakpoint-consistency` — Tailwind default 640/768/1024/1280
- [ ] `mobile-first` — current SPA assumes desktop; sidebar fixed at 12rem doesn't collapse
- [ ] `container-width` — main content has no max-width; long messages stretch across 1920px monitors
- [ ] `visual-hierarchy` — sidebar groups (Conversations / Work / Admin) not yet visualized

### Forms & Feedback
- [ ] `input-labels` — visible label, not placeholder-only (audit composer, modals)
- [ ] `error-recovery` — error messages need "retry" affordance
- [ ] `empty-states` — current empty states are bare text + link; need icon + helper sentence + CTA button

### Navigation
- [x] `nav-state-active` — current NavLink active style works
- [ ] `nav-hierarchy` — primary vs secondary nav distinction missing (no settings/admin separator)
- [ ] `deep-linking` — already works (React Router); verify each detail page is sharable

## 7. Anti-patterns to avoid (skill `--domain ux`)

From Minimalism & Swiss Style + Developer Tool / IDE recommendations:
- ❌ Decorative gradients / blur effects (clashes with Minimalism)
- ❌ Emoji-as-icons (use Heroicons)
- ❌ Light mode-only (medium priority — add dark mode toggle in Pass 3+)
- ❌ Tight info density without breathing room (current sparse layout is actually fine; don't over-pack)
- ❌ Animating layout properties (`width`/`height`/`top`/`left`) — use `transform`/`opacity`
- ❌ Removing focus rings for aesthetics
- ❌ Hover-only affordances (touch users have no hover)

## 8. Implementation plan — Passes

Each Pass = one slock subtask + one PR. Per-ST cadence: audit-first
commit + impl commit if Pass is ≥3h.

| Pass | Scope | Est | Touches |
|---|---|---|---|
| **P1 Tokens** | tailwind.config theme extend (colors, fonts, radii, shadows) + CSS-vars in `index.css` + load Google Fonts | ~1.5h | `web/tailwind.config.js`, `web/src/index.css`, `web/index.html` |
| **P2 Layout shell** | AppLayout v2: branded header, sidebar grouping, identity affordance, skeleton fallback, responsive sidebar collapse | ~3h | `web/src/AppLayout.tsx`, `web/src/components/Sidebar.tsx` (new), header bits |
| **P3 Home page** | new `/` (or `/home`) Overview page — recent activity / pending IRs / running tasks / fleet status (Bento Box grid) | ~3h | `web/src/pages/Home.tsx` (new), router `/` route reordering |
| **P4 Empty + loading states** | unified `<EmptyState>` + `<Skeleton>` components; sweep all list/detail pages | ~2h | new `components/EmptyState.tsx`, `components/Skeleton.tsx`, refactor 14 pages |
| **P5 a11y + checklist sweep** | focus rings, contrast verify, aria-labels, touch targets, motion-safe | ~2h | global pass + a11y audit doc |
| **P6 (optional / v2.3.1)** | global search (cmdk) + keyboard shortcut layer; dark mode toggle | ~4h | new `useKeyShortcuts`, `CommandPalette` component, dark token map |

Total core (P1–P5): ~11.5h ≈ 2 dev-days end-to-end. P6 is deferrable.

**Verification per Pass:**
1. `cd web && pnpm test` (vitest green)
2. `cd web && pnpm build` (no errors)
3. `make build && ./bin/agent-center server --config=<minimal>` → screenshot at 1440×900 + 768 + 375 → attach to slock thread
4. `make e2e` after P5 (Playwright spec pass, no regression in v22-deployed-pipeline)

## 9. Out of scope for v2.3

- Storybook / design tokens externalization (overkill until component API stabilizes)
- Dark mode default switch (light is the current contract; add as toggle in P6, don't default-flip)
- Custom icon set (use Heroicons, don't roll own)
- Branding overhaul (logo / wordmark beyond text — needs separate brand decision)
- Marketing site (this is dev-tool only; no landing)
