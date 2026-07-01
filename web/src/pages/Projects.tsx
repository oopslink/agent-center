import React, { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { OrgLink } from '@/OrgContext';

import {
  useProjects,
  useArchivedProjects,
  type Project,
} from '@/api/projects';
import { EmptyState } from '@/components/EmptyState';
import { EntityRef } from '@/components/EntityRef';
import { Skeleton } from '@/components/Skeleton';
import { ProjectCreateModal } from '@/components/ProjectCreateModal';
import { ProjectEditModal } from '@/components/ProjectEditModal';

// Projects page (/projects). Lists every project; the v2.5.3 (#58)
// "+ Add Project" button + modal lets operators create projects from
// the Web Console (previously CLI-only per ADR-0037 W1.4, retired in
// v2.5.x trajectory). Mirrors the structure of pages/Agents.tsx.
export default function Projects(): React.ReactElement {
  const { t } = useTranslation('work');
  const projects = useProjects();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <section className="space-y-4" data-testid="page-Projects">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">{t('project.list.title')}</h1>
          <p className="text-xs text-text-muted">
            {t('project.list.subtitle')}
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="projects-add-btn"
        >
          {t('project.list.addProject')}
        </button>
      </header>

      {createOpen && <ProjectCreateModal onClose={() => setCreateOpen(false)} />}

      {projects.isLoading && (
        <div className="space-y-2" data-testid="projects-loading">
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
        </div>
      )}
      {projects.isError && (
        <p className="text-sm text-danger" data-testid="projects-error">
          {(projects.error as Error).message}
        </p>
      )}
      {projects.isSuccess && projects.data.length === 0 && (
        <EmptyState
          testId="projects-empty"
          title={t('project.list.empty.title')}
          body={t('project.list.empty.body')}
        />
      )}
      {projects.isSuccess && projects.data.length > 0 && (
        <ul
          className="divide-y divide-border-base rounded-lg border border-border-base bg-bg-elevated shadow-1"
          data-testid="projects-list"
        >
          {projects.data.map((p) => (
            <ProjectRow key={p.id} project={p} />
          ))}
        </ul>
      )}

      {/* v2.9 #298: collapsed Archived group. The backend default-
          EXCLUDES archived from the active list above; this group fetches the
          archived-only list LAZILY (only once expanded) and lists read-only
          rows. Collapsed by default. */}
      <ArchivedProjectsGroup />
    </section>
  );
}

// ArchivedProjectsGroup — the collapsed Archived disclosure. Fetches the
// archived-only project list (useArchivedProjects) ONLY when expanded so the
// active page load stays a single request. Renders read-only rows (an
// "Archived" badge already shows via ProjectStatusBadge); empty → a quiet note.
function ArchivedProjectsGroup(): React.ReactElement {
  const { t } = useTranslation('work');
  const [open, setOpen] = useState(false);
  // Defer the archived fetch until the group is first opened.
  const archived = useArchivedProjects(open);

  return (
    <section className="space-y-2" data-testid="archived-projects-group">
      <button
        type="button"
        className="flex w-full items-center gap-2 rounded px-1 py-1.5 text-left text-sm font-medium text-text-secondary motion-safe:transition-colors hover:text-text-primary"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        data-testid="archived-projects-toggle"
      >
        <svg
          viewBox="0 0 24 24"
          className={[
            'h-3.5 w-3.5 motion-safe:transition-transform',
            open ? 'rotate-90' : '',
          ].join(' ')}
          fill="none"
          stroke="currentColor"
          strokeWidth="2.4"
          aria-hidden="true"
        >
          <path d="M9 6l6 6-6 6" />
        </svg>
        <span>{t('shared.archived')}</span>
      </button>

      {open && (
        <div data-testid="archived-projects-body">
          {archived.isLoading && (
            <div className="space-y-2" data-testid="archived-projects-loading">
              <Skeleton height="3rem" />
              <Skeleton height="3rem" />
            </div>
          )}
          {archived.isError && (
            <p
              className="text-sm text-danger"
              data-testid="archived-projects-error"
            >
              {(archived.error as Error).message}
            </p>
          )}
          {archived.isSuccess && archived.data.length === 0 && (
            <p
              className="px-1 text-xs italic text-text-muted"
              data-testid="archived-projects-empty"
            >
              {t('project.list.archived.empty')}
            </p>
          )}
          {archived.isSuccess && archived.data.length > 0 && (
            <ul
              className="divide-y divide-border-base rounded-lg border border-border-base bg-bg-elevated shadow-1"
              data-testid="archived-projects-list"
            >
              {archived.data.map((p) => (
                <ProjectRow key={p.id} project={p} />
              ))}
            </ul>
          )}
        </div>
      )}
    </section>
  );
}

// ProjectRow — one project list row. Shared by the active list + the archived
// group so both render identically (name + status badge + description + age).
// v2.10.2 [T139]: the card now carries quick-action shortcuts on the right
// (Edit / Work Board / Tasks / Issues / Codebase) so an operator can jump
// straight into a project's sub-views without first opening the detail page.
// The card body stays a single link to the project detail; the actions are a
// SIBLING block (not nested in the link — that would be invalid anchor markup).
function ProjectRow({ project: p }: { project: Project }): React.ReactElement {
  const { t } = useTranslation('work');
  const [editing, setEditing] = useState(false);
  return (
    <li data-testid="project-row" data-project-id={p.id}>
      <div className="flex items-start justify-between gap-3 px-4 py-3 motion-safe:transition-colors hover:bg-bg-subtle">
        <OrgLink
          to={`/projects/${encodeURIComponent(p.id)}`}
          className="flex min-w-0 flex-1 flex-col gap-1"
        >
          <div className="flex flex-wrap items-center gap-2">
            {/* v2.7 #192: project name, raw id on hover (no visible id badge). */}
            <EntityRef
              id={p.id}
              name={p.name}
              fallback={p.id}
              testId="project-name"
              className="font-medium text-text-primary"
            />
            <ProjectStatusBadge status={p.status} />
          </div>
          {/* v2.10.0 #T81 (§3.4.1): per-project count meta — the mockup's
              "12 tasks · 3 issues · 4 plans · 2 repos" line. Counts come from the
              /projects LIST response; each chip renders only when its count is
              present (the single-project GET omits them). */}
          <ProjectCounts project={p} />
          <div className="flex items-center justify-between gap-3">
            <span className="max-w-[60ch] truncate text-xs text-text-secondary">
              {p.description || <span className="italic text-text-muted">{t('project.list.noDescription')}</span>}
            </span>
            <span className="text-xs tabular-nums text-text-muted">
              {formatRelative(p.created_at, t)}
            </span>
          </div>
        </OrgLink>
        <ProjectCardActions project={p} onEdit={() => setEditing(true)} />
      </div>
      {editing && <ProjectEditModal project={p} onClose={() => setEditing(false)} />}
    </li>
  );
}

// ProjectShortcut — one quick-action descriptor. `to` (a link) and `onSelect`
// (a button, e.g. the Edit modal) are mutually exclusive.
interface ProjectShortcut {
  key: string;
  label: string;
  icon: React.ReactElement;
  to?: string;
  onSelect?: () => void;
}

// projectShortcuts — the T139 quick actions for a project card. Tasks / Issues /
// Codebase deep-link into the ProjectDetail tabs (?tab=…, the URL-param tab
// scheme owned by ProjectDetail); Work Board is its own route; Edit opens the
// shared edit modal at the row level.
function projectShortcuts(
  p: Project,
  onEdit: () => void,
  t: (key: string) => string,
): ProjectShortcut[] {
  const base = `/projects/${encodeURIComponent(p.id)}`;
  return [
    { key: 'edit', label: t('project.shortcut.edit'), icon: <EditIcon />, onSelect: onEdit },
    { key: 'board', label: t('project.shortcut.board'), icon: <BoardIcon />, to: `${base}/plans` },
    { key: 'tasks', label: t('project.shortcut.tasks'), icon: <TasksIcon />, to: `${base}?tab=tasks` },
    { key: 'issues', label: t('project.shortcut.issues'), icon: <IssuesIcon />, to: `${base}?tab=issues` },
    { key: 'plans', label: t('project.shortcut.plans'), icon: <PlansIcon />, to: `${base}?tab=plans` },
    { key: 'codebase', label: t('project.shortcut.codebase'), icon: <CodebaseIcon />, to: `${base}?tab=repos` },
  ];
}

// ProjectCardActions — the quick-action cluster on the right of a project card.
// Responsive (owner ask: "窄屏可收进菜单，不挤压"): on ≥md it renders an inline
// row of icon buttons (label as tooltip + aria-label); below md it collapses
// into a single "⋯" menu listing the same shortcuts as labelled rows, so a
// cramped width never squeezes the buttons. Each entry routes (OrgLink) or
// triggers the row-level action (Edit modal).
function ProjectCardActions({
  project: p,
  onEdit,
}: {
  project: Project;
  onEdit: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const shortcuts = projectShortcuts(p, onEdit, t);
  const [menuOpen, setMenuOpen] = useState(false);
  const menuRef = useRef<HTMLDivElement | null>(null);

  // Close the compact menu on an outside click / Escape (a11y + no stuck menu).
  useEffect(() => {
    if (!menuOpen) return;
    const onDown = (e: MouseEvent): void => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) setMenuOpen(false);
    };
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') setMenuOpen(false);
    };
    document.addEventListener('mousedown', onDown);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      document.removeEventListener('keydown', onKey);
    };
  }, [menuOpen]);

  return (
    <div className="shrink-0" data-testid="project-card-actions" data-project-id={p.id}>
      {/* ≥md: inline icon-button row. */}
      <div className="hidden items-center gap-0.5 md:flex">
        {shortcuts.map((s) =>
          s.to ? (
            <OrgLink
              key={s.key}
              to={s.to}
              className="inline-flex h-7 w-7 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary"
              title={s.label}
              aria-label={s.label}
              data-testid={`project-shortcut-${s.key}`}
            >
              {s.icon}
            </OrgLink>
          ) : (
            <button
              key={s.key}
              type="button"
              className="inline-flex h-7 w-7 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary"
              title={s.label}
              aria-label={s.label}
              data-testid={`project-shortcut-${s.key}`}
              onClick={s.onSelect}
            >
              {s.icon}
            </button>
          ),
        )}
      </div>

      {/* <md: collapse into a "⋯" menu so the actions never squeeze the card. */}
      <div className="relative md:hidden" ref={menuRef}>
        <button
          type="button"
          className="inline-flex min-h-[44px] min-w-[44px] items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary md:h-7 md:w-7 md:min-h-0 md:min-w-0"
          aria-haspopup="menu"
          aria-expanded={menuOpen}
          aria-label={t('project.actions.menuLabel')}
          data-testid="project-actions-menu-btn"
          onClick={() => setMenuOpen((v) => !v)}
        >
          <KebabIcon />
        </button>
        {menuOpen && (
          <div
            className="absolute right-0 top-full z-20 mt-1 w-40 rounded-md border border-border-base bg-bg-elevated p-1 shadow-1"
            role="menu"
            data-testid="project-actions-menu"
          >
            {shortcuts.map((s) =>
              s.to ? (
                <OrgLink
                  key={s.key}
                  to={s.to}
                  role="menuitem"
                  className="flex items-center gap-2 rounded px-2 py-1.5 text-xs text-text-primary hover:bg-bg-subtle min-h-[44px] md:min-h-0"
                  data-testid={`project-shortcut-menu-${s.key}`}
                  onClick={() => setMenuOpen(false)}
                >
                  {s.icon}
                  {s.label}
                </OrgLink>
              ) : (
                <button
                  key={s.key}
                  type="button"
                  role="menuitem"
                  className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs text-text-primary hover:bg-bg-subtle min-h-[44px] md:min-h-0"
                  data-testid={`project-shortcut-menu-${s.key}`}
                  onClick={() => {
                    setMenuOpen(false);
                    s.onSelect?.();
                  }}
                >
                  {s.icon}
                  {s.label}
                </button>
              ),
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ProjectCounts renders the per-project task/issue/plan/repo count meta line
// (v2.10.0 #T81, §3.4.1) shown on the Projects list cards. Each count is
// optional — present only on the LIST response — so a chip is rendered only
// when its value is a number, and the whole row is omitted when none are.
function ProjectCounts({ project: p }: { project: Project }): React.ReactElement | null {
  const { t } = useTranslation('work');
  const chips: Array<{ key: string; n: number; i18nKey: string }> = [];
  if (typeof p.task_count === 'number') chips.push({ key: 'tasks', n: p.task_count, i18nKey: 'project.counts.task' });
  if (typeof p.issue_count === 'number') chips.push({ key: 'issues', n: p.issue_count, i18nKey: 'project.counts.issue' });
  if (typeof p.plan_count === 'number') chips.push({ key: 'plans', n: p.plan_count, i18nKey: 'project.counts.plan' });
  if (typeof p.repo_count === 'number') chips.push({ key: 'repos', n: p.repo_count, i18nKey: 'project.counts.repo' });
  if (chips.length === 0) return null;
  return (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs tabular-nums text-text-muted"
      data-testid="project-counts"
    >
      {chips.map((c) => (
        <span key={c.key} data-testid={`project-count-${c.key}`}>
          {t(c.i18nKey, { count: c.n })}
        </span>
      ))}
    </div>
  );
}

// ProjectStatusBadge renders the active/archived status chip.
function ProjectStatusBadge({ status }: { status: Project['status'] }): React.ReactElement {
  const { t } = useTranslation('work');
  return (
    <span
      className={[
        'rounded px-2 py-0.5 text-[0.6875rem] uppercase tracking-wide',
        status === 'archived'
          ? 'bg-bg-subtle text-text-muted'
          : 'bg-success/10 text-success',
      ].join(' ')}
      data-testid={`project-status-${status}`}
    >
      {t(`project.status.${status}`)}
    </span>
  );
}

// --- T139 quick-action icons (inline SVG, no emoji per the a11y guardrail).
// aria-hidden — the accessible name lives on the wrapping button/link. ----------

const iconProps = {
  width: 15,
  height: 15,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
};

function EditIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4 12.5-12.5z" />
    </svg>
  );
}

function BoardIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <rect x="3" y="4" width="18" height="16" rx="1.5" />
      <path d="M9 4v16M15 4v16" />
    </svg>
  );
}

function TasksIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <path d="M9 6h11M9 12h11M9 18h11" />
      <path d="M4 6l1 1 1.5-1.5M4 12l1 1 1.5-1.5M4 18l1 1 1.5-1.5" />
    </svg>
  );
}

function IssuesIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 8v4M12 16h.01" />
    </svg>
  );
}

function CodebaseIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <path d="M8 9l-3 3 3 3M16 9l3 3-3 3M13.5 6l-3 12" />
    </svg>
  );
}

function PlansIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <path d="M12 3l9 5-9 5-9-5 9-5z" />
      <path d="M3 12l9 5 9-5M3 16l9 5 9-5" />
    </svg>
  );
}

function KebabIcon(): React.ReactElement {
  return (
    <svg {...iconProps}>
      <circle cx="12" cy="5" r="1" />
      <circle cx="12" cy="12" r="1" />
      <circle cx="12" cy="19" r="1" />
    </svg>
  );
}

// Tiny inline relative-time helper (matches Home.tsx). Avoids a new
// date-fns dep for "Xs / Xm / Xh / Xd ago" precision.
function formatRelative(iso: string, t: (key: string, opts?: Record<string, unknown>) => string): string {
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - ms) / 1000));
  if (delta < 60) return t('project.relativeTime.seconds', { count: delta });
  if (delta < 3600) return t('project.relativeTime.minutes', { count: Math.floor(delta / 60) });
  if (delta < 86400) return t('project.relativeTime.hours', { count: Math.floor(delta / 3600) });
  return t('project.relativeTime.days', { count: Math.floor(delta / 86400) });
}
