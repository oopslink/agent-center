import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useOrgs } from '@/api/auth';
import {
  aiRuntimeExportHref,
  canManageAIRuntime,
  useAIRuntimeCatalog,
  useApplyAIRuntimeImport,
  useCreateRuntimeCLI,
  useCreateRuntimeModel,
  useCreateRuntimeProfile,
  usePreviewAIRuntimeImport,
  useSetDefaultRuntimeProfile,
  useUpdateRuntimeCLI,
  useUpdateRuntimeModel,
  useUpdateRuntimeProfile,
  type AIRuntimeCatalog,
  type RuntimeCLI,
  type RuntimeCLIInput,
  type RuntimeExportCLI,
  type RuntimeExportDocument,
  type RuntimeExportModel,
  type RuntimeExportProfile,
  type RuntimeModel,
  type RuntimeModelInput,
  type RuntimeProfile,
  type RuntimeProfileInput,
} from '@/api/aiRuntime';
import { currentOrgSlug } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';

type RuntimeTab = 'profiles' | 'models' | 'clis';
type FormTarget =
  | { kind: 'profiles'; entry?: RuntimeProfile }
  | { kind: 'models'; entry?: RuntimeModel }
  | { kind: 'clis'; entry?: RuntimeCLI };

const inputClass = 'mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm text-text-primary';
const textareaClass = 'mt-1 w-full rounded border border-border-base bg-bg-subtle px-2 py-1.5 font-mono text-xs text-text-primary';

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
  const [formTarget, setFormTarget] = useState<FormTarget | null>(null);
  const [importOpen, setImportOpen] = useState(false);

  const defaultProfile = useMemo(() => {
    const data = catalog.data;
    if (!data?.default_runtime_profile_id) return undefined;
    return data.profiles.find((p) => p.id === data.default_runtime_profile_id);
  }, [catalog.data]);

  const openCreate = (kind: RuntimeTab) => setFormTarget({ kind });

  return (
    <section className="space-y-4" data-testid="page-AiRuntime">
      <Breadcrumb
        items={[
          { label: t('systemNav.system') },
          { label: t('aiRuntime.title') },
        ]}
      />
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border-base pb-3">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('aiRuntime.title')}</h1>
          <p className="max-w-3xl text-sm text-text-muted">{t('aiRuntime.subtitle')}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {canManage && catalog.data && (
            <>
              <button
                type="button"
                className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
                onClick={() => setImportOpen(true)}
                data-testid="ai-runtime-import-models"
              >
                {t('aiRuntime.actions.importModels')}
              </button>
              <button
                type="button"
                className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
                onClick={() => openCreate(tab)}
                data-testid={`ai-runtime-add-${tab.slice(0, -1)}`}
              >
                {t(`aiRuntime.actions.add.${tab}`)}
              </button>
            </>
          )}
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
                  onEdit={(entry) => setFormTarget({ kind: 'profiles', entry })}
                />
              )}
              {tab === 'models' && (
                <ModelsTable
                  rows={catalog.data.models}
                  canManage={canManage}
                  onEdit={(entry) => setFormTarget({ kind: 'models', entry })}
                />
              )}
              {tab === 'clis' && (
                <CLIsTable
                  rows={catalog.data.clis}
                  canManage={canManage}
                  onEdit={(entry) => setFormTarget({ kind: 'clis', entry })}
                />
              )}
            </div>
          </div>
          {formTarget?.kind === 'profiles' && (
            <ProfileFormModal
              entry={formTarget.entry}
              revision={catalog.data.revision}
              clis={catalog.data.clis}
              models={catalog.data.models}
              onClose={() => setFormTarget(null)}
            />
          )}
          {formTarget?.kind === 'models' && (
            <ModelFormModal
              entry={formTarget.entry}
              revision={catalog.data.revision}
              clis={catalog.data.clis}
              onClose={() => setFormTarget(null)}
            />
          )}
          {formTarget?.kind === 'clis' && (
            <CLIFormModal
              entry={formTarget.entry}
              revision={catalog.data.revision}
              onClose={() => setFormTarget(null)}
            />
          )}
          {importOpen && (
            <ModelImportModal
              catalog={catalog.data}
              onClose={() => setImportOpen(false)}
            />
          )}
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
  onEdit,
}: {
  rows: RuntimeProfile[];
  defaultProfileId?: string;
  defaultProfileName?: string;
  revision: number;
  canManage: boolean;
  onEdit: (entry: RuntimeProfile) => void;
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
      <table className="w-full min-w-[48rem] text-left text-sm">
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
                    <div className="flex justify-end gap-3">
                      <button
                        type="button"
                        className="text-xs text-accent hover:underline"
                        data-testid="ai-runtime-edit-profile"
                        onClick={() => onEdit(p)}
                      >
                        {t('aiRuntime.actions.edit')}
                      </button>
                      <button
                        type="button"
                        className="text-xs text-accent hover:underline disabled:text-text-muted disabled:no-underline"
                        disabled={isDefault || !p.enabled || setDefault.isPending}
                        data-testid="ai-runtime-set-default"
                        onClick={() => setDefault.mutate({ profileId: p.id, expectedRevision: revision })}
                      >
                        {t('aiRuntime.profile.setDefault')}
                      </button>
                    </div>
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

function ModelsTable({
  rows,
  canManage,
  onEdit,
}: {
  rows: RuntimeModel[];
  canManage: boolean;
  onEdit: (entry: RuntimeModel) => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  if (rows.length === 0) {
    return <EmptyState testId="ai-runtime-empty-models" title={t('aiRuntime.empty.models')} body={t('aiRuntime.empty.modelsBody')} />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[52rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr className="border-b border-border-base">
            <th className="px-3 py-2">{t('aiRuntime.model.name')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.modelKey')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.compatibleCli')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.context')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.cost')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.status')}</th>
            {canManage && <th className="px-3 py-2 text-right">{t('aiRuntime.profile.actions')}</th>}
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
              {canManage && (
                <td className="px-3 py-2 text-right">
                  <button
                    type="button"
                    className="text-xs text-accent hover:underline"
                    data-testid="ai-runtime-edit-model"
                    onClick={() => onEdit(m)}
                  >
                    {t('aiRuntime.actions.edit')}
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CLIsTable({
  rows,
  canManage,
  onEdit,
}: {
  rows: RuntimeCLI[];
  canManage: boolean;
  onEdit: (entry: RuntimeCLI) => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  if (rows.length === 0) {
    return <EmptyState testId="ai-runtime-empty-clis" title={t('aiRuntime.empty.clis')} body={t('aiRuntime.empty.clisBody')} />;
  }
  return (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[48rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr className="border-b border-border-base">
            <th className="px-3 py-2">{t('aiRuntime.cli.name')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.executable')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.version')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.features')}</th>
            <th className="px-3 py-2">{t('aiRuntime.cli.status')}</th>
            {canManage && <th className="px-3 py-2 text-right">{t('aiRuntime.profile.actions')}</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((cli) => (
            <tr key={cli.id} className="border-b border-border-base last:border-0" data-testid="ai-runtime-cli-row">
              <td className="px-3 py-2">
                <div className="flex items-center gap-2">
                  <span className="font-medium text-text-primary">{cli.display_name}</span>
                  {cli.system && (
                    <span className="rounded-full bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-medium text-text-muted">
                      {t('aiRuntime.system.badge')}
                    </span>
                  )}
                </div>
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
              {canManage && (
                <td className="px-3 py-2 text-right">
                  <button
                    type="button"
                    className="text-xs text-accent hover:underline"
                    data-testid="ai-runtime-edit-cli"
                    onClick={() => onEdit(cli)}
                  >
                    {t('aiRuntime.actions.edit')}
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function ProfileFormModal({
  entry,
  revision,
  clis,
  models,
  onClose,
}: {
  entry?: RuntimeProfile;
  revision: number;
  clis: RuntimeCLI[];
  models: RuntimeModel[];
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const create = useCreateRuntimeProfile();
  const update = useUpdateRuntimeProfile(entry?.id ?? '');
  const [key, setKey] = useState(entry?.key ?? '');
  const [name, setName] = useState(entry?.name ?? '');
  const [description, setDescription] = useState(entry?.description ?? '');
  const [cliKey, setCLIKey] = useState(entry?.cli_key ?? firstEnabledKey(clis));
  const [modelKey, setModelKey] = useState(entry?.model_key ?? firstEnabledKey(models));
  const [parameters, setParameters] = useState(prettyJSON(entry?.parameters ?? {}));
  const [enabled, setEnabled] = useState(entry?.enabled ?? true);
  const [localError, setLocalError] = useState('');
  const mut = entry ? update : create;

  const submit = async () => {
    setLocalError('');
    let parsed: Record<string, unknown>;
    try {
      parsed = parseJSONRecord(parameters, t('aiRuntime.form.parameters'));
    } catch (err) {
      setLocalError((err as Error).message);
      return;
    }
    const value: RuntimeProfileInput = {
      key: key.trim(),
      name: name.trim(),
      description: description.trim(),
      cli_key: cliKey.trim(),
      model_key: modelKey.trim(),
      parameters: parsed,
      enabled,
    };
    try {
      await mut.mutateAsync({ expectedRevision: revision, value });
      onClose();
    } catch {
      /* surfaced below */
    }
  };

  return (
    <ModalFrame
      testId="ai-runtime-profile-form"
      title={entry ? t('aiRuntime.form.editProfile') : t('aiRuntime.form.addProfile')}
      error={localError || mutationMessage(mut.error)}
      busy={mut.isPending}
      saveDisabled={!key.trim() || !name.trim() || !cliKey.trim() || !modelKey.trim()}
      onClose={onClose}
      onSave={() => void submit()}
    >
      <TextField label={t('aiRuntime.form.key')} value={key} onChange={setKey} disabled={!!entry} testId="ai-runtime-profile-key" />
      <TextField label={t('aiRuntime.profile.name')} value={name} onChange={setName} testId="ai-runtime-profile-name" />
      <TextField label={t('aiRuntime.form.description')} value={description} onChange={setDescription} testId="ai-runtime-profile-description" />
      <SelectField label={t('aiRuntime.profile.cli')} value={cliKey} onChange={setCLIKey} options={clis.map((c) => c.key)} testId="ai-runtime-profile-cli" />
      <SelectField label={t('aiRuntime.profile.model')} value={modelKey} onChange={setModelKey} options={models.map((m) => m.key)} testId="ai-runtime-profile-model" />
      <JSONField label={t('aiRuntime.form.parameters')} value={parameters} onChange={setParameters} testId="ai-runtime-profile-parameters" />
      <CheckboxField label={t('aiRuntime.form.enabled')} checked={enabled} onChange={setEnabled} testId="ai-runtime-profile-enabled" />
      {entry && <p className="text-xs text-text-muted">{t('aiRuntime.form.immutableKey')}</p>}
    </ModalFrame>
  );
}

function ModelFormModal({
  entry,
  revision,
  clis,
  onClose,
}: {
  entry?: RuntimeModel;
  revision: number;
  clis: RuntimeCLI[];
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const create = useCreateRuntimeModel();
  const update = useUpdateRuntimeModel(entry?.id ?? '');
  const [key, setKey] = useState(entry?.key ?? '');
  const [modelKey, setModelKey] = useState(entry?.model_key ?? '');
  const [displayName, setDisplayName] = useState(entry?.display_name ?? '');
  const [compatible, setCompatible] = useState((entry?.compatible_cli_keys ?? [firstEnabledKey(clis)]).filter(Boolean).join(', '));
  const [params, setParams] = useState(prettyJSON(entry?.default_parameters ?? {}));
  const [enabled, setEnabled] = useState(entry?.enabled ?? true);
  const [contextWindow, setContextWindow] = useState(String(entry?.context_window ?? 0));
  const [inputCost, setInputCost] = useState(String(entry?.input_cost_per_mtok ?? 0));
  const [outputCost, setOutputCost] = useState(String(entry?.output_cost_per_mtok ?? 0));
  const [tier, setTier] = useState(entry?.tier ?? '');
  const [localError, setLocalError] = useState('');
  const mut = entry ? update : create;

  const submit = async () => {
    setLocalError('');
    let parsed: Record<string, unknown>;
    try {
      parsed = parseJSONRecord(params, t('aiRuntime.form.defaultParameters'));
    } catch (err) {
      setLocalError((err as Error).message);
      return;
    }
    const cliKeys = splitCSV(compatible);
    if (cliKeys.length === 0) {
      setLocalError(t('aiRuntime.form.compatibleRequired'));
      return;
    }
    const value: RuntimeModelInput = {
      key: key.trim(),
      model_key: modelKey.trim(),
      display_name: displayName.trim() || modelKey.trim(),
      compatible_cli_keys: cliKeys,
      default_parameters: parsed,
      enabled,
      context_window: parseNumber(contextWindow),
      input_cost_per_mtok: parseNumber(inputCost),
      output_cost_per_mtok: parseNumber(outputCost),
      tier: tier.trim(),
    };
    try {
      await mut.mutateAsync({ expectedRevision: revision, value });
      onClose();
    } catch {
      /* surfaced below */
    }
  };

  return (
    <ModalFrame
      testId="ai-runtime-model-form"
      title={entry ? t('aiRuntime.form.editModel') : t('aiRuntime.form.addModel')}
      error={localError || mutationMessage(mut.error)}
      busy={mut.isPending}
      saveDisabled={!key.trim() || !modelKey.trim()}
      onClose={onClose}
      onSave={() => void submit()}
    >
      <TextField label={t('aiRuntime.form.key')} value={key} onChange={setKey} disabled={!!entry} testId="ai-runtime-model-key" />
      <TextField label={t('aiRuntime.model.modelKey')} value={modelKey} onChange={setModelKey} testId="ai-runtime-model-model-key" />
      <TextField label={t('aiRuntime.model.name')} value={displayName} onChange={setDisplayName} testId="ai-runtime-model-display-name" />
      <TextField label={t('aiRuntime.model.compatibleCli')} value={compatible} onChange={setCompatible} testId="ai-runtime-model-compatible-cli" />
      <div className="grid gap-3 md:grid-cols-3">
        <TextField label={t('aiRuntime.model.context')} type="number" value={contextWindow} onChange={setContextWindow} testId="ai-runtime-model-context" />
        <TextField label={t('aiRuntime.form.inputCost')} type="number" value={inputCost} onChange={setInputCost} testId="ai-runtime-model-input-cost" />
        <TextField label={t('aiRuntime.form.outputCost')} type="number" value={outputCost} onChange={setOutputCost} testId="ai-runtime-model-output-cost" />
      </div>
      <TextField label={t('aiRuntime.form.tier')} value={tier} onChange={setTier} testId="ai-runtime-model-tier" />
      <JSONField label={t('aiRuntime.form.defaultParameters')} value={params} onChange={setParams} testId="ai-runtime-model-default-parameters" />
      <CheckboxField label={t('aiRuntime.form.enabled')} checked={enabled} onChange={setEnabled} testId="ai-runtime-model-enabled" />
      {entry && <p className="text-xs text-text-muted">{t('aiRuntime.form.immutableKey')}</p>}
    </ModalFrame>
  );
}

function CLIFormModal({
  entry,
  revision,
  onClose,
}: {
  entry?: RuntimeCLI;
  revision: number;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const create = useCreateRuntimeCLI();
  const update = useUpdateRuntimeCLI(entry?.id ?? '');
  const [key, setKey] = useState(entry?.key ?? '');
  const [displayName, setDisplayName] = useState(entry?.display_name ?? '');
  const [executable, setExecutable] = useState(entry?.executable ?? '');
  const [version, setVersion] = useState(entry?.version_constraint ?? '');
  const [features, setFeatures] = useState((entry?.required_features ?? []).join(', '));
  const [schema, setSchema] = useState(prettyJSON(entry?.parameter_schema ?? { type: 'object' }));
  const [enabled, setEnabled] = useState(entry?.enabled ?? true);
  const [localError, setLocalError] = useState('');
  const mut = entry ? update : create;

  const submit = async () => {
    setLocalError('');
    let parsedSchema: Record<string, unknown>;
    try {
      parsedSchema = parseJSONRecord(schema, t('aiRuntime.form.parameterSchema'));
    } catch (err) {
      setLocalError((err as Error).message);
      return;
    }
    const value: RuntimeCLIInput = {
      key: key.trim(),
      display_name: displayName.trim(),
      executable: executable.trim(),
      version_constraint: version.trim(),
      required_features: splitCSV(features),
      parameter_schema: parsedSchema,
      enabled,
    };
    try {
      await mut.mutateAsync({ expectedRevision: revision, value });
      onClose();
    } catch {
      /* surfaced below */
    }
  };

  return (
    <ModalFrame
      testId="ai-runtime-cli-form"
      title={entry ? t('aiRuntime.form.editCli') : t('aiRuntime.form.addCli')}
      error={localError || mutationMessage(mut.error)}
      busy={mut.isPending}
      saveDisabled={!key.trim() || !displayName.trim() || !executable.trim()}
      onClose={onClose}
      onSave={() => void submit()}
    >
      {entry?.system && (
        <p className="rounded border border-status-amber-border bg-status-amber-bg px-3 py-2 text-xs text-status-amber-fg" data-testid="ai-runtime-system-note">
          {t('aiRuntime.system.note')}
        </p>
      )}
      <TextField label={t('aiRuntime.form.key')} value={key} onChange={setKey} disabled={!!entry} testId="ai-runtime-cli-key" />
      <TextField label={t('aiRuntime.cli.name')} value={displayName} onChange={setDisplayName} testId="ai-runtime-cli-display-name" />
      <TextField label={t('aiRuntime.cli.executable')} value={executable} onChange={setExecutable} testId="ai-runtime-cli-executable" />
      <TextField label={t('aiRuntime.cli.version')} value={version} onChange={setVersion} testId="ai-runtime-cli-version" />
      <TextField label={t('aiRuntime.cli.features')} value={features} onChange={setFeatures} testId="ai-runtime-cli-features" />
      <JSONField label={t('aiRuntime.form.parameterSchema')} value={schema} onChange={setSchema} testId="ai-runtime-cli-parameter-schema" />
      <CheckboxField label={t('aiRuntime.form.enabled')} checked={enabled} onChange={setEnabled} testId="ai-runtime-cli-enabled" />
      {entry && <p className="text-xs text-text-muted">{t('aiRuntime.form.immutableKey')}</p>}
    </ModalFrame>
  );
}

function ModelImportModal({ catalog, onClose }: { catalog: AIRuntimeCatalog; onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('admin');
  const preview = usePreviewAIRuntimeImport();
  const apply = useApplyAIRuntimeImport();
  const [json, setJSON] = useState('');
  const [document, setDocument] = useState<RuntimeExportDocument | null>(null);
  const [clientError, setClientError] = useState('');
  const [applied, setApplied] = useState(false);
  const modelItems = (preview.data?.report.items ?? []).filter((item) => item.entity_type === 'model');
  const diagnostics = preview.data?.report.diagnostics ?? [];

  const runPreview = async () => {
    setClientError('');
    setApplied(false);
    let parsed: unknown;
    try {
      parsed = JSON.parse(json);
      const doc = buildModelImportDocument(catalog, normalizeImportedModels(parsed, catalog.clis[0]?.key ?? 'codex'));
      setDocument(doc);
      await preview.mutateAsync({ strategy: 'merge', document: doc });
    } catch (err) {
      setDocument(null);
      setClientError((err as Error).message);
    }
  };

  const runApply = async () => {
    if (!document || !preview.data?.validation_token) return;
    setClientError('');
    try {
      await apply.mutateAsync({
        strategy: 'merge',
        document,
        validationToken: preview.data.validation_token,
      });
      setApplied(true);
    } catch {
      /* surfaced below */
    }
  };

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid="ai-runtime-import-modal">
      <div className="max-h-[90vh] w-full max-w-2xl space-y-3 overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h2 className="text-lg font-semibold text-text-primary">{t('aiRuntime.import.title')}</h2>
            <p className="text-xs text-text-muted">{t('aiRuntime.import.help')}</p>
          </div>
          <button type="button" className="rounded px-2 py-1 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            {t('aiRuntime.form.close')}
          </button>
        </div>
        <p className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="ai-runtime-import-note">
          {t('aiRuntime.import.fullDocumentNote')}
        </p>
        <textarea
          rows={10}
          className={textareaClass}
          placeholder='[{"key":"gpt-5-mini","model_key":"gpt-5-mini","display_name":"GPT-5 mini","compatible_cli_keys":["codex"],"context_window":128000}]'
          value={json}
          onChange={(e) => {
            setJSON(e.target.value);
            setDocument(null);
            preview.reset();
            apply.reset();
            setApplied(false);
          }}
          data-testid="ai-runtime-import-json"
        />
        {(clientError || preview.isError || apply.isError) && (
          <p className="text-xs text-danger" role="alert" data-testid="ai-runtime-import-error">
            {clientError || mutationMessage(preview.error) || mutationMessage(apply.error)}
          </p>
        )}
        {preview.isSuccess && (
          <div className="space-y-2 rounded border border-border-base bg-bg-subtle p-3" data-testid="ai-runtime-import-preview-result">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <p className="text-sm font-medium text-text-primary">{t('aiRuntime.import.previewTitle')}</p>
              <span className="font-mono text-[0.6875rem] text-text-muted">{preview.data.document_sha256.slice(0, 12)}</span>
            </div>
            {modelItems.length === 0 ? (
              <p className="text-xs text-text-muted" data-testid="ai-runtime-import-no-model-changes">{t('aiRuntime.import.noModelChanges')}</p>
            ) : (
              <ul className="space-y-1">
                {modelItems.map((item) => (
                  <li key={`${item.key}-${item.action}`} className="flex justify-between gap-3 text-xs" data-testid="ai-runtime-import-item">
                    <span className="font-mono text-text-primary">{item.key}</span>
                    <span className="text-text-secondary">{item.action}</span>
                  </li>
                ))}
              </ul>
            )}
            {diagnostics.length > 0 && (
              <ul className="space-y-1">
                {diagnostics.map((d) => (
                  <li key={`${d.path ?? ''}-${d.message}`} className="text-xs text-danger" data-testid="ai-runtime-import-diagnostic">
                    {d.path ? `${d.path}: ` : ''}{d.message}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
        {applied && (
          <p className="text-xs text-status-emerald-fg" data-testid="ai-runtime-import-success">
            {t('aiRuntime.import.applied')}
          </p>
        )}
        <div className="flex justify-end gap-2 pt-1">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            {t('aiRuntime.form.cancel')}
          </button>
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
            disabled={preview.isPending || json.trim() === ''}
            onClick={() => void runPreview()}
            data-testid="ai-runtime-import-preview"
          >
            {preview.isPending ? t('aiRuntime.import.previewing') : t('aiRuntime.import.preview')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            disabled={!preview.data?.validation_token || !document || apply.isPending}
            onClick={() => void runApply()}
            data-testid="ai-runtime-import-apply"
          >
            {apply.isPending ? t('aiRuntime.import.applying') : t('aiRuntime.import.apply')}
          </button>
        </div>
      </div>
    </div>
  );
}

function ModalFrame({
  testId,
  title,
  error,
  busy,
  saveDisabled,
  children,
  onClose,
  onSave,
}: {
  testId: string;
  title: string;
  error?: string;
  busy: boolean;
  saveDisabled?: boolean;
  children: React.ReactNode;
  onClose: () => void;
  onSave: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid={testId}>
      <div className="max-h-[90vh] w-full max-w-xl space-y-3 overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <h2 className="text-lg font-semibold text-text-primary">{title}</h2>
        <div className="space-y-3">{children}</div>
        {error && <p className="text-xs text-danger" role="alert" data-testid="ai-runtime-form-error">{error}</p>}
        <div className="flex justify-end gap-2 pt-1">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            {t('aiRuntime.form.cancel')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            disabled={busy || saveDisabled}
            onClick={onSave}
            data-testid="ai-runtime-form-save"
          >
            {busy ? t('aiRuntime.form.saving') : t('aiRuntime.form.save')}
          </button>
        </div>
      </div>
    </div>
  );
}

function TextField({
  label,
  value,
  onChange,
  testId,
  type = 'text',
  disabled = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  testId: string;
  type?: string;
  disabled?: boolean;
}): React.ReactElement {
  return (
    <label className="block text-xs text-text-secondary">
      {label}
      <input
        type={type}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className={`${inputClass} disabled:cursor-not-allowed disabled:opacity-60`}
        data-testid={testId}
      />
    </label>
  );
}

function SelectField({
  label,
  value,
  options,
  onChange,
  testId,
}: {
  label: string;
  value: string;
  options: string[];
  onChange: (value: string) => void;
  testId: string;
}): React.ReactElement {
  return (
    <label className="block text-xs text-text-secondary">
      {label}
      <select value={value} onChange={(e) => onChange(e.target.value)} className={inputClass} data-testid={testId}>
        {options.map((option) => (
          <option key={option} value={option}>{option}</option>
        ))}
      </select>
    </label>
  );
}

function JSONField({
  label,
  value,
  onChange,
  testId,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  testId: string;
}): React.ReactElement {
  return (
    <label className="block text-xs text-text-secondary">
      {label}
      <textarea rows={5} value={value} onChange={(e) => onChange(e.target.value)} className={textareaClass} data-testid={testId} />
    </label>
  );
}

function CheckboxField({
  label,
  checked,
  onChange,
  testId,
}: {
  label: string;
  checked: boolean;
  onChange: (value: boolean) => void;
  testId: string;
}): React.ReactElement {
  return (
    <label className="flex items-center gap-2 text-sm text-text-secondary">
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} data-testid={testId} />
      <span>{label}</span>
    </label>
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

function prettyJSON(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function parseJSONRecord(raw: string, label: string): Record<string, unknown> {
  const parsed = raw.trim() === '' ? {} : JSON.parse(raw);
  if (!isRecord(parsed)) {
    throw new Error(`${label} must be a JSON object`);
  }
  return parsed;
}

function splitCSV(raw: string): string[] {
  return raw.split(',').map((v) => v.trim()).filter(Boolean);
}

function parseNumber(raw: string): number {
  if (raw.trim() === '') return 0;
  const n = Number(raw);
  return Number.isFinite(n) ? n : 0;
}

function firstEnabledKey(entries: Array<{ key: string; enabled?: boolean }>): string {
  return entries.find((entry) => entry.enabled !== false)?.key ?? entries[0]?.key ?? '';
}

function mutationMessage(error: unknown): string {
  return error instanceof Error ? error.message : '';
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

function normalizeImportedModels(value: unknown, fallbackCLIKey: string): RuntimeExportModel[] {
  if (!Array.isArray(value)) {
    throw new Error('Import JSON must be an array of model entries.');
  }
  const seen = new Set<string>();
  return value.map((raw, index) => {
    if (!isRecord(raw)) {
      throw new Error(`entry[${index}] must be an object.`);
    }
    const key = stringField(raw.key ?? raw.runtime_key ?? raw.model_id ?? raw.model_key).trim();
    const modelKey = stringField(raw.model_key ?? raw.model_id ?? raw.key).trim();
    if (!key || !modelKey) {
      throw new Error(`entry[${index}] requires key and model_key.`);
    }
    if (seen.has(key)) {
      throw new Error(`duplicate model key "${key}".`);
    }
    seen.add(key);
    const defaultParameters = raw.default_parameters === undefined ? {} : raw.default_parameters;
    if (!isRecord(defaultParameters)) {
      throw new Error(`entry[${index}].default_parameters must be an object.`);
    }
    const compatible = normalizeStringList(raw.compatible_cli_keys, fallbackCLIKey);
    return {
      key,
      model_key: modelKey,
      display_name: stringField(raw.display_name).trim() || modelKey,
      compatible_cli_keys: compatible,
      default_parameters: defaultParameters,
      enabled: typeof raw.enabled === 'boolean' ? raw.enabled : true,
      context_window: optionalNumber(raw.context_window ?? raw.context),
      input_cost_per_mtok: optionalNumber(raw.input_cost_per_mtok ?? raw.input_cost),
      output_cost_per_mtok: optionalNumber(raw.output_cost_per_mtok ?? raw.output_cost),
      tier: stringField(raw.tier).trim() || undefined,
    };
  });
}

function buildModelImportDocument(catalog: AIRuntimeCatalog, importedModels: RuntimeExportModel[]): RuntimeExportDocument {
  const defaultProfile = catalog.profiles.find((p) => p.id === catalog.default_runtime_profile_id);
  const byKey = new Map<string, RuntimeExportModel>();
  for (const model of catalog.models.map(toExportModel)) {
    byKey.set(model.key, model);
  }
  for (const model of importedModels) {
    byKey.set(model.key, model);
  }
  return {
    schema_version: 1,
    kind: 'agent-center-ai-runtime',
    exported_at: new Date().toISOString(),
    runtime: {
      default_profile_key: defaultProfile?.key,
      clis: catalog.clis.map(toExportCLI),
      models: Array.from(byKey.values()).sort((a, b) => a.key.localeCompare(b.key)),
      profiles: catalog.profiles.map(toExportProfile),
    },
  };
}

function toExportCLI(cli: RuntimeCLI): RuntimeExportCLI {
  return {
    key: cli.key,
    display_name: cli.display_name,
    executable: cli.executable,
    version_constraint: cli.version_constraint,
    required_features: cli.required_features ?? [],
    parameter_schema: cli.parameter_schema ?? { type: 'object' },
    enabled: cli.enabled,
  };
}

function toExportModel(model: RuntimeModel): RuntimeExportModel {
  return {
    key: model.key,
    model_key: model.model_key,
    display_name: model.display_name,
    compatible_cli_keys: model.compatible_cli_keys ?? [],
    default_parameters: model.default_parameters ?? {},
    enabled: model.enabled,
    context_window: model.context_window,
    input_cost_per_mtok: model.input_cost_per_mtok,
    output_cost_per_mtok: model.output_cost_per_mtok,
    tier: model.tier,
  };
}

function toExportProfile(profile: RuntimeProfile): RuntimeExportProfile {
  return {
    key: profile.key,
    name: profile.name,
    description: profile.description,
    cli_key: profile.cli_key,
    model_key: profile.model_key,
    parameters: profile.parameters ?? {},
    enabled: profile.enabled,
  };
}

function stringField(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function normalizeStringList(value: unknown, fallback: string): string[] {
  if (Array.isArray(value)) {
    return value.map((item) => stringField(item).trim()).filter(Boolean);
  }
  if (typeof value === 'string') {
    return splitCSV(value);
  }
  return fallback ? [fallback] : [];
}

function optionalNumber(value: unknown): number | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) {
    throw new Error(`model numeric fields must be non-negative numbers.`);
  }
  return n;
}
