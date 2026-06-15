import type React from 'react';
import type { ResizablePanel } from './useResizablePanel';

// ResizeHandle — the thin draggable grip rendered on a panel edge. Presentational
// only: it spreads the handleProps from useResizablePanel and renders the hit
// area + a centered hairline. Shared by the ThreadSidebar and ParticipantsPanel so
// the affordance (col-resize cursor, hover/active highlight, keyboard separator)
// is identical. Solid tokens only (border-strong / accent) — alpha tints on the
// hex CSS-var tokens render transparent (see RoleBadge note in ParticipantsPanel).
interface Props {
  /** which edge of the panel the handle sits on. */
  edge: 'left' | 'right';
  /** the handle event handlers from useResizablePanel(). */
  handleProps: ResizablePanel['handleProps'];
  /** true while dragging — lights the grip. */
  resizing: boolean;
  /** accessible label, e.g. "Resize thread panel". */
  ariaLabel: string;
  testId?: string;
}

export function ResizeHandle({
  edge,
  handleProps,
  resizing,
  ariaLabel,
  testId = 'resize-handle',
}: Props): React.ReactElement {
  return (
    <div
      {...handleProps}
      role="separator"
      aria-orientation="vertical"
      aria-label={ariaLabel}
      tabIndex={0}
      data-testid={testId}
      data-resizing={resizing ? 'true' : 'false'}
      className={[
        'group absolute inset-y-0 z-20 w-2 cursor-col-resize touch-none select-none',
        edge === 'left' ? '-left-1' : '-right-1',
        'focus-visible:outline-none',
      ].join(' ')}
    >
      {/* Centered hairline: border-strong at rest, accent on hover/focus/drag. */}
      <span
        aria-hidden="true"
        className={[
          'pointer-events-none absolute inset-y-0 left-1/2 w-0.5 -translate-x-1/2',
          'transition-colors group-hover:bg-accent group-focus-visible:bg-accent',
          resizing ? 'bg-accent' : 'bg-border-strong',
        ].join(' ')}
      />
    </div>
  );
}
