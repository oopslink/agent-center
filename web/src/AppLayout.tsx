import type React from 'react';
import { Suspense, useEffect, useMemo, useState } from 'react';
import { NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useSSE } from '@/sse/useSSE';
import {
  conversationDeleteErrorMessage,
  useConversations,
  useDeleteConversation,
  useUnreadConversations,
} from '@/api/conversations';
import { useProjects } from '@/api/projects';
import { identityRefOf } from '@/api/members';
import { useAppStore, type SSEStatus } from '@/store/app';
import { useModalA11y } from '@/components/useModalA11y';
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
import { ResizeHandle } from '@/components/ResizeHandle';
import { useResizablePanel } from '@/components/useResizablePanel';
import { SECONDARY_NAV_REGISTRY } from '@/shell/secondaryNav';
import { MobileTabBar } from '@/shell/MobileTabBar';
import { BottomSheet } from '@/shell/BottomSheet';
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

// v2.10.2 [T128] col④ context-panel width (desktop). The whole sidebar COLUMN is
// draggable from its left edge — the resize moves the container, not the inner
// content (the bug T128 fixed). Default = the prior w-64 (256px); floor keeps the
// panel usable; ceiling = 3/4 of the viewport (75vw) so it can't swallow col③.
// Persisted + capped via the same useResizablePanel as the ThreadSidebar.
const CONTEXT_PANEL_WIDTH_KEY = 'ac.contextpanel.width';
const CONTEXT_PANEL_DEFAULT_WIDTH = 256;
const CONTEXT_PANEL_MIN_WIDTH = 240;
const contextPanelMaxWidth = (): number =>
  (typeof window === 'undefined' ? 1024 : window.innerWidth) * 0.75;

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
  id: 'workspace' | 'conversations' | 'members' | 'reminders' | 'system';
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
        // v2.10.0 [T6]: global cross-project Plan list, Tasks 平级. v2.10.2 [T142]:
        // label pluralized to "Plans" for consistency with Projects/Issues/Tasks.
        { to: p('plans'), label: 'Plans', Icon: PlanIcon },
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
      // T207 [提醒]: Reminders (Cognition BC). Promoted to a top-level module —
      // peer of Members, not nested under the Workspace col② (owner directive).
      id: 'reminders',
      label: 'Reminders',
      short: 'Reminders',
      Icon: ReminderIcon,
      defaultPath: 'reminders',
      match: ['reminders'],
      items: [{ to: p('reminders'), label: 'All reminders', Icon: ReminderIcon }],
    },
    {
      // v2.7 #164: Fleet merged into Environment (one operational page).
      id: 'system',
      label: 'System',
      short: 'System',
      Icon: SettingsIcon,
      defaultPath: 'environment',
      match: ['environment', 'settings', 'version', 'secrets', 'workers', 'fleet'],
      items: [
        { to: p('environment'), label: 'Environment', Icon: FleetIcon },
        { to: p('settings'), label: 'Settings', Icon: SettingsIcon },
        // I7-D3: Version hoisted out of Settings to its own System-level page.
        { to: p('version'), label: 'Version', Icon: InfoIcon },
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
  // v2.10.1 [M1] mobile (<768) overlays: the col④ context panel is a
  // dismissible bottom sheet (default closed, opened from the top-bar ⓘ), and
  // the org/account/theme/sign-out menu is a second bottom sheet. The desktop
  // layout (≥768) is unchanged — its real columns show instead.
  const [sheetOpen, setSheetOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  // Mobile (<768): the col② secondary nav has no column, so it reflows into a
  // bottom sheet opened from the top-bar title. Without this, tapping a module
  // (e.g. Workspace) on mobile only lands on its default page (Projects) with no
  // way to reach the module's other sections (Issues / Tasks / Plans).
  const [navSheetOpen, setNavSheetOpen] = useState(false);
  const [collapsed, setCollapsed] = useState<boolean>(readSidebarCollapsed);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const location = useLocation();
  const navigate = useNavigate();
  // Close the mobile nav sheet on any route change — tapping a section navigates,
  // so the sheet must get out of the way of the now-current screen.
  useEffect(() => {
    setNavSheetOpen(false);
  }, [location.pathname, location.search]);

  // col④ on-demand context panel host + open flag (see shell/contextPanel.tsx).
  const { Provider: ContextPanelProvider, value: panelValue, setHost, open: panelOpen, collapsed: panelCollapsed } =
    useContextPanelController();
  // T184: on desktop the sidebar can be FULLY collapsed (hidden), leaving a thin
  // expand rail. Collapse is desktop-only — on mobile col④ is a bottom sheet
  // (toggled by the top-bar ⓘ), so the collapsed flag is ignored there.
  const setPanelCollapsed = panelValue.setCollapsed;
  // T128: the col④ column itself is the resizable surface (desktop). Dragging the
  // left-edge grip widens/narrows the whole sidebar, not its inner content.
  const {
    width: ctxWidth,
    resizing: ctxResizing,
    handleProps: ctxHandleProps,
  } = useResizablePanel({
    storageKey: CONTEXT_PANEL_WIDTH_KEY,
    defaultWidth: CONTEXT_PANEL_DEFAULT_WIDTH,
    minWidth: CONTEXT_PANEL_MIN_WIDTH,
    maxWidth: contextPanelMaxWidth,
    edge: 'left',
  });

  const modules = useMemo(() => buildModules(orgBase), [orgBase]);
  const activeModule = moduleForPath(modules, location.pathname, orgBase);

  // Close the mobile context sheet + account menu on navigation (a tap that
  // routes also dismisses any open overlay — common mobile pattern).
  useEffect(() => {
    setSheetOpen(false);
    setAccountOpen(false);
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

  // Cmd/Ctrl shortcuts. ⌘1..5 jump to the five modules' default pages, in rail
  // order (org-scoped so they resolve under /organizations/{slug}/…). T207
  // inserted Reminders before System, shifting System from ⌘4 to ⌘5.
  const shortcuts = useMemo(
    () => ({
      'mod+k': () => setPaletteOpen((v) => !v),
      'mod+b': () => setCollapsed((v) => !v),
      'mod+d': () => setTheme((t) => (t === 'dark' ? 'light' : 'dark')),
      'mod+1': () => navigate(orgPath('/projects', orgCtx?.slug)),
      'mod+2': () => navigate(orgPath('/channels', orgCtx?.slug)),
      'mod+3': () => navigate(orgPath('/members/humans', orgCtx?.slug)),
      'mod+4': () => navigate(orgPath('/reminders', orgCtx?.slug)),
      'mod+5': () => navigate(orgPath('/environment', orgCtx?.slug)),
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
        {/* v2.10.1 [M1] Mobile-only top bar: the rail + col② are desktop
            columns, so on small screens we keep a slim bar hosting the active
            screen title + the actions that lived in the rail/col② (search,
            col④ context ⓘ, account/org/theme/sign-out). */}
        <MobileTopBar
          title={activeModule?.label ?? currentOrg?.name ?? me.data?.display_name ?? '…'}
          displayName={me.data?.display_name}
          hasContextPanel={panelOpen}
          sheetOpen={sheetOpen}
          hasNav={!!activeModule}
          navSheetOpen={navSheetOpen}
          onOpenNav={() => setNavSheetOpen(true)}
          onToggleSheet={() => setSheetOpen((v) => !v)}
          onOpenPalette={() => setPaletteOpen(true)}
          onOpenAccount={() => setAccountOpen(true)}
        />

        {/* col① — the module rail (desktop). v2.10.1 [T105]: it now owns the
            connection status + the user panel (theme/sign-out), both pinned at
            the bottom of the rail. */}
        <ModuleRail
          modules={modules}
          activeModuleId={activeModule?.id}
          orgBase={orgBase}
          orgSwitcher={orgSwitcher}
          displayName={me.data?.display_name}
          theme={theme}
          onSetTheme={setTheme}
          onOpenPalette={() => setPaletteOpen(true)}
        />

        {/* col② — the active module's secondary nav (desktop). */}
        <SecondaryNav
          module={activeModule}
          collapsed={collapsed}
          onToggleCollapsed={() => setCollapsed((v) => !v)}
          orgBase={orgBase}
          onOpenPalette={() => setPaletteOpen(true)}
        />

        {/* col③ — content. On mobile it is the full-screen surface between the
            fixed top bar (pt-12) and the fixed bottom tab bar (pb-14). */}
        <main className="flex min-w-0 flex-1 overflow-hidden pb-14 pt-12 md:pb-0 md:pt-0">
          <div
            className="flex h-full w-full flex-col overflow-y-auto p-4 sm:p-6"
            data-testid="app-content-shell"
          >
            <Suspense fallback={<PageSkeleton />}>
              <Outlet />
            </Suspense>
          </div>
        </main>

        {/* col④ — on-demand context panel. ONE host element (so <ContextPanel>
            always portals into the same node) presented responsively: a side
            COLUMN on desktop (≥768, shown whenever a panel is mounted) and a
            dismissible bottom SHEET on mobile (<768, shown only when the user
            opens it via the top-bar ⓘ → sheetOpen). The mobile scrim sits
            behind the sheet. */}
        {sheetOpen && panelOpen && (
          <button
            type="button"
            aria-label="Close details"
            tabIndex={-1}
            onClick={() => setSheetOpen(false)}
            data-testid="context-panel-scrim"
            className="fixed inset-0 z-30 bg-black/40 md:hidden"
          />
        )}
        <aside
          aria-label="context"
          data-testid="context-panel"
          data-open={panelOpen}
          data-sheet-open={sheetOpen}
          // T128: the desktop width is the user-dragged value (CSS var consumed by
          // md:w-[var(--ctx-w)]); on mobile the sheet stays full-width (the var is
          // unused there). Persisted + clamped to 75vw by useResizablePanel.
          style={{ ['--ctx-w' as string]: `${ctxWidth}px` }}
          className={[
            'flex-col overflow-y-auto bg-bg-elevated',
            // mobile: bottom sheet
            'fixed inset-x-0 bottom-0 z-40 max-h-[80vh] rounded-t-2xl border-t border-border-strong pb-[env(safe-area-inset-bottom)] shadow-2',
            // desktop: side column (relative so the resize grip anchors to it)
            'md:relative md:bottom-auto md:z-auto md:max-h-none md:w-[var(--ctx-w)] md:flex-shrink-0 md:rounded-none md:border-l md:border-t-0 md:border-border-base md:pb-0 md:shadow-none',
            // mobile visibility (sheetOpen) — desktop overrides via md:
            sheetOpen && panelOpen ? 'flex' : 'hidden',
            // T184: desktop hides the full column when collapsed (the expand rail
            // below takes over). Mobile ignores collapse (it's a bottom sheet).
            panelOpen && !panelCollapsed ? 'md:flex' : 'md:hidden',
          ].join(' ')}
        >
          {/* Left-edge resize grip — desktop only (the mobile sheet is full-width).
              Drag/arrow keys set the whole column's width (T128). */}
          <ResizeHandle
            edge="left"
            handleProps={ctxHandleProps}
            resizing={ctxResizing}
            ariaLabel="Resize sidebar"
            testId="context-panel-resize"
            className="hidden md:block"
          />
          {/* Mobile grab handle (sheet affordance); hidden on desktop. */}
          <div aria-hidden="true" className="mx-auto my-2 h-1 w-9 rounded-full bg-border-strong md:hidden" />
          <div ref={setHost} className="flex min-h-0 flex-1 flex-col" />
        </aside>
        {/* T184: collapsed expand rail — desktop-only thin strip shown when a panel
            is mounted but the user fully collapsed it. Clicking re-opens col④. */}
        {panelOpen && panelCollapsed && (
          <aside
            aria-label="context (collapsed)"
            data-testid="context-panel-collapsed-rail"
            className="hidden md:flex md:w-8 md:flex-shrink-0 md:flex-col md:items-center md:border-l md:border-border-base md:bg-bg-elevated md:pt-2"
          >
            <button
              type="button"
              data-testid="context-panel-expand"
              aria-label="Expand sidebar"
              title="Expand sidebar"
              onClick={() => setPanelCollapsed(false)}
              className="flex h-7 w-7 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
            >
              <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true" className="h-4 w-4">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12.5 5 7.5 10l5 5" />
              </svg>
            </button>
          </aside>
        )}

        {/* col① on mobile — the bottom Tab Bar (the desktop rail reflowed). */}
        <MobileTabBar modules={modules} activeModuleId={activeModule?.id} orgBase={orgBase} />

        {/* col② on mobile — the active module's secondary nav, reflowed into a
            bottom sheet (opened from the top-bar title). This is what gives mobile
            access to a module's sections beyond its default page — e.g. Workspace's
            Issues / Tasks / Plans, not just the Projects landing. */}
        <MobileModuleNavSheet
          open={navSheetOpen}
          onClose={() => setNavSheetOpen(false)}
          module={activeModule}
          orgBase={orgBase}
          onOpenPalette={() => setPaletteOpen(true)}
        />

        {/* Mobile account/org/theme/sign-out menu (the rail + col② footer
            actions, reflowed into a bottom sheet). */}
        <AccountSheet
          open={accountOpen}
          onClose={() => setAccountOpen(false)}
          orgs={orgs.data ?? []}
          currentSlug={orgCtx?.slug}
          displayName={me.data?.display_name}
          orgBase={orgBase}
          theme={theme}
          onSetTheme={setTheme}
        />

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
  theme,
  onSetTheme,
  onOpenPalette,
}: {
  modules: ReadonlyArray<ModuleDef>;
  activeModuleId?: ModuleDef['id'];
  orgBase: string;
  orgSwitcher: OrgSwitcherBinding;
  displayName?: string;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  onOpenPalette: () => void;
}): React.ReactElement {
  const orgName = orgSwitcher.currentOrg?.name ?? orgSwitcher.fallbackName ?? 'Organization';
  // I23 (T332): col① Conversations icon badge = the cross-source unread total.
  // Shows the @me-mention total in brand when any source @-mentions me (the
  // high-signal state), else the count of unread sources in neutral. Hidden at 0.
  const unreadDigest = useUnreadConversations();
  const digestRows = unreadDigest.data ?? [];
  const digestMentions = digestRows.reduce((n, row) => n + (row.mention_count > 0 ? 1 : 0), 0);
  const digestBadgeCount = digestMentions > 0 ? digestMentions : digestRows.length;
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
              'relative flex h-12 w-12 flex-col items-center justify-center gap-0.5 rounded-xl text-rail-fg motion-safe:transition-colors',
              active ? 'bg-white/15 text-rail-fg-active' : 'hover:bg-white/10 hover:text-rail-fg-active',
            ].join(' ')}
          >
            <span aria-hidden="true" className="inline-flex h-5 w-5">
              <m.Icon />
            </span>
            <span aria-hidden="true" className="text-[0.5rem] font-medium leading-none">
              {m.short}
            </span>
            {m.id === 'conversations' && digestBadgeCount > 0 && (
              <span
                data-testid="rail-conversations-unread-badge"
                data-mention={digestMentions > 0 ? 'true' : 'false'}
                aria-label={
                  digestMentions > 0
                    ? `${digestMentions} conversations mention you`
                    : `${digestRows.length} unread conversations`
                }
                className={[
                  'absolute right-1.5 top-1 inline-flex min-w-[1.05rem] items-center justify-center rounded-full px-1 text-[0.625rem] font-semibold leading-none tabular-nums',
                  digestMentions > 0 ? 'bg-brand text-white' : 'bg-status-slate-solid text-white',
                ].join(' ')}
              >
                {digestBadgeCount > 99 ? '99+' : digestBadgeCount}
              </span>
            )}
          </NavLink>
        );
      })}

      <div className="flex-1" />

      {/* Connection status + Search (⌘K) — bottom utility cluster, pinned just
          above the signed-in user avatar. */}
      <RailConnectionStatus />

      <button
        type="button"
        onClick={onOpenPalette}
        aria-label="Search (⌘K)"
        title="Search (⌘K)"
        data-testid="open-palette"
        className="mb-1 inline-flex h-10 w-10 items-center justify-center rounded-xl text-rail-fg hover:bg-white/10 hover:text-rail-fg-active motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-5 w-5">
          <SearchIcon />
        </span>
      </button>

      {/* Signed-in user (bottom) → right-popout panel (theme + sign out). */}
      <RailUser displayName={displayName} orgBase={orgBase} theme={theme} onSetTheme={onSetTheme} />
    </nav>
  );
}

// ============================================================================
// v2.10.1 [T105] RailConnectionStatus — the col① connection indicator (pinned at
// the bottom of the rail, above the user avatar): a
// WiFi glyph with a small status dot (top-right). Dot color tracks the Zustand
// SSE status — green=connected(open), yellow=connecting/reconnecting, red=
// disconnected(closed), muted=idle. The dot breathes (pulse) always; an abnormal
// state adds a ping ripple for a subtle "nudge". The status text is a hover
// tooltip (title) + accessible name — no always-on label (saves rail width).
// ============================================================================
const SSE_DOT: Record<SSEStatus, string> = {
  idle: 'bg-text-muted',
  connecting: 'bg-warning',
  open: 'bg-success',
  reconnecting: 'bg-warning',
  closed: 'bg-danger',
};
const SSE_TEXT: Record<SSEStatus, string> = {
  idle: 'Connecting…',
  connecting: 'Connecting…',
  open: 'Connected',
  reconnecting: 'Reconnecting…',
  closed: 'Disconnected',
};

function RailConnectionStatus(): React.ReactElement {
  const status = useAppStore((s) => s.sseStatus);
  const dot = SSE_DOT[status] ?? SSE_DOT.idle;
  const text = SSE_TEXT[status] ?? status;
  // connecting / reconnecting / closed get the extra attention ripple.
  const abnormal = status === 'connecting' || status === 'reconnecting' || status === 'closed';
  return (
    <div
      className="relative mb-1 inline-flex h-10 w-10 items-center justify-center rounded-xl text-rail-fg"
      data-testid="rail-connection"
      data-status={status}
      role="status"
      aria-label={`Connection: ${text}`}
      title={text}
    >
      <span aria-hidden="true" className="inline-flex h-5 w-5">
        <WifiIcon />
      </span>
      <span aria-hidden="true" className="absolute right-1.5 top-1.5 inline-flex h-2.5 w-2.5 items-center justify-center">
        {abnormal && (
          <span className={`absolute inline-flex h-full w-full rounded-full opacity-75 ${dot} motion-safe:animate-ping`} />
        )}
        <span className={`relative inline-flex h-2 w-2 rounded-full ring-2 ring-rail-bg ${dot} motion-safe:animate-pulse`} />
      </span>
    </div>
  );
}

// ============================================================================
// v2.10.1 [T105] RailUser — the col① bottom avatar. Click → a right-popout panel
// (anchored to the rail's bottom edge) with: Your account (→ /me), the Light/Dark
// theme toggle, and Sign out — consolidating what used to live in the col② footer.
// Esc / click-outside close + focus-trap via useModalA11y (mirrors the modals).
// ============================================================================
function RailUser({
  displayName,
  orgBase,
  theme,
  onSetTheme,
}: {
  displayName?: string;
  orgBase: string;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
}): React.ReactElement | null {
  const [open, setOpen] = useState(false);
  const panelRef = useModalA11y({ open, onClose: () => setOpen(false) });
  const signout = useSignout();
  // Always render (fallback initial) so account/theme/sign-out stay reachable even
  // before /api/auth/me resolves — mirrors the mobile top-bar account avatar.
  const name = displayName ?? 'Account';
  const initial = (displayName?.slice(0, 1) ?? 'A').toUpperCase();
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`${name} — account and settings`}
        title={name}
        data-testid="sidebar-user"
        className="mt-0.5 inline-flex h-9 w-9 items-center justify-center rounded-full bg-white/15 text-xs font-semibold text-rail-fg-active hover:bg-white/25 focus-visible:ring-2 focus-visible:ring-white motion-safe:transition-colors"
      >
        <span aria-hidden="true">{initial}</span>
      </button>
      {open && (
        <>
          {/* transparent click-outside catcher */}
          <div className="fixed inset-0 z-40" aria-hidden="true" onClick={() => setOpen(false)} />
          <div
            ref={panelRef}
            role="dialog"
            aria-modal="true"
            aria-label="Account and settings"
            data-testid="rail-user-panel"
            className="absolute bottom-0 left-full z-50 ml-2 w-56 rounded-xl border border-white/10 bg-rail-bg p-2 text-rail-fg shadow-3 backdrop-blur-md"
          >
            <div className="truncate px-2 pb-2 pt-1 text-sm font-semibold text-rail-fg-active">
              {name}
            </div>
            <NavLink
              to={`${orgBase}/me`}
              onClick={() => setOpen(false)}
              data-testid="rail-account-link"
              className="flex items-center gap-2 rounded px-2 py-1.5 text-sm text-rail-fg hover:bg-white/10 hover:text-rail-fg-active motion-safe:transition-colors"
            >
              <span aria-hidden="true" className="inline-flex h-4 w-4">
                <UsersIcon />
              </span>
              <span>Your account</span>
            </NavLink>
            <div className="mt-2 px-1">
              <ThemeSegmented theme={theme} onSetTheme={onSetTheme} />
            </div>
            <button
              type="button"
              onClick={() => signout.mutate()}
              data-testid="sidebar-signout"
              className="mt-2 flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-rail-fg hover:bg-white/10 hover:text-danger motion-safe:transition-colors"
            >
              <span aria-hidden="true" className="inline-flex h-4 w-4">
                <SignoutIcon />
              </span>
              <span>Sign out</span>
            </button>
          </div>
        </>
      )}
    </div>
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
  onToggleCollapsed,
  orgBase,
  onOpenPalette,
}: {
  module?: ModuleDef;
  collapsed: boolean;
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
      <SecondaryNavBody module={module} orgBase={orgBase} onOpenPalette={onOpenPalette} />
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
  orgBase,
  onOpenPalette,
}: {
  module?: ModuleDef;
  orgBase: string;
  onOpenPalette: () => void;
}): React.ReactElement {
  const location = useLocation();
  const navigate = useNavigate();
  const deleteConversation = useDeleteConversation();
  const [pendingDeleteDM, setPendingDeleteDM] = useState<SidebarChild | null>(null);
  // A per-module col② override (registered by the owning module task), or
  // undefined → fall back to the shell default below.
  const CustomNav = module ? SECONDARY_NAV_REGISTRY[module.id] : undefined;

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

      {/* Nav body — a module may own its col② via SECONDARY_NAV_REGISTRY (per-
          module component); otherwise the shell default renders its `items` as a
          collapsible group with the channel/DM/project expandable sub-lists. */}
      {/* T319: min-h-0 lets this flex child shrink below its content height so
          overflow-y-auto actually scrolls — without it a long col② list (many
          channels + DMs) grows past the viewport and the bottom rows are
          unreachable (no scroll, no paging). @oopslink. */}
      <div className="min-h-0 flex-1 overflow-y-auto px-2 py-2">
        {module && CustomNav ? (
          <CustomNav orgBase={orgBase} />
        ) : (
          module && (
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
          )
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

// ============================================================================
// v2.10.1 [M1] MobileTopBar — the small-screen (<768) top bar. The rail + col②
// are desktop columns, so this slim fixed bar carries the active screen title
// plus the rail/col② actions that have no column on mobile: search (⌘K
// palette), the col④ context ⓘ (shown only when a panel is mounted), and the
// account avatar (org/theme/sign-out sheet). Every button is a ≥44px target.
// ============================================================================
function MobileTopBar({
  title,
  displayName,
  hasContextPanel,
  sheetOpen,
  hasNav,
  navSheetOpen,
  onOpenNav,
  onToggleSheet,
  onOpenPalette,
  onOpenAccount,
}: {
  title: string;
  displayName?: string;
  hasContextPanel: boolean;
  sheetOpen: boolean;
  hasNav: boolean;
  navSheetOpen: boolean;
  onOpenNav: () => void;
  onToggleSheet: () => void;
  onOpenPalette: () => void;
  onOpenAccount: () => void;
}): React.ReactElement {
  return (
    <header className="fixed inset-x-0 top-0 z-30 flex h-12 items-center gap-1 border-b border-border-base bg-bg-elevated pl-3 pr-1 md:hidden">
      {hasNav ? (
        // Tappable title → opens the module's secondary-nav sheet (the only way
        // to reach a module's other sections on mobile, where col② has no column).
        <button
          type="button"
          onClick={onOpenNav}
          aria-label={`${title} sections`}
          aria-expanded={navSheetOpen}
          data-testid="mobile-nav-toggle"
          className="flex min-w-0 flex-1 items-center gap-1 rounded py-1 pr-1 text-left text-sm font-medium text-text-primary hover:bg-bg-subtle motion-safe:transition-colors"
        >
          <span className="min-w-0 truncate">{title}</span>
          <span aria-hidden="true" className="inline-flex h-4 w-4 flex-shrink-0 text-text-muted">
            <ChevronDownIcon />
          </span>
        </button>
      ) : (
        <span className="min-w-0 flex-1 truncate text-sm font-medium text-text-primary">{title}</span>
      )}
      <button
        type="button"
        onClick={onOpenPalette}
        aria-label="Search (⌘K)"
        data-testid="mobile-search"
        className="inline-flex h-11 w-11 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-5 w-5">
          <SearchIcon />
        </span>
      </button>
      {hasContextPanel && (
        <button
          type="button"
          onClick={onToggleSheet}
          aria-label="Details"
          aria-expanded={sheetOpen}
          data-testid="mobile-context-toggle"
          className="inline-flex h-11 w-11 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors"
        >
          <span aria-hidden="true" className="inline-flex h-5 w-5">
            <InfoIcon />
          </span>
        </button>
      )}
      <button
        type="button"
        onClick={onOpenAccount}
        aria-label="Account and settings"
        data-testid="mobile-account-toggle"
        className="inline-flex h-11 w-11 items-center justify-center"
      >
        <span className="inline-flex h-8 w-8 items-center justify-center rounded-full bg-brand text-xs font-semibold text-white">
          <span aria-hidden="true">{(displayName?.slice(0, 1) ?? 'A').toUpperCase()}</span>
        </span>
      </button>
    </header>
  );
}

// ============================================================================
// MobileModuleNavSheet — the col② secondary nav reflowed into a bottom sheet on
// mobile (<768), where col② has no column. Renders the active module's registered
// secondary nav (e.g. Workspace → Projects / Issues / Tasks / Plans) so every
// section is reachable on mobile, not just the module's default landing page.
// Falls back to the shared SecondaryNavBody for a module with no registered
// override. Reuses the generic <BottomSheet> primitive.
// ============================================================================
function MobileModuleNavSheet({
  open,
  onClose,
  module,
  orgBase,
  onOpenPalette,
}: {
  open: boolean;
  onClose: () => void;
  module?: ModuleDef;
  orgBase: string;
  onOpenPalette: () => void;
}): React.ReactElement | null {
  const ModuleNav = module ? SECONDARY_NAV_REGISTRY[module.id] : undefined;
  return (
    <BottomSheet open={open} onClose={onClose} title={module?.label ?? 'Navigation'} testId="mobile-nav-sheet">
      {ModuleNav ? (
        <ModuleNav orgBase={orgBase} />
      ) : (
        <SecondaryNavBody module={module} orgBase={orgBase} onOpenPalette={onOpenPalette} />
      )}
    </BottomSheet>
  );
}

// ============================================================================
// v2.10.1 [M1] AccountSheet — the mobile (<768) home for the actions that live
// in the desktop rail (org switcher, account) + col② footer (theme, sign out).
// Reuses the generic <BottomSheet> primitive.
// ============================================================================
function AccountSheet({
  open,
  onClose,
  orgs,
  currentSlug,
  displayName,
  orgBase,
  theme,
  onSetTheme,
}: {
  open: boolean;
  onClose: () => void;
  orgs: Array<{ id: string; slug: string; name: string }>;
  currentSlug?: string;
  displayName?: string;
  orgBase: string;
  theme: Theme;
  onSetTheme: (t: Theme) => void;
}): React.ReactElement {
  const navigate = useNavigate();
  const signout = useSignout();
  return (
    <BottomSheet open={open} onClose={onClose} title={displayName ?? 'Account'} testId="account-sheet">
      {orgs.length > 0 && (
        <div className="mb-3">
          <h3 className="px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
            Organizations
          </h3>
          <ul className="space-y-0.5">
            {orgs.map((o) => (
              <li key={o.id}>
                <button
                  type="button"
                  onClick={() => {
                    if (o.slug !== currentSlug) navigate(`/organizations/${o.slug}`);
                    onClose();
                  }}
                  data-testid={`account-org-${o.slug}`}
                  className={[
                    'flex min-h-[44px] w-full items-center gap-2 rounded px-2 text-sm motion-safe:transition-colors',
                    o.slug === currentSlug
                      ? 'font-medium text-brand'
                      : 'text-text-primary hover:bg-bg-subtle',
                  ].join(' ')}
                >
                  <span aria-hidden="true" className="inline-flex h-4 w-4">
                    <OrgIcon />
                  </span>
                  <span className="truncate">{o.name}</span>
                </button>
              </li>
            ))}
          </ul>
        </div>
      )}

      <NavLink
        to={`${orgBase}/me`}
        onClick={onClose}
        data-testid="account-profile-link"
        className="flex min-h-[44px] items-center gap-2 rounded px-2 text-sm text-text-primary hover:bg-bg-subtle motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4">
          <UsersIcon />
        </span>
        <span>Your account</span>
      </NavLink>

      <div className="mt-3">
        <ThemeSegmented theme={theme} onSetTheme={onSetTheme} />
      </div>

      <button
        type="button"
        onClick={() => signout.mutate()}
        data-testid="account-signout"
        className="mt-2 flex min-h-[44px] w-full items-center gap-2 rounded px-2 text-sm text-text-muted hover:bg-bg-subtle hover:text-danger motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4">
          <SignoutIcon />
        </span>
        <span>Sign out</span>
      </button>
    </BottomSheet>
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
function ChevronDownIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M5.5 8l4.5 4.5L14.5 8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
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
// InfoIcon — the mobile top-bar col④ context (ⓘ) trigger (mockup `.mtop ⓘ`).
function InfoIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="10" r="7" />
      <path d="M10 9v4.5" strokeLinecap="round" />
      <circle cx="10" cy="6.5" r="0.6" fill="currentColor" stroke="none" />
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
// WifiIcon — three signal arcs + base dot (T105 connection status). The colored
// status dot is drawn by RailConnectionStatus over the top-right corner.
function WifiIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M2.5 7.5a11 11 0 0 1 15 0" strokeLinecap="round" />
      <path d="M5 10.3a7 7 0 0 1 10 0" strokeLinecap="round" />
      <path d="M7.5 13a3.2 3.2 0 0 1 5 0" strokeLinecap="round" />
      <circle cx="10" cy="15.8" r="0.6" fill="currentColor" stroke="none" />
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
// PlanIcon — a diamond (mockup ◇), the Workspace > Plan nav glyph.
function PlanIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M10 2.5 17.5 10 10 17.5 2.5 10z" strokeLinejoin="round" />
    </svg>
  );
}
// T207: Reminders ⏰ — a clock glyph for the top-level module (rail + col②).
function ReminderIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <circle cx="10" cy="11" r="6" />
      <path d="M10 8v3l2 1.5M6 3.5 3.5 6M14 3.5 16.5 6" strokeLinecap="round" strokeLinejoin="round" />
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
