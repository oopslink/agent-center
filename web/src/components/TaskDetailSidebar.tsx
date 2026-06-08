import type React from 'react';
import { useState } from 'react';
import { OrgLink } from '@/OrgContext';
import type { Task } from '@/api/types';
import { Avatar } from '@/components/Avatar';
import { EntityRef } from '@/components/EntityRef';
import { StatusBlock } from '@/components/IssueTaskSidebar';
import { idHandle } from '@/components/workItemDisplay';
import { tagColorFor } from '@/components/tagColors';
import { validateNewTag } from '@/components/tagValidation';
import { formatLocalTime, formatStatusDuration } from '@/utils/time';

// TaskDetailSidebar — the @oopslink mockup's TWO-SECTION Task detail sidebar.
// It is TaskDetail-specific (the editable/read-only split + the live status
// duration + tag chips are not shared with IssueDetail), so it is built from
// the shared primitives (StatusBlock, Avatar, EntityRef, idHandle) rather than
// bending the shared IssueTaskSidebar — IssueDetail keeps using IssueTaskSidebar
// untouched.
//
//   TOP (editable)   : Edit-Task button · STATUS (+ in-status duration) ·
//                      ASSIGNEE (avatar + name) · TAGS (hashed chips + "+ Add")
//   BOTTOM (read-only): PROJECT · TASK ID (handle pill) · CREATED (local time)

// PencilIcon — Edit affordance (SVG, NOT emoji, per the no-emoji-icon rule).
function PencilIcon(): React.ReactElement {
  return (
    <svg
      viewBox="0 0 16 16"
      className="h-3.5 w-3.5"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M11.5 2.5l2 2L6 12l-2.5.5.5-2.5z" />
    </svg>
  );
}

// PlusIcon — "+ Add" affordance (SVG, NOT emoji).
function PlusIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 12 12" className="h-3 w-3" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" aria-hidden="true">
      <path d="M6 1.5v9M1.5 6h9" />
    </svg>
  );
}

interface Props {
  task: Task;
  projectName?: string;
  /** resolved assignee display name ("" / ref-echo when unresolved). */
  assigneeName?: string;
  /** the status-transition control + (legacy) actions, kept accessible. */
  statusControl?: React.ReactNode;
  /** opens the existing TaskEditModal. */
  onEdit: () => void;
  /** whether the task is editable (non-terminal). Terminal → no Edit / no +Add. */
  editable: boolean;
  /** inline add: PATCH the full replacement tag set via useUpdateTask({tags}). */
  onAddTag: (next: string[]) => void;
  addPending?: boolean;
  /** assignee edit affordances (Change / Unassign) — kept from the prior design. */
  assigneeControls?: React.ReactNode;
}

export function TaskDetailSidebar({
  task,
  projectName,
  assigneeName,
  statusControl,
  onEdit,
  editable,
  onAddTag,
  addPending,
  assigneeControls,
}: Props): React.ReactElement {
  const tk = task;
  const tags = tk.tags ?? [];
  const duration = formatStatusDuration(tk.status_changed_at);

  const [adding, setAdding] = useState(false);
  const [draft, setDraft] = useState('');
  const [error, setError] = useState<string | null>(null);

  const commit = () => {
    const candidate = draft.trim();
    if (candidate === '') {
      setDraft('');
      setAdding(false);
      return;
    }
    const err = validateNewTag(candidate, tags);
    if (err) {
      setError(err);
      return;
    }
    if (tags.includes(candidate)) {
      // dedup — no-op PATCH avoided; just close.
      setDraft('');
      setError(null);
      setAdding(false);
      return;
    }
    onAddTag([...tags, candidate]);
    setDraft('');
    setError(null);
    setAdding(false);
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      commit();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      setDraft('');
      setError(null);
      setAdding(false);
    }
  };

  return (
    <aside
      className="space-y-4 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="task-detail-sidebar"
      aria-label="Task details"
    >
      {/* ───────── TOP: editable ───────── */}
      <section className="space-y-3" data-testid="task-sidebar-editable">
        <div className="flex items-start justify-between gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">Details</p>
          {editable && (
            <button
              type="button"
              onClick={onEdit}
              className="inline-flex items-center gap-1 rounded bg-bg-subtle px-2 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="task-edit-button"
              aria-label="Edit task"
            >
              <PencilIcon />
              Edit Task
            </button>
          )}
        </div>

        {/* STATUS + in-status duration */}
        <div data-testid="task-sidebar-status">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Status</p>
          <div className="flex flex-wrap items-center gap-2">
            <StatusBlock status={tk.status} />
            {duration && (
              <span
                className="text-xs text-text-muted"
                data-testid="task-status-duration"
                aria-label={`In current status for ${duration}`}
                title={`In current status for ${duration}`}
              >
                {duration}
              </span>
            )}
          </div>
          {/* status-transition control (Change status menu) stays accessible. */}
          {statusControl && <div className="mt-2 flex flex-wrap gap-2">{statusControl}</div>}
        </div>

        {/* ASSIGNEE — avatar + name */}
        <div data-testid="task-sidebar-assignee">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Assignee</p>
          <div className="flex flex-wrap items-center gap-2">
            {tk.assignee ? (
              <span className="inline-flex items-center gap-2">
                <Avatar name={assigneeName && assigneeName.trim() ? assigneeName : tk.assignee} size="sm" />
                <EntityRef
                  id={tk.assignee}
                  name={assigneeName && assigneeName !== tk.assignee ? assigneeName : undefined}
                  testId="task-assignee"
                />
              </span>
            ) : (
              <span className="text-text-muted" data-testid="task-assignee-empty">
                Unassigned
              </span>
            )}
            {assigneeControls}
          </div>
        </div>

        {/* TAGS — hashed (both-mode-AA) chips + inline "+ Add" */}
        <div data-testid="task-sidebar-tags">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Tags</p>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="task-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                </span>
              );
            })}
            {tags.length === 0 && !adding && (
              <span className="text-xs text-text-muted" data-testid="task-tags-empty">
                No tags
              </span>
            )}
            {editable && !adding && (
              <button
                type="button"
                onClick={() => {
                  setAdding(true);
                  setError(null);
                }}
                className="inline-flex items-center gap-0.5 rounded border border-dashed border-border-base px-2 py-0.5 text-xs text-text-secondary hover:bg-bg-subtle"
                data-testid="task-tag-add"
                aria-label="Add tag"
              >
                <PlusIcon />
                Add
              </button>
            )}
            {editable && adding && (
              <input
                autoFocus
                value={draft}
                onChange={(e) => {
                  setDraft(e.target.value);
                  if (error) setError(null);
                }}
                onKeyDown={onKeyDown}
                onBlur={commit}
                disabled={addPending}
                className="w-28 rounded border border-border-base bg-bg-elevated px-2 py-0.5 text-xs text-text-primary placeholder:text-text-muted focus:border-accent disabled:opacity-50"
                placeholder="tag, Enter…"
                data-testid="task-tag-input"
                aria-label="New tag"
                aria-describedby="task-tag-add-hint"
              />
            )}
          </div>
          {adding && (
            <p id="task-tag-add-hint" className="mt-1 text-[0.6875rem] text-text-muted">
              Enter or comma to add · max 10 tags, ≤16 chars.
            </p>
          )}
          {error && (
            <p className="mt-1 text-xs text-danger" data-testid="task-tag-error">
              {error}
            </p>
          )}
        </div>
      </section>

      <hr className="border-border-base" />

      {/* ───────── BOTTOM: read-only ───────── */}
      <section className="space-y-3" data-testid="task-sidebar-readonly">
        {tk.project_id && (
          <div>
            <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">Project</p>
            <OrgLink
              to={`/projects/${encodeURIComponent(tk.project_id)}`}
              className="text-accent hover:underline"
              data-testid="task-project-link"
            >
              {projectName || tk.project_id}
            </OrgLink>
          </div>
        )}

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">Task ID</p>
          {/* #192 chrome rule: id-as-content → a clean handle pill (tail), full id on hover. */}
          <span
            className="inline-block rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary"
            data-testid="task-id-pill"
            title={tk.id}
          >
            {tk.org_ref || idHandle(tk.id)}
          </span>
        </div>

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">Created</p>
          <span className="text-text-secondary" data-testid="task-created">
            {formatLocalTime(tk.created_at)}
          </span>
        </div>
      </section>
    </aside>
  );
}
