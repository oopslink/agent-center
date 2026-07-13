// Teams directory — Agents (/organizations/:slug/teams/agents). The full agent
// list with a TEAMS column and a by-team filter, per the v7 mockup's DIRECTORY
// section. Large lists live in the main area (search + status/team filter),
// not the narrow rail. Phase-1 team membership comes from fixtures.
import { useMemo, useState } from 'react';
import type React from 'react';
import { useDirectoryAgents, useTeams } from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { Note } from '@/components/teams/kit';
import { Glyph } from '@/components/teams/teamsUi';

type StatusFilter = 'all' | 'working' | 'idle';

export default function TeamsDirectoryAgents(): React.ReactElement {
  const agents = useDirectoryAgents();
  const teams = useTeams();
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState<StatusFilter>('all');
  const [team, setTeam] = useState('all');

  const all = agents.data ?? [];
  const rows = useMemo(() => {
    const q = query.trim().toLowerCase();
    return all.filter((a) => {
      if (status !== 'all' && a.status !== status) return false;
      if (team !== 'all' && !a.teams.includes(team)) return false;
      if (q && !(a.name.toLowerCase().includes(q) || a.role.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [all, query, status, team]);

  const workingCount = all.filter((a) => a.status === 'working').length;

  return (
    <section className="space-y-4" data-testid="page-TeamsDirectoryAgents">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">Agents</h1>
          <p className="mt-1 font-mono text-xs text-text-muted">/organizations/:slug/teams/agents</p>
        </div>
        <span className="rounded-full border border-success/40 bg-success/10 px-2.5 py-1 text-[0.65rem] font-semibold text-success">
          {workingCount} working · {all.length} total
        </span>
      </header>

      <div className="flex flex-wrap items-center gap-3">
        <input
          className="w-72 rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus-visible:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/40"
          placeholder="搜索 agent（名称 / 角色）…"
          value={query}
          data-testid="agents-search"
          onChange={(e) => setQuery(e.target.value)}
        />
        <div className="ml-auto flex flex-wrap gap-2">
          {(['all', 'working', 'idle'] as const).map((s) => (
            <FilterChip key={s} on={status === s} testId={`agents-filter-${s}`} onClick={() => setStatus(s)}>
              {s === 'all' ? `All ${all.length}` : s === 'working' ? `Working ${workingCount}` : `Idle ${all.length - workingCount}`}
            </FilterChip>
          ))}
          <select
            className="rounded border border-border-base bg-bg-elevated px-2.5 py-1.5 text-xs font-semibold text-text-secondary focus-visible:border-accent focus-visible:outline-none"
            value={team}
            data-testid="agents-team-filter"
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

      {agents.isLoading && <Skeleton height="12rem" />}
      {agents.isSuccess && rows.length === 0 && <EmptyState title="无匹配 agent" body="换个搜索或筛选试试。" testId="agents-empty" />}
      {agents.isSuccess && rows.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-border-base">
          <table className="w-full text-sm" data-testid="agents-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="px-4 py-3 font-semibold">Agent</th>
                <th className="px-4 py-3 font-semibold">Status</th>
                <th className="px-4 py-3 font-semibold">Teams</th>
                <th className="px-4 py-3 font-semibold">Model</th>
                <th className="px-4 py-3 font-semibold">Load</th>
                <th className="px-4 py-3 font-semibold">Backlog</th>
                <th className="px-4 py-3 font-semibold">Last active</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((a) => (
                <tr key={a.name} data-testid={`agent-row-${a.name}`} className="border-b border-border-base last:border-0 hover:bg-bg-subtle">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2.5">
                      <Glyph text={a.name.replace('agent-center-', '').slice(0, 2).toUpperCase()} size="sm" kind="agent" />
                      <div>
                        <div className="font-semibold text-text-primary">{a.name}</div>
                        <div className="text-[0.6875rem] text-text-muted">{a.role}</div>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <span className={['inline-flex items-center gap-1.5 font-semibold', a.status === 'working' ? 'text-status-blue-fg' : 'text-success'].join(' ')}>
                      <span className={['h-1.5 w-1.5 rounded-full', a.status === 'working' ? 'bg-status-blue-solid' : 'bg-success'].join(' ')} aria-hidden="true" />
                      {a.status === 'working' ? 'Working' : 'Idle'}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    <TeamsCell teams={a.teams} />
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{a.model}</td>
                  <td className="px-4 py-3">
                    <span className="relative inline-block h-1.5 w-14 overflow-hidden rounded border border-border-base bg-bg-subtle align-middle">
                      <span className="absolute inset-y-0 left-0 rounded bg-status-blue-solid" style={{ width: `${Math.round(a.load * 100)}%` }} />
                    </span>
                    <span className="ml-1.5 font-mono text-[0.6875rem] text-text-muted">{a.load.toFixed(1)}</span>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{a.backlog}</td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{a.last}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Note>
        大列表放在主区（可搜索 / 状态·team 筛选），不铺在窄侧栏 —— 解决「human/agent 变多第二侧栏太挤」。侧栏只留入口。
      </Note>
    </section>
  );
}

export function TeamsCell({ teams }: { teams: string[] }): React.ReactElement {
  if (teams.length === 0) return <span className="text-text-muted">未编入</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {teams.map((t) => (
        <span key={t} className="rounded bg-success/15 px-2 py-0.5 text-[0.65rem] font-semibold text-success">
          {t}
        </span>
      ))}
    </span>
  );
}

export function FilterChip({
  on,
  onClick,
  children,
  testId,
}: {
  on: boolean;
  onClick: () => void;
  children: React.ReactNode;
  testId?: string;
}): React.ReactElement {
  return (
    <button
      type="button"
      data-testid={testId}
      onClick={onClick}
      className={[
        'rounded border px-3 py-1.5 text-xs font-semibold motion-safe:transition-colors',
        on ? 'border-accent bg-brand/10 text-brand-hover' : 'border-border-base bg-bg-elevated text-text-muted hover:border-border-strong hover:text-text-primary',
      ].join(' ')}
    >
      {children}
    </button>
  );
}
