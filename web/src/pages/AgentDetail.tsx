import type React from 'react';
import { useState } from 'react';
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
  useStartAgent,
  useStopAgent,
  type ResetScope,
} from '@/api/agents';
import { AgentBacklogBadge, AgentLoadBadge, AvailabilityBadge, LifecycleBadge } from '@/components/AgentBadges';
import { ConfirmModal } from '@/components/ConfirmModal';
import { ForceDeleteModal } from '@/components/ForceDeleteModal';
import { AgentActivityRow, CheckingGroup, ExecutorProgressGroup } from '@/components/AgentActivityRow';
import { groupActivity } from '@/components/agentActivityGrouping';
import { AgentProfile } from '@/components/AgentProfile';
import { AgentRuntime } from '@/components/AgentRuntime';
import { AgentTasks } from '@/components/AgentTasks';
import { AgentAnalyticsPanel } from '@/components/analytics/AgentAnalyticsPanel';
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
  { key: 'activity' },
  // I5 (T583): read-only runtime browser — memory/workspace tree + file preview.
  { key: 'runtime' },
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
  // v2.7.1 #228: active tab synced to ?tab= so a tab is shareable/bookmarkable.
  const [searchParams, setSearchParams] = useSearchParams();
  const tabParam = searchParams.get('tab');
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
  // v2.8 #270/#272 (b strict-two-step): archive only a settled (stopped/error)
  // agent — a running agent must be stopped first (backend also 409-guards).
  const canArchive = lc === 'stopped' || lc === 'error';

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
          {canReset && (
            <button
              type="button"
              onClick={() => setResetOpen(true)}
              disabled={transient || reset.isPending}
              className="flex items-center rounded border border-danger/40 px-2 py-1.5 text-danger hover:bg-danger/10 disabled:opacity-50"
              data-testid="agent-reset-btn"
              title={t('agents.detail.controls.resetTitle')}
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

      {tab === 'profile' && <AgentProfile agent={a} />}

      {/* I5 (T583): read-only runtime browser. */}
      {tab === 'runtime' && <AgentRuntime agentId={id} />}

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

// v2.8.1 #274: chevron-up = "load older/earlier events from the top".
function ChevronUpIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.5" aria-hidden="true">
      <path d="M5 12.5l5-5 5 5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
