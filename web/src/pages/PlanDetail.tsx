import React, { useMemo } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useProject } from '@/api/projects';
import { usePlan, useAddTaskToPlan, useRemoveTaskFromPlan, type Plan } from '@/api/plans';
import { useTasksList } from '@/api/tasks';
import type { Task } from '@/api/types';
import { useDisplayNameResolver } from '@/api/members';
import { formatLocalTime } from '@/utils/time';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { StatusChip, idHandle, shortDate } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, planProgressLabel } from '@/components/planDisplay';

// PlanDetail (/projects/:id/plans/:planId) — v2.9. P1 #286 builds the Plan
// header + the backlog→Plan task SELECTION (reusing the work-items table idiom).
// The DAG view (dependency edges, node statuses, manual advance) is #287, which
// fills the placeholder section below on top of the already-shipped plans.ts
// dependency / lifecycle hooks.
export default function PlanDetail(): React.ReactElement {
  const { id = '', planId = '' } = useParams<{ id: string; planId: string }>();
  const project = useProject(id);
  const plan = usePlan(id, planId);

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
      <section className="space-y-3" data-testid="page-PlanDetail">
        <ErrorState
          message="Couldn't load this plan."
          error={plan.error}
          testId="plan-not-found"
        />
        <OrgLink
          to={`/projects/${encodeURIComponent(id)}/plans`}
          className="text-xs text-accent hover:underline"
        >
          ← Back to plans
        </OrgLink>
      </section>
    );
  }
  if (!plan.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-PlanDetail">
        Plan lookup failed.
      </section>
    );
  }

  const p = plan.data;
  return (
    <section className="space-y-4" data-testid="page-PlanDetail" data-plan-id={p.id}>
      <Breadcrumb
        items={[
          { label: 'Projects', to: '/projects' },
          { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
          { label: 'Plans', to: `/projects/${encodeURIComponent(id)}/plans` },
          { label: p.name },
        ]}
      />
      <header className="space-y-2 border-b border-border-base pb-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="font-heading text-2xl font-semibold text-text-primary" title={p.id}>
              {p.name}
            </h1>
            <PlanStatusChip status={p.status} />
            <PlanFailedIndicator hasFailed={p.has_failed} />
          </div>
          <span className="text-xs text-text-muted" data-testid="plan-progress">
            {planProgressLabel(p.progress)} done
          </span>
        </div>
        {p.description && (
          <p className="max-w-3xl text-sm text-text-secondary">{p.description}</p>
        )}
        <dl className="flex flex-wrap gap-x-4 text-xs text-text-muted">
          {p.target_date && (
            <div className="flex items-center gap-1">
              <dt className="uppercase tracking-wide text-[0.625rem]">Target</dt>
              <dd className="text-text-secondary" title={p.target_date}>
                {formatLocalTime(p.target_date)}
              </dd>
            </div>
          )}
          <div className="flex items-center gap-1">
            <dt className="uppercase tracking-wide text-[0.625rem]">Created</dt>
            <dd className="text-text-secondary" title={p.created_at}>
              {formatLocalTime(p.created_at)}
            </dd>
          </div>
        </dl>
      </header>

      <PlanTaskSelection projectId={id} plan={p} />

      {/* #287 DAG view placeholder — the dependency graph, node statuses, and
          manual advance render here on top of the plans.ts dependency/lifecycle
          hooks. Kept as an explicit placeholder so the route is reachable now. */}
      <div
        className="rounded-lg border border-dashed border-border-base p-4 text-xs text-text-secondary"
        data-testid="plan-dag-placeholder"
      >
        The dependency graph (DAG) view, node statuses, and manual advance are
        coming in the next task (#287).
      </div>
    </section>
  );
}

// PlanTaskSelection (#286) — the backlog→Plan selection UI. Reuses the
// work-items table idiom (OrgWorkItemsView pattern: ID / Title / Status /
// Assigned to / Updated columns, StatusChip, idHandle, formatLocalTime). Only
// editable while the Plan is `draft` (the DAG is editable only in draft, §9.4).
//
// A task is in 0..1 Plan. We treat "in this Plan" via the Plan's node set; the
// backlog candidates are project tasks NOT already in this Plan. (The full
// "no plan_id" filter lands when the Task DTO carries plan_id; until then we at
// least exclude the tasks already selected here.)
export function PlanTaskSelection({
  projectId,
  plan,
}: {
  projectId: string;
  plan: Plan;
}): React.ReactElement {
  const tasks = useTasksList(projectId);
  const resolveName = useDisplayNameResolver();
  const addTask = useAddTaskToPlan(projectId, plan.id);
  const removeTask = useRemoveTaskFromPlan(projectId, plan.id);
  const editable = plan.status === 'draft';

  const selectedIds = useMemo(
    () => new Set((plan.nodes ?? []).map((n) => n.task_id)),
    [plan.nodes],
  );
  const allTasks = tasks.data ?? [];
  // Backlog candidates = project tasks not already selected into THIS Plan.
  const backlog = allTasks.filter((t) => !selectedIds.has(t.id));
  const selected = allTasks.filter((t) => selectedIds.has(t.id));

  return (
    <div className="space-y-4" data-testid="plan-task-selection">
      {/* Selected tasks (the Plan's node set). Removable while draft. */}
      <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1">
        <h2 className="mb-2 font-heading text-sm font-semibold text-text-primary">
          Tasks in this plan
        </h2>
        {selected.length === 0 ? (
          <p className="py-4 text-center text-xs text-text-muted" data-testid="plan-selected-empty">
            No tasks selected yet.
          </p>
        ) : (
          <TaskTable
            testid="plan-selected-table"
            projectId={projectId}
            tasks={selected}
            resolveName={resolveName}
            action={
              editable
                ? {
                    label: 'Remove',
                    testidPrefix: 'plan-remove-task',
                    pending: removeTask.isPending,
                    onClick: (taskId) => removeTask.mutate(taskId),
                  }
                : undefined
            }
          />
        )}
        {removeTask.isError && (
          <p className="mt-2 text-xs text-danger" data-testid="plan-remove-error">
            {(removeTask.error as Error).message}
          </p>
        )}
      </div>

      {/* Backlog picker — only while draft. Multi-select via per-row Add. */}
      {editable && (
        <div className="rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1">
          <h2 className="mb-2 font-heading text-sm font-semibold text-text-primary">
            Add from backlog
          </h2>
          {tasks.isLoading ? (
            <div className="space-y-2 py-2">
              <Skeleton height="1.5rem" />
              <Skeleton height="1.5rem" />
            </div>
          ) : tasks.isError ? (
            <p className="py-2 text-xs text-danger" data-testid="plan-backlog-error">
              {(tasks.error as Error).message}
            </p>
          ) : backlog.length === 0 ? (
            <p className="py-4 text-center text-xs text-text-muted" data-testid="plan-backlog-empty">
              No backlog tasks available.
            </p>
          ) : (
            <TaskTable
              testid="plan-backlog-table"
              projectId={projectId}
              tasks={backlog}
              resolveName={resolveName}
              action={{
                label: 'Add',
                testidPrefix: 'plan-add-task',
                pending: addTask.isPending,
                onClick: (taskId) => addTask.mutate({ task_id: taskId }),
              }}
            />
          )}
          {addTask.isError && (
            <p className="mt-2 text-xs text-danger" data-testid="plan-add-error">
              {(addTask.error as Error).message}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

// TaskTable — the shared work-items table body reused for both the selected and
// the backlog lists (ID / Title / Status / Assigned to / Updated + an action
// column). Mirrors the OrgWorkItemsView / ProjectDetail tasks table columns.
function TaskTable({
  testid,
  projectId,
  tasks,
  resolveName,
  action,
}: {
  testid: string;
  projectId: string;
  tasks: Task[];
  resolveName: (ref: string) => string;
  action?: {
    label: string;
    testidPrefix: string;
    pending: boolean;
    onClick: (taskId: string) => void;
  };
}): React.ReactElement {
  return (
    <div className="overflow-x-auto">
      <table className="w-full text-left text-xs" data-testid={testid}>
        <thead>
          <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
            <th className="py-1.5 pr-3 font-medium">ID</th>
            <th className="py-1.5 pr-3 font-medium">Title</th>
            <th className="py-1.5 pr-3 font-medium">Status</th>
            <th className="py-1.5 pr-3 font-medium">Assigned to</th>
            <th className="py-1.5 pr-3 font-medium">Updated</th>
            {action && <th className="py-1.5 font-medium" />}
          </tr>
        </thead>
        <tbody className="divide-y divide-border-base">
          {tasks.map((tk) => (
            <tr key={tk.id} data-testid="plan-task-row" data-task-id={tk.id} data-status={tk.status}>
              <td className="py-1.5 pr-3 font-mono text-text-muted" title={tk.id}>
                {tk.org_ref || `#${idHandle(tk.id)}`}
              </td>
              <td className="max-w-[16rem] truncate py-1.5 pr-3">
                <OrgLink
                  to={`/projects/${encodeURIComponent(projectId)}/tasks/${encodeURIComponent(tk.id)}`}
                  className="text-text-primary hover:text-accent"
                >
                  {tk.title || tk.id}
                </OrgLink>
              </td>
              <td className="py-1.5 pr-3">
                <StatusChip status={tk.status} />
              </td>
              <td className="py-1.5 pr-3 text-text-secondary">
                {tk.assignee ? (
                  <span title={tk.assignee}>
                    {resolveName(tk.assignee) === tk.assignee ? tk.assignee : resolveName(tk.assignee)}
                  </span>
                ) : (
                  '—'
                )}
              </td>
              <td className="py-1.5 pr-3 tabular-nums text-text-muted" title={formatLocalTime(tk.updated_at)}>
                {shortDate(tk.updated_at)}
              </td>
              {action && (
                <td className="py-1.5 text-right">
                  <button
                    type="button"
                    data-testid={`${action.testidPrefix}-${tk.id}`}
                    disabled={action.pending}
                    onClick={() => action.onClick(tk.id)}
                    className="rounded border border-border-base px-2 py-0.5 text-xs text-text-primary hover:bg-bg-subtle disabled:opacity-50"
                  >
                    {action.label}
                  </button>
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
