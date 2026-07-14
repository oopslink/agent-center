// Team detail (/organizations/:slug/teams/:teamId) — 4 tabs:
// Overview / Members / 关联项目 / Team Memory. Header carries the Extract →
// Template entry (one of the two extract entry points; the other is the
// Templates page). Members enforces the agent-exclusivity migration confirm.
import { useState } from 'react';
import type React from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useOptionalOrgContext } from '@/OrgContext';
import {
  useAssociateProject,
  useDisassociateProject,
  useRemoveMember,
  useTeam,
  useTeamMemoryIndex,
  useTeamMembers,
  useTeamProjects,
} from '@/api/teams';
import { useProjects } from '@/api/projects';
import { ConfirmModal } from '@/components/ConfirmModal';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { AddMemberModal } from '@/components/teams/AddMemberModal';
import { ExtractModal } from '@/components/teams/ExtractModal';
import { MemoryPane } from '@/components/teams/MemoryPane';
import {
  btnGhost,
  btnSm,
  btnSmDanger,
  btnSmPrimary,
  Card,
  Field,
  inputCls,
  ModalShell,
  Note,
  SectionHead,
  SpecLine,
  Tabs,
} from '@/components/teams/kit';
import {
  ExtractIcon,
  Glyph,
  KindTag,
  RoleBar,
  RoleLegend,
  StatusChip,
  roleColorChip,
} from '@/components/teams/teamsUi';
import { roleColor, type TeamView } from '@/api/teams';

const TABS = [
  { key: 'ov', label: 'Overview' },
  { key: 'mm', label: 'Members' },
  { key: 'pj', label: '关联项目' },
  { key: 'tm', label: 'Team Memory' },
] as const;
type TabKey = (typeof TABS)[number]['key'];

export default function TeamDetail(): React.ReactElement {
  const { teamId = '' } = useParams();
  const team = useTeam(teamId);
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [tab, setTab] = useState<TabKey>('ov');
  const [extracting, setExtracting] = useState(false);

  if (team.isLoading) {
    return (
      <section className="space-y-4" data-testid="page-TeamDetail">
        <Skeleton height="5rem" />
        <Skeleton height="16rem" />
      </section>
    );
  }
  if (team.isError || !team.data) {
    return (
      <section data-testid="page-TeamDetail">
        <button type="button" className={btnGhost} onClick={() => navigate(`${orgBase}/teams`)}>
          ← Teams
        </button>
        <p className="mt-4 text-sm text-danger" data-testid="team-detail-error">
          {(team.error as Error)?.message ?? 'team_not_found'}
        </p>
      </section>
    );
  }

  const t = team.data;
  const roleNames = t.roles.map((r) => r.role);

  return (
    <section className="space-y-2" data-testid="page-TeamDetail">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Glyph text={t.glyph} size="lg" />
          <div>
            <div className="flex items-center gap-3">
              <h1 className="font-heading text-xl font-semibold text-text-primary">{t.name}</h1>
              <StatusChip status={t.status} />
            </div>
            <div className="mt-1.5 flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-text-muted">
              <span className="font-mono">{t.id}</span>
              <span>{t.members_count} members · {t.roles.length} roles</span>
              <span>{t.projects_count} projects</span>
              <span>created {t.created}</span>
            </div>
          </div>
        </div>
        <button type="button" className={btnGhost} data-testid="team-extract" onClick={() => setExtracting(true)}>
          <ExtractIcon className="h-4 w-4" /> Extract → Template
        </button>
      </div>

      <Tabs tabs={TABS} active={tab} onChange={setTab} testId="team-tabs" />

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === 'ov' && <OverviewPane team={t} />}
        {tab === 'mm' && <MembersPane teamId={t.id} roleOptions={roleNames} team={t} />}
        {/* panes below */}
        {tab === 'pj' && <ProjectsPane teamId={t.id} />}
        {tab === 'tm' && <MemoryPane teamId={t.id} heading="⌗ team-memory" />}
      </div>

      {extracting && (
        <ExtractModal
          team={t}
          onClose={() => setExtracting(false)}
          onSaved={() => navigate(`${orgBase}/teams/templates`)}
        />
      )}
    </section>
  );
}

function OverviewPane({ team: t }: { team: TeamView }): React.ReactElement {
  // Only the structural facts (roles / members / projects) and the team-memory
  // index are truthful data sources in Phase-1. Live task/agent telemetry has no
  // team-scoped facade aggregate yet — show an honest "—" placeholder rather than
  // a fabricated constant (a fresh empty team must not read as "3 running tasks").
  const memory = useTeamMemoryIndex(t.id);
  const memoryEntries = memory.data?.length;
  const NA = <span className="text-text-muted">—</span>;
  return (
    <div className="grid gap-3.5 md:grid-cols-2">
      <Card>
        <SectionHead title="角色配比" hint="声明式 slots" />
        <RoleBar roles={t.roles} className="w-full" />
        <RoleLegend roles={t.roles} />
        <div className="mt-3.5">
          {t.roles.map((r) => (
            <SpecLine
              key={r.role}
              k={
                <span className="flex items-center gap-1.5">
                  <span className="h-2 w-2 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
                  {r.role} ×{r.count ?? 1}
                </span>
              }
              v={`${r.cli} · ${r.model} · conc ${r.max_concurrency}`}
            />
          ))}
        </div>
      </Card>
      <Card>
        <SectionHead title="团队概况" hint="结构 + team-memory" />
        <SpecLine k="已编入成员" v={`${t.members_count}`} />
        <SpecLine k="声明角色" v={`${t.roles.length}`} />
        <SpecLine k="关联项目" v={`${t.projects_count}`} />
        <SpecLine
          k="team-memory"
          v={memory.isLoading ? NA : memoryEntries != null ? `${memoryEntries} entries` : NA}
        />
        <SpecLine k="运行中任务" v={NA} />
        <SpecLine k="阻塞任务" v={NA} />
        <Note testId="team-health-note">
          运行中 / 阻塞任务、成员在线状态为实时运行遥测，Phase-1 尚未接入 facade 聚合，接入后填真值（暂显 —）。
        </Note>
      </Card>
    </div>
  );
}

function MembersPane({
  teamId,
  roleOptions,
  team,
}: {
  teamId: string;
  roleOptions: string[];
  team: TeamView;
}): React.ReactElement {
  const members = useTeamMembers(teamId);
  const remove = useRemoveMember();
  const [adding, setAdding] = useState(false);
  const [removingRef, setRemovingRef] = useState<string | null>(null);

  return (
    <div>
      <SectionHead
        title="Members"
        action={
          <button type="button" className={btnSmPrimary} data-testid="members-add" onClick={() => setAdding(true)}>
            + Add member
          </button>
        }
      />
      <Note testId="members-exclusivity-note">
        <b>独占规则：</b>agent 同一时刻只属于一个 team（add 时若已在别处 → 二次确认迁移）；human 可同时多 team。
      </Note>

      {members.isLoading && <Skeleton height="8rem" />}
      {members.isSuccess && members.data.length === 0 && (
        <EmptyState title="还没有成员" body="点 + Add member 把 agent / human 编入声明角色。" testId="members-empty" />
      )}
      {members.isSuccess && members.data.length > 0 && (
        <div className="overflow-hidden rounded-lg border border-border-base">
          <table className="w-full text-sm" data-testid="members-table">
            <thead>
              <tr className="border-b border-border-base text-left text-[0.6875rem] uppercase tracking-wide text-text-muted">
                <th className="px-4 py-3 font-semibold">成员</th>
                <th className="px-4 py-3 font-semibold">类型</th>
                <th className="px-4 py-3 font-semibold">声明角色</th>
                <th className="px-4 py-3 font-semibold">CLI / Model</th>
                <th className="px-4 py-3 font-semibold">Tags</th>
                <th className="px-4 py-3 font-semibold">并发</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {members.data.map((m) => (
                <tr key={m.member_ref} data-testid={`member-row-${m.member_ref}`} className="border-b border-border-base last:border-0">
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2.5">
                      <Glyph text={m.name[0]?.toUpperCase() ?? '?'} size="sm" kind={m.kind} />
                      <div>
                        <div className="font-semibold text-text-primary">{m.name}</div>
                        <div className="font-mono text-[0.6875rem] text-text-muted">{m.member_ref}</div>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-3">
                    <KindTag kind={m.kind} />
                    {m.exclusive && <span className="ml-1.5 text-[0.625rem] font-semibold text-warning">独占</span>}
                  </td>
                  <td className="px-4 py-3">
                    <span className="rounded border border-border-base bg-bg-subtle px-2 py-0.5 text-xs font-semibold" style={roleColorChip(m.role)}>
                      {m.role}
                    </span>
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">
                    {m.cli} · {m.model}
                  </td>
                  <td className="px-4 py-3">
                    {m.tags.length ? (
                      m.tags.map((tag) => (
                        <span key={tag} className="mr-1 inline-block rounded border border-border-base bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] text-text-secondary">
                          {tag}
                        </span>
                      ))
                    ) : (
                      <span className="text-text-muted">—</span>
                    )}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-text-muted">{m.concurrency}</td>
                  <td className="px-4 py-3 text-right">
                    <button type="button" className={btnSmDanger} data-testid={`member-remove-${m.member_ref}`} onClick={() => setRemovingRef(m.member_ref)}>
                      Remove
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {adding && (
        <AddMemberModal team={team} roleOptions={roleOptions} onClose={() => setAdding(false)} onAdded={() => setAdding(false)} />
      )}
      <ConfirmModal
        open={removingRef !== null}
        title="移除成员"
        message={`确认把 ${removingRef} 移出本 team？`}
        confirmLabel="移除"
        danger
        busy={remove.isPending}
        onCancel={() => setRemovingRef(null)}
        onConfirm={async () => {
          if (removingRef) await remove.mutateAsync({ team_id: teamId, member_ref: removingRef });
          setRemovingRef(null);
        }}
      />
    </div>
  );
}

function ProjectsPane({ teamId }: { teamId: string }): React.ReactElement {
  const projects = useTeamProjects(teamId);
  const disassociate = useDisassociateProject();
  const [unlinkId, setUnlinkId] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);

  return (
    <div>
      <SectionHead
        title="关联项目"
        action={
          <button
            type="button"
            className={btnSm}
            data-testid="associate-project"
            onClick={() => setPicking(true)}
          >
            + Associate project
          </button>
        }
      />
      {projects.isLoading && <Skeleton height="6rem" />}
      {projects.isSuccess && projects.data.length === 0 && (
        <EmptyState title="未关联项目" body="关联项目后，该 team 成员可被派发该项目任务。" testId="projects-empty" />
      )}
      {(projects.data ?? []).map((p) => (
        <div key={p.project_id} data-testid={`assoc-${p.project_id}`} className="mb-2.5 flex items-center gap-3 rounded-lg border border-border-base bg-bg-elevated px-3.5 py-3 shadow-1">
          <Glyph text={p.glyph} kind="human" />
          <div className="flex-1">
            <b className="font-semibold text-text-primary">{p.name}</b>
            <span className="block font-mono text-[0.6875rem] text-text-muted">
              {p.project_id} · {p.repo}
            </span>
          </div>
          <span
            className={[
              'rounded px-2 py-0.5 text-[0.65rem] font-semibold',
              p.relation === 'primary' ? 'bg-success/15 text-success' : 'border border-border-base bg-bg-subtle text-text-muted',
            ].join(' ')}
          >
            {p.relation}
          </span>
          <button type="button" className={btnSm} data-testid={`unlink-${p.project_id}`} onClick={() => setUnlinkId(p.project_id)}>
            解绑
          </button>
        </div>
      ))}
      {projects.isSuccess && projects.data.length > 0 && (
        <Note>关联项目后，该 team 成员可被派发该项目任务；team-memory 在项目上下文可见。</Note>
      )}
      <ConfirmModal
        open={unlinkId !== null}
        title="解绑项目"
        message="确认解绑该项目？成员将不再被派发其任务。"
        confirmLabel="解绑"
        danger
        busy={disassociate.isPending}
        onCancel={() => setUnlinkId(null)}
        onConfirm={async () => {
          if (unlinkId) await disassociate.mutateAsync({ team_id: teamId, project_id: unlinkId });
          setUnlinkId(null);
        }}
      />
      {picking && (
        <ProjectPickerModal
          teamId={teamId}
          linkedIds={new Set((projects.data ?? []).map((p) => p.project_id))}
          onClose={() => setPicking(false)}
        />
      )}
    </div>
  );
}

// Real associate-project picker: lists the org's active projects (GET /projects)
// minus the ones already linked to this team, and associates the SELECTED project
// with its true {project_id, name} — no fabricated `project-N` / 'new-project'.
function ProjectPickerModal({
  teamId,
  linkedIds,
  onClose,
}: {
  teamId: string;
  linkedIds: Set<string>;
  onClose: () => void;
}): React.ReactElement {
  const all = useProjects();
  const associate = useAssociateProject();
  const candidates = (all.data ?? []).filter((p) => !linkedIds.has(p.id));
  const [selected, setSelected] = useState('');
  const chosen = candidates.find((p) => p.id === selected);

  const submit = async () => {
    if (!chosen) return;
    try {
      await associate.mutateAsync({ team_id: teamId, project_id: chosen.id, name: chosen.name });
      onClose();
    } catch {
      /* surfaced via error */
    }
  };

  return (
    <ModalShell
      open
      onClose={onClose}
      testId="associate-project-modal"
      title="关联项目"
      subtitle="从组织的项目里选一个关联到本 team；成员即可被派发该项目任务。"
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              取消
            </button>
            <button
              type="button"
              className={btnSmPrimary}
              data-testid="associate-project-submit"
              disabled={!chosen || associate.isPending}
              onClick={submit}
            >
              {associate.isPending ? '关联中…' : '关联'}
            </button>
          </div>
        </>
      }
    >
      {all.isLoading && <Skeleton height="4rem" />}
      {all.isSuccess && candidates.length === 0 && (
        <EmptyState
          title="没有可关联的项目"
          body="组织下的活跃项目都已关联，或还没有项目。"
          testId="associate-project-empty"
        />
      )}
      {candidates.length > 0 && (
        <Field label="选择项目" required>
          <select
            className={inputCls}
            value={selected}
            data-testid="associate-project-select"
            onChange={(e) => setSelected(e.target.value)}
          >
            <option value="">— 选择项目 —</option>
            {candidates.map((p) => (
              <option key={p.id} value={p.id}>
                {p.name}（{p.id}）
              </option>
            ))}
          </select>
        </Field>
      )}
      {associate.isError && <p className="mt-2 text-xs text-danger">{(associate.error as Error).message}</p>}
    </ModalShell>
  );
}
