// Team WebUI — Add member modal (+ the agent-exclusivity migration confirm).
// An agent belongs to exactly one team: adding one that's already elsewhere
// triggers a second-confirm migration. A human may join many teams freely.
//
// The pickers are the REAL org directory (GET /directory/{agents,humans}); each
// entry carries a canonical `ref` ("agent:<id>" / "user:<id>") used verbatim as
// the member_ref — no fabricated/truncated refs reach the write path.
import { useState } from 'react';
import type React from 'react';
import type { TFunction } from 'i18next';
import { Trans, useTranslation } from 'react-i18next';
import { ApiError } from '@/api/client';
import { useAddMember, useDirectoryAgents, useDirectoryHumans, type TeamView } from '@/api/teams';
import { Skeleton } from '@/components/Skeleton';
import { EmptyState } from '@/components/EmptyState';
import { btnGhost, btnPrimary, Field, inputCls, ModalShell, SpecLine } from './kit';
import { WarnIcon } from './teamsUi';

type Kind = 'agent' | 'human';

// Map the facade's typed member errors to friendly copy — never surface the raw
// `[404 identity_not_found] …` envelope. Codes per the backend contract:
//   identity_not_found (404) — identity gone / cross-org / kind mismatch
//   invalid_input      (400) — malformed ref (truncated / empty id segment)
//   conflict           (409) — the agent is on a THIRD team (rare migration edge)
//   not_found          (404) — the migration source is stale (ref not in source team)
function memberErrorMessage(err: unknown, t: TFunction): string {
  const code = err instanceof ApiError ? err.code : '';
  switch (code) {
    case 'identity_not_found':
      return t('addMemberModal.errIdentityNotFound');
    case 'invalid_input':
      return t('addMemberModal.errInvalidInput');
    case 'conflict':
      return t('addMemberModal.errConflict');
    case 'not_found':
      return t('addMemberModal.errNotFound');
    default:
      return t('addMemberModal.errDefault');
  }
}

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
  const { t } = useTranslation('teams');
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
  const candidateAgents = (agents.data ?? []).filter((a) => !a.team_ids.includes(team.id));
  const candidateHumans = (humans.data ?? []).filter((h) => !h.team_ids.includes(team.id));
  const selectedAgent = candidateAgents.find((a) => a.ref === agentRef) ?? candidateAgents[0];
  const selectedHuman = candidateHumans.find((h) => h.ref === humanRef) ?? candidateHumans[0];

  // The directory carries the agent's current team id AND name (aligned). Take the
  // id straight from the entry: resolving it by matching the NAME against the teams
  // list would move the agent out of the wrong team if it was renamed between the
  // two fetches. Agents are exclusive → index 0 is its (single) current team.
  const fromTeamId = selectedAgent?.team_ids[0];
  const fromTeamName = selectedAgent?.teams[0];
  const needsMigration = kind === 'agent' && !!fromTeamId;

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
      // Swallow the rejection ONLY to avoid an unhandled promise; the failure is
      // still shown to the user via add.isError below (the modal stays open on
      // error — onClose/onAdded run only after a successful await).
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
            <WarnIcon className="h-5 w-5" /> {t('addMemberModal.migrateTitle')}
          </span>
        }
        subtitle={
          <Trans i18nKey="addMemberModal.migrateSubtitle" ns="teams" values={{ name: selectedAgent.name, from: fromTeamName }} components={{ b: <b /> }} />
        }
        footer={
          <>
            <span />
            <div className="flex gap-2.5">
              <button type="button" className={btnGhost} onClick={() => setMigrating(false)}>
                {t('common.back')}
              </button>
              <button
                type="button"
                className="inline-flex items-center gap-1.5 rounded bg-warning px-3.5 py-2 text-sm font-semibold text-white hover:opacity-90"
                data-testid="migrate-confirm"
                onClick={() => doAdd({ migrateFrom: fromTeamId })}
              >
                {t('addMemberModal.migrateConfirm')}
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
            <Trans i18nKey="addMemberModal.migrateWarning" ns="teams" values={{ from: fromTeamName }} components={{ b: <b className="font-semibold text-warning" /> }} />
          </div>
        </div>
        <div className="rounded-lg border border-border-base bg-bg-subtle p-4">
          <SpecLine k={t('addMemberModal.migrateMovedOut')} v={fromTeamName} />
          <SpecLine k={t('addMemberModal.migrateMovedIn')} v={`${team.name} · ${role}`} />
          <SpecLine k={t('addMemberModal.migrateRunningTasks')} v={<span className="text-warning">{t('addMemberModal.migrateRunningTasksValue')}</span>} />
        </div>
        {add.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="migrate-error">
            {t('addMemberModal.migrateError', { message: memberErrorMessage(add.error, t) })}
          </p>
        )}
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
      title={t('addMemberModal.title')}
      subtitle={t('addMemberModal.subtitle')}
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button
              type="button"
              className={btnPrimary}
              data-testid="add-member-submit"
              disabled={candidateCount === 0 || add.isPending}
              onClick={onSubmit}
            >
              {t('addMemberModal.add')}
            </button>
          </div>
        </>
      }
    >
      <Field label={t('addMemberModal.kindLabel')}>
        <div className="flex gap-2.5">
          <button
            type="button"
            data-testid="add-member-kind-agent"
            className={kind === 'agent' ? btnPrimary + ' flex-1 justify-center' : btnGhost + ' flex-1 justify-center'}
            onClick={() => setKind('agent')}
          >
            {t('addMemberModal.kindAgent')}
          </button>
          <button
            type="button"
            data-testid="add-member-kind-human"
            className={kind === 'human' ? btnPrimary + ' flex-1 justify-center' : btnGhost + ' flex-1 justify-center'}
            onClick={() => setKind('human')}
          >
            {t('addMemberModal.kindHuman')}
          </button>
        </div>
      </Field>

      {list.isLoading && <Skeleton height="4rem" />}
      {!list.isLoading && candidateCount === 0 && (
        <EmptyState
          title={kind === 'agent' ? t('addMemberModal.emptyAgentTitle') : t('addMemberModal.emptyHumanTitle')}
          body={kind === 'agent' ? t('addMemberModal.emptyAgentBody') : t('addMemberModal.emptyHumanBody')}
          testId="add-member-empty"
        />
      )}

      {!list.isLoading && candidateCount > 0 && kind === 'agent' && (
        <Field
          label={t('addMemberModal.selectAgent')}
          required
          hint={
            needsMigration ? (
              <span className="text-warning">
                <Trans i18nKey="addMemberModal.selectAgentHintMigrate" ns="teams" values={{ name: selectedAgent?.name, from: fromTeamName }} components={{ b: <b className="font-semibold" /> }} />
              </span>
            ) : (
              t('addMemberModal.selectAgentHint')
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
                {a.name} · {a.model}{a.teams.length ? t('addMemberModal.onTeam', { team: a.teams[0] }) : t('addMemberModal.free')}
              </option>
            ))}
          </select>
        </Field>
      )}

      {!list.isLoading && candidateCount > 0 && kind === 'human' && (
        <Field label={t('addMemberModal.selectHuman')} required hint={t('addMemberModal.selectHumanHint')}>
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

      <Field label={t('addMemberModal.roleLabel')} required>
        <select className={inputCls} value={role} data-testid="add-member-role" onChange={(e) => setRole(e.target.value)}>
          {roleOptions.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
      </Field>

      {add.isError && (
        <p className="text-xs text-danger" data-testid="add-member-error">
          {memberErrorMessage(add.error, t)}
        </p>
      )}
    </ModalShell>
  );
}
