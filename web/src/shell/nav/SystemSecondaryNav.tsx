import type React from 'react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router-dom';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';

// ============================================================================
// v2.25 [T716] System — col② secondary nav (registered in
// shell/secondaryNav.tsx). Single localized source for the System module's
// col② nav (Environment / Settings / Version), replacing the previous
// hardcoded-English fallback in AppLayout's buildModuleNavSections `case
// 'system'`. Labels come from the `admin` namespace (`systemNav.*`) so zh
// renders correctly; routes/testids stay stable literals.
//
// The markup mirrors AppLayout's DefaultModuleNav group rendering (CAPS
// collapsible group header + icon rows, same design-token classes) so the
// pixel output is unchanged — only the copy is now localized. The three icons
// are small inline SVGs copied 1:1 from AppLayout's local FleetIcon/
// SettingsIcon/VersionIcon (they are module-private there; duplicating the
// markup keeps this component self-contained and avoids a circular import).
// ============================================================================

const GROUP_STATE_KEY = 'ac.sidebar.groups';
// Stable, language-independent discriminator for the System group's collapse
// state (the visible header label is localized; the persistence key is not).
const SYSTEM_GROUP_ID = 'system';

function readGroupOpen(id: string): boolean {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return true;
    const raw = localStorage.getItem(GROUP_STATE_KEY);
    if (!raw) return true;
    const parsed = JSON.parse(raw) as Record<string, boolean>;
    return parsed[id] === undefined ? true : parsed[id];
  } catch {
    return true;
  }
}

function writeGroupOpen(id: string, open: boolean): void {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.setItem !== 'function') return;
    const raw = localStorage.getItem(GROUP_STATE_KEY);
    const parsed = (raw ? (JSON.parse(raw) as Record<string, boolean>) : {}) ?? {};
    parsed[id] = open;
    localStorage.setItem(GROUP_STATE_KEY, JSON.stringify(parsed));
  } catch {
    // ignore (private mode / SSR)
  }
}

interface SystemRow {
  to: string;
  label: string;
  Icon: () => React.ReactElement;
}

export function SystemSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const { t } = useTranslation('admin');
  const [open, setOpen] = useState(() => readGroupOpen(SYSTEM_GROUP_ID));
  useEffect(() => {
    writeGroupOpen(SYSTEM_GROUP_ID, open);
  }, [open]);

  const rows: SystemRow[] = [
    { to: `${orgBase}/environment`, label: t('systemNav.environment'), Icon: FleetIcon },
    { to: `${orgBase}/settings`, label: t('systemNav.settings'), Icon: SettingsIcon },
    { to: `${orgBase}/version`, label: t('systemNav.version'), Icon: VersionIcon },
  ];

  return (
    <ul className="space-y-4" data-testid="system-secondary-nav">
      <li>
        <h2 className="px-1">
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            data-testid="sidebar-group-toggle-system"
            className="flex w-full items-center justify-between rounded px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
          >
            <span data-testid="section-label">{t('systemNav.system')}</span>
            <span aria-hidden="true" className="text-text-muted">{open ? '⌄' : '›'}</span>
          </button>
        </h2>
        {open && (
          <ul className="space-y-0.5">
            {rows.map((r) => (
              <li key={r.to}>
                <NavLink
                  to={r.to}
                  className={({ isActive }) => [
                    'flex flex-1 items-center justify-between rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                    isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
                  ].join(' ')}
                >
                  <span className="flex items-center gap-2">
                    <span aria-hidden="true" className="inline-flex h-4 w-4"><r.Icon /></span>
                    <span className="flex flex-1 items-center justify-between gap-1.5">
                      <span>{r.label}</span>
                    </span>
                  </span>
                </NavLink>
              </li>
            ))}
          </ul>
        )}
      </li>
    </ul>
  );
}

// Inline icons — mirror AppLayout's module-private FleetIcon/SettingsIcon/
// VersionIcon 1:1 so the System nav looks identical.
function FleetIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><rect x="2.5" y="6" width="6" height="8" rx="1" /><rect x="11.5" y="6" width="6" height="8" rx="1" /><path d="M5.5 9.5h0.01M14.5 9.5h0.01" strokeLinecap="round" /></svg>);
}
function SettingsIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="10" cy="10" r="2.5" /><path d="M10 3v2M10 15v2M3 10h2M15 10h2M5.05 5.05l1.4 1.4M13.55 13.55l1.4 1.4M5.05 14.95l1.4-1.4M13.55 6.45l1.4-1.4" strokeLinecap="round" /></svg>);
}
function VersionIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M3.5 8.5 8.5 3.5a1.5 1.5 0 0 1 2.1 0l5.9 5.9a1.5 1.5 0 0 1 0 2.1l-5 5a1.5 1.5 0 0 1-2.1 0L3.5 10.6V8.5z" strokeLinejoin="round" /><circle cx="7" cy="7" r="1.2" /></svg>);
}
