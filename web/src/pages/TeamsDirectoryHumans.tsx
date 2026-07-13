// Teams directory — Humans (/organizations/:slug/teams/humans). The full human
// list with a TEAMS column (a human may belong to MANY teams — unlike an agent's
// single-team exclusivity) and a by-team filter, per the v7 mockup.
import { useMemo, useState } from 'react';
import type React from 'react';
import { useDirectoryHumans, useTeams } from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { Note } from '@/components/teams/kit';
import { FilterChip, TeamsCell } from './TeamsDirectoryAgents';

type StatusFilter = 'all' | 'joined';

export default function TeamsDirectoryHumans(): React.ReactElement {
  const humans = useDirectoryHumans();
  const teams = useTeams();
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [team, setTeam] = useState('all');

  const all = humans.data ?? [];
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return all.filter((h) => {
      if (status === 'joined' && h.status !== 'Joined') return false;
      if (team !== 'all' && !h.teams.includes(team)) return false;
      if (q && !(h.name.toLowerCase().includes(q) || h.email.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [all, query, status, team]);

  const joinedCount = all.filter((h) => h.status === 'Joined').length;

  return (
    <section className="space-y-4" data-testid="page-TeamsDirectoryHumans">
      <header>
        <h1 className="font-heading text-2xl font-semibold text-text-primary">Humans</h1>
        <p className="mt-1 font-mono text-xs text-text-muted">/organizations/:slug/teams/humans</p>
      </header>

      <div className="flex flex-wrap items-center gap-3">
        <input
          className="w-72 rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
          placeholder="搜索 human（名称 / email）…"
          value={query}
          data-testid="humans-search"
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="ml-auto flex flex-wrap gap-2">
          <FilterChip on={status === 'all'} testId="humans-filter-all" onClick={() => setStatus('all')}>
            All {all.length}
          </FilterChip>
          <FilterChip on={status === 'joined'} testId="humans-filter-joined" onClick={() => setStatus('joined')}>
            Joined {joinedCount}
          </FilterChip>
          <select
            className="rounded border border-border-base bg-bg-elevated px-2.5 py-1.5 text-xs font-semibold text-text-secondary focus-visible:border-accent focus-visible:outline-none"
            value={team}
            data-testid="humans-team-filter"
            onChange={(e) => setTeam(e.target.value)}
          >
            <option value="all">按 team：全部</option>
            {(teams.data ?? []).map((t) => (
              <option key={t.id} value={t.name}>
                {t.name}
              </option>
            ))}
          </select>
        </div>
      </div>

      {humans.isLoading && <Skeleton height="12rem" />}
      {humans.isSuccess && rows.length === 0 && <EmptyState title="无匹配 human" body="换个搜索或筛选试试。" testId="humans-empty" />}
      {humans.isSuccess && rows.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-border-base">
          <table className="w-full text-sm" data-testid="humans-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="px-4 py-3 font-semibold">Identity</th>
                <th className="px-4 py-3 font-semibold">Role</th>
                <th className="px-4 py-3 font-semibold">Status</th>
                <th className="px-4 py-3 font-semibold">Teams</th>
                <th className="px-4 py-3 font-semibold">Email</th>
                <th className="px-4 py-3 font-semibold">Created</th>
                <th className="px-4 py-3 font-semibold">Last active</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((h) => (
                <tr key={h.name} data-testid={`human-row-${h.name}`} className="border-b border-border-base last:border-0 hover:bg-bg-subtle">
                  <td className="px-4 py-3 font-semibold text-text-primary">{h.name}</td>
                  <td className="px-4 py-3 font-semibold text-brand-hover">{h.role}</td>
                  <td className="px-4 py-3">
                    <span className={h.status === 'Joined' ? 'font-semibold text-success' : 'font-semibold text-text-muted'}>{h.status}</span>
                  </td>
                  <td className="px-4 py-3">
                    <TeamsCell teams={h.teams} />
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{h.email}</td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{h.created}</td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{h.last}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Note>
        human 可属于<b>多个 team</b>（与 agent 独占单 team 不同）—— TEAMS 列展示全部所属 team，顶部可<b>按 team 过滤</b>。
      </Note>
    </section>
  );
}
