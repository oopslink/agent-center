import type React from 'react';
import { useEffect, useRef, useState } from 'react';
import RFB from '@novnc/novnc';
import { Trans, useTranslation } from 'react-i18next';
import { OrgLink, useOptionalOrgContext } from '@/OrgContext';
import { useTablistKeyboard } from '@/components/useTablistKeyboard';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { useCreateConversation } from '@/api/conversations';
import {
  useAgent,
  useAgentActivity,
  useArchiveAgent,
  useForceDeleteAgent,
  useResetAgent,
  useRestartAgent,
  useAgentSandboxAction,
  useAgentSandboxCommandStatus,
  useAgentSandboxDesktopSession,
  useStartAgent,
  useStopAgent,
  sandboxDesktopWebsocketURL,
  type ResetScope,
  type SandboxAction,
} from '@/api/agents';
import { useAgentConcurrency } from '@/api/concurrency';
import { AgentBacklogBadge, AgentLoadBadge, AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { ConfirmModal } from '@/components/ConfirmModal';
import { ForceDeleteModal } from '@/components/ForceDeleteModal';
import { ExecutorSlotPanel } from '@/components/ExecutorSlotPanel';
import { AgentActivityRow, CheckingGroup, ExecutorProgressGroup } from '@/components/AgentActivityRow';
import { groupActivity } from '@/components/agentActivityGrouping';
import { AgentMemoryManager } from '@/components/AgentMemoryManager';
import { AgentProfile } from '@/components/AgentProfile';
import { AgentRuntime } from '@/components/AgentRuntime';
import { AgentTasks } from '@/components/AgentTasks';
import { AgentAnalyticsPanel } from '@/components/analytics/AgentAnalyticsPanel';
import { AccessPermissionsPanel } from '@/components/AccessPermissionsPanel';
import { Breadcrumb } from '@/components/Breadcrumb';

// v2.7.1 #228: AgentDetail is a tab surface.
// Profile/Activity/WorkItems get fleshed out in follow-up PRs (b/c/d).
// I28/F7 (v2.15.0): the 5th tab `analytics` mounts the per-agent dashboard
// (overview cards + activity heatmap + tokens/cost trend + top cost tasks),
// route /agents/:id?tab=analytics. (T470: the "NEW" pill was dropped @oopslink.)
// The tab `key` is the STABLE discriminator (route ?tab=, testids, keyboard nav);
// the visible label is localized at render via t('agents.detail.tabs.<key>').
const AGENT_TABS = [
  { key: 'profile' },
  { key: 'permissions' },
  { key: 'activity' },
  // I5 (T583): read-only runtime browser — memory/workspace tree + file preview.
  { key: 'runtime' },
  { key: 'memory' },
  { key: 'tasks' },
  { key: 'analytics' },
] as const;
type AgentTab = (typeof AGENT_TABS)[number]['key'];

// AgentDetail (/agents/:id). Agent BC (v2.7 #101). Header (name, lifecycle,
// availability, worker) + lifecycle controls gated by state, a Reset modal
// (scope + double-confirm), a WorkItem queue and an Activity stream.
// No profile-edit (no backend update-profile endpoint in #101 scope).
export default function AgentDetail(): React.ReactElement {
  const { t } = useTranslation('members');
  const { id = '' } = useParams<{ id: string }>();
  const agent = useAgent(id);
  const activity = useAgentActivity(id);
  const concurrency = useAgentConcurrency(id);
  // #274: flatten the cursor-paginated pages into one chronological event list
  // (newest-first). Grouping/folding runs over this FULL accumulated set so a
  // Checking run spanning a page boundary merges rather than fragmenting.
  const activityEvents = activity.data?.pages.flatMap((p) => p.activity) ?? [];

  const start = useStartAgent(id);
  const stop = useStopAgent(id);
  const restart = useRestartAgent(id);
  const reset = useResetAgent(id);
  const archive = useArchiveAgent(id);
  const forceDelete = useForceDeleteAgent();
  const sandboxAction = useAgentSandboxAction(id);

  // v2.7.1 #240: header "Send message" → open (or reuse) the 1:1 DM with this
  // agent. The backend dedups (#215), so createConversation returns the existing
  // DM id when one already exists — no duplicate DM is ever created.
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const createDm = useCreateConversation();
  const messageAgent = async () => {
    if (createDm.isPending) return;
    try {
      // v2.7.1 #240 fix: DM members are PREFIXED identity refs (`agent:<id>` /
      // `user:<id>`, same as #215 / DMStartModal). A bare business id is rejected
      // by the backend ref validator (400 invalid_input). This is an agent page,
      // so the peer is always `agent:<id>`.
      const res = await createDm.mutateAsync({ kind: 'dm', members: [`agent:${id}`] });
      const slug = org?.slug;
      navigate(slug ? `/organizations/${slug}/dms/${res.conversation_id}` : `/dms/${res.conversation_id}`);
    } catch {
      // surfaced via the action-error line below
    }
  };

  const [resetOpen, setResetOpen] = useState(false);
  const [archiveOpen, setArchiveOpen] = useState(false);
  // v2.8.1: force-delete (admin escape hatch) — typed-name confirm. Kept open on
  // 409/error; the error is fed into the modal's `error` prop.
  const [forceDeleteOpen, setForceDeleteOpen] = useState(false);
  const [forceDeleteError, setForceDeleteError] = useState<string | null>(null);
  // v2.8 #270: stop/restart are disruptive → confirm before firing. (start is
  // non-destructive and stays direct; reset has its own scope modal.)
  const [confirmAction, setConfirmAction] = useState<'stop' | 'restart' | null>(null);
  const [sandboxDesktopOpen, setSandboxDesktopOpen] = useState(false);
  const [sandboxCommandId, setSandboxCommandId] = useState<string | null>(null);
  const sandboxCommand = useAgentSandboxCommandStatus(id, sandboxCommandId);
  useEffect(() => {
    const status = sandboxCommand.data?.command_status;
    if (status === 'succeeded' || status === 'failed' || status === 'canceled') {
      void agent.refetch();
      void concurrency.refetch();
    }
  }, [agent, concurrency, sandboxCommand.data?.command_status]);
  // v2.7.1 #228: active tab synced to ?tab= so a tab is shareable/bookmarkable.
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab');
  const desktopParam = searchParams.get('desktop');
  useEffect(() => {
    if (desktopParam === '1' && agent.data?.sandbox_enabled) {
      setSandboxDesktopOpen(true);
    }
  }, [agent.data?.sandbox_enabled, desktopParam]);
  const tab: AgentTab = (AGENT_TABS.some((t) => t.key === tabParam) ? tabParam : 'profile') as AgentTab;
  const setTab = (t: AgentTab) =>
    setSearchParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.set('tab', t);
        return p;
      },
      { replace: true },
    );
  // v2.8 #273: shared WAI-ARIA tablist keyboard nav (arrow keys + roving tabindex
  // + Home/End). Back-write of #228 — these tabs were previously click-only.
  const tablist = useTablistKeyboard({
    keys: AGENT_TABS.map((t) => t.key),
    active: tab,
  });

  if (agent.isLoading) {
    return (
      <section className="text-sm text-text-muted" data-testid="page-AgentDetail">
        {t('agents.detail.loading')}
      </section>
    );
  }
  if (agent.isError) {
    return (
      <section className="space-y-3" data-testid="page-AgentDetail">
        <p className="text-sm text-danger" data-testid="agent-not-found">
          {(agent.error as Error).message}
        </p>
        <OrgLink to="/agents" className="text-accent hover:underline">
          {t('agents.detail.backToAgents')}
        </OrgLink>
      </section>
    );
  }
  if (!agent.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-AgentDetail">
        {t('agents.detail.lookupFailed')}
      </section>
    );
  }

  const a = agent.data;
  const lc = a.lifecycle;
  // v2.8 #270: archived is a terminal soft-delete state → the detail page is
  // read-only history (no lifecycle actions); the LifecycleBadge shows it.
  const isArchived = lc === 'archived';
  const transient = lc === 'stopping' || lc === 'resetting';
  // Start moves stopped/error/failed → running. Including 'failed' is deliberate:
  // the backend's Agent.Start explicitly allows it as the operator's MANUAL recovery
  // out of the terminal crash-loop circuit-breaker (agent.go: "Starting a
  // terminal-FAILED agent is the operator's MANUAL recovery"). Omitting it here hid
  // the only recovery button on a failed agent, forcing a runtime-wiping Reset first.
  const canStart = lc === 'stopped' || lc === 'error' || lc === 'failed';
  const canStopRestart = lc === 'running';
  // v2.16 W5 (design §3.1): Reset wipes runtime state, so it is only available
  // from a SETTLED lifecycle — stopped / error / failed. A running / stopping /
  // resetting (or archived) agent must settle first (backend also 409-guards with
  // reset_requires_stopped). The operator stops the agent, then resets.
  const canReset = lc === 'stopped' || lc === 'error' || lc === 'failed';
  const showReset = !isArchived;
  // v2.8 #270/#272 (b strict-two-step): archive only a settled (stopped/error)
  // agent — a running agent must be stopped first (backend also 409-guards).
  const canArchive = lc === 'stopped' || lc === 'error';
  const agentSubjectRef = `agent:${a.identity_member_id || a.id}`;
  const sandboxEnabled = (a.sandbox_enabled ?? false) && !isArchived;
  const sandboxBinding = concurrency.data?.sandbox_binding ?? a.sandbox_binding;
  const sandboxCommandStatus = sandboxCommand.data?.command_status;
  const sandboxCommandRunning = sandboxCommandStatus === 'pending' || sandboxCommandStatus === 'running';
  const sandboxPending = sandboxAction.isPending || sandboxCommandRunning;
  const sandboxAwaitingRuntimeState = sandboxEnabled && !sandboxBinding && (sandboxPending || sandboxCommandStatus === 'succeeded');
  const sandboxState = sandboxBinding?.state || (sandboxAwaitingRuntimeState ? 'syncing' : 'unprovisioned');
  const sandboxDeleted = sandboxState === 'deleted';
  const sandboxStarting = sandboxState === 'provisioning' || (sandboxAction.data?.action === 'start' && sandboxPending);
  const sandboxRunning = sandboxState === 'running';
  const sandboxStateLabel = sandboxBinding
    ? t('agents.detail.sandbox.stateValue', { state: sandboxState })
    : t(sandboxAwaitingRuntimeState ? 'agents.detail.sandbox.stateSyncing' : 'agents.detail.sandbox.stateUnprovisioned');
  const sandboxProgress = sandboxPending
    ? t('agents.detail.sandbox.actionStatus', {
        action: sandboxAction.data?.action ?? 'sandbox',
        status: sandboxCommandStatus ?? sandboxAction.data?.status ?? 'pending',
      })
    : sandboxAwaitingRuntimeState
      ? t('agents.detail.sandbox.waitingForRuntimeState')
      : sandboxStarting
        ? t('agents.detail.sandbox.transitioning')
        : null;
  const sandboxCanStart = sandboxEnabled && !sandboxPending && !sandboxDeleted && !sandboxRunning && sandboxState !== 'provisioning';
  const sandboxCanSuspend = sandboxEnabled && !sandboxPending && (sandboxState === 'running' || sandboxState === 'ready');
  const sandboxCanOpen = sandboxEnabled && !sandboxPending && !sandboxDeleted;
  const sandboxCanResetDelete = sandboxEnabled && !sandboxPending && !sandboxDeleted;
  const runSandboxAction = (action: SandboxAction, openDesktop = false) => {
    sandboxAction.mutate(action, {
      onSuccess: (result) => {
        setSandboxCommandId(result.command_id ?? null);
        if (openDesktop) setSandboxDesktopOpen(true);
      },
    });
  };
  const closeSandboxDesktop = () => {
    setSandboxDesktopOpen(false);
    if (desktopParam === '1') {
      setSearchParams(
        (prev) => {
          const p = new URLSearchParams(prev);
          p.delete('desktop');
          return p;
        },
        { replace: true },
      );
    }
  };
  const popOutSandboxDesktop = () => {
    const url = new URL(window.location.href);
    url.searchParams.set('desktop', '1');
    window.open(url.toString(), '_blank', 'noopener,noreferrer,width=1280,height=900');
  };

  const lifecyclePending =
    start.isPending || stop.isPending || restart.isPending;
  const lifecycleError =
    (start.error as Error | null)?.message ??
    (stop.error as Error | null)?.message ??
    (restart.error as Error | null)?.message ??
    (reset.error as Error | null)?.message ??
    null;

  return (
    <section
      className="space-y-4"
      data-testid="page-AgentDetail"
      data-agent-id={a.id}
      data-lifecycle={a.lifecycle}
    >
      <Breadcrumb
        items={[{ label: t('agents.detail.breadcrumbMembers') }, { label: t('agents.detail.breadcrumbAgents'), to: '/agents' }, { label: a.name }]}
      />
      <header className="flex flex-wrap items-center justify-between gap-3 border-b border-border-base pb-3">
        {/* @oopslink: worker subtitle removed — it duplicated the Profile
            section's "Computer <name> OFFLINE/ONLINE" row. Header keeps just
            the name + lifecycle + availability. */}
        <div className="flex flex-wrap items-center gap-2">
          <h2 className="text-xl font-semibold">{a.name}</h2>
          <LifecycleBadge lifecycle={a.lifecycle} />
          <AvailabilityBadge availability={a.availability} />
          {/* T342: agent load (doing/total) + backlog (pending count). */}
          <AgentLoadBadge agent={a} />
          <AgentBacklogBadge agent={a} />
        </div>

        {/* v2.10.1 M6: lifecycle controls are ≥44px touch targets on mobile
            (child-button variant); desktop keeps the compact icon buttons. */}
        <div
          className="flex flex-wrap items-center gap-2 [&>button]:min-h-[44px] [&>button]:min-w-[44px] [&>button]:justify-center md:[&>button]:min-h-0 md:[&>button]:min-w-0"
          data-testid="agent-controls"
        >
          {/* #270: archived agents are read-only — no message/lifecycle actions. */}
          {!isArchived && (
            <button
              type="button"
              onClick={() => void messageAgent()}
              disabled={createDm.isPending}
              className="flex items-center rounded border border-border-base px-2 py-1.5 text-text-primary hover:bg-bg-subtle disabled:opacity-50"
              data-testid="agent-message-btn"
              title={t('agents.detail.controls.messageTitle')}
              aria-label={t('agents.detail.controls.messageAria')}
              aria-busy={createDm.isPending}
            >
              <ChatBubbleIcon />
            </button>
          )}
          {canStart && (
            // v2.8 #271: Start is now an icon button (was the only text action —
            // #250 missed it), consistent with Stop/Restart/Reset/Message.
            <button
              type="button"
              onClick={() => start.mutate()}
              disabled={lifecyclePending}
              className="flex items-center rounded bg-brand px-2 py-1.5 text-white hover:bg-brand-hover disabled:opacity-50"
              data-testid="agent-start-btn"
              title={t('agents.detail.controls.startTitle')}
              aria-label={t('agents.detail.controls.startAria')}
            >
              <PlayIcon />
            </button>
          )}
          {canStopRestart && (
            <>
              {/* v2.7.1 #250: lifecycle controls icon-ified (same as #240 Message). */}
              <button
                type="button"
                onClick={() => setConfirmAction('stop')}
                disabled={lifecyclePending}
                className="flex items-center rounded border border-border-base px-2 py-1.5 text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                data-testid="agent-stop-btn"
                title={t('agents.detail.controls.stopTitle')}
                aria-label={t('agents.detail.controls.stopAria')}
              >
                <StopIcon />
              </button>
              <button
                type="button"
                onClick={() => setConfirmAction('restart')}
                disabled={lifecyclePending}
                className="flex items-center rounded border border-border-base px-2 py-1.5 text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                data-testid="agent-restart-btn"
                title={t('agents.detail.controls.restartTitle')}
                aria-label={t('agents.detail.controls.restartAria')}
              >
                <RestartIcon />
              </button>
            </>
          )}
          {showReset && (
            <button
              type="button"
              onClick={() => setResetOpen(true)}
              disabled={!canReset || transient || reset.isPending}
              className="flex items-center rounded border border-danger/40 px-2 py-1.5 text-danger hover:bg-danger/10 disabled:opacity-50"
              data-testid="agent-reset-btn"
              title={
                canReset
                  ? t('agents.detail.controls.resetTitle')
                  : t('agents.detail.controls.resetRequiresStoppedTitle')
              }
              aria-label={t('agents.detail.controls.resetAria')}
            >
              <ResetIcon />
            </button>
          )}
          {canArchive && (
            // v2.8 #270/#272: soft-archive (user-facing delete path) → ConfirmModal.
            <button
              type="button"
              onClick={() => setArchiveOpen(true)}
              disabled={archive.isPending}
              className="flex items-center rounded border border-danger/40 px-2 py-1.5 text-danger hover:bg-danger/10 disabled:opacity-50"
              data-testid="agent-archive-btn"
              title={t('agents.detail.controls.archiveTitle')}
              aria-label={t('agents.detail.controls.archiveAria')}
            >
              <ArchiveIcon />
            </button>
          )}
          {/* v2.8.1: force-delete is an admin escape hatch — it cleans the
              center's records regardless of lifecycle (it skips the stop/active
              guards), so unlike the soft archive/lifecycle controls it is shown
              unconditionally (the backend is org-admin gated). */}
          <button
            type="button"
            onClick={() => {
              setForceDeleteError(null);
              setForceDeleteOpen(true);
            }}
            disabled={forceDelete.isPending}
            className="flex items-center rounded border border-danger/40 px-2 py-1.5 text-danger hover:bg-danger/10 disabled:opacity-50"
            data-testid="agent-force-delete"
            title={t('agents.detail.controls.forceDeleteTitle')}
            aria-label={t('agents.detail.controls.forceDeleteAria')}
          >
            <TrashIcon />
          </button>
          {transient && (
            <span className="text-xs text-text-muted" data-testid="agent-transient-note">
              {lc}…
            </span>
          )}
        </div>
      </header>

      {sandboxEnabled && (
        <section
          className="flex flex-wrap items-center gap-3 rounded border border-border-base bg-bg-elevated px-3 py-2"
          data-testid="agent-sandbox-controls"
          aria-label={t('agents.detail.sandbox.regionAria')}
        >
          <div className="min-w-[13rem] text-xs" data-testid="agent-sandbox-runtime-status">
            <div className="font-medium uppercase tracking-wide text-text-muted">{t('agents.detail.sandbox.label')}</div>
            <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-text-primary">
              <span>{sandboxStateLabel}</span>
              {sandboxBinding?.vm_name && <span className="text-text-muted">{sandboxBinding.vm_name}</span>}
              {sandboxBinding?.updated_at && (
                <span className="text-text-muted" title={sandboxBinding.updated_at}>
                  {t(sandboxRunning ? 'agents.detail.sandbox.runningFor' : 'agents.detail.sandbox.stateFor', { age: formatAgeFrom(sandboxBinding.updated_at) })}
                </span>
              )}
              {sandboxBinding?.last_health_at && (
                <span className="text-text-muted" title={sandboxBinding.last_health_at}>
                  {t('agents.detail.sandbox.healthAt', { time: formatLocalTime(sandboxBinding.last_health_at) })}
                </span>
              )}
            </div>
            {sandboxBinding?.last_error && <div className="mt-0.5 text-danger">{sandboxBinding.last_error}</div>}
            {sandboxProgress && <div className="mt-0.5 text-text-muted">{sandboxProgress}</div>}
          </div>
          <SandboxActionButton
            action="open_console"
            pending={sandboxPending}
            disabled={!sandboxCanOpen}
            title={t('agents.detail.sandbox.openConsoleTitle')}
            ariaLabel={t('agents.detail.sandbox.openConsoleAria')}
            testId="agent-sandbox-open-console"
            onAction={(action) => runSandboxAction(action, true)}
          >
            <MonitorIcon />
          </SandboxActionButton>
          <SandboxActionButton
            action="open_browser"
            pending={sandboxPending}
            disabled={!sandboxCanOpen}
            title={t('agents.detail.sandbox.openBrowserTitle')}
            ariaLabel={t('agents.detail.sandbox.openBrowserAria')}
            testId="agent-sandbox-open-browser"
            onAction={(action) => runSandboxAction(action, true)}
          >
            <BrowserIcon />
          </SandboxActionButton>
          <SandboxActionButton
            action="start"
            pending={sandboxPending}
            disabled={!sandboxCanStart}
            title={t('agents.detail.sandbox.startTitle')}
            ariaLabel={t('agents.detail.sandbox.startAria')}
            testId="agent-sandbox-start"
            onAction={(action) => runSandboxAction(action)}
          >
            <PlayIcon />
          </SandboxActionButton>
          <SandboxActionButton
            action="suspend"
            pending={sandboxPending}
            disabled={!sandboxCanSuspend}
            title={t('agents.detail.sandbox.suspendTitle')}
            ariaLabel={t('agents.detail.sandbox.suspendAria')}
            testId="agent-sandbox-suspend"
            onAction={(action) => runSandboxAction(action)}
          >
            <StopIcon />
          </SandboxActionButton>
          <SandboxActionButton
            action="reset"
            pending={sandboxPending}
            disabled={!sandboxCanResetDelete}
            danger
            title={t('agents.detail.sandbox.resetTitle')}
            ariaLabel={t('agents.detail.sandbox.resetAria')}
            testId="agent-sandbox-reset"
            onAction={(action) => runSandboxAction(action)}
          >
            <ResetIcon />
          </SandboxActionButton>
          <SandboxActionButton
            action="delete"
            pending={sandboxPending}
            disabled={!sandboxCanResetDelete}
            danger
            title={t('agents.detail.sandbox.deleteTitle')}
            ariaLabel={t('agents.detail.sandbox.deleteAria')}
            testId="agent-sandbox-delete"
            onAction={(action) => runSandboxAction(action)}
          >
            <TrashIcon />
          </SandboxActionButton>
          {(sandboxCommand.data || sandboxAction.data) && (
            <span className="text-xs text-text-muted" data-testid="agent-sandbox-action-status">
              {t('agents.detail.sandbox.actionStatus', {
                action: sandboxAction.data?.action ?? 'sandbox',
                status: sandboxCommand.data?.command_status ?? sandboxAction.data?.status ?? 'pending',
              })}
            </span>
          )}
          {sandboxAction.isError && (
            <span className="text-xs text-danger" data-testid="agent-sandbox-action-error">
              {(sandboxAction.error as Error).message}
            </span>
          )}
        </section>
      )}

      {sandboxDesktopOpen && (
        <SandboxDesktopModal
          agentId={id}
          agentName={a.name}
          onClose={closeSandboxDesktop}
          onPopOut={popOutSandboxDesktop}
        />
      )}

      {a.lifecycle_error && (
        <p className="text-xs text-danger" data-testid="agent-lifecycle-error">
          {a.lifecycle_error}
        </p>
      )}
      {lifecycleError && (
        <p className="text-xs text-danger" data-testid="agent-action-error">
          {lifecycleError}
        </p>
      )}
      {createDm.isError && (
        <p className="text-xs text-danger" data-testid="agent-message-error">
          {(createDm.error as Error).message}
        </p>
      )}

      {/* v2.7.1 #228: tab bar. */}
      <nav
        className="flex gap-1 [&>button]:min-h-[44px] md:[&>button]:min-h-0"
        role="tablist"
        aria-orientation="horizontal"
        ref={tablist.tablistRef}
        onKeyDown={tablist.onKeyDown}
        onBlur={tablist.onBlur}
        data-testid="agent-tabs"
      >
        {AGENT_TABS.map((tab2) => (
          <button
            key={tab2.key}
            type="button"
            role="tab"
            aria-selected={tab === tab2.key}
            tabIndex={tablist.tabIndexFor(tab2.key)}
            onClick={() => setTab(tab2.key)}
            data-testid={`agent-tab-${tab2.key}`}
            className={`-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-2 text-sm font-medium ${
              tab === tab2.key
                ? 'border-brand text-text-primary'
                : 'border-transparent text-text-muted hover:text-text-primary'
            }`}
          >
            {t(`agents.detail.tabs.${tab2.key}`)}
          </button>
        ))}
      </nav>

      {tab === 'profile' && (
        <div className="space-y-3" data-testid="agent-tabpanel-overview">
          <ExecutorSlotPanel
            data={concurrency.data}
            loading={concurrency.isLoading}
            error={concurrency.error as Error | null}
            testId="agent-detail-slot-panel"
          />
          <AgentProfile agent={a} />
        </div>
      )}

      {/* I5 (T583): read-only runtime browser. */}
      {tab === 'runtime' && <AgentRuntime agentId={id} />}

      {tab === 'permissions' && (
        <AccessPermissionsPanel
          subjectRef={agentSubjectRef}
          subjectLabel={a.name}
          resource={{ kind: 'org', id: a.organization_id }}
          resourceLabel={org?.orgName ?? a.organization_id}
        />
      )}

      {tab === 'memory' && <AgentMemoryManager agentId={id} />}

      {/* I28/F7: per-agent analytics dashboard (cards + heatmap + trend + top tasks). */}
      {tab === 'analytics' && (
        <div data-testid="agent-tabpanel-analytics">
          <AgentAnalyticsPanel agentId={id} />
        </div>
      )}

      {/* Task queue (v2.7.1 #228 PR(d): read-only table). */}
      {tab === 'tasks' && <AgentTasks agentId={id} />}

      {/* Activity stream */}
      {tab === 'activity' && (
      <section className="rounded border border-border-base bg-bg-elevated p-4" role="region" aria-label={t('agents.detail.activity.regionAria')} data-testid="agent-tabpanel-activity">
        <div className="mb-2 flex items-center justify-between">
          <h3 className="text-xs font-semibold uppercase tracking-wide text-text-muted">{t('agents.detail.activity.diagnostics')}</h3>
          <button
            type="button"
            className="rounded border border-border-strong px-2 py-1 text-xs text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
            data-testid="agent-activity-refresh"
            onClick={() => void activity.refetch()}
            disabled={activity.isFetching}
            aria-busy={activity.isFetching}
          >
            {activity.isFetching ? t('agents.detail.activity.refreshing') : t('agents.detail.activity.refresh')}
          </button>
        </div>
        <ExecutorSlotPanel
          data={concurrency.data}
          loading={concurrency.isLoading}
          error={concurrency.error as Error | null}
          className="mb-3"
          testId="agent-activity-slot-panel"
        />
        {activity.isLoading && (
          <p className="text-xs text-text-muted" data-testid="agent-activity-loading">
            {t('agents.detail.activity.loading')}
          </p>
        )}
        {activity.isError && (
          <p className="text-xs text-danger" data-testid="agent-activity-error">
            {(activity.error as Error).message}
          </p>
        )}
        {activity.isSuccess && activityEvents.length === 0 && (
          <p className="text-xs text-text-muted" data-testid="agent-activity-empty">
            {t('agents.detail.activity.empty')}
          </p>
        )}
        {activity.isSuccess && activityEvents.length > 0 && (
          <>
            <ul className="divide-y divide-border-base" data-testid="agent-activity-list">
              {/* #274: fold consecutive Checking events over the full accumulated set. */}
              {groupActivity(activityEvents).map((item) =>
                item.kind === 'checking-group' ? (
                  <CheckingGroup key={item.events[0].id} events={item.events} />
                ) : item.kind === 'executor-progress-group' ? (
                  <ExecutorProgressGroup key={item.events[0].id} events={item.events} />
                ) : (
                  <AgentActivityRow key={item.event.id} event={item.event} />
                ),
              )}
            </ul>
            {/* #274: cursor-paginated "Load older" (oldest events fetched on demand);
                terminal state when next_cursor=null (末页). */}
            {activity.hasNextPage ? (
              <button
                type="button"
                className="mt-2 flex w-full items-center justify-center rounded border border-border-base px-2 py-1.5 text-text-secondary hover:bg-bg-subtle disabled:opacity-50"
                data-testid="agent-activity-load-older"
                onClick={() => void activity.fetchNextPage()}
                disabled={activity.isFetchingNextPage}
                aria-busy={activity.isFetchingNextPage}
                aria-label={t('agents.detail.activity.loadOlder')}
                title={t('agents.detail.activity.loadOlder')}
              >
                {/* v2.8.1 UX (@oopslink): icon-only — chevron-up = "load earlier
                    from the top"; swaps to a spinner while fetching. The semantic
                    label stays on aria-label/title for screen readers + hover. */}
                {activity.isFetchingNextPage ? (
                  <span
                    className="h-4 w-4 animate-spin rounded-full border-2 border-border-base border-t-brand"
                    aria-hidden="true"
                  />
                ) : (
                  <ChevronUpIcon />
                )}
              </button>
            ) : (
              <p className="mt-2 text-center text-xs text-text-muted" data-testid="agent-activity-end">
                {t('agents.detail.activity.end')}
              </p>
            )}
          </>
        )}
      </section>
      )}

      {resetOpen && (
        <ResetModal
          pending={reset.isPending}
          onClose={() => setResetOpen(false)}
          onConfirm={(scope) => {
            reset.mutate(
              { scope, confirm: true },
              { onSuccess: () => setResetOpen(false) },
            );
          }}
        />
      )}

      {/* #270: stop/restart二次确认 (disruptive — interrupts a running agent). */}
      <ConfirmModal
        open={confirmAction !== null}
        title={confirmAction === 'restart' ? t('agents.detail.confirmRestart.title') : t('agents.detail.confirmStop.title')}
        message={
          confirmAction === 'restart' ? (
            <Trans i18nKey="agents.detail.confirmRestart.message" t={t} values={{ name: a.name }}>
              Restart <strong>{{ name: a.name } as never}</strong>? Its current run is interrupted and
              the agent is started again.
            </Trans>
          ) : (
            <Trans i18nKey="agents.detail.confirmStop.message" t={t} values={{ name: a.name }}>
              Stop <strong>{{ name: a.name } as never}</strong>? Any in-progress work is interrupted
              until it is started again.
            </Trans>
          )
        }
        confirmLabel={confirmAction === 'restart' ? t('agents.detail.controls.restartTitle') : t('agents.detail.controls.stopTitle')}
        busy={stop.isPending || restart.isPending}
        onConfirm={() => {
          const m = confirmAction === 'restart' ? restart : stop;
          m.mutate(undefined, { onSuccess: () => setConfirmAction(null) });
        }}
        onCancel={() => setConfirmAction(null)}
      />

      {/* #270/#272: archive二次确认. Soft-delete — terminal, releases the worker,
          preserves history (tasks/conversations); shown as "(archived)". */}
      <ConfirmModal
        open={archiveOpen}
        title={t('agents.detail.confirmArchive.title')}
        message={
          <Trans i18nKey="agents.detail.confirmArchive.message" t={t} values={{ name: a.name }}>
            Archiving <strong>{{ name: a.name } as never}</strong> removes it from the active agent
            list and releases its worker. Its history (tasks, conversations) is
            preserved and it will show as “(archived)”. This cannot be undone.
          </Trans>
        }
        confirmLabel={t('agents.detail.confirmArchive.confirm')}
        danger
        busy={archive.isPending}
        onConfirm={() =>
          archive.mutate(undefined, { onSuccess: () => setArchiveOpen(false) })
        }
        onCancel={() => setArchiveOpen(false)}
      />

      {/* v2.8.1: force-delete (admin) — GitHub-style typed-name confirm. On 200
          navigate back to the agents list; on 409/error keep the modal open and
          surface the message via the `error` prop. */}
      <ForceDeleteModal
        open={forceDeleteOpen}
        entityKind="agent"
        entityName={a.name}
        busy={forceDelete.isPending}
        error={forceDeleteError}
        onConfirm={() => {
          setForceDeleteError(null);
          forceDelete.mutate(a.id, {
            onSuccess: () => {
              setForceDeleteOpen(false);
              const slug = org?.slug;
              navigate(slug ? `/organizations/${slug}/agents` : '/agents');
            },
            onError: (e) => setForceDeleteError((e as Error).message),
          });
        }}
        onCancel={() => setForceDeleteOpen(false)}
      />
    </section>
  );
}

function SandboxDesktopModal({
  agentId,
  agentName,
  onClose,
  onPopOut,
}: {
  agentId: string;
  agentName: string;
  onClose: () => void;
  onPopOut: () => void;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const session = useAgentSandboxDesktopSession(agentId, true);
  const screenRef = useRef<HTMLDivElement | null>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<'idle' | 'connecting' | 'connected' | 'disconnected' | 'failed'>('idle');
  const [passwordRequired, setPasswordRequired] = useState(false);
  const [password, setPassword] = useState('');
  const [connectionError, setConnectionError] = useState<string | null>(null);

  useEffect(() => {
    if (!session.data?.ok || !session.data.websocket_url || !screenRef.current || rfbRef.current) {
      return undefined;
    }
    setConnectionStatus('connecting');
    setConnectionError(null);
    const rfb = new RFB(screenRef.current, sandboxDesktopWebsocketURL(session.data.websocket_url), {
      shared: true,
    });
    rfb.scaleViewport = true;
    rfb.resizeSession = false;
    rfb.showDotCursor = true;
    rfb.background = '#111827';
    rfbRef.current = rfb;

    const onConnect = () => {
      setConnectionStatus('connected');
      setPasswordRequired(false);
      setConnectionError(null);
    };
    const onDisconnect = (event: Event) => {
      const detail = (event as CustomEvent<{ clean?: boolean }>).detail;
      setConnectionStatus(detail?.clean ? 'disconnected' : 'failed');
      if (!detail?.clean) setConnectionError(t('agents.detail.sandbox.desktopDisconnected'));
    };
    const onCredentialsRequired = () => {
      setPasswordRequired(true);
      setConnectionStatus('connecting');
    };
    const onSecurityFailure = (event: Event) => {
      const detail = (event as CustomEvent<{ reason?: string }>).detail;
      setConnectionStatus('failed');
      setConnectionError(detail?.reason || t('agents.detail.sandbox.desktopSecurityFailed'));
    };

    rfb.addEventListener('connect', onConnect);
    rfb.addEventListener('disconnect', onDisconnect);
    rfb.addEventListener('credentialsrequired', onCredentialsRequired);
    rfb.addEventListener('securityfailure', onSecurityFailure);

    return () => {
      rfb.removeEventListener('connect', onConnect);
      rfb.removeEventListener('disconnect', onDisconnect);
      rfb.removeEventListener('credentialsrequired', onCredentialsRequired);
      rfb.removeEventListener('securityfailure', onSecurityFailure);
      rfb.disconnect();
      rfbRef.current = null;
    };
  }, [session.data?.ok, session.data?.websocket_url, t]);

  const sendPassword = () => {
    if (!password) return;
    rfbRef.current?.sendCredentials({ password });
    setPasswordRequired(false);
    setPassword('');
  };

  const sessionMessage =
    session.isLoading ? t('agents.detail.sandbox.desktopLoading') :
    session.isError ? (session.error as Error).message :
    session.data?.status === 'unreachable' ? (session.data.message || t('agents.detail.sandbox.desktopUnreachable')) :
    session.data && !session.data.ok ? (session.data.message || t('agents.detail.sandbox.desktopNotConfigured')) :
    null;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4"
      role="dialog"
      aria-modal="true"
      data-testid="agent-sandbox-desktop-modal"
    >
      <div className="flex h-[min(760px,92vh)] w-[min(1180px,96vw)] flex-col rounded-lg border border-border-base bg-bg-elevated shadow-xl">
        <div className="flex items-center justify-between gap-3 border-b border-border-base px-4 py-3">
          <div>
            <h2 className="text-sm font-semibold text-text-primary">
              {t('agents.detail.sandbox.desktopTitle', { name: agentName })}
            </h2>
            <p className="text-xs text-text-muted">
              {t('agents.detail.sandbox.desktopStatus', { status: connectionStatus })}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={onPopOut}
              className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              title={t('agents.detail.sandbox.desktopPopOutTitle')}
              aria-label={t('agents.detail.sandbox.desktopPopOutTitle')}
              data-testid="agent-sandbox-desktop-popout"
            >
              <PopOutIcon />
            </button>
            <button
              type="button"
              onClick={onClose}
              className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
              data-testid="agent-sandbox-desktop-close"
            >
              {t('agents.detail.sandbox.desktopClose')}
            </button>
          </div>
        </div>
        <div className="relative min-h-0 flex-1 bg-black">
          <div ref={screenRef} className="h-full w-full overflow-hidden" data-testid="agent-sandbox-desktop-screen" />
          {sessionMessage && (
            <div className="absolute inset-0 flex items-center justify-center p-6">
              <div className="max-w-md rounded border border-border-base bg-bg-elevated p-4 text-sm text-text-primary shadow">
                {sessionMessage}
              </div>
            </div>
          )}
          {connectionError && (
            <div className="absolute bottom-3 left-3 rounded border border-danger/40 bg-bg-elevated px-3 py-2 text-xs text-danger shadow">
              {connectionError}
            </div>
          )}
          {passwordRequired && (
            <form
              className="absolute left-1/2 top-1/2 flex w-[min(360px,calc(100%-32px))] -translate-x-1/2 -translate-y-1/2 flex-col gap-2 rounded border border-border-base bg-bg-elevated p-4 shadow"
              onSubmit={(e) => {
                e.preventDefault();
                sendPassword();
              }}
            >
              <label className="text-xs font-medium text-text-primary" htmlFor="sandbox-vnc-password">
                {t('agents.detail.sandbox.desktopPassword')}
              </label>
              <input
                id="sandbox-vnc-password"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary"
                autoComplete="off"
                data-testid="agent-sandbox-desktop-password"
              />
              <button
                type="submit"
                disabled={!password}
                className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:opacity-50"
                data-testid="agent-sandbox-desktop-password-submit"
              >
                {t('agents.detail.sandbox.desktopConnect')}
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  );
}

// ResetModal — scope select + a SECOND confirmation checkbox before the
// destructive reset fires with confirm:true.
function ResetModal({
  pending,
  onClose,
  onConfirm,
}: {
  pending: boolean;
  onClose: () => void;
  onConfirm: (scope: ResetScope) => void;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const [scope, setScope] = useState<ResetScope>('memory');
  const [confirmed, setConfirmed] = useState(false);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      data-testid="agent-reset-modal"
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="text-lg font-semibold">{t('agents.detail.reset.title')}</h2>
        <p className="mt-1 text-xs text-text-muted">
          {t('agents.detail.reset.description')}
        </p>

        <label className="mt-4 mb-1 block text-xs font-medium text-text-primary">
          {t('agents.detail.reset.scope')}
        </label>
        <select
          value={scope}
          onChange={(e) => setScope(e.target.value as ResetScope)}
          className="block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary focus:border-accent"
          data-testid="agent-reset-scope"
        >
          <option value="memory">{t('agents.detail.reset.scopeMemory')}</option>
          <option value="workspace">{t('agents.detail.reset.scopeWorkspace')}</option>
          <option value="all">{t('agents.detail.reset.scopeAll')}</option>
        </select>

        <label className="mt-4 flex items-center gap-2 text-xs text-text-primary">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(e) => setConfirmed(e.target.checked)}
            data-testid="agent-reset-confirm"
          />
          {t('agents.detail.reset.understand', { scope })}
        </label>

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            data-testid="agent-reset-cancel"
          >
            {t('agents.detail.reset.cancel')}
          </button>
          <button
            type="button"
            disabled={!confirmed || pending}
            onClick={() => onConfirm(scope)}
            className="rounded bg-danger px-3 py-1.5 text-sm font-medium text-white hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
            data-testid="agent-reset-submit"
          >
            {pending ? t('agents.detail.reset.submitting') : t('agents.detail.reset.submit')}
          </button>
        </div>
      </div>
    </div>
  );
}

function SandboxActionButton({
  action,
  pending,
  disabled = false,
  danger = false,
  title,
  ariaLabel,
  testId,
  onAction,
  children,
}: {
  action: SandboxAction;
  pending: boolean;
  disabled?: boolean;
  danger?: boolean;
  title: string;
  ariaLabel: string;
  testId: string;
  onAction: (action: SandboxAction) => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={() => onAction(action)}
      disabled={pending || disabled}
      className={`flex min-h-[36px] min-w-[36px] items-center justify-center rounded border px-2 py-1.5 disabled:opacity-50 ${
        danger
          ? 'border-danger/40 text-danger hover:bg-danger/10'
          : 'border-border-base text-text-primary hover:bg-bg-subtle'
      }`}
      data-testid={testId}
      title={title}
      aria-label={ariaLabel}
      aria-busy={pending}
    >
      {children}
    </button>
  );
}

function formatLocalTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatAgeFrom(value: string): string {
  const date = new Date(value).getTime();
  if (Number.isNaN(date)) return '—';
  const seconds = Math.max(0, Math.floor((Date.now() - date) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h ${minutes % 60}m`;
  return `${Math.floor(hours / 24)}d ${hours % 24}h`;
}

// v2.7.1 #240: chat-bubble icon for the header "Send message" action
// (no-emoji UX rule — inline single-stroke SVG, matching the composer icons).
function ChatBubbleIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path
        d="M4 5.5A1.5 1.5 0 0 1 5.5 4h9A1.5 1.5 0 0 1 16 5.5v6a1.5 1.5 0 0 1-1.5 1.5H8l-3.5 3v-3H5.5A1.5 1.5 0 0 1 4 11.5v-6z"
        strokeLinejoin="round"
      />
    </svg>
  );
}

// v2.8 #271: the Start control icon (a triangular play glyph) — #250 icon-ified
// Stop/Restart/Reset/Message but left Start as text; this completes the set.
function PlayIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" className="h-4 w-4" aria-hidden="true">
      <path d="M6 4.5v11l9-5.5-9-5.5z" />
    </svg>
  );
}

// v2.8 #270: the Archive control icon (a box with a slot) for soft-archive.
function ArchiveIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M3 5.5h14v3H3v-3z" strokeLinejoin="round" />
      <path d="M4.5 8.5v6h11v-6M8 11h4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// v2.8.1: the Force-delete control icon (a trash can) — admin escape hatch.
function TrashIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4 6h12M8 6V4.5h4V6m-6 0v9.5h8V6" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M9 9v4M11 9v4" strokeLinecap="round" />
    </svg>
  );
}

// v2.7.1 #250: lifecycle control icons (no-emoji UX rule — inline single-stroke
// 20×20 SVGs, matching ChatBubbleIcon / the composer icons).
function StopIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="5.5" y="5.5" width="9" height="9" rx="1" strokeLinejoin="round" />
    </svg>
  );
}

function RestartIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M15.5 6.5a6 6 0 1 0 1.2 4" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M16 3.5v3.2h-3.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function ResetIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M4.5 6.5a6 6 0 1 1-1.2 4" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M4 3.5v3.2h3.2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function MonitorIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3.5" y="4.5" width="13" height="9" rx="1.5" strokeLinejoin="round" />
      <path d="M8 16h4M10 13.5V16" strokeLinecap="round" />
    </svg>
  );
}

function BrowserIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <rect x="3.5" y="4.5" width="13" height="11" rx="1.5" strokeLinejoin="round" />
      <path d="M4 8h12M7 6.2h.01M9 6.2h.01" strokeLinecap="round" />
    </svg>
  );
}

function PopOutIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M8 5H5.5A1.5 1.5 0 0 0 4 6.5v8A1.5 1.5 0 0 0 5.5 16h8a1.5 1.5 0 0 0 1.5-1.5V12" strokeLinecap="round" strokeLinejoin="round" />
      <path d="M11 4h5v5M10 10l5.5-5.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

// v2.8.1 #274: chevron-up = "load older/earlier events from the top".
function ChevronUpIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M5 12.5l5-5 5 5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
