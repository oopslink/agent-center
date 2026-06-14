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
import { useContextPanelController } from '@/shell/contextPanel';
import { useOptionalOrgContext, orgPath } from './OrgContext';

// ============================================================================
// AppLayout v5 — v2.10.0 [T1] three-column (on-demand four-column) desktop
// shell. The single left sidebar (v4 / #278) is split into a top-level module
// rail + a per-module secondary nav:
//
//   col① rail   — fixed dark activity bar: org logo (switcher) + the four
//                 top-level modules (Workspace / Conversations / Members /
//                 System) + bottom search (⌘K) and signed-in user (→ /me).
//   col② nav    — the active module's second-level navigation. Swaps when you
//                 switch modules in the rail. Hosts live status + Light/Dark +
//                 Sign out at the bottom. Collapsible (⌘B) → two-column.
//   col③ content— the router <Outlet> (the selected item's content).
//   col④ context— optional, view-specific panel (participants / metadata /
//                 plan conversation). Pages fill it via <ContextPanel>; absent
//                 → the layout is three columns. (see shell/contextPanel.tsx)
//
// The IA is unchanged (existing org-scoped routes); Overview/Home is removed
// (the org index redirects into the Workspace module). Visuals use the
// existing design tokens; the rail is a FIXED dark chrome surface (rail-* token).
//   - desktop col② collapses to icon-rail-only (persisted)
//   - segmented Light/Dark control at the col② bottom (persisted; applied
//     pre-React in main.tsx so there is no FOUC)
//   - keyboard shortcuts: ⌘K palette, ⌘B col② collapse, ⌘D theme,
//     ⌘1..4 jump to the four modules
//   - <CommandPalette> mounts at root so ⌘K works from anywhere
// ============================================================================

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

// ============================================================================
// IA — the four top-level modules (col①) and their second-level nav (col②).
// ============================================================================
interface NavItem {
  to: string;
  label: string;
  end?: boolean; // react-router NavLink end-match
  Icon: () => React.ReactElement;
}

// v2.8 #264 P1 / #176: an expandable col② sub-item (a channel/DM/project).
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

interface ModuleDef {
  id: 'workspace' | 'conversations' | 'members' | 'system';
  label: string;
  short: string;
  Icon: () => React.ReactElement;
  /** Route (relative to the org base) the rail icon navigates to. */
  defaultPath: string;
  /** First path segments that belong to this module (for active detection). */
  match: ReadonlyArray<string>;
  /** The module's col② nav items. */
  items: ReadonlyArray<NavItem>;
}

// v2.6-FE-6: nav targets are org-slug-prefixed (base = '' in isolated tests).
function buildModules(base: string): ReadonlyArray<ModuleDef> {
  const p = (path: string) => `${base}/${path}`;
  return [
    {
      id: 'workspace',
      label: 'Workspace',
      short: 'Work',
      Icon: WorkspaceIcon,
      defaultPath: 'projects',
      match: ['projects', 'issues', 'tasks', 'plans'],
      items: [
        { to: p('projects'), label: 'Projects', Icon: FolderIcon },
        // v2.8 #258: org-scope cross-project aggregation, Project 同级.
        { to: p('issues'), label: 'Issues', Icon: IssueIcon },
        { to: p('tasks'), label: 'Tasks', Icon: TaskIcon },
      ],
    },
    {
      id: 'conversations',
      label: 'Conversations',
      short: 'Chat',
      Icon: ChatIcon,
      defaultPath: 'channels',
      match: ['channels', 'dms'],
      items: [
        { to: p('channels'), label: 'Channels', Icon: HashIcon },
        { to: p('dms'), label: 'DMs', Icon: ChatIcon },
      ],
    },
    {
      // v2.7 #166: the org people module is "Members" (Humans + Agents).
      id: 'members',
      label: 'Members',
      short: 'Members',
      Icon: UsersIcon,
      defaultPath: 'members/humans',
      match: ['members', 'agents', 'users'],
      items: [
        { to: p('members/humans'), label: 'Humans', Icon: UsersIcon },
        // dev2/v281: "Agents" opens the enhanced canonical /agents list.
        { to: p('agents'), label: 'Agents', Icon: AgentsIcon },
      ],
    },
    {
      // v2.7 #164: Fleet merged into Environment (one operational page).
      id: 'system',
      label: 'System',
      short: 'System',
      Icon: SettingsIcon,
      defaultPath: 'environment',
      match: ['environment', 'settings', 'secrets', 'workers', 'fleet'],
      items: [
        { to: p('environment'), label: 'Environment', Icon: FleetIcon },
        { to: p('settings'), label: 'Settings', Icon: SettingsIcon },
      ],
    },
  ];
}

// Which module owns the current path? `rest` is the path minus the org base,
// e.g. "/channels/alpha" → first segment "channels" → conversations module.
// /me and unknown paths return undefined (no module highlighted).
function moduleForPath(
  modules: ReadonlyArray<ModuleDef>,
  pathname: string,
  base: string,
): ModuleDef | undefined {
  const rest = base && pathname.startsWith(base) ? pathname.slice(base.length) : pathname;
  const seg = rest.split('/').filter(Boolean)[0] ?? '';
  return modules.find((m) => m.match.includes(seg));
}

export default function AppLayout(): React.ReactElement {
  useSSE();
  const me = useMe();
  // v2.7 #155: wire the store's currentUserId to the AUTHENTICATED identity ref.
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
  const orgBase = orgCtx ? `/organizations/${orgCtx.slug}` : '';

  const [orgDropdownOpen, setOrgDropdownOpen] = useState(false);
  const [createOrgModalOpen, setCreateOrgModalOpen] = useState(false);
  const [settingsOrgId, setSettingsOrgId] = useState<string | null>(null);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(readSidebarCollapsed);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();

  // col④ on-demand context panel host + open flag (see shell/contextPanel.tsx).
  const { Provider: ContextPanelProvider, value: panelValue, setHost, open: panelOpen } =
    useContextPanelController();

  const modules = useMemo(() => buildModules(orgBase), [orgBase]);
  const activeModule = moduleForPath(modules, location.pathname, orgBase);

  // Auto-close the drawer on navigation so a tap on a nav item also
  // dismisses the overlay (common mobile pattern).
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

  // Persist collapse + theme on change.
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

  // Cmd/Ctrl shortcuts. ⌘1..4 jump to the four modules' default pages
  // (org-scoped so they resolve under /organizations/{slug}/…).
  const shortcuts = useMemo(
    () => ({
      'mod+k': () => setPaletteOpen((v) => !v),
      'mod+b': () => setCollapsed((v) => !v),
      'mod+d': () => setTheme((t) => (t === 'dark' ? 'light' : 'dark')),
      'mod+1': () => navigate(orgPath('/projects', orgCtx?.slug)),
      'mod+2': () => navigate(orgPath('/channels', orgCtx?.slug)),
      'mod+3': () => navigate(orgPath('/members/humans', orgCtx?.slug)),
      'mod+4': () => navigate(orgPath('/environment', orgCtx?.slug)),
    }),
    [navigate, orgCtx?.slug],
  );
  useKeyShortcuts(shortcuts);

  const orgSwitcher: OrgSwitcherBinding = {
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
    <ContextPanelProvider value={panelValue}>
      <div className="flex h-screen bg-bg-base">
        {/* Mobile-only top strip: the rail + col② are desktop columns, so on
            small screens we keep a slim bar hosting the hamburger (drawer) +
            the active module / org name for context. */}
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
            {activeModule?.label ?? currentOrg?.name ?? me.data?.display_name ?? '…'}
          </span>
        </header>

        {/* col① — the module rail (desktop). */}
        <ModuleRail
          modules={modules}
          activeModuleId={activeModule?.id}
          orgBase={orgBase}
          orgSwitcher={orgSwitcher}
          displayName={me.data?.display_name}
          onOpenPalette={() => setPaletteOpen(true)}
        />

        {/* col② — the active module's secondary nav (desktop). */}
        <SecondaryNav
          module={activeModule}
          collapsed={collapsed}
          theme={theme}
          onSetTheme={setTheme}
          onToggleCollapsed={() => setCollapsed((v) => !v)}
          orgBase={orgBase}
          onOpenPalette={() => setPaletteOpen(true)}
        />

        {/* col③ — content. */}
        <main className="flex min-w-0 flex-1 overflow-hidden pt-12 md:pt-0">
          <div
            className="flex h-full w-full flex-col overflow-y-auto p-4 sm:p-6"
            data-testid="app-content-shell"
          >
            <Suspense fallback={<PageSkeleton />}>
              <Outlet />
            </Suspense>
          </div>
        </main>

        {/* col④ — on-demand context panel. The host element always exists so
            <ContextPanel> can portal into it; the column is revealed only when
            a panel is mounted (panelOpen). */}
        <aside
          aria-label="context"
          data-testid="context-panel"
          data-open={panelOpen}
          className={[
            'w-64 flex-shrink-0 flex-col overflow-y-auto border-l border-border-base bg-bg-elevated',
            panelOpen ? 'hidden md:flex' : 'hidden',
          ].join(' ')}
        >
          <div ref={setHost} className="flex min-h-0 flex-1 flex-col" />
        </aside>

        {/* Mobile drawer — rail modules + active module nav. */}
        {drawerOpen && (
          <MobileDrawer
            modules={modules}
            activeModuleId={activeModule?.id}
            module={activeModule}
            theme={theme}
            onSetTheme={setTheme}
            orgBase={orgBase}
            orgSwitcher={orgSwitcher}
            displayName={me.data?.display_name}
            onOpenPalette={() => setPaletteOpen(true)}
            onDismiss={() => setDrawerOpen(false)}
          />
        )}

        {/* Org create / settings modals overlay the whole app from the root. */}
        {createOrgModalOpen && <CreateOrgModal onClose={() => setCreateOrgModalOpen(false)} />}
        {settingsOrgId && (
          <OrgSettingsModal orgId={settingsOrgId} onClose={() => setSettingsOrgId(null)} />
        )}
        <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
        <WorkerEnrolledToast />
      </div>
    </ContextPanelProvider>
  );
}

// ============================================================================
// col① — ModuleRail. Fixed dark activity bar: org logo (switcher) at the top,
// the four modules in the middle, search + user pinned at the bottom.
// ============================================================================
function ModuleRail({
  modules,
  activeModuleId,
  orgBase,
  orgSwitcher,
  displayName,
  onOpenPalette,
}: {
  modules: ReadonlyArray<ModuleDef>;
  activeModuleId?: ModuleDef['id'];
  orgBase: string;
  orgSwitcher: OrgSwitcherBinding;
  displayName?: string;
  onOpenPalette: () => void;
}): React.ReactElement {
  const orgName = orgSwitcher.currentOrg?.name ?? orgSwitcher.fallbackName ?? 'Organization';
  return (
    <nav
      aria-label="modules"
      className="hidden w-16 flex-shrink-0 flex-col items-center gap-1 bg-rail-bg py-2.5 md:flex"
    >
      {/* Org logo = the org switcher trigger (opens the dropdown). */}
      <div className="relative mb-2">
        <button
          type="button"
          data-testid="org-switcher"
          onClick={orgSwitcher.onToggle}
          aria-expanded={orgSwitcher.open}
          aria-haspopup="true"
          title={orgName}
          className="inline-flex h-9 w-9 items-center justify-center rounded-lg bg-brand text-sm font-extrabold text-white hover:bg-brand-hover focus-visible:ring-2 focus-visible:ring-white motion-safe:transition-colors"
        >
          <span aria-hidden="true">{(orgName[0] ?? 'A').toUpperCase()}</span>
          <span className="sr-only">{orgName} — switch organization</span>
        </button>
        {orgSwitcher.open && (
          <OrgDropdown
            orgs={orgSwitcher.orgs}
            currentSlug={orgSwitcher.currentSlug}
            onClose={orgSwitcher.onClose}
            onCreateOrg={orgSwitcher.onCreateOrg}
            onOpenSettings={orgSwitcher.onOpenSettings}
          />
        )}
      </div>

      {modules.map((m) => {
        const active = m.id === activeModuleId;
        return (
          <NavLink
            key={m.id}
            to={m.defaultPath ? `${orgBase}/${m.defaultPath}` : orgBase || '/'}
            title={m.label}
            aria-label={m.label}
            aria-current={active ? 'page' : undefined}
            data-testid={`rail-module-${m.id}`}
            data-active={active}
            className={[
              'flex h-12 w-12 flex-col items-center justify-center gap-0.5 rounded-xl text-rail-fg motion-safe:transition-colors',
              active ? 'bg-white/15 text-rail-fg-active' : 'hover:bg-white/10 hover:text-rail-fg-active',
            ].join(' ')}
          >
            <span aria-hidden="true" className="inline-flex h-5 w-5">
              <m.Icon />
            </span>
            <span aria-hidden="true" className="text-[0.5rem] font-medium leading-none">
              {m.short}
            </span>
          </NavLink>
        );
      })}

      <div className="flex-1" />

      {/* Search (⌘K). */}
      <button
        type="button"
        onClick={onOpenPalette}
        aria-label="Search (⌘K)"
        title="Search (⌘K)"
        data-testid="open-palette"
        className="inline-flex h-10 w-10 items-center justify-center rounded-xl text-rail-fg hover:bg-white/10 hover:text-rail-fg-active motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-5 w-5">
          <SearchIcon />
        </span>
      </button>

      {/* Signed-in user → /me. */}
      {displayName && (
        <NavLink
          to={`${orgBase}/me`}
          title={displayName}
          aria-label={`${displayName} — your account`}
          data-testid="sidebar-user"
          className="mt-0.5 inline-flex h-9 w-9 items-center justify-center rounded-full bg-white/15 text-xs font-semibold text-rail-fg-active hover:bg-white/25 motion-safe:transition-colors"
        >
          <span aria-hidden="true">{displayName.slice(0, 1).toUpperCase()}</span>
        </NavLink>
      )}
    </nav>
  );
}

// ============================================================================
// col② — SecondaryNav. Header (module title + search) + the module's nav items
// (with the channel/DM/project expandable sub-lists) + footer (live + theme +
// sign out). Collapsible to nothing (⌘B / chevron) → two-column layout.
// ============================================================================
function SecondaryNav({
  module,
  collapsed,
  theme,
  onSetTheme,
  onToggleCollapsed,
  orgBase,
  onOpenPalette,
}: {
  module?: ModuleDef;
  collapsed: boolean;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  onToggleCollapsed: () => void;
  orgBase: string;
  onOpenPalette: () => void;
}): React.ReactElement {
  return (
    <nav
      aria-label="primary"
      data-collapsed={collapsed}
      className={[
        'group/sidebar relative w-64 flex-shrink-0 flex-col border-r border-border-base bg-bg-elevated',
        collapsed ? 'hidden' : 'hidden md:flex',
      ].join(' ')}
    >
      <SecondaryNavBody
        module={module}
        theme={theme}
        onSetTheme={onSetTheme}
        orgBase={orgBase}
        onOpenPalette={onOpenPalette}
      />
      {/* Collapse chevron embedded in the right edge (Slack/VSCode pattern). */}
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
  );
}

// SecondaryNavBody — shared by the desktop col② and the mobile drawer. Renders
// the module header + the live nav tree + the footer.
function SecondaryNavBody({
  module,
  theme,
  onSetTheme,
  orgBase,
  onOpenPalette,
}: {
  module?: ModuleDef;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  orgBase: string;
  onOpenPalette: () => void;
}): React.ReactElement {
  const signout = useSignout();
  const location = useLocation();
  const navigate = useNavigate();
  const deleteConversation = useDeleteConversation();
  const [pendingDeleteDM, setPendingDeleteDM] = useState<SidebarChild | null>(null);

  // v2.5.x #63 — per-group + per-expandable-item expand state. Default true.
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

  // Channel + DM + project lists for the expandable sub-items. Each list is
  // small + cached; the hooks are shared across modules so we always call them.
  const channels = useConversations({ kind: 'channel' });
  const dms = useConversations({ kind: 'dm' });
  const projects = useProjects();
  const channelChildren: SidebarChild[] = (channels.data ?? [])
    .filter((c) => c.status !== 'archived')
    .map((c) => ({
      to: `${orgBase}/channels/${encodeURIComponent(c.id)}`,
      label: `# ${c.name}`,
      unreadCount: c.unread_count,
      mentionCount: c.mention_count,
    }));
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
  const projectChildren: SidebarChild[] = (projects.data ?? []).map((p) => ({
    to: `${orgBase}/projects/${encodeURIComponent(p.id)}`,
    label: p.name || p.id,
  }));

  const childrenFor = (item: NavItem): SidebarChild[] | null =>
    item.to.endsWith('/channels')
      ? channelChildren
      : item.to.endsWith('/dms')
        ? dmChildren
        : item.to.endsWith('/projects')
          ? projectChildren
          : null;

  return (
    <>
      {/* Module header — title + ⌘K search trigger (the col② local search). */}
      <div className="flex-shrink-0 border-b border-border-base px-3.5 pb-2.5 pt-3">
        <h2 data-testid="module-title" className="text-base font-semibold text-text-primary">
          {module?.label ?? 'Workspace'}
        </h2>
        <button
          type="button"
          onClick={onOpenPalette}
          data-testid="secondary-search"
          aria-label="Search (⌘K)"
          className="mt-2.5 flex w-full items-center gap-2 rounded-lg border border-border-base bg-bg-subtle px-2.5 py-1.5 text-xs text-text-muted hover:bg-bg-base motion-safe:transition-colors"
        >
          <span aria-hidden="true" className="inline-flex h-3.5 w-3.5">
            <SearchIcon />
          </span>
          <span className="flex-1 text-left">Search…</span>
          <kbd className="rounded border border-border-base px-1 font-mono text-[0.625rem] text-text-muted">
            ⌘K
          </kbd>
        </button>
      </div>

      {/* Nav tree — the active module's section, expanded by default. */}
      <div className="flex-1 overflow-y-auto px-2 py-2">
        {module && (
          <NavGroup
            label={module.label}
            items={module.items}
            open={isGroupOpen(module.label)}
            onToggle={() => toggleGroup(module.label)}
            childrenFor={childrenFor}
            isSubItemOpen={isSubItemOpen}
            toggleSubItem={toggleSubItem}
            onRequestDeleteDM={(child) => {
              deleteConversation.reset();
              setPendingDeleteDM(child);
            }}
          />
        )}
      </div>

      {deleteConversation.isError && (
        <p
          className="px-3 pb-1 text-xs text-danger"
          data-testid="sidebar-dm-delete-error"
          role="alert"
        >
          {conversationDeleteErrorMessage(deleteConversation.error)}
        </p>
      )}

      <SecondaryNavFooter theme={theme} onSetTheme={onSetTheme} onSignout={() => signout.mutate()} />

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

// NavGroup — one module section: a CAPS collapse header + its items (each of
// which may expand into a channel/DM/project sub-list with unread badges).
function NavGroup({
  label,
  items,
  open,
  onToggle,
  childrenFor,
  isSubItemOpen,
  toggleSubItem,
  onRequestDeleteDM,
}: {
  label: string;
  items: ReadonlyArray<NavItem>;
  open: boolean;
  onToggle: () => void;
  childrenFor: (item: NavItem) => SidebarChild[] | null;
  isSubItemOpen: (to: string) => boolean;
  toggleSubItem: (to: string) => void;
  onRequestDeleteDM: (child: SidebarChild) => void;
}): React.ReactElement {
  return (
    <div>
      <h3 className="px-1">
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          data-testid={`sidebar-group-toggle-${label}`}
          className="flex w-full items-center justify-between rounded px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
        >
          <span data-testid="section-label">{label}</span>
          <span aria-hidden="true" className="text-text-muted">
            {open ? '⌄' : '›'}
          </span>
        </button>
      </h3>
      {open && (
        <ul className="space-y-0.5">
          {items.map((item) => {
            const subChildren = childrenFor(item);
            const subOpen = isSubItemOpen(item.to);
            return (
              <li key={item.to}>
                <div className="flex items-center gap-1">
                  <NavLink
                    to={item.to}
                    end={item.end}
                    className={({ isActive }) =>
                      [
                        'flex flex-1 items-center justify-between rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                        isActive
                          ? 'bg-brand-hover text-white'
                          : 'text-text-primary hover:bg-bg-subtle',
                      ].join(' ')
                    }
                  >
                    <span className="flex items-center gap-2">
                      <span aria-hidden="true" className="inline-flex h-4 w-4">
                        <item.Icon />
                      </span>
                      <span className="flex flex-1 items-center justify-between gap-1.5">
                        <span>{item.label}</span>
                        {subChildren && (
                          <span
                            data-testid={`count-badge-${item.label}`}
                            aria-label={`${subChildren.length} ${item.label.toLowerCase()}`}
                            className="rounded-full bg-bg-elevated px-1.5 text-[0.6875rem] text-text-muted tabular-nums"
                          >
                            {subChildren.length}
                          </span>
                        )}
                      </span>
                    </span>
                  </NavLink>
                  {subChildren && (
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
                {subChildren && subOpen && (
                  <ul
                    className="ml-6 mt-0.5 space-y-0.5 border-l border-border-base pl-2"
                    data-testid={`sidebar-subitem-list-${item.to}`}
                  >
                    {subChildren.length === 0 && (
                      <li className="px-2 py-0.5 text-xs italic text-text-muted">(none)</li>
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
                              onClick={() => onRequestDeleteDM(child)}
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
    </div>
  );
}

// SecondaryNavFooter — pinned col② bottom: live (SSE) + Light/Dark + Sign out.
function SecondaryNavFooter({
  theme,
  onSetTheme,
  onSignout,
}: {
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  onSignout: () => void;
}): React.ReactElement {
  return (
    <div className="flex flex-col gap-1 border-t border-border-base p-2">
      <div data-testid="sidebar-live" className="px-2 py-1">
        <SSEIndicator />
      </div>
      <ThemeSegmented theme={theme} onSetTheme={onSetTheme} />
      <button
        type="button"
        onClick={onSignout}
        data-testid="sidebar-signout"
        className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-text-muted hover:bg-bg-subtle hover:text-danger motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4">
          <SignoutIcon />
        </span>
        <span>Sign out</span>
      </button>
    </div>
  );
}

// ============================================================================
// MobileDrawer — small-screen overlay: module switcher row + the active
// module's secondary nav. (Mobile is a baseline in T1; refined in a later
// phase per the mockup note.)
// ============================================================================
function MobileDrawer({
  modules,
  activeModuleId,
  module,
  theme,
  onSetTheme,
  orgBase,
  orgSwitcher,
  displayName,
  onOpenPalette,
  onDismiss,
}: {
  modules: ReadonlyArray<ModuleDef>;
  activeModuleId?: ModuleDef['id'];
  module?: ModuleDef;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  orgBase: string;
  orgSwitcher: OrgSwitcherBinding;
  displayName?: string;
  onOpenPalette: () => void;
  onDismiss: () => void;
}): React.ReactElement {
  const orgName = orgSwitcher.currentOrg?.name ?? orgSwitcher.fallbackName ?? 'Organization';
  return (
    <div className="fixed inset-0 z-40 flex md:hidden" role="dialog" aria-modal="true">
      <button
        type="button"
        aria-label="Close navigation overlay"
        onClick={onDismiss}
        className="flex-1 bg-black/40 motion-safe:transition-opacity"
      />
      <div className="flex w-72 max-w-[85%] flex-shrink-0">
        {/* Mini rail. */}
        <nav
          aria-label="modules mobile"
          className="flex w-16 flex-shrink-0 flex-col items-center gap-1 bg-rail-bg py-2.5"
        >
          <span
            aria-hidden="true"
            className="mb-2 inline-flex h-9 w-9 items-center justify-center rounded-lg bg-brand text-sm font-extrabold text-white"
          >
            {(orgName[0] ?? 'A').toUpperCase()}
          </span>
          {modules.map((m) => {
            const active = m.id === activeModuleId;
            return (
              <NavLink
                key={m.id}
                to={m.defaultPath ? `${orgBase}/${m.defaultPath}` : orgBase || '/'}
                aria-label={m.label}
                data-testid={`drawer-module-${m.id}`}
                className={[
                  'flex h-12 w-12 flex-col items-center justify-center gap-0.5 rounded-xl text-rail-fg',
                  active ? 'bg-white/15 text-rail-fg-active' : 'hover:bg-white/10',
                ].join(' ')}
              >
                <span aria-hidden="true" className="inline-flex h-5 w-5">
                  <m.Icon />
                </span>
                <span aria-hidden="true" className="text-[0.5rem] font-medium leading-none">
                  {m.short}
                </span>
              </NavLink>
            );
          })}
          <div className="flex-1" />
          {displayName && (
            <NavLink
              to={`${orgBase}/me`}
              aria-label={`${displayName} — your account`}
              data-testid="drawer-user"
              className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-white/15 text-xs font-semibold text-rail-fg-active"
            >
              <span aria-hidden="true">{displayName.slice(0, 1).toUpperCase()}</span>
            </NavLink>
          )}
        </nav>
        {/* The module's secondary nav. */}
        <nav
          aria-label="primary mobile"
          className="flex w-56 flex-shrink-0 flex-col border-l border-border-base bg-bg-elevated"
          onClick={(e) => e.stopPropagation()}
        >
          <SecondaryNavBody
            module={module}
            theme={theme}
            onSetTheme={onSetTheme}
            orgBase={orgBase}
            onOpenPalette={onOpenPalette}
          />
        </nav>
      </div>
    </div>
  );
}

// ============================================================================
// OrgSwitcher binding + OrgDropdown + CreateOrgModal (carried over from v4).
// ============================================================================
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

// ThemeSegmented — segmented Light | Dark control (v2.8.1 #278). An ARIA
// radiogroup; each segment is role="radio" with aria-checked; arrow keys +
// Enter/Space switch the theme. Wired to theme.ts via onSetTheme.
function ThemeSegmented({
  theme,
  onSetTheme,
}: {
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

// ============================================================================
// Inline Heroicons-style outline SVGs (skill rules `no-emoji-icons` +
// `icon-style-consistent`). Single stroke-width, current color.
// ============================================================================
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
// WorkspaceIcon — four-square grid (the rail "Work" module glyph).
function WorkspaceIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3" y="3" width="6" height="6" rx="1.5" />
      <rect x="11" y="3" width="6" height="6" rx="1.5" />
      <rect x="3" y="11" width="6" height="6" rx="1.5" />
      <rect x="11" y="11" width="6" height="6" rx="1.5" />
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
function SearchIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="8.5" cy="8.5" r="5" />
      <path d="M12.5 12.5 17 17" strokeLinecap="round" />
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
// v2.8 #258: Issues nav (circle-dot) + Tasks nav (checklist).
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
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true">
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
// SettingsIcon — horizontal sliders / adjustments (the rail "System" module glyph
// + the Settings nav item). Non-radial so it stays shape-distinct from the
// theme sun/moon (v2.8.1 @oopslink).
function SettingsIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 24 24" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M10.5 6h9.75M10.5 6a1.5 1.5 0 1 1-3 0m3 0a1.5 1.5 0 1 0-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-9.75 0h9.75" />
    </svg>
  );
}
// GearIcon — a true cog (Heroicons cog-6-tooth). Used only by the org-switcher's
// per-org settings button inside the dropdown.
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
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true">
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
function SignoutIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M12.5 6.5V4.5A1.5 1.5 0 0 0 11 3H5A1.5 1.5 0 0 0 3.5 4.5v11A1.5 1.5 0 0 0 5 17h6a1.5 1.5 0 0 0 1.5-1.5v-2" strokeLinecap="round" />
      <path d="M9 10h8M15 7.5l2.5 2.5-2.5 2.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
