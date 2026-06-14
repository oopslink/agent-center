// TaskEditModal — v2.8.1 #278 full Edit-Task form: batch-saves
// title / description / status / assignee / tags in ONE atomic PATCH
// (PATCH /projects/{pid}/tasks/{id} → pmBatchUpdateTaskHandler). Only the
// changed (dirty) fields are sent; the backend applies all-or-none.
import React, { useMemo, useState } from 'react';
import { useAssignTask, useUpdateTask } from '@/api/tasks';
import { useMembers, normalizeIdentityRef } from '@/api/members';
import type { MemberResult } from '@/api/members';
import type { Task, TaskStatus } from '@/api/types';
import { useModalA11y } from './useModalA11y';
import { MAX_TAG_RUNES, MAX_TAGS, runeLength, validateTags } from './tagValidation';

interface Props {
  projectId: string;
  task: Pick<Task, 'id' | 'title' | 'description' | 'status' | 'assignee' | 'tags'>;
  onClose: () => void;
  onSaved?: () => void;
}

// All TaskStatus values — free-state model (any value selectable, no adjacency),
// mirroring the backend SetStatus. Keep in sync with the TaskStatus union.
const TASK_STATUSES: TaskStatus[] = [
  'open',
  'running',
  'completed',
  'discarded',
  'reopened',
];

// Tag validation rules (rune-16, max-10, dedup) are shared with the inline "+ Add"
// affordance in the TaskDetail sidebar — see ./tagValidation (single source).

// Build the prefixed identity ref for an assignee option, mirroring
// OrgWorkItemsView: "<kind>:<bare-id>" (kind derived from the ref when absent).
function memberRef(m: MemberResult): string {
  const kind = m.kind ?? (m.identity_id.startsWith('agent') ? 'agent' : 'user');
  return `${kind}:${normalizeIdentityRef(m.identity_id)}`;
}

export function TaskEditModal({ projectId, task, onClose, onSaved }: Props): React.ReactElement {
  const [title, setTitle] = useState(task.title ?? '');
  const [description, setDescription] = useState(task.description ?? '');
  const [status, setStatus] = useState<TaskStatus>(task.status ?? 'open');
  const [assignee, setAssignee] = useState(task.assignee ?? '');
  const [tags, setTags] = useState<string[]>(task.tags ?? []);
  const [tagDraft, setTagDraft] = useState('');
  const [tagError, setTagError] = useState<string | null>(null);

  const update = useUpdateTask(projectId, task.id);
  // E2E finding F-7: a real (re)assignment must go through the dedicated assign
  // endpoint (pmAssignTaskHandler → AssignTask), which is what DISPATCHES/wakes the
  // agent. The batch PATCH (useUpdateTask) sets the assignee but does NOT dispatch,
  // so assigning a task to an agent via this modal left it in `open` forever.
  const assign = useAssignTask(projectId, task.id);
  const members = useMembers();
  // a11y: Escape closes + focus-trap (rendered = open).
  const containerRef = useModalA11y({ open: true, onClose });

  // commitTag: add the typed draft as a chip (Enter/comma). Trims, validates the
  // single tag (rune-16), dedups (no-op if already present), enforces the 10-cap.
  const commitTag = () => {
    const candidate = tagDraft.trim();
    if (candidate === '') {
      setTagDraft('');
      return;
    }
    if (runeLength(candidate) > MAX_TAG_RUNES) {
      setTagError(`Tag too long (max ${MAX_TAG_RUNES})`);
      return;
    }
    if (tags.includes(candidate)) {
      // dedup — keep first; just clear the draft.
      setTagDraft('');
      setTagError(null);
      return;
    }
    if (tags.length >= MAX_TAGS) {
      setTagError(`Max ${MAX_TAGS} tags`);
      return;
    }
    setTags([...tags, candidate]);
    setTagDraft('');
    setTagError(null);
  };

  const removeTag = (tag: string) => {
    setTags(tags.filter((t) => t !== tag));
    setTagError(null);
  };

  const onTagKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commitTag();
    }
  };

  // The current tag set incl. an uncommitted-but-valid draft would still be
  // validated on submit; we validate the committed set here.
  const tagsValidationError = useMemo(() => validateTags(tags), [tags]);

  const trimmedTitle = title.trim();
  // Dirty diff — compare against the original task so we send ONLY changed fields.
  const origTags = task.tags ?? [];
  const tagsChanged =
    tags.length !== origTags.length || tags.some((t, i) => t !== origTags[i]);
  const titleChanged = trimmedTitle !== (task.title ?? '');
  const descChanged = description.trim() !== (task.description ?? '');
  const statusChanged = status !== (task.status ?? 'open');
  const assigneeChanged = assignee !== (task.assignee ?? '');
  const anyDirty =
    titleChanged || descChanged || statusChanged || assigneeChanged || tagsChanged;

  const hasError = !!tagError || !!tagsValidationError;
  const canSubmit =
    trimmedTitle.length > 0 && anyDirty && !hasError && !update.isPending && !assign.isPending;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSubmit) return;
    // Build the dirty-only batch body. Wire keys match Dev's #278 contract:
    // {title?, description?, status?, assignee?, tags?} — "description" (not "desc").
    const body: {
      title?: string;
      description?: string;
      status?: TaskStatus;
      assignee?: string;
      tags?: string[];
    } = {};
    if (titleChanged) body.title = trimmedTitle;
    if (descChanged) body.description = description.trim();
    if (statusChanged) body.status = status;
    // F-7: route a real (re)assignment through the dedicated assign endpoint below
    // (it dispatches the agent). Only fold the assignee into the batch PATCH when
    // CLEARING it — an unassign needs no dispatch.
    if (assigneeChanged && assignee === '') body.assignee = '';
    if (tagsChanged) body.tags = tags;
    try {
      if (Object.keys(body).length > 0) {
        await update.mutateAsync(body);
      }
      if (assigneeChanged && assignee !== '') {
        await assign.mutateAsync({ assignee });
      }
      onSaved?.();
      onClose();
    } catch {
      // Surfaced via update.error / assign.error; modal stays open.
    }
  };

  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="task-edit-modal"
      role="dialog"
      aria-modal="true"
      aria-label="Edit task"
    >
      <form
        onSubmit={submit}
        className="max-h-[90vh] w-full max-w-lg overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl"
      >
        <div className="mb-4 flex items-center justify-between">
          <h2 className="text-lg font-semibold">Edit Task</h2>
          <button
            type="button"
            className="text-text-muted hover:text-text-primary"
            onClick={onClose}
            aria-label="Close"
            data-testid="task-edit-close"
          >
            X
          </button>
        </div>

        <div className="mb-3">
          <label htmlFor="task-edit-title" className="mb-1 block text-xs font-medium text-text-primary">
            Title<span className="ml-1 text-danger">*</span>
          </label>
          <input
            id="task-edit-title"
            data-testid="task-edit-title"
            className={inputClass}
            value={title}
            onChange={(e) => setTitle(e.target.value)}
          />
        </div>

        <div className="mb-3">
          <label
            htmlFor="task-edit-description"
            className="mb-1 block text-xs font-medium text-text-primary"
          >
            Description
          </label>
          <textarea
            id="task-edit-description"
            data-testid="task-edit-description"
            className={inputClass}
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={4}
          />
        </div>

        <div className="mb-3">
          <label htmlFor="task-edit-status" className="mb-1 block text-xs font-medium text-text-primary">
            Status
          </label>
          <select
            id="task-edit-status"
            data-testid="task-edit-status"
            className={inputClass}
            value={status}
            onChange={(e) => setStatus(e.target.value as TaskStatus)}
          >
            {TASK_STATUSES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>

        <div className="mb-3">
          <label htmlFor="task-edit-assignee" className="mb-1 block text-xs font-medium text-text-primary">
            Assignee
          </label>
          <select
            id="task-edit-assignee"
            data-testid="task-edit-assignee"
            className={inputClass}
            value={assignee}
            onChange={(e) => setAssignee(e.target.value)}
          >
            <option value="">Unassigned</option>
            {(members.data ?? []).map((m) => (
              <option key={m.id} value={memberRef(m)}>
                {m.display_name ?? m.identity_id} ({m.kind})
              </option>
            ))}
          </select>
        </div>

        <div className="mb-3">
          <label htmlFor="task-edit-tags-input" className="mb-1 block text-xs font-medium text-text-primary">
            Tags
          </label>
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <span
                key={tag}
                data-testid="task-edit-tag-chip"
                className="inline-flex items-center gap-1 rounded bg-bg-subtle px-2 py-0.5 text-xs text-text-primary"
              >
                {tag}
                <button
                  type="button"
                  className="text-text-muted hover:text-text-primary"
                  onClick={() => removeTag(tag)}
                  aria-label={`Remove tag ${tag}`}
                  data-testid="task-edit-tag-remove"
                >
                  x
                </button>
              </span>
            ))}
          </div>
          <input
            id="task-edit-tags-input"
            data-testid="task-edit-tags-input"
            className={`${inputClass} mt-1.5`}
            value={tagDraft}
            onChange={(e) => {
              setTagDraft(e.target.value);
              if (tagError) setTagError(null);
            }}
            onKeyDown={onTagKeyDown}
            placeholder="Type a tag, press Enter or comma…"
            aria-describedby="task-edit-tags-hint"
          />
          <p id="task-edit-tags-hint" className="mt-1 text-[0.6875rem] text-text-muted">
            Up to {MAX_TAGS} tags, ≤{MAX_TAG_RUNES} characters each.
          </p>
          {(tagError || tagsValidationError) && (
            <p className="mt-1 text-xs text-danger" data-testid="task-edit-tag-error">
              {tagError ?? tagsValidationError}
            </p>
          )}
        </div>

        {(update.isError || assign.isError) && (
          <p className="mb-3 text-xs text-danger" data-testid="task-edit-error">
            {((update.error || assign.error) as Error).message}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="task-edit-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={!canSubmit}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:cursor-not-allowed disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="task-edit-submit"
          >
            {update.isPending || assign.isPending ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </form>
    </div>
  );
}

const inputClass =
  'block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';
