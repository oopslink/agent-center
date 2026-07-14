// Team WebUI — Add member modal (+ the agent-exclusivity migration confirm).
// An agent belongs to exactly one team: adding one that's already elsewhere
// triggers a second-confirm migration. A human may join many teams freely.
import { useState } from 'react';
import type React from 'react';
import { useAddMember, type TeamView } from '@/api/teams';
import { btnGhost, btnPrimary, Field, inputCls, ModalShell, SpecLine } from './kit';
import { WarnIcon } from './teamsUi';

type Kind = 'agent' | 'human';

const AGENT_OPTIONS = [
  { value: 'free', ref: 'agent:04c1…', name: 'coder-04', label: 'coder-04 · claude-code · sonnet-5（空闲）', busy: false },
  { value: 'busy', ref: 'agent:09d2…', name: 'coder-09', label: 'coder-09 · 已在 growth-experiments（占用中）', busy: true, from: 'growth-experiments' },
];

export function AddMemberModal({
  team,
  roleOptions,
  onClose,
  onAdded,
}: {
  team: TeamView;
  roleOptions: string[];
  onClose: () => void;
  onAdded: () => void;
}): React.ReactElement {
  const [kind, setKind] = useState<Kind>('agent');
  const [agentValue, setAgentValue] = useState('free');
  const [role, setRole] = useState(roleOptions[0] ?? 'coder');
  const [migrating, setMigrating] = useState(false);
  const add = useAddMember();

  const selected = AGENT_OPTIONS.find((a) => a.value === agentValue) ?? AGENT_OPTIONS[0];

  const doAdd = async (opts?: { migrateFrom?: string }) => {
    try {
      await add.mutateAsync({
        team_id: team.id,
        member_ref: kind === 'agent' ? selected.ref : 'user:new',
        name: kind === 'agent' ? selected.name : 'new-human',
        kind,
        role,
        migrateFrom: opts?.migrateFrom,
      });
      onClose();
      onAdded();
    } catch {
      /* surfaced via error */
    }
  };

  const onSubmit = () => {
    if (kind === 'agent' && selected.busy) {
      setMigrating(true);
      return;
    }
    void doAdd();
  };

  if (migrating) {
    return (
      <ModalShell
        open
        onClose={() => setMigrating(false)}
        testId="migrate-modal"
        title={
          <span className="flex items-center gap-2 text-warning">
            <WarnIcon className="h-5 w-5" /> 确认迁移
          </span>
        }
        subtitle={
          <>
            <b>{selected.name}</b> 目前是 <b>{selected.from}</b> 的独占成员。
          </>
        }
        footer={
          <>
            <span />
            <div className="flex gap-2.5">
              <button type="button" className={btnGhost} onClick={() => setMigrating(false)}>
                返回
              </button>
              <button
                type="button"
                className="inline-flex items-center gap-1.5 rounded bg-warning px-3.5 py-2 text-sm font-semibold text-white hover:opacity-90"
                data-testid="migrate-confirm"
                onClick={() => doAdd({ migrateFrom: selected.from })}
              >
                确认迁移
              </button>
            </div>
          </>
        }
      >
        <div className="mb-4 flex gap-2.5 rounded-lg border border-warning/40 bg-warning/5 px-3.5 py-3 text-xs text-text-secondary">
          <span className="mt-0.5 text-warning">
            <WarnIcon className="h-4 w-4" />
          </span>
          <div>
            agent 独占单 team。加入本 team 会
            <b className="font-semibold text-warning"> 从 {selected.from} 迁出 </b>
            该 agent，其在原 team 的运行中任务需先收尾/转派。
          </div>
        </div>
        <div className="rounded-lg border border-border-base bg-bg-subtle p-4">
          <SpecLine k="迁出" v={selected.from} />
          <SpecLine k="迁入" v={`${team.name} · ${role}`} />
          <SpecLine k="原 team 运行中任务" v={<span className="text-warning">2 → 需转派</span>} />
        </div>
      </ModalShell>
    );
  }

  return (
    <ModalShell
      open
      onClose={onClose}
      testId="add-member-modal"
      title="Add member"
      subtitle="把 agent 或 human 编入声明角色。"
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              取消
            </button>
            <button type="button" className={btnPrimary} data-testid="add-member-submit" onClick={onSubmit}>
              Add
            </button>
          </div>
        </>
      }
    >
      <Field label="类型">
        <div className="flex gap-2.5">
          <button
            type="button"
            data-testid="add-member-kind-agent"
            className={kind === 'agent' ? btnPrimary + ' flex-1 justify-center' : btnGhost + ' flex-1 justify-center'}
            onClick={() => setKind('agent')}
          >
            Agent
          </button>
          <button
            type="button"
            data-testid="add-member-kind-human"
            className={kind === 'human' ? btnPrimary + ' flex-1 justify-center' : btnGhost + ' flex-1 justify-center'}
            onClick={() => setKind('human')}
          >
            Human
          </button>
        </div>
      </Field>

      {kind === 'agent' ? (
        <Field
          label="选择 agent"
          required
          hint={
            selected.busy ? (
              <span className="text-warning">
                <b className="font-semibold">{selected.name} 当前在 {selected.from}</b> —— agent 独占单 team，Add 会弹迁移二次确认。
              </span>
            ) : (
              '选到已编入的 agent 会触发迁移二次确认（agent 独占单 team）。'
            )
          }
        >
          <select className={inputCls} value={agentValue} data-testid="add-member-agent" onChange={(e) => setAgentValue(e.target.value)}>
            {AGENT_OPTIONS.map((a) => (
              <option key={a.value} value={a.value}>
                {a.label}
              </option>
            ))}
          </select>
        </Field>
      ) : (
        <Field label="human email" required hint="human 可同时属于多个 team。">
          <input className={inputCls} placeholder="new@abc.com" data-testid="add-member-email" />
        </Field>
      )}

      <Field label="分配角色" required>
        <select className={inputCls} value={role} data-testid="add-member-role" onChange={(e) => setRole(e.target.value)}>
          {roleOptions.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
      </Field>

      {add.isError && <p className="text-xs text-danger">{(add.error as Error).message}</p>}
    </ModalShell>
  );
}
