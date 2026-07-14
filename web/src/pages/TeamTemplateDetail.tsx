// Team Template detail (/organizations/:slug/teams/templates/:templateId) —
// 4 tabs: Overview / Seed Memory / Curation & source / Instances. Header carries
// Export JSON (cross-org) + Instantiate. Curation is an audit-only view of the
// per-token extract decisions; Seed Memory is the read-only template seed.
import { useState } from 'react';
import type React from 'react';
import type { TFunction } from 'i18next';
import { useNavigate, useParams } from 'react-router-dom';
import { Trans, useTranslation } from 'react-i18next';
import { useOptionalOrgContext } from '@/OrgContext';
import {
  exportTemplateEnvelope,
  useTeamTemplate,
  useTemplateInstances,
  useTemplateScrub,
  type ScrubFinding,
  type TeamTemplate,
} from '@/api/teams';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { InstantiateModal } from '@/components/teams/InstantiateModal';
import { MemoryPane } from '@/components/teams/MemoryPane';
import { btnGhost, btnPrimary, btnSm, Card, Note, SectionHead, SpecLine, Tabs } from '@/components/teams/kit';
import { ArrowRightIcon, ExportIcon, Glyph, RoleBar, RoleLegend } from '@/components/teams/teamsUi';
import { roleColor } from '@/api/teams';

const TABS_KEYS = ['ov', 'sm', 'cu', 'in'] as const;
type TabKey = (typeof TABS_KEYS)[number];

function riskLabel(risk: ScrubFinding['risk'], t: TFunction): string {
  if (risk === 'hi') return t('templateDetail.riskHigh');
  if (risk === 'md') return t('templateDetail.riskMed');
  return t('templateDetail.riskLow');
}
function riskClass(risk: ScrubFinding['risk']): string {
  if (risk === 'hi') return 'text-danger bg-danger/10 border border-danger/30';
  if (risk === 'md') return 'text-warning bg-warning/10 border border-warning/30';
  return 'text-text-muted bg-bg-subtle border border-border-base';
}

export default function TeamTemplateDetail(): React.ReactElement {
  const { t } = useTranslation('teams');
  const { templateId = '' } = useParams();
  const template = useTeamTemplate(templateId);
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [tab, setTab] = useState<TabKey>('ov');
  const [instantiating, setInstantiating] = useState(false);

  const TABS = [
    { key: 'ov', label: t('templateDetail.tabs.overview') },
    { key: 'sm', label: t('templateDetail.tabs.seedMemory') },
    { key: 'cu', label: t('templateDetail.tabs.curation') },
    { key: 'in', label: t('templateDetail.tabs.instances') },
  ] as const;

  if (template.isLoading) {
    return (
      <section className="space-y-4" data-testid="page-TeamTemplateDetail">
        <Skeleton height="5rem" />
        <Skeleton height="16rem" />
      </section>
    );
  }
  if (template.isError || !template.data) {
    return (
      <section data-testid="page-TeamTemplateDetail">
        <button type="button" className={btnGhost} onClick={() => navigate(`${orgBase}/teams/templates`)}>
          {t('templateDetail.backToTemplates')}
        </button>
        <p className="mt-4 text-sm text-danger" data-testid="template-detail-error">
          {(template.error as Error)?.message ?? t('templateDetail.notFound')}
        </p>
      </section>
    );
  }

  const tpl = template.data;
  const totalSlots = tpl.roles.reduce((s, r) => s + r.count, 0);

  return (
    <section className="space-y-2" data-testid="page-TeamTemplateDetail">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Glyph text={tpl.name.slice(0, 2).toUpperCase()} size="lg" />
          <div>
            <div className="flex items-center gap-3">
              <h1 className="font-heading text-xl font-semibold text-text-primary">{tpl.name}</h1>
              <span className="rounded bg-success/15 px-2 py-0.5 text-[0.65rem] font-semibold text-success">{tpl.version_label}</span>
            </div>
            <div className="mt-1.5 flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-text-muted">
              <span className="font-mono">{tpl.id}</span>
              <span>{t('templateDetail.sourcePrefix', { source: tpl.source })}</span>
              <span>{t('templateDetail.slotsRoles', { slots: totalSlots, roles: tpl.roles.length })}</span>
              <span>{t('templateDetail.instancesCount', { count: tpl.instances_count })}</span>
            </div>
          </div>
        </div>
        <div className="flex gap-2.5">
          <button
            type="button"
            className={btnGhost}
            data-testid="template-export"
            onClick={() => downloadJson(`${tpl.name}.team-template.json`, exportTemplateEnvelope(tpl))}
          >
            <ExportIcon className="h-4 w-4" /> {t('templateDetail.exportJson')}
          </button>
          <button type="button" className={btnPrimary} data-testid="template-instantiate" onClick={() => setInstantiating(true)}>
            {t('templateDetail.instantiate')} <ArrowRightIcon className="h-4 w-4" />
          </button>
        </div>
      </div>

      <Tabs tabs={TABS} active={tab} onChange={setTab} testId="template-tabs" />

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === 'ov' && <OverviewPane template={tpl} totalSlots={totalSlots} />}
        {tab === 'sm' && (
          <div>
            <SectionHead title={t('templateDetail.tabs.seedMemory')} hint={t('templateDetail.seedMemoryHint')} />
            <MemoryPane teamId={tpl.id} heading={t('templateDetail.seedMemoryHeading')} />
          </div>
        )}
        {tab === 'cu' && <CurationPane templateId={tpl.id} source={tpl.source} />}
        {tab === 'in' && <InstancesPane templateId={tpl.id} orgBase={orgBase} />}
      </div>

      {instantiating && (
        <InstantiateModal
          template={tpl}
          onClose={() => setInstantiating(false)}
          onInstantiated={(id) => navigate(`${orgBase}/teams/${id}`)}
        />
      )}
    </section>
  );
}

function OverviewPane({ template: tpl, totalSlots }: { template: TeamTemplate; totalSlots: number }): React.ReactElement {
  const { t } = useTranslation('teams');
  return (
    <div>
      <div className="grid gap-3.5 md:grid-cols-2">
        <Card>
          <SectionHead title={t('templateDetail.overview.roleMix')} hint={t('templateDetail.overview.roleMixHint')} />
          <RoleBar roles={tpl.roles} className="w-full" />
          <RoleLegend roles={tpl.roles} />
          <div className="mt-2.5">
            {tpl.roles.map((r) => (
              <SpecLine
                key={r.role}
                k={
                  <span className="flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
                    {r.role} ×{r.count}
                  </span>
                }
                v={`${t('templateDetail.overview.roleSpec', { cli: r.cli, model: r.model, conc: r.max_concurrency })}${r.capability_tags.length ? ' · ' + r.capability_tags.join(', ') : ''}`}
              />
            ))}
          </div>
        </Card>
        <Card>
          <SectionHead title={t('templateDetail.overview.info')} hint={t('templateDetail.overview.infoHint')} />
          <SpecLine k={t('templateDetail.overview.version')} v={tpl.version_label} />
          <SpecLine k={t('templateDetail.overview.source')} v={tpl.source} />
          <SpecLine k={t('templateDetail.overview.workflowRef')} v={tpl.workflow_template_ref} />
          <SpecLine k={t('templateDetail.overview.curation')} v={<span className="text-success">{tpl.curated ? t('templateDetail.overview.gated') : t('templateDetail.overview.notGated')}</span>} />
          <SpecLine k={t('templateDetail.overview.instances')} v={t('templateDetail.overview.instancesValue', { count: tpl.instances_count })} />
        </Card>
      </div>
      <Note>
        <Trans i18nKey="templateDetail.overview.note" ns="teams" values={{ slots: totalSlots }} components={{ b: <b /> }} />
      </Note>
    </div>
  );
}

function CurationPane({ templateId, source }: { templateId: string; source: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  const scrub = useTemplateScrub(templateId);
  return (
    <div>
      <SectionHead title={t('templateDetail.curation.title')} hint={t('templateDetail.curation.hint')} />
      <Note>
        <Trans
          i18nKey="templateDetail.curation.note"
          ns="teams"
          values={{ source }}
          components={{
            b: <b />,
            danger: <span className="font-mono text-danger" />,
            success: <span className="font-mono text-success" />,
          }}
        />
      </Note>
      {scrub.isLoading && <Skeleton height="8rem" />}
      <div data-testid="curation-list">
        {(scrub.data ?? []).map((f, i) => {
          const kept = f.default_action === 'keep';
          return (
            <div key={i} data-testid={`curation-${i}`} className="mb-3 overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1">
              <div className="flex items-center gap-3 px-4 py-3">
                <span className={`rounded px-2 py-0.5 text-[0.6rem] font-bold uppercase tracking-wide ${riskClass(f.risk)}`}>{riskLabel(f.risk, t)}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-[0.7rem] text-text-muted">{f.loc}</span>
                <span className="text-[0.7rem] text-text-muted">{f.reason}</span>
                <span
                  className={[
                    'rounded px-2 py-0.5 text-[0.65rem] font-semibold',
                    kept ? 'bg-success/15 text-success' : 'border border-border-base bg-bg-subtle text-text-muted',
                  ].join(' ')}
                >
                  {kept ? t('templateDetail.curation.keptBadge') : t('templateDetail.curation.placeheldBadge')}
                </span>
              </div>
              <div className="px-4 pb-3 font-mono text-xs text-text-secondary">
                <span
                  className={
                    kept
                      ? 'rounded border-b border-dashed border-success bg-success/15 px-1 text-success'
                      : 'rounded border-b border-dashed border-border-strong bg-bg-subtle px-1 text-text-muted'
                  }
                >
                  {kept ? f.token : '‹placeholder›'}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function InstancesPane({ templateId, orgBase }: { templateId: string; orgBase: string }): React.ReactElement {
  const { t } = useTranslation('teams');
  const instances = useTemplateInstances(templateId);
  const navigate = useNavigate();
  return (
    <div>
      <SectionHead title={t('templateDetail.instances.title')} hint={t('templateDetail.instances.hint')} />
      {instances.isLoading && <Skeleton height="4rem" />}
      {instances.isSuccess && instances.data.length === 0 && (
        <EmptyState title={t('templateDetail.instances.emptyTitle')} body={t('templateDetail.instances.emptyBody')} testId="instances-empty" />
      )}
      {(instances.data ?? []).map((x) => (
        <div key={x.id} data-testid={`instance-${x.id}`} className="mb-2.5 flex items-center gap-3 rounded-lg border border-border-base bg-bg-elevated px-3.5 py-3 shadow-1">
          <Glyph text={x.name.slice(0, 2).toUpperCase()} kind="agent" />
          <div className="flex-1">
            <b className="font-semibold text-text-primary">{x.name}</b>
            <span className="block font-mono text-[0.6875rem] text-text-muted">{x.id}</span>
          </div>
          <button type="button" className={btnSm} onClick={() => navigate(`${orgBase}/teams/${x.id}`)}>
            {t('templateDetail.instances.open')}
          </button>
        </div>
      ))}
    </div>
  );
}

function downloadJson(filename: string, data: unknown): void {
  const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
