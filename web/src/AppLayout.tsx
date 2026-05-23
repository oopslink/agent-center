import type React from 'react';
import { Suspense } from 'react';
import { NavLink, Outlet } from 'react-router-dom';

// AppLayout — top nav + 6-section left sidebar + main content outlet.
// Sidebar sections per x9527 F3 oversight #1.
//
// The `<Suspense>` boundary wraps the outlet so lazy-loaded page chunks
// have a fallback while they stream in.
export default function AppLayout(): React.ReactElement {
  return (
    <div className="flex h-screen flex-col">
      <header className="flex h-12 items-center justify-between border-b border-slate-200 bg-white px-4">
        <span className="text-base font-semibold tracking-tight">
          agent-center
        </span>
        <span className="text-xs text-slate-500">v2 · loopback</span>
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

const navItems: ReadonlyArray<{ to: string; label: string }> = [
  { to: '/channels', label: 'Channels' },
  { to: '/dms', label: 'DMs' },
  { to: '/issues', label: 'Issues' },
  { to: '/tasks', label: 'Tasks' },
  { to: '/agents', label: 'Agents' },
  { to: '/settings', label: 'Settings' },
];

function Sidebar(): React.ReactElement {
  return (
    <nav
      aria-label="primary"
      className="w-48 flex-shrink-0 border-r border-slate-200 bg-slate-100 p-3"
    >
      <ul className="space-y-1">
        {navItems.map((item) => (
          <li key={item.to}>
            <NavLink
              to={item.to}
              className={({ isActive }) =>
                [
                  'block rounded px-3 py-1.5 text-sm transition-colors',
                  isActive
                    ? 'bg-slate-900 text-white'
                    : 'text-slate-700 hover:bg-slate-200',
                ].join(' ')
              }
            >
              {item.label}
            </NavLink>
          </li>
        ))}
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
