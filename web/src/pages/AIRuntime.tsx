import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  useRuntimeAudit,
  useRuntimeCatalog,
  useRuntimeImpact,
  useSetRuntimeDefaultProfile,
  type RuntimeCoverage,
  type RuntimeProfile,
} from '@/api/aiRuntime';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';

export default function AIRuntime(): React.ReactElement {
  const { t } = useTranslation('admin');
  const catalog = useRuntimeCatalog();
  const audit = useRuntimeAudit();
  const setDefault = useSetRuntimeDefaultProfile();
  const profiles = catalog.data?.profiles ?? [];
  const defaultProfileId = catalog.data?.default_runtime_profile_id ?? '';
  const [selectedProfileId, setSelectedProfileId] = useState(defaultProfileId);
  const [canary, setCanary] = useState(false);
  const [canaryPercent, setCanaryPercent] = useState(10);
  const effectiveSelected = selectedProfileId || defaultProfileId || profiles[0]?.id || '';
  const impact = useRuntimeImpact('profile', effectiveSelected, 'set_default');

  React.useEffect(() => {
    if (!selectedProfileId && defaultProfileId) setSelectedProfileId(defaultProfileId);
  }, [defaultProfileId, selectedProfileId]);

  const basicCoverage = useMemo(
    () => (catalog.data?.coverage ?? []).filter((c) => c.scope === 'basic_capability_coverage'),
    [catalog.data?.coverage],
  );
  const mutationError = setDefault.error as Error | null;

  const submitDefault = async () => {
    if (!catalog.data || !effectiveSelected) return;
    await setDefault.mutateAsync({
      expected_revision: catalog.data.revision,
      profile_id: effectiveSelected,
      rollout: canary ? { enabled: true, label: 'canary', percent: canaryPercent } : undefined,
    });
  };

  return (
    <section className="space-y-4 text-text-primary" data-testid="page-AIRuntime">
      <header className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold">{t('aiRuntime.title')}</h1>
          <p className="max-w-3xl text-xs leading-5 text-text-muted">{t('aiRuntime.subtitle')}</p>
        </div>
        {catalog.data && (
          <div className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="ai-runtime-revision">
            {t('aiRuntime.revision', { revision: catalog.data.revision })}
          </div>
        )}
      </header>

      {catalog.isLoading && (
        <div className="space-y-2" data-testid="ai-runtime-loading">
          <Skeleton height="3rem" />
          <Skeleton height="8rem" />
        </div>
      )}
      {catalog.isError && (
        <p className="text-sm text-danger" data-testid="ai-runtime-error">
          {(catalog.error as Error).message}
        </p>
      )}
      {catalog.isSuccess && profiles.length === 0 && (
        <EmptyState testId="ai-runtime-empty" title={t('aiRuntime.empty.title')} body={t('aiRuntime.empty.body')} />
      )}

      {catalog.data && profiles.length > 0 && (
        <>
          <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ai-runtime-default-editor">
            <div className="grid gap-3 md:grid-cols-[minmax(0,1fr)_9rem_8rem_auto] md:items-end">
              <label className="text-xs font-medium text-text-secondary">
                {t('aiRuntime.defaultProfile')}
                <select
                  className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
                  value={effectiveSelected}
                  onChange={(e) => setSelectedProfileId(e.target.value)}
                  data-testid="ai-runtime-default-select"
                >
                  {profiles.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name || p.key}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex items-center gap-2 text-xs text-text-secondary">
                <input
                  type="checkbox"
                  checked={canary}
                  onChange={(e) => setCanary(e.target.checked)}
                  data-testid="ai-runtime-canary-toggle"
                />
                {t('aiRuntime.canary')}
              </label>
              <label className="text-xs font-medium text-text-secondary">
                {t('aiRuntime.canaryPercent')}
                <input
                  className="mt-1 w-full rounded border border-border-base bg-bg-base px-2 py-1.5 text-sm text-text-primary"
                  type="number"
                  min={1}
                  max={100}
                  disabled={!canary}
                  value={canaryPercent}
                  onChange={(e) => setCanaryPercent(Math.max(1, Math.min(100, Number(e.target.value) || 1)))}
                  data-testid="ai-runtime-canary-percent"
                />
              </label>
              <button
                type="button"
                className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
                disabled={setDefault.isPending || !effectiveSelected || effectiveSelected === defaultProfileId}
                onClick={() => void submitDefault()}
                data-testid="ai-runtime-default-save"
              >
                {setDefault.isPending ? t('aiRuntime.saving') : t('aiRuntime.saveDefault')}
              </button>
            </div>
            {mutationError && <p className="mt-2 text-xs text-danger">{mutationError.message}</p>}
          </section>

          <div className="grid gap-4 xl:grid-cols-[minmax(0,1.2fr)_minmax(20rem,0.8fr)]">
            <section className="rounded border border-border-base bg-bg-elevated" data-testid="ai-runtime-profiles">
              <div className="border-b border-border-base px-4 py-3">
                <h2 className="text-sm font-semibold">{t('aiRuntime.profiles')}</h2>
              </div>
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead className="bg-bg-subtle text-left text-xs uppercase text-text-muted">
                    <tr>
                      <th className="px-3 py-2">{t('aiRuntime.col.profile')}</th>
                      <th className="px-3 py-2">{t('aiRuntime.col.cli')}</th>
                      <th className="px-3 py-2">{t('aiRuntime.col.model')}</th>
                      <th className="px-3 py-2">{t('aiRuntime.col.coverage')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {profiles.map((p) => (
                      <tr key={p.id} className="border-t border-border-base">
                        <td className="px-3 py-2">
                          <span className="font-medium">{p.name || p.key}</span>
                          {p.id === defaultProfileId && (
                            <span className="ml-2 rounded bg-brand/10 px-1.5 py-0.5 text-[0.625rem] font-medium text-brand">
                              {t('aiRuntime.default')}
                            </span>
                          )}
                        </td>
                        <td className="px-3 py-2 font-mono text-xs">{p.cli_key}</td>
                        <td className="px-3 py-2 font-mono text-xs">{p.model_key}</td>
                        <td className="px-3 py-2">{coverageText(basicCoverage, p)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>

            <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ai-runtime-impact">
              <h2 className="text-sm font-semibold">{t('aiRuntime.impact')}</h2>
              {impact.isLoading && <p className="mt-2 text-xs text-text-muted">{t('aiRuntime.loadingImpact')}</p>}
              {impact.data && (
                <div className="mt-3 space-y-3 text-xs">
                  <div>
                    <div className="font-medium text-text-secondary">{t('aiRuntime.references')}</div>
                    {impact.data.reference_counts.length > 0 ? (
                      <ul className="mt-1 space-y-1">
                        {impact.data.reference_counts.map((r) => (
                          <li key={r.source} className="flex justify-between gap-3 rounded bg-bg-subtle px-2 py-1">
                            <span>{r.source}</span>
                            <span className="font-mono">{r.count}</span>
                          </li>
                        ))}
                      </ul>
                    ) : (
                      <p className="mt-1 text-text-muted">{t('aiRuntime.noReferences')}</p>
                    )}
                  </div>
                  <div className="rounded bg-bg-subtle px-2 py-2">
                    <div className="font-medium text-text-secondary">{t('aiRuntime.snapshotPolicy')}</div>
                    <p className="mt-1 text-text-muted">{impact.data.historical_snapshot_policy}</p>
                  </div>
                </div>
              )}
            </section>
          </div>

          <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ai-runtime-basic-coverage">
            <h2 className="text-sm font-semibold">{t('aiRuntime.basicCoverage')}</h2>
            <CoverageTable coverage={basicCoverage} empty={t('aiRuntime.noCoverage')} />
          </section>

          <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ai-runtime-schedulability-note">
            <h2 className="text-sm font-semibold">{t('aiRuntime.schedulability')}</h2>
            <p className="mt-2 text-xs leading-5 text-text-muted">{t('aiRuntime.schedulabilityUnavailable')}</p>
          </section>

          <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="ai-runtime-audit">
            <h2 className="text-sm font-semibold">{t('aiRuntime.audit')}</h2>
            {audit.data && audit.data.length > 0 ? (
              <ul className="mt-2 divide-y divide-border-base text-xs">
                {audit.data.map((ev) => (
                  <li key={ev.id} className="flex items-center justify-between gap-3 py-2">
                    <span>
                      <span className="font-medium">{ev.action}</span> {ev.entity_type}/{ev.entity_key}
                    </span>
                    <span className="font-mono text-text-muted">r{ev.revision}</span>
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-xs text-text-muted">{t('aiRuntime.noAudit')}</p>
            )}
          </section>
        </>
      )}
    </section>
  );
}

function CoverageTable({ coverage, empty }: { coverage: RuntimeCoverage[]; empty: string }): React.ReactElement {
  if (coverage.length === 0) {
    return <p className="mt-2 text-xs text-text-muted">{empty}</p>;
  }
  return (
    <div className="mt-2 overflow-x-auto">
      <table className="w-full text-xs">
        <thead className="text-left uppercase text-text-muted">
          <tr>
            <th className="py-1 pr-3">Profile</th>
            <th className="py-1 pr-3">Status</th>
            <th className="py-1 pr-3">Eligible</th>
            <th className="py-1 pr-3">Online</th>
          </tr>
        </thead>
        <tbody>
          {coverage.map((c) => (
            <tr key={c.profile_id} className="border-t border-border-base">
              <td className="py-1 pr-3 font-mono">{c.profile_id}</td>
              <td className="py-1 pr-3">{c.status}</td>
              <td className="py-1 pr-3">{c.eligible_worker_count}</td>
              <td className="py-1 pr-3">{c.online_worker_count}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function coverageText(coverage: RuntimeCoverage[], profile: RuntimeProfile): string {
  const item = coverage.find((c) => c.profile_id === profile.id);
  if (!item) return 'not measured';
  return `${item.status} (${item.eligible_worker_count}/${item.online_worker_count})`;
}
