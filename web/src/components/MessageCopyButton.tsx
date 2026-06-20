import type React from 'react';
import { useState } from 'react';

// T246 — per-message "copy" affordance. Copies the message's raw content (the
// markdown source the sender wrote) to the clipboard, with a brief "Copied"
// confirmation: an icon swap plus an aria-live="polite" SR status — mirroring the
// CollapsibleCodeBlock copy idiom. Icon-only chrome (no-emoji-icons a11y rule);
// the accessible name rides on aria-label/title. Holds its own per-instance
// `copied` state so each message row's button is independent.
export function MessageCopyButton({ content }: { content: string }): React.ReactElement {
  const [copied, setCopied] = useState(false);

  const copy = () => {
    void navigator.clipboard.writeText(content);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 2000);
  };

  return (
    <span className="flex items-center gap-1">
      <span
        className="text-[0.625rem] font-normal text-text-muted"
        data-testid="message-copy-status"
        aria-live="polite"
      >
        {copied ? 'Copied' : ''}
      </span>
      <button
        type="button"
        onClick={copy}
        aria-label="Copy message"
        title="Copy message"
        data-testid="message-copy-btn"
        className="rounded p-0.5 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent motion-safe:transition-colors"
      >
        {copied ? <CheckIcon /> : <CopyIcon />}
      </button>
    </span>
  );
}

// Icon-only copy affordance (swaps to a check on success), sized for the small
// message header line. SVG glyphs (no emoji icons per the a11y guardrail).
function CopyIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="7" y="7" width="9" height="9" rx="1.5" strokeLinejoin="round" />
      <path d="M13 4.5H5.5A1.5 1.5 0 0 0 4 6v7.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function CheckIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="2" aria-hidden="true">
      <path d="M4.5 10.5l3.5 3.5 7.5-8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
