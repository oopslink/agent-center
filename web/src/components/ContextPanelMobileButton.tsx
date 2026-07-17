import type React from 'react';
import { useTranslation } from 'react-i18next';

// ============================================================================
// The mobile ⓘ entry point for the col④ Context Panel.
//
// On mobile the shell hosts the panel inside a bottom sheet that starts CLOSED,
// so a page that mounts a <ContextPanel> there has no way to reveal it on its
// own — the content registers (the sheet is its portal host) but stays
// unreachable until something calls the sheet's open trigger. Pages pair this
// button with their <ContextPanel> and wire onClick to
// useContextPanelMobileTrigger().open.
//
// Presentational only: the page owns the trigger (and the gate deciding when a
// panel exists), so the button can't drift out of sync with the panel's own
// mount condition.
// ============================================================================

export function ContextPanelMobileButton({ onClick }: { onClick: () => void }): React.ReactElement {
  const { t } = useTranslation();
  const label = t('shell.contextPanel.openMobileSheet');
  return (
    <button
      type="button"
      data-testid="context-panel-mobile-open"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="inline-flex min-h-[2.75rem] min-w-[2.75rem] shrink-0 items-center justify-center rounded-full text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
    >
      <svg
        viewBox="0 0 20 20"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        className="h-5 w-5"
        aria-hidden="true"
      >
        <circle cx="10" cy="10" r="7.25" />
        <path strokeLinecap="round" d="M10 9.25v4" />
        <path strokeLinecap="round" d="M10 6.5v.75" />
      </svg>
    </button>
  );
}
