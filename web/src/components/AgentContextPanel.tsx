import type React from 'react';
import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { OrgLink } from '@/OrgContext';
import { useAgentTasks } from '@/api/agents';
import { usePlans, type Plan } from '@/api/plans';
import { planProgressLabel } from '@/components/planDisplay';
import { refLabel } from '@/components/workItemDisplay';
import type { AgentTask, AgentTaskStatus } from '@/api/types';

// ============================================================================
// v2.10.0 [T7] Members — the Agent "current work" context block. Surfaces the
// selected agent's CURRENT work item and the PLAN that work item belongs to
// (mockup `docs/design/v2.10.0/members.html` §①, "当前工作项 · 归属计划").
//
// v2.24.x (@oopslink): the standalone right-hand col④ sidebar was retired as
// redundant with the Tasks tab; this block now renders inline at the TOP of the
// Tasks tab (AgentTasks) via the `inline` variant (a wide 2-column grid). The
// default (sidebar/stacked) layout is kept for back-compat / isolated tests.
//
// Labels follow the app's English UI convention (the mockup annotates in zh-CN,
// but every live string in the console — Profile/Activity/Work items/Humans/… —
// is English).
// ============================================================================

// A task is "current" if it is active; failing that, the most-recently
// updated non-terminal task; failing that, the most recent task overall. This
// mirrors what an operator means by "what is this agent doing right now".
const TERMINAL_STATUS: ReadonlySet<AgentTaskStatus> = new Set<AgentTaskStatus>([
  'done',
  'canceled',
  'superseded',
  'failed',
]);

// Localized label for a task status. The status string is the stable
// discriminator (from the API); only the displayed label is translated. An
// unmapped status falls back to its raw value (never a missing-key render).
const STATUS_LABEL_KEY: Record<AgentTaskStatus, string> = {
  active: 'agents.context.status.active',
  paused: 'agents.context.status.paused',
  queued: 'agents.context.status.queued',
  waiting_input: 'agents.context.status.waiting_input',
  failed: 'agents.context.status.failed',
  done: 'agents.context.status.done',
  canceled: 'agents.context.status.canceled',
  superseded: 'agents.context.status.superseded',
};

function i18nStatusLabel(status: AgentTaskStatus, t: TFunction): string {
  const key = STATUS_LABEL_KEY[status];
  return key ? t(key) : status;
}

function byUpdatedDesc(a: AgentTask, b: AgentTask): number {
  return (b.updated_at ?? '').localeCompare(a.updated_at ?? '');
}

export function pickCurrentTask(items: AgentTask[]): AgentTask | undefined {
  if (items.length === 0) return undefined;
  const active = items.find((w) => w.status === 'active');
  if (active) return active;
  const sorted = [...items].sort(byUpdatedDesc);
  return sorted.find((w) => !TERMINAL_STATUS.has(w.status)) ?? sorted[0];
}

function taskIdOf(item: AgentTask): string {
  return item.task_id || item.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
}

export function AgentContextPanel({
  agentId,
  inline = false,
}: {
  agentId: string;
  // inline: wide 2-column layout for embedding at the top of the Tasks tab.
  // Default (false): the stacked/padded layout the old col④ sidebar used.
  inline?: boolean;
}): React.ReactElement {
  const { t } = useTranslation('members');
  const workItems = useAgentTasks(agentId);
  const current = useMemo(
    () => pickCurrentTask(workItems.data ?? []),
    [workItems.data],
  );

  // Owning plan: resolve the current task → the plan whose node set
  // contains it. As of v2.9.2 (task-0543ece9) the plan-list `nodes_preview`
  // carries EVERY node (the old 4-node cap is gone), so the list read is a
  // reliable membership source — no per-plan detail fetch needed.
  const plans = usePlans(current?.project_id);
  const owningPlan: Plan | undefined = useMemo(() => {
    if (!current) return undefined;
    const taskId = taskIdOf(current);
    if (!taskId) return undefined;
    return (plans.data ?? []).find((p) =>
      (p.nodes_preview ?? p.nodes ?? []).some((n) => n.task_id === taskId),
    );
  }, [plans.data, current]);

  return (
    <div
      className={inline ? 'grid gap-4 sm:grid-cols-2' : 'flex flex-col gap-5 p-4'}
      data-testid="agent-context-panel"
    >
      <section data-testid="agent-context-current">
        <h4 className="mb-2 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
          {t('agents.context.currentTask')}
        </h4>
        {workItems.isLoading ? (
          <p className="text-xs text-text-muted" data-testid="agent-context-loading">
            {t('agents.context.loading')}
          </p>
        ) : current ? (
          <CurrentTaskCard item={current} />
        ) : (
          <p className="text-xs text-text-muted" data-testid="agent-context-no-task">
            {t('agents.context.noTask')}
          </p>
        )}
      </section>

      <section data-testid="agent-context-plan">
        <h4 className="mb-2 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
          {t('agents.context.plan')}
        </h4>
        {owningPlan ? (
          <OwningPlanRow plan={owningPlan} />
        ) : (
          <p className="text-xs text-text-muted" data-testid="agent-context-no-plan">
            {current ? t('agents.context.notPartOfPlan') : '—'}
          </p>
        )}
      </section>
    </div>
  );
}

function CurrentTaskCard({ item }: { item: AgentTask }): React.ReactElement {
  const { t } = useTranslation('members');
  const taskId = taskIdOf(item);
  const linkable = Boolean(item.task_title && item.project_id && taskId);
  // T100/T126: prefer the task's org_ref (T84). Tasks carry no human-facing
  // number (#192), so absent an org_ref fall back to the FULL task id
  // (never the retired #id-tail hash; full ref on hover).
  const handle = refLabel(item.org_ref, taskId || item.id);
  // The task status is a STABLE discriminator; the label is localized here. An
  // unknown/new status falls back to the raw value rather than a missing key.
  const statusLabel = i18nStatusLabel(item.status, t);

  return (
    <div
      className="rounded-lg border border-border-base bg-bg-base p-3"
      data-testid="agent-context-task"
      data-task-id={item.id}
      data-status={item.status}
    >
      {linkable ? (
        <OrgLink
          to={`/projects/${encodeURIComponent(item.project_id as string)}/tasks/${encodeURIComponent(taskId)}`}
          className="block text-sm font-medium text-text-primary hover:text-accent"
          data-testid="agent-context-task-link"
        >
          {item.task_title}
        </OrgLink>
      ) : (
        <span className="block text-sm font-medium text-text-primary">
          {item.task_title || t('agents.context.taskFallback')}
        </span>
      )}
      <div className="mt-1 text-xs text-text-muted">
        <span data-testid="agent-context-task-status">{statusLabel}</span>
        {' · '}
        <span className="font-mono" title={item.task_ref}>
          {handle}
        </span>
      </div>
    </div>
  );
}

function OwningPlanRow({ plan }: { plan: Plan }): React.ReactElement {
  const { t } = useTranslation('members');
  return (
    <OrgLink
      to={`/projects/${encodeURIComponent(plan.project_id)}/plans/${encodeURIComponent(plan.id)}`}
      className="flex items-start justify-between gap-2 rounded-lg border border-border-base bg-bg-base p-3 hover:border-border-strong"
      data-testid="agent-context-plan-link"
      data-plan-id={plan.id}
    >
      <div className="min-w-0">
        <span className="block truncate text-sm font-medium text-text-primary">{plan.name}</span>
        <span className="mt-0.5 block text-xs text-text-muted">
          {planProgressLabel(plan.progress)}
        </span>
      </div>
      <span className="shrink-0 rounded bg-brand/10 px-1.5 py-0.5 text-[0.625rem] font-semibold uppercase tracking-wide text-brand">
        {t('agents.context.planBadge')}
      </span>
    </OrgLink>
  );
}
