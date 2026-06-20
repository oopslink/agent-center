import tsParser from '@typescript-eslint/parser';

// Narrow, lint-enforceable red-line suite (mechanism > memory — #259 §5 /
// #168 a11y institutionalization). The web console has no general lint suite;
// these rules exist solely to PREVENT REGRESSIONS of specific UX/a11y red lines:
//
//   1. #169 — no native browser dialogs (window.confirm/alert/prompt); use
//      ConfirmModal. Native dialogs are blocking, unstyled, inaccessible.
//   2. #270/#271 — agent lifecycle action buttons must render an ICON component,
//      never raw text. #250 icon-ified Stop/Restart/Reset/Message but left Start
//      as text "Start"; that inconsistency is the "icon reverts to text" report
//      (#271). The rule below flags any <button data-testid="agent-*-btn"> with a
//      direct non-whitespace text child. The lone legitimate progress text
//      (`{lc}…`) is a <span>, not a button, so it is naturally exempt.
//   3. ux-standards §1a — no <input type="checkbox"> as the control for an
//      entity multi-pick (use EntityMultiSelect: dropdown + chips) or a boolean
//      setting (use a role="switch" toggle). The reminder modal shipped a
//      pill-grid remindee + a raw skip-overlap checkbox; this rule prevents the
//      regression. Two legitimate checkbox idioms are allowlisted by data-testid:
//      data-table row-selection (agents-select-all / agent-select-checkbox, #232)
//      and destructive-action acknowledgment (agent-reset-confirm).
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

// Same rationale for jsx-a11y: the source carries a few `// eslint-disable-next-line
// jsx-a11y/...` directives (e.g. an intentional onClick on a test wrapper div).
// This config does NOT lint a11y, but ESLint hard-errors on a disable directive
// that references an unknown rule ("Definition for rule '...' was not found"),
// which left the spa-eslint gate baseline-red on main (gate-health bug, NOT a
// code violation). Register the referenced jsx-a11y rules as no-op stubs so the
// directives stay valid without pulling in a11y linting (out of scope here).
const jsxA11yStub = {
  rules: {
    'no-static-element-interactions': noop,
    'click-events-have-key-events': noop,
  },
};

export default [
  {
    files: ['src/**/*.{ts,tsx}'],
    // Legacy react-hooks disable directives are no-ops here (see stub above);
    // don't flag them as "unused" — this config isn't the hooks linter.
    linterOptions: { reportUnusedDisableDirectives: 'off' },
    plugins: { 'react-hooks': reactHooksStub, 'jsx-a11y': jsxA11yStub },
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
      // #270/#271: agent action buttons must be icon-component, not text. Flags a
      // <button data-testid="agent-*-btn"> with a direct non-whitespace JSXText
      // child (e.g. the old text `Start`). Icon buttons (a child JSXElement like
      // <PlayIcon/>) pass; the `{lc}…` progress note is a <span>, not a button.
      'no-restricted-syntax': [
        'error',
        {
          selector:
            'JSXElement[openingElement.name.name="button"]:has(JSXAttribute[name.name="data-testid"][value.value=/^agent-[a-z]+-btn$/]):has(JSXText[value=/\\S/])',
          message:
            'Agent action buttons (data-testid="agent-*-btn") must render an icon component, not text (#270/#271). Wrap the glyph in an <Icon/> component with title + aria-label; never inline a text label.',
        },
        {
          // ux-standards §1a: a checkbox-typed <input> is banned except for the
          // two allowlisted idioms (table row-select, destructive-confirm ack).
          selector:
            'JSXOpeningElement[name.name="input"]:has(JSXAttribute[name.name="type"][value.value="checkbox"]):not(:has(JSXAttribute[name.name="data-testid"][value.value=/^(agents-select-all|agent-select-checkbox|agent-reset-confirm)$/]))',
          message:
            'A checkbox is banned for entity multi-pick or a boolean setting (ux-standards §1a). Use EntityMultiSelect (dropdown + chips) for multi-pick, or a role="switch" toggle for a boolean. The only allowlisted checkboxes are table row-selection and destructive-action acknowledgment.',
        },
      ],
    },
  },
];
