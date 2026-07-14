// Team WebUI — Instantiate template modal. Project-DECOUPLED: only builds the
// team + role identities + seeds memory + binds a workflow; project association
// is deferred to the team detail's 关联项目 tab. Role配比 is editable per slot.
import { useState } from 'react';
import type React from 'react';
import { useInstantiateTeam, type RoleInput, type TeamTemplate } from '@/api/teams';
import { btnGhost, btnPrimary, Card, Field, inputCls, ModalShell, SmallLabel, SpecLine } from './kit';
import { RoleBuilder, totalSlots } from './RoleBuilder';

function templateRoles(t: TeamTemplate): RoleInput[] {
  return t.roles.map((r) => ({
    role: r.role,
    cli: r.cli,
    model: r.model,
    max_concurrency: r.max_concurrency,
    count: r.count,
    tags: r.capability_tags.join(', '),
    description: r.description || '',
  }));
}

export function InstantiateModal({
  template,
  onClose,
  onInstantiated,
}: {
  template: TeamTemplate | null;
  onClose: () => void;
  onInstantiated: (teamId: string) => void;
}): React.ReactElement | null {
  const [name, setName] = useState(() => (template ? `${template.name.toLowerCase().replace(/ /g, '-')}-01` : ''));
  const [roles, setRoles] = useState<RoleInput[]>(() => (template ? templateRoles(template) : []));
  const instantiate = useInstantiateTeam();

  if (!template) return null;
  const total = totalSlots(roles);
  const canSubmit = name.trim().length > 0 && roles.length > 0 && !instantiate.isPending;

  const submit = async () => {
    if (!canSubmit) return;
    try {
      const team = await instantiate.mutateAsync({ template_id: template.id, team_name: name.trim(), roles });
      onClose();
      onInstantiated(team.id);
    } catch {
      /* surfaced via error */
    }
  };

  return (
    <ModalShell
      open
      onClose={onClose}
      testId="instantiate-modal"
      wide
      title="Instantiate template"
      subtitle={
        <>
          把 <b>{template.name}</b> 实例化成一支 team。<b>与 project 无关</b> —— 建 team + 按配比声明角色 slots + seed memory +
          绑 workflow；<b>Phase-1 不铸 agent 身份</b>（运行时另行编入），project 之后在详情「关联项目」里加。
        </>
      }
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              取消
            </button>
            <button type="button" className={btnPrimary} disabled={!canSubmit} data-testid="instantiate-submit" onClick={submit}>
              {instantiate.isPending ? '实例化中…' : 'Instantiate →'}
            </button>
          </div>
        </>
      }
    >
      <Field label="新 team 名称" required>
        <input className={inputCls} value={name} data-testid="instantiate-name" onChange={(e) => setName(e.target.value)} />
      </Field>

      <div className="mb-3 mt-1 flex items-center justify-between gap-3">
        <label className="text-xs font-semibold text-text-secondary">
          角色配比 <span className="font-normal text-text-muted">(从模版带出，可调 count / cli / model / 并发 / tags)</span>
        </label>
        <span className="text-[0.6875rem] text-text-muted" data-testid="instantiate-total">
          Σ {total} slots → 声明 {total} 个角色 slot
        </span>
      </div>
      <RoleBuilder roles={roles} onChange={setRoles} showDescription idPrefix="instantiate" />

      <Card className="mt-3.5 bg-bg-subtle" testId="instantiate-summary">
        <SmallLabel>实例化将执行（与 project 无关）</SmallLabel>
        <SpecLine
          k="声明角色 slots"
          v={`${total} 个 · ${roles.map((r) => `${r.role || '未命名'}×${r.count}`).join(' / ')}`}
        />
        <SpecLine k="建 memory-repo" v="从模版种子 MEMORY.md" />
        <SpecLine k="绑定 workflow" v="team 级 · 不依赖 project" />
        <SpecLine k="agent 身份" v={<span className="text-text-muted">Phase-1 不铸 · 运行时另行编入</span>} />
        <SpecLine k="project 关联" v="后置 → 详情「关联项目」(0..N)" />
      </Card>
      <p className="mt-2 text-[0.6875rem] text-text-muted">
        ＊ 实例化只建 team 与角色声明，<b>不创建 agent 身份</b>（Phase-1 facade 铸 0 agent，成员运行时另行编入）；项目相关的事（seed
        项目事实进 team repo、实际跑 plan）延后到 <span className="font-mono">associate_project</span> 时；workflow 绑定不依赖 project。
      </p>
    </ModalShell>
  );
}
