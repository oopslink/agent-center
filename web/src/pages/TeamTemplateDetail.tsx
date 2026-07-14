// Team Template detail (/organizations/:slug/teams/templates/:templateId) —
// 4 tabs: Overview / Seed Memory / Curation & 来源 / Instances. Header carries
// Export JSON (cross-org) + Instantiate. Curation is an audit-only view of the
// per-token extract decisions; Seed Memory is the read-only template seed.
import { useState } from 'react';
import type React from 'react';
import { useNavigate, useParams } from 'react-router-dom';
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

const TABS = [
  { key: 'ov', label: 'Overview' },
  { key: 'sm', label: 'Seed Memory' },
  { key: 'cu', label: 'Curation & 来源' },
  { key: 'in', label: 'Instances' },
] as const;
type TabKey = (typeof TABS)[number]['key'];

const RISK_LABEL: Record<ScrubFinding['risk'], string> = { hi: 'high', md: 'med', lo: 'low' };
function riskClass(risk: ScrubFinding['risk']): string {
  if (risk === 'hi') return 'text-danger bg-danger/10 border border-danger/30';
  if (risk === 'md') return 'text-warning bg-warning/10 border border-warning/30';
  return 'text-text-muted bg-bg-subtle border border-border-base';
}

export default function TeamTemplateDetail(): React.ReactElement {
  const { templateId = '' } = useParams();
  const template = useTeamTemplate(templateId);
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const orgBase = org ? `/organizations/${org.slug}` : '';
  const [tab, setTab] = useState<TabKey>('ov');
  const [instantiating, setInstantiating] = useState(false);

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
          ← Templates
        </button>
        <p className="mt-4 text-sm text-danger" data-testid="template-detail-error">
          {(template.error as Error)?.message ?? 'template_not_found'}
        </p>
      </section>
    );
  }

  const t = template.data;
  const totalSlots = t.roles.reduce((s, r) => s + r.count, 0);

  return (
    <section className="space-y-2" data-testid="page-TeamTemplateDetail">
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start gap-4">
          <Glyph text={t.name.slice(0, 2).toUpperCase()} size="lg" />
          <div>
            <div className="flex items-center gap-3">
              <h1 className="font-heading text-xl font-semibold text-text-primary">{t.name}</h1>
              <span className="rounded bg-success/15 px-2 py-0.5 text-[0.65rem] font-semibold text-success">{t.version_label}</span>
            </div>
            <div className="mt-1.5 flex flex-wrap gap-x-3.5 gap-y-1 text-xs text-text-muted">
              <span className="font-mono">{t.id}</span>
              <span>来源：{t.source}</span>
              <span>{totalSlots} slots · {t.roles.length} roles</span>
              <span>{t.instances_count} 个实例</span>
            </div>
          </div>
        </div>
        <div className="flex gap-2.5">
          <button
            type="button"
            className={btnGhost}
            data-testid="template-export"
            onClick={() => downloadJson(`${t.name}.team-template.json`, exportTemplateEnvelope(t))}
          >
            <ExportIcon className="h-4 w-4" /> Export JSON
          </button>
          <button type="button" className={btnPrimary} data-testid="template-instantiate" onClick={() => setInstantiating(true)}>
            Instantiate <ArrowRightIcon className="h-4 w-4" />
          </button>
        </div>
      </div>

      <Tabs tabs={TABS} active={tab} onChange={setTab} testId="template-tabs" />

      <div role="tabpanel" id={`panel-${tab}`} aria-labelledby={`tab-${tab}`}>
        {tab === 'ov' && <OverviewPane template={t} totalSlots={totalSlots} />}
        {tab === 'sm' && (
          <div>
            <SectionHead title="Seed Memory" hint="实例化时种入新 team memory-repo · 已 curation · 只读预览" />
            <MemoryPane teamId={t.id} heading="⌗ template seed" />
          </div>
        )}
        {tab === 'cu' && <CurationPane templateId={t.id} source={t.source} />}
        {tab === 'in' && <InstancesPane templateId={t.id} orgBase={orgBase} />}
      </div>

      {instantiating && (
        <InstantiateModal
          template={t}
          onClose={() => setInstantiating(false)}
          onInstantiated={(id) => navigate(`${orgBase}/teams/${id}`)}
        />
      )}
    </section>
  );
}

function OverviewPane({ template: t, totalSlots }: { template: TeamTemplate; totalSlots: number }): React.ReactElement {
  return (
    <div>
      <div className="grid gap-3.5 md:grid-cols-2">
        <Card>
          <SectionHead title="角色配比" hint="实例化时可调 count" />
          <RoleBar roles={t.roles} className="w-full" />
          <RoleLegend roles={t.roles} />
          <div className="mt-2.5">
            {t.roles.map((r) => (
              <SpecLine
                key={r.role}
                k={
                  <span className="flex items-center gap-1.5">
                    <span className="h-2 w-2 rounded-sm" style={{ background: roleColor(r.role) }} aria-hidden="true" />
                    {r.role} ×{r.count}
                  </span>
                }
                v={`${r.cli} · ${r.model} · conc ${r.max_concurrency}${r.capability_tags.length ? ' · ' + r.capability_tags.join(', ') : ''}`}
              />
            ))}
          </div>
        </Card>
        <Card>
          <SectionHead title="模版信息" hint="provenance" />
          <SpecLine k="版本" v={t.version_label} />
          <SpecLine k="来源" v={t.source} />
          <SpecLine k="workflow-ref" v={t.workflow_template_ref} />
          <SpecLine k="curation" v={<span className="text-success">{t.curated ? '已过门' : '未过门'}</span>} />
          <SpecLine k="实例" v={`${t.instances_count} 个 team`} />
        </Card>
      </div>
      <Note>
        Instantiate 时会按此配比建一支 team、声明角色 slots、把 Seed Memory 种进新 team 的 memory-repo、绑 workflow-ref。
        <b>Phase-1 不铸 agent 身份</b>（运行时另行编入），<b>与 project 无关</b>，project 后置关联。总 {totalSlots} slots。
      </Note>
    </div>
  );
}

function CurationPane({ templateId, source }: { templateId: string; source: string }): React.ReactElement {
  const scrub = useTemplateScrub(templateId);
  return (
    <div>
      <SectionHead title="Curation & 来源" hint="extract 时的逐条处置（审计只读）" />
      <Note>
        此模版从 <b>{source}</b> 抽取。下列疑似专属 token 已逐条 curation：
        <span className="font-mono text-danger"> 占位</span>=已替换成占位符，<span className="font-mono text-success">保留</span>=人工确认保留。
      </Note>
      {scrub.isLoading && <Skeleton height="8rem" />}
      <div data-testid="curation-list">
        {(scrub.data ?? []).map((f, i) => {
          const kept = f.default_action === 'keep';
          return (
            <div key={i} data-testid={`curation-${i}`} className="mb-3 overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1">
              <div className="flex items-center gap-3 px-4 py-3">
                <span className={`rounded px-2 py-0.5 text-[0.6rem] font-bold uppercase tracking-wide ${riskClass(f.risk)}`}>{RISK_LABEL[f.risk]}</span>
                <span className="min-w-0 flex-1 truncate font-mono text-[0.7rem] text-text-muted">{f.loc}</span>
                <span className="text-[0.7rem] text-text-muted">{f.reason}</span>
                <span
                  className={[
                    'rounded px-2 py-0.5 text-[0.65rem] font-semibold',
                    kept ? 'bg-success/15 text-success' : 'border border-border-base bg-bg-subtle text-text-muted',
                  ].join(' ')}
                >
                  {kept ? '保留' : '已占位'}
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
  const instances = useTemplateInstances(templateId);
  const navigate = useNavigate();
  return (
    <div>
      <SectionHead title="Instances" hint="从此模版实例化出的 team" />
      {instances.isLoading && <Skeleton height="4rem" />}
      {instances.isSuccess && instances.data.length === 0 && (
        <EmptyState title="还没有实例" body="还没有从此模版实例化的 team。" testId="instances-empty" />
      )}
      {(instances.data ?? []).map((x) => (
        <div key={x.id} data-testid={`instance-${x.id}`} className="mb-2.5 flex items-center gap-3 rounded-lg border border-border-base bg-bg-elevated px-3.5 py-3 shadow-1">
          <Glyph text={x.name.slice(0, 2).toUpperCase()} kind="agent" />
          <div className="flex-1">
            <b className="font-semibold text-text-primary">{x.name}</b>
            <span className="block font-mono text-[0.6875rem] text-text-muted">{x.id}</span>
          </div>
          <button type="button" className={btnSm} onClick={() => navigate(`${orgBase}/teams/${x.id}`)}>
            打开
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
