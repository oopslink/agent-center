import type React from 'react';

// ============================================================================
// v2.10.0 [T1] — col② per-module secondary-nav registry.
//
// The shell (AppLayout) owns the col② CHROME — header, footer (live / Light·Dark
// / Sign out), collapse, and the col④ host. The NAV BODY in the middle is owned
// per-module so the six module tasks (T2/T4/T5/T7/T8 + Conversations/Plan
// refinements) can each refine their own col② WITHOUT all editing AppLayout
// (the shared-state single-actor 命门 — six concurrent edits to one file).
//
// Contract for a module task:
//   1. Create web/src/shell/nav/<Module>SecondaryNav.tsx exporting a component
//      of type ModuleSecondaryNav (it receives { orgBase } and renders the col②
//      nav body — full structural control; use the design tokens, no raw colors).
//   2. Register it below: `conversations: ConversationsSecondaryNav`.
//   That's the ONLY shared edit (one distinct line → git auto-merges); AppLayout
//   itself stays untouched.
//
// A module with NO entry here falls back to the shell default — its `items`
// rendered via the built-in NavGroup (CAPS group header + the channel / DM /
// project expandable sub-lists with unread badges). So today, with the registry
// empty, every module keeps the exact T1 default behavior.
// ============================================================================

export type ShellModuleId = 'workspace' | 'conversations' | 'members' | 'system';

export interface ModuleSecondaryNavProps {
  /** The org base path ('' in isolated tests, '/organizations/:slug' live). */
  orgBase: string;
}

export type ModuleSecondaryNav = React.ComponentType<ModuleSecondaryNavProps>;

// Per-module col② overrides. Empty = every module uses the shell default.
// Module tasks add their entry here (and only here, besides their own file).
export const SECONDARY_NAV_REGISTRY: Partial<Record<ShellModuleId, ModuleSecondaryNav>> = {
  // conversations: ConversationsSecondaryNav,   // T2 (dev1)
  // members:       MembersSecondaryNav,         // T7 (dev3)
  // system:        SystemSecondaryNav,          // T8 (dev3)
};
