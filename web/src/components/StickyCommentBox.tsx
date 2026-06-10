import { useEffect, useRef } from 'react';
import type React from 'react';
import { MessageComposer } from './MessageComposer';

interface Props {
  conversationId: string;
  // Reports the box's pixel height so the embedding scroll area can set
  // padding-bottom = height — keeps the latest message from hiding behind the
  // sticky box. Fires on mount and on every resize (e.g. the textarea grows).
  onHeightChange?: (height: number) => void;
}

// StickyCommentBox (5th task — Phabricator-style Issue/Task) pins the shared
// MessageComposer to the bottom of the conversation column (position: sticky)
// so the reply box stays reachable while the message list scrolls. It measures
// its own height via ResizeObserver and reports it, letting the page pad the
// scroll area so the box never occludes the newest message. Focus order is
// preserved — the composer sits after the messages in DOM order, so tabbing
// flows messages → composer naturally.
export function StickyCommentBox({ conversationId, onHeightChange }: Props): React.ReactElement {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el || !onHeightChange) return;
    const report = () => onHeightChange(el.offsetHeight);
    report(); // initial measure
    const ro = new ResizeObserver(report);
    ro.observe(el);
    return () => ro.disconnect();
  }, [onHeightChange]);

  return (
    <div
      ref={ref}
      data-testid="sticky-comment-box"
      className="sticky bottom-0 z-10 border-t border-border-base bg-bg-elevated"
    >
      <MessageComposer conversationId={conversationId} />
    </div>
  );
}
