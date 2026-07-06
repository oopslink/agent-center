// AgentCreateModal — Agent BC "Add Agent" form. v2.7 #186/#77: POST /api/agents
// was removed ("no middle state" — agent always has a member id), so this posts
// to the unified POST /api/members/agent (atomic identity-member + execution
// Agent, #157). The Worker picker is sourced from the Environment snapshot
// (useFleet().workers); name + worker_id are required; description/model/cli
// optional. (Declared skills removed in issue-4a45e9cc — skills are now OBSERVED
// per-agent.) The created agent's business id = response identity_id.
import React, { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAddAgentMember } from '@/api/members';
import { useFleet } from '@/api/fleet';
import { DEFAULT_AGENT_MODEL, KNOWN_MODELS } from '@/config/agent-defaults';
import { EntitySelect } from './EntitySelect';
import { ToggleSwitch } from './ToggleSwitch';

interface Props {
  onClose: () => void;
}

export function AgentCreateModal({ onClose }: Props): React.ReactElement {
  const { t } = useTranslation('members');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  // v2.7.1 #232: prefill the explicit default model (not a placeholder) so an
  // untouched form still submits a concrete value — store = Profile = runtime
  // stay consistent instead of persisting an empty model.
  const [model, setModel] = useState(DEFAULT_AGENT_MODEL);
  // v2.7 #181 / FINDING-F: only claude-code is executable. cli is a single-
  // option select (no free text) so the form can't create an agent bound to a
  // CLI the runtime won't run; codex/opencode open up in v2.8 (#180).
  const [cli, setCli] = useState('claude-code');
  const [workerId, setWorkerId] = useState('');
  // T728 (issue-0619f315): inject the description into the agent's system prompt.
  // Default ON — matches the backend default (nil → true).
  const [includeDescription, setIncludeDescription] = useState(true);
  const create = useAddAgentMember();
  const fleet = useFleet();
  const workers = fleet.data?.workers ?? [];

  const trimmedName = name.trim();
  const canSubmit = trimmedName.length > 0 && workerId.length > 0 && !create.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      await create.mutateAsync({
        display_name: trimmedName,
        description: description.trim() || undefined,
        role: 'member',
        model: model.trim() || undefined,
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

        <Field label={t('agents.create.modelLabel')} hint={t('agents.create.modelHint')} htmlFor="agent-create-model-input">
          {/* Editable dropdown: preset models as <datalist> suggestions while
              the field stays free text (backend accepts any model string). */}
          <input
            id="agent-create-model-input"
            data-testid="agent-create-model"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            list="agent-create-model-list"
            className={inputClass}
          />
          <datalist id="agent-create-model-list" data-testid="agent-create-model-list">
            {KNOWN_MODELS.map((m) => (
              <option key={m} value={m} />
            ))}
          </datalist>
        </Field>

        <Field label={t('agents.create.cliLabel')} hint={t('agents.create.cliHint')} htmlFor="agent-create-cli-input">
          <select
            id="agent-create-cli-input"
            data-testid="agent-create-cli"
            value={cli}
            onChange={(e) => setCli(e.target.value)}
            className={inputClass}
          >
            <option value="claude-code">claude-code</option>
          </select>
        </Field>

        <Field label={t('agents.create.workerLabel')} required hint={t('agents.create.workerHint')}>
          {/* v2.7 #191: shared searchable EntitySelect instead of a raw <select>. */}
          <EntitySelect
            testId="agent-create-worker"
            value={workerId}
            onChange={setWorkerId}
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
