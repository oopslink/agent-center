// Team WebUI — New Team modal. Declares the role配比 via the shared RoleBuilder;
// creating builds agent identities + a team-memory repo (Phase-1: fixtures).
import { useState } from 'react';
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useCreateTeam, useDirectoryAgents, useDirectoryHumans, type RoleInput } from '@/api/teams';
import { btnGhost, btnPrimary, Field, inputCls, ModalShell } from './kit';
import { newRole, RoleBuilder, totalSlots } from './RoleBuilder';
import { isSelectableRuntimePair, useRuntimeSelectorCatalog } from '@/components/RuntimeSelectors';

export function NewTeamModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (teamId: string) => void;
}): React.ReactElement | null {
  const { t } = useTranslation('teams');
  const [name, setName] = useState('');
  const [visibility, setVisibility] = useState('org-private');
  const [description, setDescription] = useState('');
  const [roles, setRoles] = useState<RoleInput[]>([newRole('planner'), { ...newRole('coder'), count: 2 }]);
  const [assignmentSubject, setAssignmentSubject] = useState('');
  const [assignmentRole, setAssignmentRole] = useState('planner');
  const [candidateAssignments, setCandidateAssignments] = useState<Array<{ subject_ref: string; role: string }>>([]);
  const create = useCreateTeam();
  const runtimeCatalog = useRuntimeSelectorCatalog();
  const agents = useDirectoryAgents();
  const humans = useDirectoryHumans();

  const roleNames = roles.map((r) => r.role.trim());
  const hasBlankRole = roleNames.some((role) => role.length === 0);
  const hasDuplicateRole = new Set(roleNames).size !== roleNames.length;
  const hasRuntimeRoles = roles.length > 0;
  const runtimeSelectionValid = !hasRuntimeRoles || roles.every((role) =>
    isSelectableRuntimePair(runtimeCatalog.catalog, role.cli, role.model, 'model-key'),
  );
  const runtimeValidationError = !hasRuntimeRoles
    ? ''
    : runtimeCatalog.isLoading
      ? t('roleBuilder.runtimeCatalogLoading')
      : Boolean(runtimeCatalog.error)
        ? t('roleBuilder.runtimeCatalogUnavailable')
        : !runtimeSelectionValid
          ? t('roleBuilder.runtimeSelectionRequired')
          : '';
  const roleValidationError = hasBlankRole
    ? t('newTeamModal.errRoleNameRequired')
    : hasDuplicateRole
      ? t('newTeamModal.errRoleNameDuplicate')
      : '';
  const canSubmit = name.trim().length > 0 && !roleValidationError && !runtimeValidationError && !create.isPending;

  const submit = async () => {
    if (!canSubmit) return;
    try {
      const team = await create.mutateAsync({
        name: name.trim(),
        description,
        visibility,
        roles: roles.map((r) => ({ ...r, role: r.role.trim(), tags: r.tags.trim() })),
        candidate_assignments: candidateAssignments,
      });
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
      title={t('newTeamModal.title')}
      subtitle={t('newTeamModal.subtitle')}
      footer={
        <>
          <span className="text-[0.6875rem] text-text-muted">{t('newTeamModal.footerHint')}</span>
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="button" className={btnPrimary} disabled={!canSubmit} data-testid="new-team-submit" onClick={submit}>
              {create.isPending ? t('newTeamModal.creating') : t('newTeamModal.submit')}
            </button>
          </div>
        </>
      }
    >
      <div className="grid grid-cols-2 gap-3">
        <Field label={t('newTeamModal.nameLabel')} required>
          <input
            className={inputCls}
            value={name}
            placeholder={t('newTeamModal.namePlaceholder')}
            data-testid="new-team-name"
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field label={t('newTeamModal.visibilityLabel')}>
          <select className={inputCls} value={visibility} data-testid="new-team-visibility" onChange={(e) => setVisibility(e.target.value)}>
            <option value="org-private">org-private</option>
            <option value="project-scoped">project-scoped</option>
          </select>
        </Field>
      </div>
      <Field label={t('newTeamModal.descriptionLabel')}>
        <textarea
          className={inputCls}
          rows={2}
          value={description}
          placeholder={t('newTeamModal.descriptionPlaceholder')}
          data-testid="new-team-desc"
          onChange={(e) => setDescription(e.target.value)}
        />
      </Field>

      <div className="mb-3 mt-5 flex items-center justify-between">
        <label className="text-xs font-semibold text-text-secondary">
          {t('newTeamModal.roleMixLabel')} <span className="text-accent">*</span>
        </label>
        <span className="text-[0.6875rem] text-text-muted">{t('newTeamModal.slotsSummary', { slots: totalSlots(roles) })}</span>
      </div>
      <RoleBuilder roles={roles} onChange={setRoles} idPrefix="new-team" />

      <div className="mt-4 rounded-lg border border-border-base bg-bg-subtle p-3" data-testid="new-team-assignment-preview">
        <div className="mb-2 flex items-center justify-between gap-2">
          <label className="text-xs font-semibold text-text-secondary">{t('newTeamModal.assignmentsLabel')}</label>
          <span className="text-[0.6875rem] text-text-muted">{t('newTeamModal.assignmentsCount', { count: candidateAssignments.length })}</span>
        </div>
        <div className="grid gap-2 md:grid-cols-[1fr_10rem_auto]">
          <select
            className={inputCls}
            value={assignmentSubject}
            data-testid="new-team-assignment-subject"
            onChange={(e) => setAssignmentSubject(e.target.value)}
          >
            <option value="">{t('newTeamModal.assignmentSubjectPlaceholder')}</option>
            {[...(agents.data ?? []), ...(humans.data ?? [])].map((subject) => (
              <option key={subject.ref} value={subject.ref}>{subject.name} · {subject.ref}</option>
            ))}
          </select>
          <select
            className={inputCls}
            value={assignmentRole}
            data-testid="new-team-assignment-role"
            onChange={(e) => setAssignmentRole(e.target.value)}
          >
            {roles.map((role) => (
              <option key={role.role} value={role.role}>{role.role || t('roleBuilder.roleNamePlaceholder')}</option>
            ))}
          </select>
          <button
            type="button"
            className={btnGhost}
            data-testid="new-team-assignment-add"
            disabled={!assignmentSubject || !assignmentRole}
            onClick={() => {
              setCandidateAssignments((prev) => [...prev.filter((a) => a.subject_ref !== assignmentSubject), { subject_ref: assignmentSubject, role: assignmentRole }]);
            }}
          >
            {t('newTeamModal.assignmentAdd')}
          </button>
        </div>
        <div className="mt-2 space-y-1">
          {candidateAssignments.length === 0 && <p className="text-xs text-text-muted">{t('newTeamModal.assignmentsEmpty')}</p>}
          {candidateAssignments.map((assignment) => {
            const role = roles.find((r) => r.role === assignment.role);
            const permissions = role?.access_requirements ?? [];
            const highRisk = permissions.some((permission) => permission.includes('manage') || permission.includes('review') || permission.includes('delete') || permission.includes('remove'));
            return (
              <div key={`${assignment.subject_ref}:${assignment.role}`} className="flex items-center justify-between gap-2 rounded border border-border-base bg-bg-base px-2 py-1.5 text-xs" data-testid="new-team-assignment-row">
                <span className="font-mono">{assignment.subject_ref} -&gt; {assignment.role}</span>
                <span className={highRisk ? 'font-semibold text-danger' : 'text-text-muted'}>
                  {t(highRisk ? 'newTeamModal.assignmentRiskHigh' : 'newTeamModal.assignmentRiskLow', { count: permissions.length })}
                </span>
              </div>
            );
          })}
        </div>
      </div>

      {roleValidationError && (
        <p className="mt-3 text-xs text-danger" data-testid="new-team-validation-error">
          {roleValidationError}
        </p>
      )}
      {runtimeValidationError && (
        <p className="mt-3 text-xs text-danger" data-testid="new-team-runtime-validation-error">
          {runtimeValidationError}
        </p>
      )}

      {create.isError && (
        <p className="mt-3 text-xs text-danger" data-testid="new-team-error">
          {(create.error as Error).message}
        </p>
      )}
    </ModalShell>
  );
}
