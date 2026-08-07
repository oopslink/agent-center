import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useOrgs } from '@/api/auth';
import {
  aiRuntimeExportHref,
  canManageAIRuntime,
  useAIRuntimeCatalog,
  useSetDefaultRuntimeProfile,
  type RuntimeCLI,
  type RuntimeModel,
  type RuntimeProfile,
} from '@/api/aiRuntime';
import { currentOrgSlug } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';

type RuntimeTab = 'profiles' | 'models' | 'clis';

export default function AiRuntime(): React.ReactElement {
  const { t } = useTranslation('admin');
  const catalog = useAIRuntimeCatalog();
  const orgCtx = useOptionalOrgContext();
  const orgs = useOrgs();
  const activeSlug = orgCtx?.slug ?? currentOrgSlug() ?? orgs.data?.[0]?.slug;
  const currentOrg = (orgs.data ?? []).find((o) => o.slug === activeSlug);
  const role = currentOrg?.role;
  const canManage = canManageAIRuntime(role);
  const [tab, setTab] = useState<RuntimeTab>('profiles');

  const defaultProfile = useMemo(() => {
    const data = catalog.data;
    if (!data?.default_runtime_profile_id) return undefined;
    return data.profiles.find((p) => p.id === data.default_runtime_profile_id);
  }, [catalog.data]);

  return (
    <section className="space-y-4" data-testid="page-AiRuntime">
      <Breadcrumb
        items={[
          { label: t('orgSettings.navTitle') },
          { label: t('aiRuntime.title') },
        ]}
      />
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border-base pb-3">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('aiRuntime.title')}</h1>
          <p className="max-w-3xl text-sm text-text-muted">{t('aiRuntime.subtitle')}</p>
        </div>
        <div className="flex items-center gap-2">
          <a
            href={aiRuntimeExportHref('yaml')}
            className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
            data-testid="ai-runtime-export-yaml"
          >
            {t('aiRuntime.actions.exportYaml')}
          </a>
        </div>
      </header>

      <div
        className="rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-secondary"
        data-testid="ai-runtime-permission"
        data-can-manage={canManage ? 'true' : 'false'}
      >
        {canManage
          ? t('aiRuntime.permissions.manage', { role: role ?? t('aiRuntime.permissions.unknownRole') })
          : t('aiRuntime.permissions.readOnly', { role: role ?? t('aiRuntime.permissions.unknownRole') })}
      </div>

      {catalog.isLoading && (
        <div className="space-y-2" data-testid="ai-runtime-loading">
          <Skeleton height="3rem" />
          <Skeleton height="12rem" />
        </div>
      )}
      {catalog.isError && (
        <p className="text-sm text-danger" role="alert" data-testid="ai-runtime-error">
          {(catalog.error as Error).message}
        </p>
      )}
      {catalog.isSuccess && (
        <>
          <div className="grid gap-3 md:grid-cols-4">
            <Summary label={t('aiRuntime.summary.revision')} value={String(catalog.data.revision)} />
            <Summary label={t('aiRuntime.tabs.profiles')} value={String(catalog.data.profiles.length)} />
            <Summary label={t('aiRuntime.tabs.models')} value={String(catalog.data.models.length)} />
            <Summary label={t('aiRuntime.tabs.clis')} value={String(catalog.data.clis.length)} />
          </div>
          <div className="rounded-lg border border-border-base bg-bg-elevated" data-testid="ai-runtime-catalog">
            <div
              role="tablist"
              aria-label={t('aiRuntime.tabs.aria')}
              className="flex gap-1 border-b border-border-base px-3 pt-2"
            >
              {(['profiles', 'models', 'clis'] as const).map((key) => (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={tab === key}
                  data-testid={`ai-runtime-tab-${key}`}
                  onClick={() => setTab(key)}
                  className={[
                    '-mb-px border-b-2 px-3 py-2 text-sm font-medium',
                    tab === key
                      ? 'border-accent text-text-primary'
                      : 'border-transparent text-text-muted hover:text-text-primary',
                  ].join(' ')}
                >
                  {t(`aiRuntime.tabs.${key}`)}
                </button>
              ))}
            </div>
            <div className="p-3">
              {tab === 'profiles' && (
                <ProfilesTable
                  rows={catalog.data.profiles}
                  defaultProfileId={catalog.data.default_runtime_profile_id}
                  defaultProfileName={defaultProfile?.name}
                  revision={catalog.data.revision}
                  canManage={canManage}
                />
              )}
              {tab === 'models' && <ModelsTable rows={catalog.data.models} />}
              {tab === 'clis' && <CLIsTable rows={catalog.data.clis} />}
            </div>
          </div>
        </>
      )}
    </section>
  );
}

function Summary({ label, value }: { label: string; value: string }): React.ReactElement {
  return (
    <div className="rounded border border-border-base bg-bg-elevated p-3">
      <div className="text-xs uppercase tracking-wide text-text-muted">{label}</div>
      <div className="mt-1 text-lg font-semibold text-text-primary">{value}</div>
    </div>
  );
}

function ProfilesTable({
  rows,
  defaultProfileId,
  defaultProfileName,
  revision,
  canManage,
}: {
  rows: RuntimeProfile[];
  defaultProfileId?: string;
  defaultProfileName?: string;
  revision: number;
  canManage: boolean;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const setDefault = useSetDefaultRuntimeProfile();
  if (rows.length === 0) {
    return <EmptyState testId="ai-runtime-empty-profiles" title={t('aiRuntime.empty.profiles')} body={t('aiRuntime.empty.profilesBody')} />;
  }
  return (
    <div className="overflow-x-auto">
      {defaultProfileName && (
        <p className="mb-2 text-xs text-text-muted" data-testid="ai-runtime-default-profile">
          {t('aiRuntime.defaultProfile', { name: defaultProfileName })}
        </p>
      )}
      <table className="w-full min-w-[44rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr className="border-b border-border-base">
            <th className="px-3 py-2">{t('aiRuntime.profile.name')}</th>
            <th className="px-3 py-2">{t('aiRuntime.profile.cli')}</th>
            <th className="px-3 py-2">{t('aiRuntime.profile.model')}</th>
            <th className="px-3 py-2">{t('aiRuntime.profile.parameters')}</th>
            <th className="px-3 py-2">{t('aiRuntime.profile.status')}</th>
            {canManage && <th className="px-3 py-2 text-right">{t('aiRuntime.profile.actions')}</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((p) => {
            const isDefault = p.id === defaultProfileId;
            return (
              <tr key={p.id} className="border-b border-border-base last:border-0" data-testid="ai-runtime-profile-row">
                <td className="px-3 py-2">
                  <div className="font-medium text-text-primary">{p.name}</div>
                  <div className="font-mono text-xs text-text-muted">{p.key}</div>
                  {isDefault && (
                    <span className="mt-1 inline-flex rounded-full bg-brand/10 px-2 py-0.5 text-xs font-medium text-brand">
                      {t('aiRuntime.profile.default')}
                    </span>
                  )}
                </td>
                <td className="px-3 py-2 font-mono text-xs text-text-secondary">{p.cli_key}</td>
                <td className="px-3 py-2 font-mono text-xs text-text-secondary">{p.model_key}</td>
                <td className="px-3 py-2 font-mono text-xs text-text-muted">{paramCount(p.parameters)}</td>
                <td className="px-3 py-2">
                  <Status enabled={p.enabled} />
                </td>
                {canManage && (
                  <td className="px-3 py-2 text-right">
                    <button
                      type="button"
                      className="text-xs text-accent hover:underline disabled:text-text-muted disabled:no-underline"
                      disabled={isDefault || !p.enabled || setDefault.isPending}
                      data-testid="ai-runtime-set-default"
                      onClick={() => setDefault.mutate({ profileId: p.id, expectedRevision: revision })}
                    >
                      {t('aiRuntime.profile.setDefault')}
                    </button>
                  </td>
                )}
              </tr>
            );
          })}
        </tbody>
      </table>
      {setDefault.isError && (
        <p className="mt-2 text-xs text-danger" role="alert" data-testid="ai-runtime-set-default-error">
          {(setDefault.error as Error).message}
        </p>
      )}
    </div>
  );
}

function ModelsTable({ rows }: { rows: RuntimeModel[] }): React.ReactElement {
  const { t } = useTranslation('admin');
  if (rows.length === 0) {
    return <EmptyState testId="ai-runtime-empty-models" title={t('aiRuntime.empty.models')} body={t('aiRuntime.empty.modelsBody')} />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[48rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr className="border-b border-border-base">
            <th className="px-3 py-2">{t('aiRuntime.model.name')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.modelKey')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.compatibleCli')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.context')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.cost')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.status')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((m) => (
            <tr key={m.id} className="border-b border-border-base last:border-0" data-testid="ai-runtime-model-row">
              <td className="px-3 py-2">
                <div className="font-medium text-text-primary">{m.display_name}</div>
                <div className="font-mono text-xs text-text-muted">{m.key}</div>
              </td>
              <td className="px-3 py-2 font-mono text-xs text-text-secondary">{m.model_key}</td>
              <td className="px-3 py-2">
                <KeyList values={m.compatible_cli_keys ?? []} />
              </td>
              <td className="px-3 py-2 text-text-secondary">{m.context_window ? m.context_window.toLocaleString() : '—'}</td>
              <td className="px-3 py-2 text-text-secondary">
                {m.input_cost_per_mtok ?? '—'} / {m.output_cost_per_mtok ?? '—'}
              </td>
              <td className="px-3 py-2">
                <Status enabled={m.enabled} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CLIsTable({ rows }: { rows: RuntimeCLI[] }): React.ReactElement {
  const { t } = useTranslation('admin');
  if (rows.length === 0) {
    return <EmptyState testId="ai-runtime-empty-clis" title={t('aiRuntime.empty.clis')} body={t('aiRuntime.empty.clisBody')} />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[44rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr className="border-b border-border-base">
            <th className="px-3 py-2">{t('aiRuntime.cli.name')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.executable')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.version')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.features')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.status')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((cli) => (
            <tr key={cli.id} className="border-b border-border-base last:border-0" data-testid="ai-runtime-cli-row">
              <td className="px-3 py-2">
                <div className="font-medium text-text-primary">{cli.display_name}</div>
                <div className="font-mono text-xs text-text-muted">{cli.key}</div>
              </td>
              <td className="px-3 py-2 font-mono text-xs text-text-secondary">{cli.executable}</td>
              <td className="px-3 py-2 text-text-secondary">{cli.version_constraint || '—'}</td>
              <td className="px-3 py-2">
                <KeyList values={cli.required_features ?? []} />
              </td>
              <td className="px-3 py-2">
                <Status enabled={cli.enabled} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function Status({ enabled }: { enabled: boolean }): React.ReactElement {
  const { t } = useTranslation('admin');
  return (
    <span
      className={[
        'inline-flex rounded-full px-2 py-0.5 text-xs font-medium',
        enabled
          ? 'bg-status-emerald-bg text-status-emerald-fg'
          : 'bg-bg-subtle text-text-muted',
      ].join(' ')}
    >
      {enabled ? t('aiRuntime.status.enabled') : t('aiRuntime.status.disabled')}
    </span>
  );
}

function KeyList({ values }: { values: string[] }): React.ReactElement {
  if (values.length === 0) return <span className="text-text-muted">—</span>;
  return (
    <span className="flex flex-wrap gap-1">
      {values.map((v) => (
        <span key={v} className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary">
          {v}
        </span>
      ))}
    </span>
  );
}

function paramCount(parameters: Record<string, unknown> | undefined): string {
  const count = Object.keys(parameters ?? {}).length;
  return count === 0 ? '—' : String(count);
}
