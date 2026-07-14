// Teams list (/organizations/:slug/teams) — the Team BC landing surface.
// Console-style table: role配比 bar + members/projects/status/created. Row →
// team detail. Header "+ New Team" opens the role-builder create flow.
//
// Phase-1 data comes from the fixture-backed teams API (see src/api/teams.ts) —
// the real Team backend lives on the worker-token /admin/agent-tools RPC surface
// and is not browser-reachable yet.
import { useState } from 'react';
import type React from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useOptionalOrgContext } from '@/OrgContext';
import { useTeams } from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { NewTeamModal } from '@/components/teams/NewTeamModal';
import { btnPrimary } from '@/components/teams/kit';
import { PlusIcon, RoleBar, RoleLegend, StatusChip } from '@/components/teams/teamsUi';

export default function Teams(): React.ReactElement {
  const { t } = useTranslation('teams');
  const teams = useTeams();
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [creating, setCreating] = useState(false);

  const openTeam = (id: string) => navigate(`${orgBase}/teams/${id}`);

  return (
    <section className="space-y-4" data-testid="page-Teams">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('teamsList.title')}</h1>
          <p className="mt-1 font-mono text-xs text-text-muted">{orgBase || '/organizations/:slug'}/teams</p>
        </div>
        <button type="button" className={btnPrimary} data-testid="teams-new" onClick={() => setCreating(true)}>
          <PlusIcon className="h-4 w-4" /> {t('teamsList.newTeam')}
        </button>
      </header>

      {teams.isLoading && (
        <div className="space-y-2">
          <Skeleton height="4rem" />
          <Skeleton height="4rem" />
          <Skeleton height="4rem" />
        </div>
      )}

      {teams.isError && (
        <p className="text-sm text-danger" data-testid="teams-error">
          {(teams.error as Error).message}
        </p>
      )}

      {teams.isSuccess && teams.data.length === 0 && (
        <EmptyState
          title={t('teamsList.empty.title')}
          body={t('teamsList.empty.body')}
          action={{ label: t('teamsList.empty.action'), onClick: () => setCreating(true) }}
          testId="teams-empty"
        />
      )}

      {teams.isSuccess && teams.data.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-border-base">
          <table className="w-full text-sm" data-testid="teams-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="px-4 py-3 font-semibold">{t('teamsList.col.team')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamsList.col.roles')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamsList.col.members')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamsList.col.projects')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamsList.col.status')}</th>
                <th className="px-4 py-3 font-semibold">{t('teamsList.col.created')}</th>
              </tr>
            </thead>
            <tbody>
              {teams.data.map((team) => (
                <tr
                  key={team.id}
                  data-testid={`team-row-${team.id}`}
                  className="cursor-pointer border-b border-border-base last:border-0 hover:bg-bg-subtle"
                  onClick={() => openTeam(team.id)}
                >
                  <td className="px-4 py-3.5">
                    <div className="font-semibold text-text-primary">{team.name}</div>
                    <div className="mt-0.5 font-mono text-[0.6875rem] text-text-muted">{team.id}</div>
                  </td>
                  <td className="px-4 py-3.5">
                    <div className="w-40">
                      <RoleBar roles={team.roles} className="w-40" />
                      <RoleLegend roles={team.roles} />
                    </div>
                  </td>
                  <td className="px-4 py-3.5 text-text-primary">{team.members_count}</td>
                  <td className="px-4 py-3.5 text-text-primary">{team.projects_count}</td>
                  <td className="px-4 py-3.5">
                    <StatusChip status={team.status} />
                  </td>
                  <td className="px-4 py-3.5 font-mono text-xs text-text-muted">{team.created}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <NewTeamModal open={creating} onClose={() => setCreating(false)} onCreated={openTeam} />
    </section>
  );
}
