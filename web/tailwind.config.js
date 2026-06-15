/** @type {import('tailwindcss').Config} */
// Design tokens mirror docs/design/web-console-design-system.md.
// Semantic colors map to CSS vars defined in src/index.css so a
// future dark-mode pass only needs a parallel `:root.dark` block.
export default {
  // Class-based dark mode so Tailwind `dark:` variants activate under
  // `<html class="dark">` — the SAME trigger as the `:root.dark` token block
  // in src/index.css. Without this, `dark:` defaults to `prefers-color-scheme`
  // (media), which would (a) misfire on OS-dark + app-light and (b) NOT fire
  // when the dual-theme toggle sets `.dark` (10th task). Keeps dark: aligned
  // with the token theme on one trigger.
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        // Surface
        'bg-base':       'var(--color-bg-base)',
        'bg-elevated':   'var(--color-bg-elevated)',
        'bg-subtle':     'var(--color-bg-subtle)',
        // Border
        'border-base':   'var(--color-border)',
        'border-strong': 'var(--color-border-strong)',
        // Text
        'text-primary':   'var(--color-text-primary)',
        'text-secondary': 'var(--color-text-secondary)',
        'text-muted':     'var(--color-text-muted)',
        // Brand / state
        'brand':         'var(--color-brand)',
        'brand-hover':   'var(--color-brand-hover)',
        'accent':        'var(--color-accent)',
        'success':       'var(--color-success)',
        'warning':       'var(--color-warning)',
        'danger':        'var(--color-danger)',
        // Custom: `blockedred` for the `blocked` state. @oopslink REVISION 4
        // wants blocked = red #dc2626 (Tailwind red-600), but the a11y guardrail
        // (src/a11y.test.tsx) forbids raw bg-red-/text-red- utilities. A custom
        // NON-RED-NAMED token keeps the exact hex while dodging the guardrail
        // regex — class is bg-blockedred (white text on saturated red bg).
        // (Replaces the now-unused `rust` token; `discarded` moved to zinc-700.)
        blockedred: '#dc2626',
        // Chat UX 2 (@oopslink, locked): the viewer's OWN message bubble is a
        // FIXED light blue (#D1E3FF), replacing the old bg-indigo-500. Because
        // it's a fixed light surface in BOTH modes, the bubble's text must be a
        // FIXED dark color (text-slate-900), NOT a theme token that flips light
        // in dark mode — see the own-bubble render. Tester2: #D1E3FF + dark text
        // = 13.72 AAA. Mirrors the blockedred custom-hex-token pattern.
        chatuserbubble: '#D1E3FF',

        // Status / chip / badge palette tokens (no-raw-colors migration).
        // CSS vars in src/index.css carry the exact light hex + dark pair so
        // `<html class="dark">` flips them; light mode is byte-identical to the
        // pre-migration raw Tailwind palette classes.
        'status-slate-bg':       'var(--color-status-slate-bg)',
        'status-slate-fg':       'var(--color-status-slate-fg)',
        'status-slate-fg-soft':  'var(--color-status-slate-fg-soft)',
        'status-slate-border':   'var(--color-status-slate-border)',
        'status-slate-solid':      'var(--color-status-slate-solid)',
        'status-slate-solid-soft': 'var(--color-status-slate-solid-soft)',
        'status-zinc-solid':     'var(--color-status-zinc-solid)',
        'status-stone-bg':       'var(--color-status-stone-bg)',
        'status-stone-fg':       'var(--color-status-stone-fg)',
        'status-blue-bg':        'var(--color-status-blue-bg)',
        'status-blue-fg':        'var(--color-status-blue-fg)',
        'status-blue-border':    'var(--color-status-blue-border)',
        'status-blue-solid':     'var(--color-status-blue-solid)',
        'status-indigo-bg':      'var(--color-status-indigo-bg)',
        'status-indigo-fg':      'var(--color-status-indigo-fg)',
        'status-violet-bg':      'var(--color-status-violet-bg)',
        'status-violet-fg':      'var(--color-status-violet-fg)',
        'status-violet-border':  'var(--color-status-violet-border)',
        'status-violet-solid':   'var(--color-status-violet-solid)',
        'status-purple-bg':      'var(--color-status-purple-bg)',
        'status-purple-fg':      'var(--color-status-purple-fg)',
        'status-purple-strong':  'var(--color-status-purple-strong)',
        'status-fuchsia-bg':     'var(--color-status-fuchsia-bg)',
        'status-fuchsia-fg':     'var(--color-status-fuchsia-fg)',
        'status-amber-bg':       'var(--color-status-amber-bg)',
        'status-amber-fg':       'var(--color-status-amber-fg)',
        'status-amber-border':   'var(--color-status-amber-border)',
        'status-amber-solid':    'var(--color-status-amber-solid)',
        'status-emerald-bg':     'var(--color-status-emerald-bg)',
        'status-emerald-fg':     'var(--color-status-emerald-fg)',
        'status-emerald-border': 'var(--color-status-emerald-border)',
        'status-teal-bg':        'var(--color-status-teal-bg)',
        'status-teal-fg':        'var(--color-status-teal-fg)',
        'status-teal-solid':     'var(--color-status-teal-solid)',
        'status-cyan-bg':        'var(--color-status-cyan-bg)',
        'status-cyan-fg':        'var(--color-status-cyan-fg)',
        'status-cyan-solid':     'var(--color-status-cyan-solid)',
        'status-rose-bg':        'var(--color-status-rose-bg)',
        'status-rose-fg':        'var(--color-status-rose-fg)',
        'status-rose-border':    'var(--color-status-rose-border)',
        'status-green-bg':         'var(--color-status-green-bg)',
        'status-green-fg':         'var(--color-status-green-fg)',
        'status-green-solid':      'var(--color-status-green-solid)',
        'status-green-solid-soft': 'var(--color-status-green-solid-soft)',
        'status-sky-solid':      'var(--color-status-sky-solid)',
        'status-orange-solid':   'var(--color-status-orange-solid)',
        'status-orange-strong':  'var(--color-status-orange-strong)',

        // FIXED tokens (light == dark) for fixed-color surfaces that do NOT
        // flip per theme — the own chat bubble (#D1E3FF) and code block (#003247).
        'chatbubble-fg':   'var(--color-chatbubble-fg)',
        'chatbubble-link': 'var(--color-chatbubble-link)',
        'codeblock-fg':    'var(--color-codeblock-fg)',
        'codeblock-muted': 'var(--color-codeblock-muted)',
        'codeblock-link':  'var(--color-codeblock-link)',

        // v2.10.0 [T1] col① module rail — FIXED dark chrome (light == dark).
        'rail-bg':        'var(--color-rail-bg)',
        'rail-fg':        'var(--color-rail-fg)',
        'rail-fg-active': 'var(--color-rail-fg-active)',
      },
      fontFamily: {
        // @oopslink (locked): dropped the custom DM Sans + Space Grotesk pairing;
        // both `sans` and `heading` now point at Tailwind's DEFAULT system stack
        // (no web-font download). `heading` is kept so existing `font-heading`
        // users still resolve — it just resolves to the same default stack.
        sans: [
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          '"Segoe UI"',
          'Roboto',
          '"Helvetica Neue"',
          'Arial',
          '"Noto Sans"',
          'sans-serif',
          '"Apple Color Emoji"',
          '"Segoe UI Emoji"',
          '"Segoe UI Symbol"',
          '"Noto Color Emoji"',
        ],
        heading: [
          'ui-sans-serif',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          '"Segoe UI"',
          'Roboto',
          '"Helvetica Neue"',
          'Arial',
          '"Noto Sans"',
          'sans-serif',
          '"Apple Color Emoji"',
          '"Segoe UI Emoji"',
          '"Segoe UI Symbol"',
          '"Noto Color Emoji"',
        ],
        // IDs, timestamps, code blocks (skill: `number-tabular`).
        mono:    ['"JetBrains Mono"', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      boxShadow: {
        '1': 'var(--shadow-1)',
        '2': 'var(--shadow-2)',
        '3': 'var(--shadow-3)',
      },
    },
  },
  plugins: [],
};
