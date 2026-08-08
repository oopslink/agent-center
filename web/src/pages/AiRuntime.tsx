import React, { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useOrgs } from '@/api/auth';
import {
  aiRuntimeExportHref,
  canManageAIRuntime,
  useAIRuntimeCatalog,
  useApplyRuntimeImport,
  useCreateRuntimeCLI,
  useCreateRuntimeModel,
  useCreateRuntimeProfile,
  usePreviewRuntimeImport,
  useSetDefaultRuntimeProfile,
  useUpdateRuntimeCLI,
  useUpdateRuntimeModel,
  useUpdateRuntimeProfile,
  type AIRuntimeCatalog,
  type RuntimeCLI,
  type RuntimeCLIInput,
  type RuntimeExportDocument,
  type RuntimeImportDiagnostic,
  type RuntimeImportPreview,
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
import { SegmentedNav } from '@/shell/SegmentedNav';
import { useSystemSegments } from './useSystemSegments';

type RuntimeTab = 'profiles' | 'models' | 'clis';
type RuntimeEditorState =
  | { kind: 'profile'; entry?: RuntimeProfile }
  | { kind: 'model'; entry?: RuntimeModel }
  | { kind: 'cli'; entry?: RuntimeCLI };

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
  const [editor, setEditor] = useState<RuntimeEditorState | null>(null);
  const [importOpen, setImportOpen] = useState(false);
  const systemSegments = useSystemSegments();

  const defaultProfile = useMemo(() => {
    const data = catalog.data;
    if (!data?.default_runtime_profile_id) return undefined;
    return data.profiles.find((p) => p.id === data.default_runtime_profile_id);
  }, [catalog.data]);

  const addKind = tab === 'profiles' ? 'profile' : tab === 'models' ? 'model' : 'cli';

  return (
    <section className="space-y-4" data-testid="page-AiRuntime">
      <SegmentedNav items={systemSegments.segments} ariaLabel={systemSegments.ariaLabel} />
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
        <div className="flex flex-wrap items-center justify-end gap-2">
          <a
            href={aiRuntimeExportHref('yaml')}
            className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
            data-testid="ai-runtime-export-yaml"
          >
            {t('aiRuntime.actions.exportYaml')}
          </a>
          {canManage && catalog.data && tab === 'models' && (
            <button
              type="button"
              className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
              onClick={() => setImportOpen(true)}
              data-testid="ai-runtime-import-models"
            >
              {t('aiRuntime.actions.importModels')}
            </button>
          )}
          {canManage && catalog.data && (
            <button
              type="button"
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
              onClick={() => setEditor({ kind: addKind })}
              data-testid={`ai-runtime-add-${addKind}`}
            >
              {t(`aiRuntime.actions.add.${addKind}`)}
            </button>
          )}
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
                  onEdit={(entry) => setEditor({ kind: 'profile', entry })}
                />
              )}
              {tab === 'models' && (
                <ModelsTable
                  rows={catalog.data.models}
                  canManage={canManage}
                  onEdit={(entry) => setEditor({ kind: 'model', entry })}
                />
              )}
              {tab === 'clis' && (
                <CLIsTable
                  rows={catalog.data.clis}
                  canManage={canManage}
                  onEdit={(entry) => setEditor({ kind: 'cli', entry })}
                />
              )}
            </div>
          </div>
          {editor && (
            <RuntimeFormModal
              state={editor}
              catalog={catalog.data}
              onClose={() => setEditor(null)}
            />
          )}
          {importOpen && (
            <ModelsImportModal
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
                      className="ml-3 text-xs text-accent hover:underline disabled:text-text-muted disabled:no-underline"
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
              <td className="px-3 py-2 text-text-secondary">{m.context_window ? m.context_window.toLocaleString() : '-'}</td>
              <td className="px-3 py-2 text-text-secondary">
                {m.input_cost_per_mtok ?? '-'} / {m.output_cost_per_mtok ?? '-'}
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
                <div className="font-medium text-text-primary">{cli.display_name}</div>
                <div className="font-mono text-xs text-text-muted">{cli.key}</div>
                {cli.system && (
                  <span className="mt-1 inline-flex rounded-full bg-bg-subtle px-2 py-0.5 text-xs font-medium text-text-muted">
                    {t('aiRuntime.cli.system')}
                  </span>
                )}
              </td>
              <td className="px-3 py-2 font-mono text-xs text-text-secondary">{cli.executable}</td>
              <td className="px-3 py-2 text-text-secondary">{cli.version_constraint || '-'}</td>
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

interface RuntimeFormFields {
  key: string;
  displayName: string;
  executable: string;
  versionConstraint: string;
  requiredFeatures: string;
  parameterSchema: string;
  modelKey: string;
  compatibleCliKeys: string;
  defaultParameters: string;
  contextWindow: string;
  inputCost: string;
  outputCost: string;
  tier: string;
  name: string;
  description: string;
  cliKey: string;
  profileModelKey: string;
  parameters: string;
  enabled: boolean;
}

function RuntimeFormModal({
  state,
  catalog,
  onClose,
}: {
  state: RuntimeEditorState;
  catalog: AIRuntimeCatalog;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const createCLI = useCreateRuntimeCLI();
  const updateCLI = useUpdateRuntimeCLI(state.kind === 'cli' ? state.entry?.id ?? '' : '');
  const createModel = useCreateRuntimeModel();
  const updateModel = useUpdateRuntimeModel(state.kind === 'model' ? state.entry?.id ?? '' : '');
  const createProfile = useCreateRuntimeProfile();
  const updateProfile = useUpdateRuntimeProfile(state.kind === 'profile' ? state.entry?.id ?? '' : '');
  const [fields, setFields] = useState<RuntimeFormFields>(() => initialRuntimeForm(state, catalog));
  const [clientError, setClientError] = useState('');
  const isEdit = !!state.entry;
  const pending = createCLI.isPending || updateCLI.isPending || createModel.isPending || updateModel.isPending || createProfile.isPending || updateProfile.isPending;
  const error = createCLI.error ?? updateCLI.error ?? createModel.error ?? updateModel.error ?? createProfile.error ?? updateProfile.error;

  const set = (patch: Partial<RuntimeFormFields>) => setFields((prev) => ({ ...prev, ...patch }));

  const submit = async () => {
    setClientError('');
    try {
      if (state.kind === 'cli') {
        const value = cliInput(fields);
        if (isEdit) await updateCLI.mutateAsync({ expectedRevision: catalog.revision, value });
        else await createCLI.mutateAsync({ expectedRevision: catalog.revision, value });
      } else if (state.kind === 'model') {
        const value = modelInput(fields);
        if (isEdit) await updateModel.mutateAsync({ expectedRevision: catalog.revision, value });
        else await createModel.mutateAsync({ expectedRevision: catalog.revision, value });
      } else {
        const value = profileInput(fields);
        if (isEdit) await updateProfile.mutateAsync({ expectedRevision: catalog.revision, value });
        else await createProfile.mutateAsync({ expectedRevision: catalog.revision, value });
      }
      onClose();
    } catch (err) {
      setClientError(err instanceof Error ? err.message : String(err));
    }
  };

  const titleKey = isEdit ? `aiRuntime.editor.${state.kind}.edit` : `aiRuntime.editor.${state.kind}.create`;
  const saveDisabled =
    pending ||
    fields.key.trim() === '' ||
    (state.kind === 'cli' && (fields.displayName.trim() === '' || fields.executable.trim() === '')) ||
    (state.kind === 'model' && (fields.modelKey.trim() === '' || fields.compatibleCliKeys.trim() === '')) ||
    (state.kind === 'profile' && (fields.name.trim() === '' || fields.cliKey.trim() === '' || fields.profileModelKey.trim() === ''));

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid="ai-runtime-editor">
      <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <div className="mb-3">
          <h2 className="text-lg font-semibold text-text-primary">{t(titleKey)}</h2>
          {isEdit && (
            <p className="mt-1 text-xs text-text-muted" data-testid="ai-runtime-editor-immutable-hint">
              {state.kind === 'cli' && state.entry?.system
                ? t('aiRuntime.editor.systemHint')
                : t('aiRuntime.editor.immutableHint')}
            </p>
          )}
        </div>

        <div className="grid gap-3 md:grid-cols-2">
          <Field label={t('aiRuntime.editor.key')}>
            <input
              className={inputClass}
              value={fields.key}
              disabled={isEdit}
              onChange={(e) => set({ key: e.target.value })}
              data-testid="ai-runtime-field-key"
            />
          </Field>

          {state.kind === 'cli' && (
            <>
              <Field label={t('aiRuntime.cli.name')}>
                <input className={inputClass} value={fields.displayName} onChange={(e) => set({ displayName: e.target.value })} data-testid="ai-runtime-field-display-name" />
              </Field>
              <Field label={t('aiRuntime.cli.executable')}>
                <input className={inputClass} value={fields.executable} onChange={(e) => set({ executable: e.target.value })} data-testid="ai-runtime-field-executable" />
              </Field>
              <Field label={t('aiRuntime.cli.version')}>
                <input className={inputClass} value={fields.versionConstraint} onChange={(e) => set({ versionConstraint: e.target.value })} />
              </Field>
              <Field label={t('aiRuntime.cli.features')}>
                <input className={inputClass} value={fields.requiredFeatures} onChange={(e) => set({ requiredFeatures: e.target.value })} data-testid="ai-runtime-field-features" />
              </Field>
              <Field label={t('aiRuntime.editor.parameterSchema')} wide>
                <textarea rows={5} className={monoInputClass} value={fields.parameterSchema} onChange={(e) => set({ parameterSchema: e.target.value })} data-testid="ai-runtime-field-parameter-schema" />
              </Field>
            </>
          )}

          {state.kind === 'model' && (
            <>
              <Field label={t('aiRuntime.model.name')}>
                <input className={inputClass} value={fields.displayName} onChange={(e) => set({ displayName: e.target.value })} data-testid="ai-runtime-field-display-name" />
              </Field>
              <Field label={t('aiRuntime.model.modelKey')}>
                <input className={inputClass} value={fields.modelKey} onChange={(e) => set({ modelKey: e.target.value })} data-testid="ai-runtime-field-model-key" />
              </Field>
              <Field label={t('aiRuntime.model.compatibleCli')}>
                <input className={inputClass} value={fields.compatibleCliKeys} onChange={(e) => set({ compatibleCliKeys: e.target.value })} data-testid="ai-runtime-field-compatible-clis" />
              </Field>
              <Field label={t('aiRuntime.model.context')}>
                <input type="number" min="0" className={inputClass} value={fields.contextWindow} onChange={(e) => set({ contextWindow: e.target.value })} />
              </Field>
              <Field label={t('aiRuntime.editor.inputCost')}>
                <input type="number" min="0" step="any" className={inputClass} value={fields.inputCost} onChange={(e) => set({ inputCost: e.target.value })} />
              </Field>
              <Field label={t('aiRuntime.editor.outputCost')}>
                <input type="number" min="0" step="any" className={inputClass} value={fields.outputCost} onChange={(e) => set({ outputCost: e.target.value })} />
              </Field>
              <Field label={t('aiRuntime.editor.tier')} wide>
                <textarea rows={2} className={inputClass} value={fields.tier} onChange={(e) => set({ tier: e.target.value })} />
              </Field>
              <Field label={t('aiRuntime.editor.defaultParameters')} wide>
                <textarea rows={5} className={monoInputClass} value={fields.defaultParameters} onChange={(e) => set({ defaultParameters: e.target.value })} data-testid="ai-runtime-field-default-parameters" />
              </Field>
            </>
          )}

          {state.kind === 'profile' && (
            <>
              <Field label={t('aiRuntime.profile.name')}>
                <input className={inputClass} value={fields.name} onChange={(e) => set({ name: e.target.value })} data-testid="ai-runtime-field-name" />
              </Field>
              <Field label={t('aiRuntime.profile.cli')}>
                <select className={inputClass} value={fields.cliKey} onChange={(e) => set({ cliKey: e.target.value })} data-testid="ai-runtime-field-cli">
                  <option value="">{t('aiRuntime.editor.choose')}</option>
                  {catalog.clis.map((cli) => (
                    <option key={cli.key} value={cli.key}>{cli.display_name || cli.key}</option>
                  ))}
                </select>
              </Field>
              <Field label={t('aiRuntime.profile.model')}>
                <select className={inputClass} value={fields.profileModelKey} onChange={(e) => set({ profileModelKey: e.target.value })} data-testid="ai-runtime-field-profile-model">
                  <option value="">{t('aiRuntime.editor.choose')}</option>
                  {catalog.models.map((model) => (
                    <option key={model.key} value={model.key}>{model.display_name || model.key}</option>
                  ))}
                </select>
              </Field>
              <Field label={t('aiRuntime.editor.description')} wide>
                <textarea rows={2} className={inputClass} value={fields.description} onChange={(e) => set({ description: e.target.value })} />
              </Field>
              <Field label={t('aiRuntime.profile.parameters')} wide>
                <textarea rows={5} className={monoInputClass} value={fields.parameters} onChange={(e) => set({ parameters: e.target.value })} data-testid="ai-runtime-field-parameters" />
              </Field>
            </>
          )}

          <div className="flex items-center gap-2 text-sm text-text-secondary">
            <button
              type="button"
              role="switch"
              aria-checked={fields.enabled}
              onClick={() => set({ enabled: !fields.enabled })}
              data-testid="ai-runtime-field-enabled"
              className={[
                'relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full motion-safe:transition-colors',
                fields.enabled ? 'bg-brand' : 'bg-bg-muted',
              ].join(' ')}
            >
              <span
                aria-hidden="true"
                className={[
                  'inline-block h-4 w-4 rounded-full bg-white shadow motion-safe:transition-transform',
                  fields.enabled ? 'translate-x-4' : 'translate-x-0.5',
                ].join(' ')}
              />
            </button>
            <span>{t('aiRuntime.status.enabled')}</span>
          </div>
        </div>

        {clientError && <p className="mt-3 text-xs text-danger" role="alert">{clientError}</p>}
        {error && (
          <p className="mt-3 text-xs text-danger" role="alert" data-testid="ai-runtime-editor-error">
            {(error as Error).message}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            {t('aiRuntime.editor.cancel')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            disabled={saveDisabled}
            onClick={() => void submit()}
            data-testid="ai-runtime-editor-save"
          >
            {pending ? t('aiRuntime.editor.saving') : t('aiRuntime.editor.save')}
          </button>
        </div>
      </div>
    </div>
  );
}

function ModelsImportModal({ catalog, onClose }: { catalog: AIRuntimeCatalog; onClose: () => void }): React.ReactElement {
  const { t } = useTranslation('admin');
  const previewImport = usePreviewRuntimeImport();
  const applyImport = useApplyRuntimeImport();
  const [raw, setRaw] = useState('');
  const [clientError, setClientError] = useState('');
  const [document, setDocument] = useState<RuntimeExportDocument | null>(null);
  const [preview, setPreview] = useState<RuntimeImportPreview | null>(null);

  const previewModels = preview?.report.items.filter((item) => item.entity_type === 'model') ?? [];
  const changedModels = previewModels.filter((item) => item.action !== 'unchanged');
  const preservedProfiles = preview?.report.items.filter((item) => item.entity_type === 'profile').length ?? 0;
  const preservedCLIs = preview?.report.items.filter((item) => item.entity_type === 'cli').length ?? 0;
  const previewDiagnostics = preview?.report.diagnostics ?? [];
  const errorDiagnostics = diagnosticsFromError(previewImport.error ?? applyImport.error);

  const runPreview = async () => {
    setClientError('');
    setPreview(null);
    setDocument(null);
    try {
      const models = normalizeModelImport(raw, catalog);
      const nextDoc = buildModelImportDocument(catalog, models);
      const result = await previewImport.mutateAsync({ strategy: 'merge', document: nextDoc });
      setDocument(nextDoc);
      setPreview(result);
    } catch (err) {
      setClientError(err instanceof Error ? err.message : String(err));
    }
  };

  const apply = async () => {
    if (!document || !preview) return;
    setClientError('');
    try {
      await applyImport.mutateAsync({
        strategy: 'merge',
        document,
        validation_token: preview.validation_token,
      });
      onClose();
    } catch (err) {
      if (!(err instanceof Error)) setClientError(String(err));
    }
  };

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid="ai-runtime-model-import">
      <div className="max-h-[90vh] w-full max-w-3xl overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2">
        <h2 className="text-lg font-semibold text-text-primary">{t('aiRuntime.import.title')}</h2>
        <p className="mt-1 text-xs text-text-muted">{t('aiRuntime.import.help')}</p>
        <textarea
          rows={10}
          className="mt-3 w-full rounded border border-border-base bg-bg-subtle px-2 py-1 font-mono text-xs text-text-primary"
          value={raw}
          onChange={(e) => {
            setRaw(e.target.value);
            setPreview(null);
            setDocument(null);
          }}
          placeholder='[{"key":"gpt-5.1","model_key":"gpt-5.1","display_name":"GPT-5.1","compatible_cli_keys":["codex"],"enabled":true}]'
          data-testid="ai-runtime-import-json"
        />

        {clientError && (
          <p className="mt-2 text-xs text-danger" role="alert" data-testid="ai-runtime-import-client-error">
            {clientError}
          </p>
        )}
        {previewImport.isError && (
          <p className="mt-2 text-xs text-danger" role="alert" data-testid="ai-runtime-import-preview-error">
            {(previewImport.error as Error).message}
          </p>
        )}
        {applyImport.isError && (
          <p className="mt-2 text-xs text-danger" role="alert" data-testid="ai-runtime-import-apply-error">
            {(applyImport.error as Error).message}
          </p>
        )}
        {(previewDiagnostics.length > 0 || errorDiagnostics.length > 0) && (
          <DiagnosticsList diagnostics={[...previewDiagnostics, ...errorDiagnostics]} />
        )}

        {preview && (
          <div className="mt-3 rounded border border-border-base bg-bg-subtle p-3" data-testid="ai-runtime-import-preview">
            <div className="flex flex-wrap gap-3 text-xs text-text-secondary">
              <span data-testid="ai-runtime-import-model-changes">
                {t('aiRuntime.import.modelChanges', { count: changedModels.length })}
              </span>
              <span>{t('aiRuntime.import.preservedProfiles', { count: preservedProfiles })}</span>
              <span>{t('aiRuntime.import.preservedClis', { count: preservedCLIs })}</span>
            </div>
            <ul className="mt-2 max-h-40 space-y-1 overflow-y-auto text-xs">
              {previewModels.length === 0 ? (
                <li className="text-text-muted">{t('aiRuntime.import.noModelChanges')}</li>
              ) : (
                previewModels.map((item) => (
                  <li key={`${item.key}-${item.action}`} className="flex items-center justify-between gap-3">
                    <span className="font-mono text-text-primary">{item.key}</span>
                    <span className="rounded bg-bg-elevated px-1.5 py-0.5 text-text-secondary">{item.action}</span>
                  </li>
                ))
              )}
            </ul>
          </div>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onClose}>
            {t('aiRuntime.editor.cancel')}
          </button>
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
            onClick={() => void runPreview()}
            disabled={previewImport.isPending || applyImport.isPending || raw.trim() === ''}
            data-testid="ai-runtime-import-preview-run"
          >
            {previewImport.isPending ? t('aiRuntime.import.previewing') : t('aiRuntime.import.preview')}
          </button>
          <button
            type="button"
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            onClick={() => void apply()}
            disabled={!preview || !document || applyImport.isPending || previewDiagnostics.some((d) => d.severity !== 'warning')}
            data-testid="ai-runtime-import-apply"
          >
            {applyImport.isPending ? t('aiRuntime.import.applying') : t('aiRuntime.import.apply')}
          </button>
        </div>
      </div>
    </div>
  );
}

function DiagnosticsList({ diagnostics }: { diagnostics: RuntimeImportDiagnostic[] }): React.ReactElement {
  return (
    <ul className="mt-2 space-y-1 rounded border border-danger/30 bg-danger/5 p-2 text-xs text-danger" data-testid="ai-runtime-import-diagnostics">
      {diagnostics.map((d, i) => (
        <li key={`${d.path ?? ''}-${d.key ?? ''}-${i}`}>
          {[d.path, d.entity_type, d.key].filter(Boolean).join(' ')} {d.message}
        </li>
      ))}
    </ul>
  );
}

function Field({ label, wide, children }: { label: string; wide?: boolean; children: React.ReactNode }): React.ReactElement {
  return (
    <label className={['block text-xs text-text-secondary', wide ? 'md:col-span-2' : ''].join(' ')}>
      <span>{label}</span>
      <div className="mt-1">{children}</div>
    </label>
  );
}

const inputClass = 'w-full rounded border border-border-base bg-bg-subtle px-2 py-1 text-sm text-text-primary disabled:cursor-not-allowed disabled:opacity-60';
const monoInputClass = `${inputClass} font-mono text-xs`;

function initialRuntimeForm(state: RuntimeEditorState, catalog: AIRuntimeCatalog): RuntimeFormFields {
  const firstCLI = catalog.clis[0]?.key ?? '';
  const firstModel = catalog.models[0]?.key ?? '';
  const base: RuntimeFormFields = {
    key: '',
    displayName: '',
    executable: '',
    versionConstraint: '',
    requiredFeatures: '',
    parameterSchema: prettyJSON({ type: 'object' }),
    modelKey: '',
    compatibleCliKeys: firstCLI,
    defaultParameters: prettyJSON({}),
    contextWindow: '',
    inputCost: '',
    outputCost: '',
    tier: '',
    name: '',
    description: '',
    cliKey: firstCLI,
    profileModelKey: firstModel,
    parameters: prettyJSON({}),
    enabled: true,
  };
  if (state.kind === 'cli' && state.entry) {
    const x = state.entry;
    return {
      ...base,
      key: x.key,
      displayName: x.display_name,
      executable: x.executable,
      versionConstraint: x.version_constraint ?? '',
      requiredFeatures: (x.required_features ?? []).join(', '),
      parameterSchema: prettyJSON(x.parameter_schema ?? { type: 'object' }),
      enabled: x.enabled,
    };
  }
  if (state.kind === 'model' && state.entry) {
    const x = state.entry;
    return {
      ...base,
      key: x.key,
      displayName: x.display_name,
      modelKey: x.model_key,
      compatibleCliKeys: (x.compatible_cli_keys ?? []).join(', '),
      defaultParameters: prettyJSON(x.default_parameters ?? {}),
      contextWindow: String(x.context_window ?? ''),
      inputCost: String(x.input_cost_per_mtok ?? ''),
      outputCost: String(x.output_cost_per_mtok ?? ''),
      tier: x.tier ?? '',
      enabled: x.enabled,
    };
  }
  if (state.kind === 'profile' && state.entry) {
    const x = state.entry;
    return {
      ...base,
      key: x.key,
      name: x.name,
      description: x.description ?? '',
      cliKey: x.cli_key,
      profileModelKey: x.model_key,
      parameters: prettyJSON(x.parameters ?? {}),
      enabled: x.enabled,
    };
  }
  return base;
}

function cliInput(fields: RuntimeFormFields): RuntimeCLIInput {
  return {
    key: fields.key.trim(),
    display_name: fields.displayName.trim(),
    executable: fields.executable.trim(),
    version_constraint: fields.versionConstraint.trim() || undefined,
    required_features: splitList(fields.requiredFeatures),
    parameter_schema: parseObjectJSON(fields.parameterSchema, 'parameter_schema'),
    enabled: fields.enabled,
  };
}

function modelInput(fields: RuntimeFormFields): RuntimeModelInput {
  return {
    key: fields.key.trim(),
    model_key: fields.modelKey.trim(),
    display_name: fields.displayName.trim() || fields.modelKey.trim(),
    compatible_cli_keys: splitList(fields.compatibleCliKeys),
    default_parameters: parseObjectJSON(fields.defaultParameters, 'default_parameters'),
    enabled: fields.enabled,
    context_window: parseOptionalNumber(fields.contextWindow, 'context_window') ?? 0,
    input_cost_per_mtok: parseOptionalNumber(fields.inputCost, 'input_cost_per_mtok') ?? 0,
    output_cost_per_mtok: parseOptionalNumber(fields.outputCost, 'output_cost_per_mtok') ?? 0,
    tier: fields.tier.trim() || undefined,
  };
}

function profileInput(fields: RuntimeFormFields): RuntimeProfileInput {
  return {
    key: fields.key.trim(),
    name: fields.name.trim(),
    description: fields.description.trim() || undefined,
    cli_key: fields.cliKey.trim(),
    model_key: fields.profileModelKey.trim(),
    parameters: parseObjectJSON(fields.parameters, 'parameters'),
    enabled: fields.enabled,
  };
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
  if (values.length === 0) return <span className="text-text-muted">-</span>;
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
  return count === 0 ? '-' : String(count);
}

function splitList(raw: string): string[] {
  return raw.split(/[\n,]/).map((v) => v.trim()).filter(Boolean);
}

function parseObjectJSON(raw: string, field: string): Record<string, unknown> {
  const text = raw.trim() || '{}';
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch (err) {
    throw new Error(`${field}: ${(err as Error).message}`);
  }
  if (!isRecord(value)) {
    throw new Error(`${field}: JSON value must be an object`);
  }
  return value;
}

function parseOptionalNumber(raw: string, field: string): number | undefined {
  if (raw.trim() === '') return undefined;
  const value = Number(raw);
  if (!Number.isFinite(value) || value < 0) {
    throw new Error(`${field}: value must be a non-negative number`);
  }
  return value;
}

function prettyJSON(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function normalizeModelImport(raw: string, catalog: AIRuntimeCatalog): RuntimeModelInput[] {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    throw new Error(`json: ${(err as Error).message}`);
  }
  if (!Array.isArray(parsed)) {
    throw new Error('json: expected an array of model entries');
  }
  const defaultCLI = catalog.clis.find((cli) => cli.enabled)?.key ?? catalog.clis[0]?.key ?? 'codex';
  const seen = new Set<string>();
  return parsed.map((item, index) => {
    if (!isRecord(item)) {
      throw new Error(`models[${index}]: entry must be an object`);
    }
    const key = stringValue(item.key ?? item.model_id ?? item.model_key);
    const modelKey = stringValue(item.model_key ?? item.model_id ?? item.key);
    if (!key || !modelKey) {
      throw new Error(`models[${index}]: key and model_key are required`);
    }
    if (seen.has(key)) {
      throw new Error(`models[${index}]: duplicate key ${key}`);
    }
    seen.add(key);
    const compatible = listValue(item.compatible_cli_keys, [defaultCLI]);
    if (compatible.length === 0) {
      throw new Error(`models[${index}]: compatible_cli_keys is required`);
    }
    const model: RuntimeModelInput = {
      key,
      model_key: modelKey,
      display_name: stringValue(item.display_name) || modelKey,
      compatible_cli_keys: compatible,
      default_parameters: objectValue(item.default_parameters, {}),
      enabled: boolValue(item.enabled, true),
      tier: stringValue(item.tier) || undefined,
    };
    const contextWindow = numberValue(item.context_window, 'context_window', index);
    const inputCost = numberValue(item.input_cost_per_mtok ?? item.input_cost, 'input_cost_per_mtok', index);
    const outputCost = numberValue(item.output_cost_per_mtok ?? item.output_cost, 'output_cost_per_mtok', index);
    if (contextWindow !== undefined) model.context_window = contextWindow;
    if (inputCost !== undefined) model.input_cost_per_mtok = inputCost;
    if (outputCost !== undefined) model.output_cost_per_mtok = outputCost;
    return model;
  });
}

function buildModelImportDocument(catalog: AIRuntimeCatalog, incoming: RuntimeModelInput[]): RuntimeExportDocument {
  const byKey = new Map<string, RuntimeModelInput>();
  for (const model of catalog.models) {
    byKey.set(model.key, exportModel(model));
  }
  for (const model of incoming) {
    byKey.set(model.key, model);
  }
  const defaultProfile = catalog.profiles.find((p) => p.id === catalog.default_runtime_profile_id);
  return {
    schema_version: 1,
    kind: 'agent-center-ai-runtime',
    exported_at: new Date().toISOString(),
    runtime: {
      default_profile_key: defaultProfile?.key,
      clis: catalog.clis.map(exportCLI),
      models: [...byKey.values()].sort((a, b) => a.key.localeCompare(b.key)),
      profiles: catalog.profiles.map(exportProfile),
    },
  };
}

function exportCLI(cli: RuntimeCLI): RuntimeCLIInput {
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

function exportModel(model: RuntimeModel): RuntimeModelInput {
  return {
    key: model.key,
    model_key: model.model_key,
    display_name: model.display_name,
    compatible_cli_keys: model.compatible_cli_keys ?? [],
    default_parameters: model.default_parameters ?? {},
    enabled: model.enabled,
    context_window: model.context_window ?? 0,
    input_cost_per_mtok: model.input_cost_per_mtok ?? 0,
    output_cost_per_mtok: model.output_cost_per_mtok ?? 0,
    tier: model.tier,
  };
}

function exportProfile(profile: RuntimeProfile): RuntimeProfileInput {
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

function diagnosticsFromError(error: unknown): RuntimeImportDiagnostic[] {
  const body = (error as { body?: unknown } | undefined)?.body;
  if (!isRecord(body)) return [];
  const report = body.report;
  if (isRecord(report) && Array.isArray(report.diagnostics)) {
    return report.diagnostics.filter(isRuntimeDiagnostic);
  }
  const nested = body.error;
  if (isRecord(nested) && isRecord(nested.details) && Array.isArray(nested.details.diagnostics)) {
    return nested.details.diagnostics.filter(isRuntimeDiagnostic);
  }
  return [];
}

function isRuntimeDiagnostic(value: unknown): value is RuntimeImportDiagnostic {
  return isRecord(value) && typeof value.message === 'string';
}

function stringValue(value: unknown): string {
  if (value === undefined || value === null) return '';
  return String(value).trim();
}

function listValue(value: unknown, fallback: string[]): string[] {
  if (Array.isArray(value)) {
    return value.map(stringValue).filter(Boolean);
  }
  if (typeof value === 'string') {
    const values = splitList(value);
    return values.length > 0 ? values : fallback;
  }
  return fallback;
}

function objectValue(value: unknown, fallback: Record<string, unknown>): Record<string, unknown> {
  if (value === undefined || value === null) return fallback;
  if (!isRecord(value)) throw new Error('default_parameters: value must be an object');
  return value;
}

function boolValue(value: unknown, fallback: boolean): boolean {
  if (typeof value === 'boolean') return value;
  return fallback;
}

function numberValue(value: unknown, field: string, index: number): number | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const number = Number(value);
  if (!Number.isFinite(number) || number < 0) {
    throw new Error(`models[${index}].${field}: value must be a non-negative number`);
  }
  return number;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
