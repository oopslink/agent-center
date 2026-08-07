import React, { useMemo, useState } from 'react';
import {
  useAIRuntimeCatalog,
  useAIRuntimeCoverage,
  useCreateRuntimeProfile,
  useSetDefaultRuntimeProfile,
  useUpdateRuntimeProfile,
  type RuntimeCatalog,
  type RuntimeCoverage,
  type RuntimeImpactPreview,
  type RuntimeModel,
  type RuntimeProfile,
} from '@/api/aiRuntime';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { ToggleSwitch } from '@/components/ToggleSwitch';

type EditingProfile = RuntimeProfile | null;

export default function OrgAIRuntime(): React.ReactElement {
  const catalog = useAIRuntimeCatalog();
  const coverage = useAIRuntimeCoverage();
  const setDefault = useSetDefaultRuntimeProfile();
  const [editing, setEditing] = useState<EditingProfile>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [lastImpact, setLastImpact] = useState<RuntimeImpactPreview | null>(null);

  const profiles = catalog.data?.profiles ?? [];
  const defaultProfile = profiles.find((p) => p.id === catalog.data?.default_runtime_profile_id);

  const makeDefault = async (profileID: string) => {
    if (!catalog.data) return;
    const resp = await setDefault.mutateAsync({
      expected_revision: catalog.data.revision,
      profile_id: profileID,
    });
    setLastImpact(resp.impact_preview ?? null);
  };

  return (
    <section className="space-y-5" data-testid="page-OrgAIRuntime">
      <header className="flex flex-col gap-3 md:flex-row md:items-start md:justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">AI Runtime</h1>
          <p className="mt-1 text-xs text-text-muted">
            Default: <span className="font-medium text-text-secondary">{defaultProfile?.name ?? 'Not configured'}</span>
          </p>
        </div>
        <button
          type="button"
          className="self-start rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
          onClick={() => {
            setEditing(null);
            setFormOpen(true);
          }}
          disabled={!catalog.data || catalog.data.clis.length === 0 || catalog.data.models.length === 0}
          data-testid="ai-runtime-add-btn"
        >
          New profile
        </button>
      </header>

      {catalog.isLoading && (
        <div className="space-y-2" data-testid="ai-runtime-loading">
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
        </div>
      )}

      {catalog.isError && (
        <p className="text-sm text-danger" data-testid="ai-runtime-error">
          {(catalog.error as Error).message}
        </p>
      )}

      {catalog.isSuccess && (
        <>
          <RuntimeProfiles
            catalog={catalog.data}
            busyDefault={setDefault.isPending}
            onEdit={(profile) => {
              setEditing(profile);
              setFormOpen(true);
            }}
            onDefault={(profileID) => void makeDefault(profileID)}
          />
          <RuntimeCoveragePanel coverage={coverage.data?.coverage ?? []} loading={coverage.isLoading} diagnostics={coverage.data?.diagnostics ?? []} profiles={profiles} />
          <RuntimeInventory catalog={catalog.data} />
        </>
      )}

      {lastImpact && <ImpactPreview preview={lastImpact} onDismiss={() => setLastImpact(null)} />}

      {formOpen && catalog.data && (
        <ProfileFormModal
          catalog={catalog.data}
          profile={editing}
          onImpact={setLastImpact}
          onClose={() => setFormOpen(false)}
        />
      )}
    </section>
  );
}

function RuntimeProfiles({
  catalog,
  busyDefault,
  onEdit,
  onDefault,
}: {
  catalog: RuntimeCatalog;
  busyDefault: boolean;
  onEdit: (profile: RuntimeProfile) => void;
  onDefault: (profileID: string) => void;
}): React.ReactElement {
  if (catalog.profiles.length === 0) {
    return <EmptyState testId="ai-runtime-empty" title="No runtime profiles" body="Create a profile after CLIs and models are present." />;
  }
  return (
    <div className="overflow-x-auto rounded-lg border border-border-base" data-testid="ai-runtime-list">
      <table className="w-full text-sm">
        <thead className="bg-bg-subtle text-left text-xs uppercase text-text-muted">
          <tr>
            <th className="px-3 py-2">Profile</th>
            <th className="px-3 py-2">CLI</th>
            <th className="px-3 py-2">Model</th>
            <th className="px-3 py-2">State</th>
            <th className="px-3 py-2" />
          </tr>
        </thead>
        <tbody>
          {catalog.profiles.map((profile) => {
            const isDefault = profile.id === catalog.default_runtime_profile_id;
            return (
              <tr key={profile.id} className="border-t border-border-base" data-testid="ai-runtime-row" data-profile-id={profile.id}>
                <td className="px-3 py-2">
                  <div className="font-medium text-text-primary">{profile.name}</div>
                  <div className="font-mono text-xs text-text-muted">{profile.key}</div>
                </td>
                <td className="px-3 py-2 font-mono text-xs text-text-secondary">{profile.cli_key}</td>
                <td className="px-3 py-2 font-mono text-xs text-text-secondary">{profile.model_key}</td>
                <td className="px-3 py-2">
                  <span className={`rounded px-2 py-1 text-xs ${profile.enabled ? 'bg-status-green-bg text-status-green-fg' : 'bg-bg-subtle text-text-muted'}`}>
                    {profile.enabled ? 'Enabled' : 'Disabled'}
                  </span>
                  {isDefault && (
                    <span className="ml-2 rounded bg-brand/10 px-2 py-1 text-xs font-medium text-brand" data-testid="runtime-default-badge">
                      Default
                    </span>
                  )}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-right">
                  <button type="button" className="text-xs text-accent hover:underline" onClick={() => onEdit(profile)} data-testid="ai-runtime-edit">
                    Edit
                  </button>
                  {!isDefault && profile.enabled && (
                    <button
                      type="button"
                      className="ml-3 text-xs text-accent hover:underline disabled:text-text-muted"
                      disabled={busyDefault}
                      onClick={() => onDefault(profile.id)}
                      data-testid="runtime-set-default"
                    >
                      Make default
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function RuntimeCoveragePanel({
  coverage,
  loading,
  diagnostics,
  profiles,
}: {
  coverage: RuntimeCoverage[];
  loading: boolean;
  diagnostics: Array<{ message: string; severity?: string }>;
  profiles: RuntimeProfile[];
}): React.ReactElement {
  const profileNames = useMemo(() => new Map(profiles.map((p) => [p.id, p.name])), [profiles]);
  return (
    <section className="space-y-2" data-testid="runtime-coverage-panel">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="text-sm font-semibold text-text-primary">Basic capability coverage</h2>
        <span className="rounded bg-bg-subtle px-2 py-1 text-xs text-text-muted">Effective schedulability: not inferred</span>
      </div>
      {loading ? (
        <Skeleton height="3rem" />
      ) : coverage.length > 0 ? (
        <div className="overflow-x-auto rounded-lg border border-border-base">
          <table className="w-full text-sm">
            <thead className="bg-bg-subtle text-left text-xs uppercase text-text-muted">
              <tr>
                <th className="px-3 py-2">Profile</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Eligible</th>
                <th className="px-3 py-2">Online</th>
              </tr>
            </thead>
            <tbody>
              {coverage.map((row) => (
                <tr key={row.profile_id} className="border-t border-border-base" data-testid="runtime-coverage-row">
                  <td className="px-3 py-2 text-text-primary">{profileNames.get(row.profile_id) ?? row.profile_id}</td>
                  <td className="px-3 py-2 text-text-secondary">{row.status}</td>
                  <td className="px-3 py-2 text-text-secondary">{row.eligible_worker_count}</td>
                  <td className="px-3 py-2 text-text-secondary">{row.online_worker_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <p className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-muted" data-testid="runtime-coverage-empty">
          No coverage data returned.
        </p>
      )}
      {diagnostics.map((d) => (
        <p key={`${d.severity ?? 'info'}:${d.message}`} className="text-xs text-text-muted" data-testid="runtime-coverage-diagnostic">
          {d.message}
        </p>
      ))}
    </section>
  );
}

function RuntimeInventory({ catalog }: { catalog: RuntimeCatalog }): React.ReactElement {
  return (
    <section className="grid gap-3 md:grid-cols-2" data-testid="runtime-inventory">
      <InventoryList title="CLIs" rows={catalog.clis.map((cli) => ({ key: cli.key, label: cli.display_name, meta: cli.enabled ? cli.executable : 'disabled' }))} />
      <InventoryList title="Models" rows={catalog.models.map((model) => ({ key: model.key, label: model.display_name, meta: model.enabled ? model.model_key : 'disabled' }))} />
    </section>
  );
}

function InventoryList({ title, rows }: { title: string; rows: Array<{ key: string; label: string; meta: string }> }): React.ReactElement {
  return (
    <div className="rounded-lg border border-border-base">
      <div className="border-b border-border-base bg-bg-subtle px-3 py-2 text-xs font-semibold uppercase text-text-muted">{title}</div>
      <ul className="divide-y divide-border-base">
        {rows.map((row) => (
          <li key={row.key} className="flex items-center justify-between gap-3 px-3 py-2 text-sm">
            <span className="text-text-primary">{row.label}</span>
            <span className="font-mono text-xs text-text-muted">{row.meta}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ProfileFormModal({
  catalog,
  profile,
  onImpact,
  onClose,
}: {
  catalog: RuntimeCatalog;
  profile: RuntimeProfile | null;
  onImpact: (preview: RuntimeImpactPreview | null) => void;
  onClose: () => void;
}): React.ReactElement {
  const enabledCLIs = catalog.clis.filter((cli) => cli.enabled);
  const firstCLI = enabledCLIs[0]?.key ?? '';
  const initialCLI = profile?.cli_key || firstCLI;
  const initialModel = profile?.model_key || firstCompatibleModel(catalog.models, initialCLI)?.key || '';
  const [key, setKey] = useState(profile?.key ?? '');
  const [name, setName] = useState(profile?.name ?? '');
  const [description, setDescription] = useState(profile?.description ?? '');
  const [cliKey, setCLIKey] = useState(initialCLI);
  const [modelKey, setModelKey] = useState(initialModel);
  const [enabled, setEnabled] = useState(profile?.enabled ?? true);
  const [parametersText, setParametersText] = useState(JSON.stringify(profile?.parameters ?? {}, null, 2));
  const [parseError, setParseError] = useState<string | null>(null);
  const create = useCreateRuntimeProfile();
  const update = useUpdateRuntimeProfile(profile?.id ?? '');
  const mutation = profile ? update : create;
  const models = catalog.models.filter((model) => model.enabled && model.compatible_cli_keys.includes(cliKey));

  const submit = async () => {
    let parameters: Record<string, unknown>;
    try {
      const parsed = JSON.parse(parametersText || '{}') as unknown;
      if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
        throw new Error('parameters must be an object');
      }
      parameters = parsed as Record<string, unknown>;
      setParseError(null);
    } catch (err) {
      setParseError((err as Error).message);
      return;
    }
    try {
      const value = {
        ...(profile ? { id: profile.id } : {}),
        key: key.trim(),
        name: name.trim(),
        description: description.trim(),
        cli_key: cliKey,
        model_key: modelKey,
        parameters,
        enabled,
      };
      const resp = await mutation.mutateAsync({
        expected_revision: catalog.revision,
        value: value as RuntimeProfile,
      });
      onImpact(resp.impact_preview ?? null);
      onClose();
    } catch {
      /* surfaced below */
    }
  };

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid="ai-runtime-form">
      <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <h2 className="mb-3 text-lg font-semibold text-text-primary">{profile ? 'Edit runtime profile' : 'New runtime profile'}</h2>
        <div className="grid gap-3">
          <label className="block text-xs text-text-secondary">
            Key
            <input className={inputClass} value={key} disabled={!!profile} onChange={(e) => setKey(e.target.value)} data-testid="runtime-profile-key" />
          </label>
          <label className="block text-xs text-text-secondary">
            Name
            <input className={inputClass} value={name} onChange={(e) => setName(e.target.value)} data-testid="runtime-profile-name" />
          </label>
          <label className="block text-xs text-text-secondary">
            Description
            <input className={inputClass} value={description} onChange={(e) => setDescription(e.target.value)} data-testid="runtime-profile-description" />
          </label>
          <div className="grid gap-3 md:grid-cols-2">
            <label className="block text-xs text-text-secondary">
              CLI
              <select
                className={inputClass}
                value={cliKey}
                onChange={(e) => {
                  const nextCLI = e.target.value;
                  const nextModel = firstCompatibleModel(catalog.models, nextCLI)?.key ?? '';
                  setCLIKey(nextCLI);
                  setModelKey(nextModel);
                }}
                data-testid="runtime-profile-cli"
              >
                {enabledCLIs.map((cli) => (
                  <option key={cli.key} value={cli.key}>
                    {cli.display_name}
                  </option>
                ))}
              </select>
            </label>
            <label className="block text-xs text-text-secondary">
              Model
              <select className={inputClass} value={modelKey} onChange={(e) => setModelKey(e.target.value)} data-testid="runtime-profile-model">
                {models.map((model) => (
                  <option key={model.key} value={model.key}>
                    {model.display_name}
                  </option>
                ))}
              </select>
            </label>
          </div>
          <label className="inline-flex items-center gap-2 text-xs text-text-secondary">
            <ToggleSwitch
              checked={enabled}
              onChange={setEnabled}
              ariaLabel="Enabled"
              testId="runtime-profile-enabled"
            />
            Enabled
          </label>
          <label className="block text-xs text-text-secondary">
            Parameters
            <textarea
              className={`${inputClass} min-h-28 font-mono text-xs`}
              value={parametersText}
              onChange={(e) => setParametersText(e.target.value)}
              spellCheck={false}
              data-testid="runtime-profile-parameters"
            />
          </label>
        </div>
        {parseError && <p className="mt-2 text-xs text-danger" data-testid="runtime-profile-parse-error">{parseError}</p>}
        {mutation.isError && <p className="mt-2 text-xs text-danger" data-testid="runtime-profile-error">{(mutation.error as Error).message}</p>}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            Cancel
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            onClick={() => void submit()}
            disabled={mutation.isPending || !key.trim() || !name.trim() || !cliKey || !modelKey}
            data-testid="ai-runtime-form-save"
          >
            Save
          </button>
        </div>
      </div>
    </div>
  );
}

function ImpactPreview({ preview, onDismiss }: { preview: RuntimeImpactPreview; onDismiss: () => void }): React.ReactElement {
  const c = preview.reference_counts;
  return (
    <aside className="rounded-lg border border-border-base bg-bg-subtle p-3" data-testid="runtime-impact-preview">
      <div className="mb-2 flex items-center justify-between gap-3">
        <h2 className="text-sm font-semibold text-text-primary">Impact preview</h2>
        <button type="button" className="text-xs text-text-muted hover:text-text-primary" onClick={onDismiss}>
          Dismiss
        </button>
      </div>
      <div className="grid gap-2 text-xs text-text-secondary md:grid-cols-3">
        <Metric label="Affected new runs" value={preview.affected_new_runs} />
        <Metric label="Default profile" value={c.default_profile} />
        <Metric label="Executor candidates" value={c.executor_profile_selections} />
        <Metric label="Team profiles" value={c.team_role_profile_selections} />
        <Metric label="Team inherits" value={c.team_role_inherit_selections} />
        <Metric label="Historical snapshots" value={c.historical_execution_snapshot} />
      </div>
      <p className="mt-2 text-xs text-text-muted">{preview.historical_note}</p>
      <p className="mt-1 text-xs text-text-muted">Gray release ready: {preview.gray_release_ready ? 'yes' : 'no'}</p>
    </aside>
  );
}

function Metric({ label, value }: { label: string; value: number }): React.ReactElement {
  return (
    <span className="rounded border border-border-base bg-bg-elevated px-2 py-1">
      <span className="text-text-muted">{label}: </span>
      <span className="font-semibold text-text-primary">{value}</span>
    </span>
  );
}

function firstCompatibleModel(models: RuntimeModel[], cliKey: string): RuntimeModel | undefined {
  return models.find((model) => model.enabled && model.compatible_cli_keys.includes(cliKey));
}

const inputClass = 'mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm text-text-primary';
