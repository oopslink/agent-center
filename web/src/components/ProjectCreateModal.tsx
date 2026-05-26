// ProjectCreateModal — v2.5.5 (#59) simplified Add Project form.
//
// id is server-generated (proj-<8hex>); kind / default_agent_cli are
// gone. Tags is a free-text combobox with 6 builtin suggestions —
// operator can pick from the chip row or type a new one and Enter to
// add it. Submission only requires `name`.
import React, { useState, useRef } from 'react';
import { useCreateProject } from '@/api/projects';

interface Props {
  onClose: () => void;
}

// v2.5.5 design (msg=68d33af4) — small builtin pool, free type still
// allowed. Pool size 6 keeps the chip row scannable on a 360-wide
// modal without scrolling.
const SUGGESTED_TAGS = ['coding', 'research', 'ops', 'docs', 'experimental', 'archived'];

export function ProjectCreateModal({ onClose }: Props): React.ReactElement {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [tags, setTags] = useState<string[]>([]);
  const [tagInput, setTagInput] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);
  const create = useCreateProject();

  const trimmedName = name.trim();
  const canSubmit = trimmedName.length > 0 && !create.isPending;

  const addTag = (raw: string) => {
    const t = raw.trim();
    if (!t) return;
    if (tags.includes(t)) return;
    setTags([...tags, t]);
    setTagInput('');
  };

  const removeTag = (t: string) => {
    setTags(tags.filter((x) => x !== t));
  };

  const onTagKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      addTag(tagInput);
    } else if (e.key === 'Backspace' && tagInput === '' && tags.length > 0) {
      // Convenience: backspace into empty input pops the last chip so
      // the operator can edit/recreate it without reaching for the
      // mouse.
      removeTag(tags[tags.length - 1]);
    }
  };

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    try {
      await create.mutateAsync({
        name: trimmedName,
        description: description || undefined,
        tags: tags.length > 0 ? tags : undefined,
      });
      onClose();
    } catch {
      // surfaced via create.error below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="project-create-modal"
      role="dialog"
      aria-modal="true"
    >
      <form
        onSubmit={submit}
        className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Add Project</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="project-create-close"
          >
            X
          </button>
        </div>

        <Field label="Name" required>
          <input
            data-testid="project-create-name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My Project"
            className={inputClass}
          />
        </Field>

        <Field label="Description" hint="Optional. Shown in the project list.">
          <textarea
            data-testid="project-create-description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={3}
            className={inputClass}
          />
        </Field>

        <Field
          label="Tags"
          hint="Click a suggestion or type your own (Enter / , to add)."
        >
          <div className={tagChipsContainerClass}>
            {tags.map((t) => (
              <span key={t} className={tagChipClass} data-testid={`project-create-tag-chip-${t}`}>
                {t}
                <button
                  type="button"
                  className="ml-1 text-text-muted hover:text-text-primary"
                  onClick={() => removeTag(t)}
                  aria-label={`Remove tag ${t}`}
                >
                  x
                </button>
              </span>
            ))}
            <input
              ref={inputRef}
              data-testid="project-create-tag-input"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              onKeyDown={onTagKeyDown}
              placeholder={tags.length === 0 ? 'add a tag...' : ''}
              className="flex-1 min-w-[6rem] bg-transparent text-sm text-text-primary placeholder:text-text-muted outline-none"
            />
          </div>
          <div className="mt-2 flex flex-wrap gap-1.5" data-testid="project-create-tag-suggestions">
            {SUGGESTED_TAGS.filter((s) => !tags.includes(s)).map((s) => (
              <button
                key={s}
                type="button"
                className={suggestionPillClass}
                onClick={() => {
                  addTag(s);
                  inputRef.current?.focus();
                }}
                data-testid={`project-create-tag-suggest-${s}`}
              >
                + {s}
              </button>
            ))}
          </div>
        </Field>

        {create.isError && (
          <p className="mb-3 text-xs text-danger" data-testid="project-create-error">
            {(create.error as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="project-create-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="project-create-submit"
          >
            {create.isPending ? 'Creating...' : 'Create project'}
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

const tagChipsContainerClass =
  'flex flex-wrap items-center gap-1.5 rounded border border-border-base bg-bg-elevated px-2 py-1.5 focus-within:border-accent';

const tagChipClass =
  'inline-flex items-center rounded bg-bg-subtle px-2 py-0.5 text-xs text-text-primary';

const suggestionPillClass =
  'rounded-full border border-border-base px-2 py-0.5 text-[0.6875rem] text-text-muted hover:bg-bg-subtle hover:text-text-primary';
