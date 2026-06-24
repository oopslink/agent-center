import type React from 'react';
import { NavLink } from 'react-router-dom';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';

// ============================================================================
// I41 (T470) Organization Settings — col② secondary nav.
//
// The five Org Settings sections (Profile / Humans / Agents / Invitations /
// Danger Zone) live in the shell's REAL second sidebar (col②), 1:1 with the
// existing IA (Members / Reminders …) — NOT as a page-internal card-nav inside
// col③ (the form the owner rejected). Org Settings is reached via the org
// switcher gear (it is not a top-level rail module), so the shell renders this
// nav whenever the route is /organizations/:slug/organization-settings/*.
//
// Each row routes to organization-settings/<section>; the OrganizationSettings
// page reads the :section param and renders the matching panel in col③.
// ============================================================================

export const ORG_SETTINGS_SECTIONS: ReadonlyArray<{ slug: string; label: string }> = [
  { slug: 'profile', label: 'Profile' },
  { slug: 'humans', label: 'Humans' },
  { slug: 'agents', label: 'Agents' },
  { slug: 'invitations', label: 'Invitations' },
  { slug: 'danger', label: 'Danger Zone' },
];

export function OrgSettingsSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  return (
    <ul className="space-y-0.5" data-testid="org-settings-secondary-nav">
      {ORG_SETTINGS_SECTIONS.map((s) => (
        <li key={s.slug}>
          <NavLink
            to={`${orgBase}/organization-settings/${s.slug}`}
            className={({ isActive }) =>
              [
                'block rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
              ].join(' ')
            }
            data-testid={`org-settings-nav-${s.slug}`}
          >
            {s.label}
          </NavLink>
        </li>
      ))}
    </ul>
  );
}

export default OrgSettingsSecondaryNav;
