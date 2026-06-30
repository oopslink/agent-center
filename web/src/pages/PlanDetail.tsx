import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { OrgLink, orgPath, useOptionalOrgContext } from '@/OrgContext';
import { useProject } from '@/api/projects';
import { ApiError } from '@/api/client';
import {
  usePlan,
  useStartPlan,
  useStopPlan,
  useAddDependency,
  useRemoveDependency,
  useRemoveTaskFromPlan,
  useResumePausedNode,
  usePatchPlan,
  useDeletePlan,
  useArchivePlan,
  useUnmergedBranches,
  friendlyDestructivePlanError,
  type Plan,
  type PlanNode,
  type PlanNodeStatus,
  type PatchPlanInput,
} from '@/api/plans';
import { useConversation } from '@/api/conversations';
import { useAssignTask, useUnassignTask } from '@/api/tasks';
import {
  useDisplayNameResolver,
  useMembers,
  identityRefOf,
  normalizeIdentityRef,
  refKind,
  type MemberResult,
} from '@/api/members';
import { formatLocalTime } from '@/utils/time';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { Avatar } from '@/components/Avatar';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';
import { StatusChip, refLabel } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, AutoAdvancingIndicator, TaskArchivedBadge, planProgressLabel, PlanRefTag } from '@/components/planDisplay';
import { ConversationView } from '@/components/ConversationView';
import { ConversationSidebar, EmbeddedConversationSidebar, EmbeddedSidebarToggle } from '@/components/ConversationSidebar';
import { ContextPanel } from '@/shell/contextPanel';
import { SenderSidebarProvider, useSenderSidebar } from '@/components/SenderSidebarContext';
import { SenderDetailSidebar } from '@/components/SenderDetailSidebar';
import type { Participant } from '@/api/types';
import { useIsMobile } from '@/components/WorkItemMobileMeta';
import { TaskTitleLink } from '@/components/TaskTitleLink';
import { RelatedIssuesBlock } from '@/components/RelatedIssuesBlock';
import { dependencyEdgeError, validDropTargets } from './planDagEdit';

// PlanDetail (/projects/:id/plans/:planId) — v2.9 Plan-Orchestration EXECUTION
// view (#287). The mockup's ② Plan Detail: a header (name + status + failed +
// meta + Start/Stop), two tabs (DAG「推进计划」 / Task list「任务列表 N」 — NO
// backlog; selection lives on the #291 Work Board), a grid of the DAG (main) +
// the Plan conversation (side ~330px). The #286 backlog→Plan SELECTION is
// REMOVED here entirely.
//
// node_status is DERIVED by the orchestrator (§9.2) — we DISPLAY it, never store
// or edit it. This view renders the derived DAG + header lifecycle controls
// (Start / Stop / Advance, each once).
//
// v2.9 Stage A1 (#287 fast-follow): a DRAFT plan's DAG is now EDITABLE here — a
// draft-only dependency-edge editor (add via labeled selects, remove via a list
// of edges) sits below the graph. The backend AddPlanDependency/RemovePlanDependency
// are draft-gated (§9.4: running/done → ErrPlanNotDraft), cycle-guarded
// (ErrPlanCycle) and self-edge-rejected (ErrSelfDependency); the editor is gated
// to draft to match, and a running/done plan stays DISPLAY-ONLY.

// v2.9.1 UX point 4: chat / DAG / Task list as three independent tabs (default
// chat). Replaces the prior DAG|Task two-tab + resizable chat side-splitter.
type Tab = 'chat' | 'dag' | 'tasks';

// (v2.9.1 point 4) The DAG↔chat resizable side-splitter (v2.9 Stage A8) was
// removed — chat is now a top-level tab (see the 3-tab layout above), so the
// chat-width state / localStorage persistence / lg-breakpoint splitter are gone.

export default function PlanDetail(): React.ReactElement {
  const { id = '', planId = '' } = useParams<{ id: string; planId: string }>();
  const project = useProject(id);
  const plan = usePlan(id, planId);
  // T184: the plan conversation gets the shared col④ sidebar too. Resolve it for
  // participants (enabled:!!id makes this a no-op until the plan loads).
  const planConv = useConversation(plan.data?.conversation_id);
  // T324: desktop embeds the conversation sidebar inside the chat tab; mobile
  // keeps it in the col④ bottom sheet (mounted below for mobile only).
  const isMobile = useIsMobile();
  const [tab, setTab] = useState<Tab>('chat');
  // T347: chat maximize state lives here so the toggle can sit on the tab row.
  const [chatMaximized, setChatMaximized] = useState(false);
  // T348: DAG compact (zoom-to-fit) state lifted here too, so its toggle becomes an
  // icon on the tab row (next to maximize) instead of a text button on the canvas.
  const [dagCompact, setDagCompact] = useState(false);

  const projectName = project.data?.name ?? id;

  if (plan.isLoading) {
    return (
      <section className="space-y-3" data-testid="page-PlanDetail">
        <Skeleton width="16rem" height="1.75rem" />
        <Skeleton height="8rem" />
      </section>
    );
  }
  if (plan.isError) {
    return (
      <section className="space-y-3" role="alert" data-testid="page-PlanDetail">
        <ErrorState
          message="Couldn't load this plan."
          error={plan.error}
          testId="plan-not-found"
        />
        <OrgLink
          to={`/projects/${encodeURIComponent(id)}/plans`}
          className="text-xs text-accent hover:underline"
        >
          ← Back to plans
        </OrgLink>
      </section>
    );
  }
  if (!plan.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-PlanDetail">
        Plan lookup failed.
      </section>
    );
  }

  const p = plan.data;

  return (
    <section
      className="-mx-4 -mt-2 flex min-h-0 flex-1 flex-col px-4 pt-2 md:mx-0 md:mt-0 md:gap-4 md:px-0 md:pt-0"
      data-testid="page-PlanDetail"
      data-plan-id={p.id}
    >
      <div className="hidden md:block">
        <Breadcrumb
          items={[
            { label: 'Projects', to: '/projects' },
            { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
            { label: 'Plans', to: `/projects/${encodeURIComponent(id)}/plans` },
            { label: p.name },
          ]}
        />
      </div>

      {/* T341: the card is height-bounded (flex-1) + overflow-hidden so its rounded
          border stays crisp and the CHAT fills the remaining height with its
          composer pinned at the bottom (reachable inline — T340's grow-with-content
          had pushed the composer off-screen). The chat body drops the min-h-[60vh]
          floor (which had spilled past the border) — a bounded card makes flex-1
          resolve correctly. Maximize (added on the chat) is the full-screen escape. */}
      <div className="flex min-h-0 flex-1 flex-col overflow-hidden md:flex-row md:rounded-lg md:border md:border-border-base md:bg-bg-elevated md:shadow-1" data-testid="plan-detail-card">
        {/* Two-pane (双栏方案 B): MOBILE keeps the full single-row header above one
            column; DESKTOP splits into [left: slim title + tabs + content] | [right:
            PlanInfoRail with status/goal/progress/up-next/participants/files]. */}
        {isMobile && <PlanDetailHeader projectId={id} plan={p} />}

        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {!isMobile && <PlanTitleBar plan={p} />}

        {/* v2.13.0 / I18 F4 — the ship-gate board (cycle Integrate nodes not yet
            merged). @oopslink: on DESKTOP it now lives in the right-hand PlanInfoRail;
            MOBILE (no rail) keeps it in-flow here. Renders only when there is unmerged
            work to reconcile (a non-cycle / fully-merged plan shows nothing). */}
        {isMobile && <UnmergedBranchesPanel projectId={id} planId={p.id} />}

        {/* Tabs — Chat (default) / DAG / Task List. English-only labels (T132:
            the prior「(中文)」括注 removed). NO backlog tab (planning is on the
            Board). v2.9.1 point 4. */}
        {/* T328: the plan id (P27) sits on the tab row (right-aligned, into the
            empty space) — @oopslink — instead of a separate "P27 · chat" sub-header
            row inside the chat tab, saving a row (esp. on mobile). */}
        <div className="flex items-center gap-1 border-b border-border-base px-3 pt-2 md:px-6" data-testid="plan-tabs">
          <div className="flex min-w-0 items-center gap-1" role="tablist">
            <TabButton id="chat" active={tab === 'chat'} onSelect={setTab}>
              Chat
            </TabButton>
            <TabButton id="dag" active={tab === 'dag'} onSelect={setTab}>
              DAG
            </TabButton>
            <TabButton id="tasks" active={tab === 'tasks'} onSelect={setTab}>
              Task List
            </TabButton>
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-2 pl-2">
            {/* T347: the chat maximize toggle lives on the tab row (was floating
                above the chat body). Only meaningful on the Chat tab. */}
            {tab === 'chat' && (
              <button
                type="button"
                onClick={() => setChatMaximized((m) => !m)}
                data-testid="plan-chat-maximize"
                aria-pressed={chatMaximized}
                aria-label={chatMaximized ? 'Restore chat' : 'Maximize chat'}
                title={chatMaximized ? 'Restore (Esc)' : 'Maximize chat'}
                className="inline-flex h-11 w-11 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent md:h-7 md:w-7"
              >
                {chatMaximized ? <PlanChatRestoreIcon /> : <PlanChatMaximizeIcon />}
              </button>
            )}
            {/* T348: DAG compact (zoom-to-fit) toggle — icon on the tab row, mirroring
                the chat maximize button. Desktop-only (the mobile DAG is a stepper). */}
            {tab === 'dag' && (
              <button
                type="button"
                onClick={() => setDagCompact((c) => !c)}
                data-testid="plan-dag-compact-toggle"
                aria-pressed={dagCompact}
                aria-label={dagCompact ? 'Reset DAG zoom' : 'Compact DAG (zoom to fit)'}
                title={dagCompact ? 'Reset zoom' : 'Compact (zoom to fit)'}
                className={`hidden h-11 w-11 items-center justify-center rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent md:inline-flex md:h-7 md:w-7 ${
                  dagCompact
                    ? 'text-accent'
                    : 'text-text-muted hover:bg-bg-subtle hover:text-text-primary'
                }`}
              >
                <PlanCompactIcon />
              </button>
            )}
            {p.org_ref && (
              <span
                className="truncate font-mono text-xs font-semibold text-text-muted"
                data-testid="plan-conversation-code"
                title={p.name}
              >
                {p.org_ref}
              </span>
            )}
          </div>
        </div>

        {/* Single tabbed content area (point 4: chat is now a tab, not a side
            splitter). Chat stays mounted-but-hidden across tabs so its SSE
            subscription + scroll/composer-draft survive; DAG/Task mount lazily
            when their tab is active. */}
        <div className="flex min-h-0 flex-1 flex-col p-0" data-testid="plan-detail-content">
          {/* Chat stays mounted-but-hidden across tabs (SSE/scroll/draft survive).
              When active it must FILL the card height so the message stream scrolls
              INSIDE the viewport instead of growing the page (T180). The flex
              classes are applied only when active — a `display:flex` utility would
              otherwise override the `hidden` attribute's `display:none`. */}
          <div
            role="tabpanel"
            hidden={tab !== 'chat'}
            data-testid="plan-panel-chat"
            className={tab === 'chat' ? 'flex min-h-0 flex-1 flex-col' : undefined}
          >
            <PlanConversationSide
              conversationId={p.conversation_id}
              maximized={chatMaximized}
              onToggleMaximize={() => setChatMaximized((m) => !m)}
            />
          </div>
          {/* T341: the card is overflow-hidden + height-bounded, so the DAG /
              Task-List panels (tall content) must scroll INSIDE the card — give the
              active panel min-h-0 flex-1 overflow-auto (else the content is clipped
              and unscrollable, esp. on mobile — 'DAG 翻不了了' @oopslink).
              T579: on DESKTOP the panel becomes a flex column (overflow-hidden) so the
              DAG canvas can flex-grow to FILL the pane height (no more fixed 480px box
              floating with dead space below — @oopslink). Mobile keeps overflow-auto
              for the vertical stepper. */}
          <div
            role="tabpanel"
            hidden={tab !== 'dag'}
            data-testid="plan-panel-dag"
            className={
              tab === 'dag'
                ? 'min-h-0 flex-1 overflow-auto p-3 md:flex md:flex-col md:overflow-hidden md:p-4'
                : undefined
            }
          >
            {tab === 'dag' && <PlanDag projectId={id} plan={p} compact={dagCompact} />}
          </div>
          <div
            role="tabpanel"
            hidden={tab !== 'tasks'}
            data-testid="plan-panel-tasks"
            className={tab === 'tasks' ? 'min-h-0 flex-1 overflow-auto p-3 md:p-4' : undefined}
          >
            {tab === 'tasks' && <PlanTaskList projectId={id} plan={p} />}
          </div>
        </div>
        </div>

        {!isMobile && (
          <PlanInfoRail
            projectId={id}
            plan={p}
            participants={planConv.data?.participants ?? []}
            onOpenDag={() => setTab('dag')}
          />
        )}
      </div>

      {/* T324: MOBILE keeps the plan conversation's Participants/Threads/Files in
          the col④ bottom sheet; DESKTOP embeds it inside the Chat tab (right pane,
          in PlanConversationSide), so mount the col④ panel for mobile only. */}
      {planConv.data && isMobile && (
        <ContextPanel>
          <ConversationSidebar
            conversationId={planConv.data.id}
            participants={planConv.data.participants ?? []}
          />
        </ContextPanel>
      )}
    </section>
  );
}

// ── Header ────────────────────────────────────────────────────────────────
function PlanDetailHeader({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  const start = useStartPlan(projectId, plan.id);
  const stop = useStopPlan(projectId, plan.id);
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState<null | 'delete' | 'archive'>(null);
  const [actionsOpen, setActionsOpen] = useState(false);
  const [goalOpen, setGoalOpen] = useState(false);
  const goalLong = plan.description.trim().length > 80 || plan.description.includes('\n');
  const isMobile = useIsMobile();
  // Mobile: goal is hidden behind "Show goal" in the actions dropdown.
  const [mobileGoalOpen, setMobileGoalOpen] = useState(false);

  const creatorName = resolveName(plan.creator_ref);
  const creatorLabel =
    creatorName === plan.creator_ref ? normalizeIdentityRef(plan.creator_ref) : creatorName;

  // Destructive-action entry gate (PD bar 1 — UX, not security): Delete + Archive
  // are exposed ONLY for a NON-running, NON-archived plan. A running plan would
  // be rejected by the backend (409 plan_conflict) — the real boundary — so we
  // simply HIDE the entries rather than offer an action that can't succeed. An
  // archived plan is TERMINAL (re-archive/delete-after-archive aren't part of the
  // flow) so it shows read-only: no Delete / Archive.
  const canDestroy = plan.status !== 'running' && plan.status !== 'archived';

  return (
    <header className="space-y-2 px-3 py-2 md:border-b md:border-border-base md:px-6 md:py-3" data-testid="plan-detail-header">
      {/* Mobile: single row — ref + title + status + progress + creator + actions */}
      <div className="flex items-center gap-2">
        <PlanRefTag planId={plan.id} orgRef={plan.org_ref} testId="plan-detail-ref" />
        <h1 className="min-w-0 truncate font-heading text-lg font-semibold text-text-primary md:text-xl" title={plan.id}>
          {plan.name}
        </h1>
        {isMobile && (
          <span className="ml-auto flex items-center gap-1.5 text-xs text-text-muted">
            <PlanStatusChip status={plan.status} />
            <PlanFailedIndicator hasFailed={plan.has_failed} />
            <span data-testid="plan-progress">{planProgressLabel(plan.progress)}</span>
            <span title={plan.creator_ref}>@{creatorLabel}</span>
            <span className="relative">
              <button
                type="button"
                onClick={() => setActionsOpen((o) => !o)}
                aria-expanded={actionsOpen}
                data-testid="plan-actions-toggle"
                className="inline-flex min-h-[2.75rem] items-center gap-1 rounded-full border border-border-base bg-bg-subtle px-2.5 text-xs font-medium text-text-secondary whitespace-nowrap"
              >
                Actions <span aria-hidden="true">▾</span>
              </button>
              {actionsOpen && (
                <div className="absolute right-0 top-full z-20 mt-1 w-44 flex-col rounded-lg border border-border-base bg-bg-elevated p-1 shadow-2" data-testid="plan-actions" role="menu">
                  {plan.description.trim() !== '' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setMobileGoalOpen((v) => !v); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">
                      {mobileGoalOpen ? 'Hide goal' : 'Show goal'}
                    </button>
                  )}
                  {plan.status === 'running' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); stop.mutate(); }} disabled={stop.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50">Stop</button>
                  )}
                  {plan.status !== 'archived' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setEditing(true); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">Edit</button>
                  )}
                  {plan.status === 'draft' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); start.mutate(); }} disabled={start.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50">Start</button>
                  )}
                  {canDestroy && (
                    <>
                      <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('archive'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">Archive</button>
                      <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('delete'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle">Delete</button>
                    </>
                  )}
                </div>
              )}
            </span>
          </span>
        )}
      </div>
      {/* Row 2 (desktop only): status chips + inline meta + actions. The
          Progress/Creator meta rides on THIS row (was a separate 4th header row)
          so the header stays to 3 rows and the chat gets the height back. */}
      <div className={`${isMobile ? 'hidden' : 'flex'} flex-wrap items-center gap-x-3 gap-y-2`}>
        <PlanStatusChip status={plan.status} />
        {plan.status === 'running' && <AutoAdvancingIndicator variant="detail" />}
        <PlanFailedIndicator hasFailed={plan.has_failed} />
        <dl className="flex items-center gap-x-3 text-xs text-text-muted" data-testid="plan-detail-meta">
          <div className="flex items-center gap-1">
            <dt className="uppercase tracking-wide text-[0.625rem]">Progress</dt>
            <dd className="text-text-secondary" data-testid="plan-progress">{planProgressLabel(plan.progress)}</dd>
          </div>
          {plan.target_date && (
            <div className="flex items-center gap-1">
              <dt className="uppercase tracking-wide text-[0.625rem]">Target</dt>
              <dd className="text-text-secondary" title={plan.target_date}>{formatLocalTime(plan.target_date)}</dd>
            </div>
          )}
          <div className="flex items-center gap-1">
            <dt className="uppercase tracking-wide text-[0.625rem]">Creator</dt>
            <dd className="text-text-secondary" title={plan.creator_ref} data-testid="plan-creator">@{creatorLabel}</dd>
          </div>
        </dl>
        <span className="flex-1" />
        {/* T341: on MOBILE the action buttons collapse into an "Actions ▾" dropdown
            (@oopslink); on DESKTOP the wrapper dissolves (md:contents) and the menu
            is always shown inline (md:flex md:static) regardless of the toggle. */}
        <div className="relative md:contents" data-testid="plan-actions">
          <button
            type="button"
            onClick={() => setActionsOpen((o) => !o)}
            aria-expanded={actionsOpen}
            data-testid="plan-actions-toggle"
            className="inline-flex min-h-[2.75rem] items-center gap-1 rounded-full border border-border-base bg-bg-subtle px-3 text-xs font-medium text-text-secondary whitespace-nowrap md:hidden"
          >
            Actions <span aria-hidden="true">▾</span>
          </button>
          <div
            className={`${actionsOpen ? 'flex' : 'hidden'} absolute right-0 top-full z-20 mt-1 w-44 flex-col rounded-lg border border-border-base bg-bg-elevated p-1 shadow-2 md:relative md:mt-0 md:flex md:w-auto md:flex-row md:items-center md:gap-2 md:border-0 md:bg-transparent md:p-0 md:shadow-none`}
          >
        {/* Lifecycle (§9.4 / §9.6): running → Advance (dispatch ready) + Stop
            (→ draft); draft → Start. Each control is rendered exactly ONCE here
            (the DAG footer keeps the legend only). */}
        {plan.status === 'running' && (
          // §9.6: a running plan auto-advances; the manual "Advance now" override
          // was removed (@oopslink) — Stop (→ draft) is the only running control.
          <button
            type="button"
            data-testid="plan-stop-btn"
            disabled={stop.isPending}
            onClick={() => { setActionsOpen(false); stop.mutate(); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50 md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base"
          >
            ■ Stop (→ draft)
          </button>
        )}
        {/* T238: name + goal are DESCRIPTIVE metadata — editable in any
            non-archived status (draft/running/done). target_date stays draft-only
            (the modal hides it off-draft, and the backend rejects it). An archived
            plan is terminal/read-only, so no Edit. */}
        {plan.status !== 'archived' && (
          <button
            type="button"
            data-testid="plan-edit-btn"
            onClick={() => { setActionsOpen(false); setEditing(true); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base md:hover:text-text-primary"
          >
            Edit
          </button>
        )}
        {plan.status === 'draft' && (
          <button
            type="button"
            data-testid="plan-start-btn"
            disabled={start.isPending}
            onClick={() => { setActionsOpen(false); start.mutate(); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50 md:min-h-0 md:w-auto md:rounded md:border-0 md:bg-accent md:px-3 md:py-1.5 md:text-xs md:text-white md:hover:opacity-90"
          >
            ▸ Start
          </button>
        )}
        {/* Destructive lifecycle (v2.9 Stage B): Archive + Delete. Exposed only
            for a NON-running, NON-archived plan (canDestroy). Each opens a
            CONSEQUENCE-explaining confirm modal — never acts on a single click.
            The real block on a running plan is the backend 409; hiding here is
            the UX gate. */}
        {canDestroy && (
          <>
            <button
              type="button"
              data-testid="plan-archive-btn"
              onClick={() => { setActionsOpen(false); setConfirming('archive'); }}
              title="Archive this plan and all its tasks (terminal, cannot be undone)"
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base md:hover:text-text-primary"
            >
              Archive
            </button>
            <button
              type="button"
              data-testid="plan-delete-btn"
              onClick={() => { setActionsOpen(false); setConfirming('delete'); }}
              title="Delete this plan (unloads its tasks to the Backlog, cannot be undone)"
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-danger md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-danger md:hover:bg-bg-base"
            >
              Delete
            </button>
          </>
        )}
          </div>
        </div>
      </div>
      {isMobile && (
        <>
          {/* Mobile goal panel (toggled from Actions) */}
          {mobileGoalOpen && plan.description.trim() !== '' && (
            <div className="relative rounded-lg border border-border-base bg-bg-elevated p-3">
              <button type="button" onClick={() => setMobileGoalOpen(false)} aria-label="Close goal" className="absolute right-2 top-2 inline-flex h-8 w-8 items-center justify-center rounded-full text-text-muted hover:bg-bg-subtle hover:text-text-primary">
                <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="h-4 w-4" aria-hidden="true"><path strokeLinecap="round" d="M5 5l10 10M15 5L5 15" /></svg>
              </button>
              <p className="whitespace-pre-wrap text-sm text-text-secondary" data-testid="plan-goal">{plan.description}</p>
            </div>
          )}
        </>
      )}
      {/* Desktop: goal + meta (hidden on mobile) */}
      {!isMobile && plan.description.trim() !== '' && (
        <div data-testid="plan-goal-wrap">
          <p
            className={`whitespace-pre-wrap text-sm text-text-secondary ${
              goalLong && !goalOpen ? 'line-clamp-1' : ''
            }`}
            data-testid="plan-goal"
            title="Plan goal"
          >
            {plan.description}
          </p>
          {goalLong && (
            <button
              type="button"
              onClick={() => setGoalOpen((v) => !v)}
              data-testid="plan-goal-toggle"
              aria-expanded={goalOpen}
              className="mt-0.5 text-xs font-medium text-accent hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              {goalOpen ? 'Show less' : 'Show more'}
            </button>
          )}
        </div>
      )}
      {(start.isError || stop.isError) && (
        <p className="text-xs text-danger" data-testid="plan-lifecycle-error">
          {((start.error ?? stop.error) as Error).message}
        </p>
      )}
      {editing && (
        <PlanEditModal projectId={projectId} plan={plan} onClose={() => setEditing(false)} />
      )}
      {confirming === 'delete' && (
        <PlanDeleteModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
      )}
      {confirming === 'archive' && (
        <PlanArchiveModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
      )}
    </header>
  );
}

// PlanTitleBar — the slim DESKTOP header for the two-pane layout: just the plan
// ref + name. All the status/actions/meta that used to crowd the header now live
// in the right-hand PlanInfoRail (@oopslink 双栏方案 B). Mobile keeps the full
// single-row PlanDetailHeader instead.
function PlanTitleBar({ plan }: { plan: Plan }): React.ReactElement {
  return (
    <div
      className="flex items-center gap-2 px-5 py-3"
      data-testid="plan-title-bar"
    >
      <PlanRefTag planId={plan.id} orgRef={plan.org_ref} testId="plan-detail-ref" />
      <h1
        className="min-w-0 truncate font-heading text-lg font-semibold text-text-primary md:text-xl"
        title={plan.id}
      >
        {plan.name}
      </h1>
    </div>
  );
}

// PlanProgressBar — a slim horizontal bar for the rail's Progress section (saves
// vertical/horizontal space vs a donut in the narrow rail — @oopslink). The fill
// uses the success token so it flips per mode.
function PlanProgressBar({ done, total }: { done: number; total: number }): React.ReactElement {
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div data-testid="plan-progress-bar" aria-label={`${pct}% complete`}>
      <div className="mb-1.5 flex items-baseline justify-between">
        <span className="text-sm font-bold text-text-primary" data-testid="plan-progress">
          {planProgressLabel({ done, total })}
        </span>
        <span className="text-xs text-text-muted">{pct}%</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded-full" style={{ background: 'var(--color-bg-subtle)' }}>
        <div className="h-full rounded-full transition-[width]" style={{ width: `${pct}%`, background: 'var(--color-success)' }} />
      </div>
    </div>
  );
}

// nodeDotColor — the status dot beside an "Up next" node row. Up next only ever
// lists non-terminal nodes (done/failed are filtered out), so this maps the live
// states: running/dispatched (active) → accent, ready → secondary, blocked/paused
// → muted.
function nodeDotColor(status: PlanNodeStatus): string {
  switch (status) {
    case 'running':
    case 'dispatched':
      return 'var(--color-accent)';
    case 'ready':
      return 'var(--color-text-secondary)';
    default: // blocked | paused
      return 'var(--color-text-muted)';
  }
}

// PlanInfoRail — the DESKTOP right-hand information rail (双栏方案 B). It owns the
// status + lifecycle controls + goal + progress + up-next + participants + the
// conversation's Threads/Files panel — everything that used to live in the wide
// header. Self-contained: it drives its own lifecycle hooks + confirm modals and
// opens the EXISTING agent-activity sidebar (SenderDetailSidebar, unchanged) via
// local state when the @creator tag or a participant avatar is clicked.
function PlanInfoRail({
  projectId,
  plan,
  participants,
  onOpenDag,
}: {
  projectId: string;
  plan: Plan;
  participants: Participant[];
  onOpenDag?: () => void;
}): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  const start = useStartPlan(projectId, plan.id);
  const stop = useStopPlan(projectId, plan.id);
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState<null | 'delete' | 'archive'>(null);
  const [goalOpen, setGoalOpen] = useState(false);
  // The agent-activity sidebar (SenderDetailSidebar, unchanged) — opened by the
  // @creator tag / a participant avatar. Local state keeps the rail decoupled
  // from the chat's own SenderSidebarProvider.
  const [agentRef, setAgentRef] = useState<string | null>(null);

  const goalLong = plan.description.trim().length > 80 || plan.description.includes('\n');
  const canDestroy = plan.status !== 'running' && plan.status !== 'archived';

  const creatorName = resolveName(plan.creator_ref);
  const creatorLabel =
    creatorName === plan.creator_ref ? normalizeIdentityRef(plan.creator_ref) : creatorName;

  const nodes = plan.nodes ?? plan.nodes_preview ?? [];
  const upNext = nodes
    .filter((n) => !n.archived && n.node_status !== 'done' && n.node_status !== 'failed')
    .slice(0, 6);
  const upNextHidden = Math.max(
    0,
    nodes.filter((n) => !n.archived && n.node_status !== 'done' && n.node_status !== 'failed').length - upNext.length,
  );

  const railBtn =
    'flex-1 rounded-lg border border-border-strong bg-bg-subtle px-3 py-2 text-center text-xs font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary disabled:opacity-50';

  return (
    <aside
      className="hidden w-[360px] shrink-0 flex-col overflow-y-auto border-l border-border-base bg-bg-base/40 md:flex"
      data-testid="plan-info-rail"
    >
      {/* @oopslink: the unmerged-branch ship-gate board moved into the rail. Sits at
          the very top as an alert (above status/progress); renders nothing when there
          is no unmerged work, so it costs no space on a clean plan. */}
      <UnmergedBranchesPanel projectId={projectId} planId={plan.id} className="m-4 mb-0" />

      {/* Status + Progress (merged, top of rail) — the plan STATUS chip and the
          PROGRESS bar now share ONE block at the very TOP of the rail (@oopslink:
          进度放到顶端 + plan 状态和进度合并到一起). The standalone Progress section
          that used to sit below Participants was removed. Owns plan-detail-meta. */}
      <div className="space-y-3 border-b border-border-base p-5" data-testid="plan-detail-meta">
        <div className="flex flex-wrap items-center gap-2">
          <PlanStatusChip status={plan.status} />
          {plan.status === 'running' && <AutoAdvancingIndicator variant="detail" />}
          <PlanFailedIndicator hasFailed={plan.has_failed} />
          <span className="ml-auto text-xs text-text-muted">nodes done</span>
        </div>
        <PlanProgressBar done={plan.progress.done} total={plan.progress.total} />
        {plan.target_date && (
          <p className="text-xs text-text-muted">
            <span className="uppercase tracking-wide">Target</span>{' '}
            <span className="text-text-secondary" title={plan.target_date}>{formatLocalTime(plan.target_date)}</span>
          </p>
        )}
        {/* T570: all lifecycle + edit + destructive actions sit on ONE compact row
            (was two stacked rows). flex-wrap keeps them on a single line when they
            fit (Edit · Archive · Delete in the 360px rail) and only wraps if the
            running/draft Start/Stop button is also present. */}
        <div className="flex flex-wrap gap-2">
          {plan.status === 'running' && (
            <button
              type="button"
              data-testid="plan-stop-btn"
              disabled={stop.isPending}
              onClick={() => stop.mutate()}
              className={`${railBtn} text-danger hover:text-danger`}
            >
              ■ Stop (→ draft)
            </button>
          )}
          {plan.status === 'draft' && (
            <button
              type="button"
              data-testid="plan-start-btn"
              disabled={start.isPending}
              onClick={() => start.mutate()}
              className="flex-1 rounded-lg border-0 bg-accent px-3 py-2 text-center text-xs font-semibold text-white hover:opacity-90 disabled:opacity-50"
            >
              ▸ Start
            </button>
          )}
          {plan.status !== 'archived' && (
            <button type="button" data-testid="plan-edit-btn" onClick={() => setEditing(true)} className={railBtn}>
              Edit
            </button>
          )}
          {canDestroy && (
            <>
              <button type="button" data-testid="plan-archive-btn" onClick={() => setConfirming('archive')} className={railBtn}>
                Archive
              </button>
              <button
                type="button"
                data-testid="plan-delete-btn"
                onClick={() => setConfirming('delete')}
                className={`${railBtn} border-danger text-danger hover:text-danger`}
              >
                Delete
              </button>
            </>
          )}
        </div>
        {(start.isError || stop.isError) && (
          <p className="text-xs text-danger" data-testid="plan-lifecycle-error">
            {((start.error ?? stop.error) as Error).message}
          </p>
        )}
      </div>

      {/* Goal + creator tag. The section ALWAYS renders (the @creator tag — which
          opens the agent-activity sidebar — must show even when there's no goal). */}
      <div className="border-b border-border-base p-5" data-testid="plan-goal-wrap">
        <div className="mb-2 flex items-center gap-2">
          <h3 className="text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">Goal</h3>
          <button
            type="button"
            data-testid="plan-creator-tag"
            onClick={() => setAgentRef(plan.creator_ref)}
            title={`Open ${creatorLabel} activity`}
            className="ml-auto inline-flex items-center gap-1.5 rounded-full border border-border-base bg-bg-subtle py-0.5 pl-1 pr-2 text-xs font-medium text-text-secondary hover:border-accent hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            <Avatar name={creatorLabel} kind={refKind(plan.creator_ref) === 'agent' ? 'agent' : 'human'} size="sm" />
            <span data-testid="plan-creator">@{creatorLabel}</span>
            <svg viewBox="0 0 12 12" className="h-3 w-3 opacity-60" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
              <path d="M4.5 2.5 8 6l-3.5 3.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
          </button>
        </div>
        {plan.description.trim() !== '' ? (
          <>
            <p
              className={`whitespace-pre-wrap text-sm text-text-secondary ${goalLong && !goalOpen ? 'line-clamp-4' : ''}`}
              data-testid="plan-goal"
              title="Plan goal"
            >
              {plan.description}
            </p>
            {goalLong && (
              <button
                type="button"
                onClick={() => setGoalOpen((v) => !v)}
                data-testid="plan-goal-toggle"
                aria-expanded={goalOpen}
                className="mt-1.5 text-xs font-medium text-accent hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                {goalOpen ? 'Show less' : 'Show more'}
              </button>
            )}
          </>
        ) : (
          <p className="text-sm italic text-text-muted">No goal set.</p>
        )}
      </div>

      {/* Participants — avatars open the agent-activity sidebar. Sits below the
          merged Status+Progress block and Goal (Progress now lives at the top). */}
      {participants.length > 0 && (
        <div className="border-b border-border-base p-5">
          <h3 className="mb-3 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">Participants</h3>
          <div className="flex flex-wrap gap-2" data-testid="plan-rail-participants">
            {participants.map((pt) => {
              const nm = resolveName(pt.identity_id);
              const label = nm === pt.identity_id ? normalizeIdentityRef(pt.identity_id) : nm;
              return (
                <button
                  key={pt.identity_id}
                  type="button"
                  onClick={() => setAgentRef(pt.identity_id)}
                  title={label}
                  aria-label={`Open ${label} activity`}
                  className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  <Avatar name={label} kind={(pt.kind as 'agent' | 'human') ?? (refKind(pt.identity_id) === 'agent' ? 'agent' : 'human')} />
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Up next */}
      <div className="border-b border-border-base p-5">
        <h3 className="mb-3 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">Up next</h3>
        {upNext.length === 0 ? (
          <p className="text-xs text-text-muted">No nodes queued.</p>
        ) : (
          <ul className="space-y-2" data-testid="plan-upnext">
            {upNext.map((n) => (
              <li
                key={n.task_id}
                className="flex items-center gap-2.5 rounded-lg border border-border-base bg-bg-subtle px-3 py-2 text-sm text-text-secondary"
              >
                <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: nodeDotColor(n.node_status) }} />
                <span className="min-w-0 truncate" title={n.title}>{n.title}</span>
                {n.org_ref && <span className="ml-auto shrink-0 font-mono text-[0.625rem] text-text-muted">{n.org_ref}</span>}
              </li>
            ))}
            {upNextHidden > 0 && (
              <li>
                <button type="button" onClick={onOpenDag} className="text-xs font-medium text-accent hover:underline">
                  +{upNextHidden} more — view DAG
                </button>
              </li>
            )}
          </ul>
        )}
      </div>

      {/* Related Issues — the source issue(s) this plan's tasks derive from, so you
          can hop from a plan back to the issue that spawned it. Issue-side mirror of the
          issue sidebar's Derived Tasks list. Self-fetching (useRelatedIssues). */}
      <RelatedIssuesBlock projectId={projectId} currentPlanId={plan.id} />

      {/* T570: Threads / Files panel removed from the rail per @oopslink — the rail
          is now status/goal/participants/progress/up-next only. */}

      {/* The (unchanged) agent-activity sidebar, opened from the @creator tag / avatars. */}
      <SenderDetailSidebar open={agentRef !== null} senderRef={agentRef} onClose={() => setAgentRef(null)} />

      {editing && <PlanEditModal projectId={projectId} plan={plan} onClose={() => setEditing(false)} />}
      {confirming === 'delete' && <PlanDeleteModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />}
      {confirming === 'archive' && <PlanArchiveModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />}
    </aside>
  );
}

// ── Destructive confirm modals (v2.9 Stage B) ────────────────────────────────
// Both mirror the PlanEditModal/PlanCreateModal pattern (bg-black/50 scrim +
// solid bg-bg-elevated surface = the sanctioned both-mode-AA modal). They are
// CONSEQUENCE-EXPLAINING (PD bar 2): the body spells out exactly what the action
// does (not just "are you sure?"). Cancel closes WITHOUT acting. On error the
// modal STAYS OPEN and shows a FRIENDLY inline message (#218,
// friendlyDestructivePlanError — status-agnostic message-substring match).

// PlanDeleteModal — DELETE /{id}. On success the plan no longer exists, so we
// navigate AWAY to the project's Plans board (the detail route would 404).
function PlanDeleteModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const del = useDeletePlan(projectId, plan.id);

  const onConfirm = async () => {
    try {
      await del.mutateAsync();
      // The plan is GONE — leave the (now-404) detail route for the board.
      onClose();
      navigate(orgPath(`/projects/${encodeURIComponent(projectId)}/plans`, org?.slug));
    } catch {
      // surfaced inline below (#218); modal stays open.
    }
  };

  return (
    <DestructiveConfirmModal
      testId="plan-delete-modal"
      title="Delete this plan?"
      planName={plan.name}
      body="This unloads all this plan's tasks back to the Backlog, permanently deletes the plan's conversation, and deletes the plan. This cannot be undone."
      confirmLabel="Delete plan"
      pendingLabel="Deleting…"
      pending={del.isPending}
      error={del.isError ? friendlyDestructivePlanError(del.error) : null}
      errorTestId="plan-delete-error"
      cancelTestId="plan-delete-cancel"
      confirmTestId="plan-delete-confirm"
      onCancel={onClose}
      onConfirm={onConfirm}
    />
  );
}

// PlanArchiveModal — POST /{id}/archive. On success the plan (+ all its tasks)
// flip to the terminal archived state; the plan stays readable, so we just close
// and let the invalidation refresh the now-archived view.
function PlanArchiveModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const archive = useArchivePlan(projectId, plan.id);

  const onConfirm = async () => {
    try {
      await archive.mutateAsync();
      onClose();
    } catch {
      // surfaced inline below (#218); modal stays open.
    }
  };

  return (
    <DestructiveConfirmModal
      testId="plan-archive-modal"
      title="Archive this plan?"
      planName={plan.name}
      body="This archives the plan and all its tasks (a terminal state). This cannot be undone."
      confirmLabel="Archive plan"
      pendingLabel="Archiving…"
      pending={archive.isPending}
      error={archive.isError ? friendlyDestructivePlanError(archive.error) : null}
      errorTestId="plan-archive-error"
      cancelTestId="plan-archive-cancel"
      confirmTestId="plan-archive-confirm"
      onCancel={onClose}
      onConfirm={onConfirm}
    />
  );
}

// Shared scrim+surface confirm dialog for the two destructive actions. Same
// modal idiom as PlanEditModal (bg-black/50 scrim + solid bg-bg-elevated). The
// confirm button is danger-toned (border + danger text, no white-on-light-red
// flip — that fails dark-mode AA).
function DestructiveConfirmModal({
  testId,
  title,
  planName,
  body,
  confirmLabel,
  pendingLabel,
  pending,
  error,
  errorTestId,
  cancelTestId,
  confirmTestId,
  onCancel,
  onConfirm,
}: {
  testId: string;
  title: string;
  planName: string;
  body: string;
  confirmLabel: string;
  pendingLabel: string;
  pending: boolean;
  error: string | null;
  errorTestId: string;
  cancelTestId: string;
  confirmTestId: string;
  onCancel: () => void;
  onConfirm: () => void;
}): React.ReactElement {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid={testId}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-2 text-lg font-semibold">{title}</h2>
        <p className="mb-3 text-sm text-text-secondary">
          <span className="font-medium text-text-primary">{planName}</span>
        </p>
        <p className="text-sm text-text-secondary">{body}</p>
        {error && (
          <p className="mt-3 text-xs font-medium text-danger" role="alert" data-testid={errorTestId}>
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onCancel}
            data-testid={cancelTestId}
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={pending}
            className="rounded border border-danger bg-bg-subtle px-3 py-1.5 text-sm font-semibold text-danger hover:bg-bg-base disabled:opacity-50"
            onClick={onConfirm}
            data-testid={confirmTestId}
          >
            {pending ? pendingLabel : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Plan-edit modal (v2.9 Stage A3) ──────────────────────────────────────────
// Edits name / goal (= the `description` DTO field — the contract names it
// `description`, NOT `goal`; verified vs PatchPlanInput in api/plans.ts) /
// target_date via usePatchPlan (PATCH /{id}). draft-only — opened only from the
// header's draft-gated Edit button (§9.4: running/done is immutable, the backend
// rejects with ErrPlanNotDraft). Mirrors PlanCreateModal's structure + styling
// (bg-black/50 scrim + bg-bg-elevated surface = the sanctioned both-mode-AA modal
// pattern). #218: a patch failure surfaces a FRIENDLY inline message, never the
// raw API error.
//
// Partial-update / TargetDateSet semantics (verified vs the backend contract):
//   • Only CHANGED fields are sent (a no-op submit sends {}), so unchanged fields
//     stay untouched server-side.
//   • target_date: the create flow stores an absolute RFC3339 instant from a
//     YYYY-MM-DD picker. We pre-fill the picker from the stored instant (local
//     date). On submit, if the user CLEARED it → send target_date: '' (the
//     backend's TargetDateSet="" path CLEARS it; absent = unchanged). If set to a
//     new date → send the RFC3339 instant. If unchanged → omit it entirely.
const PLAN_EDIT_MODAL_INPUT =
  'mt-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

// A stored RFC3339 instant → the YYYY-MM-DD the date picker expects (local date).
function instantToDateInput(instant: string | null | undefined): string {
  if (!instant) return '';
  const d = new Date(instant);
  if (Number.isNaN(d.getTime())) return '';
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function friendlyPatchError(error: unknown): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('archived')) {
    return 'This plan is archived and can no longer be edited.';
  }
  if (lower.includes('draft')) {
    // T238: name/goal edit any time; only the target date is draft-only.
    return 'The target date can only be changed while the plan is a draft.';
  }
  return "Couldn't save your changes. Please try again.";
}

function PlanEditModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const [name, setName] = useState(plan.name);
  const [description, setDescription] = useState(plan.description ?? '');
  const [targetDate, setTargetDate] = useState(() => instantToDateInput(plan.target_date));
  const patch = usePatchPlan(projectId, plan.id);
  // T238: name + goal are editable in any non-archived status; target_date
  // (scheduling) stays draft-only, so the field only shows for a draft plan.
  const isDraft = plan.status === 'draft';

  // The picker value the field STARTED at — used to detect "cleared" vs "changed"
  // vs "unchanged" without re-parsing the stored instant on every render.
  const originalTargetDate = instantToDateInput(plan.target_date);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    const input: PatchPlanInput = {};
    // name — required; send only when changed.
    if (name.trim() !== plan.name) input.name = name.trim();
    // goal (= description) — send only when changed (cleared → '').
    if (description.trim() !== (plan.description ?? '')) input.description = description.trim();
    // target_date — draft-only (§9.4); distinguish cleared / changed / unchanged.
    if (isDraft && targetDate !== originalTargetDate) {
      if (targetDate === '') {
        // cleared → '' (the backend's TargetDateSet="" path CLEARS it).
        input.target_date = '';
      } else {
        // YYYY-MM-DD → RFC3339 with local offset (absolute instant), matching
        // the create flow so the backend stores an absolute time, not naive UTC.
        const d = new Date(`${targetDate}T00:00:00`);
        if (!Number.isNaN(d.getTime())) input.target_date = d.toISOString();
      }
    }

    try {
      await patch.mutateAsync(input);
      onClose();
    } catch {
      // surfaced inline below (#218)
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="plan-edit-modal"
      role="dialog"
      aria-modal="true"
      aria-label="Edit plan"
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold">Edit Plan</h2>
        <label className="block text-xs font-medium" htmlFor="plan-edit-name">
          Name
        </label>
        <input
          id="plan-edit-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-edit-name"
          autoFocus
        />
        <label className="mt-3 block text-xs font-medium" htmlFor="plan-edit-description">
          Goal
        </label>
        <textarea
          id="plan-edit-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-edit-description"
        />
        {isDraft && (
          <>
            <label className="mt-3 block text-xs font-medium" htmlFor="plan-edit-target-date">
              Target date
            </label>
            <input
              id="plan-edit-target-date"
              type="date"
              lang="en"
              value={targetDate}
              onChange={(e) => setTargetDate(e.target.value)}
              className={PLAN_EDIT_MODAL_INPUT}
              data-testid="plan-edit-target-date"
            />
          </>
        )}
        {patch.isError && (
          <p className="mt-3 text-xs font-medium text-danger" role="alert" data-testid="plan-edit-error">
            {friendlyPatchError(patch.error)}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="plan-edit-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={patch.isPending || !name.trim()}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-edit-submit"
          >
            {patch.isPending ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </form>
    </div>
  );
}

function TabButton({
  id,
  active,
  onSelect,
  children,
}: {
  id: Tab;
  active: boolean;
  onSelect: (t: Tab) => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      data-testid={`plan-tab-${id}`}
      onClick={() => onSelect(id)}
      className={`-mb-px min-h-[44px] rounded-t-lg border border-b-0 px-3.5 py-1.5 text-xs font-semibold md:min-h-0 ${
        active
          ? 'border-border-base bg-bg-elevated text-text-primary shadow-[inset_0_2px_0_var(--color-accent,#3b82f6)]'
          : 'border-transparent bg-bg-subtle text-text-secondary hover:text-text-primary'
      }`}
    >
      {children}
    </button>
  );
}

// ── 6-state node palette (LOCKED, Tester2 computed-truth) ────────────────────
// SOLID X-100/X-800 literal pairs (theme-independent, both-mode AA ~6-9 contrast)
// + matching X-300 border + an inline SVG icon (NOT emoji). label+SVG+color is
// the triple-distinguisher (the 6 states are color-close, so never color alone).
interface NodeStateStyle {
  label: string;
  cls: string; // bg + text (the chip)
  border: string; // node box border (X-300)
  icon: React.ReactElement;
}

const ICON_CLS = 'h-2.5 w-2.5';

const NODE_STATE: Record<PlanNodeStatus, NodeStateStyle> = {
  blocked: {
    label: 'blocked',
    cls: 'bg-status-slate-bg text-status-slate-fg',
    border: 'border-status-slate-border',
    // lock
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <rect x="5" y="11" width="14" height="9" rx="2" />
        <path d="M8 11V7a4 4 0 0 1 8 0v4" />
      </svg>
    ),
  },
  ready: {
    label: 'ready',
    cls: 'bg-status-blue-bg text-status-blue-fg',
    border: 'border-status-blue-border',
    // circle (○)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <circle cx="12" cy="12" r="8" />
      </svg>
    ),
  },
  dispatched: {
    label: 'dispatched',
    cls: 'bg-status-violet-bg text-status-violet-fg',
    border: 'border-status-violet-border',
    // clock / hourglass
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7v5l3 2" />
      </svg>
    ),
  },
  running: {
    label: 'running',
    cls: 'bg-status-amber-bg text-status-amber-fg',
    border: 'border-status-amber-border',
    // play (▶)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="currentColor" aria-hidden="true">
        <path d="M7 5v14l12-7z" />
      </svg>
    ),
  },
  // T53: the agent paused its work item — the node is set aside, not actively
  // running. Stone (muted warm-gray) reads as "halted/waiting", distinct from the
  // amber `running`, so the DAG tells the truth instead of a phantom-running node.
  paused: {
    label: 'paused',
    cls: 'bg-status-stone-bg text-status-stone-fg',
    border: 'border-status-stone-border',
    // pause (⏸)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="currentColor" aria-hidden="true">
        <rect x="6" y="5" width="4" height="14" rx="1" />
        <rect x="14" y="5" width="4" height="14" rx="1" />
      </svg>
    ),
  },
  done: {
    label: 'done',
    cls: 'bg-status-emerald-bg text-status-emerald-fg',
    border: 'border-status-emerald-border',
    // check mark
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true">
        <path d="M5 13l4 4L19 7" />
      </svg>
    ),
  },
  failed: {
    label: 'failed',
    cls: 'bg-status-rose-bg text-status-rose-fg',
    border: 'border-status-rose-border',
    // cross / x
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    ),
  },
};

const NODE_STATE_ORDER: PlanNodeStatus[] = ['blocked', 'ready', 'dispatched', 'running', 'paused', 'done', 'failed'];

function NodeStateChip({ status }: { status: PlanNodeStatus }): React.ReactElement {
  const s = NODE_STATE[status] ?? NODE_STATE.blocked;
  return (
    <span
      className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide ${s.cls}`}
      data-testid="node-state-chip"
      data-node-status={status}
    >
      {s.icon}
      {s.label}
    </span>
  );
}

// UnmergedBranchesPanel (v2.13.0 / I18 F4) — the PD's ship-gate board: the cycle
// plan's `Integrate(T)` nodes that have NOT yet merged back into the integration
// trunk (= unmerged feature branches), GET …/plans/{id}/unmerged-branches. It is
// the structural counterpart of the §2.5 集成完成 Gate: while any row remains, the
// gate is not clear and the plan must not ship.
//
// It renders NOTHING when there is nothing to reconcile — a non-cycle plan (no
// node metadata) or a fully-merged cycle plan both return an empty board, and an
// empty board has no actionable content, so the panel stays out of the way rather
// than printing a misleading "all merged" on every plan. When rows exist it shows
// a warning-styled checklist (branch → base, derived node_status, owner) so the PD
// sees exactly which branches still owe a merge and why each is still open.
function UnmergedBranchesPanel({
  projectId,
  planId,
  className = 'mx-4 mt-2',
}: {
  projectId: string;
  planId: string;
  // Outer spacing — defaults to the in-flow placement; the right-hand rail passes
  // its own (@oopslink: unmerged branches moved into the PlanInfoRail sidebar).
  className?: string;
}): React.ReactElement | null {
  const board = useUnmergedBranches(projectId, planId);
  const rows = board.data?.unmerged ?? [];
  // T315: the panel is collapsible (the PD asked) — a long unmerged list was a
  // cramped wall on the plan view. Default OPEN so the ship-gate detail stays
  // visible; the header toggles it.
  const [open, setOpen] = useState(true);
  // Stay silent while loading, on error, or when there is nothing unmerged — this
  // is an alert surface, not a permanent fixture (a failed fetch must not block
  // the rest of the plan view).
  if (rows.length === 0) {
    return null;
  }
  return (
    <div
      className={`overflow-hidden rounded-md border border-warning/40 bg-warning/5 ${className}`}
      data-testid="plan-unmerged-board"
      data-unmerged-count={rows.length}
    >
      {/* T315: clickable header = collapse toggle (chevron rotates when open). */}
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs font-semibold text-warning hover:bg-warning/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-warning/40"
        data-testid="plan-unmerged-toggle"
      >
        <svg
          viewBox="0 0 12 12"
          aria-hidden="true"
          className={`h-3 w-3 shrink-0 transition-transform ${open ? 'rotate-90' : ''}`}
        >
          <path d="M4 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        <span>Unmerged branches</span>
        <span
          className="inline-flex items-center rounded-full bg-warning/15 px-1.5 py-0.5 text-[0.625rem] font-bold text-warning"
          data-testid="plan-unmerged-count"
        >
          {rows.length}
        </span>
        <span className="min-w-0 flex-1 truncate font-normal text-text-muted">
          — not yet merged back; clear before Ship
        </span>
      </button>
      {open && (
        <ul
          className="space-y-1 border-t border-warning/20 px-2 py-2"
          data-testid="plan-unmerged-list"
        >
          {rows.map((u) => (
            <li
              key={u.task_id}
              className="rounded border border-border-base/60 bg-bg-base/40 px-2 py-1.5"
              data-testid="plan-unmerged-row"
              data-task-id={u.task_id}
            >
              {/* line 1: id · title · status — the at-a-glance row. */}
              <div className="flex items-center gap-2 text-xs">
                <TaskIdTag taskId={u.task_id} orgRef={u.org_ref} testId="plan-unmerged-ref" />
                <span className="min-w-0 flex-1 truncate font-medium text-text-primary" title={u.title}>
                  {u.title}
                </span>
                <NodeStateChip status={u.node_status} />
                {u.skip_merge_check && (
                  <span
                    className="inline-flex shrink-0 items-center rounded bg-bg-subtle px-1 py-0.5 text-[0.625rem] font-medium text-text-muted"
                    data-testid="plan-unmerged-skipcheck"
                    title="merge check structurally skipped (no-code feature); still counts until done"
                  >
                    skip-check
                  </span>
                )}
              </div>
              {/* line 2: branch → base, secondary (truncates instead of wrapping). */}
              <div className="mt-1 flex items-center gap-1 font-mono text-[0.625rem] text-text-secondary">
                <span className="min-w-0 truncate" title={u.branch || u.task_id}>
                  {u.branch || u.task_id}
                </span>
                <span className="shrink-0 text-text-muted">→</span>
                <span className="min-w-0 shrink-0 truncate text-text-muted" title={u.base || 'trunk'}>
                  {u.base || 'trunk'}
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

// TaskIdTag — a small monospace pill showing the human Task id (org_ref "T123"),
// taken DIRECTLY from the PlanNode's own org_ref (api/plans PlanNode.org_ref —
// list_plans/detail already return it; T126 removed the old FE task-list re-
// resolver that missed completed tasks → #id-tail). Falls back to the FULL
// task_id (never a #id-tail hash) when org_ref is absent. Full task_id on hover.
function TaskIdTag({
  taskId,
  orgRef,
  testId,
}: {
  taskId: string;
  orgRef?: string;
  testId: string;
}): React.ReactElement {
  const label = refLabel(orgRef, taskId);
  return (
    <span
      className="inline-flex shrink-0 items-center rounded bg-bg-subtle px-1 py-0.5 font-mono text-[0.625rem] font-semibold text-text-secondary"
      data-testid={testId}
      title={taskId}
    >
      {label}
    </span>
  );
}

// assignee_ref → avatar (agent/human) + clean handle. The name/avatar is a click
// entry that opens the EXISTING agent-activity sidebar (the same single
// SenderDetailSidebar the participant rail / @mentions drive), routed through the
// DAG's SenderSidebarProvider — so clicking the assignee on a DAG node (both the
// mobile stepper and the desktop graph reuse this tag) pops the activity panel.
// When rendered with NO provider (e.g. an isolated unit test) `useSenderSidebar`
// returns null and the tag degrades to a static, non-clickable span — unchanged.
function AssigneeTag({ assigneeRef }: { assigneeRef: string }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  const openSender = useSenderSidebar();
  if (!assigneeRef) {
    return <span className="text-text-muted">—</span>;
  }
  const kind = refKind(assigneeRef) === 'agent' ? 'agent' : 'human';
  const resolved = resolveName(assigneeRef);
  const label = resolved === assigneeRef ? normalizeIdentityRef(assigneeRef) : resolved;
  const inner = (
    <>
      <span className="shrink-0">
        <Avatar name={label} kind={kind} size="sm" />
      </span>
      <span className="min-w-0 truncate">{label}</span>
    </>
  );
  if (openSender) {
    return (
      <button
        type="button"
        data-testid="plan-node-assignee"
        onClick={() => openSender(assigneeRef)}
        title={`Open ${label} activity`}
        aria-label={`Open ${label} activity`}
        className="flex min-w-0 items-center gap-1.5 rounded text-text-secondary hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
      >
        {inner}
      </button>
    );
  }
  return (
    <span className="flex min-w-0 items-center gap-1.5 text-text-secondary" title={assigneeRef}>
      {inner}
    </span>
  );
}

// ── DAG (the core) ──────────────────────────────────────────────────────────
// Layered left→right layout from node.depends_on:
//   level(n) = 0 if no (in-plan) deps else max(level(dep))+1   (longest-path)
//   x = level * COL_W;  y = even vertical spread within the level.
// Edges: SVG path from each dep node's RIGHT-mid → this node's LEFT-mid, with an
// arrow marker (upstream → downstream). node_status is DERIVED → display only.
const COL_W = 200;
const NODE_W = 168;
const NODE_H = 84;
const ROW_GAP = 28;
const PAD_X = 14;
const PAD_Y = 16;

interface Positioned {
  node: PlanNode;
  level: number;
  x: number;
  y: number;
}

// v2.9 Stage A5 — synthetic Start/End flow anchors (NOT real tasks): a Start
// node a column LEFT of all roots (edges → every level-0 root) and an End node
// a column RIGHT of the deepest level (edges ← every leaf, i.e. a node nothing
// else depends on). They give parallel/independent chains a clear left→right
// progression. They are layout/flow markers only: no node_status / 6-state
// chip, not dispatchable, not counted, not in the task list.
interface SyntheticAnchor {
  // center point of the anchor marker (used for both placement + edge endpoint)
  cx: number;
  cy: number;
  // the real-node anchor points this connects to (root left-mids for Start,
  // leaf right-mids for End)
  links: { taskId: string; x: number; y: number }[];
}

const SYNTH_R = 26; // synthetic marker radius (circle terminal)

function layoutDag(nodes: PlanNode[]): {
  positioned: Positioned[];
  width: number;
  height: number;
  start: SyntheticAnchor | null;
  end: SyntheticAnchor | null;
} {
  const byId = new Map(nodes.map((n) => [n.task_id, n]));
  // Only consider deps that are actually in this plan (defensive — a dangling
  // dep ref must not break level computation).
  const depsOf = (n: PlanNode) => n.depends_on.filter((d) => byId.has(d));

  // Longest-path level via memoized DFS (cycle-guarded; the DAG is acyclic by
  // contract, the guard just prevents a hang on bad data).
  const levelCache = new Map<string, number>();
  const inStack = new Set<string>();
  function level(id: string): number {
    if (levelCache.has(id)) return levelCache.get(id)!;
    if (inStack.has(id)) return 0; // cycle guard
    const n = byId.get(id);
    if (!n) return 0;
    inStack.add(id);
    const deps = depsOf(n);
    const lvl = deps.length === 0 ? 0 : Math.max(...deps.map((d) => level(d) + 1));
    inStack.delete(id);
    levelCache.set(id, lvl);
    return lvl;
  }

  // Group by level, preserving input order within a level.
  const byLevel = new Map<number, PlanNode[]>();
  let maxLevel = 0;
  for (const n of nodes) {
    const lvl = level(n.task_id);
    maxLevel = Math.max(maxLevel, lvl);
    const arr = byLevel.get(lvl) ?? [];
    arr.push(n);
    byLevel.set(lvl, arr);
  }

  // Reserve a left gutter column for the Start anchor when there are any nodes,
  // so real nodes are shifted right by one column (Start sits at x≈PAD_X, real
  // level-0 nodes at PAD_X + COL_W). Empty plan ⇒ no gutter, no anchors.
  const hasNodes = nodes.length > 0;
  const SYNTH_COL = COL_W; // width of each synthetic gutter column
  const baseX = PAD_X + (hasNodes ? SYNTH_COL : 0);

  // Even vertical spread within each level.
  const positioned: Positioned[] = [];
  let maxRows = 0;
  for (const [lvl, group] of byLevel) {
    maxRows = Math.max(maxRows, group.length);
    group.forEach((node, row) => {
      positioned.push({
        node,
        level: lvl,
        x: baseX + lvl * COL_W,
        y: PAD_Y + row * (NODE_H + ROW_GAP),
      });
    });
  }

  // Roots = real level-0 nodes (no in-plan deps). Leaves = nodes that no other
  // in-plan node depends on. Start → every root; every leaf → End.
  const dependedOn = new Set<string>();
  for (const n of nodes) for (const d of depsOf(n)) dependedOn.add(d);

  const contentHeight = Math.max(PAD_Y * 2 + maxRows * (NODE_H + ROW_GAP) - ROW_GAP, 200);
  const midY = contentHeight / 2;

  let start: SyntheticAnchor | null = null;
  let end: SyntheticAnchor | null = null;
  if (hasNodes) {
    const roots = positioned.filter((p) => p.level === 0);
    const leaves = positioned.filter((p) => !dependedOn.has(p.node.task_id));
    start = {
      cx: PAD_X + SYNTH_R,
      cy: midY,
      // edge endpoint = root node's LEFT-mid
      links: roots.map((p) => ({ taskId: p.node.task_id, x: p.x, y: p.y + NODE_H / 2 })),
    };
    const endCx = baseX + (maxLevel + 1) * COL_W + SYNTH_R;
    end = {
      cx: endCx,
      cy: midY,
      // edge endpoint = leaf node's RIGHT-mid
      links: leaves.map((p) => ({ taskId: p.node.task_id, x: p.x + NODE_W, y: p.y + NODE_H / 2 })),
    };
  }

  // Width spans from PAD_X (Start) to End marker (when present), else the real
  // layout extent.
  const realRight = baseX + maxLevel * COL_W + NODE_W;
  const width = hasNodes
    ? (end ? end.cx + SYNTH_R + PAD_X : realRight + PAD_X)
    : PAD_X * 2 + (maxLevel + 1) * COL_W - (COL_W - NODE_W);
  const height = contentHeight;
  return { positioned, width, height, start, end };
}

// Synthetic Start/End flow anchor — a distinct, non-task marker. No node_status
// / 6-state chip, no assignee, not clickable, not counted. Solid theme tokens
// (both-mode AA), plain text "Start"/"End" (no emoji). Positioned by its center
// (cx,cy) so it lines up with its flow edges.
function SyntheticAnchorMarker({
  kind,
  anchor,
}: {
  kind: 'start' | 'end';
  anchor: SyntheticAnchor;
}): React.ReactElement {
  const label = kind === 'start' ? 'Start' : 'End';
  return (
    <div
      // T347: Start gets a solid accent BORDER (the entry point); End keeps the
      // neutral strong border. Both keep readable text-text-secondary; no
      // alpha-tint-on-token (renders transparent — guardrail) and no 6-state border.
      className={`absolute flex items-center justify-center rounded-full border-[1.5px] bg-bg-elevated text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary shadow-2 ${
        kind === 'start' ? 'border-accent' : 'border-border-strong'
      }`}
      style={{
        left: anchor.cx - SYNTH_R,
        top: anchor.cy - SYNTH_R,
        width: SYNTH_R * 2,
        height: SYNTH_R * 2,
      }}
      data-testid={`plan-dag-synthetic-${kind}`}
      aria-hidden="true"
    >
      {label}
    </div>
  );
}

// ── PlanStepper (mobile <md) ─────────────────────────────────────────────────
// v2.10.1 [M4] On mobile the left→right SVG DAG (PlanDag) becomes a VERTICAL
// stepper / timeline (mockup `docs/design/v2.10.1/v2.10.1-mobile` — Plan frame).
// It fixes the 375px-critical③: the wide absolute-positioned graph had touch
// targets too small + dead vertical height. The stepper renders the same nodes
// in topological order (by DAG level) as big, tappable cards with a
// status-colored timeline dot — display-only (in-graph dependency EDITING stays
// desktop-only). The same node tokens/components (NODE_STATE / NodeStateChip /
// TaskIdTag / TaskTitleLink / AssigneeTag) are reused so the two views stay in
// sync.
function PlanStepper({
  positioned,
  projectId,
}: {
  positioned: Positioned[];
  projectId: string;
}): React.ReactElement {
  // Topological-ish order: DAG level, then stable vertical position within it.
  const ordered = useMemo(
    () => [...positioned].sort((a, b) => a.level - b.level || a.y - b.y || a.x - b.x),
    [positioned],
  );
  return (
    <ol className="relative mt-1 md:hidden" data-testid="plan-stepper">
      {ordered.map((p, i) => {
        const s = NODE_STATE[p.node.node_status] ?? NODE_STATE.blocked;
        const taskId = p.node.task_id;
        const last = i === ordered.length - 1;
        return (
          <li
            key={taskId}
            className="relative pb-3 pl-6"
            data-testid="plan-stepper-node"
            data-task-id={taskId}
            data-node-status={p.node.node_status}
            data-level={p.level}
          >
            {/* Timeline rail (omit after the last node). */}
            {!last && (
              <span
                aria-hidden="true"
                className="absolute bottom-0 left-[5px] top-5 w-px bg-border-base"
              />
            )}
            {/* Status-colored timeline dot (reuses the node state tokens). */}
            <span
              aria-hidden="true"
              data-testid="plan-stepper-dot"
              className={`absolute left-0 top-3.5 h-3 w-3 rounded-full border ${s.border} ${s.cls}`}
            />
            <div className={`rounded-lg border bg-bg-elevated p-2.5 shadow-1 ${s.border}`}>
              <div className="mb-1 flex items-center justify-between gap-2">
                <TaskIdTag taskId={taskId} orgRef={p.node.org_ref} testId="plan-stepper-taskid" />
                <span className="inline-flex items-center gap-1">
                  <TaskArchivedBadge archived={p.node.archived} taskId={taskId} />
                  <NodeStateChip status={p.node.node_status} />
                </span>
              </div>
              {/* Big tappable title (≥44px touch target), opens the task. */}
              <TaskTitleLink
                projectId={projectId}
                taskId={taskId}
                title={p.node.title || refLabel(p.node.org_ref, taskId)}
                className="min-h-[44px] py-1 text-sm font-semibold"
              />
              <div className="mt-0.5 text-xs">
                <AssigneeTag assigneeRef={p.node.assignee_ref} />
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function PlanDag({
  projectId,
  plan,
  compact,
}: {
  projectId: string;
  plan: Plan;
  // T348: compact (zoom-to-fit) is controlled by the tab-row icon in PlanDetail.
  compact: boolean;
}): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const isDraft = plan.status === 'draft';
  // v2.9.1 UX point 2: "Compact" uniformly zooms the DAG down so a long (many-level)
  // / wide plan fits in view without endless horizontal scrolling. CSS transform
  // (content scales cleanly); the scroll area is sized to the scaled extent.
  const scale = compact ? 0.7 : 1;

  // v2.9.1 point 3: IN-GRAPH dependency editing (draft-only). The dependency
  // STRUCTURE is edited directly on the graph — no separate dropdown box (§21
  // single entry). Each draft node has a focusable "connect" control that enters
  // CONNECT MODE with that node as the source; valid targets (validDropTargets,
  // = excludes self/exists/cycle — cycle/self blocked at the UI layer) light up
  // as activatable targets. Each existing edge has a focusable delete control.
  // Add = AddPlanDependency(from=source, to=target) → "source depends on target".
  const addDep = useAddDependency(projectId, plan.id);
  const removeDep = useRemoveDependency(projectId, plan.id);
  // connectFrom = the SOURCE task_id of the in-progress connection (null = not in
  // connect mode). Only meaningful while draft.
  const [connectFrom, setConnectFrom] = useState<string | null>(null);
  const titleOf = useCallback(
    (taskId: string) => {
      const n = nodes.find((m) => m.task_id === taskId);
      return n?.title || refLabel(n?.org_ref, taskId);
    },
    [nodes],
  );

  const exitConnect = useCallback(() => setConnectFrom(null), []);

  // Escape exits connect mode without adding (a11y: a cancel affordance is also
  // rendered visibly below).
  useEffect(() => {
    if (connectFrom == null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') exitConnect();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [connectFrom, exitConnect]);

  // Leaving draft (e.g. plan starts) must drop any in-progress connect mode.
  useEffect(() => {
    if (!isDraft) setConnectFrom(null);
  }, [isDraft]);

  // The legal targets for the active source (self/exists/cycle excluded). Only
  // these become activatable target controls; everything else is inert.
  const dropTargets = useMemo(
    () => (connectFrom != null ? validDropTargets(nodes, connectFrom) : new Set<string>()),
    [nodes, connectFrom],
  );

  const onTargetActivate = useCallback(
    (target: string) => {
      if (connectFrom == null) return;
      // UI-layer guard (cycle/self never even offered, but double-check).
      if (dependencyEdgeError(nodes, connectFrom, target) !== null) return;
      addDep.mutate(
        { from_task_id: connectFrom, to_task_id: target },
        { onSuccess: () => setConnectFrom(null) },
      );
      // Exit connect mode immediately (optimistic UX); on error the friendly
      // message surfaces below and the user can retry.
      setConnectFrom(null);
    },
    [addDep, connectFrom, nodes],
  );

  const mutationError = addDep.isError ? addDep.error : removeDep.isError ? removeDep.error : null;

  const { positioned, width, height, start, end } = useMemo(() => layoutDag(nodes), [nodes]);
  const posById = useMemo(
    () => new Map(positioned.map((p) => [p.node.task_id, p])),
    [positioned],
  );

  // Edges: dep (upstream) → node (downstream). Path from dep right-mid to node
  // left-mid; a horizontal-ease cubic for a clean orthogonal-ish curve.
  const edges = useMemo(() => {
    // Each real edge: dep (upstream `to`) → node (downstream `from`). The plan
    // node `p` lists `depId` in depends_on, i.e. "p depends on depId" ⟺
    // AddPlanDependency(from=p, to=depId). `from`/`to` are kept on the edge so a
    // draft delete control can call RemoveDependency with the exact ids; `mx/my`
    // is the curve midpoint where the delete control is anchored.
    const out: { key: string; d: string; from: string; to: string; mx: number; my: number }[] = [];
    for (const p of positioned) {
      for (const depId of p.node.depends_on) {
        const dep = posById.get(depId);
        if (!dep) continue;
        const x1 = dep.x + NODE_W;
        const y1 = dep.y + NODE_H / 2;
        const x2 = p.x;
        const y2 = p.y + NODE_H / 2;
        const midX = (x1 + x2) / 2;
        out.push({
          key: `${depId}->${p.node.task_id}`,
          d: `M${x1},${y1} C${midX},${y1} ${midX},${y2} ${x2},${y2}`,
          from: p.node.task_id,
          to: depId,
          mx: midX,
          my: (y1 + y2) / 2,
        });
      }
    }
    return out;
  }, [positioned, posById]);

  // v2.9 A5: synthetic flow edges — Start anchor → each root's left-mid, and
  // each leaf's right-mid → End anchor. Same cubic shape as real edges; kept on
  // a SEPARATE testid (`plan-dag-synthetic-edge`) so real-edge assertions/counts
  // are unaffected. A dashed/lighter stroke reads as a flow anchor, not a dep.
  const synthEdges = useMemo(() => {
    const out: { key: string; d: string }[] = [];
    if (start) {
      for (const l of start.links) {
        const midX = (start.cx + l.x) / 2;
        out.push({
          key: `start->${l.taskId}`,
          d: `M${start.cx},${start.cy} C${midX},${start.cy} ${midX},${l.y} ${l.x},${l.y}`,
        });
      }
    }
    if (end) {
      for (const l of end.links) {
        const midX = (l.x + end.cx) / 2;
        out.push({
          key: `${l.taskId}->end`,
          d: `M${l.x},${l.y} C${midX},${l.y} ${midX},${end.cy} ${end.cx},${end.cy}`,
        });
      }
    }
    return out;
  }, [start, end]);

  return (
    // SenderSidebarProvider owns the ONE agent-activity sidebar for the whole DAG
    // surface — every node's AssigneeTag (mobile stepper + desktop graph) opens it
    // through useSenderSidebar(). Scoped to the DAG, decoupled from the rail's own
    // sidebar and the chat's provider.
    <SenderSidebarProvider>
    {/* T579: desktop = flex column filling the panel so the canvas (flex-1 below)
        grows to occupy the full pane height; the legend/note stay pinned beneath it. */}
    <div data-testid="plan-dag" className="md:flex md:min-h-0 md:flex-1 md:flex-col">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-dag-empty">
          No tasks in this plan yet. Add tasks from the Work Board.
        </p>
      ) : (
        <>
        {/* v2.10.1 [M4] Mobile (<md): the left→right SVG DAG becomes a vertical
            stepper. The desktop graph + its controls are md:-only. */}
        <PlanStepper positioned={positioned} projectId={projectId} />
        {/* T348: the Compact toggle moved to the tab row (icon). The connect-mode
            banner (point 3, draft-only) stays here, shown only while connecting. */}
        {isDraft && connectFrom != null && (
          <div className="mb-2 hidden items-center gap-2 md:flex">
            <div
              className="flex flex-1 items-center gap-2 rounded border border-accent bg-bg-elevated px-2 py-1 text-[0.6875rem] text-text-secondary"
              data-testid="plan-connect-banner"
              role="status"
            >
              <span className="min-w-0 truncate">
                Pick a highlighted task that{' '}
                <span className="font-semibold text-text-primary">{titleOf(connectFrom)}</span> depends on.
              </span>
              <button
                type="button"
                data-testid="plan-connect-cancel"
                onClick={exitConnect}
                aria-label="Cancel adding dependency"
                className="ml-auto shrink-0 rounded border border-border-strong bg-bg-subtle px-2 py-0.5 font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
        <div
          className="relative hidden overflow-auto rounded-lg border border-border-base bg-bg-subtle hex-dot-grid md:block md:min-h-0 md:flex-1"
          data-testid="plan-dag-canvas"
          data-compact={compact ? 'true' : 'false'}
          // T347: a subtle dot-grid gives the DAG a "canvas" feel instead of a flat
          // panel (@oopslink: 太素了).
          // T579: was a fixed maxHeight:480 box that left dead space below it; now
          // flex-1 + min-h-0 inside the flex-column plan-dag so the canvas FILLS the
          // pane height and scrolls internally when the graph overflows.
        >
          {/* Sizing wrapper reserves the SCALED extent so the scroll area is
              correct; the inner layer keeps its natural size and is zoomed via
              transform (transform doesn't affect layout box). */}
          <div style={{ width: width * scale, height: height * scale }}>
          <div
            className="relative"
            data-testid="plan-dag-scaler"
            style={{
              width,
              height,
              transform: scale === 1 ? undefined : `scale(${scale})`,
              transformOrigin: 'top left',
            }}
          >
            {/* Edges (z-0, behind nodes). */}
            <svg
              className="absolute left-0 top-0"
              width={width}
              height={height}
              data-testid="plan-dag-svg"
              aria-hidden="true"
            >
              <defs>
                <marker
                  id="plan-dag-arrow"
                  viewBox="0 0 10 10"
                  refX="8"
                  refY="5"
                  markerWidth="7"
                  markerHeight="7"
                  orient="auto-start-reverse"
                >
                  <path d="M0,0 L10,5 L0,10 z" className="fill-border-strong" />
                </marker>
              </defs>
              <g fill="none" className="stroke-border-strong" strokeWidth="1.6" markerEnd="url(#plan-dag-arrow)">
                {edges.map((e) => (
                  <path key={e.key} d={e.d} data-testid="plan-dag-edge" data-edge={e.key} />
                ))}
              </g>
              {/* Synthetic flow edges (Start→roots, leaves→End): lighter +
                  dashed so they read as flow anchors, not real dependencies. */}
              <g
                fill="none"
                className="stroke-border-base"
                strokeWidth="1.4"
                strokeDasharray="4 3"
                markerEnd="url(#plan-dag-arrow)"
              >
                {synthEdges.map((e) => (
                  <path
                    key={e.key}
                    d={e.d}
                    data-testid="plan-dag-synthetic-edge"
                    data-edge={e.key}
                  />
                ))}
              </g>
            </svg>
            {/* Draft-only IN-GRAPH edge delete controls (z-10, anchored at each
                edge's curve midpoint). A real <button> (keyboard-focusable) →
                useRemoveDependency.mutate({from, to}). Running/done = none. */}
            {isDraft &&
              edges.map((e) => (
                <button
                  key={`del-${e.from}->${e.to}`}
                  type="button"
                  data-testid="plan-edge-delete"
                  // from->to (dependent->dependency) matches the RemoveDependency
                  // body: "from depends on to".
                  data-edge={`${e.from}->${e.to}`}
                  disabled={removeDep.isPending}
                  onClick={() => removeDep.mutate({ from_task_id: e.from, to_task_id: e.to })}
                  aria-label={`Remove dependency: ${titleOf(e.from)} depends on ${titleOf(e.to)}`}
                  title={`Remove dependency: ${titleOf(e.from)} depends on ${titleOf(e.to)}`}
                  className="absolute z-10 flex h-5 w-5 items-center justify-center rounded-full border border-border-strong bg-bg-elevated text-xs font-bold leading-none text-text-secondary shadow-1 hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
                  // centered on the edge midpoint (button is 20px = w-5/h-5).
                  style={{ left: e.mx - 10, top: e.my - 10 }}
                >
                  {/* ASCII multiplication sign (not emoji). */}
                  <span aria-hidden="true">&times;</span>
                </button>
              ))}
            {/* Nodes (z-10). */}
            {positioned.map((p) => {
              const s = NODE_STATE[p.node.node_status] ?? NODE_STATE.blocked;
              const taskId = p.node.task_id;
              const inConnect = connectFrom != null;
              const isSource = connectFrom === taskId;
              const isTarget = inConnect && !isSource && dropTargets.has(taskId);
              // T347: a status-colored left accent bar (derived from the border
              // token) + hover lift make the nodes read as status cards, not plain
              // boxes. overflow-hidden clips the bar to the rounded corner.
              const accentCls = s.border.replace(/^border-/, 'bg-');
              return (
                <div
                  key={taskId}
                  className={`absolute overflow-hidden rounded-lg border-[1.5px] bg-bg-elevated p-2 pl-3 shadow-1 transition duration-150 motion-safe:hover:-translate-y-0.5 hover:shadow-2 ${
                    isTarget
                      ? 'border-accent ring-2 ring-accent'
                      : isSource
                        ? 'border-accent'
                        : s.border
                  }`}
                  style={{ left: p.x, top: p.y, width: NODE_W }}
                  data-testid="plan-dag-node"
                  data-task-id={taskId}
                  data-level={p.level}
                  data-connect-source={isSource ? 'true' : undefined}
                  data-connect-target={isTarget ? 'true' : undefined}
                >
                  <span
                    className={`absolute inset-y-0 left-0 w-1.5 ${accentCls}`}
                    aria-hidden="true"
                  />
                  {/* v2.9.1 UX point 1: human Task id (T-number) visible on the node.
                      T329b: the status badge (+ archived) moves UP here to the id row
                      (right-aligned) so the assignee row below gets the FULL node width
                      — a long agent name is no longer cut/blocked by the badge. Mirrors
                      the mobile stepper layout. */}
                  <div className="mb-1 flex items-center justify-between gap-1">
                    <TaskIdTag taskId={taskId} orgRef={p.node.org_ref} testId="plan-node-taskid" />
                    <span className="inline-flex shrink-0 items-center gap-1">
                      <TaskArchivedBadge archived={p.node.archived} taskId={taskId} />
                      <NodeStateChip status={p.node.node_status} />
                      {/* Draft connect control (point 3): a real keyboard-focusable
                          button. Activating enters connect mode with this node as
                          the source. Hidden once running/done (display-only). */}
                      {isDraft && !inConnect && (
                        <button
                          type="button"
                          data-testid="plan-node-connect"
                          data-task-id={taskId}
                          onClick={() => setConnectFrom(taskId)}
                          aria-label={`Add dependency from ${titleOf(taskId)}`}
                          title={`Add a dependency from ${titleOf(taskId)} (pick the task it depends on)`}
                          className="shrink-0 rounded border border-border-strong bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                        >
                          + Dep
                        </button>
                      )}
                    </span>
                  </div>
                  <div className="mb-1.5 text-xs font-semibold text-text-primary" title={p.node.title}>
                    <TaskTitleLink
                      projectId={projectId}
                      taskId={taskId}
                      title={p.node.title || refLabel(p.node.org_ref, taskId)}
                    />
                  </div>
                  {/* Assignee gets its OWN full-width row (truncates only if extreme). */}
                  <div className="flex min-w-0 text-[0.6875rem]">
                    <AssigneeTag assigneeRef={p.node.assignee_ref} />
                  </div>
                  {/* Connect-mode target affordance (point 3): ONLY valid targets
                      (self/exists/cycle excluded) become activatable. A real
                      keyboard-focusable button overlaying the node; activating it
                      adds "source depends on target". Invalid nodes get nothing. */}
                  {isTarget && (
                    <button
                      type="button"
                      data-testid="plan-connect-target"
                      data-task-id={taskId}
                      onClick={() => onTargetActivate(taskId)}
                      aria-label={`Make ${titleOf(connectFrom!)} depend on ${titleOf(taskId)}`}
                      title={`Make ${titleOf(connectFrom!)} depend on ${titleOf(taskId)}`}
                      className="absolute inset-0 rounded-lg border-2 border-accent bg-transparent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    />
                  )}
                </div>
              );
            })}
            {/* Synthetic Start/End anchors (z-10) — distinct flow markers, NOT
                tasks: no node_status / 6-state chip, no assignee, not
                clickable/dispatchable, not in any count. Rendered as a labeled
                circular terminal. */}
            {start && <SyntheticAnchorMarker kind="start" anchor={start} />}
            {end && <SyntheticAnchorMarker kind="end" anchor={end} />}
          </div>
          </div>
        </div>
        </>
      )}

      {/* Legend (all 6 states) — the lifecycle controls live in the header
          (Start / Stop / Advance), rendered exactly once each there. */}
      <div className="mt-3 flex flex-wrap items-center gap-1.5" data-testid="plan-dag-legend">
        {NODE_STATE_ORDER.map((st) => (
          <NodeStateChip key={st} status={st} />
        ))}
      </div>

      {/* node_status is DERIVED (§9.2) and shown, not edited. In DRAFT the
          dependency STRUCTURE is editable IN-GRAPH (point 3): each node has a
          "+ Dep" connect control, and each edge has an "×" delete control. Once
          running/done the graph is DISPLAY-ONLY (backend rejects with
          ErrPlanNotDraft). */}
      <p className="mt-2 text-[0.6875rem] text-text-muted" data-testid="plan-dag-note">
        Node status is derived (= f(task status, all upstream done, dispatch record)) and shown, not edited.{' '}
        {isDraft ? (
          <>
            This plan is a draft, so its dependencies are editable right on the graph — use a node's "+ Dep"
            button to add an edge, or an edge's "×" to remove one.
          </>
        ) : (
          <>
            Display-only graph: a running plan auto-advances (the system dispatches ready nodes as upstream
            tasks complete) and "Advance now" is a manual override. Dependencies can only be edited while the
            plan is a draft.
          </>
        )}
      </p>

      {/* #218 friendly add/remove error (never the raw API message). The
          single in-graph entry point (§21) — no separate editor box. */}
      {isDraft && mutationError && (
        <p className="mt-2 text-xs font-medium text-danger" role="alert" data-testid="plan-edge-error">
          {friendlyDependencyError(mutationError)}
        </p>
      )}
    </div>
    </SenderSidebarProvider>
  );
}

// ── Draft-only dependency-edge editor (v2.9 Stage A1) ────────────────────────
// from/to semantics (verified against the backend, plan_view.go + plan_flow.go):
// a node's `depends_on` lists `edge.ToTaskID` where `edge.FromTaskID == node`,
// i.e. AddPlanDependency(from, to) means **from depends_on to** (`to` is the
// upstream dependency that completes first; `from` is the downstream dependent).
// So an edge "B depends on A" → { from_task_id: B, to_task_id: A }.
//
// #218: add/remove failures map the backend Go error message (all surface as a
// 400 invalid_request, distinguished by the message text) to a FRIENDLY string;
// the raw error is never shown.
function friendlyDependencyError(error: unknown): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('itself') || lower.includes('self')) {
    return "A task can't depend on itself.";
  }
  if (lower.includes('cycle')) {
    return 'That would create a cycle in the plan.';
  }
  if (lower.includes('draft')) {
    return 'Dependencies can only be edited while the plan is a draft.';
  }
  return "Couldn't update the plan's dependencies. Please try again.";
}

// ── Task list tab ────────────────────────────────────────────────────────────
// §9.4: removing a task from a Plan is a PLANNING action — only a DRAFT plan
// exposes a per-row "Remove" control (consistent with add-to-plan / the A1 edge
// editor). A running/done plan renders the rows read-only (no Remove column).
// memberRef — build the prefixed identity ref ("agent:<id>"/"user:<id>") for an
// assignee <option>, mirroring TaskEditModal.memberRef (kind derived when absent).
// Reuses the shared identityRefOf when kind is present; falls back to deriving
// kind from the id for legacy rows with no explicit kind.
function memberRef(m: MemberResult): string {
  const kind = m.kind ?? (m.identity_id.startsWith('agent') ? 'agent' : 'user');
  return identityRefOf({ kind, identity_id: m.identity_id });
}

// T41 (v2.9.1 #291): the Task-list tab is the COMPREHENSIVE management surface
// for a big plan — every node is rendered (no cap, ever), a search box narrows
// the visible rows by title / Task-id / assignee, and the table scrolls
// vertically within the tab. Inline assignee reassignment lives per-row.
function PlanTaskList({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const canRemove = plan.status === 'draft';
  const members = useMembers();
  const [query, setQuery] = useState('');

  // Case-insensitive filter on title OR Task-id (org_ref) OR assignee handle.
  // Empty box ⇒ ALL nodes (never capped). Matching keeps input order. org_ref
  // comes straight off the node (T126).
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return nodes;
    return nodes.filter((n) => {
      const orgRef = n.org_ref ?? '';
      const assignee = n.assignee_ref ? normalizeIdentityRef(n.assignee_ref) : '';
      const haystack = `${n.title ?? ''} ${orgRef} ${assignee}`.toLowerCase();
      return haystack.includes(q);
    });
  }, [nodes, query]);

  return (
    <div data-testid="plan-task-list">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-task-list-empty">
          No tasks in this plan yet.
        </p>
      ) : (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              data-testid="plan-task-search"
              aria-label="Filter tasks"
              placeholder="Filter by title, Task id, or assignee…"
              className="min-w-[14rem] flex-1 rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
            <span className="text-[0.6875rem] text-text-muted" data-testid="plan-task-search-count">
              Showing {filtered.length} of {nodes.length}
            </span>
          </div>
          {filtered.length === 0 ? (
            <p className="py-8 text-center text-xs text-text-muted" data-testid="plan-task-search-empty">
              No tasks match your filter.
            </p>
          ) : (
            <div className="max-h-[28rem] overflow-x-auto overflow-y-auto">
              <table className="w-full text-left text-xs" data-testid="plan-task-list-table">
                <thead>
                  <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                    <th className="py-1.5 pr-3 font-medium">Task</th>
                    <th className="py-1.5 pr-3 font-medium">Title</th>
                    <th className="py-1.5 pr-3 font-medium">Assignee</th>
                    <th className="py-1.5 pr-3 font-medium">Task status</th>
                    <th className="py-1.5 font-medium">Node status</th>
                    {canRemove && <th className="py-1.5 pl-3 text-right font-medium">Action</th>}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-base">
                  {filtered.map((n) => (
                    <PlanTaskRow
                      key={n.task_id}
                      projectId={projectId}
                      planId={plan.id}
                      node={n}
                      orgRef={n.org_ref}
                      canRemove={canRemove}
                      members={members.data ?? []}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

// resumeNodeErrorMessage (T101) — turn the resume failure into an ACCURATE operator
// hint instead of a generic "try again". A node shows `paused` because its agent set
// the work item aside (pause_task) — and the agent typically did so to switch to
// ANOTHER task, so it is now active on that one. Operator resume then hits the
// single-active invariant (ResumeWorkByOperator → 409 agent_busy): the right answer
// is to explain WHY, not retry blindly. Maps the backend error CODE (ApiError.code);
// any unmapped error keeps the original generic copy.
function resumeNodeErrorMessage(error: unknown): string {
  const code = error instanceof ApiError ? error.code : '';
  switch (code) {
    case 'agent_busy':
      // The agent paused this item to work another; it can't be double-activated.
      return "Its agent is busy on another task — it'll resume this one when that finishes, or the agent can resume it from its side.";
    case 'node_not_paused':
      return 'Nothing to resume — this node has no paused task (it may have already resumed).';
    case 'plan_not_running':
      return "The plan isn't running, so its nodes can't be resumed.";
    default:
      return "Couldn't resume this node. Please try again.";
  }
}

// PlanTaskRow — one task-list row. When the plan is draft, the trailing cell
// holds a "Remove" button → useRemoveTaskFromPlan(task_id) (task returns to the
// Backlog on success via query invalidation). #218: a remove failure surfaces a
// friendly inline message in the row, never a raw API error.
function PlanTaskRow({
  projectId,
  planId,
  node,
  orgRef,
  canRemove,
  members,
}: {
  projectId: string;
  planId: string;
  node: PlanNode;
  orgRef?: string;
  canRemove: boolean;
  members: MemberResult[];
}): React.ReactElement {
  const remove = useRemoveTaskFromPlan(projectId, planId);
  // Confirmation state for the trash-icon remove button. First click arms it
  // (shows "Confirm?" inline); second click executes; clicking elsewhere or
  // after 3 s resets back to the icon.
  const [confirmArmed, setConfirmArmed] = useState(false);
  const confirmTimer = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const armConfirm = () => {
    setConfirmArmed(true);
    if (confirmTimer.current) clearTimeout(confirmTimer.current);
    confirmTimer.current = setTimeout(() => setConfirmArmed(false), 3000);
  };
  const cancelConfirm = () => {
    setConfirmArmed(false);
    if (confirmTimer.current) clearTimeout(confirmTimer.current);
  };
  const handleRemoveClick = () => {
    if (!confirmArmed) {
      armConfirm();
    } else {
      cancelConfirm();
      remove.mutate(node.task_id);
    }
  };
  // Reset confirmation if the row unmounts (e.g., task removed by other means).
  useEffect(() => () => { if (confirmTimer.current) clearTimeout(confirmTimer.current); }, []);
  // T53: operator resume of a paused node (its agent set the work item aside).
  const resume = useResumePausedNode(projectId, planId);
  // T41 inline 分派: reassigning is NOT draft-gated (allowed regardless of plan
  // status). assign("") would set an empty assignee; the dedicated unassign
  // endpoint is the established "clear assignee" path, so route "" → unassign.
  const assign = useAssignTask(projectId, node.task_id);
  const unassign = useUnassignTask(projectId, node.task_id);
  const assignError = assign.isError || unassign.isError;
  const onAssigneeChange = (next: string) => {
    if (next === '') unassign.mutate();
    else assign.mutate({ assignee: next });
  };
  const title = node.title || refLabel(node.org_ref, node.task_id);
  // T147: ONE assignee control. Build the dropdown options — "" = Unassigned
  // (routes to the unassign endpoint), then each project member with an avatar
  // leading so the (single) dropdown trigger shows the current assignee's
  // avatar + name. Mirrors the old <option> list (memberRef + "name (kind)").
  const assigneeOptions: EntityOption[] = useMemo(() => {
    const opts: EntityOption[] = [{ value: '', label: 'Unassigned' }];
    for (const m of members) {
      const name = m.display_name ?? normalizeIdentityRef(m.identity_id);
      opts.push({
        value: memberRef(m),
        label: name,
        badge: m.kind,
        leading: <Avatar name={name} kind={m.kind === 'agent' ? 'agent' : 'human'} size="sm" />,
      });
    }
    return opts;
  }, [members]);
  return (
    <tr data-testid="plan-task-row" data-task-id={node.task_id}>
      {/* v2.9.1 UX point 1: human Task id (T-number) column. */}
      <td className="py-1.5 pr-3 align-top">
        <TaskIdTag taskId={node.task_id} orgRef={orgRef} testId="plan-row-taskid" />
      </td>
      <td className="max-w-[18rem] py-1.5 pr-3 text-text-primary" title={node.title}>
        <TaskTitleLink
          projectId={projectId}
          taskId={node.task_id}
          title={title}
        />
        {remove.isError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-task-remove-error-${node.task_id}`}
          >
            Couldn't remove this task from the plan. Please try again.
          </span>
        )}
      </td>
      <td className="py-1.5 pr-3 align-top">
        {/* T147: a SINGLE dropdown — its trigger shows the current assignee
            (avatar + name), and opening it reassigns. Replaces the old redundant
            pair (a read-only AssigneeTag stacked above a separate <select> that
            showed the same value). "" routes to the unassign endpoint. */}
        {/* Width is driven by min-width (not max): the Task-List table is
            width-constrained, so a `max-w` cap never widens the column — it
            only ever shrinks toward the min. The avatar + padding + chevron eat
            ~4rem of the trigger, so an 11rem column left only ~15 chars of text
            and truncated common handles (agent-center-tester1 → "agent-center-te…").
            15rem fits ~25 chars; the table's overflow-x-auto scrolls if a row
            ever needs more, and EntitySelect keeps a title tooltip as a fallback. */}
        <div className="min-w-[15rem] max-w-[20rem]">
          <EntitySelect
            testId="plan-row-assign"
            options={assigneeOptions}
            value={node.assignee_ref ?? ''}
            onChange={onAssigneeChange}
            ariaLabel={`Reassign ${title}`}
            disabled={assign.isPending || unassign.isPending}
            placeholder="Unassigned"
            searchPlaceholder="Search members…"
          />
        </div>
        {assignError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-task-assign-error-${node.task_id}`}
          >
            Couldn't reassign this task. Please try again.
          </span>
        )}
      </td>
      <td className="py-1.5 pr-3">
        <StatusChip status={node.task_status} />
      </td>
      <td className="py-1.5">
        <span className="inline-flex items-center gap-1.5">
          <NodeStateChip status={node.node_status} />
          {/* T570: a DONE node shows WHEN it completed (statusChangedAt). Rendered
              next to the chip, muted, with the full timestamp on hover. */}
          {node.node_status === 'done' && node.completed_at && (
            <span
              className="text-[0.625rem] text-text-muted"
              data-testid="plan-row-completed-at"
              title={node.completed_at}
            >
              {formatLocalTime(node.completed_at)}
            </span>
          )}
          {/* Stage B (#283): archive badge is ORTHOGONAL — coexists with the
              node-status chip when the plan (and thus the task) is archived. */}
          <TaskArchivedBadge archived={node.archived} taskId={node.task_id} />
          {/* T53: a paused node (agent set its work item aside) gets an operator
              Resume action that resumes the node + wakes its agent. */}
          {node.node_status === 'paused' && (
            <button
              type="button"
              className="rounded border border-status-stone-border bg-status-stone-bg px-2 py-0.5 text-[0.6875rem] font-semibold text-status-stone-fg hover:opacity-80 disabled:opacity-50"
              disabled={resume.isPending}
              aria-label={`Resume ${title}`}
              title="Resume this paused node (wake its agent)"
              data-testid={`plan-node-resume-${node.task_id}`}
              onClick={() => resume.mutate(node.task_id)}
            >
              {resume.isPending ? 'Resuming…' : 'Resume'}
            </button>
          )}
        </span>
        {resume.isError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-node-resume-error-${node.task_id}`}
          >
            {resumeNodeErrorMessage(resume.error)}
          </span>
        )}
      </td>
      {canRemove && (
        <td className="py-1.5 pl-3 text-right">
          <span className="inline-flex items-center justify-end gap-1.5">
            {confirmArmed && (
              <span
                className="text-[0.6875rem] font-semibold text-danger"
                aria-live="polite"
              >
                Confirm?
              </span>
            )}
            <button
              type="button"
              className={`rounded p-1 transition-colors disabled:opacity-50 ${
                confirmArmed
                  ? 'text-danger hover:bg-danger/10'
                  : 'text-text-muted hover:bg-bg-subtle hover:text-text-primary'
              }`}
              disabled={remove.isPending}
              aria-label={`Remove ${node.title || refLabel(node.org_ref, node.task_id)} from plan`}
              title={confirmArmed ? 'Click again to confirm removal' : 'Remove from plan (back to backlog)'}
              data-testid={`plan-task-remove-${node.task_id}`}
              onClick={handleRemoveClick}
              onBlur={cancelConfirm}
            >
              {/* Single-stroke trash icon, 16×16 viewBox, strokeWidth 1.5 */}
              <svg
                xmlns="http://www.w3.org/2000/svg"
                viewBox="0 0 16 16"
                width="16"
                height="16"
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
              >
                {/* lid */}
                <path d="M2 4h12" />
                {/* handle */}
                <path d="M5.5 4V2.5h5V4" />
                {/* body */}
                <path d="M3 4l.75 9.5h8.5L13 4" />
                {/* inner lines */}
                <path d="M6 7v4.5M8 7v4.5M10 7v4.5" />
              </svg>
            </button>
          </span>
        </td>
      )}
    </tr>
  );
}

// ── Plan conversation side (REUSE ConversationView) ──────────────────────────
// This is where the orchestrator @-dispatches + discussion appear (bound post
// #266). Render the Plan's conversation by its conversation_id. Empty
// conversation_id → friendly "initializing" state (don't crash).
function PlanConversationSide({
  conversationId,
  maximized,
  onToggleMaximize,
}: {
  conversationId: string;
  // T347: maximize state is lifted to PlanDetail so the toggle can live on the tab
  // row (@oopslink). When maximized the chat is a full-viewport overlay (composer
  // pinned); a restore button sits in the overlay. Esc also restores.
  maximized: boolean;
  onToggleMaximize: () => void;
}): React.ReactElement {
  const conv = useConversation(conversationId || undefined);
  const isMobile = useIsMobile(); // T324: embed the conv sidebar on desktop only
  // Embedded sidebar collapse — lifted so the restore button can sit in the
  // maximize header bar instead of a standalone w-9 strip.
  const [embeddedCollapsed, setEmbeddedCollapsed] = useState(() => {
    try { return window.localStorage.getItem('ac.convsidebar.embedded.collapsed') === '1'; } catch { return false; }
  });
  const toggleEmbeddedCollapsed = (v: boolean): void => {
    setEmbeddedCollapsed(v);
    try { window.localStorage.setItem('ac.convsidebar.embedded.collapsed', v ? '1' : '0'); } catch { /* */ }
  };
  useEffect(() => {
    if (!maximized) return;
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') onToggleMaximize();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      document.body.style.overflow = prevOverflow;
      window.removeEventListener('keydown', onKey);
    };
  }, [maximized, onToggleMaximize]);

  return (
    <SenderSidebarProvider>
      {/* T328: the "P27 · chat" sub-header was removed — the plan id now lives on
          the tab row. T347: the maximize toggle moved to the tab row; when maximized
          the chat is a full-viewport overlay with a restore button (top-right). */}
      <section
        className={
          maximized
            ? 'fixed inset-0 z-50 m-0 flex min-h-0 flex-col bg-bg-base p-3'
            : 'flex min-h-0 flex-1 flex-col'
        }
        data-testid="plan-conversation"
        data-maximized={maximized ? 'true' : 'false'}
      >
        {maximized && (
          <div className="mb-1 flex items-center justify-end gap-1">
            {!isMobile && embeddedCollapsed && (
              <EmbeddedSidebarToggle collapsed={embeddedCollapsed} onExpand={() => toggleEmbeddedCollapsed(false)} />
            )}
            <button
              type="button"
              onClick={onToggleMaximize}
              data-testid="plan-conversation-restore"
              aria-label="Restore chat"
              title="Restore (Esc)"
              className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              <PlanChatRestoreIcon />
            </button>
          </div>
        )}
        {!conversationId ? (
          <p
            className="rounded border border-dashed border-border-base p-4 text-xs italic text-text-muted"
            data-testid="plan-conversation-initializing"
          >
            Conversation initializing — the plan's chat is being set up.
          </p>
        ) : (
          <div
            // T341: flex-1 fills the bounded card (composer pinned); no min-h floor.
            // De-nested (@oopslink "太挤"): no inner border/rounding on desktop — the
            // chat sits flush in the card (header border-b above, composer border-t
            // below already frame it), removing the box-in-a-box double frame.
            className="flex min-h-0 flex-1 overflow-hidden"
            data-testid="plan-conversation-body"
          >
            {/* T327: min-w-0 lets the messages column shrink so the embedded
                sidebar (+ its collapse toggle) stays on-screen, not pushed off-right. */}
            <div className="flex min-h-0 min-w-0 flex-1 flex-col">
              <ConversationView surface="task-thread" conversationId={conversationId} />
              {conv.isError && (
                <p className="p-2 text-[0.6875rem] text-text-muted">
                  Couldn't refresh conversation details.
                </p>
              )}
            </div>
            {/* 双栏方案 B: in the DOCKED desktop view the right-hand PlanInfoRail
                owns Participants/Threads/Files, so the chat no longer embeds its
                own sidebar (that would be a 3rd column). Keep it only when the chat
                is MAXIMIZED (full-screen overlay — the rail isn't visible then);
                mobile still uses the col④ bottom sheet. */}
            {!isMobile && maximized && conv.data && (
              <EmbeddedConversationSidebar
                conversationId={conversationId}
                participants={conv.data.participants ?? []}
                collapsed={embeddedCollapsed}
                onToggleCollapsed={toggleEmbeddedCollapsed}
              />
            )}
          </div>
        )}
        {!maximized && (
          <p className="mt-2 hidden text-[0.6875rem] text-text-muted md:block">
            Dispatch = @assignee in this conversation (notify human / wake agent); also the place to discuss this plan.
          </p>
        )}
      </section>
    </SenderSidebarProvider>
  );
}

// Maximize / restore glyphs for the plan chat (single-stroke SVGs, no-emoji rule).
function PlanChatMaximizeIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M8 4H4v4M16 8V4h-4M4 12v4h4M12 16h4v-4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
// T348: DAG compact (zoom-to-fit) — two arrows pointing inward = "compress to fit".
function PlanCompactIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M2 10h6M6 6.5 9.5 10 6 13.5M18 10h-6M14 6.5 10.5 10 14 13.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
function PlanChatRestoreIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M4 8h4V4M12 4v4h4M8 16v-4H4M16 12h-4v4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
