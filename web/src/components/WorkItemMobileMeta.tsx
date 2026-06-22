import React, { useEffect, useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { Avatar } from '@/components/Avatar';
import { EntityRef } from '@/components/EntityRef';
import { StatusBlock, type StatusKey } from '@/components/IssueTaskSidebar';
import { useSenderSidebar } from '@/components/SenderSidebarContext';
import { refLabel } from '@/components/workItemDisplay';
import { tagColorFor } from '@/components/tagColors';
import { formatLocalTime, formatStatusDuration } from '@/utils/time';

// WorkItemMobileMeta (T145) — mobile-only (<md) meta surfaces for the Task / Issue
// detail pages. The desktop layout keeps the right-hand TaskDetailSidebar /
// IssueDetailSidebar untouched; on a phone that sidebar was stacking at the very
// BOTTOM (you had to scroll past the whole description + attachments to see
// status / assignee / plan). These two pieces fix that:
//
//   • MobileMetaSummary — a compact "status · assignee · plan" bar shown ABOVE the
//     description so the key metadata is on the first screen.
//   • MobileDetailsPanel — a collapsible <details> with the remaining fields as
//     single-line label/value rows (≥44px touch) + the Edit button.
//
// Both are md:hidden (the ≥md sidebar covers desktop). They use DISTINCT testids
// (wi-mobile-*) so they never collide with the desktop sidebar's testids — both
// trees coexist in the DOM (one hidden by CSS at each breakpoint).

const MOBILE_LABEL = 'shrink-0 text-xs uppercase tracking-wide text-text-muted';
// ≥44px touch target (mobile UX standard) for tappable rows / buttons.
const TOUCH_ROW = 'min-h-[2.75rem]';

// useIsMobile — true below the md breakpoint (<768px), matching the shell's mobile
// seam. JS-gated (not just `md:hidden`) so the mobile meta does NOT mount on
// desktop AT ALL — that keeps the shared StatusBlock / EntityRef / Avatar (which
// carry fixed test-ids) from rendering TWICE (mobile + desktop sidebar) in the
// DOM, which would otherwise break getByTestId in the existing detail-page tests.
// matchMedia-guarded for jsdom (absent → false → mobile tree never mounts in the
// default test env; the mobile tests stub matchMedia to opt in).
const MOBILE_QUERY = '(max-width: 767px)';
function readIsMobile(): boolean {
  try {
    return (
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia(MOBILE_QUERY).matches
    );
  } catch {
    return false;
  }
}
export function useIsMobile(): boolean {
  const [isMobile, setIsMobile] = useState(readIsMobile);
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return;
    const mq = window.matchMedia(MOBILE_QUERY);
    const update = (): void => setIsMobile(mq.matches);
    update();
    mq.addEventListener?.('change', update);
    return () => mq.removeEventListener?.('change', update);
  }, []);
  return isMobile;
}

// AssigneeRef — the avatar + display name for a work item's assignee. When a
// SenderSidebarProvider is present (Task page) the name is a button that opens
// the agent activity sidebar; otherwise it renders inert (Issue has no assignee
// so this is Task-only in practice).
function AssigneeRef({
  assignee,
  assigneeName,
}: {
  assignee: string;
  assigneeName?: string;
}): React.ReactElement {
  const openSender = useSenderSidebar();
  const seed = assigneeName && assigneeName.trim() ? assigneeName : assignee;
  const name = assigneeName && assigneeName !== assignee ? assigneeName : undefined;
  const inner = (
    <>
      <Avatar name={seed} size="sm" />
      <EntityRef id={assignee} name={name} testId="wi-mobile-assignee" />
    </>
  );
  return openSender ? (
    <button
      type="button"
      onClick={() => openSender(assignee)}
      className="inline-flex items-center gap-1.5 rounded hover:bg-bg-subtle focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      data-testid="wi-mobile-assignee-open"
      title={`Open ${name ?? assignee}'s activity`}
    >
      {inner}
    </button>
  ) : (
    <span className="inline-flex items-center gap-1.5">{inner}</span>
  );
}

// MobileWorkItemBar (T309) — the @oopslink mobile mockup: a SINGLE compact bar
// under the title — status + assignee inline, with [Show info] (toggles the
// description/attachments/details panel) + [Edit] — so the CHAT below it fills
// the rest of the screen (the core surface). md:hidden (the desktop sidebar +
// inline description cover ≥md). showInfo is owned by the page so it can render
// the info panel (description/attachments/details) right below this bar.
export function MobileWorkItemBar({
  status,
  statusChangedAt,
  assignee,
  assigneeName,
  showInfo,
  onToggleInfo,
  editable,
  onEdit,
  kind,
}: {
  status: StatusKey;
  statusChangedAt?: string;
  assignee?: string | null;
  assigneeName?: string;
  showInfo: boolean;
  onToggleInfo: () => void;
  editable: boolean;
  onEdit: () => void;
  kind: 'task' | 'issue';
}): React.ReactElement | null {
  const isMobile = useIsMobile();
  const duration = formatStatusDuration(statusChangedAt);
  if (!isMobile) return null;
  const btn =
    'rounded border border-border-base px-2 py-1 text-xs font-medium text-text-secondary hover:bg-bg-subtle';
  return (
    <div
      className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2 rounded-lg border border-border-base bg-bg-elevated px-3 py-2"
      data-testid="wi-mobile-bar"
    >
      <span className="inline-flex items-center gap-2" data-testid="wi-mobile-status">
        <StatusBlock status={status} />
        {duration && <span className="text-xs text-text-muted">{duration}</span>}
      </span>
      {assignee !== undefined && (
        <span className="inline-flex items-center gap-1.5">
          {assignee ? (
            <AssigneeRef assignee={assignee} assigneeName={assigneeName} />
          ) : (
            <span className="text-xs text-text-muted" data-testid="wi-mobile-assignee-empty">
              Unassigned
            </span>
          )}
        </span>
      )}
      <span className="ml-auto flex items-center gap-1.5">
        <button
          type="button"
          onClick={onToggleInfo}
          className={btn}
          data-testid="wi-mobile-showinfo"
          aria-expanded={showInfo}
        >
          {showInfo ? 'Hide info' : 'Show info'}
        </button>
        {editable && (
          <button type="button" onClick={onEdit} className={btn} data-testid="wi-mobile-edit-button">
            {kind === 'task' ? 'Edit Task' : 'Edit Issue'}
          </button>
        )}
      </span>
    </div>
  );
}

// MobileDetailsContent (T309) — the compact metadata rows (Project / ID / Created
// / Tags) WITHOUT the <details> chrome, so the page can drop them into the
// MobileWorkItemBar's "Show info" panel alongside the description + attachments.
export function MobileDetailsContent({
  kind,
  projectId,
  projectName,
  itemId,
  orgRef,
  createdAt,
  tags,
}: {
  kind: 'task' | 'issue';
  projectId: string;
  projectName?: string;
  itemId: string;
  orgRef?: string;
  createdAt: string;
  tags: string[];
}): React.ReactElement {
  return (
    <div className="space-y-1" data-testid="wi-mobile-details-content">
      {projectId && (
        <Row label="Project">
          <OrgLink
            to={`/projects/${encodeURIComponent(projectId)}`}
            className="text-accent hover:underline"
            data-testid="wi-mobile-project-link"
          >
            {projectName || projectId}
          </OrgLink>
        </Row>
      )}
      <Row label={kind === 'task' ? 'Task ID' : 'Issue ID'}>
        <span
          className="inline-block rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary"
          data-testid="wi-mobile-id-pill"
          title={itemId}
        >
          {refLabel(orgRef, itemId)}
        </span>
      </Row>
      <Row label="Created">{formatLocalTime(createdAt)}</Row>
      <div className={`flex items-center justify-between gap-3 ${TOUCH_ROW}`}>
        <span className={MOBILE_LABEL}>Tags</span>
        <span className="flex flex-wrap items-center justify-end gap-1.5">
          {tags.length > 0 ? (
            tags.map((tag) => {
              const c = tagColorFor(tag);
              return (
                <span
                  key={tag}
                  data-testid="wi-mobile-tag-chip"
                  data-tag={tag}
                  className={`inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ${c.bg} ${c.text}`}
                >
                  {tag}
                </span>
              );
            })
          ) : (
            <span className="text-xs text-text-muted">No tags</span>
          )}
        </span>
      </div>
    </div>
  );
}

// Row — a single-line label/value pair (label muted, value same line), ≥44px.
function Row({
  label,
  children,
  testId,
}: {
  label: string;
  children: React.ReactNode;
  testId?: string;
}): React.ReactElement {
  return (
    <div className={`flex items-center justify-between gap-3 ${TOUCH_ROW}`} data-testid={testId}>
      <span className={MOBILE_LABEL}>{label}</span>
      <span className="min-w-0 truncate text-right text-text-secondary">{children}</span>
    </div>
  );
}
