// Team WebUI — New Team modal. Declares the role配比 via the shared RoleBuilder;
// creating builds agent identities + a team-memory repo (Phase-1: fixtures).
import { useState } from 'react';
import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useCreateTeam, useInstantiateTeamApply, useInstantiateTeamPreview, type RoleInput, type TeamInstantiatePreview } from '@/api/teams';
import { ConfirmModal } from '@/components/ConfirmModal';
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
  const [preview, setPreview] = useState<TeamInstantiatePreview | null>(null);
  const create = useCreateTeam();
  const previewTeam = useInstantiateTeamPreview();
  const applyTeam = useInstantiateTeamApply();
  const runtimeCatalog = useRuntimeSelectorCatalog();

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
  const canSubmit = name.trim().length > 0 && !roleValidationError && !runtimeValidationError && !create.isPending && !previewTeam.isPending && !applyTeam.isPending;

  const rolePayload = () => roles.map((r) => ({ ...r, role: r.role.trim(), tags: r.tags.trim() }));
  const needsAccessPreview = roles.some((role) => (role.access_profiles ?? []).length > 0 || (role.access_requirements ?? []).length > 0);

  const submit = async () => {
    if (!canSubmit) return;
    try {
      if (needsAccessPreview) {
        const nextPreview = await previewTeam.mutateAsync({
          template_id: '',
          team_name: name.trim(),
          roles: rolePayload(),
          assignments: [],
        });
        setPreview(nextPreview);
        return;
      }
      const team = await create.mutateAsync({
        name: name.trim(),
        description,
        visibility,
        roles: rolePayload(),
      });
      onClose();
      onCreated(team.id);
    } catch {
      /* surfaced via create.error */
    }
  };

  if (!open) return null;
  return (
    <>
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
      {previewTeam.isError && (
        <p className="mt-3 text-xs text-danger" data-testid="new-team-preview-error">
          {(previewTeam.error as Error).message}
        </p>
      )}
    </ModalShell>
    <ConfirmModal
      open={preview !== null}
      title={t('newTeamModal.previewTitle', { defaultValue: 'Confirm access profile changes' })}
      message={t('newTeamModal.previewMessage', { defaultValue: 'This will apply {{count}} authorization operations before creating the team.', count: preview?.operations.length ?? 0 })}
      confirmLabel={t('newTeamModal.previewConfirm', { defaultValue: 'Apply and create' })}
      busy={applyTeam.isPending}
      onCancel={() => setPreview(null)}
      onConfirm={async () => {
        if (!preview) return;
        const team = await applyTeam.mutateAsync({
          template_id: '',
          team_name: name.trim(),
          roles: rolePayload(),
          assignments: [],
          preview_request_id: preview.request_id,
          idempotency_key: crypto.randomUUID(),
        });
        setPreview(null);
        onClose();
        onCreated(team.id);
      }}
    />
    </>
  );
}
