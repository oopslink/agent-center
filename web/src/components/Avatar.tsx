import type React from 'react';
import { useTranslation } from 'react-i18next';

// Avatar (7th DM redesign / 8th Channel redesign — shared component, built once).
// A name-hashed gradient disc with initials. Discriminators are MULTI-LAYER so
// they never rely on color alone:
//   • shape: human = circle (rounded-full), agent = rounded-square (rounded-lg)
//   • online: a status dot with aria-label + title (not color-only)
// The gradient palette is INDIGO-family only (v2.8.1 7th-bubbles, matching the
// @oopslink indigo chat accent), curated so WHITE initials clear WCAG-AA on every
// stop (all stops are indigo/violet-500 or darker — indigo-500 white = 4.47:1).

interface AvatarProps {
  name: string;
  kind?: 'human' | 'agent';
  size?: 'sm' | 'md' | 'lg';
  /** when defined, renders an online/offline status dot. */
  online?: boolean;
}

// v2.8.1 7th-bubbles: INDIGO-family gradients only (matches the @oopslink-locked
// indigo chat accent). A small set of indigo/violet variations; every stop is
// WHITE-AA (≥4.5:1 white-on-bg — indigo-500/violet-500 is the lightest stop used;
// indigo-400 was excluded because white-on-indigo-400 is ~2.7:1 = FAIL). `paletteFor`
// hashes the name → a stable variation within the indigo family.
const PALETTE = [
  'from-indigo-500 to-indigo-700',
  'from-indigo-600 to-violet-700',
  'from-violet-500 to-indigo-700',
  'from-violet-600 to-indigo-800',
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
  const { t } = useTranslation('common');
  const gradient = paletteFor(name);
  const shape = kind === 'agent' ? 'rounded-lg' : 'rounded-full';
  return (
    <span className="relative inline-flex shrink-0">
      <span
        data-testid="avatar"
        role="img"
        aria-label={kind === 'agent' ? t('avatar.agentLabel', { name }) : name}
        className={`inline-flex select-none items-center justify-center bg-gradient-to-br font-semibold text-white ${gradient} ${shape} ${SIZE[size]}`}
      >
        {initialsOf(name)}
      </span>
      {online !== undefined && (
        <span
          data-testid="avatar-status"
          aria-label={online ? t('avatar.online') : t('avatar.offline')}
          title={online ? t('avatar.onlineTitle') : t('avatar.offlineTitle')}
          className={`absolute -bottom-0.5 -right-0.5 h-2.5 w-2.5 rounded-full border-2 border-bg-elevated ${
            online ? 'bg-status-green-solid-soft' : 'bg-status-slate-solid-soft'
          }`}
        />
      )}
    </span>
  );
}
