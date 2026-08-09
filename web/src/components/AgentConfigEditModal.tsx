// AgentConfigEditModal — T236: edit an agent's LLM config (CLI / Model /
// Reasoning / Mode / Provider). Saving persists the config and then RESTARTS the
// agent so the change takes effect, gated behind a second confirmation ("this
// will restart the agent — continue?") per the task. A stopped agent needs no
// restart (the new config applies on its next start), so the confirm wording +
// action adapt to lifecycle.
import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useRestartAgent, useUpdateAgentConfig } from '@/api/agents';
import { useAIRuntimeCatalog } from '@/api/aiRuntime';
import { useWorkers } from '@/api/workers';
import type { Agent, ExecutorProfile } from '@/api/types';
import { useModalA11y } from './useModalA11y';
import { ConfirmModal } from './ConfirmModal';
import { executorBadgeClass } from './executorProfiles';
import { ToggleSwitch } from './ToggleSwitch';
import {
  coerceRuntimePair,
  normalizeRuntimePairs,
  runtimeCLIOptions,
  runtimeModelOptions,
  validateRuntimePair,
} from './runtimeSelection';

interface Props {
  agent: Agent;
  onClose: () => void;
}

// Reasoning effort allowlist (backend agent.SupportedReasoningEfforts); "" = the
// runtime default.
const REASONING_OPTIONS = ['', 'minimal', 'low', 'medium', 'high'];

export function AgentConfigEditModal({ agent, onClose }: Props): React.ReactElement {
  const { t } = useTranslation('members');
  const [description, setDescription] = useState(agent.description ?? '');
  const [model, setModel] = useState(agent.model ?? '');
  const [cli, setCli] = useState(agent.cli ?? '');
  const [reasoning, setReasoning] = useState(agent.reasoning ?? '');
  const [mode, setMode] = useState(agent.mode ?? '');
  const [provider, setProvider] = useState(agent.provider ?? '');
  const [envText, setEnvText] = useState(formatEnvVars(agent.env_vars ?? {}));
  const [envError, setEnvError] = useState<string | null>(null);
  const [runtimeError, setRuntimeError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);

  // v2.18.1 concurrency config.
  const [maxConcurrent, setMaxConcurrent] = useState(agent.max_concurrent_tasks ?? 0);
  const [executors, setExecutors] = useState<ExecutorProfile[]>(agent.allowed_executors ?? []);
  // The pending "add a profile" row (committed via the Add button).
  const [draftCli, setDraftCli] = useState('');
  const [draftModel, setDraftModel] = useState('');

  const addExecutor = () => {
    const validation = validateRuntimePair(runtime.data, selectedWorker, { cli: draftCli, model: draftModel });
    if (!validation.ok) {
      setRuntimeError(t('agentRuntime.runtimeSelection.invalidExecutor'));
      return;
    }
    const { cli: nextCli, model: nextModel } = validation.pair;
    // Skip exact {cli,model} duplicates (the server dedups too).
    if (executors.some((e) => e.cli === nextCli && e.model === nextModel)) {
      setDraftModel('');
      return;
    }
    setExecutors((xs) => [...xs, { cli: nextCli, model: nextModel }]);
    setDraftModel('');
    setRuntimeError(null);
  };
  const removeExecutor = (i: number) => {
    setExecutors((xs) => xs.filter((_, idx) => idx !== i));
    setRuntimeError(null);
  };

  const trulyParallel = maxConcurrent >= 2 && executors.length > 0;

  // T566 (issue-577a7b0e): per-agent auto-assign opt-out (default true).
  const [autoAssignable, setAutoAssignable] = useState(agent.auto_assignable ?? true);
  const [executorGitWorktree, setExecutorGitWorktree] = useState(
    agent.executor_git_worktree ?? false,
  );

  // T728 (issue-0619f315): inject the agent's description into its system prompt
  // (default true). Echoes the persisted value so editing shows the current state.
  const [includeDescription, setIncludeDescription] = useState(
    agent.include_description_in_system_prompt ?? true,
  );

  const update = useUpdateAgentConfig(agent.id);
  const restart = useRestartAgent(agent.id);
  const runtime = useAIRuntimeCatalog();
  const workers = useWorkers();
  const containerRef = useModalA11y({ open: true, onClose });
  const selectedWorker = useMemo(
    () => (workers.data ?? []).find((w) => w.worker_id === agent.worker_id),
    [agent.worker_id, workers.data],
  );
  const cliOptions = runtimeCLIOptions(runtime.data, selectedWorker);
  const modelOptions = runtimeModelOptions(runtime.data, selectedWorker, cli);
  const draftModelOptions = runtimeModelOptions(runtime.data, selectedWorker, draftCli);
  const mainRuntimeValidation = validateRuntimePair(runtime.data, selectedWorker, { cli, model });
  const draftRuntimeValidation = validateRuntimePair(runtime.data, selectedWorker, { cli: draftCli, model: draftModel });

  const isRunning = agent.lifecycle === 'running';
  const busy = update.isPending || restart.isPending;
  const error = (update.error ?? restart.error) as Error | null;

  useEffect(() => {
    if (!runtime.data || !selectedWorker) return;
    const validation = validateRuntimePair(runtime.data, selectedWorker, { cli, model });
    if (validation.ok && (validation.pair.cli !== cli || validation.pair.model !== model)) {
      setCli(validation.pair.cli);
      setModel(validation.pair.model);
    }
  }, [cli, model, runtime.data, selectedWorker]);

  useEffect(() => {
    if (!runtime.data || !selectedWorker) return;
    const next = coerceRuntimePair(runtime.data, selectedWorker, { cli: draftCli, model: draftModel });
    if (!next) return;
    if (next.cli !== draftCli || next.model !== draftModel) {
      setDraftCli(next.cli);
      setDraftModel(next.model);
    }
  }, [draftCli, draftModel, runtime.data, selectedWorker]);

  const parseCurrentEnv = () =>
    parseEnvVars(envText, {
      format: t('agentRuntime.configModal.env.errorFormat'),
      name: (name) => t('agentRuntime.configModal.env.errorName', { name: name || t('agentRuntime.configModal.env.emptyName') }),
    });

  const validateRuntimeConfig = ():
    | { ok: true; main: ExecutorProfile; executors: ExecutorProfile[] }
    | { ok: false; message: string } => {
    const main = validateRuntimePair(runtime.data, selectedWorker, { cli, model });
    if (!main.ok) {
      return { ok: false, message: t('agentRuntime.runtimeSelection.invalidMain') };
    }
    const normalizedExecutors = normalizeRuntimePairs(runtime.data, selectedWorker, executors);
    if (!normalizedExecutors.ok) {
      return { ok: false, message: t('agentRuntime.runtimeSelection.invalidExecutor') };
    }
    return { ok: true, main: main.pair, executors: normalizedExecutors.pairs };
  };

  const apply = async () => {
    const parsedEnv = parseCurrentEnv();
    if (!parsedEnv.ok) {
      setEnvError(parsedEnv.error);
      setConfirming(false);
      return;
    }
    const runtimeConfig = validateRuntimeConfig();
    if (!runtimeConfig.ok) {
      setRuntimeError(runtimeConfig.message);
      setConfirming(false);
      return;
    }
    try {
      await update.mutateAsync({
        model: runtimeConfig.main.model,
        cli: runtimeConfig.main.cli,
        reasoning,
        mode: mode.trim(),
        provider: provider.trim(),
        env_vars: parsedEnv.env,
        max_concurrent_tasks: maxConcurrent,
        allowed_executors: runtimeConfig.executors,
        auto_assignable: autoAssignable,
        executor_git_worktree: executorGitWorktree,
        description: description.trim(),
        include_description_in_system_prompt: includeDescription,
      });
      // A running agent must restart to pick up the new config; a stopped agent
      // applies it on its next start (nothing to restart now).
      if (isRunning) {
        await restart.mutateAsync();
      }
      onClose();
    } catch {
      // Surfaced via `error`; keep the modal open so the operator can retry.
      setConfirming(false);
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-40 flex items-center justify-center bg-black/50 p-4"
      data-testid="agent-config-edit-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t('agentRuntime.configModal.dialogLabel')}
    >
      <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">{t('agentRuntime.configModal.title')}</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label={t('agentRuntime.configModal.close')}
            data-testid="agent-config-edit-close"
          >
            <span aria-hidden="true">X</span>
          </button>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            const parsedEnv = parseCurrentEnv();
            if (!parsedEnv.ok) {
              setEnvError(parsedEnv.error);
              return;
            }
            const runtimeConfig = validateRuntimeConfig();
            if (!runtimeConfig.ok) {
              setRuntimeError(runtimeConfig.message);
              return;
            }
            setEnvError(null);
            setRuntimeError(null);
            setConfirming(true);
          }}
        >
          <Field label={t('agentRuntime.configModal.fields.description')} htmlFor="agent-config-description-input">
            <input
              id="agent-config-description-input"
              data-testid="agent-config-description"
              className={inputClass}
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t('agentRuntime.configModal.fields.descriptionPlaceholder')}
            />
          </Field>

          <Field label={t('agentRuntime.configModal.fields.cli')} htmlFor="agent-config-cli-input">
            <select
              id="agent-config-cli-input"
              data-testid="agent-config-cli"
              className={inputClass}
              value={cli}
              onChange={(e) => {
                const nextCli = e.target.value;
                const next = coerceRuntimePair(runtime.data, selectedWorker, { cli: nextCli, model });
                setCli(next?.cli ?? nextCli);
                setModel(next?.model ?? '');
                setRuntimeError(null);
              }}
              disabled={!runtime.data || !selectedWorker || cliOptions.length === 0}
            >
              {cli && !cliOptions.some((option) => option.key === cli) && (
                <option value={cli} disabled>
                  {t('agentRuntime.runtimeSelection.legacyOption', { value: cli })}
                </option>
              )}
              {cliOptions.map((option) => (
                <option key={option.key} value={option.key}>
                  {runtimeOptionLabel(option.display_name, option.key)}
                </option>
              ))}
            </select>
            <RuntimeSelectionHint
              testId="agent-config-runtime-hint"
              loading={runtime.isLoading || workers.isLoading}
              workerSelected={!!selectedWorker}
              cliCount={cliOptions.length}
              modelCount={modelOptions.length}
              invalid={runtime.isSuccess && workers.isSuccess && !mainRuntimeValidation.ok}
            />
          </Field>

          <Field label={t('agentRuntime.configModal.fields.model')} htmlFor="agent-config-model-input">
            <select
              id="agent-config-model-input"
              data-testid="agent-config-model"
              className={inputClass}
              value={model}
              onChange={(e) => {
                setModel(e.target.value);
                setRuntimeError(null);
              }}
              disabled={!runtime.data || !selectedWorker || modelOptions.length === 0}
            >
              {model && !modelOptions.some((option) => option.model_key === model) && (
                <option value={model} disabled>
                  {t('agentRuntime.runtimeSelection.legacyOption', { value: model })}
                </option>
              )}
              {modelOptions.map((option) => (
                <option key={option.key} value={option.model_key}>
                  {runtimeOptionLabel(option.display_name, option.model_key)}
                </option>
              ))}
            </select>
          </Field>

          <Field label={t('agentRuntime.configModal.fields.reasoning')} hint={t('agentRuntime.configModal.fields.reasoningHint')} htmlFor="agent-config-reasoning-input">
            <select
              id="agent-config-reasoning-input"
              data-testid="agent-config-reasoning"
              className={inputClass}
              value={reasoning}
              onChange={(e) => setReasoning(e.target.value)}
            >
              {REASONING_OPTIONS.map((r) => (
                <option key={r || 'default'} value={r}>
                  {r || t('agentRuntime.configModal.fields.reasoningDefault')}
                </option>
              ))}
            </select>
          </Field>

          <Field label={t('agentRuntime.configModal.fields.mode')} hint={t('agentRuntime.configModal.fields.modeHint')} htmlFor="agent-config-mode-input">
            <input
              id="agent-config-mode-input"
              data-testid="agent-config-mode"
              className={inputClass}
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              placeholder={t('agentRuntime.configModal.fields.modePlaceholder')}
            />
          </Field>

          <Field label={t('agentRuntime.configModal.fields.provider')} hint={t('agentRuntime.configModal.fields.providerHint')} htmlFor="agent-config-provider-input">
            <input
              id="agent-config-provider-input"
              data-testid="agent-config-provider"
              className={inputClass}
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
              placeholder={t('agentRuntime.configModal.fields.providerPlaceholder')}
            />
          </Field>

          <Field
            label={t('agentRuntime.configModal.env.heading')}
            hint={t('agentRuntime.configModal.env.hint')}
            htmlFor="agent-config-env-input"
          >
            <textarea
              id="agent-config-env-input"
              data-testid="agent-config-env"
              className={`${inputClass} min-h-28 font-mono text-xs`}
              value={envText}
              onChange={(e) => {
                setEnvText(e.target.value);
                setEnvError(null);
              }}
              placeholder={t('agentRuntime.configModal.env.placeholder')}
              spellCheck={false}
            />
            {envError && (
              <p className="mt-1 text-[0.6875rem] text-danger" data-testid="agent-config-env-error">
                {envError}
              </p>
            )}
          </Field>

          {/* v2.18.1 (issue-8746a5b9): executor concurrency config. */}
          <div className="mb-3 mt-5 border-t border-border-base pt-4" data-testid="agent-config-concurrency">
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">
              {t('agentRuntime.configModal.concurrency.heading')}
            </h3>

            <Field
              label={t('agentRuntime.configModal.concurrency.maxLabel')}
              hint={t('agentRuntime.configModal.concurrency.maxHint')}
              htmlFor="agent-config-max-concurrent-input"
            >
              <input
                id="agent-config-max-concurrent-input"
                data-testid="agent-config-max-concurrent"
                type="number"
                min={0}
                className={inputClass}
                value={maxConcurrent}
                onChange={(e) => setMaxConcurrent(Math.max(0, Math.floor(Number(e.target.value) || 0)))}
              />
            </Field>

            <Field
              label={t('agentRuntime.configModal.concurrency.executorsLabel')}
              hint={t('agentRuntime.configModal.concurrency.executorsHint')}
            >
              {executors.length > 0 ? (
                <ul className="mb-2 flex flex-wrap gap-2" data-testid="agent-config-executors">
                  {executors.map((e, i) => {
                    const supported = validateRuntimePair(runtime.data, selectedWorker, e).ok;
                    return (
                      <li
                        key={`${e.cli}::${e.model}`}
                        className={[
                          'inline-flex items-center gap-1.5 rounded border bg-bg-subtle px-2 py-1 text-xs',
                          supported ? 'border-border-base' : 'border-danger/40',
                        ].join(' ')}
                        data-testid="agent-config-executor-chip"
                        data-supported={supported ? 'true' : 'false'}
                      >
                        <span className={`rounded px-1 py-0.5 text-[0.5625rem] font-medium uppercase tracking-wide ${executorBadgeClass(e.cli)}`}>
                          {e.cli}
                        </span>
                        <span className="font-mono text-text-primary">{e.model}</span>
                        {!supported && (
                          <span className="text-[0.625rem] text-danger">
                            {t('agentRuntime.runtimeSelection.unsupportedShort')}
                          </span>
                        )}
                        <button
                          type="button"
                          className="text-text-muted hover:text-danger"
                          onClick={() => removeExecutor(i)}
                          aria-label={t('agentRuntime.configModal.concurrency.removeExecutor', { cli: e.cli, model: e.model })}
                          data-testid="agent-config-executor-remove"
                        >
                          <span aria-hidden="true">×</span>
                        </button>
                      </li>
                    );
                  })}
                </ul>
              ) : (
                <p className="mb-2 text-[0.6875rem] italic text-text-muted" data-testid="agent-config-executors-empty">
                  {t('agentRuntime.configModal.concurrency.executorsEmpty')}
                </p>
              )}

              <div className="flex gap-2">
                <select
                  className={`${inputClass} w-auto`}
                  value={draftCli}
                  onChange={(e) => {
                    const nextCli = e.target.value;
                    const next = coerceRuntimePair(runtime.data, selectedWorker, { cli: nextCli, model: draftModel });
                    setDraftCli(next?.cli ?? nextCli);
                    setDraftModel(next?.model ?? '');
                    setRuntimeError(null);
                  }}
                  data-testid="agent-config-executor-cli"
                  aria-label={t('agentRuntime.configModal.concurrency.executorCli')}
                  disabled={!runtime.data || !selectedWorker || cliOptions.length === 0}
                >
                  {draftCli && !cliOptions.some((option) => option.key === draftCli) && (
                    <option value={draftCli} disabled>
                      {t('agentRuntime.runtimeSelection.legacyOption', { value: draftCli })}
                    </option>
                  )}
                  {cliOptions.map((option) => (
                    <option key={option.key} value={option.key}>
                      {runtimeOptionLabel(option.display_name, option.key)}
                    </option>
                  ))}
                </select>
                <select
                  className={inputClass}
                  value={draftModel}
                  onChange={(e) => {
                    setDraftModel(e.target.value);
                    setRuntimeError(null);
                  }}
                  data-testid="agent-config-executor-model"
                  aria-label={t('agentRuntime.configModal.concurrency.executorModel')}
                  disabled={!runtime.data || !selectedWorker || draftModelOptions.length === 0}
                >
                  {draftModel && !draftModelOptions.some((option) => option.model_key === draftModel) && (
                    <option value={draftModel} disabled>
                      {t('agentRuntime.runtimeSelection.legacyOption', { value: draftModel })}
                    </option>
                  )}
                  {draftModelOptions.map((option) => (
                    <option key={option.key} value={option.model_key}>
                      {runtimeOptionLabel(option.display_name, option.model_key)}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="shrink-0 rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle disabled:cursor-not-allowed disabled:text-text-muted"
                  onClick={addExecutor}
                  disabled={!draftRuntimeValidation.ok}
                  data-testid="agent-config-executor-add"
                >
                  {t('agentRuntime.configModal.concurrency.add')}
                </button>
              </div>
            </Field>

            <p
              className={`rounded px-2 py-1.5 text-xs ${trulyParallel ? 'bg-status-green-bg text-status-green-fg' : 'bg-bg-subtle text-text-muted'}`}
              data-testid="agent-config-concurrency-status"
              data-enabled={trulyParallel}
            >
              {trulyParallel
                ? t('agentRuntime.configModal.concurrency.enabled', { count: maxConcurrent })
                : t('agentRuntime.configModal.concurrency.disabled')}
            </p>
          </div>

          <div className="mb-3 mt-5 border-t border-border-base pt-4" data-testid="agent-config-git-worktree-section">
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">
              {t('agentRuntime.configModal.gitWorktree.heading')}
            </h3>
            <div className="flex items-start gap-2.5">
              <ToggleSwitch
                checked={executorGitWorktree}
                onChange={setExecutorGitWorktree}
                ariaLabel={t('agentRuntime.configModal.gitWorktree.ariaLabel')}
                testId="agent-config-executor-git-worktree"
              />
              <span className="text-xs">
                <span className="font-medium text-text-primary">{t('agentRuntime.configModal.gitWorktree.label')}</span>
                <span className="mt-0.5 block text-[0.6875rem] text-text-muted">
                  {t('agentRuntime.configModal.gitWorktree.description')}
                </span>
              </span>
            </div>
          </div>

          {/* T566 (issue-577a7b0e): per-agent auto-assign opt-out. */}
          <div className="mb-3 mt-5 border-t border-border-base pt-4" data-testid="agent-config-auto-assign-section">
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">
              {t('agentRuntime.configModal.autoAssign.heading')}
            </h3>
            <div className="flex items-start gap-2.5">
              <ToggleSwitch
                checked={autoAssignable}
                onChange={setAutoAssignable}
                ariaLabel={t('agentRuntime.configModal.autoAssign.ariaLabel')}
                testId="agent-config-auto-assignable"
              />
              <span className="text-xs">
                <span className="font-medium text-text-primary">{t('agentRuntime.configModal.autoAssign.label')}</span>
                <span className="mt-0.5 block text-[0.6875rem] text-text-muted">
                  {t('agentRuntime.configModal.autoAssign.description')}
                </span>
              </span>
            </div>
          </div>

          {/* T728 (issue-0619f315): inject description into the system prompt. */}
          <div className="mb-3 mt-5 border-t border-border-base pt-4" data-testid="agent-config-desc-prompt-section">
            <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">
              {t('agentRuntime.configModal.descriptionPrompt.heading')}
            </h3>
            <div className="flex items-start gap-2.5">
              <ToggleSwitch
                checked={includeDescription}
                onChange={setIncludeDescription}
                ariaLabel={t('agentRuntime.configModal.descriptionPrompt.ariaLabel')}
                testId="agent-config-include-description"
              />
              <span className="text-xs">
                <span className="font-medium text-text-primary">{t('agentRuntime.configModal.descriptionPrompt.label')}</span>
                <span className="mt-0.5 block text-[0.6875rem] text-text-muted">
                  {t('agentRuntime.configModal.descriptionPrompt.description')}
                </span>
                <span className="mt-0.5 block text-[0.6875rem] italic text-text-muted" data-testid="agent-config-include-description-restart-hint">
                  {t('agentRuntime.configModal.descriptionPrompt.restartHint')}
                </span>
              </span>
            </div>
          </div>

          {error && (
            <p className="mb-3 text-xs text-danger" data-testid="agent-config-edit-error">
              {error.message}
            </p>
          )}

          {runtimeError && (
            <p className="mb-3 text-xs text-danger" data-testid="agent-config-runtime-error">
              {runtimeError}
            </p>
          )}

          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              onClick={onClose}
              data-testid="agent-config-edit-cancel"
            >
              {t('agentRuntime.configModal.cancel')}
            </button>
            <button
              type="submit"
              disabled={busy}
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
              data-testid="agent-config-edit-save"
            >
              {t('agentRuntime.configModal.save')}
            </button>
          </div>
        </form>
      </div>

      {/* Second confirmation: applying config restarts a running agent. */}
      <ConfirmModal
        open={confirming}
        title={isRunning ? t('agentRuntime.configModal.confirm.titleRestart') : t('agentRuntime.configModal.confirm.titleSave')}
        message={
          isRunning
            ? t('agentRuntime.configModal.confirm.messageRestart')
            : t('agentRuntime.configModal.confirm.messageSave')
        }
        confirmLabel={isRunning ? t('agentRuntime.configModal.confirm.labelRestart') : t('agentRuntime.configModal.confirm.labelSave')}
        busy={busy}
        onConfirm={() => void apply()}
        onCancel={() => setConfirming(false)}
      />
    </div>
  );
}

function formatEnvVars(env: Record<string, string>): string {
  return Object.keys(env)
    .sort()
    .map((k) => `${k}=${env[k] ?? ''}`)
    .join('\n');
}

type ParseEnvResult = { ok: true; env: Record<string, string> } | { ok: false; error: string };

function parseEnvVars(text: string, errors: { format: string; name: (name: string) => string }): ParseEnvResult {
  const env: Record<string, string> = {};
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line) continue;
    const eq = line.indexOf('=');
    if (eq <= 0) {
      return { ok: false, error: errors.format };
    }
    const key = line.slice(0, eq).trim();
    if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) {
      return { ok: false, error: errors.name(key) };
    }
    env[key] = line.slice(eq + 1);
  }
  return { ok: true, env };
}

function Field({
  label,
  hint,
  htmlFor,
  children,
}: {
  label: string;
  hint?: string;
  htmlFor?: string;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-3">
      <label htmlFor={htmlFor} className="mb-1 block text-xs font-medium text-text-primary">{label}</label>
      {children}
      {hint && <p className="mt-1 text-[0.6875rem] text-text-muted">{hint}</p>}
    </div>
  );
}

function RuntimeSelectionHint({
  testId,
  loading,
  workerSelected,
  cliCount,
  modelCount,
  invalid,
}: {
  testId: string;
  loading: boolean;
  workerSelected: boolean;
  cliCount: number;
  modelCount: number;
  invalid: boolean;
}): React.ReactElement | null {
  const { t } = useTranslation('members');
  let message = '';
  if (loading) message = t('agentRuntime.runtimeSelection.loading');
  else if (!workerSelected) message = t('agentRuntime.runtimeSelection.noWorker');
  else if (cliCount === 0) message = t('agentRuntime.runtimeSelection.noCLI');
  else if (modelCount === 0) message = t('agentRuntime.runtimeSelection.noModel');
  else if (invalid) message = t('agentRuntime.runtimeSelection.unsupportedLegacy');
  if (!message) return null;
  return (
    <p className="mt-1 text-[0.6875rem] text-text-muted" data-testid={testId}>
      {message}
    </p>
  );
}

function runtimeOptionLabel(displayName: string | undefined, key: string): string {
  return displayName && displayName !== key ? `${displayName} (${key})` : key;
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
