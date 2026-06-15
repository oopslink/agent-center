import type React from 'react';
import { useMemo } from 'react';
import { OrgLink } from '@/OrgContext';
import { useAgentWorkItems } from '@/api/agents';
import { usePlans, type Plan } from '@/api/plans';
import { planProgressLabel } from '@/components/planDisplay';
import { refLabel } from '@/components/workItemDisplay';
import type { AgentWorkItem, WorkItemStatus } from '@/api/types';

// ============================================================================
// v2.10.0 [T7] Members — the col④ on-demand context panel for an Agent detail
// view. Surfaces the selected agent's CURRENT work item and the PLAN that work
// item belongs to (mockup `docs/design/v2.10.0/members.html` §①, col④ "当前工作项
// · 归属计划"). Rendered by AgentDetail inside <ContextPanel> so it portals into
// the shell's fourth column (and is simply absent in three-column layouts /
// isolated tests with no shell host).
//
// Labels follow the app's English UI convention (the mockup annotates in zh-CN,
// but every live string in the console — Profile/Activity/Work items/Humans/… —
// is English).
// ============================================================================

// A work item is "current" if it is active; failing that, the most-recently
// updated non-terminal item; failing that, the most recent item overall. This
// mirrors what an operator means by "what is this agent doing right now".
const TERMINAL_STATUS: ReadonlySet<WorkItemStatus> = new Set<WorkItemStatus>([
  'done',
  'canceled',
  'superseded',
  'failed',
]);

const STATUS_LABEL: Record<WorkItemStatus, string> = {
  active: 'Running',
  paused: 'Paused',
  queued: 'Pending',
  waiting_input: 'Blocked',
  failed: 'Failed',
  done: 'Done',
  canceled: 'Canceled',
  superseded: 'Superseded',
};

function byUpdatedDesc(a: AgentWorkItem, b: AgentWorkItem): number {
  return (b.updated_at ?? '').localeCompare(a.updated_at ?? '');
}

export function pickCurrentWorkItem(items: AgentWorkItem[]): AgentWorkItem | undefined {
  if (items.length === 0) return undefined;
  const active = items.find((w) => w.status === 'active');
  if (active) return active;
  const sorted = [...items].sort(byUpdatedDesc);
  return sorted.find((w) => !TERMINAL_STATUS.has(w.status)) ?? sorted[0];
}

function taskIdOf(item: AgentWorkItem): string {
  return item.task_id || item.task_ref?.replace(/^pm:\/\/tasks\//, '') || '';
}

export function AgentContextPanel({ agentId }: { agentId: string }): React.ReactElement {
  const workItems = useAgentWorkItems(agentId);
  const current = useMemo(
    () => pickCurrentWorkItem(workItems.data ?? []),
    [workItems.data],
  );

  // Owning plan: resolve the current work item's task → the plan whose node set
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
    <div className="flex flex-col gap-5 p-4" data-testid="agent-context-panel">
      <section data-testid="agent-context-current">
        <h4 className="mb-2 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
          Current work item
        </h4>
        {workItems.isLoading ? (
          <p className="text-xs text-text-muted" data-testid="agent-context-loading">
            Loading…
          </p>
        ) : current ? (
          <CurrentWorkItemCard item={current} />
        ) : (
          <p className="text-xs text-text-muted" data-testid="agent-context-no-workitem">
            No active work item.
          </p>
        )}
      </section>

      <section data-testid="agent-context-plan">
        <h4 className="mb-2 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted">
          Plan
        </h4>
        {owningPlan ? (
          <OwningPlanRow plan={owningPlan} />
        ) : (
          <p className="text-xs text-text-muted" data-testid="agent-context-no-plan">
            {current ? 'Not part of a plan.' : '—'}
          </p>
        )}
      </section>
    </div>
  );
}

function CurrentWorkItemCard({ item }: { item: AgentWorkItem }): React.ReactElement {
  const taskId = taskIdOf(item);
  const linkable = Boolean(item.task_title && item.project_id && taskId);
  // T100/T126: prefer the task's org_ref (T84). Work items carry no human-facing
  // number (#192), so absent an org_ref fall back to the FULL task/work-item id
  // (never the retired #id-tail hash; full ref on hover).
  const handle = refLabel(item.org_ref, taskId || item.id);
  const statusLabel = STATUS_LABEL[item.status] ?? item.status;

  return (
    <div
      className="rounded-lg border border-border-base bg-bg-base p-3"
      data-testid="agent-context-workitem"
      data-workitem-id={item.id}
      data-status={item.status}
    >
      {linkable ? (
        <OrgLink
          to={`/projects/${encodeURIComponent(item.project_id as string)}/tasks/${encodeURIComponent(taskId)}`}
          className="block text-sm font-medium text-text-primary hover:text-accent"
          data-testid="agent-context-workitem-link"
        >
          {item.task_title}
        </OrgLink>
      ) : (
        <span className="block text-sm font-medium text-text-primary">
          {item.task_title || 'Work item'}
        </span>
      )}
      <div className="mt-1 text-xs text-text-muted">
        <span data-testid="agent-context-workitem-status">{statusLabel}</span>
        {' · '}
        <span className="font-mono" title={item.task_ref}>
          {handle}
        </span>
      </div>
    </div>
  );
}

function OwningPlanRow({ plan }: { plan: Plan }): React.ReactElement {
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
        Plan
      </span>
    </OrgLink>
  );
}
