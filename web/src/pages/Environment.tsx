import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useFleet } from '@/api/fleet';
import { formatLocalTime } from '@/utils/time';
import { useAgents } from '@/api/agents';
import { useDisplayNameResolver } from '@/api/members';
import { useTransferSessions } from '@/api/workers';
import { withOrgSlug } from '@/api/client';
import { useOptionalOrgContext, OrgLink } from '@/OrgContext';
import type { Agent, FleetWorkerRow, TransferSession, TaskExecRow, FleetIssueRow } from '@/api/types';
import { useTablistKeyboard } from '@/components/useTablistKeyboard';
import { refLabel } from '@/components/workItemDisplay';
import { LifecycleBadge } from '@/components/AgentBadges';
import { AddWorkerModal } from '@/components/AddWorkerModal';
import { InstallCommandModal } from '@/components/InstallCommandModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { useSystemSegments } from './useSystemSegments';

// Environment page (/environment). v2.7 #164: Fleet merged into Environment — this
// is the single operational page for the organization's workers + agents + work
// items + file transfers. v2.8.1 #281: visual redesign (mockup-locked) — a stats
// strip, a 3-section worker card (header / CLI / Agents), and a single Activity
// tablist that folds the old separate Work-items / Issues / Transfers sections.
// All data still comes from the same hooks (/api/fleet, /api/agents,
// /api/files/transfers) — no backend change.
const HIGHLIGHT_MS = 3_000;

// The agent lifecycle that counts as "running" for the Agents-Running stat. The
// AgentLifecycle union (api/types) has an explicit 'running' state, so we count
// exactly that rather than guessing a non-stopped set.
const RUNNING_LIFECYCLE = 'running';

type InstallCommandModalState = { workerID: string; mode: 'show' | 'remint' } | null;

// ---------------------------------------------------------------------------
// Inline SVG icons (a11y: no emoji / unicode-glyph affordances — #211 guard).
// All strokes use currentColor so they inherit the surrounding text token and
// stay both-mode AA. aria-hidden — the adjacent text carries the meaning.
// ---------------------------------------------------------------------------
function MonitorIcon({ className }: { className?: string }): React.ReactElement {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <rect x="2" y="3" width="20" height="14" rx="2" />
      <path d="M8 21h8M12 17v4" />
    </svg>
  );
}

function TerminalIcon({ className }: { className?: string }): React.ReactElement {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M4 17l6-5-6-5M12 19h8" />
    </svg>
  );
}

function UserIcon({ className }: { className?: string }): React.ReactElement {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="8" r="4" />
      <path d="M4 21v-1a6 6 0 0 1 6-6h4a6 6 0 0 1 6 6v1" />
    </svg>
  );
}

function ClockIcon({ className }: { className?: string }): React.ReactElement {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7v5l3 3" />
    </svg>
  );
}

function ChevronIcon({ className }: { className?: string }): React.ReactElement {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d="M9 18l6-6-6-6" />
    </svg>
  );
}

// Above this many bound agents, a worker's Agents list collapses by default
// (owner request) — the header becomes a toggle and the grid is hidden until
// the user expands it. ≤ threshold keeps the list always-open & non-collapsible.
const AGENTS_COLLAPSE_THRESHOLD = 3;

export default function Environment(): React.ReactElement {
  const { t } = useTranslation('admin');
  const systemSegments = useSystemSegments();
  const fleet = useFleet();
  const agents = useAgents();
  const transfers = useTransferSessions();
  const orgCtx = useOptionalOrgContext();
  const base = orgCtx ? `/organizations/${orgCtx.slug}` : '';

  const [modalOpen, setModalOpen] = useState(false);
  const [installModal, setInstallModal] = useState<InstallCommandModalState>(null);
  const [highlighted, setHighlighted] = useState<Record<string, number>>({});

  useEffect(() => {
    const handler = (ev: Event) => {
      const detail = (ev as CustomEvent<{ worker_id?: string }>).detail || {};
      if (!detail.worker_id) return;
      const id = detail.worker_id;
      setHighlighted((prev) => ({ ...prev, [id]: Date.now() + HIGHLIGHT_MS }));
      setTimeout(() => {
        setHighlighted((prev) => {
          if (!prev[id]) return prev;
          const next = { ...prev };
          delete next[id];
          return next;
        });
      }, HIGHLIGHT_MS);
    };
    window.addEventListener('agent-center:worker-enrolled', handler);
    return () => window.removeEventListener('agent-center:worker-enrolled', handler);
  }, []);

  const agentsByWorker = (workerID: string): Agent[] =>
    (agents.data ?? []).filter((a) => a.worker_id === workerID);

  // Stats — derived from the already-loaded snapshots. Counts default to 0 when
  // the data hasn't resolved yet, so the cells render "0" gracefully.
  const workers = fleet.data?.workers ?? [];
  const workItems = fleet.data?.tasks ?? [];
  const pendingIssues = fleet.data?.pending_issues ?? [];
  const workersOnline = workers.filter((w) => w.status === 'online').length;
  const agentsRunning = (agents.data ?? []).filter((a) => a.lifecycle === RUNNING_LIFECYCLE).length;

  return (
    <section className="space-y-6" data-testid="page-Environment">
      {/* v2.10.1 [M7] Mobile (<md): System module 二级段控 (Environment |
          Settings) — desktop keeps the col② nav. */}
      <SegmentedNav items={systemSegments.segments} ariaLabel={systemSegments.ariaLabel} />
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">{t('environment.title')}</h1>
          <p className="text-xs text-text-muted">
            {t('environment.subtitle')}
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setModalOpen(true)}
          data-testid="environment-add-worker-btn"
        >
          {t('environment.addWorker')}
        </button>
      </header>

      {modalOpen && <AddWorkerModal onClose={() => setModalOpen(false)} />}
      {installModal && (
        <InstallCommandModal
          workerID={installModal.workerID}
          mode={installModal.mode}
          onClose={() => setInstallModal(null)}
        />
      )}

      {/* Stats strip — four big-number cells. Agents-Running is the only
          conditional-color cell (green when >0), but the number + label carry
          the meaning so it is not color-only (#211). */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4" data-testid="environment-stats">
        <StatCell
          testId="environment-stat-workers-online"
          value={workersOnline}
          label={t('environment.stats.workersOnline')}
        />
        <StatCell
          testId="environment-stat-agents-running"
          value={agentsRunning}
          label={t('environment.stats.agentsRunning')}
          valueClassName={agentsRunning > 0 ? 'text-success' : 'text-text-muted'}
        />
        <StatCell
          testId="environment-stat-tasks"
          value={workItems.length}
          label={t('environment.stats.tasks')}
        />
        <StatCell
          testId="environment-stat-pending-issues"
          value={pendingIssues.length}
          label={t('environment.stats.pendingIssues')}
        />
      </div>

      {fleet.isLoading && (
        <p className="text-sm text-text-muted" data-testid="environment-loading">
          {t('environment.loading')}
        </p>
      )}
      {fleet.isError && (
        <p className="text-sm text-danger" data-testid="environment-error">
          {(fleet.error as Error).message}
        </p>
      )}

      {fleet.data?.warnings && fleet.data.warnings.length > 0 && (
        <div
          className="rounded border border-warning/40 bg-warning/10 p-3 text-sm text-warning"
          data-testid="environment-warnings"
        >
          <p className="font-medium">{t('environment.partialSnapshot')}</p>
          <ul className="ml-4 list-disc text-xs">
            {fleet.data.warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}

      {fleet.isSuccess && (
        <Section title={t('environment.workers.title')}>
          {fleet.data.workers.length === 0 ? (
            <div
              className="rounded border border-dashed border-border-strong bg-bg-subtle p-6 text-center"
              data-testid="environment-workers-empty"
            >
              <p className="text-sm text-text-secondary">{t('environment.workers.emptyTitle')}</p>
              <p className="mt-2 text-xs text-text-muted">
                {t('environment.workers.emptyHint')}
              </p>
              <button
                type="button"
                className="mt-4 rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover"
                onClick={() => setModalOpen(true)}
                data-testid="environment-workers-empty-cta"
              >
                {t('environment.workers.emptyCta')}
              </button>
            </div>
          ) : (
            <ul className="space-y-3" data-testid="environment-workers">
              {fleet.data.workers.map((wk) => (
                <WorkerCard
                  key={wk.worker_id}
                  worker={wk}
                  agents={agentsByWorker(wk.worker_id)}
                  flashing={Boolean(highlighted[wk.worker_id])}
                  onShowInstall={() => setInstallModal({ workerID: wk.worker_id, mode: 'show' })}
                  onReMintInstall={() => setInstallModal({ workerID: wk.worker_id, mode: 'remint' })}
                />
              ))}
            </ul>
          )}
        </Section>
      )}

      <ActivitySection
        base={base}
        workItems={fleet.isSuccess ? fleet.data.tasks : []}
        issues={fleet.isSuccess ? fleet.data.pending_issues : []}
        transfers={transfers}
      />
    </section>
  );
}

// StatCell — one big-number / small-label stat card. valueClassName lets the
// caller tint the number (Agents-Running goes green when >0).
function StatCell({
  value,
  label,
  testId,
  valueClassName = 'text-text-primary',
}: {
  value: number;
  label: string;
  testId: string;
  valueClassName?: string;
}): React.ReactElement {
  return (
    <div
      className="rounded border border-border-base bg-bg-elevated p-4"
      data-testid={testId}
    >
      <div className={`text-2xl font-bold sm:text-3xl ${valueClassName}`} data-testid={`${testId}-value`}>
        {value}
      </div>
      <div className="mt-1 text-xs text-text-muted">{label}</div>
    </div>
  );
}

// WorkerCard — one worker, three visually-separated sub-sections:
// header (name + status dot + active count + heartbeat + Remove), a CLI
// sub-section (probed capabilities), and an Agents sub-section (bound agents).
function WorkerCard({
  worker,
  agents,
  flashing,
  onShowInstall,
  onReMintInstall,
}: {
  worker: FleetWorkerRow;
  agents: Agent[];
  flashing: boolean;
  onShowInstall: () => void;
  onReMintInstall: () => void;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const online = worker.status === 'online';
  // Collapse the Agents list by default when it's long (> threshold). State is
  // seeded once from the initial count; if SSE later pushes the count past the
  // threshold the section stays in whatever state the user left it.
  const agentsCollapsible = agents.length > AGENTS_COLLAPSE_THRESHOLD;
  const [agentsOpen, setAgentsOpen] = useState(!agentsCollapsible);
  return (
    <li
      className={`rounded border border-border-base bg-bg-elevated p-4 ${
        flashing
          ? 'motion-safe:animate-pulse bg-success/10 motion-safe:transition-colors motion-safe:duration-700'
          : ''
      }`}
      data-testid="environment-worker"
      data-worker-id={worker.worker_id}
      data-status={worker.status}
      data-just-enrolled={flashing ? 'true' : undefined}
    >
      {/* Header row */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2">
          <MonitorIcon className="mt-0.5 h-4 w-4 shrink-0 text-text-muted" />
          <WorkerNameCell worker={worker} />
        </div>
        <div className="flex items-center gap-3">
          {/* status: dot + uppercase text — not color-only (#211). */}
          <span
            className="inline-flex items-center gap-1.5 rounded bg-bg-subtle px-2 py-0.5 text-xs uppercase text-text-secondary"
            data-testid="environment-worker-status"
            data-status={worker.status}
          >
            <span
              className={`inline-block h-1.5 w-1.5 rounded-full ${online ? 'bg-success' : 'bg-text-muted'}`}
              aria-hidden="true"
            />
            {worker.status}
          </span>
          <span className="font-mono text-xs text-text-muted" data-testid="environment-worker-active">
            {t('environment.worker.active', { count: worker.active_count })}
          </span>
          <span className="text-xs text-text-muted">
            {worker.last_heartbeat_at
              ? t('environment.worker.heartbeat', { time: formatLocalTime(worker.last_heartbeat_at) })
              : t('environment.worker.heartbeatNone')}
          </span>
          <RemoveWorkerButton worker={worker} />
        </div>
      </div>

      {/* CLI sub-section */}
      <div className="mt-3 border-t border-border-base pt-3">
        <p className="mb-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
          {t('environment.worker.cli')}
        </p>
        <WorkerCapabilities worker={worker} />
        <WorkerInstallActions
          worker={worker}
          onShowInstall={onShowInstall}
          onReMintInstall={onReMintInstall}
        />
      </div>

      {/* Agents sub-section */}
      <div className="mt-3 border-t border-border-base pt-3">
        {agentsCollapsible ? (
          <button
            type="button"
            onClick={() => setAgentsOpen((v) => !v)}
            aria-expanded={agentsOpen}
            className="mb-1.5 flex w-full items-center gap-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted hover:text-text-secondary"
            data-testid="environment-worker-agents-toggle"
          >
            <ChevronIcon
              className={`h-3 w-3 shrink-0 transition-transform ${agentsOpen ? 'rotate-90' : ''}`}
            />
            <span>{t('environment.worker.agents')}</span>
            <span className="font-mono normal-case tracking-normal text-text-muted">
              ({agents.length})
            </span>
          </button>
        ) : (
          <p className="mb-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
            {t('environment.worker.agents')}
          </p>
        )}
        {agents.length === 0 ? (
          <p className="text-xs text-text-muted" data-testid="environment-worker-noagents">
            {t('environment.worker.noAgents')}
          </p>
        ) : agentsCollapsible && !agentsOpen ? null : (
          // T143: a SHARED grid so every row's columns line up (name / CLI /
          // model / lifecycle). Each <li> is `display:contents` so its cells join
          // the parent grid — the name column (minmax(0,max-content)) sizes to the
          // widest name yet truncates on a narrow card, and the badge columns
          // (max-content) align across rows. gap-x-4 gives the name → rest spacing
          // the owner asked for. Every column renders a cell (a "—" placeholder
          // when absent) so a missing CLI/model never shifts the alignment.
          <ul
            className="grid grid-cols-[minmax(0,max-content)_max-content_max-content_max-content] items-center gap-x-4 gap-y-1.5"
            data-testid="environment-worker-agents"
          >
            {agents.map((a) => (
              <li
                key={a.id}
                className="contents"
                data-testid="environment-agent"
                data-agent-id={a.id}
              >
                {/* Col 1: icon + clickable NAME → AgentDetail (replaces "Open →"). */}
                <OrgLink
                  to={`/agents/${encodeURIComponent(a.id)}`}
                  className="flex min-w-0 items-center gap-2 text-sm font-medium text-accent hover:underline"
                  data-testid="environment-agent-link"
                  title={a.name}
                >
                  <UserIcon className="h-4 w-4 shrink-0 text-text-muted" />
                  <span className="truncate">{a.name}</span>
                </OrgLink>
                {/* Col 2: CLI / provider */}
                {a.cli ? (
                  <span
                    className="justify-self-start rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.6875rem] text-text-secondary"
                    data-testid="environment-agent-cli"
                    data-cli={a.cli}
                  >
                    {a.cli}
                  </span>
                ) : (
                  <span className="text-[0.6875rem] text-text-muted">—</span>
                )}
                {/* Col 3: model */}
                {a.model ? (
                  <span
                    className="justify-self-start rounded bg-accent/10 px-1.5 py-0.5 text-[0.6875rem] text-accent"
                    data-testid="environment-agent-model"
                    data-model={a.model}
                  >
                    {a.model}
                  </span>
                ) : (
                  <span className="text-[0.6875rem] text-text-muted">—</span>
                )}
                {/* Col 4: lifecycle / status */}
                <span className="justify-self-start">
                  <LifecycleBadge lifecycle={a.lifecycle} />
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}

// ---------------------------------------------------------------------------
// Activity section — a 4-tab tablist (All / Work Items / Issues / Transfers)
// folding the old three separate sections into one stream switcher. WAI-ARIA
// manual-activation keyboard nav via useTablistKeyboard (mirrors WorkerDetail).
// ---------------------------------------------------------------------------
const ACTIVITY_TABS = [
  { key: 'all' },
  { key: 'tasks' },
  { key: 'issues' },
  { key: 'transfers' },
] as const;
type ActivityTab = (typeof ACTIVITY_TABS)[number]['key'];

function ActivitySection({
  base,
  workItems,
  issues,
  transfers,
}: {
  base: string;
  workItems: TaskExecRow[];
  issues: FleetIssueRow[];
  transfers: ReturnType<typeof useTransferSessions>;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const [tab, setTab] = useState<ActivityTab>('all');
  const tablist = useTablistKeyboard({ keys: ACTIVITY_TABS.map((t) => t.key), active: tab });
  const transferRows = transfers.data ?? [];

  // The active tab's stream is empty? (transfers loading/error are handled
  // inside the panel, so only treat the resolved-empty case as "empty".)
  const tabIsEmpty =
    (tab === 'all' && workItems.length === 0 && issues.length === 0 && transferRows.length === 0) ||
    (tab === 'tasks' && workItems.length === 0) ||
    (tab === 'issues' && issues.length === 0) ||
    (tab === 'transfers' && transfers.isSuccess && transferRows.length === 0);

  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-text-primary">{t('environment.activity.title')}</h3>
        <nav
          className="flex gap-1"
          role="tablist"
          aria-orientation="horizontal"
          ref={tablist.tablistRef}
          onKeyDown={tablist.onKeyDown}
          onBlur={tablist.onBlur}
          data-testid="environment-activity-tabs"
        >
          {ACTIVITY_TABS.map((t_) => (
            <button
              key={t_.key}
              type="button"
              role="tab"
              id={`environment-activity-tab-${t_.key}`}
              aria-selected={tab === t_.key}
              aria-controls={`environment-activity-panel-${t_.key}`}
              tabIndex={tablist.tabIndexFor(t_.key)}
              onClick={() => setTab(t_.key)}
              data-testid={`environment-activity-tab-${t_.key}`}
              className={`rounded px-2.5 py-1 text-xs font-medium ${
                tab === t_.key
                  ? 'bg-bg-subtle text-text-primary'
                  : 'text-text-muted hover:text-text-primary'
              }`}
            >
              {t(`environment.activity.tabs.${t_.key}`)}
            </button>
          ))}
        </nav>
      </div>

      <div
        role="tabpanel"
        id={`environment-activity-panel-${tab}`}
        aria-labelledby={`environment-activity-tab-${tab}`}
        tabIndex={0}
        data-testid={`environment-activity-panel-${tab}`}
      >
        {tabIsEmpty ? (
          <ActivityEmpty />
        ) : (
          <>
            {tab === 'all' && (
              <AllStream base={base} workItems={workItems} issues={issues} transfers={transferRows} />
            )}
            {tab === 'tasks' && <WorkItemsList base={base} workItems={workItems} />}
            {tab === 'issues' && <IssuesList base={base} issues={issues} />}
            {tab === 'transfers' && <TransfersPanel transfers={transfers} />}
          </>
        )}
      </div>
    </section>
  );
}

function ActivityEmpty(): React.ReactElement {
  const { t } = useTranslation('admin');
  return (
    <div
      className="flex flex-col items-center gap-2 rounded border border-dashed border-border-base bg-bg-subtle p-8 text-center"
      data-testid="environment-activity-empty"
    >
      <ClockIcon className="h-8 w-8 text-text-muted" />
      <p className="text-sm font-medium text-text-secondary">{t('environment.activity.emptyTitle')}</p>
      <p className="text-xs text-text-muted">
        {t('environment.activity.emptyHint')}
      </p>
    </div>
  );
}

// A small type-indicator chip used in the unified "All" stream so each row's
// origin (work item / issue / transfer) is legible without color alone.
function TypeTag({ label, testId }: { label: string; testId: string }): React.ReactElement {
  return (
    <span
      className="shrink-0 rounded bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-text-muted"
      data-testid={testId}
    >
      {label}
    </span>
  );
}

// AllStream — union of work items + issues + transfers as unified list rows,
// each prefixed with a type tag. Reuses the same content/links as the dedicated
// tabs so a row reads identically wherever it appears.
function AllStream({
  base,
  workItems,
  issues,
  transfers,
}: {
  base: string;
  workItems: TaskExecRow[];
  issues: FleetIssueRow[];
  transfers: TransferSession[];
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const resolveAgent = useWorkItemAgentResolver(base);
  return (
    <ul
      className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
      data-testid="environment-activity-all-list"
    >
      {workItems.map((wi) => (
        <li
          key={`wi-${wi.task_id}`}
          className="flex items-center gap-2 px-3 py-2 text-xs"
          data-testid="environment-activity-all-row"
          data-kind="task"
        >
          <TypeTag label={t('environment.activity.typeTask')} testId="environment-activity-all-type" />
          <TaskExecContent base={base} wi={wi} agent={resolveAgent(wi.agent_id)} />
        </li>
      ))}
      {issues.map((i) => (
        <li
          key={`iss-${i.issue_id}`}
          className="flex items-center gap-2 px-3 py-2 text-xs"
          data-testid="environment-activity-all-row"
          data-kind="issue"
        >
          <TypeTag label={t('environment.activity.typeIssue')} testId="environment-activity-all-type" />
          <IssueContent base={base} issue={i} />
        </li>
      ))}
      {transfers.map((tr) => (
        <li
          key={`tr-${tr.id}`}
          className="flex items-center gap-2 px-3 py-2 text-xs"
          data-testid="environment-activity-all-row"
          data-kind="transfer"
        >
          <TypeTag label={t('environment.activity.typeTransfer')} testId="environment-activity-all-type" />
          <TransferContent tr={tr} />
        </li>
      ))}
    </ul>
  );
}

// v2.10.2 [T141]: resolve a work item's agent member-id → its display NAME + the
// /agents/{id} detail link. The fleet row exposes the agent's MEMBER id (#185, no
// entity-ULID leak); the execution Agent carries identity_member_id == that member
// id (#157 MembersAgents pattern) → the route id, and the members list → the name.
// Unresolved → name falls back to a clean #hash (never the raw agent-<id>) and the
// agent renders as plain text (no broken link).
function useWorkItemAgentResolver(base: string): (memberID: string) => { name: string; href: string | null } {
  const displayName = useDisplayNameResolver();
  const agents = useAgents();
  const agentIDByMember = useMemo(() => {
    const m = new Map<string, string>();
    for (const a of agents.data ?? []) {
      if (a.identity_member_id) m.set(a.identity_member_id, a.id);
    }
    return m;
  }, [agents.data]);
  return (memberID: string) => {
    if (!memberID) return { name: '', href: null };
    const resolved = displayName(memberID);
    const name = resolved === memberID ? memberID : resolved;
    const agentID = agentIDByMember.get(memberID);
    return { name, href: agentID ? `${base}/agents/${encodeURIComponent(agentID)}` : null };
  };
}

// Shared row-content renderers (kept in sync between the All stream and the
// dedicated tabs).
// v2.10.2 [T140]: render the work item as "T<n> + title" (org_ref + title) and
// link to the CORRECT project-scoped task page — /projects/{project_id}/tasks/
// {task_id} — instead of the raw "task-<id>" + the bare ${base}/tasks/{id} that
// 404'd (tasks nest under their project). refLabel shows org_ref when present,
// else the FULL id — never a retired #<id-tail> hash (T126); the link needs project_id (the
// route's required segment) — without it the row stays plain text, not a 404 link.
// v2.10.2 [T141]: the agent shows its display NAME + links to the agent detail
// page (resolved by the caller via useWorkItemAgentResolver), not the raw agent-id.
function TaskExecContent({
  base,
  wi,
  agent,
}: {
  base: string;
  wi: TaskExecRow;
  agent: { name: string; href: string | null };
}): React.ReactElement {
  const { t } = useTranslation('admin');
  const ref = refLabel(wi.task_org_ref, wi.task_id ?? '');
  const label = [ref, wi.task_title].filter(Boolean).join(' · ') || wi.task_id;
  const taskHref =
    wi.task_id && wi.project_id
      ? `${base}/projects/${encodeURIComponent(wi.project_id)}/tasks/${encodeURIComponent(wi.task_id)}`
      : null;
  return (
    <span className="flex flex-1 items-center justify-between gap-2">
      <span>
        {taskHref ? (
          <Link
            to={taskHref}
            className="text-accent hover:underline"
            data-testid="environment-workitem-task-link"
            title={wi.task_id}
          >
            {label}
          </Link>
        ) : (
          <span className="text-text-muted" title={wi.task_id}>
            {label}
          </span>
        )}{' '}
        <span className="text-text-muted">{t('environment.activity.agentLabel')}</span>{' '}
        {agent.href ? (
          <Link
            to={agent.href}
            className="text-accent hover:underline"
            data-testid="environment-workitem-agent-link"
            title={wi.agent_id}
          >
            {agent.name}
          </Link>
        ) : (
          <span className="font-mono" title={wi.agent_id}>
            {agent.name}
          </span>
        )}
        {wi.current_activity ? (
          <span className="text-text-muted"> · {wi.current_activity}</span>
        ) : null}
      </span>
      <span className="shrink-0 rounded bg-bg-subtle px-2 py-0.5 uppercase text-text-secondary">
        {wi.status}
      </span>
    </span>
  );
}

// Issuecontent links to the project-scoped issue page (T140: same path fix as the
// work item — /projects/{project_id}/issues/{id}, not the bare ${base}/issues/{id}
// that 404'd).
function IssueContent({ base, issue }: { base: string; issue: FleetIssueRow }): React.ReactElement {
  const href = issue.project_id
    ? `${base}/projects/${encodeURIComponent(issue.project_id)}/issues/${encodeURIComponent(issue.issue_id)}`
    : null;
  return href ? (
    <Link to={href} className="text-accent hover:underline">
      {issue.title}
    </Link>
  ) : (
    <span className="text-text-muted">{issue.title}</span>
  );
}

function TransferContent({ tr }: { tr: TransferSession }): React.ReactElement {
  return (
    <span className="flex flex-1 items-center gap-2">
      <span className="text-text-secondary">{tr.direction}</span>
      <span className="font-mono text-text-muted">
        {tr.scope}/{tr.scope_id}
      </span>
      <span className="text-text-muted">{tr.content_type}</span>
      <span className="ml-auto shrink-0 font-mono text-text-muted">{tr.size}</span>
    </span>
  );
}

function WorkItemsList({ base, workItems }: { base: string; workItems: TaskExecRow[] }): React.ReactElement {
  const resolveAgent = useWorkItemAgentResolver(base);
  return (
    <ul
      className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
      data-testid="environment-workitem-list"
    >
      {workItems.map((wi) => (
        <li
          key={wi.task_id}
          className="flex items-center justify-between px-3 py-2 text-xs"
          data-testid="environment-workitem-row"
          data-task-id={wi.task_id}
        >
          <TaskExecContent base={base} wi={wi} agent={resolveAgent(wi.agent_id)} />
        </li>
      ))}
    </ul>
  );
}

function IssuesList({ base, issues }: { base: string; issues: FleetIssueRow[] }): React.ReactElement {
  return (
    <ul
      className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
      data-testid="environment-issues-list"
    >
      {issues.map((i) => (
        <li key={i.issue_id} className="px-3 py-2 text-xs" data-testid="environment-issue-row">
          <IssueContent base={base} issue={i} />
        </li>
      ))}
    </ul>
  );
}

function TransfersPanel({
  transfers,
}: {
  transfers: ReturnType<typeof useTransferSessions>;
}): React.ReactElement {
  const { t } = useTranslation('admin');
  return (
    <>
      {transfers.isLoading && (
        <div data-testid="transfers-loading">
          <p className="text-xs text-text-muted">{t('environment.transfers.loading')}</p>
        </div>
      )}
      {transfers.isError && (
        <p className="text-sm text-danger" data-testid="transfers-error">
          {(transfers.error as Error).message}
        </p>
      )}
      {/* v2.10.1 [M7] Mobile (<md): the 4-column table crams at 375px, so it
          reflows to a card list (reusing TransferContent). The table is
          md:-only. */}
      {transfers.isSuccess && transfers.data.length > 0 && (
        <ul
          className="space-y-2 md:hidden"
          data-testid="transfers-cards"
        >
          {transfers.data.map((tr) => (
            <li
              key={tr.id}
              data-testid="transfer-card"
              data-transfer-id={tr.id}
              data-scope={tr.scope}
              className="rounded border border-border-base bg-bg-elevated p-3 text-xs"
            >
              <TransferContent tr={tr} />
            </li>
          ))}
        </ul>
      )}
      {transfers.isSuccess && transfers.data.length > 0 && (
        <table
          className="hidden w-full table-fixed border-separate border-spacing-0 rounded border border-border-base bg-bg-elevated text-text-primary md:table"
          data-testid="transfers-table"
        >
          <thead>
            <tr className="text-left text-xs uppercase tracking-wide text-text-muted">
              <th className="w-1/5 border-b border-border-base px-3 py-2">{t('environment.transfers.columns.direction')}</th>
              <th className="w-1/5 border-b border-border-base px-3 py-2">{t('environment.transfers.columns.scope')}</th>
              <th className="w-2/5 border-b border-border-base px-3 py-2">{t('environment.transfers.columns.content')}</th>
              <th className="border-b border-border-base px-3 py-2 text-right">{t('environment.transfers.columns.size')}</th>
            </tr>
          </thead>
          <tbody>
            {transfers.data.map((tr) => (
              <tr
                key={tr.id}
                className="text-sm"
                data-testid="transfer-row"
                data-transfer-id={tr.id}
                data-scope={tr.scope}
              >
                <td className="border-b border-border-base px-3 py-2">{tr.direction}</td>
                <td className="border-b border-border-base px-3 py-2 font-mono text-xs">
                  {tr.scope}/{tr.scope_id}
                </td>
                <td className="border-b border-border-base px-3 py-2 text-xs text-text-muted">
                  {tr.content_type}
                </td>
                <td className="border-b border-border-base px-3 py-2 text-right font-mono text-xs">
                  {tr.size}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }): React.ReactElement {
  return (
    <section>
      <h3 className="mb-2 text-sm font-semibold uppercase tracking-wide text-text-muted">{title}</h3>
      {children}
    </section>
  );
}

// WorkerNameCell: name + worker id, both linking into WorkerDetail. v2.8 #273:
// rename moved to the detail Management tab (single-directional convergence per
// PD — no inline-rename here, no duplicate-rename middle state).
function WorkerNameCell({ worker }: { worker: FleetWorkerRow }): React.ReactElement {
  const { t } = useTranslation('admin');
  const displayName = worker.name || worker.worker_id;
  return (
    <div className="flex flex-col">
      <OrgLink
        to={`/workers/${worker.worker_id}`}
        className="text-left text-sm font-medium text-text-primary hover:text-accent hover:underline"
        title={t('environment.worker.openDetailsTitle', { name: displayName })}
        data-testid="environment-worker-name"
      >
        {displayName}
      </OrgLink>
      <OrgLink
        to={`/workers/${worker.worker_id}`}
        className="font-mono text-[0.6875rem] text-text-muted hover:text-accent hover:underline"
        data-testid="environment-worker-id"
      >
        {worker.worker_id}
      </OrgLink>
    </div>
  );
}

// WorkerCapabilities renders the worker's probed agent-CLI list (v2.7 #176 /
// FINDING-C): the CLIs ProbeAllAdapters discovered, each tagged enabled or
// disabled. Only detected CLIs are shown (the operator cares about what the
// worker can actually run); a worker that has reported none shows an empty
// hint. This is the §5 "Environment 可见" exit surface.
function WorkerCapabilities({ worker }: { worker: FleetWorkerRow }): React.ReactElement {
  const { t } = useTranslation('admin');
  const detected = (worker.capabilities ?? []).filter((c) => c.detected);
  if (detected.length === 0) {
    return (
      <p className="text-xs text-text-muted" data-testid="environment-worker-nocaps">
        {t('environment.worker.noCaps')}
      </p>
    );
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5" data-testid="environment-worker-capabilities">
      <TerminalIcon className="h-3.5 w-3.5 text-text-muted" />
      {detected.map((c) => (
        <span
          key={c.agent_cli}
          className={`rounded px-1.5 py-0.5 text-xs ${
            c.enabled ? 'bg-success/15 text-success' : 'bg-bg-subtle text-text-muted'
          }`}
          data-testid="environment-worker-capability"
          data-agent-cli={c.agent_cli}
          data-detected={c.detected ? 'true' : 'false'}
          data-enabled={c.enabled ? 'true' : 'false'}
        >
          {c.agent_cli}
          {c.version ? ` ${c.version}` : ''}
          {c.enabled ? '' : t('environment.worker.disabledSuffix')}
        </span>
      ))}
      {/* v2.7 #181 / FINDING-F: detected ≠ runnable. Only claude-code executes
          in v2.7; codex/opencode are discovery-only until v2.8 (#180). Be
          explicit so the list isn't read as "all runnable". */}
      <span className="text-xs italic text-text-muted" data-testid="environment-worker-executable-note">
        {t('environment.worker.executableNote')}
      </span>
    </div>
  );
}

// WorkerInstallActions: install command (offline only) + re-mint. v2.8.1 #281:
// split out of the old WorkerRowActions — Remove now lives in the card header,
// the install actions sit under the CLI sub-section. #169: native window.confirm
// replaced with ConfirmModal.
function WorkerInstallActions({
  worker,
  onShowInstall,
  onReMintInstall,
}: {
  worker: FleetWorkerRow;
  onShowInstall: () => void;
  onReMintInstall: () => void;
}): React.ReactElement | null {
  const { t } = useTranslation('admin');
  const [confirmReMint, setConfirmReMint] = useState(false);
  if (worker.status !== 'offline') return null;
  return (
    <div className="mt-2 flex flex-wrap gap-2" data-testid="environment-worker-actions">
      <button
        type="button"
        className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
        onClick={onShowInstall}
        data-testid="environment-worker-show-install"
      >
        {t('environment.worker.showInstall')}
      </button>
      <button
        type="button"
        className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
        onClick={() => setConfirmReMint(true)}
        data-testid="environment-worker-remint-install"
      >
        {t('environment.worker.remintInstall')}
      </button>
      <ConfirmModal
        open={confirmReMint}
        title={t('environment.worker.remintConfirmTitle')}
        message={t('environment.worker.remintConfirmMessage')}
        confirmLabel={t('environment.worker.remintConfirmLabel')}
        onConfirm={() => {
          setConfirmReMint(false);
          onReMintInstall();
        }}
        onCancel={() => setConfirmReMint(false)}
      />
    </div>
  );
}

// RemoveWorkerButton: DELETE /api/workers/{id} after confirm. Ported from Fleet
// (#164). #169: native window.confirm replaced with ConfirmModal.
function RemoveWorkerButton({ worker }: { worker: FleetWorkerRow }): React.ReactElement {
  const { t } = useTranslation('admin');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const confirmMessage =
    worker.status === 'online'
      ? t('environment.worker.removeConfirmMessageOnline', { name: worker.name || worker.worker_id })
      : t('environment.worker.removeConfirmMessageOffline', { name: worker.name || worker.worker_id });
  const handleRemove = async () => {
    setBusy(true);
    setError(null);
    try {
      const resp = await fetch(withOrgSlug(`/api/workers/${encodeURIComponent(worker.worker_id)}`), {
        method: 'DELETE',
      });
      if (!resp.ok && resp.status !== 204) {
        let detail = `HTTP ${resp.status}`;
        try {
          const body = (await resp.json()) as { message?: string };
          if (body.message) detail = body.message;
        } catch {
          // ignore parse failure
        }
        throw new Error(detail);
      }
      setConfirmOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      setConfirmOpen(false);
    } finally {
      setBusy(false);
    }
  };
  return (
    <span className="flex items-center gap-2">
      {error && (
        <span className="text-xs text-danger" data-testid="environment-worker-remove-error">
          {error}
        </span>
      )}
      <button
        type="button"
        disabled={busy}
        className="rounded border border-danger/40 px-2 py-1 text-xs text-danger hover:bg-danger/10 disabled:cursor-not-allowed disabled:text-text-muted"
        onClick={() => setConfirmOpen(true)}
        data-testid="environment-worker-remove"
      >
        {busy ? t('environment.worker.removing') : t('environment.worker.remove')}
      </button>
      <ConfirmModal
        open={confirmOpen}
        title={t('environment.worker.removeConfirmTitle')}
        message={confirmMessage}
        confirmLabel={t('environment.worker.removeConfirmLabel')}
        danger
        busy={busy}
        onConfirm={() => void handleRemove()}
        onCancel={() => setConfirmOpen(false)}
      />
    </span>
  );
}
