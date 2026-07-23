import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation, Trans } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useNavigate, useParams } from 'react-router-dom';
import { OrgLink, orgPath, useOptionalOrgContext } from '@/OrgContext';
import { useProject } from '@/api/projects';
import { ApiError } from '@/api/client';
import {
  usePlan,
  usePlanGraph,
  usePlanStages,
  useStartPlan,
  useStopPlan,
  useAddDependency,
  useRemoveDependency,
  useRemoveTaskFromPlan,
  useResumePausedNode,
  usePatchPlan,
  useDeletePlan,
  useArchivePlan,
  friendlyDestructivePlanError,
  type Plan,
  type PlanNode,
  type PlanNodeStatus,
  type PlanGraphNode,
  type PlanGraphEdge,
  type PlanGraphEdgeKind,
  type PatchPlanInput,
  type PlanStage,
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
import { ObjectAuditTimeline } from '@/components/ObjectAuditTimeline';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { Avatar } from '@/components/Avatar';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';
import { StatusChip, refLabel, fullDateTime } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, AutoAdvancingIndicator, TaskArchivedBadge, planProgressLabel, PlanRefTag } from '@/components/planDisplay';
import { ConversationView } from '@/components/ConversationView';
import { ConversationSidebar, EmbeddedConversationSidebar, EmbeddedSidebarToggle } from '@/components/ConversationSidebar';
import { ContextPanel, useContextPanelMobileTrigger } from '@/shell/contextPanel';
import { ContextPanelMobileButton } from '@/components/ContextPanelMobileButton';
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
type Tab = 'chat' | 'dag' | 'tasks' | 'history';

// (v2.9.1 point 4) The DAG↔chat resizable side-splitter (v2.9 Stage A8) was
// removed — chat is now a top-level tab (see the 3-tab layout above), so the
// chat-width state / localStorage persistence / lg-breakpoint splitter are gone.

export default function PlanDetail(): React.ReactElement {
  const { t } = useTranslation('work');
  const { id = '', planId = '' } = useParams<{ id: string; planId: string }>();
  const project = useProject(id);
  const plan = usePlan(id, planId);
  // T184: the plan conversation gets the shared col④ sidebar too. Resolve it for
  // participants (enabled:!!id makes this a no-op until the plan loads).
  const planConv = useConversation(plan.data?.conversation_id);
  // T324: desktop embeds the conversation sidebar inside the chat tab; mobile
  // keeps it in the col④ bottom sheet (mounted below for mobile only).
  const isMobile = useIsMobile();
  // T324 follow-up: the col④ panel we mount for mobile (below) lands in a sheet
  // that starts closed — this opens it from the ⓘ in the header row.
  const ctxTrigger = useContextPanelMobileTrigger();
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
          message={t('plan.detail.loadError')}
          error={plan.error}
          testId="plan-not-found"
        />
        <OrgLink
          to={`/projects/${encodeURIComponent(id)}/plans`}
          className="text-xs text-accent hover:underline"
        >
          {t('plan.detail.backToPlans')}
        </OrgLink>
      </section>
    );
  }
  if (!plan.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-PlanDetail">
        {t('plan.detail.lookupFailed')}
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
            { label: t('plan.detail.breadcrumb.projects'), to: '/projects' },
            { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
            { label: t('plan.detail.breadcrumb.plans'), to: `/projects/${encodeURIComponent(id)}/plans` },
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
        {isMobile && (
          <PlanDetailHeader
            projectId={id}
            plan={p}
            // Same gate as the <ContextPanel> below, so the ⓘ appears only when
            // the sheet actually has the conversation sidebar to show.
            onOpenContext={planConv.data && ctxTrigger ? ctxTrigger.open : undefined}
          />
        )}

        <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {!isMobile && <PlanTitleBar plan={p} />}

        {/* Tabs — Chat (default) / DAG / Task List. English-only labels (T132:
            the prior「(中文)」括注 removed). NO backlog tab (planning is on the
            Board). v2.9.1 point 4. */}
        {/* T328: the plan id (P27) sits on the tab row (right-aligned, into the
            empty space) — @oopslink — instead of a separate "P27 · chat" sub-header
            row inside the chat tab, saving a row (esp. on mobile). */}
        <div className="flex items-center gap-1 border-b border-border-base px-3 pt-2 md:px-6" data-testid="plan-tabs">
          <div className="flex min-w-0 items-center gap-1" role="tablist">
            <TabButton id="chat" active={tab === 'chat'} onSelect={setTab}>
              {t('plan.detail.tabs.chat')}
            </TabButton>
            <TabButton id="dag" active={tab === 'dag'} onSelect={setTab}>
              {t('plan.detail.tabs.dag')}
            </TabButton>
            <TabButton id="tasks" active={tab === 'tasks'} onSelect={setTab}>
              {t('plan.detail.tabs.tasks')}
            </TabButton>
            <TabButton id="history" active={tab === 'history'} onSelect={setTab}>
              {t('plan.detail.tabs.history')}
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
                aria-label={chatMaximized ? t('plan.detail.chat.restore') : t('plan.detail.chat.maximize')}
                title={chatMaximized ? t('plan.detail.chat.restoreEsc') : t('plan.detail.chat.maximize')}
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
                aria-label={dagCompact ? t('plan.detail.dag.resetZoomAria') : t('plan.detail.dag.compactAria')}
                title={dagCompact ? t('plan.detail.dag.resetZoom') : t('plan.detail.dag.compact')}
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
            {tab === 'dag' && (
              <PlanDag projectId={id} plan={p} compact={dagCompact} />
            )}
          </div>
          <div
            role="tabpanel"
            hidden={tab !== 'tasks'}
            data-testid="plan-panel-tasks"
            className={tab === 'tasks' ? 'min-h-0 flex-1 overflow-auto p-3 md:p-4' : undefined}
          >
            {tab === 'tasks' && <PlanTaskList projectId={id} plan={p} />}
          </div>
          {/* 变更记录 / audit-trail (change-log design §7): the plan's semantic change
              history as a tab (create/start/stop/node add-remove/dependency edits). */}
          <div
            role="tabpanel"
            hidden={tab !== 'history'}
            data-testid="plan-panel-history"
            className={tab === 'history' ? 'min-h-0 flex-1 overflow-auto p-3 md:p-4' : undefined}
          >
            {tab === 'history' && <ObjectAuditTimeline objectType="plan" projectId={id} objectId={p.id} />}
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
function PlanDetailHeader({
  projectId,
  plan,
  onOpenContext,
}: {
  projectId: string;
  plan: Plan;
  /** Opens the mobile col④ sheet. Undefined when there is no panel to open. */
  onOpenContext?: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
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
            {onOpenContext && <ContextPanelMobileButton onClick={onOpenContext} />}
            <span className="relative">
              <button
                type="button"
                onClick={() => setActionsOpen((o) => !o)}
                aria-expanded={actionsOpen}
                data-testid="plan-actions-toggle"
                className="inline-flex min-h-[2.75rem] items-center gap-1 rounded-full border border-border-base bg-bg-subtle px-2.5 text-xs font-medium text-text-secondary whitespace-nowrap"
              >
                {t('plan.detail.actions.menu')} <span aria-hidden="true">▾</span>
              </button>
              {actionsOpen && (
                <div className="absolute right-0 top-full z-20 mt-1 w-44 flex-col rounded-lg border border-border-base bg-bg-elevated p-1 shadow-2" data-testid="plan-actions" role="menu">
                  {plan.description.trim() !== '' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setMobileGoalOpen((v) => !v); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">
                      {mobileGoalOpen ? t('plan.detail.actions.hideGoal') : t('plan.detail.actions.showGoal')}
                    </button>
                  )}
                  {plan.status === 'running' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); stop.mutate(); }} disabled={stop.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50">{t('plan.detail.actions.stop')}</button>
                  )}
                  {plan.status !== 'archived' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setEditing(true); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">{t('plan.detail.actions.edit')}</button>
                  )}
                  {plan.status === 'draft' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); start.mutate(); }} disabled={start.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50">{t('plan.detail.actions.start')}</button>
                  )}
                  {canDestroy && (
                    <>
                      <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('archive'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">{t('plan.detail.actions.archive')}</button>
                      <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('delete'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle">{t('plan.detail.actions.delete')}</button>
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
            <dt className="uppercase tracking-wide text-[0.625rem]">{t('plan.detail.meta.progress')}</dt>
            <dd className="text-text-secondary" data-testid="plan-progress">{planProgressLabel(plan.progress)}</dd>
          </div>
          {plan.target_date && (
            <div className="flex items-center gap-1">
              <dt className="uppercase tracking-wide text-[0.625rem]">{t('plan.detail.meta.target')}</dt>
              <dd className="text-text-secondary" title={plan.target_date}>{formatLocalTime(plan.target_date)}</dd>
            </div>
          )}
          <div className="flex items-center gap-1">
            <dt className="uppercase tracking-wide text-[0.625rem]">{t('plan.detail.meta.creator')}</dt>
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
            {t('plan.detail.actions.menu')} <span aria-hidden="true">▾</span>
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
            {t('plan.detail.lifecycle.stop')}
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
            {t('plan.detail.actions.edit')}
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
            {t('plan.detail.lifecycle.start')}
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
              title={t('plan.detail.actions.archiveTitle')}
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base md:hover:text-text-primary"
            >
              {t('plan.detail.actions.archive')}
            </button>
            <button
              type="button"
              data-testid="plan-delete-btn"
              onClick={() => { setActionsOpen(false); setConfirming('delete'); }}
              title={t('plan.detail.actions.deleteTitle')}
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-danger md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-danger md:hover:bg-bg-base"
            >
              {t('plan.detail.actions.delete')}
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
              <button type="button" onClick={() => setMobileGoalOpen(false)} aria-label={t('plan.detail.goal.close')} className="absolute right-2 top-2 inline-flex h-8 w-8 items-center justify-center rounded-full text-text-muted hover:bg-bg-subtle hover:text-text-primary">
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
            title={t('plan.detail.goal.title')}
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
              {goalOpen ? t('plan.detail.goal.showLess') : t('plan.detail.goal.showMore')}
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
  const { t } = useTranslation('work');
  const pct = total > 0 ? Math.round((done / total) * 100) : 0;
  return (
    <div data-testid="plan-progress-bar" aria-label={t('plan.detail.progressComplete', { pct })}>
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
  const { t } = useTranslation('work');
  const resolveName = useDisplayNameResolver();
  const start = useStartPlan(projectId, plan.id);
  const stop = useStopPlan(projectId, plan.id);
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState<null | 'delete' | 'archive'>(null);
  const [goalOpen, setGoalOpen] = useState(false);
  // @oopslink: Up next is collapsible (mirrors the unmerged-branch panel). Default
  // open so the queue stays visible; the header chevron toggles it.
  const [upNextOpen, setUpNextOpen] = useState(true);
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
      {/* Status + Progress (merged, top of rail) — the plan STATUS chip and the
          PROGRESS bar now share ONE block at the very TOP of the rail (@oopslink:
          进度放到顶端 + plan 状态和进度合并到一起). The standalone Progress section
          that used to sit below Participants was removed. Owns plan-detail-meta. */}
      <div className="space-y-3 border-b border-border-base p-5" data-testid="plan-detail-meta">
        <div className="flex flex-wrap items-center gap-2">
          <PlanStatusChip status={plan.status} />
          {plan.status === 'running' && <AutoAdvancingIndicator variant="detail" />}
          <PlanFailedIndicator hasFailed={plan.has_failed} />
          <span className="ml-auto text-xs text-text-muted">{t('plan.detail.rail.nodesDone')}</span>
        </div>
        <PlanProgressBar done={plan.progress.done} total={plan.progress.total} />
        {plan.target_date && (
          <p className="text-xs text-text-muted">
            <span className="uppercase tracking-wide">{t('plan.detail.meta.target')}</span>{' '}
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
              {t('plan.detail.lifecycle.stop')}
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
              {t('plan.detail.lifecycle.start')}
            </button>
          )}
          {plan.status !== 'archived' && (
            <button type="button" data-testid="plan-edit-btn" onClick={() => setEditing(true)} className={railBtn}>
              {t('plan.detail.actions.edit')}
            </button>
          )}
          {canDestroy && (
            <>
              <button type="button" data-testid="plan-archive-btn" onClick={() => setConfirming('archive')} className={railBtn}>
                {t('plan.detail.actions.archive')}
              </button>
              <button
                type="button"
                data-testid="plan-delete-btn"
                onClick={() => setConfirming('delete')}
                className={`${railBtn} border-danger text-danger hover:text-danger`}
              >
                {t('plan.detail.actions.delete')}
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
          <h3 className="text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">{t('plan.detail.goal.heading')}</h3>
          <button
            type="button"
            data-testid="plan-creator-tag"
            onClick={() => setAgentRef(plan.creator_ref)}
            title={t('plan.detail.openActivity', { name: creatorLabel })}
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
              title={t('plan.detail.goal.title')}
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
                {goalOpen ? t('plan.detail.goal.showLess') : t('plan.detail.goal.showMore')}
              </button>
            )}
          </>
        ) : (
          <p className="text-sm italic text-text-muted">{t('plan.detail.goal.empty')}</p>
        )}
      </div>

      {/* Participants — avatars open the agent-activity sidebar. Sits below the
          merged Status+Progress block and Goal (Progress now lives at the top). */}
      {participants.length > 0 && (
        <div className="border-b border-border-base p-5">
          <h3 className="mb-3 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted">{t('plan.detail.participants')}</h3>
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
                  aria-label={t('plan.detail.openActivity', { name: label })}
                  className="rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                >
                  <Avatar name={label} kind={(pt.kind as 'agent' | 'human') ?? (refKind(pt.identity_id) === 'agent' ? 'agent' : 'human')} />
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Up next (collapsible) */}
      <div className="border-b border-border-base p-5" data-testid="plan-upnext-section">
        <button
          type="button"
          onClick={() => setUpNextOpen((o) => !o)}
          aria-expanded={upNextOpen}
          data-testid="plan-upnext-toggle"
          className="mb-3 flex w-full items-center gap-2 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <svg
            viewBox="0 0 12 12"
            aria-hidden="true"
            className={`h-3 w-3 shrink-0 transition-transform ${upNextOpen ? 'rotate-90' : ''}`}
          >
            <path d="M4 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>{t('plan.detail.upNext.title')}</span>
          {upNext.length > 0 && (
            <span className="inline-flex items-center rounded-full bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-bold text-text-secondary">
              {upNext.length}
            </span>
          )}
        </button>
        {upNextOpen && (upNext.length === 0 ? (
          <p className="text-xs text-text-muted">{t('plan.detail.upNext.empty')}</p>
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
                  {t('plan.detail.upNext.more', { count: upNextHidden })}
                </button>
              </li>
            )}
          </ul>
        ))}
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
  const { t } = useTranslation('work');
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
      title={t('plan.detail.deleteModal.title')}
      planName={plan.name}
      body={t('plan.detail.deleteModal.body')}
      confirmLabel={t('plan.detail.deleteModal.confirm')}
      pendingLabel={t('plan.detail.deleteModal.pending')}
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
  const { t } = useTranslation('work');
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
      title={t('plan.detail.archiveModal.title')}
      planName={plan.name}
      body={t('plan.detail.archiveModal.body')}
      confirmLabel={t('plan.detail.archiveModal.confirm')}
      pendingLabel={t('plan.detail.archiveModal.pending')}
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
  const { t } = useTranslation('work');
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
            {t('plan.detail.cancel')}
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

function friendlyPatchError(error: unknown, t: TFunction): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('archived')) {
    return t('plan.detail.editModal.errorArchived');
  }
  if (lower.includes('draft')) {
    // T238: name/goal edit any time; only the target date is draft-only.
    return t('plan.detail.editModal.errorDraft');
  }
  return t('plan.detail.editModal.errorGeneric');
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
  const { t } = useTranslation('work');
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
      aria-label={t('plan.detail.editModal.aria')}
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold">{t('plan.detail.editModal.title')}</h2>
        <label className="block text-xs font-medium" htmlFor="plan-edit-name">
          {t('plan.detail.editModal.name')}
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
          {t('plan.detail.editModal.goal')}
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
              {t('plan.detail.editModal.targetDate')}
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
            {friendlyPatchError(patch.error, t)}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="plan-edit-cancel"
          >
            {t('plan.detail.cancel')}
          </button>
          <button
            type="submit"
            disabled={patch.isPending || !name.trim()}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-edit-submit"
          >
            {patch.isPending ? t('plan.detail.editModal.saving') : t('plan.detail.editModal.save')}
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

// nodeVisualCls (mockup `.node.n-running` / `.node.n-done`): a running node gets a
// soft status-colored glow ring (draws the eye to active work); a terminal `done`
// node dims to 60% opacity (reads as "settled", not competing with active nodes).
// Additive to the card's own status border — never replaces it.
function nodeVisualCls(status: PlanNodeStatus): string {
  if (status === 'running') return 'ring-2 ring-status-amber-border/35';
  if (status === 'done') return 'opacity-60';
  return '';
}

function NodeStateChip({ status }: { status: PlanNodeStatus }): React.ReactElement {
  const { t } = useTranslation('work');
  const s = NODE_STATE[status] ?? NODE_STATE.blocked;
  const known = status in NODE_STATE ? status : 'blocked';
  return (
    <span
      className={`inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide ${s.cls}`}
      data-testid="node-state-chip"
      data-node-status={status}
    >
      {s.icon}
      {t(`plan.detail.nodeStatus.${known}`)}
    </span>
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
  const { t } = useTranslation('work');
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
        title={t('plan.detail.openActivity', { name: label })}
        aria-label={t('plan.detail.openActivity', { name: label })}
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
// Layered TOP→BOTTOM layout from node.depends_on:
//   level(n) = 0 if no (in-plan) deps else max(level(dep))+1   (longest-path)
//   y = level * (NODE_H + LEVEL_GAP);  x = even horizontal spread within the level.
// Edges: SVG path from each dep node's BOTTOM-mid → this node's TOP-mid, with an
// arrow marker (upstream → downstream). node_status is DERIVED → display only.
const COL_W = 200;
const NODE_W = 168;
const NODE_H = 84;
const PAD_X = 14;
const PAD_Y = 16;
// T800: control-node marker cell width (circle 56 / diamond 64 + breathing room) and
// the uniform horizontal gap between columns. A level holding only control markers
// uses CTRL_W instead of the full card width, so Start/End and a condition diamond
// don't float in an over-wide column (the "过大空列" around a condition).
const CTRL_W = 76;
const COL_GAP = COL_W - NODE_W; // 32 — preserves the prior business-column spacing
// Top-to-bottom layout: LEVEL_GAP is the vertical gap between successive dependency
// LEVELS (each level is a horizontal row, flow runs downward), sized to leave room
// for the connecting arrows. COL_GAP is reused as the horizontal gap between sibling
// nodes within a level.
const LEVEL_GAP = 48;

// DagCanvas is the shared DESKTOP scroll viewport for both DAG renderers (graph +
// legacy). It (1) CENTERS the graph in view by default — small graphs are flex-
// centered, larger ones scroll-centered on mount / when the content size changes —
// and (2) supports GRAB-TO-PAN: drag the background to pan the whole canvas. Panning
// starts only on the background (SVG / dot-grid / card body) — a pointerdown that
// lands on an interactive element (node link, assignee, button) is left alone so
// clicks still work. Re-centers when contentW/contentH change (e.g. the compact toggle).
// Zoom range/step mirror the design mockup (docs/design/assets/plan-dag-canvas-mockup.html):
// 50%–150% in 10-point steps via the buttons, 8-point steps via ctrl/cmd+wheel.
const ZOOM_MIN = 0.5;
const ZOOM_MAX = 1.5;
const ZOOM_STEP = 0.1;
const ZOOM_WHEEL_STEP = 0.08;

function DagCanvas({
  contentW,
  contentH,
  compact,
  testId,
  legend,
  children,
}: {
  contentW: number;
  contentH: number;
  compact: boolean;
  testId: string;
  // Bottom bar rendered INSIDE the shared canvas-shell (mockup's `.legend-fixed`) —
  // owned by the caller (state-chip legend for the legacy DAG, edge-kind legend for
  // the graph DAG) so DagCanvas stays agnostic of which legend applies.
  legend?: React.ReactNode;
  children: React.ReactNode;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const ref = useRef<HTMLDivElement>(null);
  // T348 kept the compact toggle as a coarse "zoom to fit a long plan" preset; this
  // adds the mockup's continuous zoom control (buttons + wheel + Fit view) as an
  // ADDITIONAL transform layered on top of it, so the two compose instead of
  // colliding — compact still drives the `plan-dag-scaler` scale asserted by tests.
  const [zoom, setZoom] = useState(1);

  // Center the view on the content's middle whenever the content size changes (mount,
  // compact toggle, graph edit, zoom). max(0, …) keeps small content pinned so flex
  // centering (below) can take over.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.scrollLeft = Math.max(0, (el.scrollWidth - el.clientWidth) / 2);
    el.scrollTop = Math.max(0, (el.scrollHeight - el.clientHeight) / 2);
  }, [contentW, contentH, compact, zoom]);

  // Reset to 100% whenever the underlying content changes shape (compact toggle,
  // graph edit) so zoom never gets "stuck" showing a stale scale for new content.
  useEffect(() => {
    setZoom(1);
  }, [contentW, contentH, compact]);

  const clampZoom = (z: number) => Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, z));
  const zoomIn = () => setZoom((z) => clampZoom(z + ZOOM_STEP));
  const zoomOut = () => setZoom((z) => clampZoom(z - ZOOM_STEP));
  const zoomFit = () => {
    const el = ref.current;
    if (!el || contentW <= 0) return;
    const available = el.clientWidth - 24;
    setZoom(Math.max(ZOOM_MIN, Math.min(1, available / contentW)));
  };

  // ctrl/cmd+wheel zoom (mirrors the mockup script) — a plain wheel still scrolls.
  const onWheel = (e: React.WheelEvent<HTMLDivElement>) => {
    if (!e.ctrlKey && !e.metaKey) return;
    e.preventDefault();
    setZoom((z) => clampZoom(z + (e.deltaY < 0 ? ZOOM_WHEEL_STEP : -ZOOM_WHEEL_STEP)));
  };

  // Grab-to-pan: a background mousedown starts a drag that scrolls the canvas; the
  // move/up listeners live on window so the drag continues even if the cursor leaves
  // the canvas, and are torn down on release. A mousedown on an interactive child
  // (node link / button) is left alone so clicks still work.
  const onMouseDown = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.button !== 0) return; // primary button only
    if ((e.target as HTMLElement).closest('a, button, input, select, textarea, [role="button"]')) return;
    const el = ref.current;
    if (!el) return;
    const start = { x: e.clientX, y: e.clientY, sl: el.scrollLeft, st: el.scrollTop };
    const onMove = (ev: MouseEvent) => {
      el.scrollLeft = start.sl - (ev.clientX - start.x);
      el.scrollTop = start.st - (ev.clientY - start.y);
    };
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  };

  return (
    // canvas-shell (mockup): one bordered/rounded/overflow-hidden frame housing the
    // floating zoom-controls overlay, the scrollable dot-grid canvas, and the
    // legend as a bottom bar — instead of the canvas + legend being loose siblings.
    <div className="relative hidden overflow-hidden rounded-lg border border-border-base md:flex md:min-h-0 md:flex-1 md:flex-col" data-testid="plan-dag-canvas-shell">
      <div
        className="absolute right-3 top-3 z-10 flex items-center gap-0.5 overflow-hidden rounded-lg border border-border-strong bg-bg-elevated shadow-2"
        data-testid="plan-dag-zoom-controls"
      >
        <button
          type="button"
          onClick={zoomOut}
          disabled={zoom <= ZOOM_MIN}
          aria-label={t('plan.detail.dag.zoomOut', { defaultValue: 'Zoom out' })}
          title={t('plan.detail.dag.zoomOut', { defaultValue: 'Zoom out' })}
          data-testid="plan-dag-zoom-out"
          className="flex h-7 w-7 items-center justify-center text-sm font-semibold text-text-secondary hover:bg-bg-subtle hover:text-text-primary disabled:opacity-40"
        >
          −
        </button>
        <span
          className="min-w-[2.75rem] select-none border-x border-border-base px-1.5 text-center font-mono text-[0.6875rem] text-text-muted"
          data-testid="plan-dag-zoom-pct"
        >
          {Math.round(zoom * 100)}%
        </span>
        <button
          type="button"
          onClick={zoomIn}
          disabled={zoom >= ZOOM_MAX}
          aria-label={t('plan.detail.dag.zoomIn', { defaultValue: 'Zoom in' })}
          title={t('plan.detail.dag.zoomIn', { defaultValue: 'Zoom in' })}
          data-testid="plan-dag-zoom-in"
          className="flex h-7 w-7 items-center justify-center text-sm font-semibold text-text-secondary hover:bg-bg-subtle hover:text-text-primary disabled:opacity-40"
        >
          +
        </button>
        <button
          type="button"
          onClick={zoomFit}
          data-testid="plan-dag-zoom-fit"
          className="flex h-7 items-center border-l border-border-base px-2.5 text-[0.6875rem] font-semibold text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
        >
          {t('plan.detail.dag.zoomFit', { defaultValue: 'Fit view' })}
        </button>
      </div>
      <div
        ref={ref}
        className="relative min-h-0 flex-1 cursor-grab overflow-auto bg-bg-subtle hex-dot-grid active:cursor-grabbing"
        data-testid={testId}
        data-compact={compact ? 'true' : 'false'}
        data-zoom={zoom}
        onMouseDown={onMouseDown}
        onWheel={onWheel}
      >
        {/* min-w/h-full + flex centering handles the small-graph case (centered in the
            viewport); the scroll-center effect handles the larger-than-viewport case. */}
        <div className="flex min-h-full min-w-full items-center justify-center">
          <div style={{ width: contentW * zoom, height: contentH * zoom }}>
            <div
              style={{
                width: contentW,
                height: contentH,
                transform: zoom === 1 ? undefined : `scale(${zoom})`,
                transformOrigin: 'top left',
              }}
            >
              {children}
            </div>
          </div>
        </div>
      </div>
      {legend && (
        <div
          className="flex shrink-0 flex-wrap items-center gap-4 border-t border-border-base bg-bg-elevated px-3.5 py-2"
          data-testid="plan-dag-canvas-legend-bar"
        >
          {legend}
        </div>
      )}
    </div>
  );
}

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

  // Reserve a TOP gutter band for the Start anchor when there are any nodes, so
  // real nodes are shifted DOWN one band (Start sits at y≈PAD_Y, real level-0 nodes
  // below). Empty plan ⇒ no gutter, no anchors.
  const hasNodes = nodes.length > 0;
  const SYNTH_ROW = NODE_H + LEVEL_GAP; // height of each synthetic gutter band
  const baseY = PAD_Y + (hasNodes ? SYNTH_ROW : 0);

  // Top-to-bottom flow: each level is a horizontal ROW stacked downward; sibling
  // nodes within a level spread left→right.
  const positioned: Positioned[] = [];
  let maxRows = 0;
  for (const [lvl, group] of byLevel) {
    maxRows = Math.max(maxRows, group.length);
    group.forEach((node, row) => {
      positioned.push({
        node,
        level: lvl,
        x: PAD_X + row * (NODE_W + COL_GAP),
        y: baseY + lvl * (NODE_H + LEVEL_GAP),
      });
    });
  }

  // Roots = real level-0 nodes (no in-plan deps). Leaves = nodes that no other
  // in-plan node depends on. Start → every root; every leaf → End.
  const dependedOn = new Set<string>();
  for (const n of nodes) for (const d of depsOf(n)) dependedOn.add(d);

  const contentWidth = Math.max(PAD_X * 2 + maxRows * (NODE_W + COL_GAP) - COL_GAP, 200);
  const midX = contentWidth / 2;

  let start: SyntheticAnchor | null = null;
  let end: SyntheticAnchor | null = null;
  if (hasNodes) {
    const roots = positioned.filter((p) => p.level === 0);
    const leaves = positioned.filter((p) => !dependedOn.has(p.node.task_id));
    start = {
      cx: midX,
      cy: PAD_Y + SYNTH_R,
      // edge endpoint = root node's TOP-mid
      links: roots.map((p) => ({ taskId: p.node.task_id, x: p.x + NODE_W / 2, y: p.y })),
    };
    const endCy = baseY + (maxLevel + 1) * (NODE_H + LEVEL_GAP) + SYNTH_R;
    end = {
      cx: midX,
      cy: endCy,
      // edge endpoint = leaf node's BOTTOM-mid
      links: leaves.map((p) => ({ taskId: p.node.task_id, x: p.x + NODE_W / 2, y: p.y + NODE_H })),
    };
  }

  // Height spans from PAD_Y (Start) to the End marker (when present), else the real
  // layout extent.
  const realBottom = baseY + maxLevel * (NODE_H + LEVEL_GAP) + NODE_H;
  const height = hasNodes
    ? (end ? end.cy + SYNTH_R + PAD_Y : realBottom + PAD_Y)
    : PAD_Y * 2 + (maxLevel + 1) * (NODE_H + LEVEL_GAP) - LEVEL_GAP;
  const width = contentWidth;
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
  const { t } = useTranslation('work');
  const label = kind === 'start' ? t('plan.detail.dag.anchorStart') : t('plan.detail.dag.anchorEnd');
  // Mockup `.terminal.start` / `.terminal.end`: Start is a SOLID filled accent
  // disc (the entry point, glowing) — the one node that isn't a status card, so
  // it needs to read as unmistakably different. End stays a light disc but with
  // a heavier `done`-toned ring (the flow's resting state), matching the node
  // palette's success token rather than a plain neutral border.
  return (
    <div
      className={`absolute flex items-center justify-center rounded-full text-[0.625rem] font-extrabold uppercase tracking-wide ${
        kind === 'start'
          ? 'border-[1.5px] border-accent bg-accent text-white shadow-[0_4px_16px_-4px_var(--color-accent)]'
          : 'border-2 border-status-emerald-border bg-bg-elevated text-status-emerald-fg shadow-2'
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

// ── T769: graph-backed DAG (orchestration engine) ───────────────────────────
// Renders the plan's REAL engine graph: control nodes (Start/End/Condition) +
// business nodes (bound tasks) + edges tagged by kind (seq/conditional/loopback),
// rather than the client-side depends_on reconstruction. Used when the plan
// carries a graph; PlanDag falls back to the legacy renderer for ungraphed plans.

interface GraphPositioned {
  node: PlanGraphNode;
  level: number;
  x: number;
  y: number;
  w: number; // T800: per-node cell width (control markers are slimmer than cards)
}

// Longest-path left→right layout over the FORWARD edges (loopback back-edges are
// excluded from leveling — they are drawn as return arcs). Control + business
// nodes share the layout so the graph reads as one flow.
// Exported for unit tests (T800 layout algebra: Start/End terminal ranks + slim
// control columns). Not part of the page's public surface otherwise.
export function layoutGraph(
  nodes: PlanGraphNode[],
  edges: PlanGraphEdge[],
): { positioned: GraphPositioned[]; width: number; height: number } {
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const forward = edges.filter((e) => e.kind !== 'loopback' && byId.has(e.from) && byId.has(e.to));
  const incoming = new Map<string, string[]>();
  for (const e of forward) incoming.set(e.to, [...(incoming.get(e.to) ?? []), e.from]);

  const cache = new Map<string, number>();
  const inStack = new Set<string>();
  function level(id: string): number {
    if (cache.has(id)) return cache.get(id)!;
    if (inStack.has(id)) return 0; // cycle guard (defensive)
    inStack.add(id);
    const preds = incoming.get(id) ?? [];
    const lvl = preds.length === 0 ? 0 : Math.max(...preds.map((p) => level(p) + 1));
    inStack.delete(id);
    cache.set(id, lvl);
    return lvl;
  }

  // Raw longest-path level, then FORCE terminal ranks for the structural anchors:
  // Start is the inline SOURCE (leftmost), End the inline SINK (rightmost). Holds
  // even for graphs built before the Start→root / sink→End edges existed (T800) —
  // an edge-less End would otherwise land at level 0, stacked in Start's left column.
  const raw = new Map<string, number>();
  let maxNonEnd = 0;
  for (const n of nodes) {
    const lvl = level(n.id);
    raw.set(n.id, lvl);
    if (n.control_kind !== 'end') maxNonEnd = Math.max(maxNonEnd, lvl);
  }
  const effLevel = (n: PlanGraphNode): number =>
    n.control_kind === 'start' ? 0 : n.control_kind === 'end' ? maxNonEnd + 1 : (raw.get(n.id) ?? 0);

  const byLevel = new Map<number, PlanGraphNode[]>();
  let maxLevel = 0;
  for (const n of nodes) {
    const lvl = effLevel(n);
    maxLevel = Math.max(maxLevel, lvl);
    byLevel.set(lvl, [...(byLevel.get(lvl) ?? []), n]);
  }

  // Top-to-bottom flow: each dependency LEVEL is a horizontal ROW stacked downward
  // (levelY[l]); sibling nodes within a level spread left→right by their own width.
  // A control marker (start / end / condition) keeps its slim width, so a lone
  // condition diamond doesn't reserve a full card's worth of horizontal space.
  const nodeW = (n: PlanGraphNode): number => (n.category === 'control' ? CTRL_W : NODE_W);
  const levelY: number[] = [];
  let accY = PAD_Y;
  for (let l = 0; l <= maxLevel; l++) {
    levelY[l] = accY;
    accY += NODE_H + LEVEL_GAP;
  }

  const positioned: GraphPositioned[] = [];
  let maxRowRight = 0;
  for (const [lvl, group] of byLevel) {
    let x = PAD_X;
    group.forEach((node) => {
      const w = nodeW(node);
      positioned.push({ node, level: lvl, x, y: levelY[lvl], w });
      x += w + COL_GAP;
    });
    maxRowRight = Math.max(maxRowRight, x - COL_GAP); // right edge of the last node in the row
  }
  const width = Math.max(maxRowRight + PAD_X, 200);
  const height = accY - LEVEL_GAP + PAD_Y;
  return { positioned, width, height };
}

// ── T981 follow-up: stage-grouped canvas layout ─────────────────────────────
// Two-level layout: an OUTER stage DAG (stage boxes positioned by
// depends_on_stages, longest-path leveled exactly like layoutGraph levels
// business/control nodes) wrapping an INNER sub-DAG per stage (the stage's own
// member nodes, laid out with the existing layoutGraph so within-stage flow is
// unchanged). A node belongs to a stage when its bound task_id appears in that
// stage's members (§7 read model — PlanStage.members).
//
// The stage's gate (a real graph CONDITION node, §4.2) and the Start/End
// anchors are folded into the SAME `positioned` list the flat layout produces
// — at their computed canvas coordinates — rather than re-deriving stage-to-
// -stage connectivity separately. This matters: buildStages already wires the
// barrier edges directly onto the graph (business → gate, gate → downstream
// entries, T800 Start→root / sink→End), so `graph.edges` is ALREADY the
// complete, authoritative connectivity. Reusing the existing edge-drawing
// pass (posById lookup by node id) means the canvas can never show a
// stage-boundary connection that doesn't correspond to a real graph edge, and
// there is only one edge-kind system (seq/conditional/loopback) at every
// zoom level instead of a second invented "stage edge" style.
//
// Any node that ends up neither a stage member nor Start/End/gate (§8: a plan
// whose graph predates staging for some nodes) is defensively laid out with
// the plain algorithm in a trailing row so nothing is silently dropped.
const STAGE_HEADER_H = 132;
const STAGE_ROW_GAP_X = 40; // gap between sibling stage boxes in the same row
const STAGE_LEVEL_GAP_Y = NODE_H + 60; // gap between rows — fits a gate/anchor cell + edges

export interface StageBox {
  stage: PlanStage;
  x: number;
  y: number;
  w: number;
  h: number;
}
export interface StagedGraphLayout {
  positioned: GraphPositioned[];
  boxes: StageBox[];
  width: number;
  height: number;
}

// Longest-path leveling over a stage DAG's depends_on_stages — the exact same
// algebra as layoutGraph's node leveling, just over stage ids instead of node
// ids (§4.2: "the outer stage DAG").
function levelOfStages(stages: PlanStage[]): Map<string, number> {
  const byId = new Map(stages.map((s) => [s.id, s]));
  const cache = new Map<string, number>();
  const inStack = new Set<string>();
  function level(id: string): number {
    if (cache.has(id)) return cache.get(id)!;
    if (inStack.has(id)) return 0; // cycle guard (defensive; the backend already validates acyclicity)
    inStack.add(id);
    const st = byId.get(id);
    const deps = (st?.depends_on_stages ?? []).filter((d) => byId.has(d));
    const lvl = deps.length === 0 ? 0 : Math.max(...deps.map((d) => level(d) + 1));
    inStack.delete(id);
    cache.set(id, lvl);
    return lvl;
  }
  const out = new Map<string, number>();
  for (const s of stages) out.set(s.id, level(s.id));
  return out;
}

export function stageDisplayMeta(stages: PlanStage[]): {
  byStageId: Map<string, { ref: string; name: string }>;
  byGateNodeId: Map<string, string>;
} {
  const byStageId = new Map<string, { ref: string; name: string }>();
  const byGateNodeId = new Map<string, string>();
  stages.forEach((stage, index) => {
    const ref = `S${index + 1}`;
    const name = stage.name.replace(new RegExp(`^${ref}\\s*[-:·]?\\s*`, 'i'), '').trim() || stage.name;
    byStageId.set(stage.id, { ref, name });
    if (stage.gate_node_id) byGateNodeId.set(stage.gate_node_id, ref);
  });
  return { byStageId, byGateNodeId };
}

export function layoutStagedGraph(
  nodes: PlanGraphNode[],
  edges: PlanGraphEdge[],
  stages: PlanStage[],
): StagedGraphLayout {
  if (stages.length === 0) {
    // No stages on this plan (§8 zero-regression) — degrade to the flat layout,
    // wrapped in the staged shape so callers don't need to branch.
    const flat = layoutGraph(nodes, edges);
    return { positioned: flat.positioned, boxes: [], width: flat.width, height: flat.height };
  }

  const nodeById = new Map(nodes.map((n) => [n.id, n]));
  const stageIdOfTask = new Map<string, string>();
  for (const st of stages) for (const m of st.members) stageIdOfTask.set(m.task_id, st.id);
  const gateNodeIdOfStage = new Map<string, string>();
  for (const st of stages) if (st.gate_node_id) gateNodeIdOfStage.set(st.id, st.gate_node_id);

  const groupOf = new Map<string, string>(); // node.id -> stage.id (business members only)
  for (const n of nodes) {
    if (n.category === 'business' && n.task_id && stageIdOfTask.has(n.task_id)) {
      groupOf.set(n.id, stageIdOfTask.get(n.task_id)!);
    }
  }

  const stageLevel = levelOfStages(stages);
  const rows = new Map<number, PlanStage[]>();
  let maxRow = 0;
  for (const st of stages) {
    const lvl = stageLevel.get(st.id) ?? 0;
    maxRow = Math.max(maxRow, lvl);
    rows.set(lvl, [...(rows.get(lvl) ?? []), st]);
  }

  // Inner sub-DAG per stage: the stage's own members + the edges strictly
  // between them. Cross-stage edges (barrier edges through the gate) are
  // rendered by the component's existing edge pass once every node — members,
  // gate, Start/End — carries a canvas position (below).
  const innerOf = new Map<string, ReturnType<typeof layoutGraph>>();
  for (const st of stages) {
    const memberNodes = nodes.filter((n) => groupOf.get(n.id) === st.id);
    const memberEdges = edges.filter((e) => groupOf.get(e.from) === st.id && groupOf.get(e.to) === st.id);
    innerOf.set(st.id, layoutGraph(memberNodes, memberEdges));
  }

  // Pass 1: row content widths (boxes only, gap-separated) to find the widest
  // row — every row is then centered against that width so the outer DAG reads
  // as one balanced flow instead of a ragged left-aligned stack.
  const rowWidth = (row: PlanStage[]): number => {
    const boxW = row.map((st) => Math.max(innerOf.get(st.id)!.width, NODE_W + 2 * PAD_X));
    return boxW.reduce((a, b) => a + b, 0) + STAGE_ROW_GAP_X * Math.max(0, row.length - 1);
  };
  let canvasWidth = 0;
  for (let l = 0; l <= maxRow; l++) canvasWidth = Math.max(canvasWidth, rowWidth(rows.get(l) ?? []));
  canvasWidth = Math.max(canvasWidth, CTRL_W) + 2 * PAD_X;

  // Pass 2: place boxes row by row, each row centered within canvasWidth;
  // stack rows downward leaving STAGE_LEVEL_GAP_Y for a gate cell + edges.
  const boxes: StageBox[] = [];
  const boxOf = new Map<string, StageBox>();
  const positioned: GraphPositioned[] = [];
  const covered = new Set<string>();
  let y = PAD_Y + NODE_H + STAGE_LEVEL_GAP_Y / 2; // room for the Start anchor above row 0
  for (let l = 0; l <= maxRow; l++) {
    const row = rows.get(l) ?? [];
    const w = rowWidth(row);
    let x = PAD_X + (canvasWidth - 2 * PAD_X - w) / 2;
    let rowH = 0;
    for (const st of row) {
      const inner = innerOf.get(st.id)!;
      const boxW = Math.max(inner.width, NODE_W + 2 * PAD_X);
      const boxH = inner.height + STAGE_HEADER_H;
      const box: StageBox = { stage: st, x, y, w: boxW, h: boxH };
      boxes.push(box);
      boxOf.set(st.id, box);
      rowH = Math.max(rowH, boxH);
      for (const p of inner.positioned) {
        positioned.push({ ...p, x: box.x + p.x, y: box.y + STAGE_HEADER_H + p.y });
        covered.add(p.node.id);
      }
      const gateNodeId = gateNodeIdOfStage.get(st.id);
      const gateNode = gateNodeId ? nodeById.get(gateNodeId) : undefined;
      if (gateNode) {
        positioned.push({
          node: gateNode,
          level: l,
          x: box.x + boxW / 2 - CTRL_W / 2,
          y: box.y + boxH + (STAGE_LEVEL_GAP_Y - NODE_H) / 2,
          w: CTRL_W,
        });
        covered.add(gateNode.id);
      }
      x += boxW + STAGE_ROW_GAP_X;
    }
    y += rowH + STAGE_LEVEL_GAP_Y;
  }
  const bottomY = y - STAGE_LEVEL_GAP_Y / 2;

  // Start/End anchors + any leftover node not covered by a stage (defensive).
  let start: PlanGraphNode | undefined;
  let end: PlanGraphNode | undefined;
  const orphans: PlanGraphNode[] = [];
  for (const n of nodes) {
    if (covered.has(n.id)) continue;
    if (n.control_kind === 'start') { start = n; continue; }
    if (n.control_kind === 'end') { end = n; continue; }
    orphans.push(n);
  }
  if (start) positioned.push({ node: start, level: -1, x: canvasWidth / 2 - CTRL_W / 2, y: PAD_Y, w: CTRL_W });

  let height = bottomY;
  if (orphans.length > 0) {
    const orphanEdges = edges.filter((e) => orphans.some((o) => o.id === e.from) || orphans.some((o) => o.id === e.to));
    const flat = layoutGraph(orphans, orphanEdges);
    for (const p of flat.positioned) positioned.push({ ...p, level: maxRow + 1, x: p.x + PAD_X, y: p.y + height });
    height += flat.height;
  }
  if (end) {
    positioned.push({ node: end, level: maxRow + 2, x: canvasWidth / 2 - CTRL_W / 2, y: height, w: CTRL_W });
    height += NODE_H + PAD_Y;
  } else {
    height += PAD_Y;
  }

  return { positioned, boxes, width: canvasWidth, height };
}

export function layoutLegacyStagedDag(nodes: PlanNode[], stages: PlanStage[]): ReturnType<typeof layoutDag> & { boxes: StageBox[] } {
  if (stages.length === 0) {
    return { ...layoutDag(nodes), boxes: [] };
  }

  const graphNodes: PlanGraphNode[] = nodes.map((node) => ({
    id: node.task_id,
    category: 'business',
    title: node.title,
    status: 'open',
    task_id: node.task_id,
    task_status: node.task_status,
    org_ref: node.org_ref,
    assignee_ref: node.assignee_ref,
  }));
  const graphEdges: PlanGraphEdge[] = [];
  const nodeIds = new Set(nodes.map((node) => node.task_id));
  const dependedOn = new Set<string>();
  for (const node of nodes) {
    for (const dep of node.depends_on) {
      if (!nodeIds.has(dep)) continue;
      graphEdges.push({ from: dep, to: node.task_id, kind: 'seq' });
      dependedOn.add(dep);
    }
  }

  const startId = '__legacy_stage_start__';
  const endId = '__legacy_stage_end__';
  graphNodes.push(
    { id: startId, category: 'control', control_kind: 'start', title: 'Start', status: 'open' },
    { id: endId, category: 'control', control_kind: 'end', title: 'End', status: 'open' },
  );
  const roots = nodes.filter((node) => !node.depends_on.some((dep) => nodeIds.has(dep)));
  const leaves = nodes.filter((node) => !dependedOn.has(node.task_id));
  for (const root of roots) graphEdges.push({ from: startId, to: root.task_id, kind: 'seq' });
  for (const leaf of leaves) graphEdges.push({ from: leaf.task_id, to: endId, kind: 'seq' });

  const staged = layoutStagedGraph(graphNodes, graphEdges, stages);
  const planNodeById = new Map(nodes.map((node) => [node.task_id, node]));
  const positioned: Positioned[] = staged.positioned.flatMap((entry) => {
    const node = planNodeById.get(entry.node.id);
    return node ? [{ node, level: entry.level, x: entry.x, y: entry.y }] : [];
  });
  const positionById = new Map(staged.positioned.map((entry) => [entry.node.id, entry]));
  const startPosition = positionById.get(startId);
  const endPosition = positionById.get(endId);
  const planPositionById = new Map(positioned.map((entry) => [entry.node.task_id, entry]));
  const start = startPosition
    ? {
        cx: startPosition.x + startPosition.w / 2,
        cy: startPosition.y + NODE_H / 2,
        links: roots.flatMap((node) => {
          const entry = planPositionById.get(node.task_id);
          return entry ? [{ taskId: node.task_id, x: entry.x + NODE_W / 2, y: entry.y }] : [];
        }),
      }
    : null;
  const end = endPosition
    ? {
        cx: endPosition.x + endPosition.w / 2,
        cy: endPosition.y + NODE_H / 2,
        links: leaves.flatMap((node) => {
          const entry = planPositionById.get(node.task_id);
          return entry ? [{ taskId: node.task_id, x: entry.x + NODE_W / 2, y: entry.y + NODE_H }] : [];
        }),
      }
    : null;

  return { positioned, boxes: staged.boxes, width: staged.width, height: staged.height, start, end };
}

// Per-kind edge stroke class + dash. seq = neutral, conditional = accent (routed
// by a decision), loopback = amber dashed return arc.
const EDGE_KIND_STROKE: Record<PlanGraphEdgeKind, { cls: string; dash?: string; marker: string }> = {
  seq: { cls: 'stroke-border-strong', marker: 'url(#plan-graph-arrow)' },
  conditional: { cls: 'stroke-accent', marker: 'url(#plan-graph-arrow-accent)' },
  loopback: { cls: 'stroke-status-amber-border', dash: '5 3', marker: 'url(#plan-graph-arrow-loop)' },
};

// A control node marker: Start/End circular terminals; Condition a rotated
// (diamond) square. Distinct from task cards so the control flow is legible.
// Mirrors the mockup's `.terminal.start/.end` (filled accent disc vs. a
// done-toned ring) and `.gate` diamond (two-line label: the gate id + its name).
function ControlNodeMarker({ node, gateStageRef }: { node: PlanGraphNode; gateStageRef?: string }): React.ReactElement {
  const { t } = useTranslation('work');
  const kind = node.control_kind;
  const isCondition = kind === 'condition';
  const startEndLabel = kind === 'start' ? t('plan.detail.dag.anchorStart') : t('plan.detail.dag.anchorEnd');
  // Gate label: mockup shows "GATE:S2" (mono, gate-toned) over the gate's own
  // short name — reuse node.title (the gate's own node label) for the second
  // line, falling back to a generic "Condition" if it's unset.
  const gateName = node.title || t('plan.detail.dag.controlCondition', { defaultValue: 'Condition' });
  return (
    <div
      className="flex h-full w-full items-center justify-center"
      data-testid="plan-graph-control-node"
      data-control-kind={kind}
      data-node-status={node.status}
    >
      {isCondition ? (
        <div
          className="flex h-16 w-16 rotate-45 items-center justify-center rounded-md border-[1.5px] border-status-amber-border bg-bg-elevated shadow-1"
          title={gateName}
        >
          <div className="-rotate-45 px-1 text-center leading-tight">
            <div className="font-mono text-[0.5625rem] font-bold uppercase tracking-wide text-status-amber-fg">
              {t('plan.detail.dag.gateLabel', { defaultValue: 'GATE' })}{gateStageRef ? `:${gateStageRef}` : ''}
            </div>
            <div className="mt-0.5 truncate text-[0.5rem] font-semibold text-text-secondary">{gateName}</div>
          </div>
        </div>
      ) : (
        <div
          className={`flex h-14 w-14 items-center justify-center rounded-full text-center text-[0.625rem] font-extrabold uppercase tracking-wide ${
            kind === 'start'
              ? 'border-[1.5px] border-accent bg-accent text-white shadow-[0_4px_16px_-4px_var(--color-accent)]'
              : 'border-2 border-status-emerald-border bg-bg-elevated text-status-emerald-fg shadow-1'
          }`}
          title={startEndLabel}
        >
          {startEndLabel}
        </div>
      )}
    </div>
  );
}

function PlanGraphDag({
  projectId,
  plan,
  graph,
  compact,
}: {
  projectId: string;
  plan: Plan;
  graph: { nodes: PlanGraphNode[]; edges: PlanGraphEdge[] };
  compact: boolean;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const scale = compact ? 0.7 : 1;
  const nodes = graph.nodes;
  const edges = graph.edges;

  // §7: group the canvas by Plan Stage when the plan has any (T981 follow-up —
  // the outer Stage DAG wraps each stage's own inner sub-DAG). Empty for a
  // no-stage plan, so layoutStagedGraph degrades to the identical flat layout.
  const stagesQuery = usePlanStages(projectId, plan.id);
  const stages = stagesQuery.data ?? [];
  // Stage ids are opaque persistence keys. The mockup uses compact, plan-local
  // S1/S2 refs, which can be derived from the API's stable stage order without
  // changing the read model. Strip the same prefix from legacy stage names so
  // "S1 Data" renders as "STAGE · S1  Data", not "S1  S1 Data".
  const stageDisplay = useMemo(() => stageDisplayMeta(stages), [stages]);

  // Bound task → derived 6-state node_status (for the business-node chip), taken
  // from the plan detail's PlanNode list so the graph chips match the plan view.
  const nodeStatusOf = useMemo(() => {
    const m = new Map<string, PlanNodeStatus>();
    for (const pn of plan.nodes ?? []) m.set(pn.task_id, pn.node_status);
    return m;
  }, [plan.nodes]);

  const { positioned, boxes, width, height } = useMemo(
    () => layoutStagedGraph(nodes, edges, stages),
    [nodes, edges, stages],
  );
  const posById = useMemo(() => new Map(positioned.map((p) => [p.node.id, p])), [positioned]);

  // Edge paths (top-to-bottom flow). Forward edges: source BOTTOM-mid → target
  // TOP-mid vertical cubic. Loopback: a return arc around the LEFT side (from the
  // decision back UP to its upstream target). Arrow markers auto-orient to the path.
  const drawnEdges = useMemo(() => {
    const out: { key: string; d: string; kind: PlanGraphEdgeKind }[] = [];
    for (const e of edges) {
      const a = posById.get(e.from);
      const b = posById.get(e.to);
      if (!a || !b) continue;
      if (e.kind === 'loopback') {
        const x1 = a.x;
        const y1 = a.y + NODE_H / 2;
        const x2 = b.x;
        const y2 = b.y + NODE_H / 2;
        const leftX = Math.min(x1, x2) - 34;
        out.push({ key: `loop-${e.from}->${e.to}`, kind: e.kind, d: `M${x1},${y1} C${leftX},${y1} ${leftX},${y2} ${x2},${y2}` });
        continue;
      }
      const x1 = a.x + a.w / 2;
      const y1 = a.y + NODE_H;
      const x2 = b.x + b.w / 2;
      const y2 = b.y;
      const midY = (y1 + y2) / 2;
      out.push({ key: `${e.from}->${e.to}`, kind: e.kind, d: `M${x1},${y1} C${x1},${midY} ${x2},${midY} ${x2},${y2}` });
    }
    return out;
  }, [edges, posById]);

  return (
    <SenderSidebarProvider>
      <div data-testid="plan-dag" data-graph="true" className="md:flex md:min-h-0 md:flex-1 md:flex-col">
        {/* Mobile: a simple ordered list of nodes by flow level. */}
        <ol className="mt-1 space-y-1.5 md:hidden" data-testid="plan-graph-stepper">
          {positioned
            .slice()
            .sort((p, q) => p.level - q.level || p.y - q.y)
            .map((p) => (
              <li
                key={p.node.id}
                className="rounded-lg border border-border-base bg-bg-elevated p-2 text-xs"
                data-node-category={p.node.category}
                data-control-kind={p.node.control_kind}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold text-text-primary">
                    {p.node.title || refLabel(p.node.org_ref, p.node.task_id ?? p.node.id)}
                  </span>
                  {p.node.category === 'business' && p.node.task_id ? (
                    <NodeStateChip status={nodeStatusOf.get(p.node.task_id) ?? 'blocked'} />
                  ) : (
                    <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary">
                      {p.node.control_kind}
                    </span>
                  )}
                </div>
              </li>
            ))}
        </ol>

        {/* Desktop canvas — centered by default + grab-to-pan (DagCanvas). */}
        <DagCanvas
          contentW={width * scale}
          contentH={height * scale}
          compact={compact}
          testId="plan-dag-canvas"
          legend={
            <div className="contents" data-testid="plan-graph-legend">
              <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-muted"><span className="h-0.5 w-4 bg-border-strong" />{t('plan.detail.dag.edgeSeq', { defaultValue: 'seq' })}</span>
              <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-muted"><span className="h-0.5 w-4 bg-accent" />{t('plan.detail.dag.edgeConditional', { defaultValue: 'conditional' })}</span>
              <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-muted"><span className="h-0.5 w-4 border-t-2 border-dashed border-status-amber-border" />{t('plan.detail.dag.edgeLoopback', { defaultValue: 'loopback' })}</span>
            </div>
          }
        >
            <div
              className="relative"
              data-testid="plan-dag-scaler"
              style={{ width, height, transform: scale === 1 ? undefined : `scale(${scale})`, transformOrigin: 'top left' }}
            >
              {/* Stage boxes render FIRST (bottom layer) — the sub-DAG's own
                  svg/cards paint over them in normal DOM-order stacking, and
                  they carry no z-index so they never cover the edges/cards
                  drawn after them. One box per Plan Stage (§7); a no-stage
                  plan gets none, so the canvas is byte-identical to before. */}
              {boxes.map((b) => {
                const totalMembers = b.stage.members.length;
                const doneMembers = b.stage.members.filter((m) => stageMemberDone(m.task_status)).length;
                const pct = totalMembers > 0 ? Math.round((doneMembers / totalMembers) * 100) : 0;
                const display = stageDisplay.byStageId.get(b.stage.id) ?? { ref: b.stage.id, name: b.stage.name };
                return (
                <div
                  key={b.stage.id}
                  className="absolute rounded-xl border border-border-strong bg-bg-surface"
                  style={{ left: b.x, top: b.y, width: b.w, height: b.h }}
                  data-testid={`plan-stage-box-${b.stage.id}`}
                >
                  <div className="border-b border-border-base px-3.5 py-2">
                    <div className="flex items-baseline gap-1.5">
                      <span className="font-mono text-[0.625rem] tracking-wide text-text-muted" data-testid={`plan-stage-ref-${b.stage.id}`}>{t('plan.detail.stages.idLabel', { defaultValue: 'STAGE' })} · {display.ref}</span>
                      <span className="truncate text-xs font-semibold text-text-primary" data-testid={`plan-stage-name-${b.stage.id}`}>{display.name}</span>
                    </div>
                    <div className="mt-1 flex items-center gap-2.5">
                      <span
                        className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.5625rem] font-bold uppercase tracking-wide ${STAGE_STATUS_CLASS[b.stage.status]}`}
                        data-testid={`plan-stage-status-${b.stage.id}`}
                      >
                        <span className="h-1 w-1 rounded-full bg-current" aria-hidden="true" />
                        {t(`plan.detail.stages.status.${b.stage.status}`)}
                      </span>
                      <span className="h-1 max-w-[7rem] flex-1 overflow-hidden rounded-full bg-bg-subtle" aria-hidden="true">
                        <span className="block h-full rounded-full bg-success" style={{ width: `${pct}%` }} />
                      </span>
                      <span className="font-mono text-[0.5625rem] text-text-muted" data-testid={`plan-stage-progress-${b.stage.id}`}>
                        {doneMembers}/{totalMembers}
                      </span>
                      {b.stage.rounds > 0 && (
                        <span
                          className="inline-flex items-center rounded bg-warning/10 px-1.5 py-0.5 text-[0.625rem] text-warning"
                          data-testid={`plan-stage-rounds-${b.stage.id}`}
                        >
                          {t('plan.detail.stages.retryRound', { round: b.stage.rounds, max: b.stage.max_rounds })}
                        </span>
                      )}
                    </div>
                    <StageGateAudit stage={b.stage} />
                  </div>
                </div>
                );
              })}

              <svg className="absolute left-0 top-0" width={width} height={height} data-testid="plan-graph-svg" aria-hidden="true">
                <defs>
                  <marker id="plan-graph-arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                    <path d="M0,0 L10,5 L0,10 z" className="fill-border-strong" />
                  </marker>
                  <marker id="plan-graph-arrow-accent" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                    <path d="M0,0 L10,5 L0,10 z" className="fill-accent" />
                  </marker>
                  <marker id="plan-graph-arrow-loop" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
                    <path d="M0,0 L10,5 L0,10 z" className="fill-status-amber-border" />
                  </marker>
                </defs>
                {drawnEdges.map((e) => {
                  const st = EDGE_KIND_STROKE[e.kind];
                  return (
                    <path
                      key={e.key}
                      d={e.d}
                      fill="none"
                      className={st.cls}
                      strokeWidth="1.6"
                      strokeDasharray={st.dash}
                      markerEnd={st.marker}
                      data-testid="plan-graph-edge"
                      data-edge={e.key}
                      data-edge-kind={e.kind}
                    />
                  );
                })}
              </svg>

              {positioned.map((p) => {
                if (p.node.category === 'control') {
                  return (
                    <div key={p.node.id} className="absolute" style={{ left: p.x, top: p.y, width: p.w, height: NODE_H }}>
                      <ControlNodeMarker node={p.node} gateStageRef={stageDisplay.byGateNodeId.get(p.node.id)} />
                    </div>
                  );
                }
                const taskId = p.node.task_id ?? p.node.id;
                const status = nodeStatusOf.get(taskId) ?? 'blocked';
                const s = NODE_STATE[status] ?? NODE_STATE.blocked;
                const accentCls = s.border.replace(/^border-/, 'bg-');
                return (
                  <div
                    key={p.node.id}
                    className={`absolute overflow-hidden rounded-lg border-[1.5px] bg-bg-elevated p-2 pl-3 shadow-1 transition duration-150 motion-safe:hover:-translate-y-0.5 hover:shadow-2 ${s.border} ${nodeVisualCls(status)}`}
                    style={{ left: p.x, top: p.y, width: NODE_W }}
                    data-testid="plan-graph-node"
                    data-task-id={taskId}
                    data-node-id={p.node.id}
                    data-level={p.level}
                  >
                    <span className={`absolute inset-y-0 left-0 w-1.5 ${accentCls}`} aria-hidden="true" />
                    <div className="mb-1 flex items-center justify-between gap-1">
                      <TaskIdTag taskId={taskId} orgRef={p.node.org_ref} testId="plan-graph-node-taskid" />
                      <NodeStateChip status={status} />
                    </div>
                    <div className="mb-1.5 text-xs font-semibold text-text-primary" title={p.node.title}>
                      <TaskTitleLink projectId={projectId} taskId={taskId} title={p.node.title || refLabel(p.node.org_ref, taskId)} wrap />
                    </div>
                    <div className="flex min-w-0 text-[0.6875rem]">
                      <AssigneeTag assigneeRef={p.node.assignee_ref ?? ''} />
                    </div>
                  </div>
                );
              })}
            </div>
        </DagCanvas>
      </div>
    </SenderSidebarProvider>
  );
}

// T769: when the plan carries a real orchestration graph (built by T768 on
// start), render THAT graph — control nodes (Start/End/Condition) + edges by
// kind — so the DAG reflects the engine. A plan with NO graph (draft /
// never-started / engine unwired) returns has_graph:false and falls through to
// the legacy depends_on renderer (NON-BREAKING, zero regression).
//
// --- Stage-level view (T981, plan-stage-model §7) ---------------------------

// STAGE_STATUS_CLASS maps a projected stage status to its chip colour (mirrors the
// node/plan chip palette). done=success, running=accent, reopen=warning, open=muted.
const STAGE_STATUS_CLASS: Record<PlanStage['status'], string> = {
  done: 'bg-success/15 text-success',
  running: 'bg-accent/15 text-accent',
  reopen: 'bg-warning/15 text-warning',
  open: 'bg-bg-subtle text-text-muted',
};

function StageGateAudit({ stage }: { stage: PlanStage }) {
  const spec = stage.gate_spec;
  if (!spec) return null;
  const owner = spec.assignee_ref || spec.role_ref || 'unassigned';
  return (
    <div className="mt-2 grid gap-1 border-t border-border-base pt-1.5 text-[0.625rem] leading-4 text-text-secondary" data-testid={`plan-stage-gate-audit-${stage.id}`}>
      <div className="flex min-w-0 flex-wrap gap-x-3">
        <span data-testid={`plan-stage-gate-evaluator-${stage.id}`}>{spec.evaluator_kind} · {owner}</span>
        <span data-testid={`plan-stage-gate-routes-${stage.id}`}>{spec.pass_route} / {spec.reject_route} / {spec.exhausted_route}</span>
      </div>
      <div className="line-clamp-2 break-words text-text-primary" title={spec.acceptance_contract} data-testid={`plan-stage-gate-contract-${stage.id}`}>
        {spec.acceptance_contract || 'Missing acceptance contract'}
      </div>
      {(stage.gate_outcome || stage.gate_evidence || stage.gate_reviewed_sha) && (
        <div className="flex min-w-0 flex-wrap gap-x-3" data-testid={`plan-stage-gate-evidence-${stage.id}`}>
          {stage.gate_outcome && <span className="font-semibold uppercase">{stage.gate_outcome}</span>}
          {stage.gate_evidence && <span className="max-w-[18rem] truncate" title={stage.gate_evidence}>{stage.gate_evidence}</span>}
          {stage.gate_reviewed_sha && <span className="font-mono">{stage.gate_reviewed_sha.slice(0, 12)}</span>}
        </div>
      )}
      {!!stage.diagnostics?.length && (
        <div className="truncate text-danger" title={stage.diagnostics.map((d) => `${d.code}: ${d.message}`).join('\n')} data-testid={`plan-stage-gate-diagnostics-${stage.id}`}>
          {stage.diagnostics.map((d) => d.code).join(', ')}
        </div>
      )}
    </div>
  );
}

// stageMemberDone mirrors the backend taskToStageMemberState "done" bucket (§4.1):
// a member counts as done once its task is terminal (completed or discarded).
function stageMemberDone(status: PlanStage['members'][number]['task_status']): boolean {
  return status === 'completed' || status === 'discarded';
}

// v2.30.1 fix-before-ship (React #300): PlanDag is a THIN WRAPPER — it runs the
// single graph query, then renders EITHER <PlanGraphDag/> OR <LegacyPlanDag/> as
// a sibling. The previous shape early-returned <PlanGraphDag/> from BETWEEN the
// legacy hooks (usePlanGraph → conditional return → ~13 more hooks below), so the
// first frame (query loading, has_graph undefined) ran ALL those hooks, and once
// the query resolved has_graph:true the early return fired and the rendered hook
// count dropped → React error #300 ("rendered fewer hooks than expected"),
// crashing every started (has_graph:true) plan's DAG tab regardless of graph
// shape. Splitting the two renderers into siblings gives each a STABLE,
// unconditional hook list, so no loading→resolved transition changes a
// component's hook count.
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
  const graphQuery = usePlanGraph(projectId, plan.id);
  const g = graphQuery.data;
  if (g?.has_graph && (g.nodes?.length ?? 0) > 0) {
    return <PlanGraphDag projectId={projectId} plan={plan} graph={{ nodes: g.nodes ?? [], edges: g.edges ?? [] }} compact={compact} />;
  }
  return <LegacyPlanDag projectId={projectId} plan={plan} compact={compact} />;
}

// LegacyPlanDag renders the depends_on graph for plans WITHOUT an orchestration
// graph (has_graph:false). Split out of PlanDag so ALL of its hooks run
// unconditionally — there is no graph early-return sitting between them (that
// interleaving was the React #300 root cause).
function LegacyPlanDag({
  projectId,
  plan,
  compact,
}: {
  projectId: string;
  plan: Plan;
  compact: boolean;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const nodes = plan.nodes ?? [];
  const stagesQuery = usePlanStages(projectId, plan.id);
  const stages = stagesQuery.data ?? [];
  const stageDisplay = useMemo(() => stageDisplayMeta(stages), [stages]);
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

  const { positioned, boxes, width, height, start, end } = useMemo(
    () => layoutLegacyStagedDag(nodes, stages),
    [nodes, stages],
  );
  const posById = useMemo(
    () => new Map(positioned.map((p) => [p.node.task_id, p])),
    [positioned],
  );

  // Edges: dep (upstream) → node (downstream). Top-to-bottom flow: path from dep
  // BOTTOM-mid to node TOP-mid; a vertical-ease cubic for a clean orthogonal-ish curve.
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
        const x1 = dep.x + NODE_W / 2;
        const y1 = dep.y + NODE_H;
        const x2 = p.x + NODE_W / 2;
        const y2 = p.y;
        const midY = (y1 + y2) / 2;
        out.push({
          key: `${depId}->${p.node.task_id}`,
          d: `M${x1},${y1} C${x1},${midY} ${x2},${midY} ${x2},${y2}`,
          from: p.node.task_id,
          to: depId,
          mx: (x1 + x2) / 2,
          my: midY,
        });
      }
    }
    return out;
  }, [positioned, posById]);

  // v2.9 A5: synthetic flow edges — Start anchor (above) → each root's top-mid, and
  // each leaf's bottom-mid → End anchor (below). Same cubic shape as real edges; kept
  // on a SEPARATE testid (`plan-dag-synthetic-edge`) so real-edge assertions/counts
  // are unaffected. A dashed/lighter stroke reads as a flow anchor, not a dep.
  const synthEdges = useMemo(() => {
    const out: { key: string; d: string }[] = [];
    if (start) {
      for (const l of start.links) {
        const midY = (start.cy + l.y) / 2;
        out.push({
          key: `start->${l.taskId}`,
          d: `M${start.cx},${start.cy} C${start.cx},${midY} ${l.x},${midY} ${l.x},${l.y}`,
        });
      }
    }
    if (end) {
      for (const l of end.links) {
        const midY = (l.y + end.cy) / 2;
        out.push({
          key: `${l.taskId}->end`,
          d: `M${l.x},${l.y} C${l.x},${midY} ${end.cx},${midY} ${end.cx},${end.cy}`,
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
          {t('plan.detail.dag.empty')}
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
                <Trans
                  t={t}
                  i18nKey="plan.detail.dag.connectBanner"
                  values={{ title: titleOf(connectFrom) }}
                  components={{ b: <span className="font-semibold text-text-primary" /> }}
                />
              </span>
              <button
                type="button"
                data-testid="plan-connect-cancel"
                onClick={exitConnect}
                aria-label={t('plan.detail.dag.cancelAddDependency')}
                className="ml-auto shrink-0 rounded border border-border-strong bg-bg-subtle px-2 py-0.5 font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                {t('plan.detail.cancel')}
              </button>
            </div>
          </div>
        )}
        {/* Desktop canvas — centered by default + grab-to-pan (DagCanvas). T347: the
            dot-grid gives a "canvas" feel; T579: flex-1 + min-h-0 fills the pane and
            scrolls internally when the graph overflows. */}
        <DagCanvas
          contentW={width * scale}
          contentH={height * scale}
          compact={compact}
          testId="plan-dag-canvas"
          legend={
            <div className="contents" data-testid="plan-dag-legend">
              {NODE_STATE_ORDER.map((st) => (
                <NodeStateChip key={st} status={st} />
              ))}
            </div>
          }
        >
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
            {boxes.map((box) => {
              const totalMembers = box.stage.members.length;
              const doneMembers = box.stage.members.filter((member) => stageMemberDone(member.task_status)).length;
              const pct = totalMembers > 0 ? Math.round((doneMembers / totalMembers) * 100) : 0;
              const display = stageDisplay.byStageId.get(box.stage.id) ?? { ref: box.stage.id, name: box.stage.name };
              return (
                <div
                  key={box.stage.id}
                  className="absolute rounded-xl border border-border-strong bg-bg-surface"
                  style={{ left: box.x, top: box.y, width: box.w, height: box.h }}
                  data-testid={`plan-stage-box-${box.stage.id}`}
                >
                  <div className="border-b border-border-base px-3.5 py-2">
                    <div className="flex items-baseline gap-1.5">
                      <span className="font-mono text-[0.625rem] tracking-wide text-text-muted" data-testid={`plan-stage-ref-${box.stage.id}`}>
                        {t('plan.detail.stages.idLabel', { defaultValue: 'STAGE' })} · {display.ref}
                      </span>
                      <span className="truncate text-xs font-semibold text-text-primary" data-testid={`plan-stage-name-${box.stage.id}`}>
                        {display.name}
                      </span>
                    </div>
                    <div className="mt-1 flex items-center gap-2.5">
                      <span data-testid={`plan-stage-status-${box.stage.id}`} className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.5625rem] font-bold uppercase tracking-wide ${STAGE_STATUS_CLASS[box.stage.status]}`}>
                        <span className="h-1 w-1 rounded-full bg-current" aria-hidden="true" />
                        {t(`plan.detail.stages.status.${box.stage.status}`)}
                      </span>
                      <span className="h-1 max-w-[7rem] flex-1 overflow-hidden rounded-full bg-bg-subtle" aria-hidden="true">
                        <span className="block h-full rounded-full bg-success" style={{ width: `${pct}%` }} />
                      </span>
                      <span className="font-mono text-[0.5625rem] text-text-muted" data-testid={`plan-stage-progress-${box.stage.id}`}>{doneMembers}/{totalMembers}</span>
                      {box.stage.rounds > 0 && (
                        <span className="inline-flex items-center rounded bg-warning/10 px-1.5 py-0.5 text-[0.625rem] text-warning" data-testid={`plan-stage-rounds-${box.stage.id}`}>
                          {t('plan.detail.stages.retryRound', { round: box.stage.rounds, max: box.stage.max_rounds })}
                        </span>
                      )}
                    </div>
                    <StageGateAudit stage={box.stage} />
                  </div>
                </div>
              );
            })}
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
                  aria-label={t('plan.detail.dag.removeDependency', { from: titleOf(e.from), to: titleOf(e.to) })}
                  title={t('plan.detail.dag.removeDependency', { from: titleOf(e.from), to: titleOf(e.to) })}
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
                        : `${s.border} ${nodeVisualCls(p.node.node_status)}`
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
                          aria-label={t('plan.detail.dag.addDependencyAria', { title: titleOf(taskId) })}
                          title={t('plan.detail.dag.addDependencyTitle', { title: titleOf(taskId) })}
                          className="shrink-0 rounded border border-border-strong bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                        >
                          {t('plan.detail.dag.addDep')}
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
                      aria-label={t('plan.detail.dag.makeDependOn', { from: titleOf(connectFrom!), to: titleOf(taskId) })}
                      title={t('plan.detail.dag.makeDependOn', { from: titleOf(connectFrom!), to: titleOf(taskId) })}
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
        </DagCanvas>
        </>
      )}

      {/* node_status is DERIVED (§9.2) and shown, not edited. In DRAFT the
          dependency STRUCTURE is editable IN-GRAPH (point 3): each node has a
          "+ Dep" connect control, and each edge has an "×" delete control. Once
          running/done the graph is DISPLAY-ONLY (backend rejects with
          ErrPlanNotDraft). */}
      <p className="mt-2 text-[0.6875rem] text-text-muted" data-testid="plan-dag-note">
        {t('plan.detail.dag.noteDerived')}{' '}
        {isDraft ? t('plan.detail.dag.noteDraft') : t('plan.detail.dag.noteDisplayOnly')}
      </p>

      {/* #218 friendly add/remove error (never the raw API message). The
          single in-graph entry point (§21) — no separate editor box. */}
      {isDraft && mutationError && (
        <p className="mt-2 text-xs font-medium text-danger" role="alert" data-testid="plan-edge-error">
          {friendlyDependencyError(mutationError, t)}
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
function friendlyDependencyError(error: unknown, t: TFunction): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('itself') || lower.includes('self')) {
    return t('plan.detail.depError.self');
  }
  if (lower.includes('cycle')) {
    return t('plan.detail.depError.cycle');
  }
  if (lower.includes('draft')) {
    return t('plan.detail.depError.draft');
  }
  return t('plan.detail.depError.generic');
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
  const { t } = useTranslation('work');
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
          {t('plan.detail.taskList.empty')}
        </p>
      ) : (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              data-testid="plan-task-search"
              aria-label={t('plan.detail.taskList.filterAria')}
              placeholder={t('plan.detail.taskList.filterPlaceholder')}
              className="min-w-[14rem] flex-1 rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
            <span className="text-[0.6875rem] text-text-muted" data-testid="plan-task-search-count">
              {t('plan.detail.taskList.showing', { shown: filtered.length, total: nodes.length })}
            </span>
          </div>
          {filtered.length === 0 ? (
            <p className="py-8 text-center text-xs text-text-muted" data-testid="plan-task-search-empty">
              {t('plan.detail.taskList.noMatch')}
            </p>
          ) : (
            // No inner max-height: the table flows to its full height and the
            // enclosing tab panel (min-h-0 flex-1 overflow-auto) owns vertical
            // scroll, so the WHOLE list is reachable and fills the pane instead of a
            // fixed ~28rem box clipping it with dead space below (same fix as the
            // T579 DAG canvas — @oopslink). overflow-x-auto keeps the wide table
            // horizontally scrollable on narrow screens.
            <div className="overflow-x-auto">
              <table className="w-full text-left text-xs" data-testid="plan-task-list-table">
                <thead>
                  <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colTask')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colTitle')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colAssignee')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colTaskStatus')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colNodeStatus')}</th>
                    <th className="py-1.5 font-medium">{t('plan.detail.taskList.colCreated')}</th>
                    {canRemove && <th className="py-1.5 pl-3 text-right font-medium">{t('plan.detail.taskList.colAction')}</th>}
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
function resumeNodeErrorMessage(error: unknown, t: TFunction): string {
  const code = error instanceof ApiError ? error.code : '';
  switch (code) {
    case 'agent_busy':
      // The agent paused this item to work another; it can't be double-activated.
      return t('plan.detail.resumeError.agentBusy');
    case 'node_not_paused':
      return t('plan.detail.resumeError.nodeNotPaused');
    case 'plan_not_running':
      return t('plan.detail.resumeError.planNotRunning');
    default:
      return t('plan.detail.resumeError.generic');
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
  const { t } = useTranslation('work');
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
    const opts: EntityOption[] = [{ value: '', label: t('plan.detail.taskList.unassigned') }];
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
  }, [members, t]);
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
            {t('plan.detail.taskList.removeError')}
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
            ariaLabel={t('plan.detail.taskList.reassign', { title })}
            disabled={assign.isPending || unassign.isPending}
            placeholder={t('plan.detail.taskList.unassigned')}
            searchPlaceholder={t('plan.detail.taskList.searchMembers')}
          />
        </div>
        {assignError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-task-assign-error-${node.task_id}`}
          >
            {t('plan.detail.taskList.assignError')}
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
              aria-label={t('plan.detail.taskList.resumeAria', { title })}
              title={t('plan.detail.taskList.resumeTitle')}
              data-testid={`plan-node-resume-${node.task_id}`}
              onClick={() => resume.mutate(node.task_id)}
            >
              {resume.isPending ? t('plan.detail.taskList.resuming') : t('plan.detail.taskList.resume')}
            </button>
          )}
        </span>
        {resume.isError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-node-resume-error-${node.task_id}`}
          >
            {resumeNodeErrorMessage(resume.error, t)}
          </span>
        )}
      </td>
      {/* Created column (owner ask): the underlying task's creation time as a
          full local timestamp WITH timezone; raw ISO on hover. "—" if absent. */}
      <td className="py-1.5 tabular-nums text-text-muted" data-testid="plan-row-created" title={node.created_at ?? ''}>
        {node.created_at ? fullDateTime(node.created_at) : '—'}
      </td>
      {canRemove && (
        <td className="py-1.5 pl-3 text-right">
          <span className="inline-flex items-center justify-end gap-1.5">
            {confirmArmed && (
              <span
                className="text-[0.6875rem] font-semibold text-danger"
                aria-live="polite"
              >
                {t('plan.detail.taskList.confirm')}
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
              aria-label={t('plan.detail.taskList.removeAria', { title: node.title || refLabel(node.org_ref, node.task_id) })}
              title={confirmArmed ? t('plan.detail.taskList.removeConfirmTitle') : t('plan.detail.taskList.removeTitle')}
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
  const { t } = useTranslation('work');
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
              aria-label={t('plan.detail.chat.restore')}
              title={t('plan.detail.chat.restoreEsc')}
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
            {t('plan.detail.chat.initializing')}
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
                  {t('plan.detail.chat.refreshError')}
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
