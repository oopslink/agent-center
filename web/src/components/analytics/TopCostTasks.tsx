import type React from 'react';
import { useState } from 'react';
import type { AnalyticsTopTask } from '@/api/types';
import { useAgentAnalyticsTask } from '@/api/analytics';
import { formatCostMicros, formatTokens } from '@/utils/format';
import { formatLocalTime } from '@/utils/time';
import { modelColor, modelShortLabel } from './modelColors';

// I28/F6 Top Cost Tasks (1:1 with the English mockup): a ranked #1..N list of the
// agent's most expensive tasks this month — title (falling back to the task_id
// when unresolved), a cost-proportional bar tinted by the dominant model, the $
// amount, and the model label. Clicking a row drills down into that task's raw
// usage_events (lazy fetch).

function ChevronIcon({ open }: { open: boolean }): React.ReactElement {
  return (
    <svg
      viewBox="0 0 16 16"
      className={['h-3.5 w-3.5 stroke-current transition-transform', open ? 'rotate-90' : ''].join(' ')}
      fill="none"
      strokeWidth="1.5"
      aria-hidden="true"
    >
      <path d="M6 4l4 4-4 4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function TaskRow({
  task,
  rank,
  agentId,
  maxCost,
}: {
  task: AnalyticsTopTask;
  rank: number;
  agentId: string;
  maxCost: number;
}): React.ReactElement {
  const [open, setOpen] = useState(false);
  const drill = useAgentAnalyticsTask(agentId, task.task_id, open);
  const ref = (task.org_ref ?? '').trim();
  const label = task.title.trim() !== '' ? task.title : task.task_id;
  const color = modelColor(task.dominant_model);
  const widthPct = maxCost > 0 ? Math.max(2, Math.round((task.cost_micros / maxCost) * 100)) : 0;

  return (
    <li className="border-b border-border-base last:border-b-0" data-testid={`top-task-${task.task_id}`}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-3 py-2 text-left hover:bg-bg-subtle"
      >
        <span className="w-6 shrink-0 text-xs tabular-nums text-text-muted">#{rank}</span>
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex items-baseline gap-1.5">
            {ref !== '' && (
              <span
                className="shrink-0 font-mono text-xs text-text-muted"
                data-testid={`top-task-ref-${task.task_id}`}
              >
                {ref}
              </span>
            )}
            <span
              className="truncate text-sm text-text-primary"
              title={ref !== '' ? `${ref} ${label}` : label}
              data-testid={`top-task-label-${task.task_id}`}
            >
              {label}
            </span>
          </span>
          <span className="h-1.5 w-full overflow-hidden rounded-full bg-bg-subtle">
            <span className="block h-full rounded-full" style={{ width: `${widthPct}%`, backgroundColor: color }} />
          </span>
        </span>
        <span className="flex shrink-0 flex-col items-end">
          <span className="text-sm font-medium tabular-nums text-text-primary" data-testid={`top-task-cost-${task.task_id}`}>
            {formatCostMicros(task.cost_micros)}
          </span>
          {task.dominant_model !== '' && (
            <span className="text-[0.625rem] uppercase tracking-wide" style={{ color }}>
              {modelShortLabel(task.dominant_model)}
            </span>
          )}
        </span>
        <ChevronIcon open={open} />
      </button>
      {open && (
        <div className="px-9 pb-2" data-testid={`top-task-drill-${task.task_id}`}>
          {drill.isLoading && <p className="py-1 text-xs text-text-muted">Loading usage…</p>}
          {drill.isError && <p className="py-1 text-xs text-danger">Failed to load usage events.</p>}
          {drill.data && drill.data.events.length === 0 && (
            <p className="py-1 text-xs text-text-muted">No usage events for this task.</p>
          )}
          {drill.data && drill.data.events.length > 0 && (
            <table className="w-full text-xs">
              <thead>
                <tr className="text-text-muted">
                  <th className="py-1 text-left font-normal">When</th>
                  <th className="py-1 text-left font-normal">Model</th>
                  <th className="py-1 text-right font-normal">Tokens</th>
                  <th className="py-1 text-right font-normal">Cost</th>
                </tr>
              </thead>
              <tbody>
                {drill.data.events.map((e) => (
                  <tr key={e.id} className="text-text-primary">
                    <td className="py-0.5">{formatLocalTime(e.ts)}</td>
                    <td className="py-0.5">{modelShortLabel(e.model)}</td>
                    <td className="py-0.5 text-right tabular-nums">{formatTokens(e.tokens_in + e.tokens_out)}</td>
                    <td className="py-0.5 text-right tabular-nums">{formatCostMicros(e.cost_micros)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
    </li>
  );
}

export function TopCostTasks({
  tasks,
  agentId,
}: {
  tasks: AnalyticsTopTask[];
  agentId: string;
}): React.ReactElement {
  const maxCost = tasks.reduce((m, t) => Math.max(m, t.cost_micros), 0);
  return (
    <section className="flex min-h-[18rem] flex-col rounded-lg border border-border-base bg-bg-elevated p-5" data-testid="analytics-top-tasks">
      <h3 className="mb-3 text-sm font-semibold text-text-primary">Top Cost Tasks</h3>
      {tasks.length === 0 ? (
        <p className="flex flex-1 items-center justify-center text-center text-sm text-text-muted" data-testid="analytics-top-tasks-empty">
          No task-scoped usage this month.
        </p>
      ) : (
        <ol className="flex flex-col">
          {tasks.map((t, i) => (
            <TaskRow key={t.task_id} task={t} rank={i + 1} agentId={agentId} maxCost={maxCost} />
          ))}
        </ol>
      )}
    </section>
  );
}
