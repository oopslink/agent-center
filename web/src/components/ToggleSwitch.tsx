// ToggleSwitch — a boolean on/off control as a role="switch" toggle (ux-standards
// §1a bans checkboxes for boolean settings). Mirrors the inline switch markup used
// in ReminderCreateModal, extracted so boolean settings share one accessible
// control. The caller renders its own label/description next to it.
import React from 'react';

export function ToggleSwitch({
  checked,
  onChange,
  ariaLabel,
  testId,
  disabled,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  ariaLabel: string;
  testId?: string;
  disabled?: boolean;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={ariaLabel}
      disabled={disabled}
      onClick={() => onChange(!checked)}
      data-testid={testId}
      className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors disabled:opacity-50 ${
        checked ? 'bg-brand' : 'bg-border-strong'
      }`}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
          checked ? 'translate-x-4' : 'translate-x-0.5'
        }`}
      />
    </button>
  );
}
