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
      },
      fontFamily: {
        // Body (skill: `font-pairing` Tech Startup pair — recommended
        // for SaaS / developer tools / AI products).
        sans:    ['"DM Sans"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
        // Display / brand
        heading: ['"Space Grotesk"', 'ui-sans-serif', 'system-ui', 'sans-serif'],
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
