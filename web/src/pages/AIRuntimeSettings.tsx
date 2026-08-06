import type React from 'react';
import { useEffect, useMemo, useState } from 'react';
import {
  useAIRuntimeAudit,
  useAIRuntimeBasicCoverage,
  useAIRuntimeCatalog,
  useAIRuntimeImpact,
  useSetDefaultRuntimeProfile,
  useUpdateRuntimeProfile,
  type RuntimeProfile,
} from '@/api/aiRuntime';

export default function AIRuntimeSettings(): React.ReactElement {
  const catalog = useAIRuntimeCatalog();
  const coverage = useAIRuntimeBasicCoverage();
  const audit = useAIRuntimeAudit(20);
  const profiles = catalog.data?.profiles ?? [];
  const defaultProfileID = catalog.data?.default_runtime_profile_id ?? '';
  const [defaultDraft, setDefaultDraft] = useState(defaultProfileID);
  const [selectedProfileID, setSelectedProfileID] = useState('');
  const [profileEnabled, setProfileEnabled] = useState(true);
  const [canaryPercent, setCanaryPercent] = useState(0);

  const selectedProfile = profiles.find((p) => p.id === selectedProfileID) ?? profiles[0];
  const defaultImpact = useAIRuntimeImpact({
    entity_type: 'default_profile',
    entity_id: defaultDraft,
    action: 'default_profile_changed',
    canary_percent: canaryPercent,
  });
  const profileImpact = useAIRuntimeImpact({
    entity_type: 'profile',
    entity_id: selectedProfile?.id,
    action: 'updated',
    canary_percent: canaryPercent,
  });
  const setDefault = useSetDefaultRuntimeProfile();
  const updateProfile = useUpdateRuntimeProfile();

  useEffect(() => {
    setDefaultDraft(defaultProfileID);
  }, [defaultProfileID]);

  useEffect(() => {
    if (!selectedProfile && profiles[0]) {
      setSelectedProfileID(profiles[0].id);
      return;
    }
    if (selectedProfile) {
      setProfileEnabled(selectedProfile.enabled);
    }
  }, [selectedProfile?.id, selectedProfile?.enabled, profiles.length]);

  const selectedCoverage = useMemo(() => {
    const rows = coverage.data?.basic_capability_coverage ?? [];
    return rows.filter((row) => !selectedProfile || row.profile_id === selectedProfile.id);
  }, [coverage.data, selectedProfile?.id]);

  if (catalog.isLoading) {
    return <div className="text-sm text-text-muted">Loading AI Runtime catalog...</div>;
  }

  if (catalog.isError) {
    return <div className="rounded border border-danger/40 bg-danger/10 p-3 text-sm text-danger">{(catalog.error as Error).message}</div>;
  }

  return (
    <div className="space-y-5 text-text-primary" data-testid="ai-runtime-settings">
      <div>
        <h1 className="text-xl font-semibold">AI Runtime</h1>
        <p className="mt-1 text-sm text-text-secondary">Catalog revision {catalog.data?.revision ?? 0}</p>
      </div>

      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <div className="flex flex-wrap items-end gap-3">
          <div className="min-w-64 flex-1">
            <label className="mb-1 block text-xs font-medium text-text-primary" htmlFor="ai-runtime-default-profile">Organization Default Profile</label>
            <select
              id="ai-runtime-default-profile"
              className={inputClass}
              value={defaultDraft}
              onChange={(e) => setDefaultDraft(e.target.value)}
              data-testid="ai-runtime-default-profile"
            >
              {profiles.filter((p) => p.enabled).map((profile) => (
                <option key={profile.id} value={profile.id}>{profile.name || profile.key}</option>
              ))}
            </select>
          </div>
          <div className="w-36">
            <label className="mb-1 block text-xs font-medium text-text-primary" htmlFor="ai-runtime-canary">Canary %</label>
            <input
              id="ai-runtime-canary"
              className={inputClass}
              type="number"
              min={0}
              max={100}
              value={canaryPercent}
              onChange={(e) => setCanaryPercent(Math.max(0, Math.min(100, Number(e.target.value) || 0)))}
              data-testid="ai-runtime-canary"
            />
          </div>
          <button
            type="button"
            className="rounded bg-brand px-3 py-2 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            disabled={!defaultDraft || defaultDraft === defaultProfileID || setDefault.isPending}
            onClick={() => {
              if (!catalog.data) return;
              setDefault.mutate({ expected_revision: catalog.data.revision, profile_id: defaultDraft });
            }}
            data-testid="ai-runtime-apply-default"
          >
            Apply Default
          </button>
        </div>
        <ImpactPanel title="Default Impact Preview" impact={defaultImpact.data} loading={defaultImpact.isLoading} />
      </section>

      <section className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(22rem,0.85fr)]">
        <div className="rounded border border-border-base bg-bg-elevated p-4">
          <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">Profiles</h2>
          <div className="space-y-2">
            {profiles.map((profile) => (
              <button
                type="button"
                key={profile.id}
                onClick={() => setSelectedProfileID(profile.id)}
                className={`block w-full rounded border px-3 py-2 text-left text-sm ${selectedProfile?.id === profile.id ? 'border-brand bg-brand/10' : 'border-border-base bg-bg-subtle hover:bg-bg-base'}`}
              >
                <span className="font-medium">{profile.name || profile.key}</span>
                <span className="ml-2 font-mono text-xs text-text-muted">{profile.cli_key} / {profile.model_key}</span>
                <span className={`float-right text-xs ${profile.enabled ? 'text-status-green-fg' : 'text-text-muted'}`}>
                  {profile.enabled ? 'enabled' : 'disabled'}
                </span>
              </button>
            ))}
          </div>
          {selectedProfile && (
            <div className="mt-4 border-t border-border-base pt-4">
              <label className="inline-flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  checked={profileEnabled}
                  onChange={(e) => setProfileEnabled(e.target.checked)}
                  data-testid="ai-runtime-profile-enabled"
                />
                Enabled
              </label>
              <button
                type="button"
                className="ml-3 rounded border border-border-base px-3 py-1.5 text-sm hover:bg-bg-subtle disabled:opacity-50"
                disabled={!catalog.data || profileEnabled === selectedProfile.enabled || updateProfile.isPending}
                onClick={() => {
                  if (!catalog.data) return;
                  updateProfile.mutate({
                    expected_revision: catalog.data.revision,
                    profile: { ...selectedProfile, enabled: profileEnabled },
                  });
                }}
                data-testid="ai-runtime-save-profile"
              >
                Save Profile State
              </button>
              <ImpactPanel title="Profile Impact Preview" impact={profileImpact.data} loading={profileImpact.isLoading} />
            </div>
          )}
        </div>

        <div className="rounded border border-border-base bg-bg-elevated p-4">
          <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">Basic Capability Coverage</h2>
          <p className="mb-3 text-xs text-text-secondary">{coverage.data?.effective_schedulability_note}</p>
          <div className="space-y-2">
            {(selectedCoverage.length > 0 ? selectedCoverage : coverage.data?.basic_capability_coverage ?? []).map((row) => (
              <div key={`${row.profile_id}-${row.calculated_at}`} className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-sm">
                <div className="flex items-center justify-between gap-3">
                  <span className="font-mono text-xs">{profileName(profiles, row.profile_id)}</span>
                  <span className="text-xs uppercase text-text-muted">{row.status}</span>
                </div>
                <div className="mt-1 text-xs text-text-secondary">
                  online {row.online_worker_count} · eligible {row.eligible_worker_count}
                </div>
              </div>
            ))}
            {coverage.isLoading && <p className="text-sm text-text-muted">Loading coverage...</p>}
            {(coverage.data?.diagnostics ?? []).map((diag) => (
              <p key={`${diag.code}-${diag.path}`} className="text-xs text-status-amber-fg">{diag.message}</p>
            ))}
            {!coverage.isLoading && (coverage.data?.basic_capability_coverage.length ?? 0) === 0 && (
              <p className="text-sm text-text-muted">No worker capability coverage has been reported.</p>
            )}
          </div>
        </div>
      </section>

      <section className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <CatalogList title="CLIs" rows={(catalog.data?.clis ?? []).map((cli) => ({
          id: cli.id,
          primary: cli.display_name || cli.key,
          secondary: `${cli.key} · ${cli.executable}`,
          enabled: cli.enabled,
        }))} />
        <CatalogList title="Models" rows={(catalog.data?.models ?? []).map((model) => ({
          id: model.id,
          primary: model.display_name || model.model_key,
          secondary: `${model.key} · ${model.compatible_cli_keys.join(', ')}`,
          enabled: model.enabled,
        }))} />
      </section>

      <section className="rounded border border-border-base bg-bg-elevated p-4">
        <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">Audit</h2>
        <div className="space-y-2">
          {(audit.data ?? []).map((event) => (
            <div key={`${event.id ?? event.ID}-${event.revision ?? event.Revision}`} className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-sm">
              <span className="font-medium">{event.action ?? event.Action}</span>
              <span className="ml-2 font-mono text-xs text-text-muted">{event.entity_type ?? event.EntityType}:{event.entity_key ?? event.EntityKey}</span>
              <span className="float-right text-xs text-text-muted">rev {event.revision ?? event.Revision}</span>
            </div>
          ))}
          {audit.isLoading && <p className="text-sm text-text-muted">Loading audit...</p>}
          {!audit.isLoading && (audit.data?.length ?? 0) === 0 && <p className="text-sm text-text-muted">No runtime audit events yet.</p>}
        </div>
      </section>
    </div>
  );
}

function ImpactPanel({
  title,
  impact,
  loading,
}: {
  title: string;
  impact?: ReturnType<typeof useAIRuntimeImpact>['data'];
  loading: boolean;
}): React.ReactElement {
  return (
    <div className="mt-4 rounded border border-border-base bg-bg-subtle p-3" data-testid="ai-runtime-impact">
      <div className="mb-2 flex items-center justify-between gap-3">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-text-muted">{title}</h3>
        <span className="text-xs text-text-secondary">{loading ? 'loading' : `${impact?.reference_count ?? 0} references`}</span>
      </div>
      <p className="text-xs text-text-secondary">{impact?.historical_snapshot_immutability}</p>
      <p className="mt-1 text-xs text-text-secondary">{impact?.effective_schedulability_note}</p>
      {(impact?.diagnostics ?? []).map((diag) => (
        <p key={`${diag.code}-${diag.path}`} className="mt-1 text-xs text-status-amber-fg">{diag.message}</p>
      ))}
      <div className="mt-2 space-y-1">
        {(impact?.references ?? []).slice(0, 6).map((ref) => (
          <div key={`${ref.entity_type}-${ref.entity_id}-${ref.field}`} className="text-xs text-text-muted">
            {ref.entity_type} · {ref.entity_name || ref.entity_id} · {ref.field}
          </div>
        ))}
      </div>
    </div>
  );
}

function CatalogList({
  title,
  rows,
}: {
  title: string;
  rows: Array<{ id: string; primary: string; secondary: string; enabled: boolean }>;
}): React.ReactElement {
  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4">
      <h2 className="mb-3 text-sm font-semibold uppercase tracking-wide text-text-muted">{title}</h2>
      <div className="space-y-2">
        {rows.map((row) => (
          <div key={row.id} className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-sm">
            <span className="font-medium">{row.primary}</span>
            <span className="ml-2 font-mono text-xs text-text-muted">{row.secondary}</span>
            <span className={`float-right text-xs ${row.enabled ? 'text-status-green-fg' : 'text-text-muted'}`}>
              {row.enabled ? 'enabled' : 'disabled'}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}

function profileName(profiles: RuntimeProfile[], id: string): string {
  const profile = profiles.find((p) => p.id === id);
  return profile?.name || profile?.key || id;
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
