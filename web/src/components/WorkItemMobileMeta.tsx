import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';
import { StatusBlock, type StatusKey } from '@/components/IssueTaskSidebar';
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


// MobileBannerMeta — inline chips rendered INSIDE the WorkItemConversation
// banner row on mobile (status + duration + Actions dropdown). Replaces the
// old standalone MobileWorkItemBar — no separate bar, no duplicate org_ref.
export function MobileBannerMeta({
  status,
  statusChangedAt,
  showInfo,
  onToggleInfo,
  editable,
  onEdit,
  kind,
}: {
  status: StatusKey;
  statusChangedAt?: string;
  showInfo: boolean;
  onToggleInfo: () => void;
  editable: boolean;
  onEdit: () => void;
  kind: 'task' | 'issue';
}): React.ReactElement {
  const { t } = useTranslation('work');
  const duration = formatStatusDuration(statusChangedAt);
  const [actionsOpen, setActionsOpen] = useState(false);

  return (
    <>
      <span className="inline-flex items-center" data-testid="wi-mobile-status">
        <StatusBlock status={status} />
      </span>
      {duration && <span className="whitespace-nowrap text-xs text-text-muted">{duration}</span>}
      <span className="relative shrink-0">
        <button
          type="button"
          onClick={() => setActionsOpen((v) => !v)}
          className="inline-flex min-h-[2.75rem] items-center gap-1 rounded-full border border-border-base bg-bg-subtle px-3 text-xs font-medium text-text-secondary whitespace-nowrap"
          data-testid="wi-mobile-actions-toggle"
          aria-expanded={actionsOpen}
          aria-haspopup="true"
        >
          {t('workItem.mobile.actions')} <span aria-hidden="true">▾</span>
        </button>
        {actionsOpen && (
          <div className="absolute right-0 top-full z-20 mt-1 w-36 rounded-lg border border-border-base bg-bg-elevated shadow-2" data-testid="wi-mobile-actions-menu" role="menu">
            <button
              type="button"
              role="menuitem"
              onClick={() => { setActionsOpen(false); onToggleInfo(); }}
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle"
              data-testid="wi-mobile-showinfo"
              aria-expanded={showInfo}
            >
              {showInfo ? t('workItem.mobile.hideInfo') : t('workItem.mobile.showInfo')}
            </button>
            {editable && (
              <button
                type="button"
                role="menuitem"
                onClick={() => { setActionsOpen(false); onEdit(); }}
                className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle"
                data-testid="wi-mobile-edit-button"
              >
                {kind === 'task' ? t('workItem.mobile.editTask') : t('workItem.mobile.editIssue')}
              </button>
            )}
          </div>
        )}
      </span>
    </>
  );
}

// Legacy alias — kept for backward compat with tests that render it standalone.
// Returns null (the bar is now merged into the conversation banner).
export function MobileWorkItemBar(_props: {
  status: StatusKey;
  statusChangedAt?: string;
  assignee?: string | null;
  assigneeName?: string;
  showInfo: boolean;
  onToggleInfo: () => void;
  editable: boolean;
  onEdit: () => void;
  kind: 'task' | 'issue';
  orgRef?: string;
}): React.ReactElement | null {
  return null;
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
  const { t } = useTranslation('work');
  return (
    <div className="space-y-1" data-testid="wi-mobile-details-content">
      {projectId && (
        <Row label={t('workItem.mobile.project')}>
          <OrgLink
            to={`/projects/${encodeURIComponent(projectId)}`}
            className="text-accent hover:underline"
            data-testid="wi-mobile-project-link"
          >
            {projectName || projectId}
          </OrgLink>
        </Row>
      )}
      <Row label={kind === 'task' ? t('workItem.mobile.taskId') : t('workItem.mobile.issueId')}>
        <span
          className="inline-block rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-xs text-text-secondary"
          data-testid="wi-mobile-id-pill"
          title={itemId}
        >
          {refLabel(orgRef, itemId)}
        </span>
      </Row>
      <Row label={t('workItem.mobile.created')}>{formatLocalTime(createdAt)}</Row>
      <div className={`flex items-center justify-between gap-3 ${TOUCH_ROW}`}>
        <span className={MOBILE_LABEL}>{t('workItem.mobile.tags')}</span>
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
            <span className="text-xs text-text-muted">{t('workItem.mobile.noTags')}</span>
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
