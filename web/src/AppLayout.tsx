import type React from 'react';
import { Suspense } from 'react';
import { NavLink, Outlet } from 'react-router-dom';
import { SSEIndicator } from '@/sse/SSEIndicator';
import { useSSE } from '@/sse/useSSE';
import { useInputRequests } from '@/api/inputRequests';

// AppLayout — top nav + 6-section left sidebar + main content outlet.
// Sidebar sections per x9527 F3 oversight #1.
//
// The `<Suspense>` boundary wraps the outlet so lazy-loaded page chunks
// have a fallback while they stream in. useSSE opens the single
// app-wide EventSource on mount; cleanup on unmount.
export default function AppLayout(): React.ReactElement {
  useSSE();
  return (
    <div className="flex h-screen flex-col">
      <header className="flex h-12 items-center justify-between border-b border-slate-200 bg-white px-4">
        <span className="text-base font-semibold tracking-tight">
          agent-center
        </span>
        <div className="flex items-center gap-4">
          <SSEIndicator />
          <span className="text-xs text-slate-500">v2 · loopback</span>
        </div>
      </header>
      <div className="flex flex-1 overflow-hidden">
        <Sidebar />
        <main className="flex-1 overflow-y-auto p-6">
          <Suspense fallback={<PageFallback />}>
            <Outlet />
          </Suspense>
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
}

const navItems: ReadonlyArray<NavItem> = [
  { to: '/channels', label: 'Channels' },
  { to: '/dms', label: 'DMs' },
  { to: '/issues', label: 'Issues' },
  { to: '/tasks', label: 'Tasks' },
  { to: '/inputrequests', label: 'Input Requests', badge: 'inputRequests' },
  { to: '/agents', label: 'Agents' },
  { to: '/settings', label: 'Settings' },
];

function Sidebar(): React.ReactElement {
  // Derive the IR badge directly from server state so the count always
  // reflects pending IRs (not the number of SSE pushes received). F5
  // invalidates this query on input_request.* events, so SSE still
  // drives realtime updates — just through the cache.
  const irs = useInputRequests();
  const inputRequestBadge = (irs.data ?? []).filter(
    (ir) => ir.status === 'pending',
  ).length;
  return (
    <nav
      aria-label="primary"
      className="w-48 flex-shrink-0 border-r border-slate-200 bg-slate-100 p-3"
    >
      <ul className="space-y-1">
        {navItems.map((item) => {
          const badgeCount =
            item.badge === 'inputRequests' ? inputRequestBadge : 0;
          return (
            <li key={item.to}>
              <NavLink
                to={item.to}
                className={({ isActive }) =>
                  [
                    'flex items-center justify-between rounded px-3 py-1.5 text-sm transition-colors',
                    isActive
                      ? 'bg-slate-900 text-white'
                      : 'text-slate-700 hover:bg-slate-200',
                  ].join(' ')
                }
              >
                <span>{item.label}</span>
                {badgeCount > 0 && (
                  <span
                    className="rounded-full bg-blue-600 px-1.5 py-0.5 text-xs font-medium text-white"
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
    </nav>
  );
}

function PageFallback(): React.ReactElement {
  return (
    <div
      className="text-sm text-slate-400"
      role="status"
      aria-live="polite"
      data-testid="page-fallback"
    >
      Loading…
    </div>
  );
}
