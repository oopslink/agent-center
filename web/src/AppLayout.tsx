import type React from 'react';
import { Suspense, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link, NavLink, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { useSSE } from '@/sse/useSSE';
import {
  conversationDeleteErrorMessage,
  useConversations,
  useDeleteConversation,
  useUnreadConversations,
} from '@/api/conversations';
import { useProjects } from '@/api/projects';
import { useAttention } from '@/api/attention';
import { AttentionPanel } from '@/shell/AttentionPanel';
import { useAppStore } from '@/store/app';
import { PageSkeleton } from '@/components/Skeleton';
import { UnreadBadge } from '@/components/UnreadBadge';
import { CommandPalette } from '@/components/CommandPalette';
import { WorkerEnrolledToast } from '@/components/WorkerEnrolledToast';
import { ConfirmModal } from '@/components/ConfirmModal';
import { useKeyShortcuts } from '@/useKeyShortcuts';
import { readTheme, writeTheme, type Theme } from '@/theme';
import { useMe, useSignout, useOrgs, orgApi } from '@/api/auth';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useOptionalOrgContext } from './OrgContext';
import { useContextPanelController } from '@/shell/contextPanel';
import { useResizablePanel } from '@/components/useResizablePanel';
import { MobileTabBar, type TabBarModule } from '@/shell/MobileTabBar';
import { BottomSheet } from '@/shell/BottomSheet';
import { SECONDARY_NAV_REGISTRY, type ShellModuleId } from '@/shell/secondaryNav';
import { OrgSettingsSecondaryNav } from '@/shell/nav/OrgSettingsSecondaryNav';

// AppLayout v5 — v2.10.0 [T1] three-column module rail + per-module secondary
// nav + on-demand context panel (col④). Desktop: col①(rail) | col②(secondary nav)
// | col③(content) | col④(context). Mobile: bottom tab bar + sheets.

const SIDEBAR_KEY = 'ac.sidebar.collapsed';
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
// Module definitions — the five top-level modules in the rail.
// ============================================================================
interface ModuleDef {
  id: ShellModuleId;
  label: string;
  short: string;
  defaultPath: string; // relative to orgBase
  Icon: () => React.ReactElement;
  /** URL path prefixes that activate this module (relative to orgBase, no leading /). */
  pathPrefixes: string[];
}

const MODULE_DEFS: ReadonlyArray<ModuleDef> = [
  {
    id: 'workspace',
    label: 'Workspace',
    short: 'Work',
    defaultPath: 'projects',
    Icon: FolderIcon,
    pathPrefixes: ['projects', 'issues', 'tasks', 'plans', 'repos'],
  },
  {
    id: 'conversations',
    label: 'Conversations',
    short: 'Chat',
    defaultPath: 'channels',
    Icon: ChatIcon,
    pathPrefixes: ['channels', 'dms'],
  },
  {
    id: 'members',
    label: 'Members',
    short: 'Team',
    defaultPath: 'members/humans',
    Icon: UsersIcon,
    pathPrefixes: ['members', 'agents', 'users'],
  },
  {
    id: 'reminders',
    label: 'Reminders',
    short: 'Remind',
    defaultPath: 'reminders',
    Icon: ReminderIcon,
    pathPrefixes: ['reminders'],
  },
  {
    id: 'system',
    label: 'System',
    short: 'System',
    defaultPath: 'environment',
    Icon: FleetIcon,
    pathPrefixes: ['environment', 'settings', 'version'],
  },
];

function detectActiveModule(pathname: string, orgBase: string): ShellModuleId | undefined {
  // Strip orgBase prefix + leading slash to get the effective route segment.
  let effective = pathname;
  if (orgBase && effective.startsWith(orgBase)) {
    effective = effective.slice(orgBase.length);
  }
  if (effective.startsWith('/')) effective = effective.slice(1);
  const seg = effective.split('/')[0] || '';

  // Special case: organization-settings is system-level
  if (seg === 'organization-settings') return 'system';

  for (const mod of MODULE_DEFS) {
    if (mod.pathPrefixes.some((p) => seg === p)) return mod.id;
  }
  // Default to workspace for the root path.
  return 'workspace';
}

// ============================================================================
// Nav data structures (carried over from v4)
// ============================================================================
interface NavItem {
  to: string;
  label: string;
  end?: boolean;
  Icon: () => React.ReactElement;
}

interface NavSection {
  label: string;
  items: ReadonlyArray<NavItem>;
}

interface SidebarChild {
  to: string;
  label: string;
  id?: string;
  kind?: 'channel' | 'dm' | 'project';
  canDelete?: boolean;
  unreadCount?: number;
  mentionCount?: number;
}

function buildModuleNavSections(moduleId: ShellModuleId, base: string): ReadonlyArray<NavSection> {
  const p = (path: string) => `${base}/${path}`;
  switch (moduleId) {
    case 'workspace':
      return [{ label: 'Workspace', items: [
        { to: p('projects'), label: 'Projects', Icon: FolderIcon },
        { to: p('issues'), label: 'Issues', Icon: IssueIcon },
        { to: p('tasks'), label: 'Tasks', Icon: TaskIcon },
        { to: p('plans'), label: 'Plan', Icon: PlanIcon },
        { to: p('repos'), label: 'Repos', Icon: RepoIcon },
      ] }];
    case 'conversations':
      return [{ label: 'Conversations', items: [
        { to: p('channels'), label: 'Channels', Icon: HashIcon },
        { to: p('dms'), label: 'DMs', Icon: ChatIcon },
      ] }];
    case 'members':
      return [{ label: 'Members', items: [
        { to: p('members/humans'), label: 'Humans', Icon: UsersIcon },
        { to: p('agents'), label: 'Agents', Icon: AgentsIcon },
      ] }];
    case 'reminders':
      return [{ label: 'Reminders', items: [
        { to: p('reminders'), label: 'Reminders', Icon: ReminderIcon },
      ] }];
    case 'system':
      return [{ label: 'System', items: [
        { to: p('environment'), label: 'Environment', Icon: FleetIcon },
        { to: p('settings'), label: 'Settings', Icon: SettingsIcon },
        { to: p('version'), label: 'Version', Icon: VersionIcon },
      ] }];
    default:
      return [];
  }
}

// ============================================================================
// AppLayout — the V5 4-column shell
// ============================================================================
export default function AppLayout(): React.ReactElement {
  useSSE();
  const { t } = useTranslation();
  // Display label for a top-level module. The module `id` (not the label) is
  // the stable key used for routing/active-state, so the visible label is free
  // to be localised at render. `short` is the mobile tab-bar label.
  const moduleLabel = (id: ShellModuleId) => t(`nav.${id}`);
  const moduleShort = (id: ShellModuleId) => t(`nav.short.${id}`);
  const me = useMe();
  const setCurrentUserId = useAppStore((s) => s.setCurrentUserId);
  useEffect(() => {
    const m = me.data;
    if (!m?.identity_id) return;
    const ref = (m.kind === 'agent' ? 'agent:' : 'user:') + m.identity_id;
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
  const [collapsed, setCollapsed] = useState<boolean>(readSidebarCollapsed);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [userPanelOpen, setUserPanelOpen] = useState(false);
  const [alertsPanelOpen, setAlertsPanelOpen] = useState(false);
  const [mobileNavSheetOpen, setMobileNavSheetOpen] = useState(false);
  const [mobileAccountSheetOpen, setMobileAccountSheetOpen] = useState(false);
  const [mobileAlertsOpen, setMobileAlertsOpen] = useState(false);


  const location = useLocation();
  const navigate = useNavigate();
  const displayName = me.data?.display_name;

  // Auto-close sheets on navigation.
  useEffect(() => {
    setMobileNavSheetOpen(false);
    setMobileAccountSheetOpen(false);
  }, [location.pathname]);

  // Persist sidebar + theme.
  useEffect(() => {
    try {
      if (typeof localStorage !== 'undefined' && typeof localStorage.setItem === 'function') {
        localStorage.setItem(SIDEBAR_KEY, collapsed ? '1' : '0');
      }
    } catch { /* ignore */ }
  }, [collapsed]);

  useEffect(() => {
    writeTheme(theme);
  }, [theme]);

  const onSetTheme = (t: Theme) => setTheme(t);

  // Detect active module from current route.
  const activeModuleId = detectActiveModule(location.pathname, orgBase);
  const activeModule = MODULE_DEFS.find((m) => m.id === activeModuleId);
  const isOrgSettings = location.pathname.includes('/organization-settings');

  // Keyboard shortcuts.
  const shortcuts = useMemo(
    () => ({
      'mod+k': () => setPaletteOpen((v) => !v),
      'mod+b': () => setCollapsed((v) => !v),
      'mod+d': () => setTheme((t) => (t === 'dark' ? 'light' : 'dark')),
      'mod+1': () => navigate(`${orgBase}/projects`),
      'mod+2': () => navigate(`${orgBase}/channels`),
      'mod+3': () => navigate(`${orgBase}/members/humans`),
      'mod+4': () => navigate(`${orgBase}/environment`),
    }),
    [navigate, orgBase],
  );
  useKeyShortcuts(shortcuts);

  // Context panel (col④).
  const ctxPanel = useContextPanelController();
  const ctxResize = useResizablePanel({
    storageKey: 'ac.contextpanel.width',
    defaultWidth: 256,
    minWidth: 200,
    maxWidth: 600,
    edge: 'left',
  });

  // Unread conversations for the rail badge.
  const unreadConvs = useUnreadConversations();
  const conversationsUnread = (unreadConvs.data ?? []).reduce((s, r) => s + (r.unread_count || 0), 0);
  const conversationsMentions = (unreadConvs.data ?? []).reduce((s, r) => s + (r.mention_count || 0), 0);

  // v2.26.0 I61: the "Needs your attention" rail item. Its source is the unified
  // /attention endpoint — stuck tasks (running + input_required/obstacle) UNIONed
  // with the human's directed unread (DM + @mention), so an agent→human escalation
  // surfaces even with NO human-owned task. The panel auto-opens when a NEW item
  // appears so the user catches it on ANY page; the badge persists the count.
  const attention = useAttention(orgCtx?.slug);
  const attentionItems = attention.items;
  const alertCount = attentionItems.length;
  // seenAlertIdsRef stays null until the first resolved snapshot, then tracks the
  // current item set; a freshly-appearing ref (re)opens the panel exactly once.
  const seenAlertIdsRef = useRef<Set<string> | null>(null);
  useEffect(() => {
    if (attention.isLoading) return; // wait for the first real fetch before seeding
    const ids = attentionItems.map((it) => `${it.kind}:${it.ref}`);
    const prev = seenAlertIdsRef.current;
    seenAlertIdsRef.current = new Set(ids);
    if (prev === null) {
      // First snapshot: surface existing items once on load.
      if (ids.length > 0) setAlertsPanelOpen(true);
      return;
    }
    if (ids.some((id) => !prev.has(id))) setAlertsPanelOpen(true);
  }, [attention.isLoading, attentionItems]);

  // Org switcher binding.
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
      // Navigate to org settings page.
      const org = (orgs.data ?? []).find((o) => o.id === id);
      if (org) {
        navigate(`/organizations/${org.slug}/organization-settings/profile`);
      }
    },
  };

  // Mobile tab bar module definitions.
  const tabBarModules: TabBarModule[] = MODULE_DEFS.map((m) => ({
    id: m.id,
    label: moduleLabel(m.id),
    short: moduleShort(m.id),
    defaultPath: m.defaultPath,
    Icon: m.Icon,
  }));

  const signout = useSignout();

  return (
    <ctxPanel.Provider value={ctxPanel.value}>
      {/* Mobile viewport fix: use the dynamic viewport height (.h-screen-dvh =
          100vh fallback → 100dvh) instead of h-screen. 100vh is the LARGE
          viewport on mobile browsers, so the reserved bottom band (fixed tab bar
          + the chat's pinned composer) gets pushed below the visible area and the
          input box becomes unreachable on long threads; 100dvh tracks the visible
          viewport so the composer stays above the tab bar. See index.css. */}
      <div className="flex h-screen-dvh bg-bg-base">
        {/* ────── Mobile top bar (md:hidden) ────── */}
        <header className="fixed inset-x-0 top-0 z-30 flex h-12 items-center gap-2 border-b border-border-base bg-bg-elevated px-3 md:hidden">
          {/* Left: module name → opens mobile nav sheet */}
          <button
            type="button"
            data-testid="mobile-nav-toggle"
            onClick={() => setMobileNavSheetOpen(true)}
            className="flex-1 truncate text-left text-sm font-medium text-text-primary"
          >
            {isOrgSettings ? t('nav.orgSettings') : activeModule ? moduleLabel(activeModule.id) : '…'}
          </button>
          {/* Right: alerts + account toggle */}
          <div className="flex items-center gap-1">
            {/* v2.26.0 I61: "Needs your attention" reachable on mobile too (the
                desktop rail is md:flex-only). Same unified source + panel. */}
            <button
              type="button"
              data-testid="mobile-alerts"
              data-count={alertCount}
              aria-label={alertCount > 0 ? t('shell.alerts.attention', { count: alertCount }) : t('shell.alerts.label')}
              onClick={() => setMobileAlertsOpen((v) => !v)}
              className={[
                'relative inline-flex h-8 w-8 items-center justify-center rounded motion-safe:transition-colors',
                alertCount > 0 ? 'text-danger hover:bg-danger/10' : 'text-text-secondary hover:bg-bg-subtle',
              ].join(' ')}
            >
              <span aria-hidden="true" className="inline-flex h-5 w-5"><AlertBellIcon /></span>
              {alertCount > 0 && (
                <span
                  data-testid="mobile-alerts-badge"
                  className="absolute -right-0.5 -top-0.5 inline-flex min-w-[1.05rem] items-center justify-center rounded-full bg-danger px-1 text-[0.625rem] font-semibold leading-none tabular-nums text-white"
                >
                  {alertCount > 99 ? '99+' : alertCount}
                </span>
              )}
            </button>
            <button
              type="button"
              data-testid="mobile-account-toggle"
              aria-label="Account menu"
              onClick={() => setMobileAccountSheetOpen(true)}
              className="inline-flex h-8 w-8 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle"
            >
              <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-bg-subtle text-xs font-medium text-text-secondary">
                {(displayName ?? '?').slice(0, 1).toUpperCase()}
              </span>
            </button>
          </div>
        </header>
        {/* Mobile attention popout — anchored under the top bar (rail is hidden). */}
        {mobileAlertsOpen && (
          <AttentionPanel
            items={attentionItems}
            orgBase={orgBase}
            onClose={() => setMobileAlertsOpen(false)}
            testId="mobile-alerts-panel"
            toggleTestId="mobile-alerts"
            className="fixed inset-x-2 top-12 z-40 max-h-[70vh] overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-3 shadow-2 md:hidden"
          />
        )}

        {/* ────── col① Module Rail (desktop) ────── */}
        <nav
          aria-label="modules"
          className="hidden w-14 flex-col items-center border-r border-border-base bg-rail-bg py-3 md:flex"
        >
          {/* Rail top: org switcher (always visible, never collapses) */}
          <RailOrgSwitcher orgSwitcher={orgSwitcher} />

          <div className="mt-2 flex flex-1 flex-col items-center gap-1">
            {MODULE_DEFS.map((mod) => {
              const active = mod.id === activeModuleId;
              const href = mod.defaultPath ? `${orgBase}/${mod.defaultPath}` : orgBase || '/';
              return (
                <Link
                  key={mod.id}
                  to={href}
                  data-testid={`rail-module-${mod.id}`}
                  data-active={active}
                  aria-label={moduleLabel(mod.id)}
                  aria-current={active ? 'page' : undefined}
                  className={[
                    'relative inline-flex h-10 w-10 items-center justify-center rounded-lg motion-safe:transition-colors',
                    active
                      ? 'bg-brand/10 text-brand'
                      : 'text-text-muted hover:bg-bg-subtle hover:text-text-primary',
                  ].join(' ')}
                >
                  <span aria-hidden="true" className="inline-flex h-5 w-5">
                    <mod.Icon />
                  </span>
                  {mod.id === 'conversations' && conversationsUnread > 0 && (
                    <span
                      data-testid="rail-conversations-unread-badge"
                      data-mention={conversationsMentions > 0 ? 'true' : 'false'}
                      aria-label={
                        conversationsMentions > 0
                          ? `${conversationsMentions} conversations mention you`
                          : `${conversationsUnread} unread conversations`
                      }
                      className={[
                        'absolute -right-0.5 -top-0.5 inline-flex min-w-[1.05rem] items-center justify-center rounded-full px-1 text-[0.625rem] font-semibold leading-none tabular-nums text-white',
                        conversationsMentions > 0 ? 'bg-brand' : 'bg-status-slate-solid',
                      ].join(' ')}
                    >
                      {conversationsMentions > 0 ? conversationsMentions : conversationsUnread > 99 ? '99+' : conversationsUnread}
                    </span>
                  )}
                </Link>
              );
            })}
          </div>

          {/* Rail bottom: alerts + connection status + user avatar */}
          <div className="flex flex-col items-center gap-2">
            {/* Global "stuck" alerts — a task waiting on you, visible from any page. */}
            <button
              type="button"
              data-testid="rail-alerts"
              data-count={alertCount}
              aria-label={alertCount > 0 ? t('shell.alerts.attention', { count: alertCount }) : t('shell.alerts.label')}
              onClick={() => setAlertsPanelOpen((v) => !v)}
              className={[
                'relative inline-flex h-10 w-10 items-center justify-center rounded-lg motion-safe:transition-colors',
                alertCount > 0
                  ? 'text-danger hover:bg-danger/10'
                  : 'text-text-muted hover:bg-bg-subtle hover:text-text-primary',
              ].join(' ')}
            >
              <span aria-hidden="true" className="inline-flex h-5 w-5"><AlertBellIcon /></span>
              {alertCount > 0 && (
                <span
                  data-testid="rail-alerts-badge"
                  className="absolute -right-0.5 -top-0.5 inline-flex min-w-[1.05rem] items-center justify-center rounded-full bg-danger px-1 text-[0.625rem] font-semibold leading-none tabular-nums text-white motion-safe:animate-pulse"
                >
                  {alertCount > 99 ? '99+' : alertCount}
                </span>
              )}
            </button>
            {alertsPanelOpen && (
              <AttentionPanel
                items={attentionItems}
                orgBase={orgBase}
                onClose={() => setAlertsPanelOpen(false)}
              />
            )}
            <RailConnectionStatus />
            <button
              type="button"
              data-testid="sidebar-user"
              aria-label={displayName ?? 'Account'}
              onClick={() => setUserPanelOpen((v) => !v)}
              className="relative inline-flex h-8 w-8 items-center justify-center rounded-full bg-bg-elevated text-xs font-medium text-text-secondary hover:bg-bg-subtle"
            >
              {(displayName ?? '?').slice(0, 1).toUpperCase()}
            </button>
          </div>

          {/* User popout panel */}
          {userPanelOpen && (
            <RailUserPanel
              theme={theme}
              onSetTheme={onSetTheme}
              displayName={displayName}
              orgBase={orgBase}
              onSignout={() => signout.mutate()}
              onClose={() => setUserPanelOpen(false)}
            />
          )}
        </nav>

        {/* ────── col② Secondary Nav (desktop) ────── */}
        <SecondaryNavColumn
          activeModuleId={activeModuleId}
          collapsed={collapsed}
          onToggleCollapsed={() => setCollapsed((v) => !v)}
          orgBase={orgBase}
          onOpenPalette={() => setPaletteOpen(true)}
        />

        {/* ────── col③ Content ────── */}
        <main className="flex flex-1 overflow-hidden pt-12 pb-14 md:pb-0 md:pt-0">
          <div
            className="flex h-full w-full flex-col overflow-y-auto px-4 pt-2 pb-0 md:p-4 lg:p-6"
            data-testid="app-content-shell"
          >
            <Suspense fallback={<PageSkeleton />}>
              <Outlet />
            </Suspense>
          </div>
        </main>

        {/* ────── col④ expand toggle (when collapsed) ────── */}
        {ctxPanel.open && ctxPanel.collapsed && (
          <button
            type="button"
            data-testid="context-panel-expand"
            aria-label="Show sidebar"
            title="Show sidebar"
            onClick={() => ctxPanel.value.setCollapsed(false)}
            className="hidden shrink-0 items-center border-l border-border-base bg-bg-elevated px-1 text-text-muted hover:bg-bg-subtle hover:text-text-primary md:flex"
          >
            <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="h-4 w-4" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" d="M12.5 5l-5 5 5 5" />
            </svg>
          </button>
        )}

        {/* ────── col④ Context Panel (desktop + mobile host) ────── */}
        <div
          data-testid="context-panel"
          data-open={ctxPanel.open}
          style={{ '--ctx-w': `${ctxResize.width}px` } as React.CSSProperties}
          className={[
            'relative hidden flex-col border-l border-border-base bg-bg-elevated md:flex',
            ctxPanel.open && !ctxPanel.collapsed ? 'md:w-[var(--ctx-w)]' : 'md:w-0',
          ].join(' ')}
        >
          {ctxPanel.open && !ctxPanel.collapsed && (
            <div
              data-testid="context-panel-resize"
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize context panel"
              tabIndex={0}
              {...ctxResize.handleProps}
              className="absolute inset-y-0 -left-1 z-10 w-2 cursor-col-resize hover:bg-brand/20"
            />
          )}
          <div ref={ctxPanel.setHost} className="flex-1 overflow-y-auto" />
        </div>

        {/* ────── Mobile bottom tab bar (md:hidden) ────── */}
        <MobileTabBar
          modules={tabBarModules}
          activeModuleId={activeModuleId}
          orgBase={orgBase}
          conversationsUnread={conversationsUnread}
          conversationsMentions={conversationsMentions}
        />

        {/* ────── Mobile nav sheet ────── */}
        <BottomSheet
          open={mobileNavSheetOpen}
          onClose={() => setMobileNavSheetOpen(false)}
          title={isOrgSettings ? t('nav.orgSettings') : activeModule ? moduleLabel(activeModule.id) : undefined}
          testId="mobile-nav-sheet"
        >
          {isOrgSettings ? (
            <OrgSettingsSecondaryNav orgBase={orgBase} />
          ) : (
            activeModuleId && (
              <MobileSecondaryNavContent
                moduleId={activeModuleId}
                orgBase={orgBase}
              />
            )
          )}
        </BottomSheet>

        {/* ────── Mobile account sheet ────── */}
        <BottomSheet
          open={mobileAccountSheetOpen}
          onClose={() => setMobileAccountSheetOpen(false)}
          title="Account"
          testId="account-sheet"
        >
          <div className="space-y-1">
            {/* Org switcher section */}
            <div className="pb-1">
              <p className="px-3 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
                Organization
              </p>
              {orgSwitcher.orgs.map((o) => {
                const isCurrent = o.slug === orgSwitcher.currentSlug;
                return (
                  <button
                    key={o.id}
                    type="button"
                    data-testid={isCurrent ? 'mobile-org-settings-link' : `mobile-org-switch-${o.slug}`}
                    className={[
                      'flex min-h-[44px] w-full items-center gap-3 rounded-lg px-3 text-sm hover:bg-bg-subtle',
                      isCurrent ? 'font-medium text-brand' : 'text-text-primary',
                    ].join(' ')}
                    onClick={() => {
                      if (isCurrent) {
                        orgSwitcher.onOpenSettings(o.id);
                      } else {
                        navigate(`/organizations/${o.slug}`);
                      }
                      setMobileAccountSheetOpen(false);
                    }}
                  >
                    <span aria-hidden="true" className="inline-flex h-4 w-4 shrink-0"><OrgIcon /></span>
                    <span className="truncate">{o.name}</span>
                    {isCurrent && (
                      <span className="ml-auto text-[0.6875rem] text-text-muted">Settings</span>
                    )}
                  </button>
                );
              })}
              <button
                type="button"
                data-testid="mobile-create-org"
                className="flex min-h-[44px] w-full items-center gap-3 rounded-lg px-3 text-sm text-accent hover:bg-bg-subtle"
                onClick={() => {
                  orgSwitcher.onCreateOrg();
                  setMobileAccountSheetOpen(false);
                }}
              >
                <span aria-hidden="true" className="text-base leading-none">+</span>
                <span>Create organization</span>
              </button>
            </div>
            <div className="border-t border-border-base pt-1" />
            <Link
              to={`${orgBase}/me`}
              data-testid="account-profile-link"
              className="flex min-h-[44px] items-center gap-3 rounded-lg px-3 text-sm text-text-primary hover:bg-bg-subtle"
            >
              <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-bg-subtle text-xs font-medium text-text-secondary">
                {(displayName ?? '?').slice(0, 1).toUpperCase()}
              </span>
              <span>{displayName ?? 'Your account'}</span>
            </Link>
            <div className="border-t border-border-base pt-2">
              <ThemeSegmented collapsed={false} theme={theme} onSetTheme={onSetTheme} />
            </div>
            <button
              type="button"
              data-testid="account-signout"
              onClick={() => signout.mutate()}
              className="flex min-h-[44px] w-full items-center gap-3 rounded-lg px-3 text-sm text-danger hover:bg-bg-subtle"
            >
              <span aria-hidden="true" className="inline-flex h-4 w-4"><SignoutIcon /></span>
              Sign out
            </button>
          </div>
        </BottomSheet>

        {/* ────── Modals ────── */}
        {createOrgModalOpen && (
          <CreateOrgModal onClose={() => setCreateOrgModalOpen(false)} />
        )}
        <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} />
        <WorkerEnrolledToast />
      </div>
    </ctxPanel.Provider>
  );
}

// ============================================================================
// SecondaryNavColumn (col②) — per-module secondary nav
// ============================================================================
function SecondaryNavColumn({
  activeModuleId,
  collapsed,
  onToggleCollapsed,
  orgBase,
  onOpenPalette,
}: {
  activeModuleId?: ShellModuleId;
  collapsed: boolean;
  onToggleCollapsed: () => void;
  orgBase: string;
  onOpenPalette: () => void;
}): React.ReactElement {
  const { t } = useTranslation('common');
  const location = useLocation();
  // Detect org-settings route to show its dedicated secondary nav.
  const isOrgSettings = location.pathname.includes('/organization-settings');
  // Check if the active module has a registered custom nav.
  const CustomNav = activeModuleId ? SECONDARY_NAV_REGISTRY[activeModuleId] : undefined;

  return (
    <nav
      aria-label="primary"
      data-collapsed={collapsed}
      className={[
        'group/sidebar relative hidden flex-shrink-0 border-r border-border-base md:flex',
        collapsed ? 'w-0 border-r-0' : 'w-60 flex-col bg-bg-subtle p-3',
      ].join(' ')}
    >
      {!collapsed && (
        <>
          <SidebarTop onOpenPalette={onOpenPalette} />
          <div className="mt-3 flex-1 overflow-y-auto">
            {isOrgSettings ? (
              <OrgSettingsSecondaryNav orgBase={orgBase} />
            ) : CustomNav ? (
              <CustomNav orgBase={orgBase} />
            ) : (
              activeModuleId && (
                <DefaultModuleNav
                  moduleId={activeModuleId}
                  orgBase={orgBase}
                />
              )
            )}
          </div>
        </>
      )}
      {/* Collapse toggle — sticks out to the right of the nav edge */}
      <button
        type="button"
        aria-label={collapsed ? t('shell.sidebar.expand') : t('shell.sidebar.collapse')}
        aria-pressed={collapsed}
        data-testid="sidebar-collapse-toggle"
        onClick={onToggleCollapsed}
        title={collapsed ? t('shell.sidebar.expand') : t('shell.sidebar.collapse')}
        className={[
          'absolute -right-3 top-4 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full border border-border-base bg-bg-elevated text-text-secondary shadow-sm hover:bg-bg-subtle hover:text-text-primary focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-accent motion-safe:transition-all',
          collapsed ? 'opacity-100' : 'opacity-0 group-hover/sidebar:opacity-100',
        ].join(' ')}
      >
        <SidebarToggleIcon collapsed={collapsed} />
      </button>
    </nav>
  );
}

// ============================================================================
// DefaultModuleNav — the shell-default NavGroup for unregistered modules
// ============================================================================
function DefaultModuleNav({
  moduleId,
  orgBase,
}: {
  moduleId: ShellModuleId;
  orgBase: string;
}): React.ReactElement {
  const navSections = buildModuleNavSections(moduleId, orgBase);
  const deleteConversation = useDeleteConversation();
  const [pendingDeleteDM, setPendingDeleteDM] = useState<SidebarChild | null>(null);
  const location = useLocation();
  const navigate = useNavigate();

  // Group + subitem expand state.
  const [groupExpanded, setGroupExpanded] = useState<Record<string, boolean>>(
    () => readJSONMap(GROUP_STATE_KEY),
  );
  const [subItemExpanded, setSubItemExpanded] = useState<Record<string, boolean>>(
    () => readJSONMap(SUBITEM_STATE_KEY),
  );
  useEffect(() => { writeJSONMap(GROUP_STATE_KEY, groupExpanded); }, [groupExpanded]);
  useEffect(() => { writeJSONMap(SUBITEM_STATE_KEY, subItemExpanded); }, [subItemExpanded]);
  const isGroupOpen = (label: string) => groupExpanded[label] === undefined ? true : groupExpanded[label];
  const isSubItemOpen = (to: string) => subItemExpanded[to] === undefined ? true : subItemExpanded[to];
  const toggleGroup = (label: string) => setGroupExpanded((m) => ({ ...m, [label]: !isGroupOpen(label) }));
  const toggleSubItem = (to: string) => setSubItemExpanded((m) => ({ ...m, [to]: !isSubItemOpen(to) }));

  // Sub-item data (channels, DMs, projects).
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
    const label = d.peer_display_name ? `@${d.peer_display_name}` : d.peer_identity_id ? '(deleted)' : 'Direct message';
    return {
      id: d.id,
      kind: 'dm' as const,
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

  return (
    <>
      <ul className="space-y-4">
        {navSections.map((section) => {
          const open = isGroupOpen(section.label);
          return (
            <li key={section.label}>
              {section.items.length > 0 ? (
                <h2 className="px-1">
                  <button
                    type="button"
                    onClick={() => toggleGroup(section.label)}
                    aria-expanded={open}
                    data-testid={`sidebar-group-toggle-${section.label}`}
                    className="flex w-full items-center justify-between rounded px-1 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
                  >
                    <span data-testid="section-label">{section.label}</span>
                    <span aria-hidden="true" className="text-text-muted">{open ? '⌄' : '›'}</span>
                  </button>
                </h2>
              ) : (
                <h2 data-testid="section-label" className="px-2 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
                  {section.label}
                </h2>
              )}
              {open && (
                <ul className="space-y-0.5">
                  {section.items.map((item) => {
                    const subChildren =
                      item.to.endsWith('/channels') ? channelChildren
                      : item.to.endsWith('/dms') ? dmChildren
                      : item.to.endsWith('/projects') ? projectChildren
                      : null;
                    const subOpen = isSubItemOpen(item.to);
                    return (
                      <li key={item.to}>
                        <div className="flex items-center gap-1">
                          <NavLink
                            to={item.to}
                            end={item.end}
                            className={({ isActive }) => [
                              'flex flex-1 items-center justify-between rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                              isActive ? 'bg-brand-hover text-white' : 'text-text-primary hover:bg-bg-subtle',
                            ].join(' ')}
                          >
                            <span className="flex items-center gap-2">
                              <span aria-hidden="true" className="inline-flex h-4 w-4"><item.Icon /></span>
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
                          <ul className="ml-6 mt-0.5 space-y-0.5 border-l border-border-base pl-2" data-testid={`sidebar-subitem-list-${item.to}`}>
                            {subChildren.length === 0 && (
                              <li className="px-2 py-0.5 text-xs italic text-text-muted">(none)</li>
                            )}
                            {subChildren.map((child) => (
                              <li key={child.to}>
                                <div className="flex items-center gap-1">
                                  <NavLink
                                    to={child.to}
                                    className={({ isActive }) => [
                                      'block min-w-0 flex-1 truncate rounded px-2 py-0.5 text-xs',
                                      isActive ? 'bg-brand-hover text-white' : 'text-text-secondary hover:bg-bg-subtle hover:text-text-primary',
                                    ].join(' ')}
                                    data-testid="sidebar-subitem-link"
                                  >
                                    <span className="flex items-center justify-between gap-2">
                                      <span className="truncate">{child.label}</span>
                                      <UnreadBadge unreadCount={child.unreadCount} mentionCount={child.mentionCount} />
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
      {deleteConversation.isError && (
        <p className="mt-2 px-2 text-xs text-danger" data-testid="sidebar-dm-delete-error" role="alert">
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

// ============================================================================
// MobileSecondaryNavContent — renders the active module's nav inside a sheet
// ============================================================================
function MobileSecondaryNavContent({
  moduleId,
  orgBase,
}: {
  moduleId: ShellModuleId;
  orgBase: string;
}): React.ReactElement {
  const CustomNav = SECONDARY_NAV_REGISTRY[moduleId];
  if (CustomNav) {
    return <CustomNav orgBase={orgBase} />;
  }
  return <DefaultModuleNav moduleId={moduleId} orgBase={orgBase} />;
}

// ============================================================================
// RailConnectionStatus — SSE status indicator in the rail bottom
// ============================================================================
function RailConnectionStatus(): React.ReactElement {
  const status = useAppStore((s) => s.sseStatus);
  const dotColor =
    status === 'open' ? 'bg-success'
    : status === 'reconnecting' ? 'bg-warning'
    : 'bg-danger';
  const label =
    status === 'open' ? 'Connected'
    : status === 'reconnecting' ? 'Reconnecting…'
    : status === 'closed' ? 'Disconnected'
    : 'Idle';
  return (
    <div
      data-testid="rail-connection"
      data-status={status}
      title={label}
      className="flex items-center justify-center"
    >
      <span className={`inline-block h-2 w-2 rounded-full ${dotColor}`} />
    </div>
  );
}

// ============================================================================
// RailUserPanel — popout panel from the user avatar
// ============================================================================
function RailUserPanel({
  theme,
  onSetTheme,
  displayName,
  orgBase,
  onSignout,
  onClose,
}: {
  theme: Theme;
  onSetTheme: (t: Theme) => void;
  displayName?: string;
  orgBase: string;
  onSignout: () => void;
  onClose: () => void;
}): React.ReactElement {
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [onClose]);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [onClose]);

  return (
    <div
      ref={panelRef}
      data-testid="rail-user-panel"
      className="absolute bottom-12 left-14 z-50 w-56 rounded-lg border border-border-base bg-bg-elevated p-3 shadow-2"
      onKeyDown={(e) => { if (e.key === 'Escape') onClose(); }}
    >
      <div className="mb-2 text-sm font-medium text-text-primary">{displayName}</div>
      <Link
        to={`${orgBase}/me`}
        data-testid="rail-account-link"
        className="mb-2 block rounded px-2 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
      >
        Your account
      </Link>
      <div className="border-t border-border-base pt-2">
        <ThemeSegmented collapsed={false} theme={theme} onSetTheme={onSetTheme} />
      </div>
      <button
        type="button"
        data-testid="sidebar-signout"
        onClick={onSignout}
        className="mt-2 flex w-full items-center gap-2 rounded px-2 py-1.5 text-sm text-text-muted hover:bg-bg-subtle hover:text-danger motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4"><SignoutIcon /></span>
        Sign out
      </button>
    </div>
  );
}

// ============================================================================
// SidebarTop — ⌘K search (in col②). Org switcher moved to col① rail.
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

function SidebarTop({
  onOpenPalette,
}: {
  onOpenPalette: () => void;
}): React.ReactElement {
  const { t } = useTranslation('common');
  return (
    <div className="flex-shrink-0">
      <button
        type="button"
        onClick={onOpenPalette}
        aria-label={t('shell.search.aria')}
        data-testid="open-palette"
        className="flex w-full items-center gap-2 rounded-md border border-border-base bg-bg-elevated px-2 py-1.5 text-sm text-text-muted hover:bg-bg-subtle motion-safe:transition-colors"
      >
        <span aria-hidden="true" className="inline-flex h-4 w-4"><SearchIcon /></span>
        <span className="flex-1 text-left">{t('shell.search.label')}</span>
        <kbd className="rounded border border-border-base px-1 font-mono text-[0.6875rem] text-text-muted">⌘K</kbd>
      </button>
    </div>
  );
}

// ============================================================================
// RailOrgSwitcher — org switcher in the col① rail (always visible, icon-only)
// ============================================================================
function RailOrgSwitcher({ orgSwitcher }: { orgSwitcher: OrgSwitcherBinding }): React.ReactElement {
  const { currentOrg, orgs, currentSlug, fallbackName, open, onToggle, onClose, onCreateOrg, onOpenSettings } = orgSwitcher;
  const orgName = currentOrg?.name ?? fallbackName ?? '…';
  return (
    <div className="relative flex flex-col items-center">
      <button
        type="button"
        data-testid="org-switcher"
        onClick={onToggle}
        aria-expanded={open}
        aria-haspopup="true"
        title={orgName}
        className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-brand-hover text-white hover:opacity-90 motion-safe:transition-colors"
      >
        <OrgIcon />
      </button>
      {open && (
        <OrgDropdown orgs={orgs} currentSlug={currentSlug} onClose={onClose} onCreateOrg={onCreateOrg} onOpenSettings={onOpenSettings} />
      )}
    </div>
  );
}

// ============================================================================
// OrgDropdown + CreateOrgModal
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
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      const target = e.target as HTMLElement;
      if (!target.closest('[data-org-dropdown]')) onClose();
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [onClose]);

  const handleSwitch = (slug: string) => {
    if (slug !== currentSlug) navigate(`/organizations/${slug}`);
    onClose();
  };

  return (
    <div data-org-dropdown className="absolute left-full top-0 z-50 ml-2 w-48 rounded-md border border-border bg-bg-elevated shadow-[var(--shadow-2)]" role="menu">
      {orgs.map((o) => (
        <div key={o.id} className={`flex w-full items-center ${o.slug === currentSlug ? 'bg-bg-subtle' : ''}`}>
          <button
            type="button"
            role="menuitem"
            className={`flex min-w-0 flex-1 items-center gap-2 px-3 py-2 text-sm hover:bg-bg-subtle ${o.slug === currentSlug ? 'font-medium text-brand' : 'text-text-primary'}`}
            onClick={() => handleSwitch(o.slug)}
          >
            <OrgIcon /><span className="truncate">{o.name}</span>
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
            <SettingsIcon />
          </button>
        </div>
      ))}
      {orgs.length > 0 && <hr className="border-border" />}
      <button type="button" role="menuitem" onClick={onCreateOrg} className="flex w-full items-center gap-2 px-3 py-2 text-sm text-accent hover:bg-bg-subtle">
        <span aria-hidden="true">+</span><span>Create organization</span>
      </button>
    </div>
  );
}

function CreateOrgModal({ onClose }: { onClose: () => void }): React.ReactElement {
  const qc = useQueryClient();
  const navigate = useNavigate();
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const create = useMutation({
    mutationFn: () => orgApi.create({ name: name.trim() }),
    onSuccess: (newOrg) => {
      qc.invalidateQueries({ queryKey: ['orgs'] });
      onClose();
      navigate(`/organizations/${newOrg.slug}`);
    },
    onError: (err: Error) => setError(err.message),
  });
  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (!name.trim()) { setError('Please enter an organization name'); return; }
    create.mutate();
  };
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" role="dialog" aria-modal="true" aria-labelledby="create-org-title" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}>
      <div className="w-full max-w-sm rounded-xl bg-bg-elevated border border-border p-6 shadow-[var(--shadow-3)]">
        <h2 id="create-org-title" className="text-base font-semibold text-text-primary mb-4">Create organization</h2>
        {error && <div role="alert" className="mb-3 rounded bg-danger/10 border border-danger/30 px-3 py-2 text-sm text-danger">{error}</div>}
        <form onSubmit={handleSubmit} noValidate className="space-y-3">
          <div className="space-y-1">
            <label htmlFor="new-org-name" className="block text-sm text-text-primary">Organization name</label>
            <input id="new-org-name" type="text" value={name} onChange={(e) => setName(e.target.value)} className="w-full rounded border border-border px-3 py-1.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[var(--ring-color)] bg-bg-elevated text-text-primary" placeholder="My Organization" autoFocus />
          </div>
          <div className="flex gap-2 justify-end pt-1">
            <button type="button" onClick={onClose} className="rounded px-4 py-1.5 text-sm text-text-secondary hover:bg-bg-subtle">Cancel</button>
            <button type="submit" disabled={create.isPending} className="rounded bg-brand px-4 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50">{create.isPending ? 'Creating…' : 'Create'}</button>
          </div>
        </form>
      </div>
    </div>
  );
}

// ============================================================================
// ThemeSegmented — segmented Light | Dark control
// ============================================================================
function ThemeSegmented({ collapsed, theme, onSetTheme }: { collapsed: boolean; theme: Theme; onSetTheme: (t: Theme) => void }): React.ReactElement {
  const { t } = useTranslation();
  const options: ReadonlyArray<{ value: Theme; label: string; Icon: () => React.ReactElement }> = [
    { value: 'light', label: t('theme.light'), Icon: SunIcon },
    { value: 'dark', label: t('theme.dark'), Icon: MoonIcon },
  ];
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') { e.preventDefault(); onSetTheme('dark'); }
    else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') { e.preventDefault(); onSetTheme('light'); }
  };
  if (collapsed) {
    const next: Theme = theme === 'dark' ? 'light' : 'dark';
    return (
      <button type="button" data-testid="theme-toggle" aria-label={theme === 'dark' ? t('theme.switchToLight') : t('theme.switchToDark')} title={t('theme.toggleTitle')} onClick={() => onSetTheme(next)} className="inline-flex h-9 w-full items-center justify-center rounded-md text-text-secondary hover:bg-bg-subtle motion-safe:transition-colors">
        <span aria-hidden="true" className="inline-flex h-4 w-4">{theme === 'dark' ? <SunIcon /> : <MoonIcon />}</span>
      </button>
    );
  }
  return (
    <div role="radiogroup" aria-label={t('theme.label')} data-testid="theme-toggle" onKeyDown={onKeyDown} className="flex gap-1 rounded-md border border-border-base bg-bg-elevated p-0.5">
      {options.map((opt) => {
        const selected = theme === opt.value;
        return (
          <button key={opt.value} type="button" role="radio" aria-checked={selected} aria-label={opt.label} data-testid={`theme-segment-${opt.value}`} tabIndex={selected ? 0 : -1} onClick={() => onSetTheme(opt.value)} className={['flex flex-1 items-center justify-center gap-1.5 rounded px-2 py-1 text-xs font-medium motion-safe:transition-colors', selected ? 'bg-brand-hover text-white shadow-sm' : 'text-text-secondary hover:text-text-primary'].join(' ')}>
            <span aria-hidden="true" className="inline-flex h-3.5 w-3.5"><opt.Icon /></span>
            <span>{opt.label}</span>
          </button>
        );
      })}
    </div>
  );
}

// ============================================================================
// SVG Icons
// ============================================================================
function SidebarToggleIcon({ collapsed }: { collapsed: boolean }): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d={collapsed ? 'M8 5l5 5-5 5' : 'M12 5l-5 5 5 5'} strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function SunIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="10" cy="10" r="3" /><path d="M10 2v2M10 16v2M2 10h2M16 10h2M4.2 4.2l1.4 1.4M14.4 14.4l1.4 1.4M4.2 15.8l1.4-1.4M14.4 5.6l1.4-1.4" strokeLinecap="round" /></svg>);
}
function MoonIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M16.5 12a6.5 6.5 0 1 1-8.5-8.5 5.5 5.5 0 0 0 8.5 8.5z" strokeLinejoin="round" /></svg>);
}
function FolderIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M3 6.5A1.5 1.5 0 0 1 4.5 5h3l1.5 2h6.5A1.5 1.5 0 0 1 17 8.5v6A1.5 1.5 0 0 1 15.5 16h-11A1.5 1.5 0 0 1 3 14.5v-8z" strokeLinejoin="round" /></svg>);
}
function IssueIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="10" cy="10" r="6.5" /><circle cx="10" cy="10" r="1.5" fill="currentColor" stroke="none" /></svg>);
}
function TaskIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M4 6h6M4 10h6M4 14h4" strokeLinecap="round" /><path d="M13 6.5l1.5 1.5 2.5-3" strokeLinecap="round" strokeLinejoin="round" /></svg>);
}
function PlanIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><rect x="3" y="4" width="14" height="12" rx="1.5" /><path d="M7 8h6M7 12h4" strokeLinecap="round" /></svg>);
}
function HashIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M8 3 6.5 17M13.5 3 12 17M3.5 7h13M3 13h13" strokeLinecap="round" /></svg>);
}
// T575: workspace Repos nav icon — a git-branch glyph (two nodes joined by a fork).
function RepoIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="6" cy="5" r="1.8" /><circle cx="6" cy="15" r="1.8" /><circle cx="14" cy="7" r="1.8" /><path d="M6 6.8v6.4M6 11a6 6 0 0 1 6-3" strokeLinecap="round" /></svg>);
}
function ChatIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M4 5h12a1.5 1.5 0 0 1 1.5 1.5v6a1.5 1.5 0 0 1-1.5 1.5h-5l-3 3v-3H4A1.5 1.5 0 0 1 2.5 12.5v-6A1.5 1.5 0 0 1 4 5z" strokeLinejoin="round" /></svg>);
}
function FleetIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><rect x="2.5" y="6" width="6" height="8" rx="1" /><rect x="11.5" y="6" width="6" height="8" rx="1" /><path d="M5.5 9.5h0.01M14.5 9.5h0.01" strokeLinecap="round" /></svg>);
}
function AgentsIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><rect x="4" y="6" width="12" height="9" rx="2" /><path d="M7 6V4.5M13 6V4.5M8 10v.5M12 10v.5M7.5 13h5" strokeLinecap="round" /></svg>);
}
function SettingsIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="10" cy="10" r="2.5" /><path d="M10 3v2M10 15v2M3 10h2M15 10h2M5.05 5.05l1.4 1.4M13.55 13.55l1.4 1.4M5.05 14.95l1.4-1.4M13.55 6.45l1.4-1.4" strokeLinecap="round" /></svg>);
}
function VersionIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M3.5 8.5 8.5 3.5a1.5 1.5 0 0 1 2.1 0l5.9 5.9a1.5 1.5 0 0 1 0 2.1l-5 5a1.5 1.5 0 0 1-2.1 0L3.5 10.6V8.5z" strokeLinejoin="round" /><circle cx="7" cy="7" r="1.2" /></svg>);
}
function UsersIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="7.5" cy="7" r="2.5" /><path d="M2 16c0-3 2.5-5 5.5-5s5.5 2 5.5 5" strokeLinecap="round" /><path d="M13 8.5a2 2 0 1 0 0-4M18 16c0-2.5-2-4-4-4" strokeLinecap="round" /></svg>);
}
function TrashIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M4.5 6h11M8 6V4.5h4V6M7 8.5l.5 7h5l.5-7" strokeLinecap="round" strokeLinejoin="round" /></svg>);
}
function OrgIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-3.5 w-3.5 stroke-current" strokeWidth="1.5" aria-hidden="true"><rect x="3" y="10" width="5" height="7" rx="1" /><rect x="7.5" y="3" width="5" height="14" rx="1" /><rect x="12" y="7" width="5" height="10" rx="1" /></svg>);
}
function SearchIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><circle cx="8.5" cy="8.5" r="5" /><path d="M12.5 12.5 17 17" strokeLinecap="round" /></svg>);
}
function SignoutIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M12.5 6.5V4.5A1.5 1.5 0 0 0 11 3H5A1.5 1.5 0 0 0 3.5 4.5v11A1.5 1.5 0 0 0 5 17h6a1.5 1.5 0 0 0 1.5-1.5v-2" strokeLinecap="round" /><path d="M9 10h8M15 7.5l2.5 2.5-2.5 2.5" strokeLinecap="round" strokeLinejoin="round" /></svg>);
}
function ReminderIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M10 2.5V1M10 2.5C7 2.5 4.5 5 4.5 8c0 3.5-1.5 5-2 5.5h15c-.5-.5-2-2-2-5.5 0-3-2.5-5.5-5.5-5.5z" strokeLinecap="round" strokeLinejoin="round" /><path d="M8 15.5a2 2 0 0 0 4 0" strokeLinecap="round" /></svg>);
}
// Alerts (rail) — a warning triangle, not a bell. "Needs your attention" reads
// as an alert/warning signal, kept visually distinct from the Reminders bell
// (which a bell-with-ping was too easily confused with at rail size).
function AlertBellIcon(): React.ReactElement {
  return (<svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.5" aria-hidden="true"><path d="M10 3.3 17.2 16H2.8z" strokeLinejoin="round" /><path d="M10 8.5v3.2" strokeLinecap="round" /><circle cx="10" cy="13.9" r="0.6" className="fill-current stroke-none" /></svg>);
}
