import React, { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { useFleet } from '@/api/fleet';
import { formatLocalTime } from '@/utils/time';
import { useAgents } from '@/api/agents';
import { useTransferSessions } from '@/api/workers';
import { withOrgSlug } from '@/api/client';
import { useOptionalOrgContext, OrgLink } from '@/OrgContext';
import type { Agent, FleetWorkerRow, TransferSession, WorkItemRow, FleetIssueRow } from '@/api/types';
import { useTablistKeyboard } from '@/components/useTablistKeyboard';
import { LifecycleBadge } from '@/components/AgentBadges';
import { AddWorkerModal } from '@/components/AddWorkerModal';
import { InstallCommandModal } from '@/components/InstallCommandModal';
import { ConfirmModal } from '@/components/ConfirmModal';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { SYSTEM_SEGMENTS } from './systemSegments';

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

export default function Environment(): React.ReactElement {
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
  const workItems = fleet.data?.work_items ?? [];
  const pendingIssues = fleet.data?.pending_issues ?? [];
  const workersOnline = workers.filter((w) => w.status === 'online').length;
  const agentsRunning = (agents.data ?? []).filter((a) => a.lifecycle === RUNNING_LIFECYCLE).length;

  return (
    <section className="space-y-6" data-testid="page-Environment">
      {/* v2.10.1 [M7] Mobile (<md): System module 二级段控 (Environment |
          Settings) — desktop keeps the col② nav. */}
      <SegmentedNav items={SYSTEM_SEGMENTS} ariaLabel="System sections" />
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Environment</h1>
          <p className="text-xs text-text-muted">
            Compute resources in this organization, the agents bound to them,
            in-flight work items, and file transfers.
          </p>
        </div>
        <button
          type="button"
          className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover"
          onClick={() => setModalOpen(true)}
          data-testid="environment-add-worker-btn"
        >
          + Add Worker
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
          label="Workers Online"
        />
        <StatCell
          testId="environment-stat-agents-running"
          value={agentsRunning}
          label="Agents Running"
          valueClassName={agentsRunning > 0 ? 'text-success' : 'text-text-muted'}
        />
        <StatCell
          testId="environment-stat-work-items"
          value={workItems.length}
          label="Work Items"
        />
        <StatCell
          testId="environment-stat-pending-issues"
          value={pendingIssues.length}
          label="Pending Issues"
        />
      </div>

      {fleet.isLoading && (
        <p className="text-sm text-text-muted" data-testid="environment-loading">
          Loading…
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
          <p className="font-medium">Partial snapshot:</p>
          <ul className="ml-4 list-disc text-xs">
            {fleet.data.warnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}

      {fleet.isSuccess && (
        <Section title="Workers">
          {fleet.data.workers.length === 0 ? (
            <div
              className="rounded border border-dashed border-border-strong bg-bg-subtle p-6 text-center"
              data-testid="environment-workers-empty"
            >
              <p className="text-sm text-text-secondary">No workers connected yet.</p>
              <p className="mt-2 text-xs text-text-muted">
                A worker is a machine where agents actually run. Add at least one to
                start dispatching tasks.
              </p>
              <button
                type="button"
                className="mt-4 rounded bg-brand px-4 py-2 text-sm font-medium text-white hover:bg-brand-hover"
                onClick={() => setModalOpen(true)}
                data-testid="environment-workers-empty-cta"
              >
                + Add your first worker
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
        workItems={fleet.isSuccess ? fleet.data.work_items : []}
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
  const online = worker.status === 'online';
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
            {worker.active_count} active
          </span>
          <span className="text-xs text-text-muted">
            {worker.last_heartbeat_at ? `hb ${formatLocalTime(worker.last_heartbeat_at)}` : 'hb —'}
          </span>
          <RemoveWorkerButton worker={worker} />
        </div>
      </div>

      {/* CLI sub-section */}
      <div className="mt-3 border-t border-border-base pt-3">
        <p className="mb-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
          CLI
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
        <p className="mb-1.5 text-[0.6875rem] font-semibold uppercase tracking-wide text-text-muted">
          Agents
        </p>
        {agents.length === 0 ? (
          <p className="text-xs text-text-muted" data-testid="environment-worker-noagents">
            No agents bound to this worker.
          </p>
        ) : (
          <ul className="space-y-1.5" data-testid="environment-worker-agents">
            {agents.map((a) => (
              <li
                key={a.id}
                className="flex flex-wrap items-center gap-2 text-sm"
                data-testid="environment-agent"
                data-agent-id={a.id}
              >
                <UserIcon className="h-4 w-4 shrink-0 text-text-muted" />
                <span className="font-medium text-text-primary">{a.name}</span>
                {a.cli && (
                  <span
                    className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-[0.6875rem] text-text-secondary"
                    data-testid="environment-agent-cli"
                    data-cli={a.cli}
                  >
                    {a.cli}
                  </span>
                )}
                {a.model && (
                  <span
                    className="rounded bg-accent/10 px-1.5 py-0.5 text-[0.6875rem] text-accent"
                    data-testid="environment-agent-model"
                    data-model={a.model}
                  >
                    {a.model}
                  </span>
                )}
                <LifecycleBadge lifecycle={a.lifecycle} />
                <OrgLink
                  to={`/agents/${encodeURIComponent(a.id)}`}
                  className="text-xs text-accent hover:underline"
                >
                  Open →
                </OrgLink>
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
  { key: 'all', label: 'All' },
  { key: 'work_items', label: 'Work Items' },
  { key: 'issues', label: 'Issues' },
  { key: 'transfers', label: 'Transfers' },
] as const;
type ActivityTab = (typeof ACTIVITY_TABS)[number]['key'];

function ActivitySection({
  base,
  workItems,
  issues,
  transfers,
}: {
  base: string;
  workItems: WorkItemRow[];
  issues: FleetIssueRow[];
  transfers: ReturnType<typeof useTransferSessions>;
}): React.ReactElement {
  const [tab, setTab] = useState<ActivityTab>('all');
  const tablist = useTablistKeyboard({ keys: ACTIVITY_TABS.map((t) => t.key), active: tab });
  const transferRows = transfers.data ?? [];

  // The active tab's stream is empty? (transfers loading/error are handled
  // inside the panel, so only treat the resolved-empty case as "empty".)
  const tabIsEmpty =
    (tab === 'all' && workItems.length === 0 && issues.length === 0 && transferRows.length === 0) ||
    (tab === 'work_items' && workItems.length === 0) ||
    (tab === 'issues' && issues.length === 0) ||
    (tab === 'transfers' && transfers.isSuccess && transferRows.length === 0);

  return (
    <section>
      <div className="mb-2 flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold text-text-primary">Activity</h3>
        <nav
          className="flex gap-1"
          role="tablist"
          aria-orientation="horizontal"
          ref={tablist.tablistRef}
          onKeyDown={tablist.onKeyDown}
          onBlur={tablist.onBlur}
          data-testid="environment-activity-tabs"
        >
          {ACTIVITY_TABS.map((t) => (
            <button
              key={t.key}
              type="button"
              role="tab"
              id={`environment-activity-tab-${t.key}`}
              aria-selected={tab === t.key}
              aria-controls={`environment-activity-panel-${t.key}`}
              tabIndex={tablist.tabIndexFor(t.key)}
              onClick={() => setTab(t.key)}
              data-testid={`environment-activity-tab-${t.key}`}
              className={`rounded px-2.5 py-1 text-xs font-medium ${
                tab === t.key
                  ? 'bg-bg-subtle text-text-primary'
                  : 'text-text-muted hover:text-text-primary'
              }`}
            >
              {t.label}
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
            {tab === 'work_items' && <WorkItemsList base={base} workItems={workItems} />}
            {tab === 'issues' && <IssuesList base={base} issues={issues} />}
            {tab === 'transfers' && <TransfersPanel transfers={transfers} />}
          </>
        )}
      </div>
    </section>
  );
}

function ActivityEmpty(): React.ReactElement {
  return (
    <div
      className="flex flex-col items-center gap-2 rounded border border-dashed border-border-base bg-bg-subtle p-8 text-center"
      data-testid="environment-activity-empty"
    >
      <ClockIcon className="h-8 w-8 text-text-muted" />
      <p className="text-sm font-medium text-text-secondary">No active operations</p>
      <p className="text-xs text-text-muted">
        Work items, issues, and file transfers will appear here.
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
  workItems: WorkItemRow[];
  issues: FleetIssueRow[];
  transfers: TransferSession[];
}): React.ReactElement {
  return (
    <ul
      className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
      data-testid="environment-activity-all-list"
    >
      {workItems.map((wi) => (
        <li
          key={`wi-${wi.work_item_id}`}
          className="flex items-center gap-2 px-3 py-2 text-xs"
          data-testid="environment-activity-all-row"
          data-kind="work_item"
        >
          <TypeTag label="Work" testId="environment-activity-all-type" />
          <WorkItemContent base={base} wi={wi} />
        </li>
      ))}
      {issues.map((i) => (
        <li
          key={`iss-${i.issue_id}`}
          className="flex items-center gap-2 px-3 py-2 text-xs"
          data-testid="environment-activity-all-row"
          data-kind="issue"
        >
          <TypeTag label="Issue" testId="environment-activity-all-type" />
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
          <TypeTag label="Transfer" testId="environment-activity-all-type" />
          <TransferContent tr={tr} />
        </li>
      ))}
    </ul>
  );
}

// Shared row-content renderers (kept in sync between the All stream and the
// dedicated tabs).
function WorkItemContent({ base, wi }: { base: string; wi: WorkItemRow }): React.ReactElement {
  return (
    <span className="flex flex-1 items-center justify-between gap-2">
      <span>
        {wi.task_id ? (
          <Link
            to={`${base}/tasks/${encodeURIComponent(wi.task_id)}`}
            className="font-mono text-accent hover:underline"
          >
            {wi.task_id}
          </Link>
        ) : (
          <span className="font-mono text-text-muted">{wi.work_item_id}</span>
        )}{' '}
        <span className="text-text-muted">agent</span>{' '}
        <span className="font-mono">{wi.agent_id}</span>
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

function IssueContent({ base, issue }: { base: string; issue: FleetIssueRow }): React.ReactElement {
  return (
    <Link
      to={`${base}/issues/${encodeURIComponent(issue.issue_id)}`}
      className="text-accent hover:underline"
    >
      {issue.title}
    </Link>
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

function WorkItemsList({ base, workItems }: { base: string; workItems: WorkItemRow[] }): React.ReactElement {
  return (
    <ul
      className="divide-y divide-border-base rounded border border-border-base bg-bg-elevated text-sm text-text-primary"
      data-testid="environment-workitem-list"
    >
      {workItems.map((wi) => (
        <li
          key={wi.work_item_id}
          className="flex items-center justify-between px-3 py-2 text-xs"
          data-testid="environment-workitem-row"
          data-work-item-id={wi.work_item_id}
        >
          <WorkItemContent base={base} wi={wi} />
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
  return (
    <>
      {transfers.isLoading && (
        <div data-testid="transfers-loading">
          <p className="text-xs text-text-muted">Loading…</p>
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
              <th className="w-1/5 border-b border-border-base px-3 py-2">Direction</th>
              <th className="w-1/5 border-b border-border-base px-3 py-2">Scope</th>
              <th className="w-2/5 border-b border-border-base px-3 py-2">Content</th>
              <th className="border-b border-border-base px-3 py-2 text-right">Size</th>
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
  const displayName = worker.name || worker.worker_id;
  return (
    <div className="flex flex-col">
      <OrgLink
        to={`/workers/${worker.worker_id}`}
        className="text-left text-sm font-medium text-text-primary hover:text-accent hover:underline"
        title={`Open ${displayName} details`}
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
  const detected = (worker.capabilities ?? []).filter((c) => c.detected);
  if (detected.length === 0) {
    return (
      <p className="text-xs text-text-muted" data-testid="environment-worker-nocaps">
        No agent CLIs detected.
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
          {c.enabled ? '' : ' (disabled)'}
        </span>
      ))}
      {/* v2.7 #181 / FINDING-F: detected ≠ runnable. Only claude-code executes
          in v2.7; codex/opencode are discovery-only until v2.8 (#180). Be
          explicit so the list isn't read as "all runnable". */}
      <span className="text-xs italic text-text-muted" data-testid="environment-worker-executable-note">
        Executable: claude-code only (codex/opencode discovery only — v2.8)
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
        Show install command
      </button>
      <button
        type="button"
        className="rounded border border-border-base px-2 py-1 text-xs text-text-primary hover:bg-bg-subtle"
        onClick={() => setConfirmReMint(true)}
        data-testid="environment-worker-remint-install"
      >
        Re-mint install command
      </button>
      <ConfirmModal
        open={confirmReMint}
        title="Re-mint install command?"
        message={
          'Re-mint will revoke the current install token and issue a fresh one. ' +
          'Use this if the original command expired or got lost. Continue?'
        }
        confirmLabel="Re-mint"
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
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const confirmMessage =
    worker.status === 'online'
      ? `Remove worker "${worker.name || worker.worker_id}"?\n\n` +
        'This will revoke the worker token and remove the record. ' +
        'The worker daemon will hit 401 next cycle.'
      : `Remove worker "${worker.name || worker.worker_id}"?\n\n` +
        'This will revoke any active install token and remove the record.';
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
        {busy ? 'Removing...' : 'Remove'}
      </button>
      <ConfirmModal
        open={confirmOpen}
        title="Remove worker?"
        message={confirmMessage}
        confirmLabel="Remove"
        danger
        busy={busy}
        onConfirm={() => void handleRemove()}
        onCancel={() => setConfirmOpen(false)}
      />
    </span>
  );
}
