import type React from 'react';
import { Link, NavLink, useLocation } from 'react-router-dom';
import { useProject } from '@/api/projects';
import type { ModuleSecondaryNavProps } from '@/shell/secondaryNav';

// ============================================================================
// v2.10.0 [T4] — Workspace col② secondary nav (registered override).
//
// Route-aware (docs/design/v2.10.0/projects.html):
//   • NOT inside a project → the top-level Workspace nav:
//       Projects / Issues / Tasks / Plan.
//   • Inside a project (/projects/:id…) → the project sub-nav:
//       ‹ Projects  ›  <project name>
//       Issues / Tasks / Work Board / Members / Code repos
//     The project tabs are driven by ?tab= on /projects/:id (Issues/Tasks/
//     Members/Code repos — synced with ProjectDetail's in-page tab bar); Work
//     Board is the /projects/:id/plans route.
//
// This is a per-module override (SECONDARY_NAV_REGISTRY.workspace) so it lives
// in its own file — AppLayout is untouched. The mockup drops the old Projects-
// expands-to-all-projects sub-list (the Projects LIST page covers that now).
// ============================================================================

const PROJECT_TABS: ReadonlyArray<{ key: string; label: string; Icon: () => React.ReactElement }> = [
  { key: 'issues', label: 'Issues', Icon: IssueIcon },
  { key: 'tasks', label: 'Tasks', Icon: TaskIcon },
  { key: 'members', label: 'Members', Icon: MembersIcon },
  { key: 'repos', label: 'Code repos', Icon: ReposIcon },
];

// Parse "<orgBase>/projects/<id>…" → the project id, or null when not inside a
// specific project (the bare /projects list is NOT "inside a project").
function projectIdFromPath(pathname: string, orgBase: string): string | null {
  const rest = orgBase && pathname.startsWith(orgBase) ? pathname.slice(orgBase.length) : pathname;
  const segs = rest.split('/').filter(Boolean);
  if (segs[0] === 'projects' && segs[1]) return decodeURIComponent(segs[1]);
  return null;
}

export default function WorkspaceSecondaryNav({ orgBase }: ModuleSecondaryNavProps): React.ReactElement {
  const location = useLocation();
  const projectId = projectIdFromPath(location.pathname, orgBase);
  if (projectId) {
    return <ProjectSubNav orgBase={orgBase} projectId={projectId} />;
  }
  return <TopLevelWorkspaceNav orgBase={orgBase} />;
}

// --- top-level Workspace nav (Projects / Issues / Tasks / Plan) --------------
function TopLevelWorkspaceNav({ orgBase }: { orgBase: string }): React.ReactElement {
  const items: ReadonlyArray<{ to: string; label: string; Icon: () => React.ReactElement; end?: boolean }> = [
    { to: `${orgBase}/projects`, label: 'Projects', Icon: ProjectsIcon },
    { to: `${orgBase}/issues`, label: 'Issues', Icon: IssueIcon },
    { to: `${orgBase}/tasks`, label: 'Tasks', Icon: TaskIcon },
    // v2.10.2 [T142]: "Plan" → "Plans" (plural, consistent with the siblings).
    { to: `${orgBase}/plans`, label: 'Plans', Icon: PlanIcon },
    // T207 [提醒-3]: Reminders (Cognition BC). Placement per owner — adjustable.
    { to: `${orgBase}/reminders`, label: 'Reminders', Icon: ReminderIcon },
  ];
  return (
    <div data-testid="workspace-nav-toplevel">
      <h3 className="px-2 pb-1 pt-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
        <span data-testid="section-label">Workspace</span>
      </h3>
      <ul className="space-y-0.5">
        {items.map((item) => (
          <li key={item.to}>
            <NavLink
              to={item.to}
              end={item.to.endsWith('/projects') ? true : undefined}
              className={({ isActive }) =>
                [
                  'flex items-center gap-2 rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                  isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
                ].join(' ')
              }
            >
              <span aria-hidden="true" className="inline-flex h-4 w-4">
                <item.Icon />
              </span>
              <span>{item.label}</span>
            </NavLink>
          </li>
        ))}
      </ul>
    </div>
  );
}

// --- project sub-nav (inside a project) -------------------------------------
function ProjectSubNav({ orgBase, projectId }: { orgBase: string; projectId: string }): React.ReactElement {
  const project = useProject(projectId);
  const name = project.data?.name || projectId;
  const location = useLocation();
  const base = `${orgBase}/projects/${encodeURIComponent(projectId)}`;
  const params = new URLSearchParams(location.search);
  const activeTab = params.get('tab') || 'issues';
  // On the /projects/:id detail page (no extra path segment) a tab is active;
  // on /projects/:id/plans* the Work Board entry is active instead.
  const restAfterId = location.pathname.slice(base.length).split('/').filter(Boolean);
  const onWorkBoard = restAfterId[0] === 'plans';
  const onDetail = restAfterId.length === 0;

  const itemCls = (active: boolean) =>
    [
      'flex items-center gap-2 rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
      active ? 'bg-brand-hover text-white' : 'text-text-secondary hover:bg-bg-subtle hover:text-text-primary',
    ].join(' ');

  return (
    <div data-testid="workspace-nav-project" data-project-id={projectId}>
      {/* Back to the Projects list. */}
      <Link
        to={`${orgBase}/projects`}
        data-testid="project-subnav-back"
        className="flex items-center gap-1.5 px-2 pb-1 pt-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
      >
        <span aria-hidden="true">‹</span>
        <span data-testid="section-label">Projects</span>
      </Link>
      {/* Current project header. */}
      <div className="truncate px-2 pb-1 text-sm font-semibold text-text-primary" title={name}>
        {name}
      </div>
      <ul className="space-y-0.5">
        {PROJECT_TABS.map((t) => {
          const active = onDetail && activeTab === t.key;
          return (
            <li key={t.key}>
              {/* plain Link (not NavLink): active state is ?tab=-driven, which
                  NavLink can't match (it matches on path, ignoring the query). */}
              <Link
                to={`${base}?tab=${t.key}`}
                data-testid={`project-subnav-${t.key}`}
                aria-current={active ? 'page' : undefined}
                className={itemCls(active)}
              >
                <span aria-hidden="true" className="inline-flex h-4 w-4">
                  <t.Icon />
                </span>
                <span>{t.label}</span>
              </Link>
            </li>
          );
        })}
        {/* Work Board = the per-project plan board route. */}
        <li>
          <Link
            to={`${base}/plans`}
            data-testid="project-subnav-workboard"
            aria-current={onWorkBoard ? 'page' : undefined}
            className={itemCls(onWorkBoard)}
          >
            <span aria-hidden="true" className="inline-flex h-4 w-4">
              <WorkBoardIcon />
            </span>
            <span>Work Board</span>
          </Link>
        </li>
      </ul>
    </div>
  );
}

// --- inline icons (no-emoji-icons gate) -------------------------------------
function ProjectsIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h3l1.5 2h6.5A1.5 1.5 0 0 1 17 8.5v6A1.5 1.5 0 0 1 15.5 16h-11A1.5 1.5 0 0 1 3 14.5v-8z" strokeLinejoin="round" />
    </svg>
  );
}
function IssueIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="10" r="6.5" />
      <circle cx="10" cy="10" r="1.5" fill="currentColor" stroke="none" />
    </svg>
  );
}
function TaskIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4 6h6M4 10h6M4 14h4" strokeLinecap="round" />
      <path d="M13 6.5l1.5 1.5 2.5-3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function PlanIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M10 2.5 17.5 10 10 17.5 2.5 10z" strokeLinejoin="round" />
    </svg>
  );
}
// T207: Reminders ⏰ — a clock glyph for the Workspace nav entry.
function ReminderIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="11" r="6" />
      <path d="M10 8v3l2 1.5M6 3.5 3.5 6M14 3.5 16.5 6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function MembersIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="7.5" cy="7" r="2.5" />
      <path d="M2 16c0-3 2.5-5 5.5-5s5.5 2 5.5 5" strokeLinecap="round" />
      <path d="M13 8.5a2 2 0 1 0 0-4M18 16c0-2.5-2-4-4-4" strokeLinecap="round" />
    </svg>
  );
}
function ReposIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <ellipse cx="10" cy="5" rx="6" ry="2.4" />
      <path d="M4 5v10c0 1.3 2.7 2.4 6 2.4s6-1.1 6-2.4V5M4 10c0 1.3 2.7 2.4 6 2.4s6-1.1 6-2.4" strokeLinecap="round" />
    </svg>
  );
}
function WorkBoardIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3" y="3" width="14" height="14" rx="2" />
      <path d="M8 3v14M13 3v14" strokeLinecap="round" />
    </svg>
  );
}
