import type React from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink, useLocation } from 'react-router-dom';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';
import {
  DirectoryIcon,
  GridIcon,
  TeamsIcon,
  TemplateIcon,
} from '@/components/teams/teamsUi';

// Team WebUI (Phase-1) — col② secondary nav (registered override).
//
// Two groups mirroring the v7 mockup's context column:
//   • TEAMS      — All teams (/teams), Templates (/teams/templates)
//   • DIRECTORY  — Agents (/teams/agents), Humans (/teams/humans)
// These directory pages are the mockup's DIRECTORY section (crumbs
// /teams/agents · /teams/humans) — distinct from the org-level /agents and
// /members/humans pages, so the existing Members module is untouched.

interface NavEntry {
  id: string;
  to: string;
  labelKey: string;
  Icon: (p: { className?: string }) => React.ReactElement;
  end?: boolean;
}

export default function TeamUISecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const { t } = useTranslation('common');
  const location = useLocation();
  const p = (path: string) => `${orgBase}/${path}`;

  const teamsGroup: NavEntry[] = [
    { id: 'teams', to: p('teams'), labelKey: 'shell.teamui.allTeams', Icon: GridIcon, end: true },
    { id: 'templates', to: p('teams/templates'), labelKey: 'shell.teamui.templates', Icon: TemplateIcon },
  ];
  const directoryGroup: NavEntry[] = [
    { id: 'agents', to: p('teams/agents'), labelKey: 'shell.teamui.agents', Icon: DirectoryIcon },
    { id: 'humans', to: p('teams/humans'), labelKey: 'shell.teamui.humans', Icon: DirectoryIcon },
  ];

  // "All teams" must not stay highlighted on /teams/templates|agents|humans.
  const onTeamsList = new RegExp(`${orgBase}/teams/?$`).test(location.pathname);

  return (
    <div data-testid="teamui-nav">
      <Group label={t('nav.teamui')} icon={<TeamsIcon className="h-3.5 w-3.5" />}>
        {teamsGroup.map((e) => (
          <Item key={e.to} entry={e} label={t(e.labelKey)} forceActive={e.end ? onTeamsList : undefined} />
        ))}
      </Group>
      <Group label={t('shell.teamui.directory')}>
        {directoryGroup.map((e) => (
          <Item key={e.to} entry={e} label={t(e.labelKey)} />
        ))}
      </Group>
    </div>
  );
}

function Group({ label, icon, children }: { label: string; icon?: React.ReactNode; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="mb-2">
      <h3 className="flex items-center gap-1.5 px-2 pb-1 pt-3 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
        {icon && <span aria-hidden="true">{icon}</span>}
        <span data-testid="section-label">{label}</span>
      </h3>
      <ul className="space-y-0.5">{children}</ul>
    </div>
  );
}

function Item({
  entry,
  label,
  forceActive,
}: {
  entry: NavEntry;
  label: string;
  forceActive?: boolean;
}): React.ReactElement {
  const cls = (active: boolean) =>
    [
      'flex items-center gap-2 rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
      active ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
    ].join(' ');
  return (
    <li>
      <NavLink
        to={entry.to}
        end={entry.end}
        data-testid={`teamui-nav-${entry.id}`}
        className={({ isActive }) => cls(forceActive ?? isActive)}
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4">
          <entry.Icon className="h-4 w-4" />
        </span>
        <span>{label}</span>
      </NavLink>
    </li>
  );
}
