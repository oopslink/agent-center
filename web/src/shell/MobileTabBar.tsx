import type React from 'react';
import { Link } from 'react-router-dom';

// ============================================================================
// v2.10.1 [M1] MobileTabBar — the small-screen (<768) primary navigation.
//
// The desktop col① module rail reflows to a fixed BOTTOM tab bar on mobile
// (mockup `docs/design/v2.10.1/v2.10.1-mobile` — `.mtab`): the same four
// top-level modules (Workspace / Conversations / Members / System), each a
// full-width tap target. Tapping a tab navigates to that module's default page
// (its col② list, now full-screen); the desktop rail + col② are `md:`-only.
//
// The bar is mobile-only (`md:hidden`), pinned to the bottom edge with
// safe-area padding, and every tab is ≥44px tall (the v2.10.1 touch baseline).
// ============================================================================
export interface TabBarModule {
  id: 'workspace' | 'conversations' | 'members' | 'reminders' | 'system';
  label: string;
  short: string;
  defaultPath: string;
  Icon: () => React.ReactElement;
}

export function MobileTabBar({
  modules,
  activeModuleId,
  orgBase,
  conversationsUnread = 0,
  conversationsMentions = 0,
}: {
  modules: ReadonlyArray<TabBarModule>;
  activeModuleId?: TabBarModule['id'];
  orgBase: string;
  // T343: cross-source unread badge for the Chat (conversations) tab — mirrors the
  // desktop rail. conversationsMentions>0 → high-signal @me state (brand color).
  conversationsUnread?: number;
  conversationsMentions?: number;
}): React.ReactElement {
  return (
    <nav
      aria-label="modules mobile"
      data-testid="mobile-tabbar"
      className="fixed inset-x-0 bottom-0 z-30 flex border-t border-border-base bg-bg-elevated pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      {modules.map((m) => {
        const active = m.id === activeModuleId;
        return (
          // Plain Link (not NavLink): active is MODULE-level (driven by
          // activeModuleId) so sub-routes like /channels/:id still mark the
          // Conversations tab — NavLink's exact route match would not.
          <Link
            key={m.id}
            to={m.defaultPath ? `${orgBase}/${m.defaultPath}` : orgBase || '/'}
            aria-label={m.label}
            aria-current={active ? 'page' : undefined}
            data-testid={`tab-${m.id}`}
            data-active={active}
            className={[
              'relative flex min-h-[44px] flex-1 flex-col items-center justify-center gap-0.5 py-1.5 text-[0.625rem] font-medium leading-none motion-safe:transition-colors',
              active ? 'text-brand' : 'text-text-muted hover:text-text-secondary',
            ].join(' ')}
          >
            <span aria-hidden="true" className="inline-flex h-5 w-5">
              <m.Icon />
            </span>
            <span>{m.short}</span>
            {m.id === 'conversations' && conversationsUnread > 0 && (
              <span
                data-testid="tab-conversations-unread-badge"
                data-mention={conversationsMentions > 0 ? 'true' : 'false'}
                aria-label={
                  conversationsMentions > 0
                    ? `${conversationsMentions} conversations mention you`
                    : `${conversationsUnread} unread conversations`
                }
                className={[
                  'absolute right-[18%] top-0.5 inline-flex min-w-[1.05rem] items-center justify-center rounded-full px-1 text-[0.625rem] font-semibold leading-none tabular-nums text-white',
                  conversationsMentions > 0 ? 'bg-brand' : 'bg-status-slate-solid',
                ].join(' ')}
              >
                {conversationsUnread > 99 ? '99+' : conversationsUnread}
              </span>
            )}
          </Link>
        );
      })}
    </nav>
  );
}
