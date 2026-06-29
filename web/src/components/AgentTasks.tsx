import type React from 'react';
import { useMemo, useState } from 'react';
import { OrgLink } from '@/OrgContext';
import { useAgentTasks } from '@/api/agents';
import { useAgentConcurrency, type AgentConcurrency, type ConcurrencyExecutor } from '@/api/concurrency';
import { TypeChip } from '@/components/TypeChip';
import { refLabel } from '@/components/workItemDisplay';
import type { AgentTask, AgentTaskStatus } from '@/api/types';

// AgentTasks (v2.7.1 #228 PR(d); v2.14.0 I14 rename) — the Tasks tab body. A READ-ONLY
// table (design4): ID / Title / Type / Priority / Status / Updated, a summary
// strip (N Total · In Progress · Pending · Done · Blocked) and Status/Type
// filters. There is intentionally NO "+ New" button (PD ruling A): tasks
// are a projection of task dispatch — they have no manual create endpoint, so a
// disabled/stub button would be a dead affordance. "+ New" returns in v2.8 #235
// as a "Create Task → auto-assign this agent" shortcut.
//
// v2.7.1 fallbacks (no backend schema yet → labelled, never fabricated):
//   Type = "Task" for every row (#231 will model real types), Priority = "—".

// Status → user-facing bucket (the 4 summary buckets + a catch-all). The raw
// AgentTaskStatus is kept on the row (data-status) for operators / tests.
type Bucket = 'in_progress' | 'paused' | 'pending' | 'done' | 'blocked' | 'other';

const STATUS_DISPLAY: Record<AgentTaskStatus, { label: string; cls: string; bucket: Bucket }> = {
  active: { label: 'In Progress', cls: 'bg-brand/10 text-brand', bucket: 'in_progress' },
  // v2.8.1 #278 D: agent-paused (scheduling autonomy) — a distinct bucket, not
  // "pending" (queued, waiting to be picked) nor "blocked" (system/reconciler).
  // dark: lighter text — the fixed mid-tone (violet/orange-600) on an alpha-tint
  // over the dark page bg is dark-on-dark (FAILs AA in dark mode); the lighter
  // -400 variant restores AA (violet-400 ~5.9:1, the token-based chips below
  // already adapt via --color-* dark variants). Light mode unchanged.
  paused: { label: 'Paused', cls: 'bg-violet-500/10 text-violet-600 dark:text-violet-400', bucket: 'paused' },
  // queued: double fix — orange-600 FAILed even in LIGHT (3.21:1, pre-existing
  // #228) → orange-700 (4.68 AA); + dark:orange-400 (7.03 AA) for dark mode.
  queued: { label: 'Pending', cls: 'bg-orange-500/10 text-orange-700 dark:text-orange-400', bucket: 'pending' },
  waiting_input: { label: 'Blocked', cls: 'bg-danger/10 text-danger', bucket: 'blocked' },
  failed: { label: 'Blocked', cls: 'bg-danger/10 text-danger', bucket: 'blocked' },
  done: { label: 'Done', cls: 'bg-success/10 text-success', bucket: 'done' },
  canceled: { label: 'Canceled', cls: 'bg-bg-subtle text-text-muted', bucket: 'other' },
  superseded: { label: 'Superseded', cls: 'bg-bg-subtle text-text-muted', bucket: 'other' },
};

const STATUS_FILTERS: Array<{ value: Bucket | 'all'; label: string }> = [
  { value: 'all', label: 'All Status' },
  { value: 'in_progress', label: 'In Progress' },
  { value: 'paused', label: 'Paused' },
  { value: 'pending', label: 'Pending' },
  { value: 'blocked', label: 'Blocked' },
  { value: 'done', label: 'Done' },
];

export function AgentTasks({ agentId }: { agentId: string }): React.ReactElement {
  const workItems = useAgentTasks(agentId);
  // T593: live concurrency snapshot (3s poll), overlaid onto the task rows by
  // task_id. Best-effort — if it errors / hasn't landed, the task list is unaffected.
  const concurrency = useAgentConcurrency(agentId);
  const concData = concurrency.data;
  const [statusFilter, setStatusFilter] = useState<Bucket | 'all'>('all');
  // v2.7.1: every task is type "task" (no schema). The filter is present
  // to match the design; "task" is the only non-"all" option.
  const [typeFilter, setTypeFilter] = useState<'all' | 'task'>('all');

  const items = useMemo(() => workItems.data ?? [], [workItems.data]);

  const counts = useMemo(() => {
    const c = { total: items.length, in_progress: 0, paused: 0, pending: 0, done: 0, blocked: 0 };
    for (const w of items) {
      const b = STATUS_DISPLAY[w.status]?.bucket;
      if (b === 'in_progress') c.in_progress += 1;
      else if (b === 'paused') c.paused += 1;
      else if (b === 'pending') c.pending += 1;
      else if (b === 'done') c.done += 1;
      else if (b === 'blocked') c.blocked += 1;
    }
    return c;
  }, [items]);

  const filtered = useMemo(
    () =>
      items.filter((w) => {
        const okStatus = statusFilter === 'all' || STATUS_DISPLAY[w.status]?.bucket === statusFilter;
        const okType = typeFilter === 'all' || typeFilter === 'task'; // all rows are "task" in v2.7.1
        return okStatus && okType;
      }),
    [items, statusFilter, typeFilter],
  );

  // task_id → { executor, slot }. Slots are numbered by start order (oldest = 1) —
  // the contract carries no explicit slot index; mirrors the mockup. The overlay
  // joins to a row when its underlying task_id matches an executor's task_id.
  const execByTask = useMemo(() => {
    const m = new Map<string, { exec: ConcurrencyExecutor; slot: number }>();
    const xs = concData?.executors ?? [];
    [...xs]
      .sort((a, b) => (a.started_at < b.started_at ? -1 : a.started_at > b.started_at ? 1 : 0))
      .forEach((e, i) => m.set(e.task_id, { exec: e, slot: i + 1 }));
    return m;
  }, [concData]);

  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-tabpanel-workitems">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold text-text-primary">Tasks</h3>
        <div className="flex items-center gap-2">
          <select
            className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            data-testid="agent-workitems-filter-status"
            aria-label="Filter by status"
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as Bucket | 'all')}
          >
            {STATUS_FILTERS.map((f) => (
              <option key={f.value} value={f.value}>
                {f.label}
              </option>
            ))}
          </select>
          <select
            className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            data-testid="agent-workitems-filter-type"
            aria-label="Filter by type"
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value as 'all' | 'task')}
          >
            <option value="all">All Types</option>
            <option value="task">Task</option>
          </select>
        </div>
      </div>

      {/* T593: live slots summary (active/cap + queued + heartbeat + snapshot age).
          Shown only once the concurrency snapshot has loaded; absent on error so
          the task list is never blocked by the overlay. */}
      {concData && <ConcurrencySlots data={concData} />}

      {workItems.isLoading && (
        <p className="text-xs text-text-muted" data-testid="agent-workitems-loading">
          Loading tasks…
        </p>
      )}
      {workItems.isError && (
        <p className="text-xs text-danger" data-testid="agent-workitems-error">
          {(workItems.error as Error).message}
        </p>
      )}

      {workItems.isSuccess && items.length === 0 && (
        // Dev-suggested copy: explain how tasks appear (intent, not affordance).
        <p className="text-xs text-text-muted" data-testid="agent-workitems-empty">
          Tasks appear here when they are assigned to this agent.
        </p>
      )}

      {workItems.isSuccess && items.length > 0 && (
        <>
          {/* Summary strip (v2.8.1 #278: + Paused). Order: Total · In Progress ·
              Paused · Pending · Blocked · Done. */}
          <dl className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs" data-testid="agent-workitems-summary">
            <span className="font-medium text-text-primary">{counts.total} Total</span>
            <span className="text-brand">{counts.in_progress} In Progress</span>
            <span className="text-violet-600 dark:text-violet-400">{counts.paused} Paused</span>
            <span className="text-orange-700 dark:text-orange-400">{counts.pending} Pending</span>
            <span className="text-danger">{counts.blocked} Blocked</span>
            <span className="text-success">{counts.done} Done</span>
          </dl>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs" data-testid="agent-workitems-table">
              <thead>
                <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                  <th className="py-1.5 pr-3 font-medium">ID</th>
                  <th className="py-1.5 pr-3 font-medium">Title</th>
                  <th className="py-1.5 pr-3 font-medium">Type</th>
                  <th className="py-1.5 pr-3 font-medium">Priority</th>
                  <th className="py-1.5 pr-3 font-medium">Status</th>
                  <th className="py-1.5 font-medium">Updated</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border-base">
                {filtered.map((w) => {
                  const taskId = w.task_id || w.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
                  return (
                    <TaskRow
                      key={w.id}
                      item={w}
                      slot={execByTask.get(taskId)}
                      stale={concData?.stale ?? false}
                      snapshotAgeMs={concData?.snapshot_age_ms}
                    />
                  );
                })}
              </tbody>
            </table>
          </div>

          {filtered.length === 0 && (
            <p className="mt-3 text-xs text-text-muted" data-testid="agent-workitems-no-match">
              No tasks match the current filters.
            </p>
          )}
        </>
      )}
    </section>
  );
}

function TaskRow({
  item: w,
  slot,
  stale,
  snapshotAgeMs,
}: {
  item: AgentTask;
  slot?: { exec: ConcurrencyExecutor; slot: number };
  stale?: boolean;
  snapshotAgeMs?: number;
}): React.ReactElement {
  // v2.7.1 #206: link the title to its task when resolved; raw pm ref on hover.
  const taskId = w.task_id || w.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
  const linkable = Boolean(w.task_title && w.project_id && taskId);
  const status = STATUS_DISPLAY[w.status] ?? { label: w.status, cls: 'bg-bg-subtle text-text-muted' };
  const bucket = STATUS_DISPLAY[w.status]?.bucket;

  return (
    <tr className="align-top" data-testid="agent-workitem-row" data-workitem-id={w.id} data-status={w.status}>
      {/* T100: show the underlying task's org_ref (T84) when present. The work
          item itself has no human-facing number, so absent an org_ref fall back
          to the FULL id (T126: never the retired #id-tail hash), with the full id
          also on hover (#192 — never a bare short hash as chrome). */}
      <td className="py-2 pr-3 font-mono text-text-muted" data-testid="agent-workitem-id" title={w.id}>
        {refLabel(w.org_ref, w.id)}
      </td>
      <td className="max-w-[20rem] py-2 pr-3" title={w.task_ref}>
        {linkable ? (
          <OrgLink
            to={`/projects/${encodeURIComponent(w.project_id as string)}/tasks/${encodeURIComponent(taskId)}`}
            className="block truncate text-text-secondary hover:text-accent"
            data-testid="agent-workitem-task"
          >
            {w.task_title}
          </OrgLink>
        ) : (
          <span className="block truncate text-text-secondary">{w.task_title || 'Task'}</span>
        )}
        {/* T593: live concurrency overlay. In-progress rows show the executor
            (cli·model / slot / elapsed / heartbeat / orphan); pending rows show
            the queued-for-slot hint. Done/Blocked/Paused are unchanged. */}
        {bucket === 'in_progress' && slot && (
          <ExecutorOverlay slot={slot} stale={stale} snapshotAgeMs={snapshotAgeMs} />
        )}
        {bucket === 'pending' && (
          <p className="mt-1 text-[0.6875rem] text-text-muted" data-testid="agent-task-queued">
            Queued for a slot{waitingFor(w.updated_at) ? ` · waiting ${waitingFor(w.updated_at)}` : ''}
          </p>
        )}
      </td>
      <td className="py-2 pr-3" data-testid="agent-workitem-type">
        {/* v2.7.1 fallback: every row is a Task (real types = v2.8 #231). */}
        <TypeChip kind="task" />
      </td>
      <td className="py-2 pr-3 text-text-muted" data-testid="agent-workitem-priority">
        {/* v2.7.1 fallback: no priority schema yet (#231). */}—
      </td>
      <td className="py-2 pr-3" data-testid="agent-workitem-status">
        <span className={`rounded px-1.5 py-0.5 text-[0.625rem] font-medium uppercase tracking-wide ${status.cls}`}>
          {status.label}
        </span>
      </td>
      <td className="py-2 tabular-nums text-text-muted" data-testid="agent-workitem-updated" title={w.updated_at}>
        {formatUpdated(w.updated_at)}
      </td>
    </tr>
  );
}

function formatUpdated(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return d.toLocaleDateString();
}

// ── T593: concurrency overlay ────────────────────────────────────────────────

// concurrencyMode resolves the T606 three-state freshness from the snapshot
// (issue-af03da2f), replacing the single overloaded `stale` flag that mislabeled
// every non-live case "worker unreachable":
//   - 'live'    — a fresh snapshot: show real active/cap slots.
//   - 'offline' — the bound worker is truly OFFLINE (reachable=false).
//   - 'expired' — a snapshot exists but aged past the TTL (last-known, worker online).
//   - 'nodata'  — the agent never reported a snapshot (concurrency not active on the
//                 worker) — NEUTRAL, not an error; the common non-concurrent case.
// reachable/has_snapshot are optional for back-compat with a pre-T606 Center: absent
// → online + (snapshot present iff not stale), i.e. the legacy live-vs-stale split.
type ConcurrencyMode = 'live' | 'offline' | 'expired' | 'nodata';
function concurrencyMode(data: AgentConcurrency): ConcurrencyMode {
  const reachable = data.reachable ?? true;
  const hasSnapshot = data.has_snapshot ?? !data.stale;
  if (!reachable) return 'offline';
  if (!hasSnapshot) return 'nodata';
  if (data.stale) return 'expired';
  return 'live';
}

// ConcurrencySlots — the live slots summary header: active/cap occupancy bar +
// queued count + adaptive-heartbeat label + snapshot age. Renders one of three
// non-live states (worker offline / snapshot expired / no live data) instead of a
// single amber "unreachable" strip; the task list below stays visible regardless.
function ConcurrencySlots({ data }: { data: AgentConcurrency }): React.ReactElement {
  const mode = concurrencyMode(data);
  const cap = Math.max(0, data.cap);
  const active = Math.max(0, data.active);
  const segs = Array.from({ length: cap }, (_, i) => i < active);
  // offline/expired are warnings (amber); nodata is neutral; live is normal.
  const amber = mode === 'offline' || mode === 'expired';
  const slotsLabel: Record<ConcurrencyMode, string> = {
    live: 'slots in use',
    offline: 'slots — worker offline',
    expired: 'slots — snapshot expired',
    nodata: 'no live slot data',
  };
  return (
    <div
      className={`mb-3 flex flex-wrap items-center justify-between gap-2 rounded-lg border px-3 py-2 ${
        amber ? 'border-warning/40 bg-status-amber-bg' : 'border-border-base bg-bg-subtle'
      }`}
      data-testid="agent-concurrency-summary"
      data-stale={data.stale ? 'true' : 'false'}
      data-mode={mode}
    >
      <div className="flex items-center gap-2">
        <span className="text-sm font-bold text-text-primary" data-testid="agent-concurrency-slots">
          {mode === 'live' ? active : '—'}
          <span className="text-text-muted">/{cap}</span>
        </span>
        <span className="text-xs text-text-muted">{slotsLabel[mode]}</span>
        <span className="ml-1 inline-flex items-center gap-0.5" aria-hidden="true">
          {segs.map((on, i) => (
            <span
              key={i}
              className={`h-2 w-5 rounded-sm ${
                mode === 'live' && on ? 'bg-brand' : amber && on ? 'bg-warning' : 'bg-border-strong'
              }`}
            />
          ))}
        </span>
        {mode === 'live' && data.queued > 0 && (
          <span className="text-xs text-text-muted" data-testid="agent-concurrency-queued">· {data.queued} queued</span>
        )}
      </div>
      {mode === 'live' ? (
        <span className="flex items-center gap-2 text-xs text-text-muted" data-testid="agent-concurrency-age">
          <span className="inline-flex items-center gap-1" title="Adaptive heartbeat cadence"><HeartIcon /> adaptive 3s</span>
          <span>updated {formatAge(data.snapshot_age_ms)} ago</span>
        </span>
      ) : mode === 'offline' ? (
        <span className="flex items-center gap-1 text-xs font-medium text-status-amber-fg" data-testid="agent-concurrency-age">
          <WarnIcon /> worker offline
        </span>
      ) : mode === 'expired' ? (
        <span className="flex items-center gap-1 text-xs font-medium text-status-amber-fg" data-testid="agent-concurrency-age">
          <WarnIcon /> snapshot {formatAge(data.snapshot_age_ms)} ago · last known
        </span>
      ) : (
        <span className="flex items-center gap-1 text-xs text-text-muted" data-testid="agent-concurrency-age">
          no real-time slot data · concurrency not active
        </span>
      )}
    </div>
  );
}

// ExecutorOverlay — the per-row in-progress overlay: cli·model chip, slot, elapsed
// (from started_at), heartbeat age, and an orphan badge / stale marker.
function ExecutorOverlay({
  slot,
  stale,
  snapshotAgeMs,
}: {
  slot: { exec: ConcurrencyExecutor; slot: number };
  stale?: boolean;
  snapshotAgeMs?: number;
}): React.ReactElement {
  const { exec } = slot;
  const starting = exec.state.toLowerCase().includes('starting');
  const orphan = exec.state.toLowerCase().includes('orphan');
  const elapsed = formatElapsed(exec.started_at);
  return (
    <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-[0.6875rem]" data-testid="agent-task-overlay">
      <span
        className="rounded bg-bg-subtle px-1.5 py-0.5 font-mono text-text-secondary"
        data-testid="agent-task-cli-model"
      >
        {exec.cli} · {exec.model}
      </span>
      <span className="rounded bg-status-blue-bg px-1.5 py-0.5 font-semibold uppercase tracking-wide text-status-blue-fg" data-testid="agent-task-slot">
        slot {slot.slot}
      </span>
      {elapsed && (
        <span className="text-text-muted" data-testid="agent-task-elapsed">
          ⏱ {elapsed}{starting ? ' starting' : ''}
        </span>
      )}
      {!stale && typeof snapshotAgeMs === 'number' && (
        <span className="inline-flex items-center gap-1 text-text-muted" data-testid="agent-task-heartbeat" title="Heartbeat age">
          <HeartIcon /> {formatAge(snapshotAgeMs)}
        </span>
      )}
      {orphan && (
        <span className="rounded bg-status-amber-bg px-1.5 py-0.5 font-semibold uppercase tracking-wide text-status-amber-fg" data-testid="agent-task-orphan">
          orphan · monitored
        </span>
      )}
      {stale && (
        <span className="font-medium text-status-amber-fg" data-testid="agent-task-overlay-stale">
          overlay stale
        </span>
      )}
    </div>
  );
}

// Heartbeat / warning glyphs as SVG (a11y: no emoji as icon).
function HeartIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 16 16" className="h-3 w-3 shrink-0" fill="currentColor" aria-hidden="true">
      <path d="M8 14s-5-3.3-5-7a3 3 0 0 1 5-2.2A3 3 0 0 1 13 7c0 3.7-5 7-5 7z" />
    </svg>
  );
}
function WarnIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 16 16" className="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true">
      <path d="M8 2.5 14.5 13.5H1.5z" strokeLinejoin="round" />
      <path d="M8 6.5v3.2M8 11.6v.01" strokeLinecap="round" />
    </svg>
  );
}

// formatAge — compact "Ns" / "Nm" from a millisecond age (snapshot/heartbeat).
function formatAge(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '0s';
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

// formatElapsed — "4m 12s" / "6s" / "1h 3m" from an ISO start time to now. Returns
// "" for an unparseable / future start.
function formatElapsed(startedAt: string): string {
  const start = new Date(startedAt).getTime();
  if (Number.isNaN(start)) return '';
  const s = Math.floor((Date.now() - start) / 1000);
  if (s < 0) return '';
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  const h = Math.floor(m / 60);
  return `${h}h ${m % 60}m`;
}

// waitingFor — how long a pending task has been queued (since its last update).
function waitingFor(iso: string): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const s = Math.floor((Date.now() - t) / 1000);
  if (s < 0) return '';
  return formatAge(s * 1000);
}
