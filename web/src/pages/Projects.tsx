import React, { useState } from 'react';
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

// Projects page (/projects). Lists every project; the v2.5.3 (#58)
// "+ Add Project" button + modal lets operators create projects from
// the Web Console (previously CLI-only per ADR-0037 W1.4, retired in
// v2.5.x trajectory). Mirrors the structure of pages/Agents.tsx.
export default function Projects(): React.ReactElement {
  const projects = useProjects();
  const [createOpen, setCreateOpen] = useState(false);

  return (
    <section className="space-y-4" data-testid="page-Projects">
      <header className="flex items-start justify-between">
        <div>
          <h1 className="font-heading text-2xl font-semibold text-text-primary">Projects</h1>
          <p className="text-xs text-text-muted">
            Projects organize Issues and Tasks.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="projects-add-btn"
        >
          + Add Project
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
          title="No projects yet"
          body="Projects organize work — Issues and Tasks live inside a project. Click + Add Project to create one."
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
        <span>Archived</span>
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
              No archived projects.
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
function ProjectRow({ project: p }: { project: Project }): React.ReactElement {
  return (
    <li data-testid="project-row" data-project-id={p.id}>
      <OrgLink
        to={`/projects/${encodeURIComponent(p.id)}`}
        className="flex flex-col gap-1 px-4 py-3 motion-safe:transition-colors hover:bg-bg-subtle"
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
            {p.description || <span className="italic text-text-muted">no description</span>}
          </span>
          <span className="text-xs tabular-nums text-text-muted">
            {formatRelative(p.created_at)}
          </span>
        </div>
      </OrgLink>
    </li>
  );
}

// ProjectCounts renders the per-project task/issue/plan/repo count meta line
// (v2.10.0 #T81, §3.4.1) shown on the Projects list cards. Each count is
// optional — present only on the LIST response — so a chip is rendered only
// when its value is a number, and the whole row is omitted when none are.
function ProjectCounts({ project: p }: { project: Project }): React.ReactElement | null {
  const chips: Array<{ key: string; n: number; singular: string }> = [];
  if (typeof p.task_count === 'number') chips.push({ key: 'tasks', n: p.task_count, singular: 'task' });
  if (typeof p.issue_count === 'number') chips.push({ key: 'issues', n: p.issue_count, singular: 'issue' });
  if (typeof p.plan_count === 'number') chips.push({ key: 'plans', n: p.plan_count, singular: 'plan' });
  if (typeof p.repo_count === 'number') chips.push({ key: 'repos', n: p.repo_count, singular: 'repo' });
  if (chips.length === 0) return null;
  return (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs tabular-nums text-text-muted"
      data-testid="project-counts"
    >
      {chips.map((c) => (
        <span key={c.key} data-testid={`project-count-${c.key}`}>
          {c.n} {c.singular}
          {c.n === 1 ? '' : 's'}
        </span>
      ))}
    </div>
  );
}

// ProjectStatusBadge renders the active/archived status chip.
function ProjectStatusBadge({ status }: { status: Project['status'] }): React.ReactElement {
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
      {status}
    </span>
  );
}

// Tiny inline relative-time helper (matches Home.tsx). Avoids a new
// date-fns dep for "Xs / Xm / Xh / Xd ago" precision.
function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const delta = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (delta < 60) return `${delta}s ago`;
  if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
  if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
  return `${Math.floor(delta / 86400)}d ago`;
}
