// Team WebUI — Add member modal (+ the agent-exclusivity migration confirm).
// An agent belongs to exactly one team: adding one that's already elsewhere
// triggers a second-confirm migration. A human may join many teams freely.
//
// The pickers are the REAL org directory (GET /directory/{agents,humans}); each
// entry carries a canonical `ref` ("agent:<id>" / "user:<id>") used verbatim as
// the member_ref — no fabricated/truncated refs reach the write path.
import { useState } from 'react';
import type React from 'react';
import { useAddMember, useDirectoryAgents, useDirectoryHumans, type TeamView } from '@/api/teams';
import { Skeleton } from '@/components/Skeleton';
import { EmptyState } from '@/components/EmptyState';
import { btnGhost, btnPrimary, Field, inputCls, ModalShell, SpecLine } from './kit';
import { WarnIcon } from './teamsUi';

type Kind = 'agent' | 'human';

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
  const [agentRef, setAgentRef] = useState('');
  const [humanRef, setHumanRef] = useState('');
  const [role, setRole] = useState(roleOptions[0] ?? 'coder');
  const [migrating, setMigrating] = useState(false);
  const add = useAddMember();

  const agents = useDirectoryAgents();
  const humans = useDirectoryHumans();
  // Candidates exclude identities already on THIS team (can't re-add). Agents are
  // exclusive to one team, so any remaining team membership is a DIFFERENT team →
  // adding triggers the migration confirm.
  const candidateAgents = (agents.data ?? []).filter((a) => !a.teams.includes(team.name));
  const candidateHumans = (humans.data ?? []).filter((h) => !h.teams.includes(team.name));
  const selectedAgent = candidateAgents.find((a) => a.ref === agentRef) ?? candidateAgents[0];
  const selectedHuman = candidateHumans.find((h) => h.ref === humanRef) ?? candidateHumans[0];

  const fromTeam = selectedAgent?.teams[0]; // exclusive → its (single) current team
  const needsMigration = kind === 'agent' && !!fromTeam;

  const doAdd = async (opts?: { migrateFrom?: string }) => {
    const picked = kind === 'agent' ? selectedAgent : selectedHuman;
    if (!picked) return;
    try {
      await add.mutateAsync({
        team_id: team.id,
        member_ref: picked.ref,
        name: picked.name,
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
    if (needsMigration) {
      setMigrating(true);
      return;
    }
    void doAdd();
  };

  if (migrating && selectedAgent) {
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
            <b>{selectedAgent.name}</b> 目前是 <b>{fromTeam}</b> 的独占成员。
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
                onClick={() => doAdd({ migrateFrom: fromTeam })}
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
            <b className="font-semibold text-warning"> 从 {fromTeam} 迁出 </b>
            该 agent，其在原 team 的运行中任务需先收尾/转派。
          </div>
        </div>
        <div className="rounded-lg border border-border-base bg-bg-subtle p-4">
          <SpecLine k="迁出" v={fromTeam} />
          <SpecLine k="迁入" v={`${team.name} · ${role}`} />
          <SpecLine k="原 team 运行中任务" v={<span className="text-warning">需先收尾 / 转派</span>} />
        </div>
      </ModalShell>
    );
  }

  const list = kind === 'agent' ? agents : humans;
  const candidateCount = kind === 'agent' ? candidateAgents.length : candidateHumans.length;

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
            <button
              type="button"
              className={btnPrimary}
              data-testid="add-member-submit"
              disabled={candidateCount === 0 || add.isPending}
              onClick={onSubmit}
            >
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

      {list.isLoading && <Skeleton height="4rem" />}
      {!list.isLoading && candidateCount === 0 && (
        <EmptyState
          title={kind === 'agent' ? '没有可编入的 agent' : '没有可编入的 human'}
          body={kind === 'agent' ? '组织内的 agent 都已在本 team，或还没有 agent。' : '组织内的 human 都已在本 team，或还没有 human。'}
          testId="add-member-empty"
        />
      )}

      {!list.isLoading && candidateCount > 0 && kind === 'agent' && (
        <Field
          label="选择 agent"
          required
          hint={
            needsMigration ? (
              <span className="text-warning">
                <b className="font-semibold">{selectedAgent?.name} 当前在 {fromTeam}</b> —— agent 独占单 team，Add 会弹迁移二次确认。
              </span>
            ) : (
              '选到已在别的 team 的 agent 会触发迁移二次确认（agent 独占单 team）。'
            )
          }
        >
          <select
            className={inputCls}
            value={selectedAgent?.ref ?? ''}
            data-testid="add-member-agent"
            onChange={(e) => setAgentRef(e.target.value)}
          >
            {candidateAgents.map((a) => (
              <option key={a.ref} value={a.ref}>
                {a.name} · {a.model}{a.teams.length ? `（在 ${a.teams[0]}）` : '（空闲）'}
              </option>
            ))}
          </select>
        </Field>
      )}

      {!list.isLoading && candidateCount > 0 && kind === 'human' && (
        <Field label="选择 human" required hint="human 可同时属于多个 team。">
          <select
            className={inputCls}
            value={selectedHuman?.ref ?? ''}
            data-testid="add-member-human"
            onChange={(e) => setHumanRef(e.target.value)}
          >
            {candidateHumans.map((h) => (
              <option key={h.ref} value={h.ref}>
                {h.name} · {h.email}
              </option>
            ))}
          </select>
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
