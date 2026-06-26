// AgentConfigEditModal — T236: edit an agent's LLM config (CLI / Model /
// Reasoning / Mode / Provider). Saving persists the config and then RESTARTS the
// agent so the change takes effect, gated behind a second confirmation ("this
// will restart the agent — continue?") per the task. A stopped agent needs no
// restart (the new config applies on its next start), so the confirm wording +
// action adapt to lifecycle.
import React, { useState } from 'react';
import { useRestartAgent, useUpdateAgentConfig } from '@/api/agents';
import type { Agent } from '@/api/types';
import { useModalA11y } from './useModalA11y';
import { ConfirmModal } from './ConfirmModal';

interface Props {
  agent: Agent;
  onClose: () => void;
}

// CLI options mirror the runtime allowlist (agent.IsSupportedExecutionCLI).
const CLI_OPTIONS = ['claude-code', 'codex'];
// Reasoning effort allowlist (backend agent.SupportedReasoningEfforts); "" = the
// runtime default.
const REASONING_OPTIONS = ['', 'minimal', 'low', 'medium', 'high'];

export function AgentConfigEditModal({ agent, onClose }: Props): React.ReactElement {
  const [model, setModel] = useState(agent.model ?? '');
  const [cli, setCli] = useState(agent.cli || 'claude-code');
  const [reasoning, setReasoning] = useState(agent.reasoning ?? '');
  const [mode, setMode] = useState(agent.mode ?? '');
  const [provider, setProvider] = useState(agent.provider ?? '');
  const [confirming, setConfirming] = useState(false);

  const update = useUpdateAgentConfig(agent.id);
  const restart = useRestartAgent(agent.id);
  const containerRef = useModalA11y({ open: true, onClose });

  const isRunning = agent.lifecycle === 'running';
  const busy = update.isPending || restart.isPending;
  const error = (update.error ?? restart.error) as Error | null;

  const apply = async () => {
    try {
      await update.mutateAsync({ model: model.trim(), cli, reasoning, mode: mode.trim(), provider: provider.trim() });
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
      aria-label="Edit agent LLM config"
    >
      <div className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Edit LLM config</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="agent-config-edit-close"
          >
            <span aria-hidden="true">X</span>
          </button>
        </div>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            setConfirming(true);
          }}
        >
          <Field label="CLI" htmlFor="agent-config-cli-input">
            <select
              id="agent-config-cli-input"
              data-testid="agent-config-cli"
              className={inputClass}
              value={cli}
              onChange={(e) => setCli(e.target.value)}
            >
              {CLI_OPTIONS.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Model" htmlFor="agent-config-model-input">
            <input
              id="agent-config-model-input"
              data-testid="agent-config-model"
              className={inputClass}
              value={model}
              onChange={(e) => setModel(e.target.value)}
              placeholder="e.g. claude-opus-4-8"
            />
          </Field>

          <Field label="Reasoning" hint="Reasoning effort. Default = the runtime default." htmlFor="agent-config-reasoning-input">
            <select
              id="agent-config-reasoning-input"
              data-testid="agent-config-reasoning"
              className={inputClass}
              value={reasoning}
              onChange={(e) => setReasoning(e.target.value)}
            >
              {REASONING_OPTIONS.map((r) => (
                <option key={r || 'default'} value={r}>
                  {r || 'Default'}
                </option>
              ))}
            </select>
          </Field>

          <Field label="Mode" hint="Optional. Empty = default." htmlFor="agent-config-mode-input">
            <input
              id="agent-config-mode-input"
              data-testid="agent-config-mode"
              className={inputClass}
              value={mode}
              onChange={(e) => setMode(e.target.value)}
              placeholder="default"
            />
          </Field>

          <Field label="Provider" hint="Optional. Empty = center default." htmlFor="agent-config-provider-input">
            <input
              id="agent-config-provider-input"
              data-testid="agent-config-provider"
              className={inputClass}
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
              placeholder="default"
            />
          </Field>

          {error && (
            <p className="mb-3 text-xs text-danger" data-testid="agent-config-edit-error">
              {error.message}
            </p>
          )}

          <div className="mt-4 flex justify-end gap-2">
            <button
              type="button"
              className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              onClick={onClose}
              data-testid="agent-config-edit-cancel"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={busy}
              className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
              data-testid="agent-config-edit-save"
            >
              Save
            </button>
          </div>
        </form>
      </div>

      {/* Second confirmation: applying config restarts a running agent. */}
      <ConfirmModal
        open={confirming}
        title={isRunning ? 'Restart to apply changes?' : 'Save config changes?'}
        message={
          isRunning
            ? 'Saving these LLM config changes will RESTART the agent so they take effect. Continue?'
            : 'The agent is not running — the new config will apply the next time it starts.'
        }
        confirmLabel={isRunning ? 'Save & restart' : 'Save'}
        busy={busy}
        onConfirm={() => void apply()}
        onCancel={() => setConfirming(false)}
      />
    </div>
  );
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

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
