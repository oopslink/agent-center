import type React from 'react';
import { Suspense, useEffect, useMemo, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { SSEIndicator } from '@/sse/SSEIndicator';
import { useSSE } from '@/sse/useSSE';
import {
  conversationDeleteErrorMessage,
  useConversations,
  useDeleteConversation,
} from '@/api/conversations';
import { useProjects } from '@/api/projects';
import { identityRefOf } from '@/api/members';
import { useAppStore } from '@/store/app';
import { PageSkeleton } from '@/components/Skeleton';
import { UnreadBadge } from '@/components/UnreadBadge';
import { CommandPalette } from '@/components/CommandPalette';
import { WorkerEnrolledToast } from '@/components/WorkerEnrolledToast';
import { OrgSettingsModal } from '@/components/OrgSettingsModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { useKeyShortcuts } from '@/useKeyShortcuts';
import { readTheme, writeTheme, type Theme } from '@/theme';
import { useMe, useSignout, useOrgs, orgApi } from '@/api/auth';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOptionalOrgContext, orgPath } from './OrgContext';

// AppLayout v4 — v2.8.1 #278 "Topbar→sidebar" chrome redesign. The old
// top header bar is gone: org switcher, the ⌘K search trigger, the
// Light/Dark theme control, the live (SSE) indicator, the signed-in user
// and Sign out ALL live inside the single left sidebar now (mockup-locked).
// A minimal mobile-only top strip keeps the hamburger for the drawer.
//   - desktop sidebar can collapse to an icon-only strip (persisted)
//   - segmented Light/Dark theme control at the sidebar bottom (persisted;
//     applied pre-React in main.tsx so there's no FOUC)
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
  const me = useMe();
  // v2.7 #155: wire the store's currentUserId to the AUTHENTICATED identity ref.
  // currentUserId starts EMPTY (no hardcoded placeholder); until this seeds it,
  // identity-ref comparisons (e.g. ParticipantsPanel's owner check → invite/
  // remove controls; DM peer filtering) and SSE connect are gated off. The
  // backend stamps refs as "<kind>:<id>" (user:/agent:), so build the same shape
  // from /api/auth/me (identity_id + kind), the per-request session identity
  // (#146) that also powers the attachment download gate.
  const setCurrentUserId = useAppStore((s) => s.setCurrentUserId);
  useEffect(() => {
    const m = me.data;
    if (!m?.identity_id) return;
    const ref = identityRefOf(m);
    setCurrentUserId(ref);
  }, [me.data?.identity_id, me.data?.kind, setCurrentUserId]);
  const orgs = useOrgs();
  const orgCtx = useOptionalOrgContext();
  const currentOrg = orgCtx
    ? (orgs.data ?? []).find((o) => o.slug === orgCtx.slug)
    : orgs.data?.[0];
  const [orgDropdownOpen, setOrgDropdownOpen] = useState(false);
  const [createOrgModalOpen, setCreateOrgModalOpen] = useState(false);
  // v2.7 #186-6: org settings is a per-org modal opened from the switcher gear.
  const [settingsOrgId, setSettingsOrgId] = useState<string | null>(null);
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
      'mod+4': () => navigate('/projects'),
      // dev2/v281: ⌘6 jumps to the enhanced canonical /agents page (NOT the
      // retired /members/agents). Org-scoped via orgPath so it resolves under
      // /organizations/{slug}/agents (mirrors CommandPalette's orgPath rewrite)
      // instead of falling through to OrgRedirect.
      'mod+6': () => navigate(orgPath('/agents', orgCtx?.slug)),
      'mod+7': () => navigate('/environment'),
    }),
    [navigate, orgCtx?.slug],
  );
  useKeyShortcuts(shortcuts);

  // v2.8.1 #278: org-switcher + ⌘K search + theme + live + user + Sign out
  // all moved INTO the sidebar. These props wire the sidebar-hosted org
  // switcher back to the org dropdown / create / settings modals that still
  // mount at this root level (so they overlay the whole app).
  const orgSwitcher = {
    currentOrg,
    orgs: orgs.data ?? [],
    currentSlug: orgCtx?.slug,
    fallbackName: me.data?.display_name,
    open: orgDropdownOpen,
    onToggle: () => setOrgDropdownOpen((v) => !v),
    onClose: () => setOrgDropdownOpen(false),
    onCreateOrg: () => {
      setOrgDropdownOpen(false);
      setCreateOrgModalOpen(true);
    },
    onOpenSettings: (id: string) => {
      setOrgDropdownOpen(false);
      setSettingsOrgId(id);
    },
  };

  return (
    <div className="flex h-screen bg-bg-base">
      {/* Mobile-only top strip: the sidebar is a left column on desktop, so
          on small screens we keep a slim bar that just hosts the hamburger
          for the drawer + the org name for context. */}
      <header className="fixed inset-x-0 top-0 z-30 flex h-12 items-center gap-2 border-b border-border-base bg-bg-elevated px-3 md:hidden">
        <button
          type="button"
          aria-label={drawerOpen ? 'Close navigation' : 'Open navigation'}
          aria-expanded={drawerOpen}
          data-testid="nav-toggle"
          onClick={() => setDrawerOpen((v) => !v)}
          className="-ml-1 inline-flex h-8 w-8 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors"
        >
          <HamburgerIcon />
        </button>
        <span className="truncate text-sm font-medium text-text-primary">
          {currentOrg?.name ?? me.data?.display_name ?? '…'}
        </span>
      </header>
      <Sidebar
        drawerOpen={drawerOpen}
        collapsed={collapsed}
        theme={theme}
        onSetTheme={setTheme}
        onToggleCollapsed={() => setCollapsed((v) => !v)}
        onDismiss={() => setDrawerOpen(false)}
        onOpenPalette={() => setPaletteOpen(true)}
        displayName={me.data?.display_name}
        orgSwitcher={orgSwitcher}
      />
      <main className="flex flex-1 overflow-hidden pt-12 md:pt-0">
        <div
          className="flex h-full w-full flex-col overflow-y-auto p-4 sm:p-6"
          data-testid="app-content-shell"
        >
          <Suspense fallback={<PageSkeleton />}>
            <Outlet />
          </Suspense>
        </div>
      </main>
      {/* Org create / settings modals overlay the whole app from the root. */}
      {createOrgModalOpen && (
        <CreateOrgModal onClose={() => setCreateOrgModalOpen(false)} />
      )}
      {settingsOrgId && (
        <OrgSettingsModal orgId={settingsOrgId} onClose={() => setSettingsOrgId(null)} />
      )}
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
      <WorkerEnrolledToast />
    </div>
  );
}

// v2.8.1 #278: shape passed from AppLayout into the sidebar-hosted org
// switcher (the dropdown logic itself is unchanged — see <OrgDropdown>).
interface OrgSwitcherBinding {
  currentOrg?: { id: string; slug: string; name: string };
  orgs: Array<{ id: string; slug: string; name: string }>;
  currentSlug?: string;
  fallbackName?: string;
  open: boolean;
  onToggle: () => void;
  onClose: () => void;
  onCreateOrg: () => void;
  onOpenSettings: (orgId: string) => void;
}

interface NavItem {
  to: string;
  label: string;
  end?: boolean; // react-router NavLink end-match (used for '/' Home)
  Icon: () => React.ReactElement;
}

interface NavSection {
  label: string;
  items: ReadonlyArray<NavItem>;
}

// v2.8 #264 P1 / #176: an expandable sidebar sub-item (a channel/DM/project).
// channel/DM rows carry the per-conversation unread/mention counts for their
// <UnreadBadge>; projects leave them undefined (no conversation badge).
interface SidebarChild {
  to: string;
  label: string;
  id?: string;
  kind?: 'channel' | 'dm' | 'project';
  canDelete?: boolean;
  unreadCount?: number;
  mentionCount?: number;
}

// v2.6-FE-6: nav sections are org-slug-prefixed.
function buildNavSections(base: string): ReadonlyArray<NavSection> {
  const p = (path: string) => `${base}/${path}`;
  return [
    {
      label: 'Home',
      items: [
        { to: base, label: 'Overview', Icon: HomeIcon, end: true },
      ],
    },
    {
      label: 'Workspace',
      items: [
        { to: p('projects'), label: 'Projects', Icon: FolderIcon },
        // v2.8 #258: org-scope cross-project aggregation, Project 同级.
        { to: p('issues'), label: 'Issues', Icon: IssueIcon },
        { to: p('tasks'), label: 'Tasks', Icon: TaskIcon },
      ],
    },
    {
      label: 'Conversations',
      items: [
        { to: p('channels'), label: 'Channels', Icon: HashIcon },
        { to: p('dms'), label: 'DMs', Icon: ChatIcon },
      ],
    },
    {
      // v2.7 #166: the org people group is labeled "Members" (Humans + Agents).
      // Organization Settings is NOT a sidebar item — it moved into the org
      // switcher dropdown (#166-2). The single "Agents" entry lives here (#165);
      // the old SYSTEM → Agents entry is removed. Agent management opens by
      // clicking an Agent member → AgentDetail (#157).
      label: 'Members',
      items: [
        { to: p('members/humans'), label: 'Humans', Icon: UsersIcon },
        // dev2/v281: "Agents" opens the enhanced canonical /agents list
        // (Name/Provider/Lifecycle/Availability/Last activity/Worker), NOT the
        // retired /members/agents page. p() org-scopes it to
        // /organizations/{slug}/agents.
        { to: p('agents'), label: 'Agents', Icon: AgentsIcon },
      ],
    },
    {
      // v2.7 #164: Fleet merged into Environment (one operational page). #165:
      // SYSTEM → Agents removed (single Agents entry under Members).
      label: 'System',
      items: [
        { to: p('environment'), label: 'Environment', Icon: FleetIcon },
        { to: p('settings'), label: 'Settings', Icon: SettingsIcon },
      ],
    },
  ];
}

function Sidebar({
  drawerOpen,
  collapsed,
  theme,
  onSetTheme,
  onToggleCollapsed,
  onDismiss,
  onOpenPalette,
  displayName,
  orgSwitcher,
}: {
  drawerOpen: boolean;
  collapsed: boolean;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  onToggleCollapsed: () => void;
  onDismiss: () => void;
  onOpenPalette: () => void;
  displayName?: string;
  orgSwitcher: OrgSwitcherBinding;
}): React.ReactElement {
  const signout = useSignout();
  const orgCtx = useOptionalOrgContext();
  const orgBase = orgCtx ? `/organizations/${orgCtx.slug}` : '';
  const navSections = buildNavSections(orgBase);
  const location = useLocation();
  const navigate = useNavigate();
  const deleteConversation = useDeleteConversation();
  const [pendingDeleteDM, setPendingDeleteDM] = useState<SidebarChild | null>(null);

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
  // v2.8 #264 P1 / #176: channel/DM sidebar children carry the per-row unread/
  // mention counts so each renders its own <UnreadBadge>; projects carry none.
  const channelChildren: SidebarChild[] = (channels.data ?? [])
    .filter((c) => c.status !== 'archived')
    // v2.7.1 #247: link by channel id (hash) — display still shows "# name".
    .map((c) => ({
      to: `${orgBase}/channels/${encodeURIComponent(c.id)}`,
      label: `# ${c.name}`,
      unreadCount: c.unread_count,
      mentionCount: c.mention_count,
    }));
  // v2.7.1 #215/Rule 2a: DM sidebar label = @peer_name (backend resolves the
  // other party); deleted peer → "(deleted)"; malformed DM → "Direct message".
  const dmChildren: SidebarChild[] = (dms.data ?? []).map((d) => {
    const label = d.peer_display_name
      ? `@${d.peer_display_name}`
      : d.peer_identity_id
        ? '(deleted)'
        : 'Direct message';
    return {
      id: d.id,
      kind: 'dm',
      to: `${orgBase}/dms/${encodeURIComponent(d.id)}`,
      label,
      canDelete: !!d.peer_identity_id && !d.peer_display_name,
      unreadCount: d.unread_count,
      mentionCount: d.mention_count,
    };
  });
  // v2.5.x #67 — Projects expand to the project list, mirroring the
  // Channels/DMs pattern so the Workspace group is consistent with
  // Conversations. Link target: /projects/<id>. (No conversation counts.)
  const projectChildren: SidebarChild[] = (projects.data ?? []).map((p) => ({
    to: `${orgBase}/projects/${encodeURIComponent(p.id)}`,
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
              // v2.8.1 #278: CAPS section label is also the collapse toggle.
              // Wrapped in an <h2> for heading semantics; the button stays the
              // interactive control (data-testid kept for existing tests).
              <h2 className="px-1">
                <button
                  type="button"
                  onClick={() => toggleGroup(section.label)}
                  aria-expanded={open}
                  data-testid={`sidebar-group-toggle-${section.label}`}
                  className="flex w-full items-center justify-between rounded px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
                >
                  <span data-testid="section-label">{section.label}</span>
                  <span aria-hidden="true" className="text-text-muted">
                    {open ? '⌄' : '›'}
                  </span>
                </button>
              </h2>
            ) : (
              !isCollapsed && (
                <h2
                  data-testid="section-label"
                  className="px-2 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted"
                >
                  {section.label}
                </h2>
              )
            )}
            {(isCollapsed || open) && (
              <ul className="space-y-0.5">
                {section.items.map((item) => {
                  // Channels / DMs / Projects nav items expand into sub-lists.
                  const subChildren =
                    item.to.endsWith('/channels')
                      ? channelChildren
                      : item.to.endsWith('/dms')
                        ? dmChildren
                        : item.to.endsWith('/projects')
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
                          // Collapsed = icon-only (text label hidden), so give every
                          // collapsed nav item a robust accessible name via aria-label
                          // (title alone is hover-dependent for some readers — Tester2
                          // #253 a11y flag). Expanded items keep their visible text label.
                          aria-label={isCollapsed ? item.label : undefined}
                          className={({ isActive }) =>
                            [
                              'flex flex-1 items-center rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                              isCollapsed ? 'justify-center' : 'justify-between',
                              isActive
                                ? 'bg-brand-hover text-white'
                                : 'text-text-primary hover:bg-bg-subtle',
                            ].join(' ')
                          }
                        >
                          <span className={isCollapsed ? 'inline-flex' : 'flex items-center gap-2'}>
                            <span aria-hidden="true" className="inline-flex h-4 w-4">
                              <item.Icon />
                            </span>
                            {!isCollapsed && (
                              <span className="flex flex-1 items-center justify-between gap-1.5">
                                <span>{item.label}</span>
                                {subChildren && (
                                  // v2.8.1 #278: real count from the live hook
                                  // (channels/DMs/projects length). Accessible
                                  // text via aria-label, not a bare number.
                                  <span
                                    data-testid={`count-badge-${item.label}`}
                                    aria-label={`${subChildren.length} ${item.label.toLowerCase()}`}
                                    className="rounded-full bg-bg-elevated px-1.5 text-[0.6875rem] text-text-muted tabular-nums"
                                  >
                                    {subChildren.length}
                                  </span>
                                )}
                              </span>
                            )}
                          </span>
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
                              <div className="flex items-center gap-1">
                                <NavLink
                                  to={child.to}
                                  className={({ isActive }) =>
                                    [
                                      'block min-w-0 flex-1 truncate rounded px-2 py-0.5 text-xs',
                                      isActive
                                        ? 'bg-brand-hover text-white'
                                        : 'text-text-secondary hover:bg-bg-subtle hover:text-text-primary',
                                    ].join(' ')
                                  }
                                  data-testid="sidebar-subitem-link"
                                >
                                  <span className="flex items-center justify-between gap-2">
                                    <span className="truncate">{child.label}</span>
                                    <UnreadBadge
                                      unreadCount={child.unreadCount}
                                      mentionCount={child.mentionCount}
                                    />
                                  </span>
                                </NavLink>
                                {child.kind === 'dm' && child.canDelete && (
                                  <button
                                    type="button"
                                    className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-text-muted hover:bg-danger/10 hover:text-danger"
                                    data-testid="sidebar-dm-delete-button"
                                    aria-label={`Delete DM ${child.label}`}
                                    title="Delete DM"
                                    onClick={() => {
                                      deleteConversation.reset();
                                      setPendingDeleteDM(child);
                                    }}
                                  >
                                    <TrashIcon />
                                  </button>
                                )}
                              </div>
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
      {/* Desktop sidebar — single left column holding the whole chrome
          (org switcher → search → nav → live/theme/user/signout). Width
          depends on collapsed flag. relative so the edge collapse chevron
          (#186-7a) can sit on the right border. */}
      <nav
        aria-label="primary"
        data-collapsed={collapsed}
        className={[
          'group/sidebar relative hidden flex-col flex-shrink-0 border-r border-border-base bg-bg-subtle p-3 md:flex',
          collapsed ? 'w-14' : 'w-60',
        ].join(' ')}
      >
        <SidebarTop collapsed={collapsed} onOpenPalette={onOpenPalette} orgSwitcher={orgSwitcher} />
        <div className="mt-3 flex-1 overflow-y-auto">{navTree(collapsed)}</div>
        {deleteConversation.isError && (
          <p
            className="mt-2 px-2 text-xs text-danger"
            data-testid="sidebar-dm-delete-error"
            role="alert"
          >
            {conversationDeleteErrorMessage(deleteConversation.error)}
          </p>
        )}
        <SidebarFooter
          collapsed={collapsed}
          theme={theme}
          onSetTheme={onSetTheme}
          displayName={displayName}
          orgBase={orgBase}
          onSignout={() => signout.mutate()}
        />
        {/* v2.7 #186-7a: collapse chevron embedded in the sidebar's right edge
            (Slack/VSCode pattern). → when collapsed, ← when expanded. */}
        <button
          type="button"
          aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          aria-pressed={collapsed}
          data-testid="sidebar-collapse-toggle"
          onClick={onToggleCollapsed}
          title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          className="absolute -right-3 top-4 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full border border-border-base bg-bg-elevated text-text-secondary opacity-0 shadow-sm hover:bg-bg-subtle hover:text-text-primary focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-accent group-hover/sidebar:opacity-100 motion-safe:transition-all"
        >
          <SidebarToggleIcon collapsed={collapsed} />
        </button>
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
            className="w-64 max-w-[80%] flex flex-col flex-shrink-0 border-l border-border-base bg-bg-subtle p-3 shadow-3"
            onClick={(e) => e.stopPropagation()}
          >
            <SidebarTop collapsed={false} onOpenPalette={onOpenPalette} orgSwitcher={orgSwitcher} />
            <div className="mt-3 flex-1 overflow-y-auto">{navTree(false)}</div>
            {deleteConversation.isError && (
              <p
                className="mt-2 px-2 text-xs text-danger"
                data-testid="sidebar-dm-delete-error"
                role="alert"
              >
                {conversationDeleteErrorMessage(deleteConversation.error)}
              </p>
            )}
            <SidebarFooter
              collapsed={false}
              theme={theme}
              onSetTheme={onSetTheme}
              displayName={displayName}
              orgBase={orgBase}
              onSignout={() => signout.mutate()}
            />
          </nav>
        </div>
      )}
      <ConfirmModal
        open={pendingDeleteDM !== null}
        danger
        busy={deleteConversation.isPending}
        title="Delete DM"
        message={
          pendingDeleteDM
            ? `Delete the DM "${pendingDeleteDM.label}"? This permanently removes the conversation and all its messages for everyone. This cannot be undone.`
            : undefined
        }
        confirmLabel="Delete"
        onCancel={() => {
          if (deleteConversation.isPending) return;
          setPendingDeleteDM(null);
          deleteConversation.reset();
        }}
        onConfirm={() => {
          if (!pendingDeleteDM?.id) return;
          const deletedPath = pendingDeleteDM.to;
          deleteConversation.mutate(pendingDeleteDM.id, {
            onSuccess: () => {
              if (location.pathname === deletedPath) {
                navigate(`${orgBase}/dms`);
              }
            },
            onSettled: () => setPendingDeleteDM(null),
          });
        }}
      />
    </>
  );
}

// ============================================================================
// SidebarTop — org switcher + ⌘K search (v2.8.1 #278 "Topbar→sidebar").
// Both moved out of the old header. The org switcher reuses <OrgDropdown>
// unchanged; the search input is the ⌘K command-palette trigger.
// ============================================================================
function SidebarTop({
  collapsed,
  onOpenPalette,
  orgSwitcher,
}: {
  collapsed: boolean;
  onOpenPalette: () => void;
  orgSwitcher: OrgSwitcherBinding;
}): React.ReactElement {
  const { currentOrg, orgs, currentSlug, fallbackName, open, onToggle, onClose, onCreateOrg, onOpenSettings } =
    orgSwitcher;
  const orgName = currentOrg?.name ?? fallbackName ?? '…';
  return (
    <div className="flex-shrink-0">
      {/* Org switcher. Icon-only when collapsed; logo + name + caret otherwise. */}
      <div className="relative">
        <button
          type="button"
          data-testid="org-switcher"
          onClick={onToggle}
          aria-expanded={open}
          aria-haspopup="true"
          title={collapsed ? orgName : undefined}
          className={[
            'flex w-full items-center rounded-md border border-border-base bg-bg-elevated text-sm text-text-primary hover:bg-bg-subtle motion-safe:transition-colors',
            collapsed ? 'justify-center p-2' : 'gap-2 px-2 py-1.5',
          ].join(' ')}
        >
          <span
            aria-hidden="true"
            className="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded bg-brand-hover text-white"
          >
            <OrgIcon />
          </span>
          {!collapsed && (
            <>
              <span className="min-w-0 flex-1 truncate text-left font-medium">{orgName}</span>
              <span aria-hidden="true" className="text-text-muted">
                <ChevronDownIcon />
              </span>
            </>
          )}
        </button>
        {open && (
          <OrgDropdown
            orgs={orgs}
            currentSlug={currentSlug}
            onClose={onClose}
            onCreateOrg={onCreateOrg}
            onOpenSettings={onOpenSettings}
          />
        )}
      </div>
      {/* ⌘K search trigger — opens the existing CommandPalette. */}
      {collapsed ? (
        <button
          type="button"
          onClick={onOpenPalette}
          aria-label="Search (⌘K)"
          data-testid="open-palette"
          title="Search (⌘K)"
          className="mt-2 inline-flex h-9 w-full items-center justify-center rounded-md border border-border-base bg-bg-elevated text-text-muted hover:bg-bg-subtle motion-safe:transition-colors"
        >
          <span aria-hidden="true" className="inline-flex h-4 w-4">
            <SearchIcon />
          </span>
        </button>
      ) : (
        <button
          type="button"
          onClick={onOpenPalette}
          aria-label="Search (⌘K)"
          data-testid="open-palette"
          className="mt-2 flex w-full items-center gap-2 rounded-md border border-border-base bg-bg-elevated px-2 py-1.5 text-sm text-text-muted hover:bg-bg-subtle motion-safe:transition-colors"
        >
          <span aria-hidden="true" className="inline-flex h-4 w-4">
            <SearchIcon />
          </span>
          <span className="flex-1 text-left">Search</span>
          <kbd className="rounded border border-border-base px-1 font-mono text-[0.6875rem] text-text-muted">
            ⌘K
          </kbd>
        </button>
      )}
    </div>
  );
}

// ============================================================================
// OrgDropdown + CreateOrgModal (v2.6 FE-3)
// ============================================================================

interface OrgDropdownProps {
  orgs: Array<{ id: string; slug: string; name: string }>;
  currentSlug?: string;
  onClose: () => void;
  onCreateOrg: () => void;
  onOpenSettings: (orgId: string) => void;
}

function OrgDropdown({ orgs, currentSlug, onClose, onCreateOrg, onOpenSettings }: OrgDropdownProps): React.ReactElement {
  const navigate = useNavigate();
  // Close on click-outside.
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest('[data-org-dropdown]')) onClose();
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [onClose]);

  const handleSwitch = (slug: string) => {
    if (slug !== currentSlug) {
      navigate(`/organizations/${slug}`);
    }
    onClose();
  };

  return (
    <div
      data-org-dropdown
      className="absolute left-0 top-full z-50 mt-1 w-48 rounded-md border border-border bg-bg-elevated shadow-[var(--shadow-2)]"
      role="menu"
    >
      {/* v2.7 #186-6: each org row carries its own settings gear (opens a
          per-org modal); the standalone "Organization Settings" entry is gone. */}
      {orgs.map((o) => (
        <div
          key={o.id}
          className={`flex w-full items-center ${o.slug === currentSlug ? 'bg-bg-subtle' : ''}`}
        >
          <button
            type="button"
            role="menuitem"
            className={`flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-sm hover:bg-bg-subtle ${
              o.slug === currentSlug ? 'font-medium text-brand' : 'text-text-primary'
            }`}
            onClick={() => handleSwitch(o.slug)}
          >
            <OrgIcon />
            <span className="truncate">{o.name}</span>
          </button>
          <button
            type="button"
            data-testid="org-settings-gear"
            data-org-id={o.id}
            aria-label={`Settings for ${o.name}`}
            title="Organization settings"
            onClick={() => onOpenSettings(o.id)}
            className="mr-1 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded text-text-muted hover:bg-border hover:text-text-primary"
          >
            <GearIcon />
          </button>
        </div>
      ))}
      {orgs.length > 0 && <hr className="border-border" />}
      <button
        type="button"
        role="menuitem"
        onClick={onCreateOrg}
        className="flex w-full items-center gap-2 px-3 py-2 text-sm text-accent hover:bg-bg-subtle"
      >
        <span aria-hidden="true">+</span>
        <span>Create organization</span>
      </button>
    </div>
  );
}

function validateSlugLocal(v: string): string {
  if (v.length < 3) return 'Slug must be at least 3 characters';
  if (v.length > 40) return 'Slug must be at most 40 characters';
  if (!/^[a-z0-9-]+$/.test(v)) return 'Slug may only contain [a-z0-9-]';
  if (/^-|-$/.test(v)) return 'Slug cannot start or end with a hyphen';
  return '';
}

function CreateOrgModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [error, setError] = useState('');

  const autoSlug = (n: string) =>
    n.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40);

  const create = useMutation({
    mutationFn: () => orgApi.create({ name: name.trim(), slug }),
    onSuccess: (newOrg) => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      onClose();
      // Redirect to the newly-created org (FE-3 acceptance: auto-redirect on create).
      navigate(`/organizations/${newOrg.slug}`);
    },
    onError: (err: Error) => {
      setError(err.message);
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    const slugErr = validateSlugLocal(slug);
    if (slugErr) { setError(slugErr); return; }
    if (!name.trim()) { setError('Please enter an organization name'); return; }
    create.mutate();
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-org-title"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-sm rounded-xl bg-bg-elevated border border-border p-6 shadow-[var(--shadow-3)]">
        <h2 id="create-org-title" className="text-base font-semibold text-text-primary mb-4">
          Create organization
        </h2>
        {error && (
          <div role="alert" className="mb-3 rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">
            {error}
          </div>
        )}
        <form onSubmit={handleSubmit} noValidate className="space-y-3">
          <div className="space-y-1">
            <label htmlFor="new-org-name" className="block text-sm text-text-primary">Organization name</label>
            <input
              id="new-org-name"
              type="text"
              value={name}
              onChange={(e) => {
                setName(e.target.value);
                if (!slug || slug === autoSlug(name)) setSlug(autoSlug(e.target.value));
              }}
              className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="My Organization"
            />
          </div>
          <div className="space-y-1">
            <label htmlFor="new-org-slug" className="block text-sm text-text-primary">Slug</label>
            <input
              id="new-org-slug"
              type="text"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              className="w-full rounded border border-border px-3 py-1.5 text-sm font-mono outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary"
              placeholder="my-org"
            />
          </div>
          <div className="flex gap-2 justify-end pt-1">
            <button type="button" onClick={onClose} className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle">Cancel</button>
            <button
              type="submit"
              disabled={create.isPending}
              className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
            >
              {create.isPending ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ============================================================================
// Inline Heroicons-style outline SVGs (skill rule `no-emoji-icons` +
// `icon-style-consistent`). Single stroke-width, 20×20 viewbox, current
// color. Inlining avoids pulling a whole icon library for ~7 glyphs.
// ============================================================================

// v2.7.1 #253: a clean single chevron (was a rectangle + inner divider + arrow).
// Points right "›" when collapsed (expand), left "‹" when expanded (collapse).
function SidebarToggleIcon({ collapsed }: { collapsed: boolean }): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d={collapsed ? 'M8 5l5 5-5 5' : 'M12 5l-5 5 5 5'} strokeLinecap="round" strokeLinejoin="round" />
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
// v2.8.1 #278: search (magnifier) + caret-down for the sidebar org switcher.
function SearchIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="8.5" cy="8.5" r="5" />
      <path d="M12.5 12.5 17 17" strokeLinecap="round" />
    </svg>
  );
}
function ChevronDownIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="m5 7.5 5 5 5-5" strokeLinecap="round" strokeLinejoin="round" />
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
// v2.8 #258: Issues nav (circle-dot) + Tasks nav (checklist) — inline single-stroke SVG, no-emoji.
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
// SettingsIcon — horizontal sliders / adjustments (Heroicons adjustments-horizontal).
// v2.8.1 @oopslink: in the collapsed (icon-only) sidebar the Settings glyph and the
// theme toggle were both "sun-shaped" (the old settings icon was a circle + radiating
// spokes; even a cog's radial teeth read as sun-rays at this size) → indistinguishable.
// A non-radial sliders glyph is the standard "settings/preferences" mark and is
// shape-distinct from the theme sun/moon at a glance. (org-switcher keeps its gear —
// different context, not adjacent to the theme control.)
function SettingsIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M10.5 6h9.75M10.5 6a1.5 1.5 0 1 1-3 0m3 0a1.5 1.5 0 1 0-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-9.75 0h9.75" />
    </svg>
  );
}
// GearIcon — a true cog (Heroicons cog-6-tooth). Used only by the org-switcher's
// per-org settings button, which lives in the switcher dropdown — NOT in the
// collapsed sidebar strip beside the theme toggle — so the cog reads as settings
// there without the sun-confusion that motivated the nav's sliders glyph.
function GearIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.324.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.24-.438.613-.431.992a6.759 6.759 0 0 1 0 .255c-.007.378.138.75.43.99l1.005.828c.424.35.534.954.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.57 6.57 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.28c-.09.543-.56.941-1.11.941h-2.594c-.55 0-1.02-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.992a6.932 6.932 0 0 1 0-.255c.007-.378-.138-.75-.43-.99l-1.004-.828a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.087.22-.128.332-.183.582-.495.644-.869l.214-1.281z" />
      <circle cx="12" cy="12" r="3" />
    </svg>
  );
}
function UsersIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="7.5" cy="7" r="2.5" />
      <path d="M2 16c0-3 2.5-5 5.5-5s5.5 2 5.5 5" strokeLinecap="round" />
      <path d="M13 8.5a2 2 0 1 0 0-4M18 16c0-2.5-2-4-4-4" strokeLinecap="round" />
    </svg>
  );
}
function TrashIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4.5 6h11M8 6V4.5h4V6M7 8.5l.5 7h5l.5-7" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function OrgIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3" y="10" width="5" height="7" rx="1" />
      <rect x="7.5" y="3" width="5" height="14" rx="1" />
      <rect x="12" y="7" width="5" height="10" rx="1" />
    </svg>
  );
}

// v2.8.1 #278: the sidebar bottom area (pinned). Top→bottom:
//   live status • Light/Dark segmented toggle • user • Sign out.
function SidebarFooter({
  collapsed,
  theme,
  onSetTheme,
  displayName,
  orgBase,
  onSignout,
}: {
  collapsed: boolean;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  displayName?: string;
  orgBase: string;
  onSignout: () => void;
}): React.ReactElement {
  return (
    <div className="mt-2 flex flex-col gap-1 border-t border-border-base pt-2">
      {/* Live status — reuse the SSE indicator (green dot + "live" text, so
          state is conveyed not-by-color-alone). Hidden when collapsed. */}
      {!collapsed && (
        <div data-testid="sidebar-live" className="px-2 py-1">
          <SSEIndicator />
        </div>
      )}
      {/* Segmented Light / Dark theme control. Replaces the old header icon
          button; wired to theme.ts via onSetTheme. ⌘D still flips it. */}
      <ThemeSegmented collapsed={collapsed} theme={theme} onSetTheme={onSetTheme} />
      {/* Signed-in user. Reuses /api/auth/me display_name as the identity. */}
      {displayName && (
        <NavLink
          to={`${orgBase}/me`}
          title={collapsed ? displayName : undefined}
          className={({ isActive }) =>
            [
              'flex items-center rounded px-2 py-1.5 text-sm',
              collapsed ? 'justify-center' : 'gap-2',
              isActive
                ? 'bg-brand-hover text-white'
                : 'text-text-secondary hover:bg-bg-subtle hover:text-text-primary',
            ].join(' ')
          }
          data-testid="sidebar-user"
        >
          <span
            aria-hidden="true"
            className="inline-flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full bg-bg-elevated text-xs font-medium text-text-secondary"
          >
            {displayName.slice(0, 1).toUpperCase()}
          </span>
          {!collapsed && <span className="truncate">{displayName}</span>}
        </NavLink>
      )}
      {/* Sign out — reuses useSignout() (clears query cache → /signin). */}
      <button
        type="button"
        onClick={onSignout}
        title={collapsed ? 'Sign out' : undefined}
        data-testid="sidebar-signout"
        className={[
          'flex w-full items-center rounded px-2 py-1.5 text-sm text-text-muted hover:bg-bg-subtle hover:text-danger motion-safe:transition-colors',
          collapsed ? 'justify-center' : 'gap-2',
        ].join(' ')}
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4">
          <SignoutIcon />
        </span>
        {!collapsed && <span>Sign out</span>}
      </button>
    </div>
  );
}

// ThemeSegmented — segmented Light | Dark control (v2.8.1 #278). Replaces
// the old header icon toggle. Implemented as an ARIA radiogroup: each
// segment is role="radio" with aria-checked; the selected one carries a
// filled (not color-only) highlight. Arrow keys + Enter/Space switch the
// theme; the whole group is reachable with a single Tab stop. Wired to
// theme.ts via onSetTheme (writeTheme persists + applies html.dark).
function ThemeSegmented({
  collapsed,
  theme,
  onSetTheme,
}: {
  collapsed: boolean;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
}): React.ReactElement {
  const options: ReadonlyArray<{ value: Theme; label: string; Icon: () => React.ReactElement }> = [
    { value: 'light', label: 'Light', Icon: SunIcon },
    { value: 'dark', label: 'Dark', Icon: MoonIcon },
  ];

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault();
      onSetTheme('dark');
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault();
      onSetTheme('light');
    }
  };

  // Collapsed strip: a single icon button that toggles (keeps the affordance
  // small but still keyboard/aria accessible). data-testid kept stable.
  if (collapsed) {
    const next: Theme = theme === 'dark' ? 'light' : 'dark';
    return (
      <button
        type="button"
        data-testid="theme-toggle"
        aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
        title="Toggle theme (⌘D)"
        onClick={() => onSetTheme(next)}
        className="inline-flex h-9 w-full items-center justify-center rounded-md text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4">
          {theme === 'dark' ? <SunIcon /> : <MoonIcon />}
        </span>
      </button>
    );
  }

  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      data-testid="theme-toggle"
      onKeyDown={onKeyDown}
      className="flex gap-1 rounded-md border border-border-base bg-bg-elevated p-0.5"
    >
      {options.map((opt) => {
        const selected = theme === opt.value;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={`${opt.label} theme`}
            data-testid={`theme-segment-${opt.value}`}
            tabIndex={selected ? 0 : -1}
            onClick={() => onSetTheme(opt.value)}
            className={[
              'flex flex-1 items-center justify-center gap-1.5 rounded px-2 py-1 text-xs font-medium motion-safe:transition-colors',
              selected
                ? 'bg-brand-hover text-white shadow-sm'
                : 'text-text-secondary hover:text-text-primary',
            ].join(' ')}
          >
            <span aria-hidden="true" className="inline-flex h-3.5 w-3.5">
              <opt.Icon />
            </span>
            <span>{opt.label}</span>
          </button>
        );
      })}
    </div>
  );
}
function SignoutIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M12.5 6.5V4.5A1.5 1.5 0 0 0 11 3H5A1.5 1.5 0 0 0 3.5 4.5v11A1.5 1.5 0 0 0 5 17h6a1.5 1.5 0 0 0 1.5-1.5v-2" strokeLinecap="round" />
      <path d="M9 10h8M15 7.5l2.5 2.5-2.5 2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
