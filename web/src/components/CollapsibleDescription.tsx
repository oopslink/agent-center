import type React from 'react';
import { useId, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownMessage } from './MarkdownMessage';

// T179 (@oopslink) — the Task/Issue detail description, collapsible. A long
// description default-collapses to a few lines + a "Show more" toggle so it does
// NOT push the conversation/chat region off-screen (the mobile pain: a wall of
// description squeezed the chat to a sliver). Expanding reveals the full text in
// a height-capped, internally-scrolling region (the prior behavior), so even a
// very long description never reclaims the whole viewport.
//
// Collapse is decided by RAW content size (line count OR char length), NOT a
// measured pixel height — deterministic and unit-testable in jsdom (which has no
// layout, so scrollHeight is always 0). The full markdown is always rendered and
// merely CSS-clipped when collapsed, so slicing never breaks markdown structure.
//
// a11y mirrors CollapsibleCodeBlock: the toggle is a SIBLING button (never nests
// interactive), aria-expanded + aria-controls → the region's useId() id, and the
// region keeps role="region" + aria-label; tabIndex is 0 only when it actually
// scrolls (expanded / non-collapsible), -1 when clipped (nothing to scroll).
export interface CollapsibleDescriptionProps {
  /** the markdown description text (non-empty; callers render their own empty state). */
  content: string;
  /** data-testid for the content region (e.g. "task-description" / "issue-description"). */
  testId: string;
  /** a11y label for the region (e.g. "Task description"). */
  ariaLabel: string;
  /** collapse when the content exceeds this many lines (default 8). */
  collapsedThreshold?: number;
  /** collapse when the content exceeds this many chars — catches long single-line prose (default 400). */
  maxChars?: number;
}

export function CollapsibleDescription({
  content,
  testId,
  ariaLabel,
  collapsedThreshold = 8,
  maxChars = 400,
}: CollapsibleDescriptionProps): React.ReactElement {
  const { t } = useTranslation('work');
  const collapsible = content.split('\n').length > collapsedThreshold || content.length > maxChars;
  const [expanded, setExpanded] = useState(false);
  const regionId = useId();
  const collapsed = collapsible && !expanded;

  return (
    <div className="mt-4">
      <div
        id={regionId}
        data-testid={testId}
        role="region"
        aria-label={ariaLabel}
        tabIndex={collapsed ? -1 : 0}
        className={
          collapsed
            ? 'max-h-20 overflow-hidden text-sm text-text-secondary'
            : 'max-h-64 overflow-y-auto text-sm text-text-secondary'
        }
      >
        <MarkdownMessage content={content} />
      </div>
      {collapsible && (
        <button
          type="button"
          className="mt-1 text-xs font-medium text-accent hover:underline"
          data-testid={`${testId}-toggle`}
          aria-expanded={expanded}
          aria-controls={regionId}
          onClick={() => setExpanded((e) => !e)}
        >
          {expanded ? t('widgets.collapsible.showLess') : t('widgets.collapsible.showMore')}
        </button>
      )}
    </div>
  );
}
