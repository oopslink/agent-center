// Team WebUI — New Team modal. Declares the role配比 via the shared RoleBuilder;
// creating builds agent identities + a team-memory repo (Phase-1: fixtures).
import { useState } from 'react';
import type React from 'react';
import { useTranslation } from 'react-i18next';
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
  const { t } = useTranslation('teams');
  const [name, setName] = useState('');
  const [visibility, setVisibility] = useState('org-private');
  const [description, setDescription] = useState('');
  const [roles, setRoles] = useState<RoleInput[]>([newRole('planner'), { ...newRole('coder'), count: 2 }]);
  const create = useCreateTeam();

  const roleNames = roles.map((r) => r.role.trim());
  const hasBlankRole = roleNames.some((role) => role.length === 0);
  const hasDuplicateRole = new Set(roleNames).size !== roleNames.length;
  const roleValidationError = hasBlankRole
    ? t('newTeamModal.errRoleNameRequired')
    : hasDuplicateRole
      ? t('newTeamModal.errRoleNameDuplicate')
      : '';
  const canSubmit = name.trim().length > 0 && !roleValidationError && !create.isPending;

  const submit = async () => {
    if (!canSubmit) return;
    try {
      const team = await create.mutateAsync({
        name: name.trim(),
        description,
        visibility,
        roles: roles.map((r) => ({ ...r, role: r.role.trim(), tags: r.tags.trim() })),
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

      {roleValidationError && (
        <p className="mt-3 text-xs text-danger" data-testid="new-team-validation-error">
          {roleValidationError}
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
