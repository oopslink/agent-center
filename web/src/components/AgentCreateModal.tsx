// AgentCreateModal — Agent BC "Add Agent" form. v2.7 #186/#77: POST /api/agents
// was removed ("no middle state" — agent always has a member id), so this posts
// to the unified POST /api/members/agent (atomic identity-member + execution
// Agent, #157). The Worker picker is sourced from the Environment snapshot
// (useFleet().workers); name + worker_id are required; description/model/cli
// optional. (Declared skills removed in issue-4a45e9cc — skills are now OBSERVED
// per-agent.) The created agent's business id = response identity_id.
import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAddAgentMember } from '@/api/members';
import { useFleet } from '@/api/fleet';
import { useAIRuntimeCatalog } from '@/api/aiRuntime';
import { EntitySelect } from './EntitySelect';
import { ToggleSwitch } from './ToggleSwitch';
import { deriveRuntimeChoices, validateRuntimePair } from './executorProfiles';

interface Props {
  onClose: () => void;
}

export function AgentCreateModal({ onClose }: Props): React.ReactElement {
  const { t } = useTranslation('members');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [model, setModel] = useState('');
  const [cli, setCli] = useState('');
  const [workerId, setWorkerId] = useState('');
  const [validationError, setValidationError] = useState<string | null>(null);
  // T728 (issue-0619f315): inject the description into the agent's system prompt.
  // Default ON — matches the backend default (nil → true).
  const [includeDescription, setIncludeDescription] = useState(true);
  const create = useAddAgentMember();
  const fleet = useFleet();
  const runtimeCatalog = useAIRuntimeCatalog();
  const workers = fleet.data?.workers ?? [];
  const selectedWorker = workers.find((w) => w.worker_id === workerId);
  const runtimeChoices = useMemo(
    () => deriveRuntimeChoices(runtimeCatalog.data, selectedWorker),
    [runtimeCatalog.data, selectedWorker],
  );
  const cliSelectDisabled = !workerId || runtimeChoices.cliOptions.length === 0;
  const selectedModelOptions = runtimeChoices.modelOptionsByCli[cli] ?? [];
  const modelSelectDisabled = !workerId || !cli || selectedModelOptions.length === 0;
  const cliPlaceholder = !workerId
    ? t('agents.create.cliSelectWorkerPlaceholder')
    : t('agents.create.cliEmpty');
  const modelPlaceholder = !workerId
    ? t('agents.create.modelSelectWorkerPlaceholder')
    : (!cli ? t('agents.create.modelSelectCliPlaceholder') : t('agents.create.modelEmpty'));

  useEffect(() => {
    if (!workerId || !runtimeChoices.defaultCli) return;
    setCli((current) =>
      runtimeChoices.cliOptions.some((option) => option.value === current)
        ? current
        : runtimeChoices.defaultCli,
    );
  }, [runtimeChoices, workerId]);

  useEffect(() => {
    if (!workerId || !cli) return;
    const modelOptions = runtimeChoices.modelOptionsByCli[cli] ?? [];
    const fallbackModel =
      cli === runtimeChoices.defaultCli ? runtimeChoices.defaultModel : (modelOptions[0]?.value ?? '');
    setModel((current) =>
      modelOptions.some((option) => option.value === current)
        ? current
        : fallbackModel,
    );
  }, [cli, runtimeChoices, workerId]);

  const trimmedName = name.trim();
  const validationMessages = {
    catalogLoading: t('agents.create.validation.catalogLoading'),
    catalogMissing: t('agents.create.validation.catalogMissing'),
    workerMissing: t('agents.create.validation.workerMissing'),
    cliUnavailable: (value: string) => t('agents.create.validation.cliUnavailable', { cli: value }),
    modelUnavailable: (value: string, cliValue: string) =>
      t('agents.create.validation.modelUnavailable', { model: value, cli: cliValue }),
    executorUnavailable: (cliValue: string, modelValue: string) =>
      t('agents.create.validation.executorUnavailable', { cli: cliValue, model: modelValue }),
  };
  const runtimeError = validateRuntimePair(runtimeChoices, cli, model, validationMessages, {
    catalogLoading: runtimeCatalog.isLoading,
    catalogReady: runtimeCatalog.isSuccess,
    workerSelected: !!workerId,
  });
  const canSubmit = trimmedName.length > 0 && !runtimeError && !create.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    const submitError = validateRuntimePair(runtimeChoices, cli, model, validationMessages, {
      catalogLoading: runtimeCatalog.isLoading,
      catalogReady: runtimeCatalog.isSuccess,
      workerSelected: !!workerId,
    });
    if (submitError) {
      setValidationError(submitError);
      return;
    }
    if (!canSubmit) return;
    try {
      await create.mutateAsync({
        display_name: trimmedName,
        description: description.trim() || undefined,
        role: 'member',
        model: model.trim(),
        cli,
        worker_id: workerId,
        include_description_in_system_prompt: includeDescription,
      });
      onClose();
    } catch {
      // surfaced via create.error below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="agent-create-modal"
      role="dialog"
      aria-modal="true"
      aria-labelledby="agent-create-title"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 id="agent-create-title" className="text-lg font-semibold">{t('agents.create.title')}</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label={t('agents.create.close')}
            data-testid="agent-create-close"
          >
            X
          </button>
        </div>

        <Field label={t('agents.create.nameLabel')} required htmlFor="agent-create-name-input">
          <input
            id="agent-create-name-input"
            data-testid="agent-create-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t('agents.create.namePlaceholder')}
            className={inputClass}
          />
        </Field>

        <Field label={t('agents.create.descriptionLabel')} hint={t('agents.create.descriptionHint')} htmlFor="agent-create-desc-input">
          <textarea
            id="agent-create-desc-input"
            data-testid="agent-create-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={2}
            className={inputClass}
          />
        </Field>

        {/* T728: inject the description into the system prompt (default on). */}
        <div className="mb-3 flex items-start gap-2.5" data-testid="agent-create-desc-prompt-section">
          <ToggleSwitch
            checked={includeDescription}
            onChange={setIncludeDescription}
            ariaLabel={t('agents.create.descriptionPrompt.ariaLabel')}
            testId="agent-create-include-description"
          />
          <span className="text-xs">
            <span className="font-medium text-text-primary">{t('agents.create.descriptionPrompt.label')}</span>
            <span className="mt-0.5 block text-[0.6875rem] text-text-muted">
              {t('agents.create.descriptionPrompt.description')}
            </span>
          </span>
        </div>

        <Field label={t('agents.create.workerLabel')} required hint={t('agents.create.workerHint')}>
          {/* v2.7 #191: shared searchable EntitySelect instead of a raw <select>. */}
          <EntitySelect
            testId="agent-create-worker"
            value={workerId}
            onChange={(next) => {
              setWorkerId(next);
              setValidationError(null);
            }}
            options={workers.map((w) => ({
              value: w.worker_id,
              label: w.name || w.worker_id,
              badge: w.status,
            }))}
            placeholder={t('agents.create.workerSelectPlaceholder')}
            searchPlaceholder={t('agents.create.workerSearchPlaceholder')}
            emptyLabel={t('agents.create.workerEmptyLabel')}
            ariaLabel={t('agents.create.workerLabel')}
          />
          {fleet.isSuccess && workers.length === 0 && (
            <p className="mt-1 text-[0.6875rem] text-text-muted" data-testid="agent-create-no-workers">
              {t('agents.create.noWorkers')}
            </p>
          )}
        </Field>

        <Field label={t('agents.create.cliLabel')} hint={t('agents.create.cliHint')} htmlFor="agent-create-cli-input">
          <select
            id="agent-create-cli-input"
            data-testid="agent-create-cli"
            value={cli}
            onChange={(e) => {
              setCli(e.target.value);
              setValidationError(null);
            }}
            className={inputClass}
            disabled={cliSelectDisabled}
          >
            {cliSelectDisabled && <option value="">{cliPlaceholder}</option>}
            {runtimeChoices.cliOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </Field>

        <Field label={t('agents.create.modelLabel')} hint={t('agents.create.modelHint')} htmlFor="agent-create-model-input">
          <select
            id="agent-create-model-input"
            data-testid="agent-create-model"
            value={model}
            onChange={(e) => {
              setModel(e.target.value);
              setValidationError(null);
            }}
            className={inputClass}
            disabled={modelSelectDisabled}
          >
            {modelSelectDisabled && <option value="">{modelPlaceholder}</option>}
            {selectedModelOptions.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </Field>

        {(validationError || (workerId && runtimeError)) && (
          <p className="mb-3 text-xs text-danger" data-testid="agent-create-validation-error">
            {validationError ?? runtimeError}
          </p>
        )}

        {create.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="agent-create-error">
            {(create.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="agent-create-cancel"
          >
            {t('agents.create.cancel')}
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="agent-create-submit"
          >
            {create.isPending ? t('agents.create.submitting') : t('agents.create.submit')}
          </button>
        </div>
      </form>
    </div>
  );
}

function Field({
  label,
  hint,
  required,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  htmlFor?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-3">
      <label htmlFor={htmlFor} className="mb-1 block text-xs font-medium text-text-primary">
        {label}
        {required && <span className="ml-1 text-danger">*</span>}
      </label>
      {children}
      {hint && <p className="mt-1 text-[0.6875rem] text-text-muted">{hint}</p>}
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
