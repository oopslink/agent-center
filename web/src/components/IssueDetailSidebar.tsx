import type React from 'react';
import { OrgLink } from '@/OrgContext';
import type { Issue } from '@/api/types';
import { StatusBlock } from '@/components/IssueTaskSidebar';
import { idHandle } from '@/components/workItemDisplay';
import { tagColorFor } from '@/components/tagColors';
import { formatLocalTime, formatStatusDuration } from '@/utils/time';

// IssueDetailSidebar — the @oopslink mockup's TWO-SECTION Issue detail sidebar,
// a visual mirror of TaskDetailSidebar (symmetric: TaskDetail→TaskDetailSidebar,
// IssueDetail→IssueDetailSidebar). It is built from the SAME shared primitives
// (StatusBlock, EntityRef, idHandle, tagColorFor, formatStatusDuration/
// formatLocalTime) so the two sidebars stay pixel-aligned with zero risk to the
// Task side. Issues have NO assignee, so that section is omitted; otherwise the
// labels / order / dividers / spacing / typography are identical to Task.
//
// v2.8.1 sidebar-align: editing is modal-only — the SOLE edit entry is the
// Edit-Issue pencil button (→ IssueEditModal, the single atomic PATCH of
// title/description/status/tags). The top section is a READ-ONLY display of
// status (+ in-status duration) and tags (hashed chips); no inline status menu,
// no "+ Add" tag input.
//
//   TOP (display)    : Edit-Issue button · STATUS (+ in-status duration) ·
//                      TAGS (hashed chips)        ← NO assignee (Issues have none)
//   BOTTOM (read-only): PROJECT · ISSUE ID (handle pill) · CREATED (local time)

// PencilIcon — Edit affordance (SVG, NOT emoji, per the no-emoji-icon rule).
// Identical glyph/size to TaskDetailSidebar's pencil for visual parity.
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
  issue: Issue;
  projectName?: string;
  /** opens the existing IssueEditModal — the single edit path. */
  onEdit: () => void;
  /** whether the issue is editable (non-terminal). Terminal → no Edit button. */
  editable: boolean;
}

export function IssueDetailSidebar({
  issue,
  projectName,
  onEdit,
  editable,
}: Props): React.ReactElement {
  const iss = issue;
  const tags = iss.tags ?? [];
  const duration = formatStatusDuration(iss.status_changed_at);

  return (
    <aside
      className="space-y-4 rounded border border-border-base bg-bg-elevated p-3 text-sm"
      data-testid="issue-detail-sidebar"
      aria-label="Issue details"
    >
      {/* ───────── TOP: display (edit only via the modal) ───────── */}
      <section className="space-y-3" data-testid="issue-sidebar-editable">
        <div className="flex items-start justify-between gap-2">
          <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">Details</p>
          {editable && (
            <button
              type="button"
              onClick={onEdit}
              className="inline-flex items-center gap-1 rounded bg-bg-subtle px-2 py-1 text-xs font-medium text-text-primary hover:bg-border-base"
              data-testid="issue-edit-button"
              aria-label="Edit issue"
            >
              <PencilIcon />
              Edit Issue
            </button>
          )}
        </div>

        {/* STATUS + in-status duration — display only */}
        <div data-testid="issue-sidebar-status">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Status</p>
          <div className="flex flex-wrap items-center gap-2">
            <StatusBlock status={iss.status} />
            {duration && (
              <span
                className="text-xs text-text-muted"
                data-testid="issue-status-duration"
                aria-label={`In current status for ${duration}`}
                title={`In current status for ${duration}`}
              >
                {duration}
              </span>
            )}
          </div>
        </div>

        {/* TAGS — hashed (both-mode-AA) chips, display only */}
        <div data-testid="issue-sidebar-tags">
          <p className="mb-1 text-xs uppercase tracking-wide text-text-muted">Tags</p>
          <div className="flex flex-wrap items-center gap-1.5">
            {tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="issue-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                </span>
              );
            })}
            {tags.length === 0 && (
              <span className="text-xs text-text-muted" data-testid="issue-tags-empty">
                No tags
              </span>
            )}
          </div>
        </div>
      </section>

      <hr className="border-border-base" />

      {/* ───────── BOTTOM: read-only ───────── */}
      <section className="space-y-3" data-testid="issue-sidebar-readonly">
        {iss.project_id && (
          <div>
            <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">Project</p>
            <OrgLink
              to={`/projects/${encodeURIComponent(iss.project_id)}`}
              className="text-accent hover:underline"
              data-testid="issue-project-link"
              title={iss.project_id}
            >
              {projectName || iss.project_id}
            </OrgLink>
          </div>
        )}

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">Issue ID</p>
          {/* #192 chrome rule: id-as-content → a clean handle pill (tail), full id on hover. */}
          <span
            className="inline-block rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary"
            data-testid="issue-id-pill"
            title={iss.id}
          >
            {iss.org_ref || idHandle(iss.id)}
          </span>
        </div>

        <div>
          <p className="mb-0.5 text-xs uppercase tracking-wide text-text-muted">Created</p>
          <span className="text-text-secondary" data-testid="issue-created">
            {formatLocalTime(iss.created_at)}
          </span>
        </div>
      </section>
    </aside>
  );
}
