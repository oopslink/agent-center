import React, { useState } from 'react';
import { OrgLink } from '@/OrgContext';

import { useProjects, type Project } from '@/api/projects';
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
            <li key={p.id} data-testid="project-row" data-project-id={p.id}>
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
          ))}
        </ul>
      )}
    </section>
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
