import type React from 'react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { OrgLink } from '@/OrgContext';
import { useAgentTasks } from '@/api/agents';
import { useAgentConcurrency } from '@/api/concurrency';
import { AgentContextPanel } from '@/components/AgentContextPanel';
import {
  ExecutorSlotPanel,
  ExecutorTaskOverlay,
  buildExecutorSlotByTask,
  formatAge,
  type ExecutorSlotMatch,
} from '@/components/ExecutorSlotPanel';
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

const STATUS_DISPLAY: Record<AgentTaskStatus, { labelKey: string; cls: string; bucket: Bucket }> = {
  active: { labelKey: 'agentRuntime.tasks.status.in_progress', cls: 'bg-brand/10 text-brand', bucket: 'in_progress' },
  // v2.8.1 #278 D: agent-paused (scheduling autonomy) — a distinct bucket, not
  // "pending" (queued, waiting to be picked) nor "blocked" (system/reconciler).
  // dark: lighter text — the fixed mid-tone (violet/orange-600) on an alpha-tint
  // over the dark page bg is dark-on-dark (FAILs AA in dark mode); the lighter
  // -400 variant restores AA (violet-400 ~5.9:1, the token-based chips below
  // already adapt via --color-* dark variants). Light mode unchanged.
  paused: { labelKey: 'agentRuntime.tasks.status.paused', cls: 'bg-violet-500/10 text-violet-600 dark:text-violet-400', bucket: 'paused' },
  // queued: double fix — orange-600 FAILed even in LIGHT (3.21:1, pre-existing
  // #228) → orange-700 (4.68 AA); + dark:orange-400 (7.03 AA) for dark mode.
  queued: { labelKey: 'agentRuntime.tasks.status.pending', cls: 'bg-orange-500/10 text-orange-700 dark:text-orange-400', bucket: 'pending' },
  waiting_input: { labelKey: 'agentRuntime.tasks.status.blocked', cls: 'bg-danger/10 text-danger', bucket: 'blocked' },
  failed: { labelKey: 'agentRuntime.tasks.status.blocked', cls: 'bg-danger/10 text-danger', bucket: 'blocked' },
  done: { labelKey: 'agentRuntime.tasks.status.done', cls: 'bg-success/10 text-success', bucket: 'done' },
  canceled: { labelKey: 'agentRuntime.tasks.status.canceled', cls: 'bg-bg-subtle text-text-muted', bucket: 'other' },
  superseded: { labelKey: 'agentRuntime.tasks.status.superseded', cls: 'bg-bg-subtle text-text-muted', bucket: 'other' },
};

const STATUS_FILTERS: Array<{ value: Bucket | 'all'; labelKey: string }> = [
  { value: 'all', labelKey: 'agentRuntime.tasks.statusFilter.all' },
  { value: 'in_progress', labelKey: 'agentRuntime.tasks.statusFilter.in_progress' },
  { value: 'paused', labelKey: 'agentRuntime.tasks.statusFilter.paused' },
  { value: 'pending', labelKey: 'agentRuntime.tasks.statusFilter.pending' },
  { value: 'blocked', labelKey: 'agentRuntime.tasks.statusFilter.blocked' },
  { value: 'done', labelKey: 'agentRuntime.tasks.statusFilter.done' },
];

export function AgentTasks({ agentId }: { agentId: string }): React.ReactElement {
  const { t } = useTranslation('members');
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

  // task_id -> live executor/slot. New workers carry slot_index; old workers can
  // still show an unnumbered executor overlay, but never a fabricated #N.
  const execByTask = useMemo(() => {
    return buildExecutorSlotByTask(concData);
  }, [concData]);

  return (
    <section className="rounded border border-border-base bg-bg-elevated p-4" data-testid="agent-tabpanel-workitems">
      {/* v2.24.x (@oopslink): the agent's CURRENT task + owning PLAN, moved here
          from the retired right-hand col④ sidebar. Inline = wide 2-column block
          above the task table, so opening Tasks shows "what is it doing now · in
          which plan" before the full queue. */}
      <div className="mb-4 border-b border-border-base pb-4">
        <AgentContextPanel agentId={agentId} inline />
      </div>

      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold text-text-primary">{t('agentRuntime.tasks.heading')}</h3>
        <div className="flex items-center gap-2">
          <select
            className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            data-testid="agent-workitems-filter-status"
            aria-label={t('agentRuntime.tasks.filterByStatus')}
            value={statusFilter}
            onChange={(e) => setStatusFilter(e.target.value as Bucket | 'all')}
          >
            {STATUS_FILTERS.map((f) => (
              <option key={f.value} value={f.value}>
                {t(f.labelKey)}
              </option>
            ))}
          </select>
          <select
            className="rounded border border-border-strong bg-bg-elevated px-2 py-1 text-xs text-text-primary"
            data-testid="agent-workitems-filter-type"
            aria-label={t('agentRuntime.tasks.filterByType')}
            value={typeFilter}
            onChange={(e) => setTypeFilter(e.target.value as 'all' | 'task')}
          >
            <option value="all">{t('agentRuntime.tasks.typeFilter.all')}</option>
            <option value="task">{t('agentRuntime.tasks.typeFilter.task')}</option>
          </select>
        </div>
      </div>

      <ExecutorSlotPanel
        data={concData}
        error={concurrency.error as Error | null}
        compact={false}
        testId="agent-concurrency-summary"
      />

      {workItems.isLoading && (
        <p className="text-xs text-text-muted" data-testid="agent-workitems-loading">
          {t('agentRuntime.tasks.loading')}
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
          {t('agentRuntime.tasks.empty')}
        </p>
      )}

      {workItems.isSuccess && items.length > 0 && (
        <>
          {/* Summary strip (v2.8.1 #278: + Paused). Order: Total · In Progress ·
              Paused · Pending · Blocked · Done. */}
          <dl className="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs" data-testid="agent-workitems-summary">
            <span className="font-medium text-text-primary">{t('agentRuntime.tasks.summary.total', { count: counts.total })}</span>
            <span className="text-brand">{t('agentRuntime.tasks.summary.inProgress', { count: counts.in_progress })}</span>
            <span className="text-violet-600 dark:text-violet-400">{t('agentRuntime.tasks.summary.paused', { count: counts.paused })}</span>
            <span className="text-orange-700 dark:text-orange-400">{t('agentRuntime.tasks.summary.pending', { count: counts.pending })}</span>
            <span className="text-danger">{t('agentRuntime.tasks.summary.blocked', { count: counts.blocked })}</span>
            <span className="text-success">{t('agentRuntime.tasks.summary.done', { count: counts.done })}</span>
          </dl>

          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs" data-testid="agent-workitems-table">
              <thead>
                <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                  <th className="py-1.5 pr-3 font-medium">{t('agentRuntime.tasks.columns.id')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('agentRuntime.tasks.columns.title')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('agentRuntime.tasks.columns.type')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('agentRuntime.tasks.columns.plan')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('agentRuntime.tasks.columns.priority')}</th>
                  <th className="py-1.5 pr-3 font-medium">{t('agentRuntime.tasks.columns.status')}</th>
                  <th className="py-1.5 font-medium">{t('agentRuntime.tasks.columns.updated')}</th>
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
                      t={t}
                    />
                  );
                })}
              </tbody>
            </table>
          </div>

          {filtered.length === 0 && (
            <p className="mt-3 text-xs text-text-muted" data-testid="agent-workitems-no-match">
              {t('agentRuntime.tasks.noMatch')}
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
  t,
}: {
  item: AgentTask;
  slot?: ExecutorSlotMatch;
  stale?: boolean;
  snapshotAgeMs?: number;
  t: TFunction;
}): React.ReactElement {
  // v2.7.1 #206: link the title to its task when resolved; raw pm ref on hover.
  const taskId = w.task_id || w.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
  const linkable = Boolean(w.task_title && w.project_id && taskId);
  const statusMeta = STATUS_DISPLAY[w.status];
  const statusLabel = statusMeta ? t(statusMeta.labelKey) : w.status;
  const statusCls = statusMeta?.cls ?? 'bg-bg-subtle text-text-muted';
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
          <span className="block truncate text-text-secondary">{w.task_title || t('agentRuntime.tasks.defaultTitle')}</span>
        )}
        {/* T593: live concurrency overlay. In-progress rows show the executor
            (cli·model / slot / elapsed / heartbeat / orphan); pending rows show
            the queued-for-slot hint. Done/Blocked/Paused are unchanged. */}
        {bucket === 'in_progress' && slot && <ExecutorTaskOverlay match={slot} stale={stale} snapshotAgeMs={snapshotAgeMs} />}
        {bucket === 'pending' && (
          <p className="mt-1 text-[0.6875rem] text-text-muted" data-testid="agent-task-queued">
            {waitingFor(w.updated_at)
              ? t('agentRuntime.tasks.queuedWaiting', { duration: waitingFor(w.updated_at) })
              : t('agentRuntime.tasks.queued')}
          </p>
        )}
      </td>
      <td className="py-2 pr-3" data-testid="agent-workitem-type">
        {/* v2.7.1 fallback: every row is a Task (real types = v2.8 #231). */}
        <TypeChip kind="task" />
      </td>
      <td className="py-2 pr-3 text-text-muted" data-testid="agent-workitem-plan" title={w.plan_name || undefined}>
        {w.plan_name || '—'}
      </td>
      <td className="py-2 pr-3 text-text-muted" data-testid="agent-workitem-priority">
        {/* v2.7.1 fallback: no priority schema yet (#231). */}—
      </td>
      <td className="py-2 pr-3" data-testid="agent-workitem-status">
        <span className={`rounded px-1.5 py-0.5 text-[0.625rem] font-medium uppercase tracking-wide ${statusCls}`}>
          {statusLabel}
        </span>
      </td>
      <td className="py-2 tabular-nums text-text-muted" data-testid="agent-workitem-updated" title={w.updated_at}>
        {formatUpdated(w.updated_at, t)}
      </td>
    </tr>
  );
}

function formatUpdated(iso: string, t: TFunction): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' });
  const yesterday = new Date(now);
  yesterday.setDate(now.getDate() - 1);
  if (d.toDateString() === yesterday.toDateString()) return t('agentRuntime.tasks.yesterday');
  return d.toLocaleDateString();
}

// waitingFor — how long a pending task has been queued (since its last update).
function waitingFor(iso: string): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return '';
  const s = Math.floor((Date.now() - t) / 1000);
  if (s < 0) return '';
  return formatAge(s * 1000);
}
