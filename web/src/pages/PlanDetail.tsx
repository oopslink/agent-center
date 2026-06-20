import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { OrgLink, orgPath, useOptionalOrgContext } from '@/OrgContext';
import { useProject } from '@/api/projects';
import { ApiError } from '@/api/client';
import {
  usePlan,
  useStartPlan,
  useStopPlan,
  useAdvancePlan,
  useAddDependency,
  useRemoveDependency,
  useRemoveTaskFromPlan,
  useResumePausedNode,
  usePatchPlan,
  useDeletePlan,
  useArchivePlan,
  friendlyDestructivePlanError,
  type Plan,
  type PlanNode,
  type PlanNodeStatus,
  type PatchPlanInput,
} from '@/api/plans';
import { useConversation } from '@/api/conversations';
import { useAssignTask, useUnassignTask } from '@/api/tasks';
import {
  useDisplayNameResolver,
  useMembers,
  identityRefOf,
  normalizeIdentityRef,
  refKind,
  type MemberResult,
} from '@/api/members';
import { formatLocalTime } from '@/utils/time';
import { Skeleton } from '@/components/Skeleton';
import { Breadcrumb } from '@/components/Breadcrumb';
import { ErrorState } from '@/components/ErrorState';
import { Avatar } from '@/components/Avatar';
import { EntitySelect, type EntityOption } from '@/components/EntitySelect';
import { StatusChip, refLabel } from '@/components/workItemDisplay';
import { PlanStatusChip, PlanFailedIndicator, AutoAdvancingIndicator, TaskArchivedBadge, planProgressLabel, PlanRefTag } from '@/components/planDisplay';
import { ConversationView } from '@/components/ConversationView';
import { ConversationSidebar } from '@/components/ConversationSidebar';
import { ContextPanel } from '@/shell/contextPanel';
import { SenderSidebarProvider } from '@/components/SenderSidebarContext';
import { TaskTitleLink } from '@/components/TaskTitleLink';
import { dependencyEdgeError, validDropTargets } from './planDagEdit';

// PlanDetail (/projects/:id/plans/:planId) — v2.9 Plan-Orchestration EXECUTION
// view (#287). The mockup's ② Plan Detail: a header (name + status + failed +
// meta + Start/Stop), two tabs (DAG「推进计划」 / Task list「任务列表 N」 — NO
// backlog; selection lives on the #291 Work Board), a grid of the DAG (main) +
// the Plan conversation (side ~330px). The #286 backlog→Plan SELECTION is
// REMOVED here entirely.
//
// node_status is DERIVED by the orchestrator (§9.2) — we DISPLAY it, never store
// or edit it. This view renders the derived DAG + header lifecycle controls
// (Start / Stop / Advance, each once).
//
// v2.9 Stage A1 (#287 fast-follow): a DRAFT plan's DAG is now EDITABLE here — a
// draft-only dependency-edge editor (add via labeled selects, remove via a list
// of edges) sits below the graph. The backend AddPlanDependency/RemovePlanDependency
// are draft-gated (§9.4: running/done → ErrPlanNotDraft), cycle-guarded
// (ErrPlanCycle) and self-edge-rejected (ErrSelfDependency); the editor is gated
// to draft to match, and a running/done plan stays DISPLAY-ONLY.

// v2.9.1 UX point 4: chat / DAG / Task list as three independent tabs (default
// chat). Replaces the prior DAG|Task two-tab + resizable chat side-splitter.
type Tab = 'chat' | 'dag' | 'tasks';

// (v2.9.1 point 4) The DAG↔chat resizable side-splitter (v2.9 Stage A8) was
// removed — chat is now a top-level tab (see the 3-tab layout above), so the
// chat-width state / localStorage persistence / lg-breakpoint splitter are gone.

export default function PlanDetail(): React.ReactElement {
  const { id = '', planId = '' } = useParams<{ id: string; planId: string }>();
  const project = useProject(id);
  const plan = usePlan(id, planId);
  // T184: the plan conversation gets the shared col④ sidebar too. Resolve it for
  // participants (enabled:!!id makes this a no-op until the plan loads).
  const planConv = useConversation(plan.data?.conversation_id);
  const [tab, setTab] = useState<Tab>('chat');

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
    <section
      className="flex min-h-0 flex-1 flex-col gap-4"
      data-testid="page-PlanDetail"
      data-plan-id={p.id}
    >
      <Breadcrumb
        items={[
          { label: 'Projects', to: '/projects' },
          { label: projectName, to: `/projects/${encodeURIComponent(id)}` },
          { label: 'Plans', to: `/projects/${encodeURIComponent(id)}/plans` },
          { label: p.name },
        ]}
      />

      <div className="flex min-h-0 flex-1 flex-col rounded-lg border border-border-base bg-bg-elevated shadow-1" data-testid="plan-detail-card">
        <PlanDetailHeader projectId={id} plan={p} />

        {/* Tabs — Chat (default) / DAG / Task List. English-only labels (T132:
            the prior「(中文)」括注 removed). NO backlog tab (planning is on the
            Board). v2.9.1 point 4. */}
        <div className="flex items-center gap-1 px-4 pt-2" role="tablist" data-testid="plan-tabs">
          <TabButton id="chat" active={tab === 'chat'} onSelect={setTab}>
            Chat
          </TabButton>
          <TabButton id="dag" active={tab === 'dag'} onSelect={setTab}>
            DAG
          </TabButton>
          <TabButton id="tasks" active={tab === 'tasks'} onSelect={setTab}>
            Task List
          </TabButton>
        </div>

        {/* Single tabbed content area (point 4: chat is now a tab, not a side
            splitter). Chat stays mounted-but-hidden across tabs so its SSE
            subscription + scroll/composer-draft survive; DAG/Task mount lazily
            when their tab is active. */}
        <div className="flex min-h-0 flex-1 flex-col p-4" data-testid="plan-detail-content">
          {/* Chat stays mounted-but-hidden across tabs (SSE/scroll/draft survive).
              When active it must FILL the card height so the message stream scrolls
              INSIDE the viewport instead of growing the page (T180). The flex
              classes are applied only when active — a `display:flex` utility would
              otherwise override the `hidden` attribute's `display:none`. */}
          <div
            role="tabpanel"
            hidden={tab !== 'chat'}
            data-testid="plan-panel-chat"
            className={tab === 'chat' ? 'flex min-h-0 flex-1 flex-col' : undefined}
          >
            <PlanConversationSide conversationId={p.conversation_id} />
          </div>
          <div role="tabpanel" hidden={tab !== 'dag'} data-testid="plan-panel-dag">
            {tab === 'dag' && <PlanDag projectId={id} plan={p} />}
          </div>
          <div role="tabpanel" hidden={tab !== 'tasks'} data-testid="plan-panel-tasks">
            {tab === 'tasks' && <PlanTaskList projectId={id} plan={p} />}
          </div>
        </div>
      </div>

      {/* T184: the plan's conversation gets the shared col④ sidebar
          (Participants / Threads / Files) — same as channels/DMs/tasks/issues. */}
      {planConv.data && (
        <ContextPanel>
          <ConversationSidebar
            conversationId={planConv.data.id}
            participants={planConv.data.participants ?? []}
          />
        </ContextPanel>
      )}
    </section>
  );
}

// ── Header ────────────────────────────────────────────────────────────────
function PlanDetailHeader({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  const start = useStartPlan(projectId, plan.id);
  const stop = useStopPlan(projectId, plan.id);
  const advance = useAdvancePlan(projectId, plan.id);
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState<null | 'delete' | 'archive'>(null);

  const creatorName = resolveName(plan.creator_ref);
  const creatorLabel =
    creatorName === plan.creator_ref ? normalizeIdentityRef(plan.creator_ref) : creatorName;

  // Destructive-action entry gate (PD bar 1 — UX, not security): Delete + Archive
  // are exposed ONLY for a NON-running, NON-archived plan. A running plan would
  // be rejected by the backend (409 plan_conflict) — the real boundary — so we
  // simply HIDE the entries rather than offer an action that can't succeed. An
  // archived plan is TERMINAL (re-archive/delete-after-archive aren't part of the
  // flow) so it shows read-only: no Delete / Archive.
  const canDestroy = plan.status !== 'running' && plan.status !== 'archived';

  return (
    <header className="space-y-2 border-b border-border-base p-4" data-testid="plan-detail-header">
      <div className="flex flex-wrap items-center gap-2">
        {/* v2.10.1 [T99]: the human Plan id (P123). */}
        <PlanRefTag planId={plan.id} orgRef={plan.org_ref} testId="plan-detail-ref" />
        <h1 className="font-heading text-xl font-semibold text-text-primary" title={plan.id}>
          {plan.name}
        </h1>
        <PlanStatusChip status={plan.status} />
        {/* P2-4: a RUNNING plan IS being auto-advanced (the orchestrator
            dispatches ready nodes by events). Subtle informational signal. */}
        {plan.status === 'running' && <AutoAdvancingIndicator variant="detail" />}
        <PlanFailedIndicator hasFailed={plan.has_failed} />
        <span className="flex-1" />
        {/* Lifecycle (§9.4 / §9.6): running → Advance (dispatch ready) + Stop
            (→ draft); draft → Start. Each control is rendered exactly ONCE here
            (the DAG footer keeps the legend only). */}
        {plan.status === 'running' && (
          <>
            {/* Manual Advance is KEPT as an OVERRIDE (§9.6): the system already
                auto-advances a running plan; this button is reframed as a "do it
                now" override (same idempotent dispatch path, INSERT-OR-IGNORE
                no-op if already dispatched). Function unchanged. */}
            <button
              type="button"
              data-testid="plan-advance-btn"
              disabled={advance.isPending}
              onClick={() => advance.mutate()}
              title="Manually dispatch ready nodes now (the system already advances automatically)"
              aria-label="Manually dispatch ready nodes now (the system already advances automatically)"
              className="rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-base disabled:opacity-50"
            >
              ▸ Advance now
            </button>
            <button
              type="button"
              data-testid="plan-stop-btn"
              disabled={stop.isPending}
              onClick={() => stop.mutate()}
              className="rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-base disabled:opacity-50"
            >
              ■ Stop (→ draft)
            </button>
          </>
        )}
        {/* T238: name + goal are DESCRIPTIVE metadata — editable in any
            non-archived status (draft/running/done). target_date stays draft-only
            (the modal hides it off-draft, and the backend rejects it). An archived
            plan is terminal/read-only, so no Edit. */}
        {plan.status !== 'archived' && (
          <button
            type="button"
            data-testid="plan-edit-btn"
            onClick={() => setEditing(true)}
            className="rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary"
          >
            Edit
          </button>
        )}
        {plan.status === 'draft' && (
          <button
            type="button"
            data-testid="plan-start-btn"
            disabled={start.isPending}
            onClick={() => start.mutate()}
            className="rounded bg-accent px-3 py-1.5 text-xs font-semibold text-white hover:opacity-90 disabled:opacity-50"
          >
            ▸ Start
          </button>
        )}
        {/* Destructive lifecycle (v2.9 Stage B): Archive + Delete. Exposed only
            for a NON-running, NON-archived plan (canDestroy). Each opens a
            CONSEQUENCE-explaining confirm modal — never acts on a single click.
            The real block on a running plan is the backend 409; hiding here is
            the UX gate. */}
        {canDestroy && (
          <>
            <button
              type="button"
              data-testid="plan-archive-btn"
              onClick={() => setConfirming('archive')}
              title="Archive this plan and all its tasks (terminal, cannot be undone)"
              className="rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary"
            >
              Archive
            </button>
            <button
              type="button"
              data-testid="plan-delete-btn"
              onClick={() => setConfirming('delete')}
              title="Delete this plan (unloads its tasks to the Backlog, cannot be undone)"
              className="rounded border border-danger bg-bg-subtle px-3 py-1.5 text-xs font-semibold text-danger hover:bg-bg-base"
            >
              Delete
            </button>
          </>
        )}
      </div>
      <dl className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-text-muted" data-testid="plan-detail-meta">
        <div className="flex items-center gap-1">
          <dt className="uppercase tracking-wide text-[0.625rem]">Progress</dt>
          <dd className="text-text-secondary" data-testid="plan-progress">
            {planProgressLabel(plan.progress)}
          </dd>
        </div>
        {plan.target_date && (
          <div className="flex items-center gap-1">
            <dt className="uppercase tracking-wide text-[0.625rem]">Target</dt>
            <dd className="text-text-secondary" title={plan.target_date}>
              {formatLocalTime(plan.target_date)}
            </dd>
          </div>
        )}
        <div className="flex items-center gap-1">
          <dt className="uppercase tracking-wide text-[0.625rem]">Creator</dt>
          <dd className="text-text-secondary" title={plan.creator_ref} data-testid="plan-creator">
            @{creatorLabel}
          </dd>
        </div>
      </dl>
      {(start.isError || stop.isError || advance.isError) && (
        <p className="text-xs text-danger" data-testid="plan-lifecycle-error">
          {((start.error ?? stop.error ?? advance.error) as Error).message}
        </p>
      )}
      {editing && (
        <PlanEditModal projectId={projectId} plan={plan} onClose={() => setEditing(false)} />
      )}
      {confirming === 'delete' && (
        <PlanDeleteModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
      )}
      {confirming === 'archive' && (
        <PlanArchiveModal projectId={projectId} plan={plan} onClose={() => setConfirming(null)} />
      )}
    </header>
  );
}

// ── Destructive confirm modals (v2.9 Stage B) ────────────────────────────────
// Both mirror the PlanEditModal/PlanCreateModal pattern (bg-black/50 scrim +
// solid bg-bg-elevated surface = the sanctioned both-mode-AA modal). They are
// CONSEQUENCE-EXPLAINING (PD bar 2): the body spells out exactly what the action
// does (not just "are you sure?"). Cancel closes WITHOUT acting. On error the
// modal STAYS OPEN and shows a FRIENDLY inline message (#218,
// friendlyDestructivePlanError — status-agnostic message-substring match).

// PlanDeleteModal — DELETE /{id}. On success the plan no longer exists, so we
// navigate AWAY to the project's Plans board (the detail route would 404).
function PlanDeleteModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const navigate = useNavigate();
  const org = useOptionalOrgContext();
  const del = useDeletePlan(projectId, plan.id);

  const onConfirm = async () => {
    try {
      await del.mutateAsync();
      // The plan is GONE — leave the (now-404) detail route for the board.
      onClose();
      navigate(orgPath(`/projects/${encodeURIComponent(projectId)}/plans`, org?.slug));
    } catch {
      // surfaced inline below (#218); modal stays open.
    }
  };

  return (
    <DestructiveConfirmModal
      testId="plan-delete-modal"
      title="Delete this plan?"
      planName={plan.name}
      body="This unloads all this plan's tasks back to the Backlog, permanently deletes the plan's conversation, and deletes the plan. This cannot be undone."
      confirmLabel="Delete plan"
      pendingLabel="Deleting…"
      pending={del.isPending}
      error={del.isError ? friendlyDestructivePlanError(del.error) : null}
      errorTestId="plan-delete-error"
      cancelTestId="plan-delete-cancel"
      confirmTestId="plan-delete-confirm"
      onCancel={onClose}
      onConfirm={onConfirm}
    />
  );
}

// PlanArchiveModal — POST /{id}/archive. On success the plan (+ all its tasks)
// flip to the terminal archived state; the plan stays readable, so we just close
// and let the invalidation refresh the now-archived view.
function PlanArchiveModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const archive = useArchivePlan(projectId, plan.id);

  const onConfirm = async () => {
    try {
      await archive.mutateAsync();
      onClose();
    } catch {
      // surfaced inline below (#218); modal stays open.
    }
  };

  return (
    <DestructiveConfirmModal
      testId="plan-archive-modal"
      title="Archive this plan?"
      planName={plan.name}
      body="This archives the plan and all its tasks (a terminal state). This cannot be undone."
      confirmLabel="Archive plan"
      pendingLabel="Archiving…"
      pending={archive.isPending}
      error={archive.isError ? friendlyDestructivePlanError(archive.error) : null}
      errorTestId="plan-archive-error"
      cancelTestId="plan-archive-cancel"
      confirmTestId="plan-archive-confirm"
      onCancel={onClose}
      onConfirm={onConfirm}
    />
  );
}

// Shared scrim+surface confirm dialog for the two destructive actions. Same
// modal idiom as PlanEditModal (bg-black/50 scrim + solid bg-bg-elevated). The
// confirm button is danger-toned (border + danger text, no white-on-light-red
// flip — that fails dark-mode AA).
function DestructiveConfirmModal({
  testId,
  title,
  planName,
  body,
  confirmLabel,
  pendingLabel,
  pending,
  error,
  errorTestId,
  cancelTestId,
  confirmTestId,
  onCancel,
  onConfirm,
}: {
  testId: string;
  title: string;
  planName: string;
  body: string;
  confirmLabel: string;
  pendingLabel: string;
  pending: boolean;
  error: string | null;
  errorTestId: string;
  cancelTestId: string;
  confirmTestId: string;
  onCancel: () => void;
  onConfirm: () => void;
}): React.ReactElement {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      data-testid={testId}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div className="w-full max-w-md rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-2 text-lg font-semibold">{title}</h2>
        <p className="mb-3 text-sm text-text-secondary">
          <span className="font-medium text-text-primary">{planName}</span>
        </p>
        <p className="text-sm text-text-secondary">{body}</p>
        {error && (
          <p className="mt-3 text-xs font-medium text-danger" role="alert" data-testid={errorTestId}>
            {error}
          </p>
        )}
        <div className="mt-5 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onCancel}
            data-testid={cancelTestId}
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={pending}
            className="rounded border border-danger bg-bg-subtle px-3 py-1.5 text-sm font-semibold text-danger hover:bg-bg-base disabled:opacity-50"
            onClick={onConfirm}
            data-testid={confirmTestId}
          >
            {pending ? pendingLabel : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Plan-edit modal (v2.9 Stage A3) ──────────────────────────────────────────
// Edits name / goal (= the `description` DTO field — the contract names it
// `description`, NOT `goal`; verified vs PatchPlanInput in api/plans.ts) /
// target_date via usePatchPlan (PATCH /{id}). draft-only — opened only from the
// header's draft-gated Edit button (§9.4: running/done is immutable, the backend
// rejects with ErrPlanNotDraft). Mirrors PlanCreateModal's structure + styling
// (bg-black/50 scrim + bg-bg-elevated surface = the sanctioned both-mode-AA modal
// pattern). #218: a patch failure surfaces a FRIENDLY inline message, never the
// raw API error.
//
// Partial-update / TargetDateSet semantics (verified vs the backend contract):
//   • Only CHANGED fields are sent (a no-op submit sends {}), so unchanged fields
//     stay untouched server-side.
//   • target_date: the create flow stores an absolute RFC3339 instant from a
//     YYYY-MM-DD picker. We pre-fill the picker from the stored instant (local
//     date). On submit, if the user CLEARED it → send target_date: '' (the
//     backend's TargetDateSet="" path CLEARS it; absent = unchanged). If set to a
//     new date → send the RFC3339 instant. If unchanged → omit it entirely.
const PLAN_EDIT_MODAL_INPUT =
  'mt-1 block w-full rounded border border-border-base bg-bg-elevated px-3 py-2 text-sm text-text-primary placeholder:text-text-muted focus:border-accent';

// A stored RFC3339 instant → the YYYY-MM-DD the date picker expects (local date).
function instantToDateInput(instant: string | null | undefined): string {
  if (!instant) return '';
  const d = new Date(instant);
  if (Number.isNaN(d.getTime())) return '';
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function friendlyPatchError(error: unknown): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('archived')) {
    return 'This plan is archived and can no longer be edited.';
  }
  if (lower.includes('draft')) {
    // T238: name/goal edit any time; only the target date is draft-only.
    return 'The target date can only be changed while the plan is a draft.';
  }
  return "Couldn't save your changes. Please try again.";
}

function PlanEditModal({
  projectId,
  plan,
  onClose,
}: {
  projectId: string;
  plan: Plan;
  onClose: () => void;
}): React.ReactElement {
  const [name, setName] = useState(plan.name);
  const [description, setDescription] = useState(plan.description ?? '');
  const [targetDate, setTargetDate] = useState(() => instantToDateInput(plan.target_date));
  const patch = usePatchPlan(projectId, plan.id);
  // T238: name + goal are editable in any non-archived status; target_date
  // (scheduling) stays draft-only, so the field only shows for a draft plan.
  const isDraft = plan.status === 'draft';

  // The picker value the field STARTED at — used to detect "cleared" vs "changed"
  // vs "unchanged" without re-parsing the stored instant on every render.
  const originalTargetDate = instantToDateInput(plan.target_date);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;

    const input: PatchPlanInput = {};
    // name — required; send only when changed.
    if (name.trim() !== plan.name) input.name = name.trim();
    // goal (= description) — send only when changed (cleared → '').
    if (description.trim() !== (plan.description ?? '')) input.description = description.trim();
    // target_date — draft-only (§9.4); distinguish cleared / changed / unchanged.
    if (isDraft && targetDate !== originalTargetDate) {
      if (targetDate === '') {
        // cleared → '' (the backend's TargetDateSet="" path CLEARS it).
        input.target_date = '';
      } else {
        // YYYY-MM-DD → RFC3339 with local offset (absolute instant), matching
        // the create flow so the backend stores an absolute time, not naive UTC.
        const d = new Date(`${targetDate}T00:00:00`);
        if (!Number.isNaN(d.getTime())) input.target_date = d.toISOString();
      }
    }

    try {
      await patch.mutateAsync(input);
      onClose();
    } catch {
      // surfaced inline below (#218)
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50"
      data-testid="plan-edit-modal"
      role="dialog"
      aria-modal="true"
      aria-label="Edit plan"
    >
      <form onSubmit={submit} className="w-full max-w-lg rounded-lg bg-bg-elevated p-6 text-text-primary shadow-xl">
        <h2 className="mb-4 text-lg font-semibold">Edit Plan</h2>
        <label className="block text-xs font-medium" htmlFor="plan-edit-name">
          Name
        </label>
        <input
          id="plan-edit-name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-edit-name"
          autoFocus
        />
        <label className="mt-3 block text-xs font-medium" htmlFor="plan-edit-description">
          Goal
        </label>
        <textarea
          id="plan-edit-description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={3}
          className={PLAN_EDIT_MODAL_INPUT}
          data-testid="plan-edit-description"
        />
        {isDraft && (
          <>
            <label className="mt-3 block text-xs font-medium" htmlFor="plan-edit-target-date">
              Target date
            </label>
            <input
              id="plan-edit-target-date"
              type="date"
              lang="en"
              value={targetDate}
              onChange={(e) => setTargetDate(e.target.value)}
              className={PLAN_EDIT_MODAL_INPUT}
              data-testid="plan-edit-target-date"
            />
          </>
        )}
        {patch.isError && (
          <p className="mt-3 text-xs font-medium text-danger" role="alert" data-testid="plan-edit-error">
            {friendlyPatchError(patch.error)}
          </p>
        )}
        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            className="rounded border border-border-base px-3 py-1.5 text-sm text-text-primary hover:bg-bg-subtle"
            onClick={onClose}
            data-testid="plan-edit-cancel"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={patch.isPending || !name.trim()}
            className="rounded bg-brand px-3 py-1.5 text-sm font-medium text-white hover:bg-brand-hover disabled:bg-bg-subtle disabled:text-text-muted"
            data-testid="plan-edit-submit"
          >
            {patch.isPending ? 'Saving…' : 'Save changes'}
          </button>
        </div>
      </form>
    </div>
  );
}

function TabButton({
  id,
  active,
  onSelect,
  children,
}: {
  id: Tab;
  active: boolean;
  onSelect: (t: Tab) => void;
  children: React.ReactNode;
}): React.ReactElement {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      data-testid={`plan-tab-${id}`}
      onClick={() => onSelect(id)}
      className={`rounded-t-lg border border-b-0 px-3.5 py-1.5 text-xs font-semibold ${
        active
          ? 'border-border-base bg-bg-elevated text-text-primary shadow-[inset_0_2px_0_var(--color-accent,#3b82f6)]'
          : 'border-transparent bg-bg-subtle text-text-secondary hover:text-text-primary'
      }`}
    >
      {children}
    </button>
  );
}

// ── 6-state node palette (LOCKED, Tester2 computed-truth) ────────────────────
// SOLID X-100/X-800 literal pairs (theme-independent, both-mode AA ~6-9 contrast)
// + matching X-300 border + an inline SVG icon (NOT emoji). label+SVG+color is
// the triple-distinguisher (the 6 states are color-close, so never color alone).
interface NodeStateStyle {
  label: string;
  cls: string; // bg + text (the chip)
  border: string; // node box border (X-300)
  icon: React.ReactElement;
}

const ICON_CLS = 'h-2.5 w-2.5';

const NODE_STATE: Record<PlanNodeStatus, NodeStateStyle> = {
  blocked: {
    label: 'blocked',
    cls: 'bg-status-slate-bg text-status-slate-fg',
    border: 'border-status-slate-border',
    // lock
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <rect x="5" y="11" width="14" height="9" rx="2" />
        <path d="M8 11V7a4 4 0 0 1 8 0v4" />
      </svg>
    ),
  },
  ready: {
    label: 'ready',
    cls: 'bg-status-blue-bg text-status-blue-fg',
    border: 'border-status-blue-border',
    // circle (○)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <circle cx="12" cy="12" r="8" />
      </svg>
    ),
  },
  dispatched: {
    label: 'dispatched',
    cls: 'bg-status-violet-bg text-status-violet-fg',
    border: 'border-status-violet-border',
    // clock / hourglass
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7v5l3 2" />
      </svg>
    ),
  },
  running: {
    label: 'running',
    cls: 'bg-status-amber-bg text-status-amber-fg',
    border: 'border-status-amber-border',
    // play (▶)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="currentColor" aria-hidden="true">
        <path d="M7 5v14l12-7z" />
      </svg>
    ),
  },
  // T53: the agent paused its work item — the node is set aside, not actively
  // running. Stone (muted warm-gray) reads as "halted/waiting", distinct from the
  // amber `running`, so the DAG tells the truth instead of a phantom-running node.
  paused: {
    label: 'paused',
    cls: 'bg-status-stone-bg text-status-stone-fg',
    border: 'border-status-stone-border',
    // pause (⏸)
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="currentColor" aria-hidden="true">
        <rect x="6" y="5" width="4" height="14" rx="1" />
        <rect x="14" y="5" width="4" height="14" rx="1" />
      </svg>
    ),
  },
  done: {
    label: 'done',
    cls: 'bg-status-emerald-bg text-status-emerald-fg',
    border: 'border-status-emerald-border',
    // check mark
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true">
        <path d="M5 13l4 4L19 7" />
      </svg>
    ),
  },
  failed: {
    label: 'failed',
    cls: 'bg-status-rose-bg text-status-rose-fg',
    border: 'border-status-rose-border',
    // cross / x
    icon: (
      <svg viewBox="0 0 24 24" className={ICON_CLS} fill="none" stroke="currentColor" strokeWidth="3" aria-hidden="true">
        <path d="M6 6l12 12M18 6L6 18" />
      </svg>
    ),
  },
};

const NODE_STATE_ORDER: PlanNodeStatus[] = ['blocked', 'ready', 'dispatched', 'running', 'paused', 'done', 'failed'];

function NodeStateChip({ status }: { status: PlanNodeStatus }): React.ReactElement {
  const s = NODE_STATE[status] ?? NODE_STATE.blocked;
  return (
    <span
      className={`inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[0.625rem] font-bold uppercase tracking-wide ${s.cls}`}
      data-testid="node-state-chip"
      data-node-status={status}
    >
      {s.icon}
      {s.label}
    </span>
  );
}

// TaskIdTag — a small monospace pill showing the human Task id (org_ref "T123"),
// taken DIRECTLY from the PlanNode's own org_ref (api/plans PlanNode.org_ref —
// list_plans/detail already return it; T126 removed the old FE task-list re-
// resolver that missed completed tasks → #id-tail). Falls back to the FULL
// task_id (never a #id-tail hash) when org_ref is absent. Full task_id on hover.
function TaskIdTag({
  taskId,
  orgRef,
  testId,
}: {
  taskId: string;
  orgRef?: string;
  testId: string;
}): React.ReactElement {
  const label = refLabel(orgRef, taskId);
  return (
    <span
      className="inline-flex shrink-0 items-center rounded bg-bg-subtle px-1 py-0.5 font-mono text-[0.625rem] font-semibold text-text-secondary"
      data-testid={testId}
      title={taskId}
    >
      {label}
    </span>
  );
}

// assignee_ref → avatar (agent/human) + clean handle.
function AssigneeTag({ assigneeRef }: { assigneeRef: string }): React.ReactElement {
  const resolveName = useDisplayNameResolver();
  if (!assigneeRef) {
    return <span className="text-text-muted">—</span>;
  }
  const kind = refKind(assigneeRef) === 'agent' ? 'agent' : 'human';
  const resolved = resolveName(assigneeRef);
  const label = resolved === assigneeRef ? normalizeIdentityRef(assigneeRef) : resolved;
  return (
    <span className="inline-flex items-center gap-1.5 text-text-secondary" title={assigneeRef}>
      <Avatar name={label} kind={kind} size="sm" />
      <span className="truncate">{label}</span>
    </span>
  );
}

// ── DAG (the core) ──────────────────────────────────────────────────────────
// Layered left→right layout from node.depends_on:
//   level(n) = 0 if no (in-plan) deps else max(level(dep))+1   (longest-path)
//   x = level * COL_W;  y = even vertical spread within the level.
// Edges: SVG path from each dep node's RIGHT-mid → this node's LEFT-mid, with an
// arrow marker (upstream → downstream). node_status is DERIVED → display only.
const COL_W = 200;
const NODE_W = 168;
const NODE_H = 84;
const ROW_GAP = 28;
const PAD_X = 14;
const PAD_Y = 16;

interface Positioned {
  node: PlanNode;
  level: number;
  x: number;
  y: number;
}

// v2.9 Stage A5 — synthetic Start/End flow anchors (NOT real tasks): a Start
// node a column LEFT of all roots (edges → every level-0 root) and an End node
// a column RIGHT of the deepest level (edges ← every leaf, i.e. a node nothing
// else depends on). They give parallel/independent chains a clear left→right
// progression. They are layout/flow markers only: no node_status / 6-state
// chip, not dispatchable, not counted, not in the task list.
interface SyntheticAnchor {
  // center point of the anchor marker (used for both placement + edge endpoint)
  cx: number;
  cy: number;
  // the real-node anchor points this connects to (root left-mids for Start,
  // leaf right-mids for End)
  links: { taskId: string; x: number; y: number }[];
}

const SYNTH_R = 26; // synthetic marker radius (circle terminal)

function layoutDag(nodes: PlanNode[]): {
  positioned: Positioned[];
  width: number;
  height: number;
  start: SyntheticAnchor | null;
  end: SyntheticAnchor | null;
} {
  const byId = new Map(nodes.map((n) => [n.task_id, n]));
  // Only consider deps that are actually in this plan (defensive — a dangling
  // dep ref must not break level computation).
  const depsOf = (n: PlanNode) => n.depends_on.filter((d) => byId.has(d));

  // Longest-path level via memoized DFS (cycle-guarded; the DAG is acyclic by
  // contract, the guard just prevents a hang on bad data).
  const levelCache = new Map<string, number>();
  const inStack = new Set<string>();
  function level(id: string): number {
    if (levelCache.has(id)) return levelCache.get(id)!;
    if (inStack.has(id)) return 0; // cycle guard
    const n = byId.get(id);
    if (!n) return 0;
    inStack.add(id);
    const deps = depsOf(n);
    const lvl = deps.length === 0 ? 0 : Math.max(...deps.map((d) => level(d) + 1));
    inStack.delete(id);
    levelCache.set(id, lvl);
    return lvl;
  }

  // Group by level, preserving input order within a level.
  const byLevel = new Map<number, PlanNode[]>();
  let maxLevel = 0;
  for (const n of nodes) {
    const lvl = level(n.task_id);
    maxLevel = Math.max(maxLevel, lvl);
    const arr = byLevel.get(lvl) ?? [];
    arr.push(n);
    byLevel.set(lvl, arr);
  }

  // Reserve a left gutter column for the Start anchor when there are any nodes,
  // so real nodes are shifted right by one column (Start sits at x≈PAD_X, real
  // level-0 nodes at PAD_X + COL_W). Empty plan ⇒ no gutter, no anchors.
  const hasNodes = nodes.length > 0;
  const SYNTH_COL = COL_W; // width of each synthetic gutter column
  const baseX = PAD_X + (hasNodes ? SYNTH_COL : 0);

  // Even vertical spread within each level.
  const positioned: Positioned[] = [];
  let maxRows = 0;
  for (const [lvl, group] of byLevel) {
    maxRows = Math.max(maxRows, group.length);
    group.forEach((node, row) => {
      positioned.push({
        node,
        level: lvl,
        x: baseX + lvl * COL_W,
        y: PAD_Y + row * (NODE_H + ROW_GAP),
      });
    });
  }

  // Roots = real level-0 nodes (no in-plan deps). Leaves = nodes that no other
  // in-plan node depends on. Start → every root; every leaf → End.
  const dependedOn = new Set<string>();
  for (const n of nodes) for (const d of depsOf(n)) dependedOn.add(d);

  const contentHeight = Math.max(PAD_Y * 2 + maxRows * (NODE_H + ROW_GAP) - ROW_GAP, 200);
  const midY = contentHeight / 2;

  let start: SyntheticAnchor | null = null;
  let end: SyntheticAnchor | null = null;
  if (hasNodes) {
    const roots = positioned.filter((p) => p.level === 0);
    const leaves = positioned.filter((p) => !dependedOn.has(p.node.task_id));
    start = {
      cx: PAD_X + SYNTH_R,
      cy: midY,
      // edge endpoint = root node's LEFT-mid
      links: roots.map((p) => ({ taskId: p.node.task_id, x: p.x, y: p.y + NODE_H / 2 })),
    };
    const endCx = baseX + (maxLevel + 1) * COL_W + SYNTH_R;
    end = {
      cx: endCx,
      cy: midY,
      // edge endpoint = leaf node's RIGHT-mid
      links: leaves.map((p) => ({ taskId: p.node.task_id, x: p.x + NODE_W, y: p.y + NODE_H / 2 })),
    };
  }

  // Width spans from PAD_X (Start) to End marker (when present), else the real
  // layout extent.
  const realRight = baseX + maxLevel * COL_W + NODE_W;
  const width = hasNodes
    ? (end ? end.cx + SYNTH_R + PAD_X : realRight + PAD_X)
    : PAD_X * 2 + (maxLevel + 1) * COL_W - (COL_W - NODE_W);
  const height = contentHeight;
  return { positioned, width, height, start, end };
}

// Synthetic Start/End flow anchor — a distinct, non-task marker. No node_status
// / 6-state chip, no assignee, not clickable, not counted. Solid theme tokens
// (both-mode AA), plain text "Start"/"End" (no emoji). Positioned by its center
// (cx,cy) so it lines up with its flow edges.
function SyntheticAnchorMarker({
  kind,
  anchor,
}: {
  kind: 'start' | 'end';
  anchor: SyntheticAnchor;
}): React.ReactElement {
  const label = kind === 'start' ? 'Start' : 'End';
  return (
    <div
      className="absolute flex items-center justify-center rounded-full border-[1.5px] border-border-strong bg-bg-elevated text-[0.625rem] font-semibold uppercase tracking-wide text-text-secondary shadow-1"
      style={{
        left: anchor.cx - SYNTH_R,
        top: anchor.cy - SYNTH_R,
        width: SYNTH_R * 2,
        height: SYNTH_R * 2,
      }}
      data-testid={`plan-dag-synthetic-${kind}`}
      aria-hidden="true"
    >
      {label}
    </div>
  );
}

// ── PlanStepper (mobile <md) ─────────────────────────────────────────────────
// v2.10.1 [M4] On mobile the left→right SVG DAG (PlanDag) becomes a VERTICAL
// stepper / timeline (mockup `docs/design/v2.10.1/v2.10.1-mobile` — Plan frame).
// It fixes the 375px-critical③: the wide absolute-positioned graph had touch
// targets too small + dead vertical height. The stepper renders the same nodes
// in topological order (by DAG level) as big, tappable cards with a
// status-colored timeline dot — display-only (in-graph dependency EDITING stays
// desktop-only). The same node tokens/components (NODE_STATE / NodeStateChip /
// TaskIdTag / TaskTitleLink / AssigneeTag) are reused so the two views stay in
// sync.
function PlanStepper({
  positioned,
  projectId,
}: {
  positioned: Positioned[];
  projectId: string;
}): React.ReactElement {
  // Topological-ish order: DAG level, then stable vertical position within it.
  const ordered = useMemo(
    () => [...positioned].sort((a, b) => a.level - b.level || a.y - b.y || a.x - b.x),
    [positioned],
  );
  return (
    <ol className="relative mt-1 md:hidden" data-testid="plan-stepper">
      {ordered.map((p, i) => {
        const s = NODE_STATE[p.node.node_status] ?? NODE_STATE.blocked;
        const taskId = p.node.task_id;
        const last = i === ordered.length - 1;
        return (
          <li
            key={taskId}
            className="relative pb-3 pl-6"
            data-testid="plan-stepper-node"
            data-task-id={taskId}
            data-node-status={p.node.node_status}
            data-level={p.level}
          >
            {/* Timeline rail (omit after the last node). */}
            {!last && (
              <span
                aria-hidden="true"
                className="absolute bottom-0 left-[5px] top-5 w-px bg-border-base"
              />
            )}
            {/* Status-colored timeline dot (reuses the node state tokens). */}
            <span
              aria-hidden="true"
              data-testid="plan-stepper-dot"
              className={`absolute left-0 top-3.5 h-3 w-3 rounded-full border ${s.border} ${s.cls}`}
            />
            <div className={`rounded-lg border bg-bg-elevated p-2.5 shadow-1 ${s.border}`}>
              <div className="mb-1 flex items-center justify-between gap-2">
                <TaskIdTag taskId={taskId} orgRef={p.node.org_ref} testId="plan-stepper-taskid" />
                <span className="inline-flex items-center gap-1">
                  <TaskArchivedBadge archived={p.node.archived} taskId={taskId} />
                  <NodeStateChip status={p.node.node_status} />
                </span>
              </div>
              {/* Big tappable title (≥44px touch target), opens the task. */}
              <TaskTitleLink
                projectId={projectId}
                taskId={taskId}
                title={p.node.title || refLabel(p.node.org_ref, taskId)}
                className="min-h-[44px] py-1 text-sm font-semibold"
              />
              <div className="mt-0.5 text-xs">
                <AssigneeTag assigneeRef={p.node.assignee_ref} />
              </div>
            </div>
          </li>
        );
      })}
    </ol>
  );
}

function PlanDag({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const isDraft = plan.status === 'draft';
  // v2.9.1 UX point 2: a "Compact" toggle uniformly zooms the DAG down so a long
  // (many-level) / wide plan fits in view without endless horizontal scrolling.
  // CSS transform (content scales cleanly, no node-content overflow); the scroll
  // area is sized to the scaled extent. Layout algorithm is untouched.
  const [compact, setCompact] = useState(false);
  const scale = compact ? 0.7 : 1;

  // v2.9.1 point 3: IN-GRAPH dependency editing (draft-only). The dependency
  // STRUCTURE is edited directly on the graph — no separate dropdown box (§21
  // single entry). Each draft node has a focusable "connect" control that enters
  // CONNECT MODE with that node as the source; valid targets (validDropTargets,
  // = excludes self/exists/cycle — cycle/self blocked at the UI layer) light up
  // as activatable targets. Each existing edge has a focusable delete control.
  // Add = AddPlanDependency(from=source, to=target) → "source depends on target".
  const addDep = useAddDependency(projectId, plan.id);
  const removeDep = useRemoveDependency(projectId, plan.id);
  // connectFrom = the SOURCE task_id of the in-progress connection (null = not in
  // connect mode). Only meaningful while draft.
  const [connectFrom, setConnectFrom] = useState<string | null>(null);
  const titleOf = useCallback(
    (taskId: string) => {
      const n = nodes.find((m) => m.task_id === taskId);
      return n?.title || refLabel(n?.org_ref, taskId);
    },
    [nodes],
  );

  const exitConnect = useCallback(() => setConnectFrom(null), []);

  // Escape exits connect mode without adding (a11y: a cancel affordance is also
  // rendered visibly below).
  useEffect(() => {
    if (connectFrom == null) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') exitConnect();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [connectFrom, exitConnect]);

  // Leaving draft (e.g. plan starts) must drop any in-progress connect mode.
  useEffect(() => {
    if (!isDraft) setConnectFrom(null);
  }, [isDraft]);

  // The legal targets for the active source (self/exists/cycle excluded). Only
  // these become activatable target controls; everything else is inert.
  const dropTargets = useMemo(
    () => (connectFrom != null ? validDropTargets(nodes, connectFrom) : new Set<string>()),
    [nodes, connectFrom],
  );

  const onTargetActivate = useCallback(
    (target: string) => {
      if (connectFrom == null) return;
      // UI-layer guard (cycle/self never even offered, but double-check).
      if (dependencyEdgeError(nodes, connectFrom, target) !== null) return;
      addDep.mutate(
        { from_task_id: connectFrom, to_task_id: target },
        { onSuccess: () => setConnectFrom(null) },
      );
      // Exit connect mode immediately (optimistic UX); on error the friendly
      // message surfaces below and the user can retry.
      setConnectFrom(null);
    },
    [addDep, connectFrom, nodes],
  );

  const mutationError = addDep.isError ? addDep.error : removeDep.isError ? removeDep.error : null;

  const { positioned, width, height, start, end } = useMemo(() => layoutDag(nodes), [nodes]);
  const posById = useMemo(
    () => new Map(positioned.map((p) => [p.node.task_id, p])),
    [positioned],
  );

  // Edges: dep (upstream) → node (downstream). Path from dep right-mid to node
  // left-mid; a horizontal-ease cubic for a clean orthogonal-ish curve.
  const edges = useMemo(() => {
    // Each real edge: dep (upstream `to`) → node (downstream `from`). The plan
    // node `p` lists `depId` in depends_on, i.e. "p depends on depId" ⟺
    // AddPlanDependency(from=p, to=depId). `from`/`to` are kept on the edge so a
    // draft delete control can call RemoveDependency with the exact ids; `mx/my`
    // is the curve midpoint where the delete control is anchored.
    const out: { key: string; d: string; from: string; to: string; mx: number; my: number }[] = [];
    for (const p of positioned) {
      for (const depId of p.node.depends_on) {
        const dep = posById.get(depId);
        if (!dep) continue;
        const x1 = dep.x + NODE_W;
        const y1 = dep.y + NODE_H / 2;
        const x2 = p.x;
        const y2 = p.y + NODE_H / 2;
        const midX = (x1 + x2) / 2;
        out.push({
          key: `${depId}->${p.node.task_id}`,
          d: `M${x1},${y1} C${midX},${y1} ${midX},${y2} ${x2},${y2}`,
          from: p.node.task_id,
          to: depId,
          mx: midX,
          my: (y1 + y2) / 2,
        });
      }
    }
    return out;
  }, [positioned, posById]);

  // v2.9 A5: synthetic flow edges — Start anchor → each root's left-mid, and
  // each leaf's right-mid → End anchor. Same cubic shape as real edges; kept on
  // a SEPARATE testid (`plan-dag-synthetic-edge`) so real-edge assertions/counts
  // are unaffected. A dashed/lighter stroke reads as a flow anchor, not a dep.
  const synthEdges = useMemo(() => {
    const out: { key: string; d: string }[] = [];
    if (start) {
      for (const l of start.links) {
        const midX = (start.cx + l.x) / 2;
        out.push({
          key: `start->${l.taskId}`,
          d: `M${start.cx},${start.cy} C${midX},${start.cy} ${midX},${l.y} ${l.x},${l.y}`,
        });
      }
    }
    if (end) {
      for (const l of end.links) {
        const midX = (l.x + end.cx) / 2;
        out.push({
          key: `${l.taskId}->end`,
          d: `M${l.x},${l.y} C${midX},${l.y} ${midX},${end.cy} ${end.cx},${end.cy}`,
        });
      }
    }
    return out;
  }, [start, end]);

  return (
    <div data-testid="plan-dag">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-dag-empty">
          No tasks in this plan yet. Add tasks from the Work Board.
        </p>
      ) : (
        <>
        {/* v2.10.1 [M4] Mobile (<md): the left→right SVG DAG becomes a vertical
            stepper. The desktop graph + its controls are md:-only. */}
        <PlanStepper positioned={positioned} projectId={projectId} />
        {/* v2.9.1 point 2: compact (zoom-to-fit) toggle for long/wide DAGs. */}
        <div className="mb-2 hidden items-center justify-between gap-2 md:flex">
          {/* Connect-mode banner (point 3, draft-only): shown while a connection
              is in progress. Tells the user to pick a highlighted target and
              offers a visible Cancel affordance (Escape also exits). */}
          {isDraft && connectFrom != null ? (
            <div
              className="flex flex-1 items-center gap-2 rounded border border-accent bg-bg-elevated px-2 py-1 text-[0.6875rem] text-text-secondary"
              data-testid="plan-connect-banner"
              role="status"
            >
              <span className="min-w-0 truncate">
                Pick a highlighted task that{' '}
                <span className="font-semibold text-text-primary">{titleOf(connectFrom)}</span> depends on.
              </span>
              <button
                type="button"
                data-testid="plan-connect-cancel"
                onClick={exitConnect}
                aria-label="Cancel adding dependency"
                className="ml-auto shrink-0 rounded border border-border-strong bg-bg-subtle px-2 py-0.5 font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
              >
                Cancel
              </button>
            </div>
          ) : (
            <span />
          )}
          <button
            type="button"
            onClick={() => setCompact((c) => !c)}
            aria-pressed={compact}
            data-testid="plan-dag-compact-toggle"
            className="shrink-0 rounded border border-border-strong px-2 py-0.5 text-[0.6875rem] font-medium text-text-secondary hover:bg-bg-subtle hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent"
          >
            {compact ? 'Compact: on' : 'Compact'}
          </button>
        </div>
        <div
          className="relative hidden overflow-auto rounded-lg border border-border-base bg-bg-subtle md:block"
          data-testid="plan-dag-canvas"
          data-compact={compact ? 'true' : 'false'}
          style={{ maxHeight: 480 }}
        >
          {/* Sizing wrapper reserves the SCALED extent so the scroll area is
              correct; the inner layer keeps its natural size and is zoomed via
              transform (transform doesn't affect layout box). */}
          <div style={{ width: width * scale, height: height * scale }}>
          <div
            className="relative"
            data-testid="plan-dag-scaler"
            style={{
              width,
              height,
              transform: scale === 1 ? undefined : `scale(${scale})`,
              transformOrigin: 'top left',
            }}
          >
            {/* Edges (z-0, behind nodes). */}
            <svg
              className="absolute left-0 top-0"
              width={width}
              height={height}
              data-testid="plan-dag-svg"
              aria-hidden="true"
            >
              <defs>
                <marker
                  id="plan-dag-arrow"
                  viewBox="0 0 10 10"
                  refX="8"
                  refY="5"
                  markerWidth="7"
                  markerHeight="7"
                  orient="auto-start-reverse"
                >
                  <path d="M0,0 L10,5 L0,10 z" className="fill-border-strong" />
                </marker>
              </defs>
              <g fill="none" className="stroke-border-strong" strokeWidth="1.6" markerEnd="url(#plan-dag-arrow)">
                {edges.map((e) => (
                  <path key={e.key} d={e.d} data-testid="plan-dag-edge" data-edge={e.key} />
                ))}
              </g>
              {/* Synthetic flow edges (Start→roots, leaves→End): lighter +
                  dashed so they read as flow anchors, not real dependencies. */}
              <g
                fill="none"
                className="stroke-border-base"
                strokeWidth="1.4"
                strokeDasharray="4 3"
                markerEnd="url(#plan-dag-arrow)"
              >
                {synthEdges.map((e) => (
                  <path
                    key={e.key}
                    d={e.d}
                    data-testid="plan-dag-synthetic-edge"
                    data-edge={e.key}
                  />
                ))}
              </g>
            </svg>
            {/* Draft-only IN-GRAPH edge delete controls (z-10, anchored at each
                edge's curve midpoint). A real <button> (keyboard-focusable) →
                useRemoveDependency.mutate({from, to}). Running/done = none. */}
            {isDraft &&
              edges.map((e) => (
                <button
                  key={`del-${e.from}->${e.to}`}
                  type="button"
                  data-testid="plan-edge-delete"
                  // from->to (dependent->dependency) matches the RemoveDependency
                  // body: "from depends on to".
                  data-edge={`${e.from}->${e.to}`}
                  disabled={removeDep.isPending}
                  onClick={() => removeDep.mutate({ from_task_id: e.from, to_task_id: e.to })}
                  aria-label={`Remove dependency: ${titleOf(e.from)} depends on ${titleOf(e.to)}`}
                  title={`Remove dependency: ${titleOf(e.from)} depends on ${titleOf(e.to)}`}
                  className="absolute z-10 flex h-5 w-5 items-center justify-center rounded-full border border-border-strong bg-bg-elevated text-xs font-bold leading-none text-text-secondary shadow-1 hover:bg-bg-subtle hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-50"
                  // centered on the edge midpoint (button is 20px = w-5/h-5).
                  style={{ left: e.mx - 10, top: e.my - 10 }}
                >
                  {/* ASCII multiplication sign (not emoji). */}
                  <span aria-hidden="true">&times;</span>
                </button>
              ))}
            {/* Nodes (z-10). */}
            {positioned.map((p) => {
              const s = NODE_STATE[p.node.node_status] ?? NODE_STATE.blocked;
              const taskId = p.node.task_id;
              const inConnect = connectFrom != null;
              const isSource = connectFrom === taskId;
              const isTarget = inConnect && !isSource && dropTargets.has(taskId);
              return (
                <div
                  key={taskId}
                  className={`absolute rounded-lg border-[1.5px] bg-bg-elevated p-2 shadow-1 ${
                    isTarget
                      ? 'border-accent ring-2 ring-accent'
                      : isSource
                        ? 'border-accent'
                        : s.border
                  }`}
                  style={{ left: p.x, top: p.y, width: NODE_W }}
                  data-testid="plan-dag-node"
                  data-task-id={taskId}
                  data-level={p.level}
                  data-connect-source={isSource ? 'true' : undefined}
                  data-connect-target={isTarget ? 'true' : undefined}
                >
                  {/* v2.9.1 UX point 1: human Task id (T-number) visible on the node. */}
                  <div className="mb-1 flex items-center justify-between gap-1">
                    <TaskIdTag taskId={taskId} orgRef={p.node.org_ref} testId="plan-node-taskid" />
                    {/* Draft connect control (point 3): a real keyboard-focusable
                        button. Activating enters connect mode with this node as
                        the source. Hidden once running/done (display-only). */}
                    {isDraft && !inConnect && (
                      <button
                        type="button"
                        data-testid="plan-node-connect"
                        data-task-id={taskId}
                        onClick={() => setConnectFrom(taskId)}
                        aria-label={`Add dependency from ${titleOf(taskId)}`}
                        title={`Add a dependency from ${titleOf(taskId)} (pick the task it depends on)`}
                        className="shrink-0 rounded border border-border-strong bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                      >
                        + Dep
                      </button>
                    )}
                  </div>
                  <div className="mb-1.5 text-xs font-semibold text-text-primary" title={p.node.title}>
                    <TaskTitleLink
                      projectId={projectId}
                      taskId={taskId}
                      title={p.node.title || refLabel(p.node.org_ref, taskId)}
                    />
                  </div>
                  <div className="flex items-center justify-between gap-1.5">
                    <span className="min-w-0 text-[0.6875rem]">
                      <AssigneeTag assigneeRef={p.node.assignee_ref} />
                    </span>
                    <span className="inline-flex items-center gap-1">
                      <TaskArchivedBadge archived={p.node.archived} taskId={taskId} />
                      <NodeStateChip status={p.node.node_status} />
                    </span>
                  </div>
                  {/* Connect-mode target affordance (point 3): ONLY valid targets
                      (self/exists/cycle excluded) become activatable. A real
                      keyboard-focusable button overlaying the node; activating it
                      adds "source depends on target". Invalid nodes get nothing. */}
                  {isTarget && (
                    <button
                      type="button"
                      data-testid="plan-connect-target"
                      data-task-id={taskId}
                      onClick={() => onTargetActivate(taskId)}
                      aria-label={`Make ${titleOf(connectFrom!)} depend on ${titleOf(taskId)}`}
                      title={`Make ${titleOf(connectFrom!)} depend on ${titleOf(taskId)}`}
                      className="absolute inset-0 rounded-lg border-2 border-accent bg-transparent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
                    />
                  )}
                </div>
              );
            })}
            {/* Synthetic Start/End anchors (z-10) — distinct flow markers, NOT
                tasks: no node_status / 6-state chip, no assignee, not
                clickable/dispatchable, not in any count. Rendered as a labeled
                circular terminal. */}
            {start && <SyntheticAnchorMarker kind="start" anchor={start} />}
            {end && <SyntheticAnchorMarker kind="end" anchor={end} />}
          </div>
          </div>
        </div>
        </>
      )}

      {/* Legend (all 6 states) — the lifecycle controls live in the header
          (Start / Stop / Advance), rendered exactly once each there. */}
      <div className="mt-3 flex flex-wrap items-center gap-1.5" data-testid="plan-dag-legend">
        {NODE_STATE_ORDER.map((st) => (
          <NodeStateChip key={st} status={st} />
        ))}
      </div>

      {/* node_status is DERIVED (§9.2) and shown, not edited. In DRAFT the
          dependency STRUCTURE is editable IN-GRAPH (point 3): each node has a
          "+ Dep" connect control, and each edge has an "×" delete control. Once
          running/done the graph is DISPLAY-ONLY (backend rejects with
          ErrPlanNotDraft). */}
      <p className="mt-2 text-[0.6875rem] text-text-muted" data-testid="plan-dag-note">
        Node status is derived (= f(task status, all upstream done, dispatch record)) and shown, not edited.{' '}
        {isDraft ? (
          <>
            This plan is a draft, so its dependencies are editable right on the graph — use a node's "+ Dep"
            button to add an edge, or an edge's "×" to remove one.
          </>
        ) : (
          <>
            Display-only graph: a running plan auto-advances (the system dispatches ready nodes as upstream
            tasks complete) and "Advance now" is a manual override. Dependencies can only be edited while the
            plan is a draft.
          </>
        )}
      </p>

      {/* #218 friendly add/remove error (never the raw API message). The
          single in-graph entry point (§21) — no separate editor box. */}
      {isDraft && mutationError && (
        <p className="mt-2 text-xs font-medium text-danger" role="alert" data-testid="plan-edge-error">
          {friendlyDependencyError(mutationError)}
        </p>
      )}
    </div>
  );
}

// ── Draft-only dependency-edge editor (v2.9 Stage A1) ────────────────────────
// from/to semantics (verified against the backend, plan_view.go + plan_flow.go):
// a node's `depends_on` lists `edge.ToTaskID` where `edge.FromTaskID == node`,
// i.e. AddPlanDependency(from, to) means **from depends_on to** (`to` is the
// upstream dependency that completes first; `from` is the downstream dependent).
// So an edge "B depends on A" → { from_task_id: B, to_task_id: A }.
//
// #218: add/remove failures map the backend Go error message (all surface as a
// 400 invalid_request, distinguished by the message text) to a FRIENDLY string;
// the raw error is never shown.
function friendlyDependencyError(error: unknown): string {
  const raw = error instanceof Error ? error.message : String(error ?? '');
  const lower = raw.toLowerCase();
  if (lower.includes('itself') || lower.includes('self')) {
    return "A task can't depend on itself.";
  }
  if (lower.includes('cycle')) {
    return 'That would create a cycle in the plan.';
  }
  if (lower.includes('draft')) {
    return 'Dependencies can only be edited while the plan is a draft.';
  }
  return "Couldn't update the plan's dependencies. Please try again.";
}

// ── Task list tab ────────────────────────────────────────────────────────────
// §9.4: removing a task from a Plan is a PLANNING action — only a DRAFT plan
// exposes a per-row "Remove" control (consistent with add-to-plan / the A1 edge
// editor). A running/done plan renders the rows read-only (no Remove column).
// memberRef — build the prefixed identity ref ("agent:<id>"/"user:<id>") for an
// assignee <option>, mirroring TaskEditModal.memberRef (kind derived when absent).
// Reuses the shared identityRefOf when kind is present; falls back to deriving
// kind from the id for legacy rows with no explicit kind.
function memberRef(m: MemberResult): string {
  const kind = m.kind ?? (m.identity_id.startsWith('agent') ? 'agent' : 'user');
  return identityRefOf({ kind, identity_id: m.identity_id });
}

// T41 (v2.9.1 #291): the Task-list tab is the COMPREHENSIVE management surface
// for a big plan — every node is rendered (no cap, ever), a search box narrows
// the visible rows by title / Task-id / assignee, and the table scrolls
// vertically within the tab. Inline assignee reassignment lives per-row.
function PlanTaskList({ projectId, plan }: { projectId: string; plan: Plan }): React.ReactElement {
  const nodes = plan.nodes ?? [];
  const canRemove = plan.status === 'draft';
  const members = useMembers();
  const [query, setQuery] = useState('');

  // Case-insensitive filter on title OR Task-id (org_ref) OR assignee handle.
  // Empty box ⇒ ALL nodes (never capped). Matching keeps input order. org_ref
  // comes straight off the node (T126).
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return nodes;
    return nodes.filter((n) => {
      const orgRef = n.org_ref ?? '';
      const assignee = n.assignee_ref ? normalizeIdentityRef(n.assignee_ref) : '';
      const haystack = `${n.title ?? ''} ${orgRef} ${assignee}`.toLowerCase();
      return haystack.includes(q);
    });
  }, [nodes, query]);

  return (
    <div data-testid="plan-task-list">
      {nodes.length === 0 ? (
        <p className="py-10 text-center text-xs text-text-muted" data-testid="plan-task-list-empty">
          No tasks in this plan yet.
        </p>
      ) : (
        <>
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              data-testid="plan-task-search"
              aria-label="Filter tasks"
              placeholder="Filter by title, Task id, or assignee…"
              className="min-w-[14rem] flex-1 rounded border border-border-base bg-bg-elevated px-2 py-1 text-xs text-text-primary placeholder:text-text-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            />
            <span className="text-[0.6875rem] text-text-muted" data-testid="plan-task-search-count">
              Showing {filtered.length} of {nodes.length}
            </span>
          </div>
          {filtered.length === 0 ? (
            <p className="py-8 text-center text-xs text-text-muted" data-testid="plan-task-search-empty">
              No tasks match your filter.
            </p>
          ) : (
            <div className="max-h-[28rem] overflow-x-auto overflow-y-auto">
              <table className="w-full text-left text-xs" data-testid="plan-task-list-table">
                <thead>
                  <tr className="border-b border-border-base text-[0.625rem] uppercase tracking-wide text-text-muted">
                    <th className="py-1.5 pr-3 font-medium">Task</th>
                    <th className="py-1.5 pr-3 font-medium">Title</th>
                    <th className="py-1.5 pr-3 font-medium">Assignee</th>
                    <th className="py-1.5 pr-3 font-medium">Task status</th>
                    <th className="py-1.5 font-medium">Node status</th>
                    {canRemove && <th className="py-1.5 pl-3 text-right font-medium">Action</th>}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border-base">
                  {filtered.map((n) => (
                    <PlanTaskRow
                      key={n.task_id}
                      projectId={projectId}
                      planId={plan.id}
                      node={n}
                      orgRef={n.org_ref}
                      canRemove={canRemove}
                      members={members.data ?? []}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  );
}

// resumeNodeErrorMessage (T101) — turn the resume failure into an ACCURATE operator
// hint instead of a generic "try again". A node shows `paused` because its agent set
// the work item aside (pause_task) — and the agent typically did so to switch to
// ANOTHER task, so it is now active on that one. Operator resume then hits the
// single-active invariant (ResumeWorkByOperator → 409 agent_busy): the right answer
// is to explain WHY, not retry blindly. Maps the backend error CODE (ApiError.code);
// any unmapped error keeps the original generic copy.
function resumeNodeErrorMessage(error: unknown): string {
  const code = error instanceof ApiError ? error.code : '';
  switch (code) {
    case 'agent_busy':
      // The agent paused this item to work another; it can't be double-activated.
      return "Its agent is busy on another work item — it'll resume this one when that finishes, or the agent can resume it from its side.";
    case 'node_not_paused':
      return 'Nothing to resume — this node has no paused work item (it may have already resumed).';
    case 'plan_not_running':
      return "The plan isn't running, so its nodes can't be resumed.";
    default:
      return "Couldn't resume this node. Please try again.";
  }
}

// PlanTaskRow — one task-list row. When the plan is draft, the trailing cell
// holds a "Remove" button → useRemoveTaskFromPlan(task_id) (task returns to the
// Backlog on success via query invalidation). #218: a remove failure surfaces a
// friendly inline message in the row, never a raw API error.
function PlanTaskRow({
  projectId,
  planId,
  node,
  orgRef,
  canRemove,
  members,
}: {
  projectId: string;
  planId: string;
  node: PlanNode;
  orgRef?: string;
  canRemove: boolean;
  members: MemberResult[];
}): React.ReactElement {
  const remove = useRemoveTaskFromPlan(projectId, planId);
  // T53: operator resume of a paused node (its agent set the work item aside).
  const resume = useResumePausedNode(projectId, planId);
  // T41 inline 分派: reassigning is NOT draft-gated (allowed regardless of plan
  // status). assign("") would set an empty assignee; the dedicated unassign
  // endpoint is the established "clear assignee" path, so route "" → unassign.
  const assign = useAssignTask(projectId, node.task_id);
  const unassign = useUnassignTask(projectId, node.task_id);
  const assignError = assign.isError || unassign.isError;
  const onAssigneeChange = (next: string) => {
    if (next === '') unassign.mutate();
    else assign.mutate({ assignee: next });
  };
  const title = node.title || refLabel(node.org_ref, node.task_id);
  // T147: ONE assignee control. Build the dropdown options — "" = Unassigned
  // (routes to the unassign endpoint), then each project member with an avatar
  // leading so the (single) dropdown trigger shows the current assignee's
  // avatar + name. Mirrors the old <option> list (memberRef + "name (kind)").
  const assigneeOptions: EntityOption[] = useMemo(() => {
    const opts: EntityOption[] = [{ value: '', label: 'Unassigned' }];
    for (const m of members) {
      const name = m.display_name ?? normalizeIdentityRef(m.identity_id);
      opts.push({
        value: memberRef(m),
        label: name,
        badge: m.kind,
        leading: <Avatar name={name} kind={m.kind === 'agent' ? 'agent' : 'human'} size="sm" />,
      });
    }
    return opts;
  }, [members]);
  return (
    <tr data-testid="plan-task-row" data-task-id={node.task_id}>
      {/* v2.9.1 UX point 1: human Task id (T-number) column. */}
      <td className="py-1.5 pr-3 align-top">
        <TaskIdTag taskId={node.task_id} orgRef={orgRef} testId="plan-row-taskid" />
      </td>
      <td className="max-w-[18rem] py-1.5 pr-3 text-text-primary" title={node.title}>
        <TaskTitleLink
          projectId={projectId}
          taskId={node.task_id}
          title={title}
        />
        {remove.isError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-task-remove-error-${node.task_id}`}
          >
            Couldn't remove this task from the plan. Please try again.
          </span>
        )}
      </td>
      <td className="py-1.5 pr-3 align-top">
        {/* T147: a SINGLE dropdown — its trigger shows the current assignee
            (avatar + name), and opening it reassigns. Replaces the old redundant
            pair (a read-only AssigneeTag stacked above a separate <select> that
            showed the same value). "" routes to the unassign endpoint. */}
        {/* Width is driven by min-width (not max): the Task-List table is
            width-constrained, so a `max-w` cap never widens the column — it
            only ever shrinks toward the min. The avatar + padding + chevron eat
            ~4rem of the trigger, so an 11rem column left only ~15 chars of text
            and truncated common handles (agent-center-tester1 → "agent-center-te…").
            15rem fits ~25 chars; the table's overflow-x-auto scrolls if a row
            ever needs more, and EntitySelect keeps a title tooltip as a fallback. */}
        <div className="min-w-[15rem] max-w-[20rem]">
          <EntitySelect
            testId="plan-row-assign"
            options={assigneeOptions}
            value={node.assignee_ref ?? ''}
            onChange={onAssigneeChange}
            ariaLabel={`Reassign ${title}`}
            disabled={assign.isPending || unassign.isPending}
            placeholder="Unassigned"
            searchPlaceholder="Search members…"
          />
        </div>
        {assignError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-task-assign-error-${node.task_id}`}
          >
            Couldn't reassign this task. Please try again.
          </span>
        )}
      </td>
      <td className="py-1.5 pr-3">
        <StatusChip status={node.task_status} />
      </td>
      <td className="py-1.5">
        <span className="inline-flex items-center gap-1.5">
          <NodeStateChip status={node.node_status} />
          {/* Stage B (#283): archive badge is ORTHOGONAL — coexists with the
              node-status chip when the plan (and thus the task) is archived. */}
          <TaskArchivedBadge archived={node.archived} taskId={node.task_id} />
          {/* T53: a paused node (agent set its work item aside) gets an operator
              Resume action that resumes the node + wakes its agent. */}
          {node.node_status === 'paused' && (
            <button
              type="button"
              className="rounded border border-status-stone-border bg-status-stone-bg px-2 py-0.5 text-[0.6875rem] font-semibold text-status-stone-fg hover:opacity-80 disabled:opacity-50"
              disabled={resume.isPending}
              aria-label={`Resume ${title}`}
              title="Resume this paused node (wake its agent)"
              data-testid={`plan-node-resume-${node.task_id}`}
              onClick={() => resume.mutate(node.task_id)}
            >
              {resume.isPending ? 'Resuming…' : 'Resume'}
            </button>
          )}
        </span>
        {resume.isError && (
          <span
            className="mt-0.5 block text-[0.6875rem] font-normal text-danger"
            role="alert"
            data-testid={`plan-node-resume-error-${node.task_id}`}
          >
            {resumeNodeErrorMessage(resume.error)}
          </span>
        )}
      </td>
      {canRemove && (
        <td className="py-1.5 pl-3 text-right">
          <button
            type="button"
            className="rounded border border-border-strong bg-bg-subtle px-2 py-0.5 text-[0.6875rem] font-semibold text-text-secondary hover:bg-bg-base hover:text-text-primary disabled:opacity-50"
            disabled={remove.isPending}
            aria-label={`Remove ${node.title || refLabel(node.org_ref, node.task_id)} from plan`}
            title="Remove from plan (back to backlog)"
            data-testid={`plan-task-remove-${node.task_id}`}
            onClick={() => remove.mutate(node.task_id)}
          >
            Remove
          </button>
        </td>
      )}
    </tr>
  );
}

// ── Plan conversation side (REUSE ConversationView) ──────────────────────────
// This is where the orchestrator @-dispatches + discussion appear (bound post
// #266). Render the Plan's conversation by its conversation_id. Empty
// conversation_id → friendly "initializing" state (don't crash).
function PlanConversationSide({ conversationId }: { conversationId: string }): React.ReactElement {
  const conv = useConversation(conversationId || undefined);

  return (
    <SenderSidebarProvider>
      <section className="flex min-h-0 flex-1 flex-col" data-testid="plan-conversation">
        <div className="mb-2 flex items-center gap-2 text-sm font-semibold text-text-primary">
          Plan conversation
          <span className="rounded border border-border-base px-1.5 py-0.5 text-[0.625rem] font-normal uppercase tracking-wide text-text-muted">
            chat
          </span>
        </div>

        {!conversationId ? (
          <p
            className="rounded border border-dashed border-border-base p-4 text-xs italic text-text-muted"
            data-testid="plan-conversation-initializing"
          >
            Conversation initializing — the plan's chat is being set up.
          </p>
        ) : (
          <div
            className="flex min-h-0 flex-1 flex-col overflow-hidden rounded border border-border-base"
            data-testid="plan-conversation-body"
          >
            <ConversationView surface="task-thread" conversationId={conversationId} />
            {conv.isError && (
              <p className="p-2 text-[0.6875rem] text-text-muted">
                Couldn't refresh conversation details.
              </p>
            )}
          </div>
        )}
        <p className="mt-2 text-[0.6875rem] text-text-muted">
          Dispatch = @assignee in this conversation (notify human / wake agent); also the place to discuss this plan.
        </p>
      </section>
    </SenderSidebarProvider>
  );
}
