// AgentCreateModal — Agent BC (v2.7 #101) "Add Agent" form. Posts to
// POST /api/agents. The Worker picker is sourced from the Fleet snapshot
// (useFleet().workers) and submits the chosen worker_id. name + worker_id
// are required; description/model/cli/skills are optional.
import React, { useState } from 'react';
import { useCreateAgent } from '@/api/agents';
import { useFleet } from '@/api/fleet';

interface Props {
  onClose: () => void;
}

export function AgentCreateModal({ onClose }: Props): React.ReactElement {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [model, setModel] = useState('');
  // v2.7 #181 / FINDING-F: only claude-code is executable. cli is a single-
  // option select (no free text) so the form can't create an agent bound to a
  // CLI the runtime won't run; codex/opencode open up in v2.8 (#180).
  const [cli, setCli] = useState('claude-code');
  const [skills, setSkills] = useState('');
  const [workerId, setWorkerId] = useState('');
  const create = useCreateAgent();
  const fleet = useFleet();
  const workers = fleet.data?.workers ?? [];

  const trimmedName = name.trim();
  const canSubmit = trimmedName.length > 0 && workerId.length > 0 && !create.isPending;

  const parseSkills = (raw: string): string[] =>
    raw
      .split(',')
      .map((s) => s.trim())
      .filter((s) => s.length > 0);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    const parsedSkills = parseSkills(skills);
    try {
      await create.mutateAsync({
        name: trimmedName,
        description: description.trim() || undefined,
        model: model.trim() || undefined,
        cli,
        skills: parsedSkills.length > 0 ? parsedSkills : undefined,
        worker_id: workerId,
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
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Add Agent</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="agent-create-close"
          >
            X
          </button>
        </div>

        <Field label="Name" required>
          <input
            data-testid="agent-create-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="my-agent"
            className={inputClass}
          />
        </Field>

        <Field label="Description" hint="Optional. Shown in the agent list.">
          <textarea
            data-testid="agent-create-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={2}
            className={inputClass}
          />
        </Field>

        <Field label="Model" hint="Optional.">
          <input
            data-testid="agent-create-model"
            value={model}
            onChange={(e) => setModel(e.target.value)}
            placeholder="claude-opus"
            className={inputClass}
          />
        </Field>

        <Field label="CLI" hint="v2.7 runs claude-code only (codex/opencode coming in v2.8).">
          <select
            data-testid="agent-create-cli"
            value={cli}
            onChange={(e) => setCli(e.target.value)}
            className={inputClass}
          >
            <option value="claude-code">claude-code</option>
          </select>
        </Field>

        <Field label="Skills" hint="Optional. Comma-separated.">
          <input
            data-testid="agent-create-skills"
            value={skills}
            onChange={(e) => setSkills(e.target.value)}
            placeholder="review, planning"
            className={inputClass}
          />
        </Field>

        <Field label="Worker" required hint="Sourced from the Fleet.">
          <select
            data-testid="agent-create-worker"
            value={workerId}
            onChange={(e) => setWorkerId(e.target.value)}
            className={inputClass}
          >
            <option value="">Select a worker…</option>
            {workers.map((w) => (
              <option key={w.worker_id} value={w.worker_id}>
                {w.name || w.worker_id} ({w.status})
              </option>
            ))}
          </select>
          {fleet.isSuccess && workers.length === 0 && (
            <p className="mt-1 text-[0.6875rem] text-text-muted" data-testid="agent-create-no-workers">
              No workers in the fleet yet — enroll a worker first.
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
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="agent-create-submit"
          >
            {create.isPending ? 'Creating...' : 'Create agent'}
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
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <div className="mb-3">
      <label className="mb-1 block text-xs font-medium text-text-primary">
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
