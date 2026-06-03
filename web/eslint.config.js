import tsParser from '@typescript-eslint/parser';

// Minimal, intentionally narrow ESLint config (task #169).
//
// The web console has no general lint suite — this config exists solely to
// PREVENT REGRESSIONS of native browser dialogs. window.confirm / alert /
// prompt are blocking, unstyled, and inaccessible; all confirmation UX must
// use ConfirmModal (or an equivalent modal/toast). Both the bare-global form
// (`confirm(...)`) and the member form (`window.confirm(...)`) are banned.
//
// Run with `pnpm lint`.
const banned = [
  { name: 'confirm', replacement: 'ConfirmModal' },
  { name: 'alert', replacement: 'a modal/toast' },
  { name: 'prompt', replacement: 'a modal input field' },
];

const message = (name, replacement) =>
  `Native window.${name}() is banned (#169) — use ${replacement} instead. ` +
  'Native dialogs are blocking, unstyled, and inaccessible.';

// The source carries a few legacy `// eslint-disable-next-line
// react-hooks/exhaustive-deps` directives from a previous lint setup. This
// config does NOT lint React hooks, but ESLint errors on disable directives
// that reference an unknown rule. Register react-hooks as no-op rules so those
// directives stay valid without pulling in hooks linting (out of scope for #169).
const noop = { create: () => ({}) };
const reactHooksStub = {
  rules: { 'exhaustive-deps': noop, 'rules-of-hooks': noop },
};

export default [
  {
    files: ['src/**/*.{ts,tsx}'],
    // Legacy react-hooks disable directives are no-ops here (see stub above);
    // don't flag them as "unused" — this config isn't the hooks linter.
    linterOptions: { reportUnusedDisableDirectives: 'off' },
    plugins: { 'react-hooks': reactHooksStub },
    languageOptions: {
      parser: tsParser,
      ecmaVersion: 'latest',
      sourceType: 'module',
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    rules: {
      'no-restricted-globals': [
        'error',
        ...banned.map(({ name, replacement }) => ({ name, message: message(name, replacement) })),
      ],
      'no-restricted-properties': [
        'error',
        ...banned.map(({ name, replacement }) => ({
          object: 'window',
          property: name,
          message: message(name, replacement),
        })),
      ],
    },
  },
];
