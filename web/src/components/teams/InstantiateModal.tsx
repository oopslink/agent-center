// Team WebUI — Instantiate template modal. Project-DECOUPLED: only builds the
// team + role identities + seeds memory + binds a workflow; project association
// is deferred to the team detail's 关联项目 tab. Role配比 is editable per slot.
import { useState } from 'react';
import type React from 'react';
import { Trans, useTranslation } from 'react-i18next';
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
  const { t } = useTranslation('teams');
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
      title={t('instantiateModal.title')}
      subtitle={
        <Trans i18nKey="instantiateModal.subtitle" ns="teams" values={{ name: template.name }} components={{ b: <b /> }} />
      }
      footer={
        <>
          <span />
          <div className="flex gap-2.5">
            <button type="button" className={btnGhost} onClick={onClose}>
              {t('common.cancel')}
            </button>
            <button type="button" className={btnPrimary} disabled={!canSubmit} data-testid="instantiate-submit" onClick={submit}>
              {instantiate.isPending ? t('instantiateModal.instantiating') : t('instantiateModal.submit')}
            </button>
          </div>
        </>
      }
    >
      <Field label={t('instantiateModal.nameLabel')} required>
        <input className={inputCls} value={name} data-testid="instantiate-name" onChange={(e) => setName(e.target.value)} />
      </Field>

      <div className="mb-3 mt-1 flex items-center justify-between gap-3">
        <label className="text-xs font-semibold text-text-secondary">
          {t('instantiateModal.roleMixLabel')} <span className="font-normal text-text-muted">{t('instantiateModal.roleMixHint')}</span>
        </label>
        <span className="text-[0.6875rem] text-text-muted" data-testid="instantiate-total">
          {t('instantiateModal.slotsSummary', { total })}
        </span>
      </div>
      <RoleBuilder roles={roles} onChange={setRoles} showDescription idPrefix="instantiate" />

      <Card className="mt-3.5 bg-bg-subtle" testId="instantiate-summary">
        <SmallLabel>{t('instantiateModal.summaryLabel')}</SmallLabel>
        <SpecLine
          k={t('instantiateModal.declaredSlots')}
          v={t('instantiateModal.declaredSlotsValue', {
            total,
            breakdown: roles.map((r) => `${r.role || t('instantiateModal.unnamed')}×${r.count}`).join(' / '),
          })}
        />
        <SpecLine k={t('instantiateModal.memoryRepo')} v={t('instantiateModal.memoryRepoValue')} />
        <SpecLine k={t('instantiateModal.bindWorkflow')} v={t('instantiateModal.bindWorkflowValue')} />
        <SpecLine k={t('instantiateModal.agentIdentity')} v={<span className="text-text-muted">{t('instantiateModal.agentIdentityValue')}</span>} />
        <SpecLine k={t('instantiateModal.projectLink')} v={t('instantiateModal.projectLinkValue')} />
      </Card>
      <p className="mt-2 text-[0.6875rem] text-text-muted">
        <Trans i18nKey="instantiateModal.footnote" ns="teams" components={{ b: <b />, code: <span className="font-mono" /> }} />
      </p>
    </ModalShell>
  );
}
