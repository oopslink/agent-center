import React from 'react';
import { useTranslation } from 'react-i18next';
import { useOptionalOrgContext, orgPath } from '@/OrgContext';

// TaskTitleLink (v2.9 Stage A6) — renders a task title as a link that opens the
// TaskDetail page (`/projects/{projectId}/tasks/{taskId}`) in a NEW TAB.
//
// Why a raw <a target="_blank"> (not OrgLink/Link): a new tab needs a real
// anchor with target="_blank" + rel="noopener noreferrer" (SPA Link cannot open
// a new tab). We replicate OrgLink's org-prefixing by computing the href through
// the SAME orgPath() helper OrgLink uses, reading the optional OrgContext — so
// inside an org route the href is /organizations/{slug}/projects/.../tasks/...,
// and outside one (e.g. plain test render) it is the bare app-absolute path.
// MATCHES Home.tsx's link pattern: /projects/{enc(pid)}/tasks/{enc(tid)}.
//
// Coexistence (A2 remove button + A7 drag): this link wraps ONLY the title text,
// never the whole card, so dragging the card still drags and the remove button
// still fires. Clicking the title navigates (new tab).
//
// AA (both modes): the title uses text-text-primary (readable AA in light+dark);
// the link affordance is a hover underline + hover accent color — NOT
// text-text-muted, no alpha-tint. The trailing ↗ is an inline SVG (no emoji).
export function taskDetailPath(projectId: string, taskId: string): string {
  return `/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(taskId)}`;
}

export function TaskTitleLink({
  projectId,
  taskId,
  title,
  className,
}: {
  projectId: string;
  taskId: string;
  title: React.ReactNode;
  className?: string;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const ctx = useOptionalOrgContext();
  const href = orgPath(taskDetailPath(projectId, taskId), ctx?.slug);
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      // draggable=false so grabbing the title inside an A7-draggable card drags the
      // CARD (move task), not the link (drag URL) — the drag falls through to the card.
      draggable={false}
      className={`group inline-flex max-w-full items-center gap-1 text-text-primary hover:text-accent hover:underline focus-visible:underline ${className ?? ''}`}
      data-testid={`task-open-link-${taskId}`}
      title={t('widgets.taskTitleLink.openInNewTab')}
    >
      <span className="truncate">{title}</span>
      <OpenInNewTabIcon />
    </a>
  );
}

// OpenInNewTabIcon — the "open in new tab" ↗ glyph as an inline SVG (NOT an
// emoji pictograph, per the a11y guardrail). aria-hidden — the link's title /
// surrounding text carry the accessible meaning.
function OpenInNewTabIcon(): React.ReactElement {
  return (
    <svg
      width="10"
      height="10"
      viewBox="0 0 16 16"
      fill="none"
      aria-hidden="true"
      className="shrink-0 opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100"
    >
      <path
        d="M6 3h7v7M13 3L6.5 9.5M11 9.5V13H3V5h3.5"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}
