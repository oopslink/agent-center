// Team WebUI — shared presentational primitives (design language from the v7
// mockup, built on the console's semantic tokens; all icons are line-SVG per the
// no-emoji-icons a11y gate).
import type React from 'react';
import { roleColor, type RoleView } from '@/api/teams';

/** Inline style tinting a role chip's text with its role color (data-driven). */
export function roleColorChip(role: string): React.CSSProperties {
  return { color: roleColor(role) };
}

// ---------------------------------------------------------------------------
// Role proportion bar + legend
// ---------------------------------------------------------------------------

function slotCount(r: RoleView): number {
  return r.count ?? 1;
}

export function RoleBar({
  roles,
  className,
  testId,
}: {
  roles: RoleView[];
  className?: string;
  testId?: string;
}): React.ReactElement {
  const total = roles.reduce((s, r) => s + slotCount(r), 0) || 1;
  return (
    <div
      data-testid={testId}
      className={['flex h-2 overflow-hidden rounded border border-border-base bg-bg-subtle', className].filter(Boolean).join(' ')}
    >
      {roles.map((r) => (
        <span
          key={r.role}
          style={{ width: `${(slotCount(r) / total) * 100}%`, background: roleColor(r.role) }}
          title={`${r.role} ×${slotCount(r)}`}
        />
      ))}
    </div>
  );
}

export function RoleLegend({ roles }: { roles: RoleView[] }): React.ReactElement {
  return (
    <div className="mt-1.5 flex flex-wrap gap-x-2.5 gap-y-1">
      {roles.map((r) => (
        <span key={r.role} className="flex items-center gap-1 text-[0.65rem] text-text-muted">
          <span className="h-2 w-2 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
          {r.role}×{slotCount(r)}
        </span>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Status + kind chips
// ---------------------------------------------------------------------------

export function StatusChip({ status }: { status: 'active' | 'draft' }): React.ReactElement {
  const active = status === 'active';
  return (
    <span
      data-testid={`team-status-${status}`}
      className={[
        'inline-flex items-center gap-1 rounded px-2 py-0.5 text-[0.65rem] font-semibold',
        active
          ? 'bg-success/15 text-success'
          : 'border border-border-strong bg-bg-subtle text-text-muted',
      ].join(' ')}
    >
      {active ? 'active' : 'draft'}
    </span>
  );
}

export function KindTag({ kind }: { kind: 'agent' | 'human' }): React.ReactElement {
  return (
    <span
      className={[
        'inline-flex rounded px-1.5 py-0.5 text-[0.625rem] font-semibold',
        kind === 'agent' ? 'bg-brand/10 text-brand' : 'bg-status-blue-bg text-status-blue-fg',
      ].join(' ')}
    >
      {kind}
    </span>
  );
}

export function MetaPill({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <span className="rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.65rem] text-text-muted">
      {children}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Avatar glyph
// ---------------------------------------------------------------------------

export function Glyph({
  text,
  size = 'md',
  kind = 'brand',
}: {
  text: string;
  size?: 'sm' | 'md' | 'lg';
  kind?: 'brand' | 'agent' | 'human';
}): React.ReactElement {
  const dim = size === 'lg' ? 'h-12 w-12 text-lg' : size === 'sm' ? 'h-8 w-8 text-xs' : 'h-9 w-9 text-sm';
  const tone =
    kind === 'agent'
      ? 'bg-brand/10 text-brand border border-brand/25'
      : kind === 'human'
        ? 'bg-status-blue-bg text-status-blue-fg border border-status-blue-border'
        : 'bg-brand text-white';
  return (
    <span className={`inline-grid ${dim} place-items-center rounded-lg font-semibold ${tone}`} aria-hidden="true">
      {text}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Line-SVG icons
// ---------------------------------------------------------------------------

type IconProps = { className?: string };
const base = (className?: string) =>
  ['stroke-current', className || 'h-4 w-4'].join(' ');

export function TeamsIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="9" y="3" width="6" height="5" rx="1.2" />
      <rect x="3" y="15" width="6" height="5" rx="1.2" />
      <rect x="15" y="15" width="6" height="5" rx="1.2" />
      <path d="M12 8v3M6 15v-2h12v2" />
    </svg>
  );
}

export function GridIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
    </svg>
  );
}

export function TemplateIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3l8 4.5-8 4.5-8-4.5z" />
      <path d="M4 12l8 4.5 8-4.5M4 16.5L12 21l8-4.5" />
    </svg>
  );
}

export function DirectoryIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="8" r="3.2" />
      <path d="M5 20c0-3.5 3-6 7-6s7 2.5 7 6" />
    </svg>
  );
}

export function CloseIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M6 6l12 12M18 6L6 18" />
    </svg>
  );
}

export function PlusIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}

export function CheckIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M5 12l4.5 4.5L19 7" />
    </svg>
  );
}

export function WarnIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 4 2.5 20h19z" />
      <path d="M12 10v4M12 17.4v.01" />
    </svg>
  );
}

export function InfoIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="M12 11v5M12 8v.01" />
    </svg>
  );
}

export function ArrowRightIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M5 12h14M13 6l6 6-6 6" />
    </svg>
  );
}

export function ExtractIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3l8 4.5v9L12 21l-8-4.5v-9z" />
      <path d="M12 12v9M12 12L4 7.5M12 12l8-4.5" />
    </svg>
  );
}

export function ImportIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3v12M7 10l5 5 5-5" />
      <path d="M4 21h16" />
    </svg>
  );
}

export function ExportIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 15V3M7 8l5-5 5 5" />
      <path d="M4 21h16" />
    </svg>
  );
}

export function PinIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M12 3l2.2 4.6 5 .7-3.6 3.5.9 5-4.5-2.4L7.5 20l.9-5L4.8 8.3l5-.7z" />
    </svg>
  );
}

export function DocIcon({ className }: IconProps): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className={base(className)} strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M6 3h8l4 4v14H6z" />
      <path d="M14 3v4h4M9 12h6M9 16h6" />
    </svg>
  );
}
