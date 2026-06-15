import type React from 'react';
import { OrgLink } from '@/OrgContext';
import type { Task } from '@/api/types';
import { Avatar } from '@/components/Avatar';
import { EntityRef } from '@/components/EntityRef';
import { useSenderSidebar } from '@/components/SenderSidebarContext';
import { StatusBlock } from '@/components/IssueTaskSidebar';
import { idHandle } from '@/components/workItemDisplay';
import { tagColorFor } from '@/components/tagColors';
import { formatLocalTime, formatStatusDuration } from '@/utils/time';

// TaskDetailSidebar — the @oopslink mockup's TWO-SECTION Task detail sidebar.
// It is TaskDetail-specific (the editable/read-only split + the live status
// duration + tag chips are not shared with IssueDetail), so it is built from
// the shared primitives (StatusBlock, Avatar, EntityRef, idHandle) rather than
// bending the shared IssueTaskSidebar — IssueDetail keeps using IssueTaskSidebar
// untouched.
//
// v2.8.1 #281 readonly: per @oopslink, a task may ONLY be edited via the Edit
// Task modal. The top section is now a READ-ONLY DISPLAY of status (+ in-status
// duration), assignee (avatar + name) and tags (hashed chips) — the ONLY edit
// entry is the Edit Task button (→ TaskEditModal, the single atomic edit path).
// No inline status-change menu, no assignee Change/Unassign, no "+ Add" tag input.
//
//   TOP (display)    : Edit-Task button · STATUS (+ in-status duration) ·
//                      ASSIGNEE (avatar + name) · TAGS (hashed chips)
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

interface Props {
  task: Task;
  projectName?: string;
  /** resolved assignee display name ("" / ref-echo when unresolved). */
  assigneeName?: string;
  /** opens the existing TaskEditModal — the single edit path. */
  onEdit: () => void;
  /** whether the task is editable (non-terminal). Terminal → no Edit button. */
  editable: boolean;
}

export function TaskDetailSidebar({
  task,
  projectName,
  assigneeName,
  onEdit,
  editable,
}: Props): React.ReactElement {
  const tk = task;
  const tags = tk.tags ?? [];
  const duration = formatStatusDuration(tk.status_changed_at);
  // T102: clicking the assignee opens that identity's activity sidebar (the
  // shared SenderDetailSidebar — agent info + activity feed), reusing the same
  // openSender path as @mentions / message senders. Null-safe: when rendered
  // without a SenderSidebarProvider the name stays plain (no-op).
  const openSender = useSenderSidebar();

  return (
    <aside
      className="space-y-4 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="task-detail-sidebar"
      aria-label="Task details"
    >
      {/* ───────── TOP: display (edit only via the modal) ───────── */}
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

        {/* STATUS + in-status duration — display only */}
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
        </div>

        {/* ASSIGNEE — avatar + name, display only */}
        <div data-testid="task-sidebar-assignee">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Assignee</p>
          <div className="flex flex-wrap items-center gap-2">
            {tk.assignee ? (
              openSender ? (
                <button
                  type="button"
                  onClick={() => openSender(tk.assignee)}
                  data-testid="task-assignee-open"
                  title={`Open ${assigneeName && assigneeName !== tk.assignee ? assigneeName : tk.assignee}'s activity`}
                  className="inline-flex items-center gap-2 rounded hover:bg-bg-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  <Avatar name={assigneeName && assigneeName.trim() ? assigneeName : tk.assignee} size="sm" />
                  <EntityRef
                    id={tk.assignee}
                    name={assigneeName && assigneeName !== tk.assignee ? assigneeName : undefined}
                    testId="task-assignee"
                  />
                </button>
              ) : (
                <span className="inline-flex items-center gap-2">
                  <Avatar name={assigneeName && assigneeName.trim() ? assigneeName : tk.assignee} size="sm" />
                  <EntityRef
                    id={tk.assignee}
                    name={assigneeName && assigneeName !== tk.assignee ? assigneeName : undefined}
                    testId="task-assignee"
                  />
                </span>
              )
            ) : (
              <span className="text-text-muted" data-testid="task-assignee-empty">
                Unassigned
              </span>
            )}
          </div>
        </div>

        {/* TAGS — hashed (both-mode-AA) chips, display only */}
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
            {tags.length === 0 && (
              <span className="text-xs text-text-muted" data-testid="task-tags-empty">
                No tags
              </span>
            )}
          </div>
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
