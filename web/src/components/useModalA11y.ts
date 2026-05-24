import { useEffect, useRef } from 'react';

// useModalA11y attaches Escape-to-close + Tab focus-trap on the
// returned `containerRef`. Mount the hook in every modal that renders
// a `role="dialog"` shell — closes the WCAG 2.1 Level A focus-trap
// gap surfaced by the v1+v2 self-audit (slock task #9).
//
// Usage:
//   const containerRef = useModalA11y({ open, onClose });
//   if (!open) return null;
//   return <div ref={containerRef} role="dialog" ...>...</div>;
//
// Behavior:
//   - Escape (when open) calls onClose. No-op when not open.
//   - Tab / Shift+Tab cycles through focusable descendants of the
//     container, never letting focus escape to the page behind.
//   - On mount the previously-focused element is remembered;
//     unmount restores focus there so the user lands back where they
//     were (e.g. the "New channel" button that opened the modal).
export function useModalA11y({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const previouslyFocused = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;
    previouslyFocused.current = document.activeElement as HTMLElement | null;

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onClose();
        return;
      }
      if (e.key !== 'Tab') return;
      const container = containerRef.current;
      if (!container) return;
      const focusables = container.querySelectorAll<HTMLElement>(
        'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      if (focusables.length === 0) return;
      const first = focusables[0];
      const last = focusables[focusables.length - 1];
      const active = document.activeElement as HTMLElement | null;
      if (e.shiftKey) {
        if (active === first || !container.contains(active)) {
          e.preventDefault();
          last.focus();
        }
      } else {
        if (active === last || !container.contains(active)) {
          e.preventDefault();
          first.focus();
        }
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      // Restore focus to the trigger that opened the modal.
      previouslyFocused.current?.focus?.();
    };
  }, [open, onClose]);

  return containerRef;
}
