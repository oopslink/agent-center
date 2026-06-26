import type React from 'react';

// Inline icon set — satisfies the a11y rule `no-emoji-icons` (no emoji used as
// a UI affordance). Icons are decorative (aria-hidden); the host control
// supplies the accessible name via aria-label / title. SVG style matches
// planDisplay.tsx (viewBox 0 0 24 24, stroke currentColor).

interface IconProps {
  className?: string;
}

export function IconSearch({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="11" cy="11" r="7" />
      <path d="m21 21-4.35-4.35" />
    </svg>
  );
}

export function IconCalendar({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="4" width="18" height="17" rx="2" />
      <path d="M3 9h18M8 2v4M16 2v4" />
    </svg>
  );
}

export function IconClock({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 2" />
    </svg>
  );
}

export function IconPause({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M9 5v14M15 5v14" />
    </svg>
  );
}

export function IconPlay({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="currentColor" stroke="none" aria-hidden="true">
      <path d="M8 5v14l11-7z" />
    </svg>
  );
}

export function IconClose({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M6 6l12 12M18 6 6 18" />
    </svg>
  );
}

// IconEdit — pencil (edit reminder config, T477).
export function IconEdit({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z" />
    </svg>
  );
}

// IconCopy — overlapping sheets (clone reminder, T477).
export function IconCopy({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="9" y="9" width="12" height="12" rx="2" />
      <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
    </svg>
  );
}

// IconTrash — bin (delete reminder entry, T477).
export function IconTrash({ className = 'h-4 w-4' }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" className={className} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 6h18M8 6V4a1 1 0 0 1 1-1h6a1 1 0 0 1 1 1v2m2 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M10 11v6M14 11v6" />
    </svg>
  );
}
