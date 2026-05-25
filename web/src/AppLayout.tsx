import type React from 'react';
import { Suspense, useEffect, useState } from 'react';
import { NavLink, Outlet, useLocation } from 'react-router-dom';
import { SSEIndicator } from '@/sse/SSEIndicator';
import { useSSE } from '@/sse/useSSE';
import { useInputRequests } from '@/api/inputRequests';
import { PageSkeleton } from '@/components/Skeleton';

// AppLayout v2 — v2.3 P2. Three changes vs v2.2 shell:
//   1. Sidebar grouped into Conversations / Work / System (skill rule
//      `nav-hierarchy`: primary vs secondary nav must be visually
//      separated); each item has a Heroicons-style outline SVG.
//   2. Responsive: <768px collapses the sidebar into a drawer behind
//      a hamburger button in the header (skill rule `mobile-first`).
//   3. PageSkeleton replaces the "Loading…" plain-text fallback
//      (skill rule `progressive-loading`).
//
// No router change. No new dependency. Identity affordance + Home /
// Overview page land in P3.
export default function AppLayout(): React.ReactElement {
  useSSE();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const location = useLocation();

  // Auto-close the drawer on navigation so a tap on a nav item also
  // dismisses the overlay (common mobile pattern).
  useEffect(() => {
    setDrawerOpen(false);
  }, [location.pathname]);

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
          <span className="font-heading text-base font-semibold tracking-tight text-text-primary">
            agent-center
          </span>
        </div>
        <div className="flex items-center gap-3 sm:gap-4">
          <SSEIndicator />
          <span className="hidden text-xs text-text-muted sm:inline">
            v2 · loopback
          </span>
        </div>
      </header>
      <div className="flex flex-1 overflow-hidden">
        <Sidebar drawerOpen={drawerOpen} onDismiss={() => setDrawerOpen(false)} />
        <main className="flex-1 overflow-y-auto p-4 sm:p-6">
          <div className="mx-auto max-w-6xl">
            <Suspense fallback={<PageSkeleton />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
    </div>
  );
}

type NavBadgeKey = 'inputRequests' | null;

interface NavItem {
  to: string;
  label: string;
  badge?: NavBadgeKey;
  Icon: () => React.ReactElement;
}

interface NavSection {
  label: string;
  items: ReadonlyArray<NavItem>;
}

const navSections: ReadonlyArray<NavSection> = [
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
      { to: '/agents', label: 'Agents', Icon: AgentsIcon },
      { to: '/settings', label: 'Settings', Icon: SettingsIcon },
    ],
  },
];

function Sidebar({
  drawerOpen,
  onDismiss,
}: {
  drawerOpen: boolean;
  onDismiss: () => void;
}): React.ReactElement {
  // Derive the IR badge directly from server state so the count always
  // reflects pending IRs (not the number of SSE pushes received).
  const irs = useInputRequests();
  const inputRequestBadge = (irs.data ?? []).filter(
    (ir) => ir.status === 'pending',
  ).length;

  // Build the nav once; both the desktop sidebar and the drawer reuse it.
  const navTree = (
    <ul className="space-y-4">
      {navSections.map((section) => (
        <li key={section.label}>
          <h2 className="px-2 pb-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
            {section.label}
          </h2>
          <ul className="space-y-0.5">
            {section.items.map((item) => {
              const badgeCount =
                item.badge === 'inputRequests' ? inputRequestBadge : 0;
              return (
                <li key={item.to}>
                  <NavLink
                    to={item.to}
                    className={({ isActive }) =>
                      [
                        'flex items-center justify-between rounded px-2 py-1.5 text-sm motion-safe:transition-colors',
                        isActive
                          ? 'bg-brand text-white'
                          : 'text-text-primary hover:bg-bg-subtle',
                      ].join(' ')
                    }
                  >
                    <span className="flex items-center gap-2">
                      <span aria-hidden="true" className="inline-flex h-4 w-4">
                        <item.Icon />
                      </span>
                      <span>{item.label}</span>
                    </span>
                    {badgeCount > 0 && (
                      <span
                        className="rounded-full bg-accent px-1.5 py-0.5 text-xs font-medium text-white tabular-nums"
                        data-testid={`nav-badge-${item.badge}`}
                      >
                        {badgeCount}
                      </span>
                    )}
                  </NavLink>
                </li>
              );
            })}
          </ul>
        </li>
      ))}
    </ul>
  );

  return (
    <>
      {/* Desktop sidebar — always visible at ≥768px. */}
      <nav
        aria-label="primary"
        className="hidden w-52 flex-shrink-0 border-r border-border-base bg-bg-subtle p-3 md:block"
      >
        {navTree}
      </nav>
      {/* Mobile drawer — opens on hamburger toggle. */}
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
            // Stop bubbling so clicks inside don't dismiss the drawer.
            onClick={(e) => e.stopPropagation()}
          >
            {navTree}
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

function HamburgerIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-5 w-5 stroke-current" strokeWidth="1.75" aria-hidden="true">
      <path d="M3.5 5h13M3.5 10h13M3.5 15h13" strokeLinecap="round" />
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
