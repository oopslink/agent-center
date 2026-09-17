import { withHistoricalEdges } from './planHistoricalEdges';
import React, { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useTranslation, Trans } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useNavigate, useParams } from 'react-router-dom';
import {
  Background,
  Controls,
  EdgeLabelRenderer,
  Handle,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  ReactFlowProvider,
  getSmoothStepPath,
  type EdgeProps,
  type NodeProps,
  useReactFlow,
  getNodesBounds,
  getViewportForBounds,
  useStore,
} from '@xyflow/react';
import { OrgLink, orgPath, useOptionalOrgContext } from '@/OrgContext';
import { useProject, useProjectMembers } from '@/api/projects';
import { ApiError } from '@/api/client';
import {
  usePlan,
  usePlanGraph,
  usePlanGenerations,
  usePlanStages,
  useStartPlan,
  usePausePlan,
  useResumePlan,
  useReopenPlan,
  useDiscardPlan,
  useAddDependency,
  useRemoveDependency,
  useRemoveTaskFromPlan,
  useResumePausedNode,
  usePatchPlan,
  useDeletePlan,
  useArchivePlan,
  useCommitPlanEvolution,
  friendlyDestructivePlanError,
  type Plan,
  type PlanNode,
  type PlanNodeStatus,
  type PlanGraphNode,
  type PlanGraphNodeStatus,
  type PlanGraphEdge,
  type PlanGraphEdgeKind,
  type PatchPlanInput,
  type PlanStage,
  type PlanContinuation,
  type PlanProgressControl,
  type GateVerdict,
  type PlanGenerationRead,
  type PlanGeneration,
  type PlanGenerationDiff,
  type CommitPlanEvolutionInput,
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
import { formatLocalTime, formatTimeRangeDuration } from '@/utils/time';
import { Skeleton } from '@/components/Skeleton';
import { ObjectAuditTimeline } from '@/components/ObjectAuditTimeline';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { Avatar } from '@/components/Avatar';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';
import { StatusChip, refLabel, fullDateTime } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanArchivedBadge, PlanFailedIndicator, AutoAdvancingIndicator, TaskArchivedBadge, planProgressLabel, PlanRefTag } from '@/components/planDisplay';
import { ConversationView } from '@/components/ConversationView';
import { ConversationSidebar, EmbeddedConversationSidebar, EmbeddedSidebarToggle } from '@/components/ConversationSidebar';
import { ContextPanel, useContextPanelMobileTrigger } from '@/shell/contextPanel';
import { ContextPanelMobileButton } from '@/components/ContextPanelMobileButton';
import { SenderSidebarProvider, useSenderSidebar } from '@/components/SenderSidebarContext';
import { SenderDetailSidebar } from '@/components/SenderDetailSidebar';
import type { Participant, ProjectMember } from '@/api/types';
import { useIsMobile } from '@/components/WorkItemMobileMeta';
import { TaskTitleLink } from '@/components/TaskTitleLink';
import { RelatedIssuesBlock } from '@/components/RelatedIssuesBlock';
import { ActivityRefText } from '@/components/ActivityRefText';
import { IconClose } from '@/components/icons';
import { useModalA11y } from '@/components/useModalA11y';
import { useProjectMentionCandidates } from '@/components/useProjectMentionCandidates';
import { dependencyEdgeError, validDropTargets } from './planDagEdit';
import {
  PLAN_DAG_NODE_H,
  PLAN_DAG_NODE_W,
  PLAN_DAG_STAGE_HEADER_H,
  layoutGraphFlow,
  layoutLegacyFlow,
  refreshGraphFlowNodes,
  refreshLegacyFlowNodes,
  type PlanDagFlowData,
  type PlanDagFlowEdge,
  type PlanDagFlowLayout,
  type PlanDagFlowNode,
} from './planDagFlow';

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
// v2.9 Stage A1 (#287 fast-follow): a PENDING plan's DAG is now EDITABLE here — a
// pending-only dependency-edge editor (add via labeled selects, remove via a list
// of edges) sits below the graph. The backend AddPlanDependency/RemovePlanDependency
// are pending-gated (§9.4: running/done → plan-not-pending), cycle-guarded
// (ErrPlanCycle) and self-edge-rejected (ErrSelfDependency); the editor is gated
// to pending to match, and a running/done plan stays DISPLAY-ONLY.

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
          <PlanContinuationBanner continuations={p.continuations ?? []} />
          {p.progress_control && (p.status === 'done' || p.status === 'discarded'
            ? <details className="px-3 text-xs text-text-secondary" data-testid="plan-terminal-diagnostics"><summary>{t('plan.detail.dag.terminalDiagnostics')} · {fullDateTime(p.progress_control.as_of)}</summary><PlanProgressCockpit control={p.progress_control} /></details>
            : <PlanProgressCockpit control={p.progress_control} />)}

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
            subscription + scroll/composer-pending survive; DAG/Task mount lazily
            when their tab is active. */}
        <div className="flex min-h-0 flex-1 flex-col p-0" data-testid="plan-detail-content">
          {/* Chat stays mounted-but-hidden across tabs (SSE/scroll/pending survive).
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
              projectId={id}
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

export function PlanProgressCockpit({ control }: { control: PlanProgressControl }): React.ReactElement {
  const degraded = control.decision === 'cannot_determine' || control.quality === 'suspect' || control.freshness.state !== 'fresh';
  const [open, setOpen] = useState(false);
  const actionsId = useId();
  const hasActions = control.required_actions.length > 0;
  const summary = (
    <>
      <span className="flex min-w-0 items-center gap-1.5">
        {hasActions && <DagEvolutionChevron open={open} />}
        <strong className="text-text-primary">Progress control</strong>
      </span>
      <span>{control.decision.replaceAll('_', ' ')}</span>
      <span>freshness: {control.freshness.state}</span>
      <span>quality: {control.quality}</span>
      {hasActions && (
        <span className="rounded-full border border-border-base bg-bg-elevated px-1.5 py-0.5 font-medium text-text-secondary">
          {control.required_actions.length} action{control.required_actions.length === 1 ? '' : 's'}
        </span>
      )}
    </>
  );
  return (
    <section
      className={`mx-3 mt-2 rounded-md border px-3 py-1.5 text-xs md:mx-6 ${degraded ? 'border-warning/50 bg-warning/5' : 'border-border-base bg-bg-subtle'}`}
      data-testid="plan-progress-cockpit"
      data-decision={control.decision}
      data-freshness={control.freshness.state}
    >
      {hasActions ? (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          aria-controls={actionsId}
          data-testid="plan-progress-toggle"
          className="flex w-full flex-wrap items-center gap-x-3 gap-y-1 text-left text-text-muted hover:text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          {summary}
        </button>
      ) : (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-text-muted">{summary}</div>
      )}
      {hasActions && open && (
        <ul id={actionsId} className="mt-2 space-y-1" aria-label="Required actions">
          {control.required_actions.map((action) => (
            <li key={action.id} className="rounded border border-border-base bg-bg-elevated px-2 py-1" data-action-category={action.category}>
              <span className="font-medium text-text-primary">{action.category.replaceAll('_', ' ')}</span>
              {' · '}{action.action.replaceAll('_', ' ')}
              {action.owner_display || action.owner_ref ? ` · ${action.owner_display || action.owner_ref}` : ''}
              {action.deadline_at ? ` · due ${formatLocalTime(action.deadline_at)}` : ''}
              {action.trigger_fact_refs.length > 0 ? ` · facts ${action.trigger_fact_refs.join(', ')}` : ''}
              {action.options && action.options.length > 0 ? ` · choices ${action.options.join(' / ')}` : ''}
            </li>
          ))}
        </ul>
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
  const pause = usePausePlan(projectId, plan.id);
  const resume = useResumePlan(projectId, plan.id);
  const reopen = useReopenPlan(projectId, plan.id);
  const discard = useDiscardPlan(projectId, plan.id);
  const [editing, setEditing] = useState(false);
  const [evolving, setEvolving] = useState(false);
  const [confirming, setConfirming] = useState<null | 'delete' | 'archive' | 'discard'>(null);
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
  const canDelete = plan.status === 'pending' && !plan.archived_at;
  const canArchive = (plan.status === 'done' || plan.status === 'discarded') && !plan.archived_at;
  const canDiscard = (plan.status === 'pending' || plan.status === 'running' || plan.status === 'paused') && !plan.archived_at;
  const canReopen = plan.status === 'done' && !plan.archived_at && !!plan.active_generation_id;
  const canEvolve = (plan.status === 'running' || plan.status === 'paused') && !plan.archived_at && !!plan.active_generation_id;

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
            <PlanArchivedBadge archivedAt={plan.archived_at} />
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
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); pause.mutate(); }} disabled={pause.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50">{t('plan.detail.actions.pause')}</button>
                  )}
                  {plan.status === 'paused' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); resume.mutate(); }} disabled={resume.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50">{t('plan.detail.lifecycle.resume')}</button>
                  )}
                  {canReopen && (
                    <button type="button" role="menuitem" data-testid="plan-reopen-btn" onClick={() => { setActionsOpen(false); reopen.mutate(); }} disabled={reopen.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50">{t('plan.detail.actions.reopen')}</button>
                  )}
                  {canEvolve && (
                    <button type="button" role="menuitem" data-testid="plan-evolution-btn" onClick={() => { setActionsOpen(false); setEvolving(true); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle">{t('plan.detail.actions.evolution')}</button>
                  )}
                  {!plan.archived_at && plan.status !== 'done' && plan.status !== 'discarded' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setEditing(true); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">{t('plan.detail.actions.edit')}</button>
                  )}
                  {plan.status === 'pending' && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); start.mutate(); }} disabled={start.isPending} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50">{t('plan.detail.actions.start')}</button>
                  )}
                  {canDiscard && (
                    <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('discard'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle">{t('plan.detail.actions.discard')}</button>
                  )}
                  {canArchive && (
                      <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('archive'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle">{t('plan.detail.actions.archive')}</button>
                  )}
                  {canDelete && (
                      <button type="button" role="menuitem" onClick={() => { setActionsOpen(false); setConfirming('delete'); }} className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle">{t('plan.detail.actions.delete')}</button>
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
        <PlanArchivedBadge archivedAt={plan.archived_at} />
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
            (→ pending); pending → Start. Each control is rendered exactly ONCE here
            (the DAG footer keeps the legend only). */}
        {plan.status === 'running' && (
          // §9.6: a running plan auto-advances; the manual "Advance now" override
          // was removed (@oopslink) — Stop (→ pending) is the only running control.
          <button
            type="button"
            data-testid="plan-pause-btn"
            disabled={pause.isPending}
            onClick={() => { setActionsOpen(false); pause.mutate(); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle disabled:opacity-50 md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base"
          >
            {t('plan.detail.lifecycle.pause')}
          </button>
        )}
        {plan.status === 'paused' && (
          <button
            type="button"
            data-testid="plan-resume-btn"
            disabled={resume.isPending}
            onClick={() => { setActionsOpen(false); resume.mutate(); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50 md:min-h-0 md:w-auto md:rounded md:bg-accent md:px-3 md:py-1.5 md:text-xs md:text-white"
          >
            {t('plan.detail.lifecycle.resume')}
          </button>
        )}
        {canReopen && (
          <button
            type="button"
            data-testid="plan-reopen-btn"
            disabled={reopen.isPending}
            onClick={() => { setActionsOpen(false); reopen.mutate(); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle disabled:opacity-50 md:min-h-0 md:w-auto md:rounded md:border md:border-accent md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:hover:bg-bg-base"
          >
            {t('plan.detail.actions.reopen')}
          </button>
        )}
        {canEvolve && (
          <button
            type="button"
            data-testid="plan-evolution-btn"
            onClick={() => { setActionsOpen(false); setEvolving(true); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm font-semibold text-accent hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-accent md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:hover:bg-bg-base"
          >
            {t('plan.detail.actions.evolution')}
          </button>
        )}
        {/* T238: name + goal are DESCRIPTIVE metadata — editable in any
            non-archived status (pending/running/done). target_date stays pending-only
            (the modal hides it off-pending, and the backend rejects it). An archived
            plan is terminal/read-only, so no Edit. */}
        {!plan.archived_at && plan.status !== 'done' && plan.status !== 'discarded' && (
          <button
            type="button"
            data-testid="plan-edit-btn"
            onClick={() => { setActionsOpen(false); setEditing(true); }}
            className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base md:hover:text-text-primary"
          >
            {t('plan.detail.actions.edit')}
          </button>
        )}
        {plan.status === 'pending' && (
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
        {canDiscard && (
            <button
              type="button"
              data-testid="plan-discard-btn"
              onClick={() => { setActionsOpen(false); setConfirming('discard'); }}
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-danger md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold"
            >
              {t('plan.detail.actions.discard')}
            </button>
        )}
        {canArchive && (
            <button
              type="button"
              data-testid="plan-archive-btn"
              onClick={() => { setActionsOpen(false); setConfirming('archive'); }}
              title={t('plan.detail.actions.archiveTitle')}
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-text-primary hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-border-strong md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-text-secondary md:hover:bg-bg-base md:hover:text-text-primary"
            >
              {t('plan.detail.actions.archive')}
            </button>
        )}
        {canDelete && (
            <button
              type="button"
              data-testid="plan-delete-btn"
              onClick={() => { setActionsOpen(false); setConfirming('delete'); }}
              title={t('plan.detail.actions.deleteTitle')}
              className="flex min-h-[2.75rem] w-full items-center px-3 text-sm text-danger hover:bg-bg-subtle md:min-h-0 md:w-auto md:rounded md:border md:border-danger md:bg-bg-subtle md:px-3 md:py-1.5 md:text-xs md:font-semibold md:text-danger md:hover:bg-bg-base"
            >
              {t('plan.detail.actions.delete')}
            </button>
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
	      {(start.isError || pause.isError || resume.isError || reopen.isError || discard.isError) && (
	        <p className="text-xs text-danger" data-testid="plan-lifecycle-error">
	          {((start.error ?? pause.error ?? resume.error ?? reopen.error ?? discard.error) as Error).message}
	        </p>
	      )}
      {editing && (
        <PlanEditModal projectId={projectId} plan={plan} onClose={() => setEditing(false)} />
      )}
      {evolving && (
        <PlanEvolutionModal projectId={projectId} plan={plan} onClose={() => setEvolving(false)} />
      )}
      {confirming === 'delete' && (
        <PlanDeleteModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
      )}
      {confirming === 'archive' && (
        <PlanArchiveModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
      )}
      {confirming === 'discard' && (
        <PlanDiscardModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
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

function PlanContinuationBanner({ continuations }: { continuations: PlanContinuation[] }): React.ReactElement | null {
  const active = continuations.filter((continuation) => continuation.status !== 'closed');
  if (active.length === 0) return null;
  const latest = active.slice().sort((a, b) => b.generation - a.generation)[0];
  const exhausted = latest.status === 'budget_exhausted';
  return (
    <div
      className={`mx-3 mb-1 rounded-lg border px-3 py-2 text-xs md:mx-5 ${
        exhausted
          ? 'border-status-amber-border bg-status-amber-bg text-status-amber-fg'
          : 'border-status-blue-border bg-status-blue-bg text-status-blue-fg'
      }`}
      role="status"
      aria-live="polite"
      data-testid="plan-continuation-banner"
    >
      <span className="font-semibold">Remediation continuation</span>
      <span className="ml-2 font-mono uppercase">{latest.status.replaceAll('_', ' ')}</span>
      <span className="ml-2">generation {latest.generation}</span>
      <span className="ml-2">budget {latest.remaining_budget}</span>
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
  const pause = usePausePlan(projectId, plan.id);
  const resume = useResumePlan(projectId, plan.id);
  const reopen = useReopenPlan(projectId, plan.id);
  const discard = useDiscardPlan(projectId, plan.id);
  const [editing, setEditing] = useState(false);
  const [evolving, setEvolving] = useState(false);
  const [confirming, setConfirming] = useState<null | 'delete' | 'archive' | 'discard'>(null);
  const [goalOpen, setGoalOpen] = useState(false);
  // @oopslink: Up next is collapsible (mirrors the unmerged-branch panel). Default
  // open so the queue stays visible; the header chevron toggles it.
  const [upNextOpen, setUpNextOpen] = useState(true);
  // The agent-activity sidebar (SenderDetailSidebar, unchanged) — opened by the
  // @creator tag / a participant avatar. Local state keeps the rail decoupled
  // from the chat's own SenderSidebarProvider.
  const [agentRef, setAgentRef] = useState<string | null>(null);

  const goalLong = plan.description.trim().length > 80 || plan.description.includes('\n');
  const canDelete = plan.status === 'pending' && !plan.archived_at;
  const canArchive = (plan.status === 'done' || plan.status === 'discarded') && !plan.archived_at;
  const canDiscard = (plan.status === 'pending' || plan.status === 'running' || plan.status === 'paused') && !plan.archived_at;
  const canReopen = plan.status === 'done' && !plan.archived_at && !!plan.active_generation_id;
  const canEvolve = (plan.status === 'running' || plan.status === 'paused') && !plan.archived_at && !!plan.active_generation_id;

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
          <PlanArchivedBadge archivedAt={plan.archived_at} />
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
            running/pending Start/Stop button is also present. */}
        <div className="flex flex-wrap gap-2">
          {plan.status === 'running' && (
            <button
              type="button"
              data-testid="plan-pause-btn"
              disabled={pause.isPending}
              onClick={() => pause.mutate()}
              className={`${railBtn} text-danger hover:text-danger`}
            >
              {t('plan.detail.lifecycle.pause')}
            </button>
          )}
          {plan.status === 'paused' && (
            <button
              type="button"
              data-testid="plan-resume-btn"
              disabled={resume.isPending}
              onClick={() => resume.mutate()}
              className="flex-1 rounded-lg border-0 bg-accent px-3 py-2 text-center text-xs font-semibold text-white hover:opacity-90 disabled:opacity-50"
            >
              {t('plan.detail.lifecycle.resume')}
            </button>
          )}
          {plan.status === 'pending' && (
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
          {canReopen && (
            <button
              type="button"
              data-testid="plan-reopen-btn"
              disabled={reopen.isPending}
              onClick={() => reopen.mutate()}
              className={`${railBtn} border-accent text-accent hover:text-accent`}
            >
              {t('plan.detail.actions.reopen')}
            </button>
          )}
          {canEvolve && (
            <button
              type="button"
              data-testid="plan-evolution-btn"
              onClick={() => setEvolving(true)}
              className={`${railBtn} border-accent text-accent hover:text-accent`}
            >
              {t('plan.detail.actions.evolution')}
            </button>
          )}
          {!plan.archived_at && plan.status !== 'done' && plan.status !== 'discarded' && (
            <button type="button" data-testid="plan-edit-btn" onClick={() => setEditing(true)} className={railBtn}>
              {t('plan.detail.actions.edit')}
            </button>
          )}
          {canDiscard && (
              <button type="button" data-testid="plan-discard-btn" onClick={() => setConfirming('discard')} className={`${railBtn} border-danger text-danger hover:text-danger`}>
                {t('plan.detail.actions.discard')}
              </button>
          )}
          {canArchive && (
              <button type="button" data-testid="plan-archive-btn" onClick={() => setConfirming('archive')} className={railBtn}>
                {t('plan.detail.actions.archive')}
              </button>
          )}
          {canDelete && (
              <button
                type="button"
                data-testid="plan-delete-btn"
                onClick={() => setConfirming('delete')}
                className={`${railBtn} border-danger text-danger hover:text-danger`}
              >
                {t('plan.detail.actions.delete')}
              </button>
          )}
        </div>
	        {(start.isError || pause.isError || resume.isError || reopen.isError || discard.isError) && (
	          <p className="text-xs text-danger" data-testid="plan-lifecycle-error">
	            {((start.error ?? pause.error ?? resume.error ?? reopen.error ?? discard.error) as Error).message}
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
                <TaskTitleLink
                  projectId={projectId}
                  taskId={n.task_id}
                  title={n.title}
                  className="min-w-0 flex-1"
                />
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
      {evolving && <PlanEvolutionModal projectId={projectId} plan={plan} onClose={() => setEvolving(false)} />}
      {confirming === 'delete' && <PlanDeleteModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />}
      {confirming === 'archive' && <PlanArchiveModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />}
      {confirming === 'discard' && <PlanDiscardModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />}
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

function PlanDiscardModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const discard = useDiscardPlan(projectId, plan.id);
  const onConfirm = async () => {
    try {
      await discard.mutateAsync();
      onClose();
    } catch {
      // Keep the modal open and surface the lifecycle conflict inline.
    }
  };
  return (
    <DestructiveConfirmModal
      testId="plan-discard-modal"
      title={t('plan.detail.discardModal.title')}
      planName={plan.name}
      body={t('plan.detail.discardModal.body')}
      confirmLabel={t('plan.detail.discardModal.confirm')}
      pendingLabel={t('plan.detail.discardModal.pending')}
      pending={discard.isPending}
      error={discard.isError ? friendlyDestructivePlanError(discard.error) : null}
      errorTestId="plan-discard-error"
      cancelTestId="plan-discard-cancel"
      confirmTestId="plan-discard-confirm"
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
// target_date via usePatchPlan (PATCH /{id}). pending-only — opened only from the
// header's pending-gated Edit button (§9.4: running/done is immutable, the backend
// rejects with plan-not-pending). Mirrors PlanCreateModal's structure + styling
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
  if (lower.includes('pending')) {
    // T238: name/goal edit any time; only the target date is pending-only.
    return t('plan.detail.editModal.errorPending');
  }
  return t('plan.detail.editModal.errorGeneric');
}

function planBaseVersion(plan: Plan): number {
  return typeof plan.version === 'number' && Number.isFinite(plan.version) ? plan.version : 0;
}

function friendlyEvolutionError(error: unknown, t: TFunction): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('idempotency')) {
    return t('plan.detail.evolutionModal.errorIdempotency');
  }
  if (lower.includes('in-flight') || lower.includes('in flight') || lower.includes('dispatched') || lower.includes('running node')) {
    return t('plan.detail.evolutionModal.errorInFlight');
  }
  if (lower.includes('disconnected') || lower.includes('detached') || lower.includes('prerequisite edge')) {
    return t('plan.detail.evolutionModal.errorDisconnected');
  }
  if (lower.includes('version') || lower.includes('stale') || lower.includes('parent') || lower.includes('active_generation') || lower.includes('conflict')) {
    return t('plan.detail.evolutionModal.errorVersion');
  }
  return t('plan.detail.evolutionModal.errorGeneric');
}

function parseEvolutionDiff(raw: string, t: TFunction): PlanGenerationDiff {
  const text = raw.trim();
  let parsed: unknown;
  try {
    parsed = JSON.parse(text) as unknown;
  } catch {
    throw new Error(t('plan.detail.evolutionModal.errorDiffJson'));
  }
  if (
    !parsed || typeof parsed !== 'object' || Array.isArray(parsed)
    || !Array.isArray((parsed as Record<string, unknown>).node_decisions)
    || !Array.isArray((parsed as Record<string, unknown>).tasks)
    || !Array.isArray((parsed as Record<string, unknown>).edges)
  ) {
    throw new Error(t('plan.detail.evolutionModal.errorDiffShape'));
  }
  return parsed as PlanGenerationDiff;
}

function PlanEvolutionModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const baseVersion = planBaseVersion(plan);
  const parentGenerationId = plan.active_generation_id ?? '';
  const [reason, setReason] = useState('');
  const [evidence, setEvidence] = useState('');
  const [idempotencyKey, setIdempotencyKey] = useState(() => `evo-${plan.id}-${baseVersion}-${Date.now()}`);
  const [diffText, setDiffText] = useState('{\n  "node_decisions": [],\n  "tasks": [],\n  "edges": []\n}');
  const blockContext = plan.blocked_on?.[0] ?? null;
  const [resolutionKind, setResolutionKind] = useState<'replace' | 'bypass' | ''>('');
  const [resolutionNote, setResolutionNote] = useState('');
  const [parseError, setParseError] = useState<string | null>(null);
  const commit = useCommitPlanEvolution(projectId, plan.id);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setParseError(null);
    let diff: PlanGenerationDiff;
    try {
      diff = parseEvolutionDiff(diffText, t);
    } catch (err) {
      setParseError(err instanceof Error ? err.message : t('plan.detail.evolutionModal.errorDiffJson'));
      return;
    }
    const input: CommitPlanEvolutionInput = {
      parent_generation_id: parentGenerationId,
      base_version: baseVersion,
      reason: reason.trim(),
      evidence: evidence.trim(),
      idempotency_key: idempotencyKey.trim(),
      diff,
    };
    if (blockContext && resolutionKind) {
      input.resolve_block_event_id = blockContext.event_id || blockContext.task_id;
      input.resolution_kind = resolutionKind;
      input.resolution_note = resolutionNote.trim();
    }
    try {
      await commit.mutateAsync(input);
      onClose();
    } catch {
      // surfaced inline below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid="plan-evolution-modal"
      role="dialog"
      aria-modal="true"
      aria-label={t('plan.detail.evolutionModal.aria')}
    >
      <form onSubmit={submit} className="max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <div className="mb-4 flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <h2 className="text-lg font-semibold">{t('plan.detail.evolutionModal.title')}</h2>
            <p className="mt-1 text-xs text-text-muted" data-testid="plan-evolution-base-version">
              {t('plan.detail.evolutionModal.baseVersion', { version: baseVersion })}
            </p>
            <p className="mt-1 truncate font-mono text-[0.6875rem] text-text-muted" data-testid="plan-evolution-parent-generation" title={parentGenerationId}>
              {t('plan.detail.evolutionModal.parentGeneration', { id: parentGenerationId })}
            </p>
          </div>
          <span className="rounded bg-status-blue-bg px-2 py-1 font-mono text-[0.6875rem] font-semibold text-status-blue-fg">
            {t('plan.detail.evolutionModal.liveStatus', { status: plan.status })}
          </span>
        </div>

        <label className="block text-xs font-medium" htmlFor="plan-evolution-reason">
          {t('plan.detail.evolutionModal.reason')}
        </label>
        <input
          id="plan-evolution-reason"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-evolution-reason"
          required
          autoFocus
        />

        <label className="mt-3 block text-xs font-medium" htmlFor="plan-evolution-evidence">
          {t('plan.detail.evolutionModal.evidence')}
        </label>
        <textarea
          id="plan-evolution-evidence"
          value={evidence}
          onChange={(e) => setEvidence(e.target.value)}
          rows={3}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-evolution-evidence"
          required
        />

        <label className="mt-3 block text-xs font-medium" htmlFor="plan-evolution-idempotency">
          {t('plan.detail.evolutionModal.idempotency')}
        </label>
        <input
          id="plan-evolution-idempotency"
          value={idempotencyKey}
          onChange={(e) => setIdempotencyKey(e.target.value)}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-evolution-idempotency"
          required
        />

        <label className="mt-3 block text-xs font-medium" htmlFor="plan-evolution-diff">
          {t('plan.detail.evolutionModal.diff')}
        </label>
        <textarea
          id="plan-evolution-diff"
          value={diffText}
          onChange={(e) => setDiffText(e.target.value)}
          rows={8}
          spellCheck={false}
          className={`${PLAN_EDIT_MODAL_INPUT} font-mono`}
          data-testid="plan-evolution-diff"
        />
        <p className="mt-1 text-[0.6875rem] text-text-muted">
          {t('plan.detail.evolutionModal.diffHint')}
        </p>

        {blockContext && (
          <section className="mt-4 rounded border border-border-base bg-bg-subtle p-3" data-testid="plan-blocked-resolution-panel">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
              <div className="min-w-0 text-xs text-text-secondary">
                <div className="font-semibold text-text-primary">Blocked context</div>
                <div data-testid="plan-blocked-resolution-reason">{blockContext.trigger_condition || blockContext.wait_type}</div>
                <div className="mt-1 font-mono text-[0.6875rem] text-text-muted" data-testid="plan-blocked-resolution-event">
                  {blockContext.event_id || blockContext.task_id}
                </div>
              </div>
              <div className="flex shrink-0 gap-2" role="group" aria-label="Block resolution action">
                <button
                  type="button"
                  className={`rounded border px-3 py-1.5 text-xs font-semibold ${resolutionKind === 'replace' ? 'border-brand bg-brand text-white' : 'border-border-base text-text-primary hover:bg-bg-elevated'}`}
                  onClick={() => setResolutionKind('replace')}
                  data-testid="plan-block-replace"
                >
                  Replace
                </button>
                <button
                  type="button"
                  className={`rounded border px-3 py-1.5 text-xs font-semibold ${resolutionKind === 'bypass' ? 'border-brand bg-brand text-white' : 'border-border-base text-text-primary hover:bg-bg-elevated'}`}
                  onClick={() => setResolutionKind('bypass')}
                  data-testid="plan-block-bypass"
                >
                  Bypass
                </button>
              </div>
            </div>
            {resolutionKind && (
              <>
                <label className="mt-3 block text-xs font-medium" htmlFor="plan-block-resolution-note">
                  Resolution note
                </label>
                <input
                  id="plan-block-resolution-note"
                  value={resolutionNote}
                  onChange={(e) => setResolutionNote(e.target.value)}
                  className={PLAN_EDIT_MODAL_INPUT}
                  data-testid="plan-block-resolution-note"
                  required
                />
              </>
            )}
          </section>
        )}

        {(parseError || commit.isError) && (
          <p className="mt-3 rounded border border-danger bg-bg-subtle px-3 py-2 text-xs font-medium text-danger" role="alert" data-testid="plan-evolution-error">
            {parseError ?? friendlyEvolutionError(commit.error, t)}
          </p>
        )}

        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="plan-evolution-cancel"
          >
            {t('plan.detail.cancel')}
          </button>
          <button
            type="submit"
            disabled={commit.isPending || !parentGenerationId || !reason.trim() || !evidence.trim() || !idempotencyKey.trim() || (Boolean(resolutionKind) && !resolutionNote.trim())}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-evolution-submit"
          >
            {commit.isPending ? t('plan.detail.evolutionModal.committing') : t('plan.detail.evolutionModal.commit')}
          </button>
        </div>
      </form>
    </div>
  );
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
  // (scheduling) stays pending-only, so the field only shows for a pending plan.
  const isPending = plan.status === 'pending';

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
    // target_date — pending-only (§9.4); distinguish cleared / changed / unchanged.
    if (isPending && targetDate !== originalTargetDate) {
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
        {isPending && (
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
  discarded: {
    label: 'discarded',
    cls: 'bg-status-stone-bg text-status-stone-fg',
    border: 'border-status-stone-border',
    icon: <span aria-hidden="true">×</span>,
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

const NODE_STATE_ORDER: PlanNodeStatus[] = ['blocked', 'ready', 'dispatched', 'running', 'paused', 'done', 'discarded', 'failed'];

// Highlight active work without reducing readability of completed tasks.
function nodeVisualCls(status: PlanNodeStatus): string {
  if (status === 'running') return 'ring-2 ring-status-amber-border/35';
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

function SupersededDrainingChip({ node }: { node: PlanNode }): React.ReactElement | null {
  const { t } = useTranslation('work');
  if (node.effective !== false || node.node_status !== 'running') return null;
  return (
    <span
      className="inline-flex shrink-0 items-center rounded bg-status-amber-bg px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide text-status-amber-fg"
      data-testid="superseded-draining-chip"
      title={node.superseded_reason || undefined}
    >
      {t('plan.detail.nodeStatus.supersededDraining')}
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

function nodeRevisionLabel(node: { revision?: number }): string | null {
  if (typeof node.revision === 'number' && Number.isFinite(node.revision)) return `R${Math.max(1, Math.trunc(node.revision) + 1)}`;
  return null;
}

export function generationStyle(revision?: number): React.CSSProperties | undefined {
  if (revision == null || !Number.isFinite(revision)) return undefined;
  return { '--plan-generation-hue': String((215 + Math.max(0, Math.trunc(revision)) * 137.508) % 360) } as React.CSSProperties;
}

function NodeGenerationBadge({ node, testId = 'plan-node-generation' }: { node: { revision?: number }; testId?: string }): React.ReactElement | null {
  const label = nodeRevisionLabel(node);
  if (!label) return null;
  return (
    <span
      className="plan-generation plan-generation-badge inline-flex shrink-0 items-center rounded px-1.5 py-0.5 font-mono text-[0.5625rem] font-semibold uppercase"
      style={generationStyle(node.revision)}
      data-testid={testId}
      title={`Generation ${label}`}
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
// The desktop DAG is rendered by React Flow + ELK. The remaining Position type is
// for the mobile stepper, which derives its order from the same React Flow layout.

interface Positioned {
  node: PlanNode;
  level: number;
  x: number;
  y: number;
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
  generationNodeOf,
}: {
  positioned: Positioned[];
  projectId: string;
  generationNodeOf?: Map<string, PlanGenerationRead['nodes'][number]>;
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
        const generationNode = generationNodeOf?.get(taskId);
        const generationMeta = {
          revision: generationNode?.revision,
        };
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
                  <NodeGenerationBadge node={generationMeta} />
                  <TaskArchivedBadge archived={p.node.archived} taskId={taskId} />
                  <NodeStateChip status={p.node.node_status} />
                  <SupersededDrainingChip node={p.node} />
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

// A control node marker: Start/End circular terminals; Condition a rotated
// (diamond) square. Distinct from task cards so the control flow is legible.
// Mirrors the mockup's `.terminal.start/.end` (filled accent disc vs. a
// done-toned ring) and `.gate` diamond (two-line label: the gate id + its name).
function ControlNodeMarker({ node, gateStageRef, testId = 'plan-graph-control-node' }: { node: PlanGraphNode; gateStageRef?: string; testId?: string }): React.ReactElement {
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
      className={`flex h-full w-full items-center justify-center ${
        testId === 'plan-dag-synthetic-start'
          ? 'rounded-full border-[1.5px] border-accent bg-accent text-white shadow-[0_4px_16px_-4px_var(--color-accent)]'
          : testId === 'plan-dag-synthetic-end'
            ? 'rounded-full border-2 border-status-emerald-border bg-bg-elevated text-status-emerald-fg shadow-2'
            : ''
      }`}
      data-testid={testId}
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

interface DagEvolutionRevision {
  generation: number;
  revision: number;
  label: string;
  title: string;
  reason: string;
  stageCount: number;
  taskCount: number;
  active?: boolean;
  progress?: { done: number; total: number };
  diff?: PlanGenerationDiff;
  changes?: Array<{ taskId: string; label: string; action: string; reason?: string }>;
  idempotencyKey?: string;
  generationId?: string;
  parentGenerationId?: string;
  verdictId?: string;
  verdictOutcome?: GateVerdict['outcome'];
  continuationId?: string;
  createdAt?: string;
}

function stageTaskIdSet(stages: PlanStage[]): Set<string> {
  const ids = new Set<string>();
  for (const stage of stages) {
    for (const member of stage.members ?? []) ids.add(member.task_id);
  }
  return ids;
}

function usableGenerationRead(read: PlanGenerationRead | undefined): PlanGenerationRead | undefined {
  if (!read) return undefined;
  return read.generations.length > 0 || read.nodes.length > 0 ? read : undefined;
}

function generationTitle(gen: PlanGeneration, t: TFunction): string {
  if (gen.reason.trim()) return gen.reason.trim();
  if (gen.revision === 0) return t('plan.detail.dag.evolution.initialTitle');
  return t('plan.detail.dag.evolution.reasonUnknown');
}

function buildGenerationEvolutionRevisions(
  generationRead: PlanGenerationRead,
  t: TFunction,
): DagEvolutionRevision[] {
  return generationRead.generations
    .slice()
    .sort((a, b) => a.revision - b.revision)
    .map((gen) => ({
      generation: gen.revision,
      revision: gen.revision + 1,
      label: `R${gen.revision + 1}`,
      title: generationTitle(gen, t),
      reason: gen.evidence.trim() || gen.reason.trim() || (gen.revision === 0
        ? t('plan.detail.dag.evolution.initialReason')
        : t('plan.detail.dag.evolution.reasonUnknown')),
      stageCount: new Set(gen.snapshot.tasks.map((task) => task.stage_id).filter(Boolean)).size,
      taskCount: gen.snapshot.tasks.length,
      active: gen.id === generationRead.active_generation_id,
      progress: gen.snapshot_progress,
      diff: gen.diff,
      changes: (gen.diff?.node_decisions ?? []).map((decision) => {
        const task = gen.snapshot.tasks.find((task) => task.task_id === decision.task_id);
        return { taskId: decision.task_id, label: task?.org_ref || task?.title || decision.task_id, action: decision.action, reason: decision.reason };
      }),
      idempotencyKey: gen.idempotency_key,
      generationId: gen.id,
      parentGenerationId: gen.parent_generation_id,
      createdAt: gen.created_at,
    }));
}

function buildDagEvolutionRevisions(
  generationRead: PlanGenerationRead | undefined,
  t: TFunction,
): DagEvolutionRevision[] {
  if (!generationRead?.generations.length) return [];
  return buildGenerationEvolutionRevisions(generationRead, t);
}

function generationNodeMap(generationRead: PlanGenerationRead | undefined): Map<string, PlanGenerationRead['nodes'][number]> {
  const map = new Map<string, PlanGenerationRead['nodes'][number]>();
  for (const node of generationRead?.nodes ?? []) map.set(node.task_id, node);
  return map;
}

function evolutionDiffLabel(diff: PlanGenerationDiff | undefined, t: TFunction): string {
  if (!diff) return t('plan.detail.dag.evolution.diffEmpty');
  return t('plan.detail.dag.evolution.diffSummary', {
    nodes: diff.tasks?.length ?? 0,
    decisions: diff.node_decisions?.length ?? 0,
    edges: diff.edges?.length ?? 0,
  });
}

function activeGenerationRevision(generationRead: PlanGenerationRead | undefined): number | undefined {
  return generationRead?.generations.find((generation) => generation.id === generationRead.active_generation_id)?.revision;
}

function generationAtRevision(generationRead: PlanGenerationRead | undefined, revision: number): PlanGeneration | undefined {
  return generationRead?.generations.find((generation) => generation.revision === revision);
}

export function snapshotPlanNodes(generation: PlanGeneration | undefined): PlanNode[] {
  if (!generation) return [];
  const taskById = new Map(generation.snapshot.tasks.map((task) => [task.task_id, task]));
  const dispatched = new Set(generation.snapshot.dispatch_records.map((record) => record.task_id));
  const dependsOn = new Map<string, string[]>();
  for (const edge of generation.snapshot.edges) {
    dependsOn.set(edge.from_task_id, [...(dependsOn.get(edge.from_task_id) ?? []), edge.to_task_id]);
  }
  return generation.snapshot.tasks.map((task) => {
    const dependencies = dependsOn.get(task.task_id) ?? [];
    let nodeStatus: PlanNodeStatus = 'blocked';
    if (task.status === 'completed') nodeStatus = 'done';
    else if (task.status === 'discarded') nodeStatus = 'discarded';
    else if (task.status === 'failed') nodeStatus = 'failed';
    else if (task.status === 'running') nodeStatus = 'running';
    else if (task.status === 'paused') nodeStatus = 'paused';
    else if (task.status === 'blocked') nodeStatus = 'blocked';
    else if (dispatched.has(task.task_id)) nodeStatus = 'dispatched';
    else if (dependencies.every((id) => {
      const upstream = taskById.get(id);
      return upstream?.status === 'completed' || upstream?.status === 'discarded';
    })) nodeStatus = 'ready';
    return {
      task_id: task.task_id,
      org_ref: task.org_ref,
      title: task.title,
      assignee_ref: task.assignee_ref ?? '',
      task_status: task.status,
      node_status: nodeStatus,
      depends_on: dependencies,
      effective: task.status !== 'discarded',
    };
  });
}

export function snapshotPlanGraph(generation: PlanGeneration | undefined): { nodes: PlanGraphNode[]; edges: PlanGraphEdge[] } | undefined {
  if (!generation) return undefined;
  const nodeIdByTask = new Map<string, string>();
  const nodes: PlanGraphNode[] = generation.snapshot.tasks.map((task) => {
    const id = task.node_id || `snapshot:${task.task_id}`;
    nodeIdByTask.set(task.task_id, id);
    return {
      id,
      category: 'business' as const,
      title: task.title,
      status: task.status as PlanGraphNodeStatus,
      task_id: task.task_id,
      task_status: task.status,
      org_ref: task.org_ref,
      stage_id: task.stage_id,
      follows_task_id: task.follows_task_id,
      assignee_ref: task.assignee_ref,
    };
  });
  const edges = generation.snapshot.edges.flatMap((edge) => {
    const from = nodeIdByTask.get(edge.to_task_id);
    const to = nodeIdByTask.get(edge.from_task_id);
    if (!from || !to) return [];
    const kind: PlanGraphEdgeKind = edge.kind === 'conditional' || edge.kind === 'loopback' ? edge.kind : 'seq';
    return [{ from, to, kind }];
  });
  if (nodes.length) {
    nodes.push(
      { id: '__snapshot_start__', category: 'control', control_kind: 'start', title: 'Start', status: 'open' },
      { id: '__snapshot_end__', category: 'control', control_kind: 'end', title: 'End', status: 'open' },
    );
  }
  return { nodes, edges };
}

function edgeKey(from: string, to: string): string {
  return `${from}->${to}`;
}

function taskStageMembership(nodes: PlanGraphNode[], stages: PlanStage[]): Map<string, string> {
  const validStageIds = new Set(stages.map((stage) => stage.id));
  const stageIdByTask = new Map<string, string>();
  for (const stage of stages) {
    for (const member of stage.members ?? []) stageIdByTask.set(member.task_id, stage.id);
  }
  const nodeByTask = new Map<string, PlanGraphNode>();
  for (const node of nodes) {
    if (node.category !== 'business' || !node.task_id) continue;
    nodeByTask.set(node.task_id, node);
    if (node.stage_id && validStageIds.has(node.stage_id)) stageIdByTask.set(node.task_id, node.stage_id);
  }
  const resolve = (taskId: string, seen = new Set<string>()): string | undefined => {
    const direct = stageIdByTask.get(taskId);
    if (direct) return direct;
    if (seen.has(taskId)) return undefined;
    seen.add(taskId);
    const followsTaskId = nodeByTask.get(taskId)?.follows_task_id;
    if (!followsTaskId) return undefined;
    const inherited = resolve(followsTaskId, seen);
    if (inherited) stageIdByTask.set(taskId, inherited);
    return inherited;
  };
  for (const taskId of nodeByTask.keys()) resolve(taskId);
  return stageIdByTask;
}

// Generation snapshots persist task dependencies, while the stage execution graph
// also contains derived gate/barrier edges. Rebuild those visual edges from the
// stage read model so historical evolution DAGs stay connected.
export function withStageTopologyEdges(
  nodes: PlanGraphNode[],
  edges: PlanGraphEdge[],
  stages: PlanStage[],
): PlanGraphEdge[] {
  if (stages.length === 0 || nodes.length === 0) return edges;

  const startNode = nodes.find((node) => node.category === 'control' && node.control_kind === 'start');
  const endNode = nodes.find((node) => node.category === 'control' && node.control_kind === 'end');
  const nodeById = new Map(nodes.map((node) => [node.id, node]));
  const nodeIdByTask = new Map<string, string>();
  for (const node of nodes) {
    if (node.category === 'business' && node.task_id) nodeIdByTask.set(node.task_id, node.id);
  }

  const stageById = new Map(stages.map((stage) => [stage.id, stage]));
  const stageIdByTask = taskStageMembership(nodes, stages);
  const explicitStageMemberTaskIds = new Set<string>();
  for (const stage of stages) {
    for (const member of stage.members ?? []) explicitStageMemberTaskIds.add(member.task_id);
  }

  // In staged layouts Start/End are visual anchors for the whole stage DAG, not
  // ordinary nodes inside a specific generation. Old or partial graph snapshots
  // can still carry direct Start->task / task->End edges; preserving them after
  // remediation stages are appended draws long cross-canvas lines and makes the
  // flow look disconnected. Keep real task/gate edges, then rebuild terminal
  // anchor edges from the stage topology below.
  const structuralEdges = edges.filter((edge) => {
    if (edge.kind === 'loopback') return true;
    if (startNode && edge.from === startNode.id) return false;
    if (endNode && edge.to === endNode.id) return false;
    return true;
  });
  const existing = new Set(structuralEdges.map((edge) => edgeKey(edge.from, edge.to)));
  const out = [...structuralEdges];
  const add = (from: string | undefined, to: string | undefined) => {
    if (!from || !to || from === to) return;
    if (!nodeById.has(from) || !nodeById.has(to)) return;
    const key = edgeKey(from, to);
    if (existing.has(key)) return;
    existing.add(key);
    out.push({ from, to, kind: 'seq' });
  };

  const incomingWithinStage = new Map<string, Set<string>>();
  for (const edge of edges) {
    const fromTask = nodeById.get(edge.from)?.task_id;
    const toTask = nodeById.get(edge.to)?.task_id;
    if (!fromTask || !toTask) continue;
    const fromStage = stageIdByTask.get(fromTask);
    if (!fromStage || fromStage !== stageIdByTask.get(toTask)) continue;
    if (!incomingWithinStage.has(fromStage)) incomingWithinStage.set(fromStage, new Set());
    incomingWithinStage.get(fromStage)!.add(toTask);
  }

  const gateTargetOf = (stage: PlanStage): string | undefined => {
    if (stage.gate_node_id && nodeById.has(stage.gate_node_id)) return stage.gate_node_id;
    return stage.gate_task_id ? nodeIdByTask.get(stage.gate_task_id) : undefined;
  };

  const explicitNonGateMembers = (stage: PlanStage): string[] =>
    (stage.members ?? [])
      .map((member) => member.task_id)
      .filter((taskId) => nodeIdByTask.has(taskId) && taskId !== stage.gate_task_id);

  const effectiveMembers = (stage: PlanStage): string[] =>
    [...nodeIdByTask.keys()].filter(
      (taskId) => stageIdByTask.get(taskId) === stage.id && taskId !== stage.gate_task_id,
    );

  const followsTaskInStage = (taskId: string, stageId: string): boolean => {
    const followsTaskId = nodeById.get(nodeIdByTask.get(taskId)!)?.follows_task_id;
    return !!followsTaskId && stageIdByTask.get(followsTaskId) === stageId;
  };

  const stageEntries = (stage: PlanStage): string[] => {
    const incoming = incomingWithinStage.get(stage.id) ?? new Set();
    const entries = effectiveMembers(stage).filter((taskId) => !incoming.has(taskId) && !followsTaskInStage(taskId, stage.id));
    return entries.length > 0 ? entries : effectiveMembers(stage);
  };

  const followerTasksFor = (sourceTaskId: string | undefined): string[] => {
    if (!sourceTaskId) return [];
    return [...nodeIdByTask.keys()].filter((taskId) => {
      if (explicitStageMemberTaskIds.has(taskId)) return false;
      return nodeById.get(nodeIdByTask.get(taskId)!)?.follows_task_id === sourceTaskId;
    });
  };

  const chainTails = (taskIds: string[]): string[] => {
    if (taskIds.length === 0) return [];
    const taskSet = new Set(taskIds);
    const hasDownstream = new Set<string>();
    for (const edge of out) {
      const fromTask = nodeById.get(edge.from)?.task_id;
      const toTask = nodeById.get(edge.to)?.task_id;
      if (fromTask && toTask && taskSet.has(fromTask) && taskSet.has(toTask)) {
        hasDownstream.add(fromTask);
      }
    }
    return taskIds.filter((taskId) => !hasDownstream.has(taskId));
  };

  const stageExitTargets = (stage: PlanStage): string[] => {
    const gateFollowerTails = chainTails(followerTasksFor(stage.gate_task_id))
      .map((taskId) => nodeIdByTask.get(taskId))
      .filter((nodeId): nodeId is string => !!nodeId);
    if (gateFollowerTails.length > 0) return gateFollowerTails;
    const gateTarget = gateTargetOf(stage);
    if (gateTarget) return [gateTarget];
    return effectiveMembers(stage)
      .map((taskId) => nodeIdByTask.get(taskId))
      .filter((nodeId): nodeId is string => !!nodeId);
  };

  const stagesWithDownstream = new Set<string>();
  for (const stage of stages) {
    for (const upstreamStageId of stage.depends_on_stages ?? []) {
      if (stageById.has(upstreamStageId)) stagesWithDownstream.add(upstreamStageId);
    }
  }

  for (const stage of stages) {
    const validStageDeps = (stage.depends_on_stages ?? []).filter((stageId) => stageById.has(stageId));
    if (validStageDeps.length === 0) {
      const entries = stageEntries(stage);
      const startTargets = entries.length > 0 ? entries.map((taskId) => nodeIdByTask.get(taskId)) : stageExitTargets(stage);
      for (const target of startTargets) add(startNode?.id, target);
    }

    const gateTaskNode = stage.gate_task_id ? nodeIdByTask.get(stage.gate_task_id) : undefined;
    const gateTarget = gateTargetOf(stage);
    if (gateTaskNode) {
      for (const memberTask of explicitNonGateMembers(stage)) add(nodeIdByTask.get(memberTask), gateTaskNode);
      if (stage.gate_node_id && nodeById.has(stage.gate_node_id)) add(gateTaskNode, stage.gate_node_id);
    } else if (gateTarget) {
      for (const memberTask of explicitNonGateMembers(stage)) add(nodeIdByTask.get(memberTask), gateTarget);
    }

    for (const memberTask of effectiveMembers(stage)) {
      if (explicitStageMemberTaskIds.has(memberTask)) continue;
      if (incomingWithinStage.get(stage.id)?.has(memberTask)) continue;
      const followsTaskId = nodeById.get(nodeIdByTask.get(memberTask)!)?.follows_task_id;
      if (followsTaskId && stageIdByTask.get(followsTaskId) === stage.id) {
        add(nodeIdByTask.get(followsTaskId), nodeIdByTask.get(memberTask));
      }
    }

    for (const upstreamStageId of stage.depends_on_stages ?? []) {
      const upstreamStage = stageById.get(upstreamStageId);
      const upstreamExits = upstreamStage ? stageExitTargets(upstreamStage) : [];
      for (const upstreamExit of upstreamExits) {
        for (const entryTask of stageEntries(stage)) add(upstreamExit, nodeIdByTask.get(entryTask));
      }
    }
  }

  for (const stage of stages) {
    if (stagesWithDownstream.has(stage.id)) continue;
    for (const source of stageExitTargets(stage)) add(source, endNode?.id);
  }

  return out;
}

function DagEvolutionPanel({
  revisions,
  selectedGeneration,
  onSelectGeneration,
}: {
  revisions: DagEvolutionRevision[];
  selectedGeneration: number;
  onSelectGeneration: (generation: number) => void;
}): React.ReactElement | null {
  const { t } = useTranslation('work');
  const [collapsed, setCollapsed] = useState(false);
  const [playing, setPlaying] = useState(false);
  const [detailOpen, setDetailOpen] = useState(false);
  const panelBodyId = React.useId();
  const detailTitleId = React.useId();
  const detailDialogRef = useModalA11y({ open: detailOpen, onClose: () => setDetailOpen(false) });

  const selectedIndex = Math.max(0, revisions.findIndex((revision) => revision.generation === selectedGeneration));
  const selected = revisions[selectedIndex] ?? revisions[revisions.length - 1] ?? null;
  const latest = revisions[revisions.length - 1] ?? null;
  const progressPct = revisions.length <= 1 ? 100 : Math.round((selectedIndex / (revisions.length - 1)) * 100);
  const collapseLabel = collapsed
    ? t('plan.detail.dag.evolution.expand')
    : t('plan.detail.dag.evolution.collapse');

  useEffect(() => {
    if (!playing || revisions.length === 0) return;
    if (selectedIndex >= revisions.length - 1) {
      setPlaying(false);
      return;
    }
    const timer = window.setTimeout(() => {
      onSelectGeneration(revisions[selectedIndex + 1].generation);
    }, 900);
    return () => window.clearTimeout(timer);
  }, [playing, revisions, selectedIndex, onSelectGeneration]);

  if (selected == null || latest == null) return null;

  return (
    <>
      <section
        className="mb-2 overflow-hidden rounded-lg border border-border-base bg-bg-elevated shadow-1"
        data-testid="plan-dag-evolution"
        data-collapsed={collapsed ? 'true' : 'false'}
      >
        <div className="flex items-center gap-2 border-b border-border-base px-3 py-2">
          <button
            type="button"
            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full border border-border-base bg-bg-subtle text-text-secondary hover:border-accent hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            aria-label={collapseLabel}
            title={collapseLabel}
            aria-expanded={!collapsed}
            aria-controls={panelBodyId}
            data-testid="plan-dag-evolution-toggle"
            onClick={() => setCollapsed((v) => !v)}
          >
            <DagEvolutionChevron open={!collapsed} />
          </button>
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <h3 className="text-sm font-semibold text-text-primary">{t('plan.detail.dag.evolution.title')}</h3>
              <span
                className="plan-generation plan-generation-badge rounded px-1.5 py-0.5 font-mono text-[0.625rem] font-semibold"
                style={generationStyle(selected.generation)}
                data-testid="plan-dag-evolution-active-revision"
              >
                {selected.label}
              </span>
              <span className="text-[0.6875rem] text-text-muted" data-testid="plan-dag-evolution-progress-label">
                {t('plan.detail.dag.evolution.progress', { current: selected.label, latest: latest.label })}
              </span>
            </div>
            <p title={selected.title} className="mt-0.5 line-clamp-1 min-w-0 break-words text-xs text-text-secondary" data-testid="plan-dag-evolution-summary">
              <span className="font-semibold text-text-primary">{selected.title}</span>
              <span className="text-text-muted">
                {' '}
                · {t('plan.detail.dag.evolution.stageTaskCount', { stages: selected.stageCount, tasks: selected.taskCount })}
              </span>
              {selected.progress && (
                <span className="text-text-muted" data-testid="plan-dag-evolution-generation-progress">
                  {' '}
                  · {t('plan.detail.dag.evolution.generationProgress', { done: selected.progress.done, total: selected.progress.total })}
                </span>
              )}
              {selected.diff && (
                <span className="text-text-muted" data-testid="plan-dag-evolution-diff">
                  {' '}
                  · {evolutionDiffLabel(selected.diff, t)}
                </span>
              )}
              {selected.verdictOutcome && (
                <span className="text-text-muted"> · {selected.verdictOutcome}</span>
              )}
            </p>
          </div>
          <div className="hidden shrink-0 items-center gap-1.5 sm:flex">
            <button
              type="button"
              className="rounded border border-border-base bg-bg-subtle px-2.5 py-1 text-xs font-semibold text-text-secondary hover:border-accent hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              data-testid="plan-dag-evolution-detail-open"
              onClick={() => setDetailOpen(true)}
            >
              {t('plan.detail.dag.evolution.details')}
            </button>
            <button
              type="button"
              className="rounded border border-border-base bg-bg-subtle px-2.5 py-1 text-xs font-semibold text-text-secondary hover:border-accent hover:text-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              data-testid="plan-dag-evolution-current"
              disabled={selected.generation === latest.generation}
              onClick={() => {
                setPlaying(false);
                onSelectGeneration(latest.generation);
              }}
            >
              {t('plan.detail.dag.evolution.current')}
            </button>
            <button
              type="button"
              className="rounded bg-accent px-2.5 py-1 text-xs font-semibold text-white shadow-1 hover:opacity-90 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
              data-testid="plan-dag-evolution-play"
              disabled={revisions.length <= 1}
              onClick={() => {
                if (playing) {
                  setPlaying(false);
                  return;
                }
                if (selected.generation === latest.generation) onSelectGeneration(revisions[0].generation);
                setPlaying(true);
              }}
            >
              {playing ? t('plan.detail.dag.evolution.pause') : t('plan.detail.dag.evolution.play')}
            </button>
          </div>
        </div>

        <p className="px-3 py-1 text-xs text-text-secondary" data-testid="plan-generation-perspective">
          {selected.active
            ? t('plan.detail.dag.currentPerspective', { revision: selected.label })
            : t('plan.detail.dag.snapshotPerspective', { revision: selected.label, time: selected.createdAt ? fullDateTime(selected.createdAt) : t('plan.detail.dag.unknownTime') })}
        </p>
        {!!selected.changes?.length && (
          <div className="space-y-1 px-3 pb-2 text-xs text-text-secondary" data-testid="plan-generation-changes">
            {!selected.diff?.tasks?.length && <p className="font-semibold text-text-primary">{t('plan.detail.dag.noNewTasks', { revision: selected.label })}</p>}
            <div className="flex flex-wrap gap-1.5">
              {selected.changes.map((change) => (
                <span key={change.taskId} title={change.reason} style={generationStyle(selected.generation)}
                  className="plan-generation plan-generation-badge rounded px-2 py-1" data-task-id={change.taskId}>
                  {change.label} · {t(`plan.detail.dag.decision.${change.action}`, { revision: selected.label })}
                </span>
              ))}
            </div>
          </div>
        )}
        <div className="h-1 bg-bg-subtle" aria-hidden="true">
          <span className="block h-full bg-accent transition-[width]" style={{ width: `${progressPct}%` }} />
        </div>

        {!collapsed && (
          <div id={panelBodyId} className="space-y-2 p-2.5" data-testid="plan-dag-evolution-body">
            <div
              className="flex max-h-[5.75rem] gap-2 overflow-x-auto overflow-y-hidden pb-1"
              data-testid="plan-dag-evolution-revisions"
            >
              {revisions.map((revision) => {
                const active = revision.generation === selected.generation;
                return (
                  <button
                    key={revision.generation}
                    type="button"
                    style={{ ...generationStyle(revision.generation), borderTopColor: 'var(--plan-generation-color)' }}
                    className={`plan-generation plan-generation-timeline flex h-20 w-44 shrink-0 flex-col overflow-hidden rounded-md border px-2.5 py-2 text-left transition hover:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${
                      active ? 'border-accent bg-accent/10 shadow-1' : 'border-border-base bg-bg-surface'
                    }`}
                    aria-pressed={active}
                    data-testid={`plan-dag-evolution-revision-${revision.revision}`}
                    data-active={active ? 'true' : 'false'}
                    onClick={() => {
                      setPlaying(false);
                      onSelectGeneration(revision.generation);
                    }}
                  >
                    <div className="flex min-w-0 items-center justify-between gap-2">
                      <span className="shrink-0 font-mono text-xs font-bold text-text-primary">{revision.label}</span>
                      {revision.active && (
                        <span className="rounded bg-success px-1 py-0.5 text-[0.5625rem] font-bold uppercase tracking-wide text-white">
                          {t('plan.detail.dag.evolution.active')}
                        </span>
                      )}
                    </div>
                    <div
                      className="mt-1 line-clamp-1 min-w-0 break-words text-xs font-semibold leading-4 text-text-primary"
                      title={revision.title}
                      data-testid={`plan-dag-evolution-reason-${revision.revision}`}
                    >
                      {revision.title}
                    </div>
                    <div className="mt-auto flex min-w-0 items-center gap-1.5 text-[0.625rem] text-text-muted">
                      <span className="shrink-0 rounded bg-bg-subtle px-1.5 py-0.5">
                        {t('plan.detail.dag.evolution.stageTaskCount', { stages: revision.stageCount, tasks: revision.taskCount })}
                      </span>
                      {revision.progress && (
                        <span className="shrink-0 rounded bg-bg-subtle px-1.5 py-0.5 font-mono" data-testid={`plan-dag-evolution-progress-${revision.revision}`}>
                          {revision.progress.done}/{revision.progress.total}
                        </span>
                      )}
                      {revision.diff && (
                        <span className="min-w-0 truncate rounded bg-bg-subtle px-1.5 py-0.5" data-testid={`plan-dag-evolution-diff-${revision.revision}`}>
                          {evolutionDiffLabel(revision.diff, t)}
                        </span>
                      )}
                    </div>
                  </button>
                );
              })}
            </div>
            <div className="flex gap-1.5 sm:hidden">
              <button
                type="button"
                className="flex-1 rounded border border-border-base bg-bg-subtle px-2.5 py-1.5 text-xs font-semibold text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                data-testid="plan-dag-evolution-detail-open-mobile"
                onClick={() => setDetailOpen(true)}
              >
                {t('plan.detail.dag.evolution.details')}
              </button>
              <button
                type="button"
                className="flex-1 rounded border border-border-base bg-bg-subtle px-2.5 py-1.5 text-xs font-semibold text-text-secondary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
                data-testid="plan-dag-evolution-current-mobile"
                disabled={selected.generation === latest.generation}
                onClick={() => {
                  setPlaying(false);
                  onSelectGeneration(latest.generation);
                }}
              >
                {t('plan.detail.dag.evolution.current')}
              </button>
              <button
                type="button"
                className="flex-1 rounded bg-accent px-2.5 py-1.5 text-xs font-semibold text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
                data-testid="plan-dag-evolution-play-mobile"
                disabled={revisions.length <= 1}
                onClick={() => {
                  if (playing) {
                    setPlaying(false);
                    return;
                  }
                  if (selected.generation === latest.generation) onSelectGeneration(revisions[0].generation);
                  setPlaying(true);
                }}
              >
                {playing ? t('plan.detail.dag.evolution.pause') : t('plan.detail.dag.evolution.play')}
              </button>
            </div>
          </div>
        )}
      </section>

      {detailOpen && (
        <div
          ref={detailDialogRef}
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          role="dialog"
          aria-modal="true"
          aria-labelledby={detailTitleId}
          data-testid="plan-dag-evolution-detail-modal"
          onClick={() => setDetailOpen(false)}
        >
          <div
            className="max-h-[85vh] w-full max-w-2xl overflow-hidden rounded-lg border border-border-base bg-bg-elevated text-text-primary shadow-[var(--shadow-3)]"
            data-testid="plan-dag-evolution-selected-detail"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between gap-3 border-b border-border-base px-4 py-3">
              <div className="min-w-0">
                <h2 id={detailTitleId} className="text-sm font-semibold">
                  {t('plan.detail.dag.evolution.detailTitle', { revision: selected.label })}
                </h2>
                <p className="mt-0.5 line-clamp-1 text-xs text-text-secondary">{selected.title}</p>
              </div>
              <button
                type="button"
                className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                aria-label={t('plan.detail.dag.evolution.closeDetails')}
                data-testid="plan-dag-evolution-detail-close"
                onClick={() => setDetailOpen(false)}
              >
                <IconClose className="h-4 w-4" />
              </button>
            </div>
            <div className="max-h-[calc(85vh-4rem)] space-y-4 overflow-y-auto p-4">
              <div className="flex min-w-0 flex-wrap items-center gap-2">
                <span className="rounded bg-status-blue-bg px-1.5 py-0.5 font-mono text-[0.625rem] font-semibold text-status-blue-fg">
                  {selected.label}
                </span>
                <span className="text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
                  {t('plan.detail.dag.evolution.stageTaskCount', { stages: selected.stageCount, tasks: selected.taskCount })}
                </span>
                {selected.progress && (
                  <span className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.6875rem] text-text-muted">
                    {t('plan.detail.dag.evolution.generationProgress', { done: selected.progress.done, total: selected.progress.total })}
                  </span>
                )}
                {selected.active && (
                  <span className="rounded bg-success px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide text-white">
                    {t('plan.detail.dag.evolution.active')}
                  </span>
                )}
              </div>
              <div
                className="min-w-0 whitespace-pre-wrap break-words text-xs leading-5 text-text-secondary"
                data-testid="plan-dag-evolution-selected-reason"
              >
                <div className="mb-1 font-semibold text-text-primary">{t('plan.detail.dag.evolution.reasonLabel')}</div>
                {selected.reason}
              </div>
              {selected.diff && (
                <div className="text-xs text-text-secondary" data-testid="plan-dag-evolution-selected-diff">
                  <div className="mb-1 font-semibold text-text-primary">{t('plan.detail.dag.evolution.diffLabel')}</div>
                  {evolutionDiffLabel(selected.diff, t)}
                </div>
              )}
              <div className="space-y-1.5 rounded-md border border-border-base bg-bg-subtle/60 p-3 text-[0.6875rem] text-text-muted">
                {selected.generationId && (
                  <div className="truncate font-mono" title={selected.generationId} data-testid="plan-dag-evolution-selected-generation-id">
                    {t('plan.detail.dag.evolution.generationId', { id: selected.generationId })}
                    {selected.parentGenerationId ? ` · ${t('plan.detail.dag.evolution.parentId', { id: selected.parentGenerationId })}` : ''}
                  </div>
                )}
                {selected.idempotencyKey && (
                  <div className="truncate font-mono" title={selected.idempotencyKey}>
                    {t('plan.detail.dag.evolution.idempotency', { key: selected.idempotencyKey })}
                  </div>
                )}
                {selected.verdictId && (
                  <div className="truncate font-mono" title={selected.verdictId}>
                    {selected.verdictOutcome
                      ? t('plan.detail.dag.evolution.verdictWithOutcome', { outcome: selected.verdictOutcome, id: selected.verdictId })
                      : t('plan.detail.dag.evolution.verdict', { id: selected.verdictId })}
                  </div>
                )}
                {selected.continuationId && (
                  <div className="truncate font-mono" title={selected.continuationId}>
                    {t('plan.detail.dag.evolution.continuation', { id: selected.continuationId })}
                  </div>
                )}
                {selected.createdAt && (
                  <div>{formatLocalTime(selected.createdAt)}</div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </>
  );
}

function DagEvolutionChevron({ open }: { open: boolean }): React.ReactElement {
  return (
    <svg
      viewBox="0 0 12 12"
      className={`h-3.5 w-3.5 transition-transform ${open ? 'rotate-90' : ''}`}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      aria-hidden="true"
    >
      <path d="M4.5 2.25 8.25 6 4.5 9.75" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function PlanGraphDag({
  projectId,
  plan,
  graph,
  compact,
  generationRead,
}: {
  projectId: string;
  plan: Plan;
  graph: { nodes: PlanGraphNode[]; edges: PlanGraphEdge[] };
  compact: boolean;
  generationRead?: PlanGenerationRead;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const scale = compact ? 0.7 : 1;
  const [showHistory, setShowHistory] = useState(false);
  const historicalTaskIds = useMemo(() => new Set((plan.nodes ?? []).filter((node) => node.effective === false).map((node) => node.task_id)), [plan.nodes]);
  const graphNodes = useMemo(() => showHistory ? graph.nodes : graph.nodes.filter((node) => !node.task_id || !historicalTaskIds.has(node.task_id)), [graph.nodes, historicalTaskIds, showHistory]);
  const graphEdges = graph.edges;

  // §7: group the canvas by Plan Stage when the plan has any (T981 follow-up).
  // ELK handles flat and staged layouts through the shared React Flow adapter.
  const stagesQuery = usePlanStages(projectId, plan.id);
  const stages = stagesQuery.data ?? [];
  const evolutionRevisions = useMemo(() => buildDagEvolutionRevisions(generationRead, t), [generationRead, t]);
  const currentGeneration = useMemo(
    () => activeGenerationRevision(generationRead) ?? 0,
    [generationRead],
  );
  const [selectedGeneration, setSelectedGeneration] = useState<number | null>(null);
  const selectEvolutionGeneration = useCallback((generation: number) => setSelectedGeneration(generation), []);
  const effectiveGeneration = selectedGeneration ?? currentGeneration;
  const historicalGeneration = effectiveGeneration < currentGeneration
    ? generationAtRevision(generationRead, effectiveGeneration)
    : undefined;
  useEffect(() => {
    if (selectedGeneration == null) return;
    if (!evolutionRevisions.some((revision) => revision.generation === selectedGeneration)) setSelectedGeneration(null);
  }, [evolutionRevisions, selectedGeneration]);
  const visibleStages = useMemo(() => {
    if (historicalGeneration) {
      const taskIDs = new Set(historicalGeneration.snapshot.tasks.map((task) => task.task_id));
      return stages.filter((stage) => (stage.members ?? []).some((member) => taskIDs.has(member.task_id)));
    }
    return stages;
  }, [historicalGeneration, stages]);
  const { nodes, edges } = useMemo(() => {
    const historicalGraph = snapshotPlanGraph(historicalGeneration);
    if (historicalGraph) return historicalGraph;
    if (stages.length === 0 || effectiveGeneration >= currentGeneration) {
      return { nodes: graphNodes, edges: graphEdges };
    }
    const taskIds = stageTaskIdSet(visibleStages);
    if (taskIds.size === 0) return { nodes: graphNodes, edges: graphEdges };
    const controlIds = new Set(visibleStages.map((stage) => stage.gate_node_id).filter(Boolean));
    const filteredNodes = graphNodes.filter((node) => {
      if (node.category === 'business') return node.task_id ? taskIds.has(node.task_id) : false;
      if (node.control_kind === 'start' || node.control_kind === 'end') return true;
      return controlIds.has(node.id);
    });
    const nodeIds = new Set(filteredNodes.map((node) => node.id));
    return {
      nodes: filteredNodes,
      edges: graphEdges.filter((edge) => nodeIds.has(edge.from) && nodeIds.has(edge.to)),
    };
  }, [currentGeneration, effectiveGeneration, graphEdges, graphNodes, historicalGeneration, stages.length, visibleStages]);
  const displayHistoricalTaskIds = useMemo(() => historicalGeneration
    ? new Set(historicalGeneration.snapshot.tasks.filter((task) => task.status === 'discarded').map((task) => task.task_id))
    : historicalTaskIds, [historicalGeneration, historicalTaskIds]);
  const topologyEdges = useMemo(
    () => withHistoricalEdges(nodes, withStageTopologyEdges(nodes, edges, visibleStages), generationRead,
      historicalGeneration?.id ?? generationRead?.active_generation_id, displayHistoricalTaskIds),
    [nodes, edges, visibleStages, generationRead, historicalGeneration, displayHistoricalTaskIds],
  );
  // Stage ids are opaque persistence keys. The mockup uses compact, plan-local
  // S1/S2 refs, which can be derived from the API's stable stage order without
  // changing the read model. Strip the same prefix from legacy stage names so
  // "S1 Data" renders as "STAGE · S1  Data", not "S1  S1 Data".
  const stageDisplay = useMemo(() => stageDisplayMeta(visibleStages), [visibleStages]);

  // Bound task → derived 6-state node_status (for the business-node chip), taken
  // from the plan detail's PlanNode list so the graph chips match the plan view.
  const historicalPlanNodes = useMemo(() => snapshotPlanNodes(historicalGeneration), [historicalGeneration]);
  const nodeStatusOf = useMemo(() => {
    const m = new Map<string, PlanNodeStatus>();
    for (const pn of historicalGeneration ? historicalPlanNodes : (plan.nodes ?? [])) m.set(pn.task_id, pn.task_status === 'discarded' ? 'discarded' : pn.task_status === 'failed' ? 'failed' : pn.node_status);
    return m;
  }, [historicalGeneration, historicalPlanNodes, plan.nodes]);
  const generationNodeOf = useMemo(() => generationNodeMap(generationRead), [generationRead]);
  const generationDecisions = useMemo(() => new Map(
    (generationAtRevision(generationRead, effectiveGeneration)?.diff?.node_decisions ?? []).map((decision) => [decision.task_id, decision]),
  ), [generationRead, effectiveGeneration]);

  const topologyKey = useMemo(
    () => [
      nodes.map((node) => `${node.id}:${node.follows_task_id ?? ''}`).sort().join(','),
      topologyEdges.map((edge) => `${edge.from}->${edge.to}:${edge.kind}:${edge.historical ?? false}`).sort().join(','),
      visibleStages.map((stage) => `${stage.id}:${stage.members.map((member) => member.task_id).join('.')}:${stage.depends_on_stages.join('.')}`).join(','),
    ].join('|'),
    [nodes, topologyEdges, visibleStages],
  );
  const { layout: flowLayout, loading: flowLoading } = useElkFlowLayout(
    () => layoutGraphFlow(nodes, topologyEdges, visibleStages),
    [topologyKey],
  );
  const flowNodeUi = useMemo<PlanFlowNodeUi>(() => ({
    projectId,
    nodeStatusOf,
    generationNodeOf,
    stageDisplay,
    historicalTaskIds: historicalGeneration ? new Set(historicalPlanNodes.filter((node) => !node.effective).map((node) => node.task_id)) : historicalTaskIds,
    selectedRevision: effectiveGeneration,
    generationDecisions,
  }), [generationNodeOf, nodeStatusOf, projectId, stageDisplay, historicalGeneration, historicalTaskIds, historicalPlanNodes, effectiveGeneration, generationDecisions]);
  const currentFlowNodes = useMemo(
    () => (flowLayout ? refreshGraphFlowNodes(flowLayout.nodes, nodes, visibleStages) : []),
    [flowLayout, nodes, visibleStages],
  );
  const flowNodes = useMemo(
    () => withNodeUi(currentFlowNodes, flowNodeUi),
    [currentFlowNodes, flowNodeUi],
  );
  const flowEdges = useMemo(
    () => (flowLayout ? withEdgeUi(flowLayout.edges, {}) : []),
    [flowLayout],
  );
  const flowBusinessNodes = currentFlowNodes.filter((
    node,
  ): node is PlanDagFlowNode & { data: Extract<PlanDagFlowData, { kind: 'business' }> | Extract<PlanDagFlowData, { kind: 'control' }> } =>
    node.data.kind === 'business' || node.data.kind === 'control');

  return (
    <SenderSidebarProvider>
      <div data-testid={flowLoading && !flowLayout ? 'plan-dag-layout-loading' : 'plan-dag'} data-graph="true" className="md:flex md:min-h-0 md:flex-1 md:flex-col">
        <DagEvolutionPanel
          revisions={evolutionRevisions}
          selectedGeneration={effectiveGeneration}
          onSelectGeneration={selectEvolutionGeneration}
        />
        {!historicalGeneration && historicalTaskIds.size > 0 && (
          <button type="button" role="switch" aria-checked={showHistory} onClick={() => setShowHistory((show) => !show)} data-testid="plan-dag-show-history" className={`mb-2 flex items-center gap-2 self-start rounded border px-2 py-1 text-xs ${showHistory ? 'border-accent bg-accent text-white' : 'border-border-strong text-text-secondary'}`}>
            {t('plan.detail.dag.showHistory', { count: historicalTaskIds.size })}
          </button>
        )}
        <MobileStageGateAudits stages={visibleStages} error={stagesQuery.isError} />
        {/* Mobile: a simple ordered list of nodes by flow level. */}
        <ol className="mt-1 space-y-1.5 md:hidden" data-testid="plan-graph-stepper">
          {flowBusinessNodes
            .slice()
            .sort((p, q) => p.position.y - q.position.y || p.position.x - q.position.x)
            .map((p) => (
              <li
                key={p.id}
                className="rounded-lg border border-border-base bg-bg-elevated p-2 text-xs"
                data-node-category={p.data.kind === 'business' ? p.data.node.category : 'control'}
                data-control-kind={p.data.kind === 'control' ? p.data.node.control_kind : undefined}
              >
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold text-text-primary">
                    {p.data.kind === 'control'
                      ? p.data.node.title
                      : p.data.node.title || refLabel(p.data.node.org_ref, p.data.node.task_id ?? p.data.node.id)}
                  </span>
                  {p.data.kind === 'business' && p.data.node.task_id ? (
                    <span className="inline-flex items-center gap-1">
                      <NodeGenerationBadge
                        node={{
                          revision: generationNodeOf.get(p.data.node.task_id)?.revision,
                        }}
                      />
                      <NodeStateChip status={nodeStatusOf.get(p.data.node.task_id) ?? 'blocked'} />
                    </span>
                  ) : (
                    <span className="rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary">
                      {p.data.kind === 'control' ? p.data.node.control_kind : ''}
                    </span>
                  )}
                </div>
              </li>
            ))}
        </ol>

        {flowLoading && !flowLayout ? (
          <div className="hidden rounded-lg border border-border-base bg-bg-subtle py-10 text-center text-xs text-text-muted md:block" data-testid="plan-dag-layout-loading">
            {t('plan.detail.dag.layoutLoading', { defaultValue: 'Laying out graph…' })}
          </div>
        ) : (
        <PlanFlowCanvas
          nodes={flowNodes}
          edges={flowEdges}
          compact={compact}
          topologyKey={topologyKey}
          legend={
            <div className="contents" data-testid="plan-graph-legend">
              <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-muted"><span className="h-0.5 w-4 bg-border-strong" />{t('plan.detail.dag.edgeSeq', { defaultValue: 'seq' })}</span>
              <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-muted"><span className="h-0.5 w-4 bg-accent" />{t('plan.detail.dag.edgeConditional', { defaultValue: 'conditional' })}</span>
              <span className="inline-flex items-center gap-1.5 text-[0.6875rem] text-text-muted"><span className="h-0.5 w-4 border-t-2 border-dashed border-status-amber-border" />{t('plan.detail.dag.edgeLoopback', { defaultValue: 'loopback' })}</span>
            </div>
          }
        >
          <div data-testid="plan-dag-scaler" className="hidden" data-scale={scale} style={{ transform: scale === 1 ? undefined : `scale(${scale})` }} />
        </PlanFlowCanvas>
        )}
      </div>
    </SenderSidebarProvider>
  );
}

// T769: when the plan carries a real orchestration graph (built by T768 on
// start), render THAT graph — control nodes (Start/End/Condition) + edges by
// kind — so the DAG reflects the engine. A plan with NO graph (pending /
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

function StageBoxSurface({
  stage,
  className,
  style,
  children,
  'data-testid': testId,
}: {
  stage: PlanStage;
  className: string;
  style?: React.CSSProperties;
  children: React.ReactNode;
  'data-testid': string;
}) {
  const { t } = useTranslation('work');
  const [open, setOpen] = useState(false);
  const title = t('plan.detail.stages.auditTitle', { defaultValue: 'Stage gate details' });
  const label = t('plan.detail.stages.auditOpenAria', {
    stage: stage.name || stage.id,
    defaultValue: 'Open stage gate details for {{stage}}',
  });
  return (
    <>
      <div
        className={`${className} cursor-pointer transition-colors hover:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent`}
        style={style}
        role="button"
        tabIndex={0}
        aria-label={label}
        title={title}
        data-testid={testId}
        onClick={() => setOpen(true)}
        onKeyDown={(e) => {
          if (e.key !== 'Enter' && e.key !== ' ') return;
          e.preventDefault();
          setOpen(true);
        }}
      >
        {children}
      </div>
      <StageAuditDialog
        open={open}
        onClose={() => setOpen(false)}
        stage={stage}
        prefix="plan-stage-gate"
        title={title}
      />
    </>
  );
}

function StageHeaderDetailsTarget({
  stage,
  style,
  testId,
}: {
  stage: PlanStage;
  style: React.CSSProperties;
  testId: string;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const [open, setOpen] = useState(false);
  const title = t('plan.detail.stages.auditTitle', { defaultValue: 'Stage gate details' });
  const label = t('plan.detail.stages.auditOpenAria', {
    stage: stage.name || stage.id,
    defaultValue: 'Open stage gate details for {{stage}}',
  });
  return (
    <>
      <button
        type="button"
        className="absolute z-20 cursor-pointer rounded-t-xl border-0 bg-transparent p-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        style={style}
        aria-label={label}
        title={title}
        data-testid={testId}
        onClick={(event) => {
          event.stopPropagation();
          setOpen(true);
        }}
      >
        <span className="sr-only">{label}</span>
      </button>
      <StageAuditDialog
        open={open}
        onClose={() => setOpen(false)}
        stage={stage}
        prefix="plan-stage-gate"
        title={title}
      />
    </>
  );
}

function MobileStageAuditCard({ stage }: { stage: PlanStage }) {
  const { t } = useTranslation('work');
  const [open, setOpen] = useState(false);
  const title = t('plan.detail.stages.auditTitle', { defaultValue: 'Stage gate details' });
  const label = t('plan.detail.stages.auditOpenAria', {
    stage: stage.name || stage.id,
    defaultValue: 'Open stage gate details for {{stage}}',
  });
  return (
    <>
      <section
        className="cursor-pointer rounded-lg border border-border-strong bg-bg-surface p-3 transition-colors hover:border-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        role="button"
        tabIndex={0}
        aria-label={label}
        title={title}
        data-testid={`plan-stage-mobile-audit-${stage.id}`}
        onClick={() => setOpen(true)}
        onKeyDown={(e) => {
          if (e.key !== 'Enter' && e.key !== ' ') return;
          e.preventDefault();
          setOpen(true);
        }}
      >
        <div className="flex items-center justify-between gap-2 text-xs font-semibold text-text-primary">
          <span>{stage.name}</span>
          <span>{stage.status}</span>
        </div>
      </section>
      <StageAuditDialog
        open={open}
        onClose={() => setOpen(false)}
        stage={stage}
        prefix="plan-stage-mobile-gate"
        title={title}
      />
    </>
  );
}

function StageAuditDialog({
  open,
  onClose,
  stage,
  prefix,
  title,
}: {
  open: boolean;
  onClose: () => void;
  stage: PlanStage;
  prefix: string;
  title: string;
}) {
  const id = React.useId();
  const containerRef = useModalA11y({ open, onClose });
  if (!open) return null;
  return (
    <div
      ref={containerRef}
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby={id}
      data-testid={`${prefix}-audit-dialog-${stage.id}`}
      onClick={onClose}
    >
      <div
        className="max-h-[85vh] w-full max-w-2xl overflow-hidden rounded-lg border border-border-base bg-bg-elevated text-text-primary shadow-[var(--shadow-3)]"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between gap-3 border-b border-border-base px-4 py-3">
          <h2 id={id} className="text-sm font-semibold">
            {title}
          </h2>
          <button
            type="button"
            className="inline-flex h-8 w-8 items-center justify-center rounded-full text-text-muted hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            aria-label="Close"
            data-testid={`${prefix}-audit-dialog-close-${stage.id}`}
            onClick={onClose}
          >
            <IconClose className="h-4 w-4" />
          </button>
        </div>
        <StageAuditDetails stage={stage} prefix={prefix} />
      </div>
    </div>
  );
}

function StageAuditDetails({ stage, prefix }: { stage: PlanStage; prefix: string }) {
  const spec = stage.gate_spec;
  const owner = spec?.assignee_ref || spec?.role_ref || 'Unassigned';
  const evidence = stage.gate_evidence || 'No evidence';
  const diagnostics = stage.diagnostics ?? [];
  const diagnosticsText = diagnostics.length ? diagnostics.map((d) => `${d.code}: ${d.message}`).join('\n') : 'No diagnostics';
  const tone = stageGateOutcomeTone(stage.gate_outcome);
  const reviewedSha = stage.gate_reviewed_sha ? stage.gate_reviewed_sha.slice(0, 12) : 'No reviewed SHA';
  return (
    <div className="max-h-[70vh] overflow-y-auto p-4 text-sm" data-testid={`${prefix}-audit-details-${stage.id}`}>
      <div className={`mb-3 rounded-lg border p-3 ${tone.bannerClass}`} data-testid={`${prefix}-outcome-banner-${stage.id}`}>
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <span className={`rounded px-2 py-1 text-xs font-bold uppercase tracking-wide ${tone.chipClass}`} data-testid={`${prefix}-outcome-chip-${stage.id}`}>
            {tone.label}
          </span>
          <span className="min-w-0 break-words text-sm font-semibold text-text-primary">{stage.name || stage.id}</span>
          <span className="rounded bg-bg-elevated/70 px-2 py-1 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
            {stage.status}
          </span>
        </div>
        <p className="mt-2 min-w-0 break-words text-xs leading-5 text-text-secondary">{tone.summary}</p>
        <div className="mt-2 flex min-w-0 flex-wrap gap-1.5 text-[0.6875rem]">
          <span className="rounded bg-bg-elevated/70 px-2 py-1 font-mono text-text-secondary">{reviewedSha}</span>
          <span className="rounded bg-bg-elevated/70 px-2 py-1 text-text-secondary">round {stage.rounds}/{stage.max_rounds}</span>
          <span className="rounded bg-bg-elevated/70 px-2 py-1 text-text-secondary">{stage.members.length} tasks</span>
          {diagnostics.length > 0 && (
            <span className="rounded bg-danger/10 px-2 py-1 font-semibold text-danger">
              {diagnostics.length} diagnostic{diagnostics.length === 1 ? '' : 's'}
            </span>
          )}
        </div>
      </div>
      <dl className="grid gap-3">
        <div className="grid gap-3 md:grid-cols-2">
          <StageAuditDetailRow label="Evaluator" testId={`${prefix}-evaluator-${stage.id}`}>
            {spec?.evaluator_kind || 'Missing evaluator'} · <ActivityRefText text={owner} />
          </StageAuditDetailRow>
          <StageAuditDetailRow label="Lineage" testId={`${prefix}-lineage-${stage.id}`}>
            {stage.origin_verdict_id
              ? `generation ${stage.generation ?? 0} · verdict ${stage.origin_verdict_id} · continuation ${stage.continuation_id ?? 'unknown'}`
              : 'Base stage'}
          </StageAuditDetailRow>
        </div>
        <StageAuditDetailRow label="Routes" testId={`${prefix}-routes-${stage.id}`}>
          {spec ? (
            <div className="flex min-w-0 flex-wrap gap-1.5">
              <span className="sr-only">{`${spec.pass_route} / ${spec.reject_route} / ${spec.exhausted_route}`}</span>
              <StageAuditRouteChip label="Pass" value={spec.pass_route} className="border-success/30 bg-success/10 text-success" />
              <StageAuditRouteChip label="Reject" value={spec.reject_route} className="border-warning/30 bg-warning/10 text-warning" />
              <StageAuditRouteChip label="Exhausted" value={spec.exhausted_route} className="border-danger/30 bg-danger/10 text-danger" />
            </div>
          ) : 'Routes unavailable'}
        </StageAuditDetailRow>
        <StageAuditDetailRow label="Topology fingerprint" testId={`${prefix}-fingerprint-${stage.id}`}>
          <span className="font-mono text-xs">{stage.topology_fingerprint || 'Not recorded'}</span>
        </StageAuditDetailRow>
        <StageAuditDetailRow label="Acceptance contract" testId={`${prefix}-contract-${stage.id}`} strong>
          <ActivityRefText text={spec?.acceptance_contract || 'Missing acceptance contract'} />
        </StageAuditDetailRow>
        <StageAuditDetailRow label="Evidence" testId={`${prefix}-evidence-${stage.id}`} tone={stage.gate_outcome === 'reject' ? 'danger' : stage.gate_outcome === 'pass' ? 'success' : undefined}>
          <div className="mb-2 flex min-w-0 flex-wrap gap-1.5 text-xs">
            <span className={`rounded px-2 py-1 font-semibold uppercase ${tone.chipClass}`}>{stage.gate_outcome || 'Outcome pending'}</span>
            <span className="rounded bg-bg-subtle px-2 py-1 font-mono text-text-secondary">{reviewedSha}</span>
          </div>
          <div className="whitespace-pre-wrap break-words leading-5" data-testid={`${prefix}-evidence-text-${stage.id}`}>
            <ActivityRefText text={evidence} />
          </div>
        </StageAuditDetailRow>
        <StageAuditDetailRow label="Diagnostics" testId={`${prefix}-diagnostics-${stage.id}`} tone={diagnostics.length ? 'danger' : undefined}>
          <ActivityRefText text={diagnosticsText} />
        </StageAuditDetailRow>
      </dl>
    </div>
  );
}

function stageGateOutcomeTone(outcome: PlanStage['gate_outcome'] | undefined): {
  label: string;
  summary: string;
  bannerClass: string;
  chipClass: string;
} {
  if (outcome === 'pass') {
    return {
      label: 'Passed',
      summary: 'The stage gate accepted the current evidence and can continue through the pass route.',
      bannerClass: 'border-success/30 bg-success/10',
      chipClass: 'bg-success text-white',
    };
  }
  if (outcome === 'reject') {
    return {
      label: 'Rejected',
      summary: 'The stage gate rejected the submitted evidence. Review the contract, evidence, diagnostics, and reject route before continuing.',
      bannerClass: 'border-danger/30 bg-danger/10',
      chipClass: 'bg-danger text-white',
    };
  }
  return {
    label: 'Pending',
    summary: 'No gate verdict has been recorded yet. The evaluator, routes, and acceptance contract are shown for review.',
    bannerClass: 'border-border-base bg-bg-subtle',
    chipClass: 'bg-bg-elevated text-text-secondary',
  };
}

function StageAuditRouteChip({ label, value, className }: { label: string; value: string; className: string }) {
  return (
    <span className={`min-w-0 max-w-full truncate rounded border px-2 py-1 text-[0.6875rem] font-semibold ${className}`}>
      <span className="uppercase tracking-wide">{label}</span>
      <span className="mx-1 text-text-muted">→</span>
      <span className="font-mono">{value}</span>
    </span>
  );
}

function StageAuditDetailRow({
  label,
  testId,
  children,
  strong = false,
  tone,
}: {
  label: string;
  testId: string;
  children: React.ReactNode;
  strong?: boolean;
  tone?: 'danger' | 'success';
}) {
  const toneClass = tone === 'danger'
    ? 'border-danger/30 bg-danger/5'
    : tone === 'success'
      ? 'border-success/30 bg-success/5'
      : 'border-border-base bg-bg-surface';
  const textClass = strong ? 'text-text-primary' : tone === 'danger' ? 'text-danger' : 'text-text-secondary';
  return (
    <div className={`grid gap-1 rounded-lg border p-3 ${toneClass}`}>
      <dt className="text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">{label}</dt>
      <dd
        className={`${textClass} min-w-0 whitespace-pre-wrap break-words`}
        data-testid={testId}
      >
        {children}
      </dd>
    </div>
  );
}

function MobileStageGateAudits({ stages, error }: { stages: PlanStage[]; error: boolean }) {
  return (
    <div className="mb-3 grid gap-2 md:hidden" data-testid="plan-stage-mobile-audits">
      {error && (
        <div className="rounded border border-danger bg-bg-elevated p-2 text-xs text-danger" role="alert" data-testid="plan-stage-mobile-error">
          Stage gate audit data could not be loaded.
        </div>
      )}
      {!error && stages.length === 0 && (
        <div className="rounded border border-border-base bg-bg-elevated p-2 text-xs text-text-muted" data-testid="plan-stage-mobile-empty">
          No stage gate audit data.
        </div>
      )}
      {stages.map((stage) => <MobileStageAuditCard key={stage.id} stage={stage} />)}
    </div>
  );
}

// stageMemberDone mirrors the backend taskToStageMemberState "done" bucket (§4.1):
// a member counts as done once its task is terminal (completed or discarded).
function stageMemberDone(status: PlanStage['members'][number]['task_status']): boolean {
  return status === 'completed' || status === 'discarded';
}

type PlanFlowNodeUi = {
  projectId: string;
  nodeStatusOf?: Map<string, PlanNodeStatus>;
  generationNodeOf?: Map<string, PlanGenerationRead['nodes'][number]>;
  generationDecisions?: Map<string, PlanGenerationDiff['node_decisions'][number]>;
  canEditDependencies?: boolean;
  connectFrom?: string | null;
  dropTargets?: Set<string>;
  titleOf?: (taskId: string) => string;
  onStartConnect?: (taskId: string) => void;
  onTargetActivate?: (taskId: string) => void;
  stageDisplay?: ReturnType<typeof stageDisplayMeta>;
  historicalTaskIds?: Set<string>;
  selectedRevision?: number;
};

type PlanFlowEdgeUi = {
  canEditDependencies?: boolean;
  isPending?: boolean;
  titleOf?: (taskId: string) => string;
  onRemove?: (fromTaskId: string, toTaskId: string) => void;
};

function useElkFlowLayout(
  build: () => Promise<PlanDagFlowLayout>,
  deps: React.DependencyList,
): { layout: PlanDagFlowLayout | null; loading: boolean } {
  const requestRef = useRef(0);
  const [layout, setLayout] = useState<PlanDagFlowLayout | null>(null);
  const [loading, setLoading] = useState(true);
  useEffect(() => {
    let cancelled = false;
    const requestId = ++requestRef.current;
    setLoading(true);
    build()
      .then((next) => {
        if (cancelled || requestId !== requestRef.current) return;
        setLayout(next);
        setLoading(false);
      })
      .catch(() => {
        if (cancelled || requestId !== requestRef.current) return;
        setLayout({ nodes: [], edges: [], width: 0, height: 0 });
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return { layout, loading };
}

function withNodeUi(nodes: PlanDagFlowNode[], ui: PlanFlowNodeUi): PlanDagFlowNode[] {
  return nodes.map((node) => ({ ...node, data: { ...node.data, ui } as PlanDagFlowData & { ui: PlanFlowNodeUi } }));
}

function withEdgeUi(edges: PlanDagFlowEdge[], ui: PlanFlowEdgeUi): PlanDagFlowEdge[] {
  return edges.map((edge) => ({
    ...edge,
    type: 'plan',
    data: { ...(edge.data ?? { kind: 'seq' }), ui } as NonNullable<PlanDagFlowEdge['data']> & { ui: PlanFlowEdgeUi },
  }));
}

function PlanFlowFitView({ topologyKey, nodes }: { topologyKey: string; nodes: PlanDagFlowNode[] }) {
  const { setViewport, setCenter } = useReactFlow();
  const { t } = useTranslation('work');
  const width = useStore((state) => state.width);
  const height = useStore((state) => state.height);
  const fitted = useRef<string | null>(null);
  useEffect(() => {
    if (!width || !height || !nodes.length) return;
    const key = `${topologyKey}:${width}:${height}`;
    if (fitted.current === key) return;
    // ELK has already sized every card and stage. Use the new layout directly;
    // React Flow's measured internals can still describe the previous layout.
    const bounds = getNodesBounds(nodes.filter((node) => !node.parentId));
    fitted.current = key;
    void setViewport(getViewportForBounds(bounds, width, height, 0.05, 1.5, 0.12), { duration: 180 });
  }, [setViewport, topologyKey, nodes, width, height]);
  const overview = () => setViewport(getViewportForBounds(getNodesBounds(nodes.filter((node) => !node.parentId)), width, height, 0.05, 1.5, 0.12), { duration: 180 });
  const readable = () => {
    const tasks = nodes.filter((node) => node.data.kind === 'business' || node.data.kind === 'legacy');
    const selected = tasks.filter((node) => {
      const ui = (node.data as PlanDagFlowData & { ui?: PlanFlowNodeUi }).ui;
      const taskId = node.data.kind === 'business' || node.data.kind === 'legacy' ? node.data.node.task_id : undefined;
      return !!taskId && ui?.selectedRevision != null && (ui.generationDecisions?.has(taskId) || ui.generationNodeOf?.get(taskId)?.revision === ui.selectedRevision);
    });
    const target = (selected.length ? selected : tasks).slice().sort((a, b) => a.position.y - b.position.y)[0];
    if (!target) return;
    const parent = nodes.find((node) => node.id === target.parentId);
    void setCenter(target.position.x + (parent?.position.x ?? 0) + (target.width ?? PLAN_DAG_NODE_W) / 2, target.position.y + (parent?.position.y ?? 0) + (target.height ?? PLAN_DAG_NODE_H) / 2, { zoom: 1, duration: 180 });
  };
  return <Panel position="top-left" className="flex gap-1 rounded bg-bg-elevated p-1 shadow-1">
    <button type="button" onClick={() => void overview()} className="rounded border border-border-strong px-2 py-1 text-xs text-text-primary" data-testid="plan-dag-overview">{t('plan.detail.dag.overview')}</button>
    <button type="button" onClick={readable} className="rounded border border-border-strong px-2 py-1 text-xs text-text-primary" data-testid="plan-dag-readable">{t('plan.detail.dag.readable')}</button>
  </Panel>;
}

function PlanFlowCanvas({
  nodes,
  edges,
  compact,
  topologyKey,
  legend,
  children,
}: {
  nodes: PlanDagFlowNode[];
  edges: PlanDagFlowEdge[];
  compact: boolean;
  topologyKey: string;
  legend: React.ReactNode;
  children?: React.ReactNode;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const nodeTypes = useMemo(() => ({
    stage: PlanFlowStageNode,
    business: PlanFlowBusinessNode,
    legacy: PlanFlowLegacyNode,
    control: PlanFlowControlNode,
  }), []);
  const edgeTypes = useMemo(() => ({ plan: PlanFlowEdge }), []);
  const [showMiniMap, setShowMiniMap] = useState(false);
  const displayEdges = useMemo(() => edges.map((edge) => {
    if (edge.data?.kind !== 'lineage') return edge;
    const target = nodes.find((node) => node.id === edge.target);
    const ui = (target?.data as (PlanDagFlowData & { ui?: PlanFlowNodeUi }) | undefined)?.ui;
    const taskId = target?.data.kind === 'business' || target?.data.kind === 'legacy' ? target.data.node.task_id : undefined;
    const revision = taskId ? ui?.generationNodeOf?.get(taskId)?.revision : undefined;
    return revision == null ? edge : { ...edge, className: `${edge.className ?? ''} plan-generation`, style: { ...edge.style, ...generationStyle(revision), stroke: 'var(--plan-generation-color)' } };
  }), [edges, nodes]);
  const onCanvasMouseDown = useCallback((event: React.MouseEvent<HTMLDivElement>) => {
    if (event.button !== 0) return;
    const target = event.target as HTMLElement | null;
    if (target?.closest('a,button,input,textarea,select,[role="button"]')) return;
    const canvas = event.currentTarget;
    const startX = event.clientX;
    const startY = event.clientY;
    const startLeft = canvas.scrollLeft;
    const startTop = canvas.scrollTop;
    const onMove = (move: MouseEvent) => {
      canvas.scrollLeft = startLeft - (move.clientX - startX);
      canvas.scrollTop = startTop - (move.clientY - startY);
    };
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, []);
  return (
    <div className="relative hidden overflow-hidden rounded-lg border border-border-base md:flex md:min-h-0 md:flex-1 md:flex-col" data-testid="plan-dag-canvas-shell">
      <div
        className="relative min-h-0 flex-1 cursor-grab bg-bg-subtle"
        data-testid="plan-dag-canvas"
        data-compact={compact ? 'true' : 'false'}
        onMouseDown={onCanvasMouseDown}
      >
        <ReactFlowProvider>
          <ReactFlow
            className="plan-flow"
            nodes={nodes}
            edges={displayEdges}
            nodeTypes={nodeTypes}
            edgeTypes={edgeTypes}
            nodesDraggable={false}
            nodesConnectable={false}
            elementsSelectable={false}
            panOnDrag
            zoomOnScroll
            zoomOnPinch
            minZoom={0.05}
            maxZoom={1.5}
            fitView
            data-testid="plan-dag-reactflow"
          >
            <Background color="var(--color-border)" gap={24} size={1} />
            <Controls position="top-right" showInteractive={false} fitViewOptions={{ padding: 0.18 }} />
            {showMiniMap && <MiniMap pannable zoomable position="bottom-right" nodeStrokeWidth={2} style={{ width: 140, height: 90 }} />}
            <PlanFlowFitView nodes={nodes} topologyKey={`${topologyKey}:${compact}:${nodes.map((node) => `${node.id}:${node.position.x}:${node.position.y}:${node.width}:${node.height}`).join('|')}`} />
            {children}
          </ReactFlow>
          <svg className="hidden" aria-hidden="true" data-testid="plan-dag-svg">
            <defs>
              <marker id="plan-dag-arrow" />
            </defs>
          </svg>
          <div className="hidden" aria-hidden="true" data-testid="plan-dag-semantic-edges">
            {edges.map((edge) => {
              const kind = edge.data?.kind;
              const edgeKey = (edge.id.includes(':') ? edge.id.split(':')[1] : edge.id)
                .replaceAll('__legacy_start__', 'start')
                .replaceAll('__legacy_end__', 'end');
              const testId = kind === 'synthetic'
                ? 'plan-dag-synthetic-edge'
                : edge.data?.fromTaskId && edge.data?.toTaskId
                  ? 'plan-dag-edge'
                  : 'plan-graph-edge';
              const ui = (edge.data as NonNullable<PlanDagFlowEdge['data']> & { ui?: PlanFlowEdgeUi } | undefined)?.ui;
              return (
                <span key={edge.id} data-testid={testId} data-edge={edgeKey} data-edge-kind={kind}>
                  {!edge.data?.historical && ui?.canEditDependencies && edge.data?.fromTaskId && edge.data?.toTaskId && (
                    <button
                      type="button"
                      data-testid="plan-edge-delete"
                      data-edge={`${edge.data.fromTaskId}->${edge.data.toTaskId}`}
                      disabled={ui.isPending}
                      onClick={() => ui.onRemove?.(edge.data!.fromTaskId!, edge.data!.toTaskId!)}
                      aria-label={ui.titleOf
                        ? `Remove dependency: ${ui.titleOf(edge.data.fromTaskId)} depends on ${ui.titleOf(edge.data.toTaskId)}`
                        : 'Remove dependency'}
                      className="border-border-strong bg-bg-elevated text-text-secondary"
                    >
                      &times;
                    </button>
                  )}
                </span>
              );
            })}
          </div>
        </ReactFlowProvider>
      </div>
      <div className="flex shrink-0 flex-wrap items-center gap-4 border-t border-border-base bg-bg-elevated px-3.5 py-2" data-testid="plan-dag-canvas-legend-bar">
        {legend}
        {[...new Set(nodes.flatMap((node) => {
          const ui = (node.data as PlanDagFlowData & { ui?: PlanFlowNodeUi }).ui;
          const taskId = node.data.kind === 'business' || node.data.kind === 'legacy' ? node.data.node.task_id : undefined;
          const revision = taskId ? ui?.generationNodeOf?.get(taskId)?.revision : undefined;
          return revision == null ? [] : [revision];
        }))].sort((a, b) => a - b).map((revision) => <NodeGenerationBadge key={revision} node={{ revision }} testId="plan-generation-legend" />)}
        <span className="text-[0.6875rem] text-text-secondary">{t('plan.detail.dag.generationSemantics')}</span>
        <span className="text-[0.6875rem] text-text-secondary">{t('plan.detail.dag.displayEdges')}</span>
        <button type="button" aria-pressed={showMiniMap} onClick={() => setShowMiniMap((show) => !show)} className="ml-auto rounded border border-border-strong px-2 py-1 text-xs text-text-primary" data-testid="plan-dag-minimap-toggle">{t('plan.detail.dag.minimap')}</button>
        <span className="sr-only">{t('plan.detail.dag.reactFlowCanvas', { defaultValue: 'Interactive plan graph canvas' })}</span>
      </div>
    </div>
  );
}

function PlanFlowStageNode({ data }: NodeProps<PlanDagFlowNode>): React.ReactElement | null {
  if (data.kind !== 'stage') return null;
  const { t } = useTranslation('work');
  const totalMembers = data.stage.members.length;
  const doneMembers = data.stage.members.filter((member) => stageMemberDone(member.task_status)).length;
  const pct = totalMembers > 0 ? Math.round((doneMembers / totalMembers) * 100) : 0;
  const display = (data as PlanDagFlowData & { ui?: PlanFlowNodeUi }).ui?.stageDisplay?.byStageId.get(data.stage.id) ?? { ref: data.stage.id, name: data.stage.name };
  return (
    <StageBoxSurface className="h-full w-full rounded-xl border border-border-strong bg-bg-surface" stage={data.stage} data-testid={`plan-stage-box-${data.stage.id}`}>
      <div className="border-b border-border-base px-3.5 py-2">
        <div className="flex items-baseline gap-1.5">
          <span className="font-mono text-[0.625rem] tracking-wide text-text-muted" data-testid={`plan-stage-ref-${data.stage.id}`}>{t('plan.detail.stages.idLabel', { defaultValue: 'STAGE' })} · {display.ref}</span>
          <span className="truncate text-xs font-semibold text-text-primary" data-testid={`plan-stage-name-${data.stage.id}`}>{display.name}</span>
        </div>
        <div className="mt-1 flex items-center gap-2.5">
          <span data-testid={`plan-stage-status-${data.stage.id}`} className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.5625rem] font-bold uppercase tracking-wide ${STAGE_STATUS_CLASS[data.stage.status]}`}>
            <span className="h-1 w-1 rounded-full bg-current" aria-hidden="true" />
            {t(`plan.detail.stages.status.${data.stage.status}`)}
          </span>
          {data.stage.rounds > 0 && (
            <span data-testid={`plan-stage-rounds-${data.stage.id}`} className="rounded bg-warning px-1.5 py-0.5 text-[0.5625rem] font-bold text-white">
              {data.stage.rounds}/{data.stage.max_rounds}
            </span>
          )}
          <span className="h-1 max-w-[7rem] flex-1 overflow-hidden rounded-full bg-bg-subtle" aria-hidden="true">
            <span className="block h-full rounded-full bg-success" style={{ width: `${pct}%` }} />
          </span>
          <span className="font-mono text-[0.5625rem] text-text-muted" data-testid={`plan-stage-progress-${data.stage.id}`}>{doneMembers}/{totalMembers}</span>
        </div>
      </div>
      <StageHeaderDetailsTarget
        stage={data.stage}
        style={{ left: 0, top: 0, width: '100%', height: PLAN_DAG_STAGE_HEADER_H }}
        testId={`plan-stage-header-button-${data.stage.id}`}
      />
    </StageBoxSurface>
  );
}

function PlanFlowControlNode({ data }: NodeProps<PlanDagFlowNode>): React.ReactElement | null {
  if (data.kind !== 'control') return null;
  const ui = (data as PlanDagFlowData & { ui?: PlanFlowNodeUi }).ui;
  const legacyAnchor = data.node.id.startsWith('__legacy_') && (data.node.control_kind === 'start' || data.node.control_kind === 'end')
    ? `plan-dag-synthetic-${data.node.control_kind}`
    : undefined;
  return (
    <>
      <PlanFlowNodeHandles />
      <ControlNodeMarker node={data.node} gateStageRef={ui?.stageDisplay?.byGateNodeId.get(data.node.id)} testId={legacyAnchor} />
    </>
  );
}

function PlanFlowBusinessNode({ data }: NodeProps<PlanDagFlowNode>): React.ReactElement | null {
  if (data.kind !== 'business') return null;
  const ui = (data as PlanDagFlowData & { ui?: PlanFlowNodeUi }).ui;
  const taskId = data.node.task_id ?? data.node.id;
  const status = ui?.nodeStatusOf?.get(taskId) ?? 'blocked';
  return (
    <PlanFlowTaskCard
      projectId={ui?.projectId ?? ''}
      nodeId={data.node.id}
      taskId={taskId}
      orgRef={data.node.org_ref}
      title={data.node.title || refLabel(data.node.org_ref, taskId)}
      assigneeRef={data.node.assignee_ref ?? ''}
      status={status}
      selectedRevision={ui?.selectedRevision}
      generationDecision={ui?.generationDecisions?.get(taskId)}
      generationNode={ui?.generationNodeOf?.get(taskId)}
      historical={ui?.historicalTaskIds?.has(taskId)}
      testId="plan-graph-node"
      taskIdTestId="plan-graph-node-taskid"
    />
  );
}

function PlanFlowLegacyNode({ data }: NodeProps<PlanDagFlowNode>): React.ReactElement | null {
  if (data.kind !== 'legacy') return null;
  const ui = (data as PlanDagFlowData & { ui?: PlanFlowNodeUi }).ui;
  const taskId = data.node.task_id;
  const inConnect = ui?.connectFrom != null;
  const isSource = ui?.connectFrom === taskId;
  const isTarget = Boolean(inConnect && !isSource && ui?.dropTargets?.has(taskId));
  return (
    <PlanFlowTaskCard
      projectId={ui?.projectId ?? ''}
      nodeId={data.node.task_id}
      taskId={taskId}
      orgRef={data.node.org_ref}
      title={data.node.title || refLabel(data.node.org_ref, taskId)}
      assigneeRef={data.node.assignee_ref}
      status={data.node.task_status === 'discarded' ? 'discarded' : data.node.node_status}
      selectedRevision={ui?.selectedRevision}
      generationDecision={ui?.generationDecisions?.get(taskId)}
      generationNode={ui?.generationNodeOf?.get(taskId)}
      archived={data.node.archived}
      testId="plan-dag-node"
      taskIdTestId="plan-node-taskid"
      isSource={isSource}
      isTarget={isTarget}
      canStartConnect={Boolean(ui?.canEditDependencies && !inConnect)}
      onStartConnect={() => ui?.onStartConnect?.(taskId)}
      onTargetActivate={() => ui?.onTargetActivate?.(taskId)}
      titleOf={ui?.titleOf}
    />
  );
}

function PlanFlowTaskCard({
  projectId,
  nodeId,
  taskId,
  orgRef,
  title,
  assigneeRef,
  status,
  generationNode,
  archived,
  historical,
  selectedRevision,
  generationDecision,
  testId,
  taskIdTestId,
  isSource,
  isTarget,
  canStartConnect,
  onStartConnect,
  onTargetActivate,
  titleOf,
}: {
  projectId: string;
  nodeId: string;
  taskId: string;
  orgRef?: string;
  title: string;
  assigneeRef: string;
  status: PlanNodeStatus;
  generationNode?: PlanGenerationRead['nodes'][number];
  archived?: boolean;
  historical?: boolean;
  selectedRevision?: number;
  generationDecision?: PlanGenerationDiff['node_decisions'][number];
  testId: string;
  taskIdTestId: string;
  isSource?: boolean;
  isTarget?: boolean;
  canStartConnect?: boolean;
  onStartConnect?: () => void;
  onTargetActivate?: () => void;
  titleOf?: (taskId: string) => string;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const s = NODE_STATE[status] ?? NODE_STATE.blocked;
  const accentCls = s.border.replace(/^border-/, 'bg-');
  const hasGeneration = generationNode?.revision != null;
  const emphasis = historical || status === 'discarded' ? 'historical' : generationNode?.revision === selectedRevision ? 'new' : 'retained';
  const openTask = useCallback(() => {
    window.open(`/projects/${projectId}/tasks/${taskId}`, '_blank', 'noopener,noreferrer');
  }, [projectId, taskId]);
  const onCardClick = useCallback((event: React.MouseEvent<HTMLDivElement>) => {
    const target = event.target as HTMLElement | null;
    if (target?.closest('a,button,input,textarea,select')) return;
    openTask();
  }, [openTask]);
  const onCardKeyDown = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    // Descendant links/buttons own Enter and Space; only the card itself opens a task.
    if (event.target !== event.currentTarget) return;
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    openTask();
  }, [openTask]);
  return (
    <div
      // React Flow disables pointer events on non-selectable/non-draggable wrappers.
      // Restore hit testing for the card controls without enabling node manipulation.
      data-generation-emphasis={emphasis}
      data-generation={generationNode?.revision}
      className={`${hasGeneration ? 'plan-generation plan-generation-card' : ''} pointer-events-auto nopan relative flex h-full flex-col cursor-pointer overflow-hidden rounded-lg border-[1.5px] bg-bg-elevated p-2 pl-3 shadow-1 transition duration-150 motion-safe:hover:-translate-y-0.5 hover:shadow-2 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${
        isTarget ? 'border-accent ring-2 ring-accent' : isSource ? 'border-accent' : `${historical ? 'border-dashed border-border-strong' : s.border} ${nodeVisualCls(status)}`
      }`}
      style={{ ...generationStyle(generationNode?.revision), width: PLAN_DAG_NODE_W, height: PLAN_DAG_NODE_H }}
      role="link"
      tabIndex={0}
      onClick={onCardClick}
      onKeyDown={onCardKeyDown}
      data-testid={testId}
      data-node-id={nodeId}
      data-task-id={taskId}
      data-connect-source={isSource ? 'true' : undefined}
      data-connect-target={isTarget ? 'true' : undefined}
    >
      <PlanFlowNodeHandles />
      <span className={`absolute inset-y-0 left-0 w-1.5 ${hasGeneration ? 'plan-generation-stripe' : historical ? 'bg-border-strong' : accentCls}`} aria-hidden="true" />
      <div className="mb-1 flex items-center justify-between gap-1">
        <TaskIdTag taskId={taskId} orgRef={orgRef} testId={taskIdTestId} />
        <span className="inline-flex shrink-0 items-center gap-1">
          <NodeGenerationBadge node={{ revision: generationNode?.revision }} />
          {archived !== undefined && <TaskArchivedBadge archived={archived} taskId={taskId} />}
          <NodeStateChip status={status} />
          {canStartConnect && (
            <button
              type="button"
              data-testid="plan-node-connect"
              data-task-id={taskId}
              onClick={onStartConnect}
              aria-label={t('plan.detail.dag.addDependencyAria', { title: titleOf?.(taskId) ?? title })}
              title={t('plan.detail.dag.addDependencyTitle', { title: titleOf?.(taskId) ?? title })}
              className="shrink-0 rounded border border-border-strong bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            >
              {t('plan.detail.dag.addDep')}
            </button>
          )}
        </span>
      </div>
      <div className="mb-1.5 min-h-0 flex-1 text-xs font-semibold leading-4 text-text-primary" title={title}>
        <TaskTitleLink projectId={projectId} taskId={taskId} title={title} wrap />
      </div>
      {generationDecision && selectedRevision != null && <span
        className="plan-generation plan-generation-badge mb-0.5 shrink-0 self-start rounded px-1 text-[0.625rem]"
        style={generationStyle(selectedRevision)} title={generationDecision.reason} data-testid="plan-node-generation-decision">
        {t(`plan.detail.dag.decision.${generationDecision.action}`, { revision: `R${selectedRevision + 1}` })}
      </span>}
      <div className="flex min-w-0 shrink-0 items-center justify-between gap-1 text-[0.6875rem]">
        {!generationDecision && (historical || status === 'discarded') && <span className="shrink-0 text-text-secondary" data-testid="plan-node-history">{t('plan.detail.dag.historical')}</span>}
        <AssigneeTag assigneeRef={assigneeRef} />
      </div>
      {isTarget && (
        <button
          type="button"
          data-testid="plan-connect-target"
          data-task-id={taskId}
          onClick={onTargetActivate}
          aria-label={t('plan.detail.dag.makeDependOn', { from: titleOf?.('') ?? '', to: title })}
          title={t('plan.detail.dag.makeDependOn', { from: titleOf?.('') ?? '', to: title })}
          className="absolute inset-0 rounded-lg border-2 border-accent bg-transparent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
      )}
    </div>
  );
}

function PlanFlowNodeHandles(): React.ReactElement {
  return (
    <>
      <Handle
        id="top"
        type="target"
        position={Position.Top}
        className="pointer-events-none opacity-0"
        data-testid="plan-flow-handle-top"
        aria-hidden="true"
      />
      <Handle
        id="bottom"
        type="source"
        position={Position.Bottom}
        className="pointer-events-none opacity-0"
        data-testid="plan-flow-handle-bottom"
        aria-hidden="true"
      />
    </>
  );
}

function PlanFlowEdge(props: EdgeProps<PlanDagFlowEdge>): React.ReactElement {
  const { t } = useTranslation('work');
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, markerEnd, style, data } = props;
  const className = (props as { className?: string }).className;
  const [edgePath, labelX, labelY] = getSmoothStepPath({ sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition });
  const ui = (data as NonNullable<PlanDagFlowEdge['data']> & { ui?: PlanFlowEdgeUi } | undefined)?.ui;
  const route = data?.route;
  const middle = route?.length ? Math.floor(route.length / 2) : 0;
  const historyLabelX = route && middle > 0 ? (route[middle - 1].x + route[middle].x) / 2 : labelX;
  const historyLabelY = route && middle > 0 ? (route[middle - 1].y + route[middle].y) / 2 : labelY;
  const fromTaskId = data?.fromTaskId;
  const toTaskId = data?.toTaskId;
  const edgeKey = (id.includes(':') ? id.split(':')[1] : id)
    .replaceAll('__legacy_start__', 'start')
    .replaceAll('__legacy_end__', 'end');
  const edgeTestId = data?.kind === 'synthetic' ? 'plan-dag-synthetic-edge' : fromTaskId && toTaskId ? 'plan-dag-edge' : 'plan-graph-edge';
  return (
    <>
      <path
        id={id}
        d={data?.route?.length ? data.route.map((point, index) => `${index === 0 ? 'M' : 'L'} ${point.x} ${point.y}`).join(' ') : edgePath}
        fill="none"
        markerEnd={markerEnd}
        style={style}
        className={`${className ?? `plan-flow-edge plan-flow-edge--${data?.kind ?? 'seq'}`} ${data?.kind === 'lineage' ? 'plan-generation' : ''} ${data?.historical ? 'plan-flow-edge--historical' : ''}`}
        data-testid={edgeTestId}
        data-edge={edgeKey}
        data-edge-kind={data?.kind}
        data-edge-historical={data?.historical || undefined}
      >
        {data?.historical && <title>{t('plan.detail.dag.historicalEdge')}</title>}
      </path>
      {data?.historical && (
        <EdgeLabelRenderer>
          <span className="pointer-events-none absolute rounded bg-bg-elevated px-1 text-[10px] text-text-secondary"
            data-testid="plan-historical-edge-label"
            style={{ transform: `translate(-50%, -50%) translate(${historyLabelX}px, ${historyLabelY}px)` }}>
            {t('plan.detail.dag.inactiveEdge')}
          </span>
        </EdgeLabelRenderer>
      )}
      {!data?.historical && ui?.canEditDependencies && fromTaskId && toTaskId && (
        <EdgeLabelRenderer>
          <button
            type="button"
            data-testid="plan-edge-delete"
            data-edge={`${fromTaskId}->${toTaskId}`}
            disabled={ui.isPending}
            onClick={() => ui.onRemove?.(fromTaskId, toTaskId)}
            aria-label={ui.titleOf ? `Remove dependency ${ui.titleOf(fromTaskId)} -> ${ui.titleOf(toTaskId)}` : 'Remove dependency'}
            className="nodrag nopan absolute flex h-5 w-5 items-center justify-center rounded-full border border-border-strong bg-bg-elevated text-xs font-bold leading-none text-text-secondary shadow-1 hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
            style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
          >
            <span aria-hidden="true">&times;</span>
          </button>
        </EdgeLabelRenderer>
      )}
    </>
  );
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
  const generationQuery = usePlanGenerations(projectId, plan.id);
  const generationRead = usableGenerationRead(generationQuery.data);
  const g = graphQuery.data;
  if (g?.has_graph && (g.nodes?.length ?? 0) > 0) {
    return <PlanGraphDag projectId={projectId} plan={plan} graph={{ nodes: g.nodes ?? [], edges: g.edges ?? [] }} compact={compact} generationRead={generationRead} />;
  }
  return <LegacyPlanDag projectId={projectId} plan={plan} compact={compact} generationRead={generationRead} />;
}

// LegacyPlanDag renders the depends_on graph for plans WITHOUT an orchestration
// graph (has_graph:false). Split out of PlanDag so ALL of its hooks run
// unconditionally — there is no graph early-return sitting between them (that
// interleaving was the React #300 root cause).
function LegacyPlanDag({
  projectId,
  plan,
  compact,
  generationRead,
}: {
  projectId: string;
  plan: Plan;
  compact: boolean;
  generationRead?: PlanGenerationRead;
}): React.ReactElement {
  const { t } = useTranslation('work');
  const nodes = plan.nodes ?? [];
  const stagesQuery = usePlanStages(projectId, plan.id);
  const stages = stagesQuery.data ?? [];
  const evolutionRevisions = useMemo(() => buildDagEvolutionRevisions(generationRead, t), [generationRead, t]);
  const currentGeneration = useMemo(
    () => activeGenerationRevision(generationRead) ?? 0,
    [generationRead],
  );
  const [selectedGeneration, setSelectedGeneration] = useState<number | null>(null);
  const selectEvolutionGeneration = useCallback((generation: number) => setSelectedGeneration(generation), []);
  const effectiveGeneration = selectedGeneration ?? currentGeneration;
  const historicalGeneration = effectiveGeneration < currentGeneration
    ? generationAtRevision(generationRead, effectiveGeneration)
    : undefined;
  useEffect(() => {
    if (selectedGeneration == null) return;
    if (!evolutionRevisions.some((revision) => revision.generation === selectedGeneration)) setSelectedGeneration(null);
  }, [evolutionRevisions, selectedGeneration]);
  const visibleStages = useMemo(() => {
    if (historicalGeneration) {
      const taskIDs = new Set(historicalGeneration.snapshot.tasks.map((task) => task.task_id));
      return stages.filter((stage) => (stage.members ?? []).some((member) => taskIDs.has(member.task_id)));
    }
    return stages;
  }, [historicalGeneration, stages]);
  const visibleNodes = useMemo(() => {
    if (historicalGeneration) return snapshotPlanNodes(historicalGeneration);
    if (stages.length === 0 || effectiveGeneration >= currentGeneration) {
      return nodes;
    }
    const taskIds = stageTaskIdSet(visibleStages);
    if (taskIds.size === 0) return nodes;
    return nodes.filter((node) => taskIds.has(node.task_id));
  }, [currentGeneration, effectiveGeneration, historicalGeneration, nodes, stages.length, visibleStages]);
  const stageDisplay = useMemo(() => stageDisplayMeta(visibleStages), [visibleStages]);
  const generationNodeOf = useMemo(() => generationNodeMap(generationRead), [generationRead]);
  const generationDecisions = useMemo(() => new Map(
    (generationAtRevision(generationRead, effectiveGeneration)?.diff?.node_decisions ?? []).map((decision) => [decision.task_id, decision]),
  ), [generationRead, effectiveGeneration]);
  const isPending = plan.status === 'pending';
  const canEditDependencies = isPending && effectiveGeneration >= currentGeneration;
  // v2.9.1 UX point 2: "Compact" uniformly zooms the DAG down so a long (many-level)
  // / wide plan fits in view without endless horizontal scrolling. CSS transform
  // (content scales cleanly); the scroll area is sized to the scaled extent.
  const scale = compact ? 0.7 : 1;

  // v2.9.1 point 3: IN-GRAPH dependency editing (pending-only). The dependency
  // STRUCTURE is edited directly on the graph — no separate dropdown box (§21
  // single entry). Each pending node has a focusable "connect" control that enters
  // CONNECT MODE with that node as the source; valid targets (validDropTargets,
  // = excludes self/exists/cycle — cycle/self blocked at the UI layer) light up
  // as activatable targets. Each existing edge has a focusable delete control.
  // Add = AddPlanDependency(from=source, to=target) → "source depends on target".
  const addDep = useAddDependency(projectId, plan.id);
  const removeDep = useRemoveDependency(projectId, plan.id);
  // connectFrom = the SOURCE task_id of the in-progress connection (null = not in
  // connect mode). Only meaningful while pending.
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

  // Leaving the editable current DAG (e.g. plan starts or the user views a
  // historic revision) must drop any in-progress connect mode.
  useEffect(() => {
    if (!canEditDependencies) setConnectFrom(null);
  }, [canEditDependencies]);

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

  const topologyKey = useMemo(
    () => [
      visibleNodes.map((node) => `${node.task_id}:${node.depends_on.join('.')}`).sort().join(','),
      visibleStages.map((stage) => `${stage.id}:${stage.members.map((member) => member.task_id).join('.')}:${stage.depends_on_stages.join('.')}`).join(','),
    ].join('|'),
    [visibleNodes, visibleStages],
  );
  const { layout: flowLayout, loading: flowLoading } = useElkFlowLayout(
    () => layoutLegacyFlow(visibleNodes, visibleStages, generationRead, historicalGeneration?.id ?? generationRead?.active_generation_id),
    [topologyKey, generationRead, historicalGeneration],
  );
  const flowNodeUi = useMemo<PlanFlowNodeUi>(() => ({
    projectId,
    generationNodeOf,
    selectedRevision: effectiveGeneration,
    generationDecisions,
    canEditDependencies,
    connectFrom,
    dropTargets,
    titleOf,
    onStartConnect: setConnectFrom,
    onTargetActivate,
    stageDisplay,
  }), [canEditDependencies, connectFrom, dropTargets, generationNodeOf, onTargetActivate, projectId, stageDisplay, titleOf, effectiveGeneration, generationDecisions]);
  const flowEdgeUi = useMemo<PlanFlowEdgeUi>(() => ({
    canEditDependencies,
    isPending: removeDep.isPending,
    titleOf,
    onRemove: (fromTaskId, toTaskId) => removeDep.mutate({ from_task_id: fromTaskId, to_task_id: toTaskId }),
  }), [canEditDependencies, removeDep, titleOf]);
  const currentFlowNodes = useMemo(
    () => (flowLayout ? refreshLegacyFlowNodes(flowLayout.nodes, visibleNodes, visibleStages) : []),
    [flowLayout, visibleNodes, visibleStages],
  );
  const flowNodes = useMemo(
    () => withNodeUi(currentFlowNodes, flowNodeUi),
    [currentFlowNodes, flowNodeUi],
  );
  const flowEdges = useMemo(
    () => (flowLayout ? withEdgeUi(flowLayout.edges, flowEdgeUi) : []),
    [flowEdgeUi, flowLayout],
  );
  const positioned = useMemo<Positioned[]>(() => {
    if (!flowLayout) return [];
    const yRanks = new Map(
      [...new Set(currentFlowNodes.filter((node) => node.data.kind === 'legacy').map((node) => Math.round(node.position.y)))]
        .sort((a, b) => a - b)
        .map((y, index) => [y, index]),
    );
    return currentFlowNodes.flatMap((node) => {
      if (node.data.kind !== 'legacy') return [];
      return [{ node: node.data.node, level: yRanks.get(Math.round(node.position.y)) ?? 0, x: node.position.x, y: node.position.y }];
    });
  }, [currentFlowNodes, flowLayout]);

  return (
    // SenderSidebarProvider owns the ONE agent-activity sidebar for the whole DAG
    // surface — every node's AssigneeTag (mobile stepper + desktop graph) opens it
    // through useSenderSidebar(). Scoped to the DAG, decoupled from the rail's own
    // sidebar and the chat's provider.
    <SenderSidebarProvider>
    {/* T579: desktop = flex column filling the panel so the canvas (flex-1 below)
        grows to occupy the full pane height; the legend/note stay pinned beneath it. */}
    <div data-testid={nodes.length > 0 && flowLoading && !flowLayout ? 'plan-dag-layout-loading' : 'plan-dag'} className="md:flex md:min-h-0 md:flex-1 md:flex-col">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-dag-empty">
          {t('plan.detail.dag.empty')}
        </p>
      ) : (
        <>
        {/* v2.10.1 [M4] Mobile (<md): the left→right SVG DAG becomes a vertical
            stepper. The desktop graph + its controls are md:-only. */}
        <DagEvolutionPanel
          revisions={evolutionRevisions}
          selectedGeneration={effectiveGeneration}
          onSelectGeneration={selectEvolutionGeneration}
        />
        <MobileStageGateAudits stages={visibleStages} error={stagesQuery.isError} />
        <PlanStepper positioned={positioned} projectId={projectId} generationNodeOf={generationNodeOf} />
        {/* T348: the Compact toggle moved to the tab row (icon). The connect-mode
            banner (point 3, pending-only) stays here, shown only while connecting. */}
        {canEditDependencies && connectFrom != null && (
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
        {flowLoading && !flowLayout ? (
          <div className="hidden rounded-lg border border-border-base bg-bg-subtle py-10 text-center text-xs text-text-muted md:block" data-testid="plan-dag-layout-loading">
            {t('plan.detail.dag.layoutLoading', { defaultValue: 'Laying out graph…' })}
          </div>
        ) : (
          <PlanFlowCanvas
            nodes={flowNodes}
            edges={flowEdges}
            compact={compact}
            topologyKey={topologyKey}
            legend={
              <div className="contents" data-testid="plan-dag-legend">
                {NODE_STATE_ORDER.map((st) => (
                  <NodeStateChip key={st} status={st} />
                ))}
              </div>
            }
          >
            <div data-testid="plan-dag-scaler" className="hidden" data-scale={scale} style={{ transform: scale === 1 ? undefined : `scale(${scale})` }} />
          </PlanFlowCanvas>
        )}
        </>
      )}

      {/* node_status is DERIVED (§9.2) and shown, not edited. In PENDING the
          dependency STRUCTURE is editable IN-GRAPH (point 3): each node has a
          "+ Dep" connect control, and each edge has an "×" delete control. Once
          running/done the graph is DISPLAY-ONLY (backend rejects with
          plan-not-pending). */}
      <p className="mt-2 text-[0.6875rem] text-text-muted" data-testid="plan-dag-note">
        {t('plan.detail.dag.noteDerived')}{' '}
        {canEditDependencies ? t('plan.detail.dag.notePending') : t('plan.detail.dag.noteDisplayOnly')}
      </p>

      {/* #218 friendly add/remove error (never the raw API message). The
          single in-graph entry point (§21) — no separate editor box. */}
      {canEditDependencies && mutationError && (
        <p className="mt-2 text-xs font-medium text-danger" role="alert" data-testid="plan-edge-error">
          {friendlyDependencyError(mutationError, t)}
        </p>
      )}
    </div>
    </SenderSidebarProvider>
  );
}

// ── Pending-only dependency-edge editor (v2.9 Stage A1) ────────────────────────
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
  if (lower.includes('pending')) {
    return t('plan.detail.depError.pending');
  }
  return t('plan.detail.depError.generic');
}

// ── Task list tab ────────────────────────────────────────────────────────────
// §9.4: removing a task from a Plan is a PLANNING action — only a PENDING plan
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

function projectMemberRef(m: ProjectMember): string {
  const kind = refKind(m.identity_id);
  return identityRefOf({ kind, identity_id: m.identity_id });
}

// T41 (v2.9.1 #291): the Task-list tab is the COMPREHENSIVE management surface
// for a big plan — every node is rendered (no cap, ever), a search box narrows
// the visible rows by title / Task-id / assignee, and the table scrolls
// vertically within the tab. Inline assignee reassignment lives per-row.
function PlanTaskList({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const { t } = useTranslation('work');
  const nodes = plan.nodes ?? [];
  const canRemove = plan.status === 'pending';
  const members = useMembers();
  const projectMembers = useProjectMembers(projectId);
  const generationQuery = usePlanGenerations(projectId, plan.id);
  const generationRead = usableGenerationRead(generationQuery.data);
  const generationNodeOf = useMemo(() => generationNodeMap(generationRead), [generationRead]);
  const [query, setQuery] = useState('');
  const memberDirectory = useMemo(() => {
    const out = new Map<string, MemberResult>();
    for (const member of members.data ?? []) {
      out.set(memberRef(member), member);
    }
    return out;
  }, [members.data]);

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
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colCreated')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colStarted')}</th>
                    <th className="py-1.5 pr-3 font-medium">{t('plan.detail.taskList.colEnded')}</th>
                    <th className="py-1.5 font-medium">{t('plan.detail.taskList.colDuration')}</th>
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
                      members={projectMembers.data ?? []}
                      memberDirectory={memberDirectory}
                      generationNode={generationNodeOf.get(n.task_id)}
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

// PlanTaskRow — one task-list row. When the plan is pending, the trailing cell
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
  memberDirectory,
  generationNode,
}: {
  projectId: string;
  planId: string;
  node: PlanNode;
  orgRef?: string;
  canRemove: boolean;
  members: ProjectMember[];
  memberDirectory: Map<string, MemberResult>;
  generationNode?: PlanGenerationRead['nodes'][number];
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
  // T41 inline 分派: reassigning is NOT pending-gated (allowed regardless of plan
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
  const generationMeta = {
    revision: generationNode?.revision,
  };
  const runtimeDuration = formatTimeRangeDuration(node.dispatched_at, node.completed_at);
  // T147: ONE assignee control. Build the dropdown options — "" = Unassigned
  // (routes to the unassign endpoint), then each project member with an avatar
  // leading so the (single) dropdown trigger shows the current assignee's
  // avatar + name. Mirrors the old <option> list (memberRef + "name (kind)").
  const assigneeOptions: EntityOption[] = useMemo(() => {
    const opts: EntityOption[] = [{ value: '', label: t('plan.detail.taskList.unassigned') }];
    for (const m of members) {
      const ref = projectMemberRef(m);
      const directoryMember = memberDirectory.get(ref);
      const kind = directoryMember?.kind ?? refKind(ref);
      const name = directoryMember?.display_name ?? normalizeIdentityRef(ref);
      opts.push({
        value: ref,
        label: name,
        badge: kind,
        leading: <Avatar name={name} kind={kind === 'agent' ? 'agent' : 'human'} size="sm" />,
      });
    }
    return opts;
  }, [memberDirectory, members, t]);
  return (
    <tr data-testid="plan-task-row" data-task-id={node.task_id}>
      {/* v2.9.1 UX point 1: human Task id (T-number) column. */}
      <td className="py-1.5 pr-3 align-top">
        <span className="inline-flex items-center gap-1">
          <TaskIdTag taskId={node.task_id} orgRef={orgRef} testId="plan-row-taskid" />
          <NodeGenerationBadge node={generationMeta} testId="plan-row-generation" />
        </span>
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
          <SupersededDrainingChip node={node} />
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
      <td className="py-1.5 pr-3 tabular-nums text-text-muted" data-testid="plan-row-created" title={node.created_at ?? ''}>
        {node.created_at ? fullDateTime(node.created_at) : '—'}
      </td>
      <td className="py-1.5 pr-3 tabular-nums text-text-muted" data-testid="plan-row-started" title={node.dispatched_at ?? ''}>
        {node.dispatched_at ? fullDateTime(node.dispatched_at) : '—'}
      </td>
      <td className="py-1.5 pr-3 tabular-nums text-text-muted" data-testid="plan-row-ended" title={node.completed_at ?? ''}>
        {node.completed_at ? fullDateTime(node.completed_at) : '—'}
      </td>
      <td className="py-1.5 tabular-nums text-text-muted" data-testid="plan-row-duration">
        {runtimeDuration ?? '—'}
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
  projectId,
  conversationId,
  maximized,
  onToggleMaximize,
}: {
  projectId: string;
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
  const mentionCandidates = useProjectMentionCandidates(projectId);
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
              <ConversationView surface="task-thread" conversationId={conversationId} mentionCandidates={mentionCandidates} />
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
