import type React from 'react';

// Avatar (7th DM redesign / 8th Channel redesign — shared component, built once).
// A name-hashed gradient disc with initials. Discriminators are MULTI-LAYER so
// they never rely on color alone:
//   • shape: human = circle (rounded-full), agent = rounded-square (rounded-lg)
//   • online: a status dot with aria-label + title (not color-only)
// The gradient palette is curated so WHITE initials clear WCAG-AA on every stop
// (saturated 600/700 hues — the lighter green/teal/cyan/amber sit at 700/800).

interface AvatarProps {
  name: string;
  kind?: 'human' | 'agent';
  size?: 'sm' | 'md' | 'lg';
  /** when defined, renders an online/offline status dot. */
  online?: boolean;
}

// Curated gradient pairs — every stop is WHITE-AA (≥4.5:1 white-on-bg).
const PALETTE = [
  'from-blue-600 to-indigo-700',
  'from-indigo-600 to-violet-700',
  'from-violet-600 to-purple-700',
  'from-purple-600 to-fuchsia-700',
  'from-fuchsia-600 to-pink-700',
  'from-pink-600 to-rose-700',
  'from-rose-600 to-red-700',
  'from-red-600 to-orange-700',
  'from-orange-700 to-amber-800',
  'from-emerald-700 to-teal-700',
  'from-teal-700 to-cyan-700',
  'from-cyan-700 to-sky-700',
  'from-sky-700 to-blue-700',
  'from-slate-600 to-slate-800',
];

const SIZE: Record<'sm' | 'md' | 'lg', string> = {
  sm: 'h-6 w-6 text-[0.625rem]',
  md: 'h-8 w-8 text-xs',
  lg: 'h-10 w-10 text-sm',
};

// Deterministic FNV-ish hash → palette index (stable per name across renders).
function paletteFor(name: string): string {
  let h = 0;
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) >>> 0;
  return PALETTE[h % PALETTE.length];
}

function initialsOf(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0][0].toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

export function Avatar({ name, kind = 'human', size = 'md', online }: AvatarProps): React.ReactElement {
  const gradient = paletteFor(name);
  const shape = kind === 'agent' ? 'rounded-lg' : 'rounded-full';
  return (
    <span className="relative inline-flex shrink-0">
      <span
        data-testid="avatar"
        role="img"
        aria-label={kind === 'agent' ? `${name} (agent)` : name}
        className={`inline-flex select-none items-center justify-center bg-gradient-to-br font-semibold text-white ${gradient} ${shape} ${SIZE[size]}`}
      >
        {initialsOf(name)}
      </span>
      {online !== undefined && (
        <span
          data-testid="avatar-status"
          aria-label={online ? 'online' : 'offline'}
          title={online ? 'Online' : 'Offline'}
          className={`absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-bg-elevated ${
            online ? 'bg-green-500' : 'bg-slate-400'
          }`}
        />
      )}
    </span>
  );
}
