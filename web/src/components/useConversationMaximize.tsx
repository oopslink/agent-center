import type React from 'react';
import { useEffect, useState } from 'react';

// Shared "maximize" behavior for embedded conversation surfaces (DM / channel
// mobile tabs, and any other chatbox that wants a full-viewport escape).
//
// Maximizing promotes the chatbox to a fixed inset-0 overlay — vital on mobile
// where an inline chat shares a long detail page. While maximized the body
// scroll is locked and Esc restores. WorkItemConversation (#T206) implemented
// this inline first; this hook is the reusable distillation so DM/channel
// (and task/issue) share one implementation.
export interface ConversationMaximize {
  maximized: boolean;
  toggle: () => void;
  setMaximized: (v: boolean) => void;
}

export function useConversationMaximize(): ConversationMaximize {
  const [maximized, setMaximized] = useState(false);

  useEffect(() => {
    if (!maximized) return;
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') setMaximized(false);
    };
    window.addEventListener('keydown', onKey);
    return () => {
      document.body.style.overflow = prevOverflow;
      window.removeEventListener('keydown', onKey);
    };
  }, [maximized]);

  return { maximized, setMaximized, toggle: () => setMaximized((m) => !m) };
}

// Maximize / restore glyphs — single-stroke SVGs (no-emoji UX rule), matching
// the icon style used elsewhere. Maximize = corner arrows pushing outward;
// restore = corner arrows pulling inward.
export function MaximizeIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M8 4H4v4M16 8V4h-4M4 12v4h4M12 16h4v-4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

export function RestoreIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M4 8h4V4M12 4v4h4M8 16v-4H4M16 12h-4v4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// The maximize/restore toggle button — a compact rounded control. Used both as
// a floating affordance and inline on a control row.
export function MaximizeToggle({
  maximized,
  onToggle,
  className,
  testId = 'conversation-maximize-toggle',
}: {
  maximized: boolean;
  onToggle: () => void;
  className?: string;
  testId?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={onToggle}
      className={className}
      data-testid={testId}
      aria-pressed={maximized}
      aria-label={maximized ? 'Restore conversation' : 'Maximize conversation'}
      title={maximized ? 'Restore (Esc)' : 'Maximize'}
    >
      {maximized ? <RestoreIcon /> : <MaximizeIcon />}
    </button>
  );
}
