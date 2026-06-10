import React, { useState } from 'react';
import { useParams } from 'react-router-dom';
import { OrgLink } from '@/OrgContext';
import { useProject } from '@/api/projects';
import { usePlans, useCreatePlan, type Plan, type CreatePlanInput } from '@/api/plans';
import { formatLocalTime } from '@/utils/time';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { PlanStatusChip, PlanFailedIndicator, planProgressLabel } from '@/components/planDisplay';

// ProjectPlans (/projects/:id/plans) — v2.9 #286. The parallel Plan list for a
// project + a "New Plan" action. Reached via the project detail Plans tab (and
// directly addressable). A Plan groups backlog tasks into a depends_on DAG; the
// DAG view itself is #287 (this is the foundation + list).
export default function ProjectPlans(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const project = useProject(id);
  const plans = usePlans(id);
  const [createOpen, setCreateOpen] = useState(false);

  const projectName = project.data?.name ?? id;

  return (
    <section className="space-y-4" data-testid="page-ProjectPlans" data-project-id={id}>
      <Breadcrumb
        items={[
          { label: 'Projects', to: '/projects' },
          { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
          { label: 'Plans' },
        ]}
      />
      <header className="flex flex-wrap items-center justify-between gap-2 border-b border-border-base pb-3">
        <h1 className="font-heading text-2xl font-semibold text-text-primary">Plans</h1>
        <button
          type="button"
          className="rounded bg-brand px-2.5 py-1 text-xs font-medium text-white hover:bg-brand-hover"
          onClick={() => setCreateOpen(true)}
          data-testid="plan-create-btn"
        >
          + New Plan
        </button>
      </header>

      {createOpen && <PlanCreateModal projectId={id} onClose={() => setCreateOpen(false)} />}

      <PlanList projectId={id} query={plans} />
    </section>
  );
}

interface PlansQuery {
  data?: Plan[];
  isLoading: boolean;
  isError: boolean;
  error: unknown;
}

// PlanList — the parallel list. Each Plan: name, status chip, progress done/total,
// has_failed indicator, target_date; click → Plan detail (#287 DAG placeholder).
export function PlanList({
  projectId,
  query,
}: {
  projectId: string;
  query: PlansQuery;
}): React.ReactElement {
  const data = query.data ?? [];
  if (query.isLoading) {
    return (
      <div className="space-y-2" data-testid="plans-loading">
        <Skeleton height="3rem" />
        <Skeleton height="3rem" />
      </div>
    );
  }
  if (query.isError) {
    return (
      <ErrorState
        message="Couldn't load plans."
        error={query.error}
        testId="plans-error"
      />
    );
  }
  if (data.length === 0) {
    return (
      <p className="py-6 text-center text-xs text-text-muted" data-testid="plans-empty">
        No plans yet. Create one to group backlog tasks into a dependency plan.
      </p>
    );
  }
  return (
    <ul
      className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3"
      data-testid="plans-list"
    >
      {data.map((plan) => (
        <li key={plan.id} data-testid="plan-card" data-plan-id={plan.id} data-status={plan.status}>
          <OrgLink
            to={`/projects/${encodeURIComponent(projectId)}/plans/${encodeURIComponent(plan.id)}`}
            className="block rounded-lg border border-border-base bg-bg-elevated p-4 shadow-1 hover:border-accent"
            data-testid="plan-card-link"
          >
            <div className="flex items-start justify-between gap-2">
              <h2 className="truncate font-heading text-sm font-semibold text-text-primary" title={plan.name}>
                {plan.name}
              </h2>
              <span className="flex shrink-0 items-center gap-1">
                <PlanStatusChip status={plan.status} />
                <PlanFailedIndicator hasFailed={plan.has_failed} />
              </span>
            </div>
            {plan.description && (
              <p className="mt-1 line-clamp-2 text-xs text-text-secondary">{plan.description}</p>
            )}
            <dl className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-text-muted">
              <div className="flex items-center gap-1">
                <dt className="uppercase tracking-wide text-[0.625rem]">Progress</dt>
                <dd className="tabular-nums text-text-secondary" data-testid="plan-progress">
                  {planProgressLabel(plan.progress)}
                </dd>
              </div>
              {plan.target_date && (
                <div className="flex items-center gap-1">
                  <dt className="uppercase tracking-wide text-[0.625rem]">Target</dt>
                  <dd className="text-text-secondary" data-testid="plan-target-date" title={plan.target_date}>
                    {formatLocalTime(plan.target_date)}
                  </dd>
                </div>
              )}
            </dl>
          </OrgLink>
        </li>
      ))}
    </ul>
  );
}

const modalInputClass =
  'mt-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

// PlanCreateModal — create an empty Plan (name + optional description + target
// date). target_date is a native date input; converted to an RFC3339 instant
// (local-offset) so the backend stores an absolute time, not a naive UTC date.
export function PlanCreateModal({
  projectId,
  onClose,
}: {
  projectId: string;
  onClose: () => void;
}): React.ReactElement {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [targetDate, setTargetDate] = useState('');
  const create = useCreatePlan(projectId);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    const input: CreatePlanInput = { name: name.trim() };
    if (description.trim()) input.description = description.trim();
    // YYYY-MM-DD → RFC3339 with local offset (absolute instant, not naive UTC).
    if (targetDate) {
      const d = new Date(`${targetDate}T00:00:00`);
      if (!Number.isNaN(d.getTime())) input.target_date = d.toISOString();
    }
    try {
      await create.mutateAsync(input);
      onClose();
    } catch {
      // surfaced inline below
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="plan-create-modal"
      role="dialog"
      aria-modal="true"
      aria-label="New plan"
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold">New Plan</h2>
        <label className="block text-xs font-medium" htmlFor="plan-name">
          Name
        </label>
        <input
          id="plan-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={modalInputClass}
          data-testid="plan-create-name"
          autoFocus
        />
        <label className="mt-3 block text-xs font-medium" htmlFor="plan-description">
          Description
        </label>
        <textarea
          id="plan-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className={modalInputClass}
          data-testid="plan-create-description"
        />
        <label className="mt-3 block text-xs font-medium" htmlFor="plan-target-date">
          Target date
        </label>
        <input
          id="plan-target-date"
          type="date"
          lang="en"
          value={targetDate}
          onChange={(e) => setTargetDate(e.target.value)}
          className={modalInputClass}
          data-testid="plan-create-target-date"
        />
        {create.isError && (
          <p className="mt-3 text-xs text-danger" data-testid="plan-create-error">
            {(create.error as Error).message}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={create.isPending || !name.trim()}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-create-submit"
          >
            {create.isPending ? 'Creating…' : 'Create Plan'}
          </button>
        </div>
      </form>
    </div>
  );
}
