import type React from 'react';
import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';

// ============================================================================
// v2.10.0 [T1] col④ — the on-demand context panel.
//
// The three-column desktop shell (AppLayout) reserves a fourth column for an
// optional, view-specific context panel (participants, selected-item metadata,
// plan conversation, …). T1 owns only the *mechanism*; each module task
// (T2/T3/T6/T7) fills it with real content by rendering <ContextPanel> from
// inside its page (which renders into col③ via the router <Outlet>).
//
// Contract:
//   - A page renders `<ContextPanel> … </ContextPanel>`. Its children portal
//     into the shell's col④ host and the column is revealed.
//   - When the page unmounts (navigation), the panel unmounts → the column
//     collapses back to a three-column layout. No manual cleanup needed.
//   - Multiple <ContextPanel> instances from one view stack in render order.
//
// Implemented with a portal (not a state-node slot) so the panel content keeps
// the rendering page's own React context / hooks, and a small ref-count so the
// shell knows whether to allocate the column.
// ============================================================================

interface ContextPanelCtx {
  /** The col④ host element the panel portals into (null until mounted). */
  host: HTMLElement | null;
  /** Ref-count hooks so the shell can show/hide the column. */
  register: () => void;
  unregister: () => void;
  /** T184: whether the (desktop) col④ is fully collapsed/hidden, + a setter. */
  collapsed: boolean;
  setCollapsed: (v: boolean) => void;
  /** Mobile-only: whether the Context Panel bottom sheet is open. */
  mobileSheetOpen: boolean;
  openMobileSheet: () => void;
  closeMobileSheet: () => void;
}

const Ctx = createContext<ContextPanelCtx | null>(null);

// T184: collapse state is persisted so a fully-collapsed sidebar stays collapsed
// across navigation + reload (mirrors useResizablePanel's width persistence).
const COLLAPSE_KEY = 'ac.contextpanel.collapsed';

function readCollapsed(): boolean {
  try {
    return window.localStorage.getItem(COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

/**
 * T184: read + toggle the col④ collapsed state from inside panel content (e.g.
 * the sidebar's collapse button). Returns null outside the shell provider (so a
 * panel rendered standalone in a unit test simply omits the collapse affordance).
 */
export function useContextPanelCollapse(): { collapsed: boolean; setCollapsed: (v: boolean) => void } | null {
  const ctx = useContext(Ctx);
  if (!ctx) return null;
  return { collapsed: ctx.collapsed, setCollapsed: ctx.setCollapsed };
}

/**
 * Used by a page's own ⓘ button to open the mobile Context Panel sheet. Returns
 * null outside the shell provider so a page can call it unconditionally. Does
 * NOT register/unregister the panel content — that's still <ContextPanel>'s job;
 * this only controls sheet visibility.
 */
export function useContextPanelMobileTrigger(): { open: () => void } | null {
  const ctx = useContext(Ctx);
  if (!ctx) return null;
  return { open: ctx.openMobileSheet };
}

/**
 * Used by AppLayout (the shell). Owns the col④ host element and an open flag
 * derived from how many <ContextPanel>s are currently mounted.
 *
 * Returns `Provider` (wrap the shell with it), `setHost` (a stable callback
 * ref for the col④ host element), `value` (the context value), `open`
 * (whether any panel is mounted → allocate the column), and `collapsed` (T184:
 * the persisted fully-collapsed flag the shell uses to hide the column).
 */
export function useContextPanelController(): {
  Provider: typeof Ctx.Provider;
  value: ContextPanelCtx;
  setHost: (el: HTMLElement | null) => void;
  open: boolean;
  collapsed: boolean;
  mobileSheetOpen: boolean;
  closeMobileSheet: () => void;
} {
  const [host, setHost] = useState<HTMLElement | null>(null);
  const [count, setCount] = useState(0);
  const [collapsed, setCollapsedState] = useState<boolean>(readCollapsed);
  const setCollapsed = (v: boolean) => {
    setCollapsedState(v);
    try {
      window.localStorage.setItem(COLLAPSE_KEY, v ? '1' : '0');
    } catch {
      /* ignore persistence failures (private mode / quota) */
    }
  };
  const [mobileSheetOpen, setMobileSheetOpen] = useState(false);
  const openMobileSheet = () => setMobileSheetOpen(true);
  const closeMobileSheet = () => setMobileSheetOpen(false);
  const value = useMemo<ContextPanelCtx>(
    () => ({
      host,
      register: () => setCount((c) => c + 1),
      unregister: () => setCount((c) => Math.max(0, c - 1)),
      collapsed,
      setCollapsed,
      mobileSheetOpen,
      openMobileSheet,
      closeMobileSheet,
    }),
    [host, collapsed, mobileSheetOpen],
  );
  return {
    Provider: Ctx.Provider,
    value,
    setHost,
    open: count > 0,
    collapsed,
    mobileSheetOpen,
    closeMobileSheet,
  };
}

/**
 * Rendered by a page to fill col④ (the on-demand context panel). Returns null
 * outside the shell (e.g. a page mounted in isolation in a unit test with no
 * provider) so pages can always render it unconditionally.
 */
export function ContextPanel({ children }: { children: React.ReactNode }): React.ReactElement | null {
  const ctx = useContext(Ctx);
  useEffect(() => {
    if (!ctx) return;
    ctx.register();
    return () => ctx.unregister();
  }, [ctx]);
  if (!ctx?.host) return null;
  return createPortal(children, ctx.host);
}
