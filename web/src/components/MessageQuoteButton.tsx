import type React from 'react';
import { useTranslation } from 'react-i18next';

// 引用 — per-message "quote" affordance. Lives in the message header line next to
// MessageCopyButton (same hover chrome). Clicking queues this message as the
// composer's quote target. Icon-only chrome (no-emoji-icons a11y rule); the
// accessible name rides on aria-label/title. Stateless — the queued target is
// owned by the QuoteContext, so this is a thin button that just forwards clicks.
export function MessageQuoteButton({ onQuote }: { onQuote: () => void }): React.ReactElement {
  const { t } = useTranslation('chat');
  return (
    <button
      type="button"
      onClick={onQuote}
      aria-label={t('message.quoteMessage')}
      title={t('message.quoteMessage')}
      data-testid="message-quote-btn"
      className="rounded p-0.5 text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent motion-safe:transition-colors"
    >
      <QuoteIcon />
    </button>
  );
}

// Icon-only quote affordance, sized for the small message header line. A single-
// stroke speech-bubble-with-quote-mark glyph (no emoji icons per the a11y
// guardrail).
function QuoteIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path
        d="M4 15.5V6.5A1.5 1.5 0 0 1 5.5 5h9A1.5 1.5 0 0 1 16 6.5v6A1.5 1.5 0 0 1 14.5 14H7l-3 1.5z"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path d="M7.5 8.5v2M10.5 8.5v2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
