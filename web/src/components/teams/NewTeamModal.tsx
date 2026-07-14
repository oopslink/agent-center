// Team WebUI — New Team modal. Declares the role配比 via the shared RoleBuilder;
// creating builds agent identities + a team-memory repo (Phase-1: fixtures).
import { useState } from 'react';
import type React from 'react';
import { useCreateTeam, type RoleInput } from '@/api/teams';
import { btnGhost, btnPrimary, Field, inputCls, ModalShell } from './kit';
import { newRole, RoleBuilder, totalSlots } from './RoleBuilder';

export function NewTeamModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (teamId: string) => void;
}): React.ReactElement | null {
  const [name, setName] = useState('');
  const [visibility, setVisibility] = useState('org-private');
  const [description, setDescription] = useState('');
  const [roles, setRoles] = useState<RoleInput[]>([newRole('planner'), { ...newRole('coder'), count: 2 }]);
  const create = useCreateTeam();

  const canSubmit = name.trim().length > 0 && roles.length > 0 && !create.isPending;

  const submit = async () => {
    if (!canSubmit) return;
    try {
      const team = await create.mutateAsync({ name: name.trim(), description, visibility, roles });
      onClose();
      onCreated(team.id);
    } catch {
      /* surfaced via create.error */
    }
  };

  if (!open) return null;
  return (
    <ModalShell
      open={open}
      onClose={onClose}
      testId="new-team-modal"
      wide
      title="New Team"
      subtitle="声明角色配比 —— 每角色单独配 CLI / model / tags / 并发。创建即声明角色 slots 与 memory-repo（Phase-1 不铸 agent 身份，运行时另行编入）。"
      footer={
        <>
          <span className="text-[0.6875rem] text-text-muted">创建后 → 声明角色 slots + team-memory repo（agent 运行时编入）</span>
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              取消
            </button>
            <button type="button" className={btnPrimary} disabled={!canSubmit} data-testid="new-team-submit" onClick={submit}>
              {create.isPending ? '创建中…' : '创建 Team'}
            </button>
          </div>
        </>
      }
    >
      <div className="grid grid-cols-2 gap-3">
        <Field label="Team name" required>
          <input
            className={inputCls}
            value={name}
            placeholder="e.g. payments-squad"
            data-testid="new-team-name"
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field label="可见性">
          <select className={inputCls} value={visibility} data-testid="new-team-visibility" onChange={(e) => setVisibility(e.target.value)}>
            <option value="org-private">org-private</option>
            <option value="project-scoped">project-scoped</option>
          </select>
        </Field>
      </div>
      <Field label="Description">
        <textarea
          className={inputCls}
          rows={2}
          value={description}
          placeholder="这个编队负责什么…"
          data-testid="new-team-desc"
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>

      <div className="mb-3 mt-5 flex items-center justify-between">
        <label className="text-xs font-semibold text-text-secondary">
          角色配比 <span className="text-accent">*</span>
        </label>
        <span className="text-[0.6875rem] text-text-muted">Σ {totalSlots(roles)} slots = 派生 {totalSlots(roles)} 个 agent</span>
      </div>
      <RoleBuilder roles={roles} onChange={setRoles} idPrefix="new-team" />

      {create.isError && (
        <p className="mt-3 text-xs text-danger" data-testid="new-team-error">
          {(create.error as Error).message}
        </p>
      )}
    </ModalShell>
  );
}
