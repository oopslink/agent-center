import type React from 'react';
import { Suspense, useEffect, useMemo, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { SSEIndicator } from '@/sse/SSEIndicator';
import { useSSE } from '@/sse/useSSE';
import { useInputRequests } from '@/api/inputRequests';
import { useConversations } from '@/api/conversations';
import { useProjects } from '@/api/projects';
import { useAppStore } from '@/store/app';
import { PageSkeleton } from '@/components/Skeleton';
import { CommandPalette } from '@/components/CommandPalette';
import { WorkerEnrolledToast } from '@/components/WorkerEnrolledToast';
import { useKeyShortcuts } from '@/useKeyShortcuts';
import { readTheme, writeTheme, type Theme } from '@/theme';

// AppLayout v3 — v2.3 P6 layered on P2's shell + P3's Home wire-in.
//   - desktop sidebar can collapse to an icon-only strip (persisted)
//   - dark mode toggle in header (persisted; applied pre-React in main.tsx)
//   - keyboard shortcuts: ⌘K palette, ⌘B sidebar toggle, ⌘D theme,
//     ⌘1..7 jump to top-level pages
//   - <CommandPalette> mounts at root so ⌘K works from anywhere

const SIDEBAR_KEY = 'ac.sidebar.collapsed';
// v2.5.x #63 — per-group + per-expandable-item expand state lives in
// these two localStorage JSON maps. Default for unseen keys is `true`
// (expanded) per the design ask.
const GROUP_STATE_KEY = 'ac.sidebar.groups';
const SUBITEM_STATE_KEY = 'ac.sidebar.subitems';

function readSidebarCollapsed(): boolean {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return false;
    return localStorage.getItem(SIDEBAR_KEY) === '1';
  } catch {
    return false;
  }
}

function readJSONMap(key: string): Record<string, boolean> {
  try {
    if (typeof localStorage === 'undefined' || typeof localStorage.getItem !== 'function') return {};
    const raw = localStorage.getItem(key);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as unknown;
    if (parsed && typeof parsed === 'object') return parsed as Record<string, boolean>;
    return {};
  } catch {
    return {};
  }
}

function writeJSONMap(key: string, value: Record<string, boolean>): void {
  try {
    if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
      localStorage.setItem(key, JSON.stringify(value));
    }
  } catch {
    // ignore
  }
}

export default function AppLayout(): React.ReactElement {
  useSSE();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(readSidebarCollapsed);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();

  // Auto-close the drawer on navigation so a tap on a nav item also
  // dismisses the overlay (common mobile pattern).
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

  // Persist sidebar + theme on change. Guarded against test environments
  // where localStorage may be a stub without setItem.
  useEffect(() => {
    try {
      if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
        localStorage.setItem(SIDEBAR_KEY, collapsed ? '1' : '0');
      }
    } catch {
      // ignore
    }
  }, [collapsed]);

  useEffect(() => {
    writeTheme(theme);
  }, [theme]);

  // Cmd/Ctrl shortcuts. Defined inside the component so closures bind
  // to the current setState handles.
  const shortcuts = useMemo(
    () => ({
      'mod+k': () => setPaletteOpen((v) => !v),
      'mod+b': () => setCollapsed((v) => !v),
      'mod+d': () => setTheme((t) => (t === 'dark' ? 'light' : 'dark')),
      'mod+1': () => navigate('/'),
      'mod+2': () => navigate('/channels'),
      'mod+3': () => navigate('/dms'),
      'mod+4': () => navigate('/issues'),
      'mod+5': () => navigate('/tasks'),
      'mod+6': () => navigate('/inputrequests'),
      'mod+7': () => navigate('/agents'),
    }),
    [navigate],
  );
  useKeyShortcuts(shortcuts);

  return (
    <div className="flex h-screen flex-col bg-bg-base">
      <header className="flex h-12 flex-shrink-0 items-center justify-between border-b border-border-base bg-bg-elevated px-3 sm:px-4">
        <div className="flex items-center gap-2 sm:gap-3">
          <button
            type="button"
            aria-label={drawerOpen ? 'Close navigation' : 'Open navigation'}
            aria-expanded={drawerOpen}
            data-testid="nav-toggle"
            onClick={() => setDrawerOpen((v) => !v)}
            className="-ml-1 inline-flex h-8 w-8 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors md:hidden"
          >
            <HamburgerIcon />
          </button>
          <button
            type="button"
            aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
            aria-pressed={collapsed}
            data-testid="sidebar-collapse-toggle"
            onClick={() => setCollapsed((v) => !v)}
            className="hidden h-8 w-8 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors md:inline-flex"
            title="Toggle sidebar (⌘B)"
          >
            <SidebarToggleIcon collapsed={collapsed} />
          </button>
          <span className="font-heading text-base font-semibold tracking-tight text-text-primary">
            agent-center
          </span>
        </div>
        <div className="flex items-center gap-3 sm:gap-4">
          <button
            type="button"
            onClick={() => setPaletteOpen(true)}
            aria-label="Open command palette"
            data-testid="open-palette"
            className="hidden items-center gap-2 rounded border border-border-base px-2 py-1 text-xs text-text-muted hover:bg-bg-subtle motion-safe:transition-colors sm:inline-flex"
          >
            <span>Search</span>
            <kbd className="font-mono">⌘K</kbd>
          </button>
          <SSEIndicator />
          <button
            type="button"
            aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
            data-testid="theme-toggle"
            onClick={() => setTheme((t) => (t === 'dark' ? 'light' : 'dark'))}
            className="inline-flex h-8 w-8 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors"
            title="Toggle theme (⌘D)"
          >
            {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
          </button>
          <span className="hidden text-xs text-text-muted sm:inline">
            v2 · loopback
          </span>
        </div>
      </header>
      <div className="flex flex-1 overflow-hidden">
        <Sidebar
          drawerOpen={drawerOpen}
          collapsed={collapsed}
          onDismiss={() => setDrawerOpen(false)}
        />
        <main className="flex flex-1 overflow-hidden">
          <div className="mx-auto flex h-full w-full max-w-6xl flex-col overflow-y-auto p-4 sm:p-6">
            <Suspense fallback={<PageSkeleton />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
      <WorkerEnrolledToast />
    </div>
  );
}

type NavBadgeKey = 'inputRequests' | null;

interface NavItem {
  to: string;
  label: string;
  badge?: NavBadgeKey;
  end?: boolean; // react-router NavLink end-match (used for '/' Home)
  Icon: () => React.ReactElement;
}

interface NavSection {
  label: string;
  items: ReadonlyArray<NavItem>;
}

const navSections: ReadonlyArray<NavSection> = [
  {
    label: 'Home',
    items: [
      { to: '/', label: 'Overview', Icon: HomeIcon, end: true },
    ],
  },
  {
    // v2.3-4: Workspace surface — Projects were not previously exposed
    // in the SPA top-level nav (backend already supported the read
    // endpoint via /api/projects since v2.1-A).
    label: 'Workspace',
    items: [
      { to: '/projects', label: 'Projects', Icon: FolderIcon },
    ],
  },
  {
    label: 'Conversations',
    items: [
      { to: '/channels', label: 'Channels', Icon: HashIcon },
      { to: '/dms', label: 'DMs', Icon: ChatIcon },
    ],
  },
  {
    label: 'Work',
    items: [
      { to: '/issues', label: 'Issues', Icon: IssuesIcon },
      { to: '/tasks', label: 'Tasks', Icon: TasksIcon },
      { to: '/inputrequests', label: 'Input Requests', badge: 'inputRequests', Icon: InboxIcon },
    ],
  },
  {
    label: 'System',
    items: [
      { to: '/fleet', label: 'Fleet', Icon: FleetIcon },
      { to: '/agents', label: 'Agents', Icon: AgentsIcon },
      { to: '/settings', label: 'Settings', Icon: SettingsIcon },
    ],
  },
];

function Sidebar({
  drawerOpen,
  collapsed,
  onDismiss,
}: {
  drawerOpen: boolean;
  collapsed: boolean;
  onDismiss: () => void;
}): React.ReactElement {
  const irs = useInputRequests();
  const inputRequestBadge = (irs.data ?? []).filter(
    (ir) => ir.status === 'pending',
  ).length;

  // v2.5.x #63 — per-group + per-expandable-item expand state. Default
  // for unseen keys is true (expanded).
  const [groupExpanded, setGroupExpanded] = useState<Record<string, boolean>>(
    () => readJSONMap(GROUP_STATE_KEY),
  );
  const [subItemExpanded, setSubItemExpanded] = useState<Record<string, boolean>>(
    () => readJSONMap(SUBITEM_STATE_KEY),
  );
  useEffect(() => {
    writeJSONMap(GROUP_STATE_KEY, groupExpanded);
  }, [groupExpanded]);
  useEffect(() => {
    writeJSONMap(SUBITEM_STATE_KEY, subItemExpanded);
  }, [subItemExpanded]);
  const isGroupOpen = (label: string) =>
    groupExpanded[label] === undefined ? true : groupExpanded[label];
  const isSubItemOpen = (to: string) =>
    subItemExpanded[to] === undefined ? true : subItemExpanded[to];
  const toggleGroup = (label: string) =>
    setGroupExpanded((m) => ({ ...m, [label]: !isGroupOpen(label) }));
  const toggleSubItem = (to: string) =>
    setSubItemExpanded((m) => ({ ...m, [to]: !isSubItemOpen(to) }));

  // Pull channel + DM + project lists for the expandable sub-items.
  // Each list is small + cached; backend already supports these reads
  // (v2.0 conversations, v2.1-A projects).
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });
  const projects = useProjects();
  const me = useAppStore((s) => s.currentUserId);
  const channelChildren = (channels.data ?? [])
    .filter((c) => c.status !== 'archived')
    .map((c) => ({ to: `/channels/${encodeURIComponent(c.name ?? '')}`, label: `# ${c.name}` }));
  const dmChildren = (dms.data ?? []).map((d) => {
    const peers = (d.participants ?? [])
      .filter((p) => !p.left_at && p.identity_id !== me)
      .map((p) => p.identity_id);
    const peerLabel = d.name || peers.join(' · ') || d.id;
    return { to: `/dms/${encodeURIComponent(d.id)}`, label: `@ ${peerLabel}` };
  });
  // v2.5.x #67 — Projects expand to the project list, mirroring the
  // Channels/DMs pattern so the Workspace group is consistent with
  // Conversations. Link target: /projects/<id>.
  const projectChildren = (projects.data ?? []).map((p) => ({
    to: `/projects/${encodeURIComponent(p.id)}`,
    label: p.name || p.id,
  }));

  // The drawer always shows full labels; the desktop bar shrinks to an
  // icon-only strip when `collapsed` is true.
  const navTree = (isCollapsed: boolean) => (
    <ul className="space-y-4">
      {navSections.map((section) => {
        const open = isGroupOpen(section.label);
        const showCollapsibleHeader = !isCollapsed && section.items.length > 0;
        return (
          <li key={section.label}>
            {showCollapsibleHeader ? (
              <button
                type="button"
                onClick={() => toggleGroup(section.label)}
                aria-expanded={open}
                data-testid={`sidebar-group-toggle-${section.label}`}
                className="flex w-full items-center justify-between rounded px-2 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:bg-bg-subtle"
              >
                <span>{section.label}</span>
                <span aria-hidden="true" className="text-text-muted">
                  {open ? '⌄' : '›'}
                </span>
              </button>
            ) : (
              !isCollapsed && (
                <h2 className="px-2 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
                  {section.label}
                </h2>
              )
            )}
            {(isCollapsed || open) && (
              <ul className="space-y-0.5">
                {section.items.map((item) => {
                  const badgeCount =
                    item.badge === 'inputRequests' ? inputRequestBadge : 0;
                  // Channels / DMs / Projects nav items expand into sub-lists.
                  const subChildren =
                    item.to === '/channels'
                      ? channelChildren
                      : item.to === '/dms'
                        ? dmChildren
                        : item.to === '/projects'
                          ? projectChildren
                          : null;
                  const subOpen = isSubItemOpen(item.to);
                  return (
                    <li key={item.to}>
                      <div className="flex items-center gap-1">
                        <NavLink
                          to={item.to}
                          end={item.end}
                          title={isCollapsed ? item.label : undefined}
                          className={({ isActive }) =>
                            [
                              'flex flex-1 items-center rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                              isCollapsed ? 'justify-center' : 'justify-between',
                              isActive
                                ? 'bg-brand text-white'
                                : 'text-text-primary hover:bg-bg-subtle',
                            ].join(' ')
                          }
                        >
                          <span className={isCollapsed ? 'inline-flex' : 'flex items-center gap-2'}>
                            <span aria-hidden="true" className="inline-flex h-4 w-4">
                              <item.Icon />
                            </span>
                            {!isCollapsed && (
                              <span className="flex items-center gap-1.5">
                                {item.label}
                                {subChildren && (
                                  <span className="rounded bg-bg-elevated px-1.5 text-[0.6875rem] text-text-muted tabular-nums">
                                    {subChildren.length}
                                  </span>
                                )}
                              </span>
                            )}
                          </span>
                          {badgeCount > 0 && (
                            <span
                              className={[
                                'rounded-full bg-accent text-xs font-medium text-white tabular-nums',
                                isCollapsed
                                  ? 'absolute ml-3 -mt-3 h-3.5 w-3.5 text-[0.625rem] leading-none flex items-center justify-center'
                                  : 'px-1.5 py-0.5',
                              ].join(' ')}
                              data-testid={`nav-badge-${item.badge}`}
                            >
                              {badgeCount}
                            </span>
                          )}
                        </NavLink>
                        {!isCollapsed && subChildren && (
                          <button
                            type="button"
                            onClick={() => toggleSubItem(item.to)}
                            aria-expanded={subOpen}
                            aria-label={`Toggle ${item.label} list`}
                            data-testid={`sidebar-subitem-toggle-${item.to}`}
                            className="rounded p-1 text-xs text-text-muted hover:bg-bg-subtle hover:text-text-primary"
                          >
                            {subOpen ? '⌄' : '›'}
                          </button>
                        )}
                      </div>
                      {!isCollapsed && subChildren && subOpen && (
                        <ul
                          className="ml-6 mt-0.5 space-y-0.5 border-l border-border-base pl-2"
                          data-testid={`sidebar-subitem-list-${item.to}`}
                        >
                          {subChildren.length === 0 && (
                            <li className="px-2 py-0.5 text-xs italic text-text-muted">
                              (none)
                            </li>
                          )}
                          {subChildren.map((child) => (
                            <li key={child.to}>
                              <NavLink
                                to={child.to}
                                className={({ isActive }) =>
                                  [
                                    'block truncate rounded px-2 py-0.5 text-xs',
                                    isActive
                                      ? 'bg-brand text-white'
                                      : 'text-text-secondary hover:bg-bg-subtle hover:text-text-primary',
                                  ].join(' ')
                                }
                                data-testid="sidebar-subitem-link"
                              >
                                {child.label}
                              </NavLink>
                            </li>
                          ))}
                        </ul>
                      )}
                    </li>
                  );
                })}
              </ul>
            )}
          </li>
        );
      })}
    </ul>
  );

  return (
    <>
      {/* Desktop sidebar — width depends on collapsed flag. */}
      <nav
        aria-label="primary"
        data-collapsed={collapsed}
        className={[
          'hidden flex-shrink-0 border-r border-border-base bg-bg-subtle p-3 md:block',
          collapsed ? 'w-14' : 'w-52',
        ].join(' ')}
      >
        {navTree(collapsed)}
      </nav>
      {/* Mobile drawer — opens on hamburger toggle (always full-width labels). */}
      {drawerOpen && (
        <div className="fixed inset-0 z-40 flex md:hidden" role="dialog" aria-modal="true">
          <button
            type="button"
            aria-label="Close navigation overlay"
            onClick={onDismiss}
            className="flex-1 bg-black/40 motion-safe:transition-opacity"
          />
          <nav
            aria-label="primary mobile"
            className="w-64 max-w-[80%] flex-shrink-0 overflow-y-auto border-l border-border-base bg-bg-subtle p-3 shadow-3"
            onClick={(e) => e.stopPropagation()}
          >
            {navTree(false)}
          </nav>
        </div>
      )}
    </>
  );
}

// ============================================================================
// Inline Heroicons-style outline SVGs (skill rule `no-emoji-icons` +
// `icon-style-consistent`). Single stroke-width, 20×20 viewbox, current
// color. Inlining avoids pulling a whole icon library for ~7 glyphs.
// ============================================================================

function SidebarToggleIcon({ collapsed }: { collapsed: boolean }): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3" y="4" width="14" height="12" rx="1.5" />
      <path d="M8 4v12" />
      <path d={collapsed ? 'M11 8l2 2-2 2' : 'M13 8l-2 2 2 2'} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function SunIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="10" r="3" />
      <path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.2 4.2l1.4 1.4M14.4 14.4l1.4 1.4M4.2 15.8l1.4-1.4M14.4 5.6l1.4-1.4" strokeLinecap="round" />
    </svg>
  );
}
function MoonIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M16.5 12a6.5 6.5 0 1 1-8.5-8.5 5.5 5.5 0 0 0 8.5 8.5z" strokeLinejoin="round" />
    </svg>
  );
}
function HomeIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 10l7-6 7 6v6.5A1.5 1.5 0 0 1 15.5 18h-3v-5h-5v5h-3A1.5 1.5 0 0 1 3 16.5V10z" strokeLinejoin="round" />
    </svg>
  );
}
function HamburgerIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.75" aria-hidden="true">
      <path d="M3.5 5h13M3.5 10h13M3.5 15h13" strokeLinecap="round" />
    </svg>
  );
}
function FolderIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h3l1.5 2h6.5A1.5 1.5 0 0 1 17 8.5v6A1.5 1.5 0 0 1 15.5 16h-11A1.5 1.5 0 0 1 3 14.5v-8z" strokeLinejoin="round" />
    </svg>
  );
}
function HashIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M8 3 6.5 17M13.5 3 12 17M3.5 7h13M3 13h13" strokeLinecap="round" />
    </svg>
  );
}
function ChatIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4 5h12a1.5 1.5 0 0 1 1.5 1.5v6a1.5 1.5 0 0 1-1.5 1.5h-5l-3 3v-3H4A1.5 1.5 0 0 1 2.5 12.5v-6A1.5 1.5 0 0 1 4 5z" strokeLinejoin="round" />
    </svg>
  );
}
function IssuesIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="10" r="6.5" />
      <path d="M10 7v3.5M10 13.25v.25" strokeLinecap="round" />
    </svg>
  );
}
function TasksIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3.5" y="3.5" width="13" height="13" rx="2" />
      <path d="M6.5 10.5l2.5 2.5 4.5-5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function InboxIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 11.5V5a1.5 1.5 0 0 1 1.5-1.5h11A1.5 1.5 0 0 1 17 5v6.5M3 11.5h4.5l1 2h3l1-2H17M3 11.5V15a1.5 1.5 0 0 0 1.5 1.5h11A1.5 1.5 0 0 0 17 15v-3.5" strokeLinejoin="round" />
    </svg>
  );
}
function FleetIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="2.5" y="6" width="6" height="8" rx="1" />
      <rect x="11.5" y="6" width="6" height="8" rx="1" />
      <path d="M5.5 9.5h0.01M14.5 9.5h0.01" strokeLinecap="round" />
    </svg>
  );
}
function AgentsIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="4" y="6" width="12" height="9" rx="2" />
      <path d="M7 6V4.5M13 6V4.5M8 10v.5M12 10v.5M7.5 13h5" strokeLinecap="round" />
    </svg>
  );
}
function SettingsIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="10" r="2.5" />
      <path d="M10 3v2M10 15v2M3 10h2M15 10h2M5.05 5.05l1.4 1.4M13.55 13.55l1.4 1.4M5.05 14.95l1.4-1.4M13.55 6.45l1.4-1.4" strokeLinecap="round" />
    </svg>
  );
}
