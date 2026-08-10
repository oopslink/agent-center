import React, { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { useOrgs } from '@/api/auth';
import {
  aiRuntimeExportHref,
  canManageAIRuntime,
  RuntimeImportError,
  useAIRuntimeCatalog,
  useApplyRuntimeImport,
  useCreateRuntimeEntry,
  usePreviewRuntimeImport,
  useUpdateRuntimeEntry,
  type AIRuntimeCatalog,
  type AIRuntimeExportCLI,
  type AIRuntimeExportDocument,
  type AIRuntimeExportModel,
  type RuntimeCLI,
  type RuntimeCLIInput,
  type RuntimeModel,
  type RuntimeModelInput,
} from '@/api/aiRuntime';
import { currentOrgSlug } from '@/api/client';
import { useOptionalOrgContext } from '@/OrgContext';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ToggleSwitch } from '@/components/ToggleSwitch';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { useSystemSegments } from './useSystemSegments';

type RuntimeTab = 'models' | 'clis';

const TABS: RuntimeTab[] = ['models', 'clis'];

export default function AiRuntime(): React.ReactElement {
  const { t } = useTranslation('admin');
  const catalog = useAIRuntimeCatalog();
  const orgCtx = useOptionalOrgContext();
  const orgs = useOrgs();
  const activeSlug = orgCtx?.slug ?? currentOrgSlug() ?? orgs.data?.[0]?.slug;
  const currentOrg = (orgs.data ?? []).find((o) => o.slug === activeSlug);
  const role = currentOrg?.role;
  const canManage = canManageAIRuntime(role);
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab') as RuntimeTab | null;
  const tab: RuntimeTab = tabParam && TABS.includes(tabParam) ? tabParam : 'models';
  const systemSegments = useSystemSegments();
  const [modelForm, setModelForm] = useState<RuntimeModel | 'new' | null>(null);
  const [cliForm, setCLIForm] = useState<RuntimeCLI | 'new' | null>(null);
  const [modelImportOpen, setModelImportOpen] = useState(false);

  const setTab = (next: RuntimeTab): void => {
    const params = new URLSearchParams(searchParams);
    if (next === 'models') {
      params.delete('tab');
    } else {
      params.set('tab', next);
    }
    setSearchParams(params, { replace: true });
  };

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
        <div className="flex flex-wrap items-center gap-2">
          {canManage && catalog.data && tab === 'models' && (
            <>
              <button
                type="button"
                className="rounded border border-border-base px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-subtle"
                data-testid="ai-runtime-import-models"
                onClick={() => setModelImportOpen(true)}
              >
                {t('aiRuntime.actions.importModels')}
              </button>
              <button
                type="button"
                className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
                data-testid="ai-runtime-create-model"
                onClick={() => setModelForm('new')}
              >
                {t('aiRuntime.actions.newModel')}
              </button>
            </>
          )}
          {canManage && catalog.data && tab === 'clis' && (
            <button
              type="button"
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
              data-testid="ai-runtime-create-cli"
              onClick={() => setCLIForm('new')}
            >
              {t('aiRuntime.actions.newCLI')}
            </button>
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
          <div className="grid gap-3 md:grid-cols-3">
            <Summary label={t('aiRuntime.summary.revision')} value={String(catalog.data.revision)} />
            <Summary label={t('aiRuntime.tabs.models')} value={String(catalog.data.models.length)} />
            <Summary label={t('aiRuntime.tabs.clis')} value={String(catalog.data.clis.length)} />
          </div>
          <div className="rounded-lg border border-border-base bg-bg-elevated" data-testid="ai-runtime-catalog">
            <div
              role="tablist"
              aria-label={t('aiRuntime.tabs.aria')}
              className="flex gap-1 border-b border-border-base px-3 pt-2"
            >
              {TABS.map((key) => (
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
              {tab === 'models' && (
                <ModelsTable
                  rows={catalog.data.models}
                  canManage={canManage}
                  onEdit={setModelForm}
                />
              )}
              {tab === 'clis' && (
                <CLIsTable
                  rows={catalog.data.clis}
                  canManage={canManage}
                  onEdit={setCLIForm}
                />
              )}
            </div>
          </div>
          {modelForm && (
            <RuntimeModelForm
              catalog={catalog.data}
              entry={modelForm === 'new' ? undefined : modelForm}
              onClose={() => setModelForm(null)}
            />
          )}
          {cliForm && (
            <RuntimeCLIForm
              revision={catalog.data.revision}
              entry={cliForm === 'new' ? undefined : cliForm}
              onClose={() => setCLIForm(null)}
            />
          )}
          {modelImportOpen && (
            <ModelImportModal catalog={catalog.data} onClose={() => setModelImportOpen(false)} />
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
      <table className="w-full min-w-[54rem] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-text-muted">
          <tr className="border-b border-border-base">
            <th className="px-3 py-2">{t('aiRuntime.model.name')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.modelKey')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.compatibleCli')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.context')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.cost')}</th>
            <th className="px-3 py-2">{t('aiRuntime.model.status')}</th>
            {canManage && <th className="px-3 py-2 text-right">{t('aiRuntime.common.actions')}</th>}
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
            {canManage && <th className="px-3 py-2 text-right">{t('aiRuntime.common.actions')}</th>}
          </tr>
        </thead>
        <tbody>
          {rows.map((cli) => (
            <tr key={cli.id} className="border-b border-border-base last:border-0" data-testid="ai-runtime-cli-row">
              <td className="px-3 py-2">
                <div className="font-medium text-text-primary">{cli.display_name}</div>
                <div className="font-mono text-xs text-text-muted">
                  {cli.key}
                  {cli.system && (
                    <span className="ml-2 rounded bg-bg-subtle px-1.5 py-0.5 font-sans text-[0.6875rem] uppercase text-text-muted">
                      {t('aiRuntime.cli.system')}
                    </span>
                  )}
                </div>
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

function RuntimeModelForm({
  catalog,
  entry,
  onClose,
}: {
  catalog: AIRuntimeCatalog;
  entry?: RuntimeModel;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const create = useCreateRuntimeEntry('models');
  const update = useUpdateRuntimeEntry('models');
  const [fields, setFields] = useState({
    key: entry?.key ?? '',
    model_key: entry?.model_key ?? '',
    display_name: entry?.display_name ?? '',
    compatible_cli_keys: (entry?.compatible_cli_keys ?? [catalog.clis[0]?.key].filter(Boolean)).join(', '),
    default_parameters: prettyJSON(entry?.default_parameters ?? {}),
    enabled: entry?.enabled ?? true,
    context_window: numberText(entry?.context_window),
    input_cost_per_mtok: numberText(entry?.input_cost_per_mtok),
    output_cost_per_mtok: numberText(entry?.output_cost_per_mtok),
    tier: entry?.tier ?? '',
  });
  const [parseError, setParseError] = useState('');
  const mutation = entry ? update : create;

  const submit = async (): Promise<void> => {
    setParseError('');
    let defaultParameters: Record<string, unknown>;
    try {
      defaultParameters = parseJSONObject(fields.default_parameters, t('aiRuntime.form.defaultParameters'));
    } catch (err) {
      setParseError((err as Error).message);
      return;
    }
    const value: RuntimeModelInput = {
      key: fields.key.trim(),
      model_key: fields.model_key.trim(),
      display_name: fields.display_name.trim(),
      compatible_cli_keys: splitKeys(fields.compatible_cli_keys),
      default_parameters: defaultParameters,
      enabled: fields.enabled,
      context_window: optionalNumber(fields.context_window),
      input_cost_per_mtok: optionalNumber(fields.input_cost_per_mtok),
      output_cost_per_mtok: optionalNumber(fields.output_cost_per_mtok),
      tier: fields.tier.trim(),
    };
    try {
      if (entry) {
        await update.mutateAsync({ id: entry.id, expectedRevision: catalog.revision, value });
      } else {
        await create.mutateAsync({ expectedRevision: catalog.revision, value });
      }
      onClose();
    } catch {
      // surfaced below
    }
  };

  return (
    <Modal testId="ai-runtime-model-form" title={entry ? t('aiRuntime.form.editModel') : t('aiRuntime.form.newModel')} onClose={onClose}>
      <Field label={t('aiRuntime.form.key')}>
        <input className={inputClass} value={fields.key} disabled={!!entry} onChange={(e) => setFields({ ...fields, key: e.target.value })} data-testid="ai-runtime-model-key" />
      </Field>
      {entry && <ImmutableKeyHint />}
      <div className="grid gap-3 md:grid-cols-2">
        <Field label={t('aiRuntime.model.modelKey')}>
          <input className={inputClass} value={fields.model_key} onChange={(e) => setFields({ ...fields, model_key: e.target.value })} data-testid="ai-runtime-model-model-key" />
        </Field>
        <Field label={t('aiRuntime.model.name')}>
          <input className={inputClass} value={fields.display_name} onChange={(e) => setFields({ ...fields, display_name: e.target.value })} data-testid="ai-runtime-model-display-name" />
        </Field>
      </div>
      <Field label={t('aiRuntime.model.compatibleCli')}>
        <input className={inputClass} value={fields.compatible_cli_keys} onChange={(e) => setFields({ ...fields, compatible_cli_keys: e.target.value })} data-testid="ai-runtime-model-compatible-clis" />
      </Field>
      <div className="grid gap-3 md:grid-cols-3">
        <Field label={t('aiRuntime.model.context')}>
          <input className={inputClass} type="number" min="0" value={fields.context_window} onChange={(e) => setFields({ ...fields, context_window: e.target.value })} data-testid="ai-runtime-model-context" />
        </Field>
        <Field label={t('aiRuntime.form.inputCost')}>
          <input className={inputClass} type="number" min="0" step="any" value={fields.input_cost_per_mtok} onChange={(e) => setFields({ ...fields, input_cost_per_mtok: e.target.value })} data-testid="ai-runtime-model-input-cost" />
        </Field>
        <Field label={t('aiRuntime.form.outputCost')}>
          <input className={inputClass} type="number" min="0" step="any" value={fields.output_cost_per_mtok} onChange={(e) => setFields({ ...fields, output_cost_per_mtok: e.target.value })} data-testid="ai-runtime-model-output-cost" />
        </Field>
      </div>
      <Field label={t('aiRuntime.form.tier')}>
        <input className={inputClass} value={fields.tier} onChange={(e) => setFields({ ...fields, tier: e.target.value })} data-testid="ai-runtime-model-tier" />
      </Field>
      <Field label={t('aiRuntime.form.defaultParameters')}>
        <textarea className={`${inputClass} font-mono`} rows={5} value={fields.default_parameters} onChange={(e) => setFields({ ...fields, default_parameters: e.target.value })} data-testid="ai-runtime-model-default-parameters" />
      </Field>
      <Checkbox checked={fields.enabled} label={t('aiRuntime.form.enabled')} onChange={(enabled) => setFields({ ...fields, enabled })} testId="ai-runtime-model-enabled" />
      <FormFooter
        busy={mutation.isPending}
        error={parseError || mutationErrorMessage(mutation.error)}
        saveDisabled={!fields.key.trim() || !fields.model_key.trim() || splitKeys(fields.compatible_cli_keys).length === 0}
        onCancel={onClose}
        onSave={() => void submit()}
      />
    </Modal>
  );
}

function RuntimeCLIForm({
  revision,
  entry,
  onClose,
}: {
  revision: number;
  entry?: RuntimeCLI;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const create = useCreateRuntimeEntry('clis');
  const update = useUpdateRuntimeEntry('clis');
  const [fields, setFields] = useState({
    key: entry?.key ?? '',
    display_name: entry?.display_name ?? '',
    executable: entry?.executable ?? '',
    version_constraint: entry?.version_constraint ?? '',
    required_features: (entry?.required_features ?? []).join(', '),
    parameter_schema: prettyJSON(entry?.parameter_schema ?? { type: 'object' }),
    enabled: entry?.enabled ?? true,
  });
  const [parseError, setParseError] = useState('');
  const mutation = entry ? update : create;

  const submit = async (): Promise<void> => {
    setParseError('');
    let schema: unknown;
    try {
      schema = parseJSONValue(fields.parameter_schema, t('aiRuntime.form.parameterSchema'));
    } catch (err) {
      setParseError((err as Error).message);
      return;
    }
    const value: RuntimeCLIInput = {
      key: fields.key.trim(),
      display_name: fields.display_name.trim(),
      executable: fields.executable.trim(),
      version_constraint: fields.version_constraint.trim(),
      required_features: splitKeys(fields.required_features),
      parameter_schema: schema,
      enabled: fields.enabled,
    };
    try {
      if (entry) {
        await update.mutateAsync({ id: entry.id, expectedRevision: revision, value });
      } else {
        await create.mutateAsync({ expectedRevision: revision, value });
      }
      onClose();
    } catch {
      // surfaced below
    }
  };

  return (
    <Modal testId="ai-runtime-cli-form" title={entry ? t('aiRuntime.form.editCLI') : t('aiRuntime.form.newCLI')} onClose={onClose}>
      {entry?.system && (
        <p className="rounded border border-status-amber-border bg-status-amber-bg px-3 py-2 text-xs text-status-amber-fg" data-testid="ai-runtime-system-hint">
          {t('aiRuntime.form.systemHint')}
        </p>
      )}
      <Field label={t('aiRuntime.form.key')}>
        <input className={inputClass} value={fields.key} disabled={!!entry} onChange={(e) => setFields({ ...fields, key: e.target.value })} data-testid="ai-runtime-cli-key" />
      </Field>
      {entry && <ImmutableKeyHint />}
      <div className="grid gap-3 md:grid-cols-2">
        <Field label={t('aiRuntime.cli.name')}>
          <input className={inputClass} value={fields.display_name} onChange={(e) => setFields({ ...fields, display_name: e.target.value })} data-testid="ai-runtime-cli-display-name" />
        </Field>
        <Field label={t('aiRuntime.cli.executable')}>
          <input className={inputClass} value={fields.executable} onChange={(e) => setFields({ ...fields, executable: e.target.value })} data-testid="ai-runtime-cli-executable" />
        </Field>
      </div>
      <div className="grid gap-3 md:grid-cols-2">
        <Field label={t('aiRuntime.cli.version')}>
          <input className={inputClass} value={fields.version_constraint} onChange={(e) => setFields({ ...fields, version_constraint: e.target.value })} data-testid="ai-runtime-cli-version" />
        </Field>
        <Field label={t('aiRuntime.cli.features')}>
          <input className={inputClass} value={fields.required_features} onChange={(e) => setFields({ ...fields, required_features: e.target.value })} data-testid="ai-runtime-cli-features" />
        </Field>
      </div>
      <Field label={t('aiRuntime.form.parameterSchema')}>
        <textarea className={`${inputClass} font-mono`} rows={5} value={fields.parameter_schema} onChange={(e) => setFields({ ...fields, parameter_schema: e.target.value })} data-testid="ai-runtime-cli-parameter-schema" />
      </Field>
      <Checkbox checked={fields.enabled} label={t('aiRuntime.form.enabled')} onChange={(enabled) => setFields({ ...fields, enabled })} testId="ai-runtime-cli-enabled" />
      <FormFooter
        busy={mutation.isPending}
        error={parseError || mutationErrorMessage(mutation.error)}
        saveDisabled={!fields.key.trim() || !fields.display_name.trim() || !fields.executable.trim()}
        onCancel={onClose}
        onSave={() => void submit()}
      />
    </Modal>
  );
}

function ModelImportModal({
  catalog,
  onClose,
}: {
  catalog: AIRuntimeCatalog;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const preview = usePreviewRuntimeImport();
  const apply = useApplyRuntimeImport();
  const [json, setJSON] = useState('');
  const [parseError, setParseError] = useState('');
  const [document, setDocument] = useState<AIRuntimeExportDocument | null>(null);

  const previewImport = async (): Promise<void> => {
    setParseError('');
    apply.reset();
    let doc: AIRuntimeExportDocument;
    try {
      const parsed = JSON.parse(json) as unknown;
      doc = buildModelsOnlyImportDocument(catalog, parsed);
    } catch (err) {
      setDocument(null);
      preview.reset();
      setParseError((err as Error).message);
      return;
    }
    setDocument(doc);
    try {
      await preview.mutateAsync({ strategy: 'merge', document: doc });
    } catch {
      // surfaced below, including report diagnostics when provided
    }
  };

  const applyImport = async (): Promise<void> => {
    if (!document || !preview.data?.validation_token) return;
    try {
      await apply.mutateAsync({
        strategy: 'merge',
        document,
        validation_token: preview.data.validation_token,
      });
    } catch {
      // surfaced below
    }
  };

  const previewItems = preview.data?.report.items ?? importErrorReport(preview.error)?.items ?? [];
  const diagnostics = preview.data?.report.diagnostics ?? importErrorReport(preview.error)?.diagnostics ?? [];
  const modelItems = previewItems.filter((item) => item.entity_type === 'model');
  const preservedCLIs = previewItems.filter((item) => item.entity_type === 'cli' && item.action === 'unchanged').length;

  return (
    <Modal testId="ai-runtime-model-import" title={t('aiRuntime.import.title')} onClose={onClose} wide>
      <p className="text-xs text-text-muted">{t('aiRuntime.import.help')}</p>
      <textarea
        rows={10}
        className={`${inputClass} font-mono`}
        value={json}
        onChange={(e) => {
          setJSON(e.target.value);
          setParseError('');
          setDocument(null);
          preview.reset();
          apply.reset();
        }}
        placeholder='[{"key":"gpt-5-mini","model_key":"gpt-5-mini","display_name":"GPT-5 mini","compatible_cli_keys":["codex"],"enabled":true}]'
        data-testid="ai-runtime-model-import-json"
      />
      <div className="rounded border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary" data-testid="ai-runtime-model-import-scope">
        {t('aiRuntime.import.scope', { clis: catalog.clis.length })}
      </div>
      {(parseError || preview.isError || apply.isError) && (
        <p className="text-xs text-danger" role="alert" data-testid="ai-runtime-model-import-error">
          {parseError || mutationErrorMessage(preview.error) || mutationErrorMessage(apply.error)}
        </p>
      )}
      {previewItems.length > 0 && (
        <div className="space-y-2" data-testid="ai-runtime-model-import-preview">
          <div className="flex flex-wrap gap-2 text-xs text-text-muted">
            <span>{t('aiRuntime.import.revision', { revision: preview.data?.report.revision ?? importErrorReport(preview.error)?.revision ?? catalog.revision })}</span>
            <span>{t('aiRuntime.import.preserved', { clis: preservedCLIs })}</span>
            {preview.data?.document_sha256 && <span className="font-mono">{preview.data.document_sha256.slice(0, 12)}</span>}
          </div>
          {modelItems.length === 0 ? (
            <p className="text-xs text-text-muted">{t('aiRuntime.import.noModelChanges')}</p>
          ) : (
            <div className="overflow-x-auto rounded border border-border-base">
              <table className="w-full min-w-[28rem] text-left text-xs">
                <thead className="bg-bg-subtle uppercase text-text-muted">
                  <tr>
                    <th className="px-3 py-2">{t('aiRuntime.import.entity')}</th>
                    <th className="px-3 py-2">{t('aiRuntime.import.key')}</th>
                    <th className="px-3 py-2">{t('aiRuntime.import.action')}</th>
                  </tr>
                </thead>
                <tbody>
                  {modelItems.map((item) => (
                    <tr key={`${item.entity_type}-${item.key}`} className="border-t border-border-base" data-testid="ai-runtime-model-import-change">
                      <td className="px-3 py-2">{item.entity_type}</td>
                      <td className="px-3 py-2 font-mono">{item.key}</td>
                      <td className="px-3 py-2">{item.action}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}
      {diagnostics.length > 0 && (
        <div className="rounded border border-status-amber-border bg-status-amber-bg p-3 text-xs text-status-amber-fg" data-testid="ai-runtime-model-import-diagnostics">
          <div className="font-medium">{t('aiRuntime.import.diagnostics')}</div>
          <ul className="mt-1 space-y-1">
            {diagnostics.map((d, i) => (
              <li key={`${d.path ?? 'diagnostic'}-${i}`}>
                <span className="font-mono">{d.path || d.key || d.code}</span>: {d.message}
              </li>
            ))}
          </ul>
        </div>
      )}
      {apply.isSuccess && (
        <p className="text-xs text-status-emerald-fg" data-testid="ai-runtime-model-import-applied">
          {t('aiRuntime.import.applied', { revision: apply.data.revision })}
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
          onClick={() => void previewImport()}
          data-testid="ai-runtime-model-import-preview-btn"
        >
          {preview.isPending ? t('aiRuntime.import.previewing') : t('aiRuntime.import.preview')}
        </button>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
          disabled={!preview.data?.validation_token || apply.isPending || apply.isSuccess}
          onClick={() => void applyImport()}
          data-testid="ai-runtime-model-import-apply"
        >
          {apply.isPending ? t('aiRuntime.import.applying') : t('aiRuntime.import.apply')}
        </button>
      </div>
    </Modal>
  );
}

function Modal({
  testId,
  title,
  onClose,
  children,
  wide,
}: {
  testId: string;
  title: string;
  onClose: () => void;
  children: React.ReactNode;
  wide?: boolean;
}): React.ReactElement {
  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-black/40 p-4" data-testid={testId}>
      <div className={`${wide ? 'max-w-3xl' : 'max-w-lg'} max-h-[90vh] w-full space-y-3 overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-4 shadow-2`} role="dialog" aria-modal="true" aria-label={title}>
        <div className="flex items-start justify-between gap-3">
          <h2 className="text-lg font-semibold text-text-primary">{title}</h2>
          <button type="button" className="rounded px-2 py-1 text-sm text-text-muted hover:bg-bg-subtle" onClick={onClose} aria-label="Close">
            x
          </button>
        </div>
        {children}
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <label className="block text-xs text-text-secondary">
      {label}
      <div className="mt-1">{children}</div>
    </label>
  );
}

function Checkbox({
  checked,
  label,
  onChange,
  testId,
}: {
  checked: boolean;
  label: string;
  onChange: (checked: boolean) => void;
  testId: string;
}): React.ReactElement {
  return (
    <label className="inline-flex items-center gap-2 text-sm text-text-secondary">
      <ToggleSwitch checked={checked} onChange={onChange} ariaLabel={label} testId={testId} />
      {label}
    </label>
  );
}

function ImmutableKeyHint(): React.ReactElement {
  const { t } = useTranslation('admin');
  return <p className="text-xs text-text-muted" data-testid="ai-runtime-immutable-key-hint">{t('aiRuntime.form.immutableKey')}</p>;
}

function FormFooter({
  busy,
  error,
  saveDisabled,
  onCancel,
  onSave,
}: {
  busy: boolean;
  error: string;
  saveDisabled: boolean;
  onCancel: () => void;
  onSave: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  return (
    <>
      {error && <p className="text-xs text-danger" role="alert" data-testid="ai-runtime-form-error">{error}</p>}
      <div className="flex justify-end gap-2 pt-1">
        <button type="button" className="rounded px-3 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle" onClick={onCancel}>
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
    </>
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

function mutationErrorMessage(error: unknown): string {
  return error instanceof Error ? error.message : '';
}

function importErrorReport(error: unknown) {
  return error instanceof RuntimeImportError ? error.report : undefined;
}

function parseJSONValue(raw: string, label: string): unknown {
  try {
    return raw.trim() === '' ? {} : JSON.parse(raw);
  } catch (err) {
    throw new Error(`${label}: ${(err as Error).message}`);
  }
}

function parseJSONObject(raw: string, label: string): Record<string, unknown> {
  const value = parseJSONValue(raw, label);
  if (!isPlainRecord(value)) {
    throw new Error(`${label}: ${label} must be a JSON object`);
  }
  return value;
}

function prettyJSON(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2);
}

function splitKeys(raw: string): string[] {
  return raw.split(',').map((v) => v.trim()).filter(Boolean);
}

function optionalNumber(raw: string): number | undefined {
  if (raw.trim() === '') return undefined;
  const parsed = Number(raw);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function numberText(value: number | undefined): string {
  return value === undefined || value === null ? '' : String(value);
}

function buildModelsOnlyImportDocument(catalog: AIRuntimeCatalog, raw: unknown): AIRuntimeExportDocument {
  if (!Array.isArray(raw)) {
    throw new Error('Import JSON must be an array of model definitions.');
  }
  const existingModels = new Map(catalog.models.map((model) => [model.key, exportModel(model)]));
  const mergedModels = new Map(existingModels);
  const seen = new Set<string>();
  const fallbackCLI = catalog.clis.find((cli) => cli.key === 'codex')?.key ?? catalog.clis[0]?.key;

  raw.forEach((item, index) => {
    if (!isPlainRecord(item)) {
      throw new Error(`models[${index}] must be a JSON object.`);
    }
    const key = stringField(item, 'key') ?? stringField(item, 'model_id') ?? stringField(item, 'model_key');
    if (!key) {
      throw new Error(`models[${index}].key is required.`);
    }
    if (seen.has(key)) {
      throw new Error(`duplicate model key "${key}"`);
    }
    seen.add(key);
    const base = existingModels.get(key);
    mergedModels.set(key, normalizeImportedModel(item, key, base, fallbackCLI, index));
  });

  return {
    schema_version: 1,
    kind: 'agent-center-ai-runtime',
    exported_at: new Date().toISOString(),
    runtime: {
      clis: catalog.clis.map(exportCLI),
      models: Array.from(mergedModels.values()).sort((a, b) => a.key.localeCompare(b.key)),
    },
  };
}

function normalizeImportedModel(
  raw: Record<string, unknown>,
  key: string,
  base: AIRuntimeExportModel | undefined,
  fallbackCLI: string | undefined,
  index: number,
): AIRuntimeExportModel {
  const compatibleRaw = raw.compatible_cli_keys;
  const compatible =
    stringArrayField(compatibleRaw) ??
    base?.compatible_cli_keys ??
    (fallbackCLI ? [fallbackCLI] : []);
  if (compatible.length === 0) {
    throw new Error(`models[${index}].compatible_cli_keys is required.`);
  }
  return {
    key,
    model_key: stringField(raw, 'model_key') ?? stringField(raw, 'model_id') ?? base?.model_key ?? key,
    display_name: stringField(raw, 'display_name') ?? base?.display_name ?? stringField(raw, 'model_key') ?? key,
    compatible_cli_keys: compatible,
    default_parameters: recordField(raw.default_parameters) ?? base?.default_parameters ?? {},
    enabled: boolField(raw.enabled) ?? base?.enabled ?? true,
    context_window: numberField(raw.context_window) ?? base?.context_window,
    input_cost_per_mtok: numberField(raw.input_cost_per_mtok) ?? numberField(raw.input_cost) ?? base?.input_cost_per_mtok,
    output_cost_per_mtok: numberField(raw.output_cost_per_mtok) ?? numberField(raw.output_cost) ?? base?.output_cost_per_mtok,
    tier: stringField(raw, 'tier') ?? base?.tier,
  };
}

function exportCLI(cli: RuntimeCLI): AIRuntimeExportCLI {
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

function exportModel(model: RuntimeModel): AIRuntimeExportModel {
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

function stringField(raw: Record<string, unknown>, key: string): string | undefined {
  const value = raw[key];
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : undefined;
}

function numberField(value: unknown): number | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) ? parsed : undefined;
}

function boolField(value: unknown): boolean | undefined {
  return typeof value === 'boolean' ? value : undefined;
}

function stringArrayField(value: unknown): string[] | undefined {
  if (Array.isArray(value)) {
    const out = value.filter((v): v is string => typeof v === 'string').map((v) => v.trim()).filter(Boolean);
    return out.length === value.length ? out : undefined;
  }
  if (typeof value === 'string') return splitKeys(value);
  return undefined;
}

function recordField(value: unknown): Record<string, unknown> | undefined {
  return value === undefined ? undefined : isPlainRecord(value) ? value : undefined;
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value);
}

const inputClass = 'w-full rounded border border-border-base bg-bg-subtle px-2 py-1.5 text-sm text-text-primary disabled:cursor-not-allowed disabled:opacity-60';
