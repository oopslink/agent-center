import type React from 'react';
import { useTranslation } from 'react-i18next';
import { NavLink } from 'react-router-dom';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';

interface NavEntry {
  id: 'overview' | 'agents' | 'projects' | 'executions' | 'collaboration';
  to: string;
  label: string;
  Icon: () => React.ReactElement;
  end?: boolean;
}

export function InsightSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const { t } = useTranslation('insights');
  const rows: NavEntry[] = [
    { id: 'overview', to: `${orgBase}/insights/overview`, label: t('insight.nav.overview'), Icon: OverviewIcon, end: true },
    { id: 'agents', to: `${orgBase}/insights/agents`, label: t('insight.nav.agents'), Icon: AgentIcon, end: true },
    { id: 'projects', to: `${orgBase}/insights/projects`, label: t('insight.nav.projects'), Icon: ProjectIcon, end: true },
    { id: 'collaboration', to: `${orgBase}/insights/collaboration`, label: t('insight.nav.collaboration'), Icon: CollaborationIcon, end: true },
    { id: 'executions', to: `${orgBase}/insights/executions?window=24h`, label: t('insight.nav.executions'), Icon: ExecutionIcon },
  ];

  return (
    <div data-testid="insight-secondary-nav">
      <h3 className="px-2 pb-1 pt-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
        <span data-testid="section-label">{t('insight.nav.section')}</span>
      </h3>
      <ul className="space-y-0.5">
        {rows.map((row) => (
          <li key={row.id}>
            <NavLink
              to={row.to}
              end={row.end}
              data-testid={`insight-nav-${row.id}`}
              className={({ isActive }) =>
                [
                  'flex items-center gap-2 rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                  isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
                ].join(' ')
              }
            >
              <span aria-hidden="true" className="inline-flex h-4 w-4">
                <row.Icon />
              </span>
              <span>{row.label}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </div>
  );
}

function CollaborationIcon(): React.ReactElement {
  return <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="5" cy="5" r="2"/><rect x="12" y="3" width="5" height="4" rx=".5"/><circle cx="5" cy="15" r="2"/><path d="M7 5h5M7 14.5l5-7"/></svg>;
}

function OverviewIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4 5.5h12M4 10h8M4 14.5h10" strokeLinecap="round" />
    </svg>
  );
}

function AgentIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="6.5" r="2.5" />
      <path d="M5 16c.7-3 2.5-4.5 5-4.5s4.3 1.5 5 4.5" strokeLinecap="round" />
    </svg>
  );
}

function ProjectIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h3l1.5 2h6.5A1.5 1.5 0 0 1 17 8.5v6A1.5 1.5 0 0 1 15.5 16h-11A1.5 1.5 0 0 1 3 14.5v-8z" strokeLinejoin="round" />
    </svg>
  );
}

function ExecutionIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M5 4.5h7l3 3v8A1.5 1.5 0 0 1 13.5 17h-7A1.5 1.5 0 0 1 5 15.5v-11z" strokeLinejoin="round" />
      <path d="M12 4.5V8h3M7.5 11h5M7.5 14h4" strokeLinecap="round" />
    </svg>
  );
}
